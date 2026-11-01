package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// Role is what a Tailscale login may do in Bield.
type Role string

// The roles a login can have.
const (
	// Admin can do everything: open terminals, change files, control Docker
	// and change Bield's settings, including who has a role.
	Admin Role = "admin"
	// Viewer can see the read-only dashboard, without logs, files, process
	// command lines or other secrets.
	Viewer Role = "viewer"
)

// Who can view Bield, as set with SetWhoCanView.
const (
	// Tailnet makes anyone who signs in from the tailnet a viewer, unless
	// they have a role of their own.
	Tailnet = "tailnet"
	// Listed lets only the logins with a role view Bield.
	Listed = "listed"
)

// DefaultWhoCanView is who can view Bield until an admin changes it. Bield
// starts closed: sharing Bield's machine with someone, or a tailnet policy
// that lets every member reach every device, shouldn't show them the whole
// tailnet. Letting more people in later is one command.
const DefaultWhoCanView = Listed

// whoCanViewKey is the kv key that holds who can view Bield.
const whoCanViewKey = "who_can_view"

// ErrLastAdmin means a change would leave Bield without an admin.
var ErrLastAdmin = errors.New("the last admin has to stay an admin, so make someone else an admin first")

// A User is a Tailscale login with a role in Bield.
type User struct {
	Login string
	Role  Role
	// Since is when the login got its role.
	Since time.Time
}

// Users returns the logins that have a role, in order.
func (s *Store) Users(ctx context.Context) ([]User, error) {
	users, err := usersIn(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

// Access returns who can view Bield, Tailnet or Listed, and the logins that
// have a role, in order. It reads both in one transaction, so a change made
// at the same time can't show up in one and not the other.
func (s *Store) Access(ctx context.Context) (whoCanView string, users []User, err error) {
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		if whoCanView, err = whoCanViewIn(ctx, tx); err != nil {
			return err
		}
		users, err = usersIn(ctx, tx)
		return err
	})
	if err != nil {
		return "", nil, fmt.Errorf("look up who has access: %w", err)
	}
	return whoCanView, users, nil
}

// Role returns the role a signed-in Tailscale login has, ignoring case: the
// one it was given, or else Viewer if anyone on the tailnet can view Bield. It
// returns "" if the login can't view Bield.
func (s *Store) Role(ctx context.Context, login string) (Role, error) {
	if login == "" {
		return "", nil
	}
	var role, who sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT (SELECT role FROM roles WHERE login = ?), (SELECT value FROM kv WHERE key = ?)",
		login, whoCanViewKey).Scan(&role, &who)
	switch {
	case err != nil:
		return "", fmt.Errorf("look up a role: %w", err)
	case role.Valid:
		return Role(role.String), nil
	case parseWhoCanView(who) == Tailnet:
		return Viewer, nil
	}
	return "", nil
}

// SetRole gives login a role, and reports whether that changed anything. It
// returns ErrLastAdmin instead of making Bield's last admin a viewer.
func (s *Store) SetRole(ctx context.Context, login string, role Role) (changed bool, err error) {
	if login, err = CleanLogin(login); err != nil {
		return false, err
	}
	if role != Admin && role != Viewer {
		return false, fmt.Errorf("unknown role %q", role)
	}
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		was, err := roleIn(ctx, tx, login)
		if err != nil || was == role {
			return err
		}
		if was == Admin {
			if err := keepAnAdmin(ctx, tx, login); err != nil {
				return err
			}
		}
		if err := grant(ctx, tx, login, role); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("make %s %s: %w", login, article(role), err)
	}
	return changed, nil
}

// RemoveUser takes login's role away, and reports whether it had one. It
// returns ErrLastAdmin instead of removing Bield's last admin.
func (s *Store) RemoveUser(ctx context.Context, login string) (removed bool, err error) {
	if login, err = CleanLogin(login); err != nil {
		return false, err
	}
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		was, err := roleIn(ctx, tx, login)
		if err != nil || was == "" {
			return err
		}
		if was == Admin {
			if err := keepAnAdmin(ctx, tx, login); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM roles WHERE login = ?", login); err != nil {
			return err
		}
		removed = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("remove %s: %w", login, err)
	}
	return removed, nil
}

// AddFirstAdmins makes logins admins if Bield has no admins yet, and reports
// whether it did. Checking and adding happen in one transaction, so it can't
// race with SetRole.
func (s *Store) AddFirstAdmins(ctx context.Context, logins []string) (added bool, err error) {
	if len(logins) == 0 {
		return false, nil
	}
	clean := make([]string, len(logins))
	for i, login := range logins {
		if clean[i], err = CleanLogin(login); err != nil {
			return false, err
		}
	}
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM roles WHERE role = 'admin'").Scan(&n); err != nil || n > 0 {
			return err
		}
		for _, login := range clean {
			if err := grant(ctx, tx, login, Admin); err != nil {
				return err
			}
		}
		added = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("add the first admins: %w", err)
	}
	return added, nil
}

// WhoCanView returns who can view Bield: Tailnet or Listed.
func (s *Store) WhoCanView(ctx context.Context) (string, error) {
	who, err := whoCanViewIn(ctx, s.db)
	if err != nil {
		return "", fmt.Errorf("look up who can view Bield: %w", err)
	}
	return who, nil
}

// SetWhoCanView sets who can view Bield, Tailnet or Listed, and reports
// whether that changed anything.
func (s *Store) SetWhoCanView(ctx context.Context, who string) (changed bool, err error) {
	if who != Tailnet && who != Listed {
		return false, fmt.Errorf("who can view Bield is %q or %q, not %q", Tailnet, Listed, who)
	}
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		was, err := whoCanViewIn(ctx, tx)
		if err != nil || was == who {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
			whoCanViewKey, who); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("set who can view Bield: %w", err)
	}
	return changed, nil
}

// CleanLogin trims login and checks that it can be a login: one word, which
// Tailscale makes an email address such as alice@example.com or alice@github.
// A Headscale tailnet names a user by email if they have one and otherwise by
// name, such as alice, so an @ can't be required.
func CleanLogin(login string) (string, error) {
	login = strings.TrimSpace(login)
	if login == "" || strings.ContainsFunc(login, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", fmt.Errorf("%q isn't a login, such as alice@example.com (or alice on a Headscale tailnet)", login)
	}
	return login, nil
}

// inTx runs fn in a transaction, which holds the write lock from the start,
// and commits it if fn succeeds.
func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// After a successful Commit, Rollback does nothing.
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// A querier is a *sql.DB or a *sql.Tx.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// usersIn returns the logins that have a role, in order.
func usersIn(ctx context.Context, q querier) ([]User, error) {
	rows, err := q.QueryContext(ctx, "SELECT login, role, granted_at FROM roles ORDER BY login")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var users []User
	for rows.Next() {
		var u User
		var since int64
		if err := rows.Scan(&u.Login, &u.Role, &since); err != nil {
			return nil, err
		}
		u.Since = time.UnixMilli(since)
		users = append(users, u)
	}
	return users, rows.Err()
}

// whoCanViewIn reads who can view Bield.
func whoCanViewIn(ctx context.Context, q querier) (string, error) {
	var who sql.NullString
	err := q.QueryRowContext(ctx, "SELECT value FROM kv WHERE key = ?", whoCanViewKey).Scan(&who)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	return parseWhoCanView(who), nil
}

// parseWhoCanView turns the stored who can view setting into Tailnet or
// Listed. Until it's set, it's DefaultWhoCanView. Any value but Tailnet,
// even an empty one, is Listed, so a bad value lets no one else in. Role and
// WhoCanView both use it, so what Bield enforces is what it reports.
func parseWhoCanView(stored sql.NullString) string {
	switch {
	case !stored.Valid:
		return DefaultWhoCanView
	case stored.String == Tailnet:
		return Tailnet
	default:
		return Listed
	}
}

// roleIn returns the role login was given, or "" if it has none.
func roleIn(ctx context.Context, tx *sql.Tx, login string) (Role, error) {
	var role Role
	err := tx.QueryRowContext(ctx, "SELECT role FROM roles WHERE login = ?", login).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// keepAnAdmin returns ErrLastAdmin if login is the only admin.
func keepAnAdmin(ctx context.Context, tx *sql.Tx, login string) error {
	var others int
	err := tx.QueryRowContext(ctx, "SELECT count(*) FROM roles WHERE role = 'admin' AND login <> ?", login).Scan(&others)
	switch {
	case err != nil:
		return err
	case others == 0:
		return ErrLastAdmin
	}
	return nil
}

// grant gives login role, as of now.
func grant(ctx context.Context, tx *sql.Tx, login string, role Role) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO roles (login, role, granted_at) VALUES (?, ?, ?)
		ON CONFLICT (login) DO UPDATE SET role = excluded.role, granted_at = excluded.granted_at`,
		login, role, time.Now().UnixMilli())
	return err
}

// article returns role with "a" or "an" in front.
func article(role Role) string {
	if role == Admin {
		return "an admin"
	}
	return "a " + string(role)
}

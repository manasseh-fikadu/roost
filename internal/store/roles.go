package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Role is what a Tailscale login may do in Roost.
type Role string

// The roles a login can have.
const (
	// Admin can do everything: open terminals, change files, control Docker
	// and change Roost's settings, including who has a role.
	Admin Role = "admin"
	// Viewer can see the read-only dashboard, without logs, files, process
	// command lines or other secrets.
	Viewer Role = "viewer"
)

// Who can view Roost, as set with SetWhoCanView.
const (
	// Tailnet makes anyone who signs in from the tailnet a viewer, unless
	// they have a role of their own.
	Tailnet = "tailnet"
	// Listed lets only the logins with a role view Roost.
	Listed = "listed"
)

// DefaultWhoCanView is who can view Roost until an admin changes it.
const DefaultWhoCanView = Tailnet

// whoCanViewKey is the kv key that holds who can view Roost.
const whoCanViewKey = "who_can_view"

// ErrLastAdmin means a change would leave Roost without an admin.
var ErrLastAdmin = errors.New("the last admin has to stay an admin, so make someone else an admin first")

// A User is a Tailscale login with a role in Roost.
type User struct {
	Login string
	Role  Role
	// Since is when the login got its role.
	Since time.Time
}

// Users returns the logins that have a role, in order.
func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT login, role, granted_at FROM roles ORDER BY login")
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var users []User
	for rows.Next() {
		var u User
		var since int64
		if err := rows.Scan(&u.Login, &u.Role, &since); err != nil {
			return nil, fmt.Errorf("list users: %w", err)
		}
		u.Since = time.UnixMilli(since)
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

// Role returns the role a signed-in Tailscale login has, ignoring case: the
// one it was given, or else Viewer if anyone on the tailnet can view Roost. It
// returns "" if the login can't view Roost.
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
// returns ErrLastAdmin instead of making Roost's last admin a viewer.
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
// returns ErrLastAdmin instead of removing Roost's last admin.
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

// AddFirstAdmins makes logins admins if Roost has no admins yet, and reports
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

// WhoCanView returns who can view Roost: Tailnet or Listed.
func (s *Store) WhoCanView(ctx context.Context) (string, error) {
	who, err := whoCanView(ctx, s.db)
	if err != nil {
		return "", fmt.Errorf("look up who can view Roost: %w", err)
	}
	return who, nil
}

// SetWhoCanView sets who can view Roost, Tailnet or Listed, and reports
// whether that changed anything.
func (s *Store) SetWhoCanView(ctx context.Context, who string) (changed bool, err error) {
	if who != Tailnet && who != Listed {
		return false, fmt.Errorf("who can view Roost is %q or %q, not %q", Tailnet, Listed, who)
	}
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		was, err := whoCanView(ctx, tx)
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
		return false, fmt.Errorf("set who can view Roost: %w", err)
	}
	return changed, nil
}

// CleanLogin trims login and checks that it looks like a Tailscale login,
// which always has an @, as in alice@example.com or alice@github.
func CleanLogin(login string) (string, error) {
	login = strings.TrimSpace(login)
	if i := strings.IndexByte(login, '@'); i <= 0 || i == len(login)-1 || strings.ContainsAny(login, " \t\r\n") {
		return "", fmt.Errorf("%q isn't a Tailscale login, such as alice@example.com", login)
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
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// whoCanView reads who can view Roost.
func whoCanView(ctx context.Context, q querier) (string, error) {
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
// WhoCanView both use it, so what Roost enforces is what it reports.
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

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Admins returns the logins of Roost's admins, in order.
func (s *Store) Admins(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT login FROM admins ORDER BY login")
	if err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var logins []string
	for rows.Next() {
		var login string
		if err := rows.Scan(&login); err != nil {
			return nil, fmt.Errorf("list admins: %w", err)
		}
		logins = append(logins, login)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list admins: %w", err)
	}
	return logins, nil
}

// IsAdmin reports whether login is one of Roost's admins, ignoring case.
func (s *Store) IsAdmin(ctx context.Context, login string) (bool, error) {
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM admins WHERE login = ?", login).Scan(new(int))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("look up admin: %w", err)
	}
	return true, nil
}

// AddAdmin makes login an admin, and reports false if it already was one.
func (s *Store) AddAdmin(ctx context.Context, login string) (bool, error) {
	login, err := cleanLogin(login)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO admins (login, added_at) VALUES (?, ?) ON CONFLICT DO NOTHING",
		login, time.Now().UnixMilli())
	if err != nil {
		return false, fmt.Errorf("add admin: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("add admin: %w", err)
	}
	return n > 0, nil
}

// AddFirstAdmins makes logins admins if Roost has no admins yet, and reports
// whether it did. Checking and adding happen in one transaction, so it can't
// race with AddAdmin.
func (s *Store) AddFirstAdmins(ctx context.Context, logins []string) (added bool, err error) {
	if len(logins) == 0 {
		return false, nil
	}
	clean := make([]string, len(logins))
	for i, login := range logins {
		if clean[i], err = cleanLogin(login); err != nil {
			return false, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("add the first admins: %w", err)
	}
	defer func() {
		if !added {
			_ = tx.Rollback()
		}
	}()
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM admins").Scan(&n); err != nil {
		return false, fmt.Errorf("add the first admins: %w", err)
	}
	if n > 0 {
		return false, nil
	}
	now := time.Now().UnixMilli()
	for _, login := range clean {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO admins (login, added_at) VALUES (?, ?) ON CONFLICT DO NOTHING", login, now); err != nil {
			return false, fmt.Errorf("add the first admins: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("add the first admins: %w", err)
	}
	return true, nil
}

// cleanLogin trims login and checks that it looks like a Tailscale login,
// which always has an @, as in alice@example.com or alice@github.
func cleanLogin(login string) (string, error) {
	login = strings.TrimSpace(login)
	if i := strings.IndexByte(login, '@'); i <= 0 || i == len(login)-1 || strings.ContainsAny(login, " \t\r\n") {
		return "", fmt.Errorf("%q isn't a Tailscale login, such as alice@example.com", login)
	}
	return login, nil
}

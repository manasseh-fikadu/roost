package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoSSHAccess means SSH access isn't set up for the device.
var ErrNoSSHAccess = errors.New("SSH access isn't set up for this device")

// SSHAccess is how Bield logs in to a machine over SSH.
type SSHAccess struct {
	// DeviceID is the device's ID, or HubID for the machine Bield runs on.
	DeviceID string
	// User is the account Bield logs in as, and Port is where sshd listens.
	User string
	Port int
	// HostKey is the machine's host key as in known_hosts, such as
	// "ssh-ed25519 AAAA...", pinned on the first connection. It's "" until
	// then, or after an admin resets it.
	HostKey  string
	PinnedAt time.Time
	// CreatedAt is when it was set up, and CreatedBy the login of the admin
	// who did, or "" if Bield did.
	CreatedAt time.Time
	CreatedBy string
}

// SSHAccess returns how Bield logs in to every machine that has SSH access
// set up, by device ID.
func (s *Store) SSHAccess(ctx context.Context) (map[string]SSHAccess, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT device_id, username, port, host_key, pinned_at, created_at, created_by FROM ssh_access")
	if err != nil {
		return nil, fmt.Errorf("read SSH access: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]SSHAccess{}
	for rows.Next() {
		var (
			a       SSHAccess
			hostKey sql.NullString
			pinned  sql.NullInt64
			created int64
		)
		if err := rows.Scan(&a.DeviceID, &a.User, &a.Port, &hostKey, &pinned, &created, &a.CreatedBy); err != nil {
			return nil, fmt.Errorf("read SSH access: %w", err)
		}
		a.HostKey, a.CreatedAt = hostKey.String, time.UnixMilli(created)
		if pinned.Valid {
			a.PinnedAt = time.UnixMilli(pinned.Int64)
		}
		out[a.DeviceID] = a
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read SSH access: %w", err)
	}
	return out, nil
}

// SetSSHAccess saves how Bield logs in to a machine, replacing what was
// saved for it.
func (s *Store) SetSSHAccess(ctx context.Context, a SSHAccess) error {
	var pinned sql.NullInt64
	if a.HostKey != "" {
		pinned = sql.NullInt64{Int64: a.PinnedAt.UnixMilli(), Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO ssh_access (device_id, username, port, host_key, pinned_at, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (device_id) DO UPDATE SET username = excluded.username, port = excluded.port,
			host_key = excluded.host_key, pinned_at = excluded.pinned_at,
			created_at = excluded.created_at, created_by = excluded.created_by`,
		a.DeviceID, a.User, a.Port, nullString(a.HostKey), pinned, a.CreatedAt.UnixMilli(), a.CreatedBy)
	if err != nil {
		return fmt.Errorf("save SSH access: %w", err)
	}
	return nil
}

// DeleteSSHAccess forgets how Bield logs in to a machine. It returns
// ErrNoSSHAccess if nothing was saved for it.
func (s *Store) DeleteSSHAccess(ctx context.Context, deviceID string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM ssh_access WHERE device_id = ?", deviceID)
	if err != nil {
		return fmt.Errorf("remove SSH access: %w", err)
	}
	return noRow(res, ErrNoSSHAccess)
}

// PinHostKey saves a machine's host key, unless one is pinned already, and
// reports whether it did. It returns ErrNoSSHAccess if nothing is saved for
// the machine.
func (s *Store) PinHostKey(ctx context.Context, deviceID, key string, at time.Time) (bool, error) {
	var pinned bool
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var current sql.NullString
		err := tx.QueryRowContext(ctx, "SELECT host_key FROM ssh_access WHERE device_id = ?", deviceID).Scan(&current)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return ErrNoSSHAccess
		case err != nil:
			return err
		case current.Valid:
			return nil
		}
		_, err = tx.ExecContext(ctx, "UPDATE ssh_access SET host_key = ?, pinned_at = ? WHERE device_id = ?",
			key, at.UnixMilli(), deviceID)
		pinned = err == nil
		return err
	})
	if err != nil && !errors.Is(err, ErrNoSSHAccess) {
		return false, fmt.Errorf("pin a host key: %w", err)
	}
	return pinned, err
}

// ReplaceHostKey pins key as a machine's host key, in place of the one
// pinned now, if any. It returns ErrNoSSHAccess if nothing is saved for the
// machine.
func (s *Store) ReplaceHostKey(ctx context.Context, deviceID, key string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, "UPDATE ssh_access SET host_key = ?, pinned_at = ? WHERE device_id = ?",
		key, at.UnixMilli(), deviceID)
	if err != nil {
		return fmt.Errorf("pin a host key: %w", err)
	}
	return noRow(res, ErrNoSSHAccess)
}

// ResetHostKey forgets a machine's pinned host key, so the next connection
// pins the one it offers. It returns ErrNoSSHAccess if nothing is saved for
// the machine.
func (s *Store) ResetHostKey(ctx context.Context, deviceID string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE ssh_access SET host_key = NULL, pinned_at = NULL WHERE device_id = ?", deviceID)
	if err != nil {
		return fmt.Errorf("reset a host key: %w", err)
	}
	return noRow(res, ErrNoSSHAccess)
}

// noRow returns notFound if res changed no row.
func noRow(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	switch {
	case err != nil:
		return err
	case n == 0:
		return notFound
	}
	return nil
}

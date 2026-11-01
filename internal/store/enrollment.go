package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// EnrollmentCodeLife is how long an enrollment code works after an admin
// makes it.
const EnrollmentCodeLife = time.Hour

// Why a machine can't enroll its agent with a code.
var (
	// ErrUnknownCode means Bield didn't make the code, or it expired a while
	// ago.
	ErrUnknownCode = errors.New("unknown enrollment code")
	// ErrCodeExpired means the code expired.
	ErrCodeExpired = errors.New("the enrollment code expired")
	// ErrCodeUsed means another machine enrolled with the code.
	ErrCodeUsed = errors.New("another machine used the enrollment code")
)

// ErrUnknownSecret means no enrolled agent has the secret.
var ErrUnknownSecret = errors.New("no enrolled agent has this secret")

// An Enrollment is when an agent enrolled, and who let it.
type Enrollment struct {
	At time.Time
	// By is the login of the admin who made the code it enrolled with.
	By string
}

// The store keeps only a hash of each code and secret, so the database can't
// be used to enroll or impersonate an agent. Both are random, so a fast hash
// is enough.
func hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// NewEnrollmentCode makes a code that enrolls the agent on one machine, for
// the admin with login by, and returns it and when it expires.
func (s *Store) NewEnrollmentCode(ctx context.Context, by string, now time.Time) (code string, expires time.Time, err error) {
	code, expires = rand.Text(), now.Add(EnrollmentCodeLife)
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO enrollment_codes (code_hash, created_by, created_at, expires_at) VALUES (?, ?, ?, ?)",
		hash(code), by, now.UnixMilli(), expires.UnixMilli())
	if err != nil {
		return "", time.Time{}, fmt.Errorf("make an enrollment code: %w", err)
	}
	return code, expires, nil
}

// CheckEnrollmentCode returns nil if the machine with node ID nodeID can
// enroll with code at now, without using the code. Otherwise it returns
// ErrUnknownCode, ErrCodeExpired or ErrCodeUsed. A nodeID of "" is a machine
// Tailscale doesn't know, which can't use a code another machine used.
func (s *Store) CheckEnrollmentCode(ctx context.Context, code, nodeID string, now time.Time) error {
	if _, err := checkCode(ctx, s.db, code, nodeID, now); err != nil {
		return wrapCodeErr("check an enrollment code", err)
	}
	return nil
}

// Enroll trades code for a new secret for the agent on the machine with node
// ID nodeID, and returns it and the enrollment. The secret replaces any that
// the machine's agent had. A code works for the first machine to use it, and
// for that machine again until it expires. Enroll returns the same errors as
// CheckEnrollmentCode.
func (s *Store) Enroll(ctx context.Context, code, nodeID string, now time.Time) (string, Enrollment, error) {
	if nodeID == "" {
		return "", Enrollment{}, errors.New("enroll an agent: no node ID")
	}
	secret, e := rand.Text(), Enrollment{At: now}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		if e.By, err = checkCode(ctx, tx, code, nodeID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE enrollment_codes SET node_id = ? WHERE code_hash = ?", nodeID, hash(code)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO enrollments (node_id, secret_hash, enrolled_at, enrolled_by) VALUES (?, ?, ?, ?)
			ON CONFLICT (node_id) DO UPDATE SET
				secret_hash = excluded.secret_hash, enrolled_at = excluded.enrolled_at, enrolled_by = excluded.enrolled_by`,
			nodeID, hash(secret), now.UnixMilli(), e.By)
		return err
	})
	if err != nil {
		return "", Enrollment{}, wrapCodeErr("enroll an agent", err)
	}
	return secret, e, nil
}

// checkCode returns who made code, if the machine with node ID nodeID can
// enroll with it at now.
func checkCode(ctx context.Context, q querier, code, nodeID string, now time.Time) (string, error) {
	var (
		by      string
		expires int64
		usedBy  sql.NullString
	)
	err := q.QueryRowContext(ctx,
		"SELECT created_by, expires_at, node_id FROM enrollment_codes WHERE code_hash = ?",
		hash(code)).Scan(&by, &expires, &usedBy)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", ErrUnknownCode
	case err != nil:
		return "", err
	case now.UnixMilli() >= expires:
		return "", ErrCodeExpired
	case usedBy.Valid && usedBy.String != nodeID:
		return "", ErrCodeUsed
	}
	return by, nil
}

// wrapCodeErr returns the errors about the code itself as they are, and
// says what failed for any other.
func wrapCodeErr(what string, err error) error {
	if errors.Is(err, ErrUnknownCode) || errors.Is(err, ErrCodeExpired) || errors.Is(err, ErrCodeUsed) {
		return err
	}
	return fmt.Errorf("%s: %w", what, err)
}

// EnrolledNode returns the node ID of the machine whose agent has secret, or
// ErrUnknownSecret.
func (s *Store) EnrolledNode(ctx context.Context, secret string) (string, error) {
	var nodeID string
	err := s.db.QueryRowContext(ctx,
		"SELECT node_id FROM enrollments WHERE secret_hash = ?", hash(secret)).Scan(&nodeID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", ErrUnknownSecret
	case err != nil:
		return "", fmt.Errorf("look up an agent's secret: %w", err)
	}
	return nodeID, nil
}

// Enrollments returns every enrolled agent's enrollment, by the node ID of
// the machine it runs on.
func (s *Store) Enrollments(ctx context.Context) (map[string]Enrollment, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT node_id, enrolled_at, enrolled_by FROM enrollments")
	if err != nil {
		return nil, fmt.Errorf("read enrollments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]Enrollment{}
	for rows.Next() {
		var (
			id string
			at int64
			e  Enrollment
		)
		if err := rows.Scan(&id, &at, &e.By); err != nil {
			return nil, fmt.Errorf("read enrollments: %w", err)
		}
		e.At = time.UnixMilli(at)
		out[id] = e
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read enrollments: %w", err)
	}
	return out, nil
}

// Unenroll deletes the secrets of the agents on the machines with the node
// IDs nodeIDs, so Bield refuses their heartbeats, and reports whether any
// had one. A machine can still enroll again with the code it used until the
// code expires, such as to install the agent again after uninstalling it.
func (s *Store) Unenroll(ctx context.Context, nodeIDs ...string) (bool, error) {
	return s.unenroll(ctx, "unenroll an agent", false, nodeIDs)
}

// Revoke is Unenroll, except that it also deletes the codes those machines
// enrolled with, so an install command run before can't enroll them again.
func (s *Store) Revoke(ctx context.Context, nodeIDs ...string) (bool, error) {
	return s.unenroll(ctx, "revoke an agent", true, nodeIDs)
}

func (s *Store) unenroll(ctx context.Context, what string, codes bool, nodeIDs []string) (bool, error) {
	var n int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		for _, id := range nodeIDs {
			res, err := tx.ExecContext(ctx, "DELETE FROM enrollments WHERE node_id = ?", id)
			if err != nil {
				return err
			}
			// SQLite always knows how many rows a DELETE affected.
			affected, _ := res.RowsAffected()
			n += affected
			if codes {
				if _, err := tx.ExecContext(ctx, "DELETE FROM enrollment_codes WHERE node_id = ?", id); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("%s: %w", what, err)
	}
	return n > 0, nil
}

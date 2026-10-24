package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// An Event is a line in the activity log: something Roost saw happen, or
// something someone did.
type Event struct {
	ID int64
	At time.Time
	// Level is "info", "success", "warning" or "error".
	Level string
	// Kind says what it's about, such as "device" or "alert".
	Kind    string
	Message string
	// DeviceID is the device it's about, if any.
	DeviceID string
	// Actor is the login of who did it, or "" if Roost saw it happen.
	Actor string
}

// AddEvents adds events to the activity log, in order.
func (s *Store) AddEvents(ctx context.Context, events ...Event) error {
	if len(events) == 0 {
		return nil
	}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx,
			"INSERT INTO events (ts, level, kind, message, device_id, actor) VALUES (?, ?, ?, ?, ?, ?)")
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, e := range events {
			if _, err := stmt.ExecContext(ctx, e.At.UnixMilli(), e.Level, e.Kind, e.Message, nullString(e.DeviceID), e.Actor); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("add to the activity log: %w", err)
	}
	return nil
}

// An EventQuery picks events from the activity log. Its zero value picks
// the latest.
type EventQuery struct {
	// Before and After, if they're set, pick events with an ID below or
	// above them.
	Before, After int64
	// Kind and DeviceID, if they're set, pick events of that kind, or about
	// that device.
	Kind, DeviceID string
	// HideKinds leaves out events of these kinds.
	HideKinds []string
	// Limit is the most events to return.
	Limit int
}

// Events returns the events that q picks, newest first.
func (s *Store) Events(ctx context.Context, q EventQuery) ([]Event, error) {
	var (
		where []string
		args  []any
	)
	if q.Before > 0 {
		where, args = append(where, "id < ?"), append(args, q.Before)
	}
	if q.After > 0 {
		where, args = append(where, "id > ?"), append(args, q.After)
	}
	if q.Kind != "" {
		where, args = append(where, "kind = ?"), append(args, q.Kind)
	}
	if q.DeviceID != "" {
		where, args = append(where, "device_id = ?"), append(args, q.DeviceID)
	}
	for _, k := range q.HideKinds {
		where, args = append(where, "kind != ?"), append(args, k)
	}
	query := "SELECT id, ts, level, kind, message, device_id, actor FROM events"
	if len(where) > 0 {
		// The conditions are constants, with placeholders for the values.
		query += " WHERE " + strings.Join(where, " AND ") //nolint:gosec // G202: see above.
	}
	query += " ORDER BY id DESC LIMIT ?"
	rows, err := s.db.QueryContext(ctx, query, append(args, max(q.Limit, 1))...)
	if err != nil {
		return nil, fmt.Errorf("read the activity log: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Event
	for rows.Next() {
		var (
			e      Event
			at     int64
			device sql.NullString
		)
		if err := rows.Scan(&e.ID, &at, &e.Level, &e.Kind, &e.Message, &device, &e.Actor); err != nil {
			return nil, fmt.Errorf("read the activity log: %w", err)
		}
		e.At, e.DeviceID = time.UnixMilli(at), device.String
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the activity log: %w", err)
	}
	return out, nil
}

// LastEventID returns the ID of the latest event, or 0 if there's none.
func (s *Store) LastEventID(ctx context.Context) (int64, error) {
	var id sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT MAX(id) FROM events").Scan(&id); err != nil {
		return 0, fmt.Errorf("read the activity log: %w", err)
	}
	return id.Int64, nil
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// KV returns the value of a setting in the kv table, and whether there's
// one.
func (s *Store) KV(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM kv WHERE key = ?", key).Scan(&value)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("read the setting %s: %w", key, err)
	}
	return value, true, nil
}

// SetKV sets a setting in the kv table.
func (s *Store) SetKV(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value", key, value)
	if err != nil {
		return fmt.Errorf("save the setting %s: %w", key, err)
	}
	return nil
}

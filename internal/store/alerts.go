package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNoAlert means no alert has the ID.
var ErrNoAlert = errors.New("no alert has this ID")

// An Alert is a condition Bield saw, such as a disk that's nearly full, from
// when it opened until it resolved. Times that haven't happened are zero.
type Alert struct {
	ID int64
	// Key names the condition, such as "disk:hub". A condition has at most
	// one open alert.
	Key string
	// Source says what the alert is about, such as "metrics" or "check".
	Source   string
	DeviceID string
	CheckID  string
	// Severity is "critical", "warning" or "info".
	Severity      string
	Title, Detail string
	OpenedAt      time.Time
	// LastSeen is when Bield last saw the condition.
	LastSeen     time.Time
	ResolvedAt   time.Time
	AckedAt      time.Time
	AckedBy      string
	SnoozedUntil time.Time
	SnoozedBy    string
	// Notes are oldest first.
	Notes []AlertNote
}

// An AlertNote is what an admin wrote on an alert.
type AlertNote struct {
	ID     int64
	At     time.Time
	Author string
	Text   string
}

const alertColumns = `id, key, source, device_id, check_id, severity, title, detail, opened_at, last_seen,
	resolved_at, acked_at, acked_by, snoozed_until, snoozed_by`

// AddAlert saves a new open alert, and returns it with its ID.
func (s *Store) AddAlert(ctx context.Context, a Alert) (Alert, error) {
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO alerts (key, source, device_id, check_id, severity, title, detail, opened_at, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		a.Key, a.Source, nullString(a.DeviceID), nullString(a.CheckID), a.Severity, a.Title, a.Detail,
		a.OpenedAt.UnixMilli(), a.LastSeen.UnixMilli()).Scan(&a.ID)
	if err != nil {
		return Alert{}, fmt.Errorf("open an alert: %w", err)
	}
	return a, nil
}

// SaveAlert saves what can change on an alert: its severity, title and
// detail, when Bield last saw it, and whether it's resolved, acknowledged
// or snoozed. It returns ErrNoAlert if there's no alert with a's ID.
func (s *Store) SaveAlert(ctx context.Context, a Alert) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE alerts SET severity = ?, title = ?, detail = ?, last_seen = ?, resolved_at = ?,
			acked_at = ?, acked_by = ?, snoozed_until = ?, snoozed_by = ?
		WHERE id = ?`,
		a.Severity, a.Title, a.Detail, a.LastSeen.UnixMilli(), nullTime(a.ResolvedAt),
		nullTime(a.AckedAt), nullString(a.AckedBy), nullTime(a.SnoozedUntil), nullString(a.SnoozedBy), a.ID)
	if err != nil {
		return fmt.Errorf("save an alert: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("save an alert: %w", err)
	} else if n == 0 {
		return ErrNoAlert
	}
	return nil
}

// AddAlertNote adds a note to an alert, and returns it with its ID. It
// returns ErrNoAlert if there's no such alert.
func (s *Store) AddAlertNote(ctx context.Context, alertID int64, n AlertNote) (AlertNote, error) {
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO alert_notes (alert_id, ts, author, text)
		SELECT ?1, ?2, ?3, ?4 WHERE EXISTS (SELECT 1 FROM alerts WHERE id = ?1) RETURNING id`,
		alertID, n.At.UnixMilli(), n.Author, n.Text).Scan(&n.ID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return AlertNote{}, ErrNoAlert
	case err != nil:
		return AlertNote{}, fmt.Errorf("add a note to an alert: %w", err)
	}
	return n, nil
}

// OpenAlerts returns every alert that isn't resolved, oldest first, with
// its notes.
func (s *Store) OpenAlerts(ctx context.Context) ([]Alert, error) {
	return s.alerts(ctx, "SELECT "+alertColumns+" FROM alerts WHERE resolved_at IS NULL ORDER BY id")
}

// AlertByID returns the alert with an ID, with its notes, or ErrNoAlert.
func (s *Store) AlertByID(ctx context.Context, id int64) (Alert, error) {
	as, err := s.alerts(ctx, "SELECT "+alertColumns+" FROM alerts WHERE id = ?", id)
	switch {
	case err != nil:
		return Alert{}, err
	case len(as) == 0:
		return Alert{}, ErrNoAlert
	}
	return as[0], nil
}

// ResolvedAlerts returns resolved alerts, the latest opened first, with
// their notes: at most limit of them, with IDs below before if it isn't
// 0.
func (s *Store) ResolvedAlerts(ctx context.Context, before int64, limit int) ([]Alert, error) {
	if before <= 0 {
		before = 1<<63 - 1
	}
	return s.alerts(ctx, "SELECT "+alertColumns+
		" FROM alerts WHERE resolved_at IS NOT NULL AND id < ? ORDER BY id DESC LIMIT ?", before, max(limit, 1))
}

func (s *Store) alerts(ctx context.Context, query string, args ...any) ([]Alert, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Alert
	for rows.Next() {
		var (
			a                                 Alert
			device, check, ackedBy, snoozedBy sql.NullString
			opened, seen                      int64
			resolved, acked, snoozedUntil     sql.NullInt64
		)
		err := rows.Scan(&a.ID, &a.Key, &a.Source, &device, &check, &a.Severity, &a.Title, &a.Detail,
			&opened, &seen, &resolved, &acked, &ackedBy, &snoozedUntil, &snoozedBy)
		if err != nil {
			return nil, fmt.Errorf("read alerts: %w", err)
		}
		a.DeviceID, a.CheckID, a.AckedBy, a.SnoozedBy = device.String, check.String, ackedBy.String, snoozedBy.String
		a.OpenedAt, a.LastSeen = time.UnixMilli(opened), time.UnixMilli(seen)
		a.ResolvedAt, a.AckedAt, a.SnoozedUntil = fromNullTime(resolved), fromNullTime(acked), fromNullTime(snoozedUntil)
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read alerts: %w", err)
	}
	if err := s.addNotes(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// addNotes reads the notes of alerts into them.
func (s *Store) addNotes(ctx context.Context, alerts []Alert) error {
	if len(alerts) == 0 {
		return nil
	}
	byID := make(map[int64]*Alert, len(alerts))
	args := make([]any, len(alerts))
	for i := range alerts {
		byID[alerts[i].ID] = &alerts[i]
		args[i] = alerts[i].ID
	}
	// Only placeholders are joined into the query.
	rows, err := s.db.QueryContext(ctx, "SELECT id, alert_id, ts, author, text FROM alert_notes WHERE alert_id IN (?"+ //nolint:gosec // G202: see above.
		strings.Repeat(", ?", len(alerts)-1)+") ORDER BY id", args...)
	if err != nil {
		return fmt.Errorf("read alerts' notes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			n       AlertNote
			alertID int64
			at      int64
		)
		if err := rows.Scan(&n.ID, &alertID, &at, &n.Author, &n.Text); err != nil {
			return fmt.Errorf("read alerts' notes: %w", err)
		}
		n.At = time.UnixMilli(at)
		a := byID[alertID]
		a.Notes = append(a.Notes, n)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read alerts' notes: %w", err)
	}
	return nil
}

func nullTime(t time.Time) sql.NullInt64 {
	return sql.NullInt64{Int64: t.UnixMilli(), Valid: !t.IsZero()}
}

func fromNullTime(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return time.UnixMilli(v.Int64)
}

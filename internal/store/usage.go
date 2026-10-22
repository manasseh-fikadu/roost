package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// A UsageRow is what used a device's CPU and memory over one minute. Package
// usage encodes and decodes Groups and Detail.
type UsageRow struct {
	DeviceID string
	// At is the end of the minute.
	At time.Time
	// Source is "hub" or "agent".
	Source string
	// Cores is how many CPU cores the device has, or 0 if it didn't say.
	Cores int
	// Groups are the biggest groups of processes, as JSON.
	Groups string
	// Detail is the minute in detail, compressed. It's nil if it was pruned.
	Detail []byte
}

// AddUsage records a minute of a device's usage.
func (s *Store) AddUsage(ctx context.Context, row UsageRow) error {
	cores := sql.NullInt64{Int64: int64(row.Cores), Valid: row.Cores > 0}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO usage_samples (ts, device_id, source, cores, groups, detail) VALUES (?, ?, ?, ?, ?, ?)",
		row.At.UnixMilli(), row.DeviceID, row.Source, cores, row.Groups, row.Detail)
	if err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	return nil
}

// UsageSince returns a device's usage since a time, oldest first, without
// the detail.
func (s *Store) UsageSince(ctx context.Context, deviceID string, since time.Time) ([]UsageRow, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT ts, source, cores, groups FROM usage_samples WHERE device_id = ? AND ts >= ? ORDER BY ts",
		deviceID, since.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read the usage history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []UsageRow
	for rows.Next() {
		row := UsageRow{DeviceID: deviceID}
		var (
			at    int64
			cores sql.NullInt64
		)
		if err := rows.Scan(&at, &row.Source, &cores, &row.Groups); err != nil {
			return nil, fmt.Errorf("read the usage history: %w", err)
		}
		row.At, row.Cores = time.UnixMilli(at), int(cores.Int64)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the usage history: %w", err)
	}
	return out, nil
}

// UsageNear returns the minute of a device's usage that's nearest to at and
// still has its detail, if one is within the given distance of it.
func (s *Store) UsageNear(ctx context.Context, deviceID string, at time.Time, within time.Duration) (UsageRow, bool, error) {
	ms, d := at.UnixMilli(), within.Milliseconds()
	before, okBefore, err := s.usageRow(ctx, `SELECT ts, source, cores, groups, detail FROM usage_samples
		WHERE device_id = ? AND ts BETWEEN ? AND ? AND detail IS NOT NULL ORDER BY ts DESC LIMIT 1`,
		deviceID, ms-d, ms)
	if err != nil {
		return UsageRow{}, false, err
	}
	after, okAfter, err := s.usageRow(ctx, `SELECT ts, source, cores, groups, detail FROM usage_samples
		WHERE device_id = ? AND ts BETWEEN ? AND ? AND detail IS NOT NULL ORDER BY ts LIMIT 1`,
		deviceID, ms, ms+d)
	switch {
	case err != nil:
		return UsageRow{}, false, err
	case okBefore && okAfter && at.Sub(before.At) > after.At.Sub(at):
		return after, true, nil
	case okBefore:
		return before, true, nil
	}
	return after, okAfter, nil
}

// usageRow reads the one row that query finds, with its detail, if there is one.
func (s *Store) usageRow(ctx context.Context, query string, deviceID string, args ...any) (UsageRow, bool, error) {
	row := UsageRow{DeviceID: deviceID}
	var (
		ts    int64
		cores sql.NullInt64
	)
	err := s.db.QueryRowContext(ctx, query, append([]any{deviceID}, args...)...).
		Scan(&ts, &row.Source, &cores, &row.Groups, &row.Detail)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return UsageRow{}, false, nil
	case err != nil:
		return UsageRow{}, false, fmt.Errorf("read usage: %w", err)
	}
	row.At, row.Cores = time.UnixMilli(ts), int(cores.Int64)
	return row, true, nil
}

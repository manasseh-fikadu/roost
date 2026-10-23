package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrNoCheck means no check has the ID.
var ErrNoCheck = errors.New("no check has this ID")

// A Check is a probe that the hub runs every so often, as an admin set it up.
type Check struct {
	ID   string
	Name string
	// Kind is "http", "tcp" or "dns".
	Kind string
	// Target is a URL for an HTTP check, host:port for a TCP check, or a
	// host name for a DNS check.
	Target string
	// Group is the group the check is listed in.
	Group    string
	Interval time.Duration
	Timeout  time.Duration
	// ExpectStatus is the HTTP status to expect, or 0 to accept any below
	// 400.
	ExpectStatus int
	// Insecure skips checking an HTTPS server's certificate.
	Insecure bool
	Paused   bool
	Created  time.Time
}

// Checks returns every check, by group and name.
func (s *Store) Checks(ctx context.Context) ([]Check, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, kind, target, grp, interval_ms, timeout_ms, expect_status, insecure, paused, created_at
		FROM checks ORDER BY grp, name, id`)
	if err != nil {
		return nil, fmt.Errorf("read the checks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Check
	for rows.Next() {
		var (
			c                          Check
			interval, timeout, created int64
			expect                     sql.NullInt64
		)
		err := rows.Scan(&c.ID, &c.Name, &c.Kind, &c.Target, &c.Group, &interval, &timeout,
			&expect, &c.Insecure, &c.Paused, &created)
		if err != nil {
			return nil, fmt.Errorf("read the checks: %w", err)
		}
		c.Interval, c.Timeout = time.Duration(interval)*time.Millisecond, time.Duration(timeout)*time.Millisecond
		c.ExpectStatus, c.Created = int(expect.Int64), time.UnixMilli(created)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the checks: %w", err)
	}
	return out, nil
}

// AddCheck saves a new check with a new ID, and returns it with that ID.
func (s *Store) AddCheck(ctx context.Context, c Check) (Check, error) {
	var id [8]byte
	_, _ = rand.Read(id[:]) // crypto/rand never fails
	c.ID = hex.EncodeToString(id[:])
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO checks (id, name, kind, target, grp, interval_ms, timeout_ms, expect_status, insecure, paused, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Kind, c.Target, c.Group, c.Interval.Milliseconds(), c.Timeout.Milliseconds(),
		nullStatus(c.ExpectStatus), c.Insecure, c.Paused, c.Created.UnixMilli())
	if err != nil {
		return Check{}, fmt.Errorf("add a check: %w", err)
	}
	return c, nil
}

// UpdateCheck replaces the check with c's ID, except when it was created.
// It returns ErrNoCheck if there's none.
func (s *Store) UpdateCheck(ctx context.Context, c Check) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE checks SET name = ?, kind = ?, target = ?, grp = ?, interval_ms = ?, timeout_ms = ?,
			expect_status = ?, insecure = ?, paused = ?
		WHERE id = ?`,
		c.Name, c.Kind, c.Target, c.Group, c.Interval.Milliseconds(), c.Timeout.Milliseconds(),
		nullStatus(c.ExpectStatus), c.Insecure, c.Paused, c.ID)
	if err != nil {
		return fmt.Errorf("change a check: %w", err)
	}
	return oneRow(res, "change a check")
}

// DeleteCheck deletes a check and its results. It returns ErrNoCheck if
// there's none.
func (s *Store) DeleteCheck(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM checks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete a check: %w", err)
	}
	return oneRow(res, "delete a check")
}

// oneRow returns ErrNoCheck if res changed no row.
func oneRow(res sql.Result, what string) error {
	n, err := res.RowsAffected()
	switch {
	case err != nil:
		return fmt.Errorf("%s: %w", what, err)
	case n == 0:
		return ErrNoCheck
	}
	return nil
}

func nullStatus(status int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(status), Valid: status != 0}
}

// A CheckResult is what happened when a check ran once.
type CheckResult struct {
	CheckID string
	// At is when it started.
	At time.Time
	OK bool
	// Latency is how long the target took to answer, or 0 if nothing did.
	Latency time.Duration
	// Detail says what happened, such as "HTTP 200".
	Detail string
}

// AddCheckResult records a result of a check. It returns ErrNoCheck if the
// check has been deleted, so its results are gone too.
func (s *Store) AddCheckResult(ctx context.Context, r CheckResult) error {
	latency := sql.NullFloat64{Float64: ms(r.Latency), Valid: r.Latency > 0}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO check_results (ts, check_id, ok, latency_ms, detail)
		SELECT ?1, ?2, ?3, ?4, ?5 WHERE EXISTS (SELECT 1 FROM checks WHERE id = ?2)`,
		r.At.UnixMilli(), r.CheckID, r.OK, latency, r.Detail)
	if err != nil {
		return fmt.Errorf("record a check's result: %w", err)
	}
	return oneRow(res, "record a check's result")
}

// RecentCheckResults returns the latest n results of each check since a
// time, oldest first, by check ID.
func (s *Store) RecentCheckResults(ctx context.Context, since time.Time, n int) (map[string][]CheckResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT check_id, ts, ok, latency_ms, detail FROM (
			SELECT *, ROW_NUMBER() OVER (PARTITION BY check_id ORDER BY ts DESC) AS nth
			FROM check_results WHERE ts >= ?
		) WHERE nth <= ? ORDER BY check_id, ts`,
		since.UnixMilli(), n)
	if err != nil {
		return nil, fmt.Errorf("read the checks' latest results: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]CheckResult{}
	for rows.Next() {
		var (
			r       CheckResult
			at      int64
			latency sql.NullFloat64
		)
		if err := rows.Scan(&r.CheckID, &at, &r.OK, &latency, &r.Detail); err != nil {
			return nil, fmt.Errorf("read the checks' latest results: %w", err)
		}
		r.At, r.Latency = time.UnixMilli(at), fromMS(latency)
		out[r.CheckID] = append(out[r.CheckID], r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the checks' latest results: %w", err)
	}
	return out, nil
}

// pingDevices is a common table expression, devices(id), of the devices
// that have pings. It finds each in pings_device_ts, one after the other,
// so a query can read each device's pings since a time from the index,
// rather than every ping in the table.
const pingDevices = `devices(id) AS (
	SELECT (SELECT MIN(device_id) FROM pings)
	UNION ALL
	SELECT (SELECT MIN(device_id) FROM pings WHERE device_id > devices.id) FROM devices WHERE devices.id IS NOT NULL
)`

// RecentPings returns the latest n pings of each device since a time, oldest
// first, by device ID.
func (s *Store) RecentPings(ctx context.Context, since time.Time, n int) (map[string][]Ping, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH RECURSIVE `+pingDevices+`
		SELECT device_id, ts, rtt_ms FROM (
			SELECT p.*, ROW_NUMBER() OVER (PARTITION BY p.device_id ORDER BY p.ts DESC) AS nth
			FROM devices JOIN pings p ON p.device_id = devices.id AND p.ts >= ?
		) WHERE nth <= ? ORDER BY device_id, ts`,
		since.UnixMilli(), n)
	if err != nil {
		return nil, fmt.Errorf("read the latest pings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]Ping{}
	for rows.Next() {
		var (
			p   Ping
			at  int64
			rtt sql.NullFloat64
		)
		if err := rows.Scan(&p.DeviceID, &at, &rtt); err != nil {
			return nil, fmt.Errorf("read the latest pings: %w", err)
		}
		p.At, p.Replied, p.RTT = time.UnixMilli(at), rtt.Valid, fromMS(rtt)
		out[p.DeviceID] = append(out[p.DeviceID], p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the latest pings: %w", err)
	}
	return out, nil
}

// A Tally counts what happened in one span of time: how many times a check
// ran and passed, or how many times a device was pinged and replied.
type Tally struct {
	// ID is the check's ID, or the device's.
	ID string
	// Start is when the span starts.
	Start     time.Time
	Total, OK int
}

// Tallies counts the results of every check, and the pings of every device,
// since a time, in spans of the given size. Each span starts at a multiple
// of size since the Unix epoch. It reads every stored ping, so it's for
// starting up, not for every minute.
func (s *Store) Tallies(ctx context.Context, since time.Time, size time.Duration) (checks, pings []Tally, err error) {
	step := size.Milliseconds()
	if step <= 0 {
		return nil, nil, fmt.Errorf("span %v is under a millisecond", size)
	}
	checks, err = readTallies(ctx, s.db, `
		SELECT check_id, ts / ?1 * ?1 AS span, COUNT(*), SUM(ok)
		FROM check_results WHERE ts >= ?2 GROUP BY check_id, span`, step, since.UnixMilli())
	if err != nil {
		return nil, nil, fmt.Errorf("count the checks' results: %w", err)
	}
	pings, err = readTallies(ctx, s.db, `
		WITH RECURSIVE `+pingDevices+`
		SELECT p.device_id, p.ts / ?1 * ?1 AS span, COUNT(*), COUNT(p.rtt_ms)
		FROM devices JOIN pings p ON p.device_id = devices.id AND p.ts >= ?2
		GROUP BY p.device_id, span`, step, since.UnixMilli())
	if err != nil {
		return nil, nil, fmt.Errorf("count the pings: %w", err)
	}
	return checks, pings, nil
}

func readTallies(ctx context.Context, q querier, query string, args ...any) ([]Tally, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Tally
	for rows.Next() {
		var (
			t     Tally
			start int64
		)
		if err := rows.Scan(&t.ID, &start, &t.Total, &t.OK); err != nil {
			return nil, err
		}
		t.Start = time.UnixMilli(start)
		out = append(out, t)
	}
	return out, rows.Err()
}

// A CheckBucket sums up a check's results in one span of time.
type CheckBucket struct {
	// Start is when the span starts.
	Start time.Time
	// Runs is how many times the check ran, and Passed how many of those
	// passed.
	Runs, Passed int
	// Latency is the average time the target took to answer, or 0 if it
	// never did.
	Latency time.Duration
}

// CheckHistory sums up a check's results since a time, in buckets of the
// given size, oldest first. Buckets with no results are left out. Each
// bucket starts at a multiple of size since the Unix epoch.
func (s *Store) CheckHistory(ctx context.Context, checkID string, since time.Time, size time.Duration) ([]CheckBucket, error) {
	step := size.Milliseconds()
	if step <= 0 {
		return nil, fmt.Errorf("bucket size %v is under a millisecond", size)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts / ?1 * ?1 AS bucket, COUNT(*), SUM(ok), AVG(latency_ms)
		FROM check_results WHERE check_id = ?2 AND ts >= ?3
		GROUP BY bucket ORDER BY bucket`,
		step, checkID, since.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read a check's history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []CheckBucket
	for rows.Next() {
		var (
			start   int64
			b       CheckBucket
			latency sql.NullFloat64
		)
		if err := rows.Scan(&start, &b.Runs, &b.Passed, &latency); err != nil {
			return nil, fmt.Errorf("read a check's history: %w", err)
		}
		b.Start, b.Latency = time.UnixMilli(start), fromMS(latency)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read a check's history: %w", err)
	}
	return out, nil
}

// A HealthSample is the health score at one moment.
type HealthSample struct {
	At time.Time
	// Score is the percentage of the live checks that passed.
	Score float64
	// Passing and Failing count the live checks, and Unavailable the
	// others, such as paused checks.
	Passing, Failing, Unavailable int
}

// AddHealth records the health score at a moment, replacing any recorded
// at the same millisecond.
func (s *Store) AddHealth(ctx context.Context, h HealthSample) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO health (ts, score, passing, failing, unavailable) VALUES (?, ?, ?, ?, ?)`,
		h.At.UnixMilli(), h.Score, h.Passing, h.Failing, h.Unavailable)
	if err != nil {
		return fmt.Errorf("record the health score: %w", err)
	}
	return nil
}

// A HealthBucket averages the health score over one span of time.
type HealthBucket struct {
	Start time.Time
	Score float64
}

// HealthHistory averages the health score since a time, in buckets of the
// given size, oldest first. Buckets with no samples are left out. Each
// bucket starts at a multiple of size since the Unix epoch.
func (s *Store) HealthHistory(ctx context.Context, since time.Time, size time.Duration) ([]HealthBucket, error) {
	step := size.Milliseconds()
	if step <= 0 {
		return nil, fmt.Errorf("bucket size %v is under a millisecond", size)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts / ?1 * ?1 AS bucket, AVG(score) FROM health WHERE ts >= ?2
		GROUP BY bucket ORDER BY bucket`,
		step, since.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read the health history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []HealthBucket
	for rows.Next() {
		var (
			start int64
			b     HealthBucket
		)
		if err := rows.Scan(&start, &b.Score); err != nil {
			return nil, fmt.Errorf("read the health history: %w", err)
		}
		b.Start = time.UnixMilli(start)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the health history: %w", err)
	}
	return out, nil
}

// fromMS returns a number of milliseconds as a duration, or 0 if it's NULL.
func fromMS(v sql.NullFloat64) time.Duration {
	if !v.Valid {
		return 0
	}
	return time.Duration(v.Float64 * float64(time.Millisecond))
}

package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

// How metrics and round-trip times are kept: the raw samples for RawKeep, in
// buckets of FineStep for FineKeep, and in buckets of CoarseStep for as long
// as an admin chose to keep history, up to 90 days.
const (
	// RawKeep is how long a device's raw samples last, unless history is kept
	// for less.
	RawKeep = 48 * time.Hour
	// FineStep and CoarseStep are the lengths of the buckets.
	FineStep   = 5 * time.Minute
	CoarseStep = time.Hour
	// FineKeep is how long the buckets of FineStep last, unless history is
	// kept for less.
	FineKeep = 14 * 24 * time.Hour
	// DetailKeep is how long history that isn't summed up, what used a
	// machine's CPU and what its containers used, lasts at most.
	DetailKeep = 7 * 24 * time.Hour

	// rollupEvery is how often the store folds samples into buckets, as
	// maintain does.
	rollupEvery = 5 * time.Minute
	// rollupGrace is how long after a bucket's end the store waits for the
	// last samples of it to be written.
	rollupGrace = time.Minute
	// rollupWindow is the most time one statement folds, so a backlog doesn't
	// hold the database for long.
	rollupWindow = 6 * time.Hour
)

// A series is a kind of history the store sums up.
type series struct {
	// name is what its watermarks in the kv table are called.
	name string
	// raw and rollup are its tables.
	raw, rollup string
	// fine folds samples with ?1 <= ts < ?2 into buckets of FineStep, and
	// coarse folds those buckets into buckets of CoarseStep.
	fine, coarse string
}

var (
	fineMS   = strconv.FormatInt(FineStep.Milliseconds(), 10)
	fineS    = strconv.FormatInt(int64(FineStep/time.Second), 10)
	coarseMS = strconv.FormatInt(CoarseStep.Milliseconds(), 10)
	coarseS  = strconv.FormatInt(int64(CoarseStep/time.Second), 10)
)

// weighted averages column of a rollup, over the samples that had a value.
func weighted(col string) string {
	return "SUM(" + col + " * n) / NULLIF(SUM(CASE WHEN " + col + " IS NOT NULL THEN n END), 0)"
}

var metricsSeries = series{
	name: "metrics", raw: "metrics", rollup: "metrics_rollup",
	fine: `INSERT OR REPLACE INTO metrics_rollup (device_id, step, ts, n, cpu, mem, disk, load1, net_rx, net_tx, temp, battery)
		SELECT device_id, ` + fineS + `, ts / ` + fineMS + ` * ` + fineMS + `, COUNT(*),
			AVG(cpu), AVG(mem), AVG(disk), AVG(load1), AVG(net_rx), AVG(net_tx), AVG(temp), AVG(battery)
		FROM metrics WHERE ts >= ?1 AND ts < ?2 GROUP BY device_id, ts / ` + fineMS,
	coarse: `INSERT OR REPLACE INTO metrics_rollup (device_id, step, ts, n, cpu, mem, disk, load1, net_rx, net_tx, temp, battery)
		SELECT device_id, ` + coarseS + `, ts / ` + coarseMS + ` * ` + coarseMS + `, SUM(n),
			` + weighted("cpu") + `, ` + weighted("mem") + `, ` + weighted("disk") + `, ` + weighted("load1") + `,
			` + weighted("net_rx") + `, ` + weighted("net_tx") + `, ` + weighted("temp") + `, ` + weighted("battery") + `
		FROM metrics_rollup WHERE step = ` + fineS + ` AND ts >= ?1 AND ts < ?2 GROUP BY device_id, ts / ` + coarseMS,
}

var pingsSeries = series{
	name: "pings", raw: "pings", rollup: "pings_rollup",
	fine: `INSERT OR REPLACE INTO pings_rollup (device_id, step, ts, pings, replies, rtt_ms)
		SELECT device_id, ` + fineS + `, ts / ` + fineMS + ` * ` + fineMS + `, COUNT(*), COUNT(rtt_ms), AVG(rtt_ms)
		FROM pings WHERE ts >= ?1 AND ts < ?2 GROUP BY device_id, ts / ` + fineMS,
	coarse: `INSERT OR REPLACE INTO pings_rollup (device_id, step, ts, pings, replies, rtt_ms)
		SELECT device_id, ` + coarseS + `, ts / ` + coarseMS + ` * ` + coarseMS + `, SUM(pings), SUM(replies),
			SUM(rtt_ms * replies) / NULLIF(SUM(replies), 0)
		FROM pings_rollup WHERE step = ` + fineS + ` AND ts >= ?1 AND ts < ?2 GROUP BY device_id, ts / ` + coarseMS,
}

var allSeries = []series{metricsSeries, pingsSeries}

// watermarkKey is the kv key that holds the time up to which a series has been
// folded into buckets of a step, in Unix milliseconds.
func (sr series) watermarkKey(step time.Duration) string {
	return "rollup:" + sr.name + ":" + strconv.FormatInt(int64(step/time.Second), 10)
}

// watermark returns the time up to which a series has been folded into
// buckets of a step, and whether it has been at all.
func (s *Store) watermark(ctx context.Context, sr series, step time.Duration) (time.Time, bool, error) {
	v, ok, err := s.KV(ctx, sr.watermarkKey(step))
	if err != nil || !ok {
		return time.Time{}, false, err
	}
	ms, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		// As if it were never set: folding again is harmless.
		return time.Time{}, false, nil //nolint:nilerr // a watermark that can't be read is as good as none.
	}
	return time.UnixMilli(ms), true, nil
}

// RollUp folds the raw samples and buckets of metrics and round-trip times
// that are over by now into buckets: those of FineStep from the samples, and
// those of CoarseStep from them. It picks up where it left off, and folds
// everything the first time. The store does it every few minutes, as long as
// it prunes.
func (s *Store) RollUp(ctx context.Context, now time.Time) error {
	for _, sr := range allSeries {
		if err := s.rollUpSeries(ctx, sr, now); err != nil {
			return fmt.Errorf("sum up the %s history: %w", sr.name, err)
		}
	}
	return nil
}

func (s *Store) rollUpSeries(ctx context.Context, sr series, now time.Time) error {
	over := now.Add(-rollupGrace)
	if err := s.fold(ctx, sr, sr.fine, FineStep, "SELECT MIN(ts) FROM "+sr.raw, over); err != nil {
		return err
	}
	// The hourly buckets come from the finer ones, which are complete up to
	// the watermark of those.
	fineDone, ok, err := s.watermark(ctx, sr, FineStep)
	if err != nil || !ok {
		return err
	}
	return s.fold(ctx, sr, sr.coarse, CoarseStep,
		"SELECT MIN(ts) FROM "+sr.rollup+" WHERE step = "+fineS, minTime(over, fineDone))
}

// fold folds with query the span of time from the series' watermark for step
// up to the last multiple of step before end, a window at a time, and moves
// the watermark. oldest is a query for the first time there is anything to
// fold, if it has never run.
func (s *Store) fold(ctx context.Context, sr series, query string, step time.Duration, oldest string, end time.Time) error {
	to := end.Truncate(step)
	from, ok, err := s.watermark(ctx, sr, step)
	if err != nil {
		return err
	}
	if !ok {
		var first sql.NullInt64
		if err := s.db.QueryRowContext(ctx, oldest).Scan(&first); err != nil {
			return err
		}
		if !first.Valid {
			return nil
		}
		from = time.UnixMilli(first.Int64).Truncate(step)
	}
	for from.Before(to) {
		next := minTime(from.Add(rollupWindow).Truncate(step), to)
		if !next.After(from) {
			next = to
		}
		if _, err := s.db.ExecContext(ctx, query, from.UnixMilli(), next.UnixMilli()); err != nil {
			return err
		}
		if err := s.SetKV(ctx, sr.watermarkKey(step), strconv.FormatInt(next.UnixMilli(), 10)); err != nil {
			return err
		}
		from = next
	}
	return nil
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// A MetricsRollup is a device's metrics over one bucket, which Bield makes by
// folding raw samples, or a demo makes up. A value is nil if no sample had it.
type MetricsRollup struct {
	// DeviceID is as in MetricsRow.
	DeviceID string
	// Step is the bucket's length: FineStep or CoarseStep.
	Step time.Duration
	// At is when the bucket starts, a multiple of Step.
	At time.Time
	// N is how many samples it sums up.
	N int
	// The values are the averages of the samples that had them.
	CPU, Mem, Disk, Load1, NetRx, NetTx, Temp, Battery *float64
}

// AddMetricsRollups records buckets of metrics, replacing the ones for the same
// device, step and time. It is for starting a new database with history. It
// moves no watermark, so Bield folds the raw samples that came later.
func (s *Store) AddMetricsRollups(ctx context.Context, rows []MetricsRollup) error {
	if len(rows) == 0 {
		return nil
	}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO metrics_rollup (device_id, step, ts, n, cpu, mem, disk, load1, net_rx, net_tx, temp, battery)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, r := range rows {
			_, err := stmt.ExecContext(ctx, r.DeviceID, int64(r.Step/time.Second), r.At.UnixMilli(), r.N,
				nullFloat(r.CPU), nullFloat(r.Mem), nullFloat(r.Disk), nullFloat(r.Load1),
				nullFloat(r.NetRx), nullFloat(r.NetTx), nullFloat(r.Temp), nullFloat(r.Battery))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record metrics buckets: %w", err)
	}
	return nil
}

// A PingRollup is the pings of a device over one bucket.
type PingRollup struct {
	DeviceID string
	// Step and At are as in MetricsRollup.
	Step time.Duration
	At   time.Time
	// Pings is how many were sent, and Replies how many got a reply.
	Pings, Replies int
	// RTT is the average round trip of the replies, if there were any.
	RTT time.Duration
}

// AddPingRollups records buckets of pings, as AddMetricsRollups does for
// metrics.
func (s *Store) AddPingRollups(ctx context.Context, rows []PingRollup) error {
	if len(rows) == 0 {
		return nil
	}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT OR REPLACE INTO pings_rollup (device_id, step, ts, pings, replies, rtt_ms)
			VALUES (?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, r := range rows {
			rtt := sql.NullFloat64{Float64: ms(r.RTT), Valid: r.Replies > 0}
			if _, err := stmt.ExecContext(ctx, r.DeviceID, int64(r.Step/time.Second), r.At.UnixMilli(), r.Pings, r.Replies, rtt); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record ping buckets: %w", err)
	}
	return nil
}

// A span says where to read a history from: the buckets of a step until cut,
// and then the raw samples.
type span struct {
	// step is the length of the buckets to read the part before cut from, or
	// 0 if there are none to read.
	step time.Duration
	// cut is where the buckets end and the raw samples begin. It's a multiple
	// of the size of the history's buckets, so they don't share one.
	cut time.Time
}

// spanFor works out where to read a history of a device's series that goes back
// to since, in buckets of size. The buckets are those of the longest step that
// fits in size, and they're read as far as the device has them, which is
// nearly to now: a raw sample is only read for what isn't in a bucket yet. A
// size that no step fits in gets the raw samples alone, which only go back
// RawKeep.
func (s *Store) spanFor(ctx context.Context, sr series, deviceID string, since time.Time, size time.Duration) span {
	sp := span{cut: since}
	var step time.Duration
	switch {
	case size >= CoarseStep && size%CoarseStep == 0:
		step = CoarseStep
	case size >= FineStep && size%FineStep == 0:
		step = FineStep
	default:
		return sp
	}
	var last sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT MAX(ts) FROM "+sr.rollup+" WHERE device_id = ? AND step = ?",
		deviceID, int64(step/time.Second)).Scan(&last)
	if err != nil || !last.Valid {
		return sp
	}
	// The raw samples fill the first bucket of the size that the device's
	// buckets don't wholly make. If they've been deleted, the buckets make
	// what they can of it.
	end := time.UnixMilli(last.Int64).Add(step)
	cut := end.Truncate(size)
	if cut.Before(time.Now().Add(-RawKeep)) && cut.Before(end) {
		cut = cut.Add(size)
	}
	if cut.After(since) {
		sp.step, sp.cut = step, cut
	}
	return sp
}

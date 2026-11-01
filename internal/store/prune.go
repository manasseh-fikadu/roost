package store

import (
	"context"
	"time"
)

// pruneInterval is how often the store deletes expired history.
const pruneInterval = time.Hour

// Retention is how long the store keeps each kind of history.
type Retention struct {
	// Activity is how long it keeps the activity log, and the alerts that
	// resolved, with their notes.
	Activity time.Duration
	// History is how long it keeps what devices used and the round-trip times
	// to them, in buckets of an hour, which can be up to 90 days. The raw
	// samples of those last 48 hours, and their buckets of 5 minutes 14 days, if
	// History is longer. What used a machine's CPU, and what its containers
	// used, aren't summed up, so they last 7 days at most.
	History time.Duration
	// Checks is how long it keeps the results of checks, and the health
	// score.
	Checks time.Duration
}

// DefaultRetention returns how long the store keeps history unless an admin
// says otherwise.
func DefaultRetention() Retention {
	return Retention{Activity: 30 * 24 * time.Hour, History: 7 * 24 * time.Hour, Checks: 7 * 24 * time.Hour}
}

// valid reports whether every span is above zero, which a bug or a bad
// setting could leave out, and which would delete everything.
func (r Retention) valid() bool { return r.Activity > 0 && r.History > 0 && r.Checks > 0 }

func activity(r Retention) time.Duration { return r.Activity }
func history(r Retention) time.Duration  { return r.History }
func checks(r Retention) time.Duration   { return r.Checks }

// raw, fine and detail are how long History lets the raw samples, the buckets
// of FineStep and the history that isn't summed up last.
func raw(r Retention) time.Duration    { return min(r.History, RawKeep) }
func fine(r Retention) time.Duration   { return min(r.History, FineKeep) }
func detail(r Retention) time.Duration { return min(r.History, DetailKeep) }

// fixed returns a keep function for rows that are kept for d whatever the
// retention is.
func fixed(d time.Duration) func(Retention) time.Duration {
	return func(Retention) time.Duration { return d }
}

// retention lists the tables that keep history and how long their rows last. A
// migration that adds such a table adds it here too. Each query deletes the rows
// older than its one parameter, a time in Unix milliseconds, or the parts of
// them that aren't kept that long.
var retention = []struct {
	table string
	keep  func(Retention) time.Duration
	query string
	// rolled, if it's set, is the series whose raw samples these are. They
	// aren't deleted until they're folded into buckets.
	rolled *series
}{
	{"events", activity, "DELETE FROM events WHERE ts < ?", nil},
	// Metrics and round-trip times are kept in full for 48 hours, and then in
	// buckets, which have to be made first. What used a machine's CPU and
	// what its containers used aren't summed up, so they go back a week at
	// most.
	{"pings", raw, "DELETE FROM pings WHERE ts < ?", &pingsSeries},
	{"metrics", raw, "DELETE FROM metrics WHERE ts < ?", &metricsSeries},
	{"pings_rollup", fine, "DELETE FROM pings_rollup WHERE step = " + fineS + " AND ts < ?", nil},
	{"metrics_rollup", fine, "DELETE FROM metrics_rollup WHERE step = " + fineS + " AND ts < ?", nil},
	{"pings_rollup", history, "DELETE FROM pings_rollup WHERE step = " + coarseS + " AND ts < ?", nil},
	{"metrics_rollup", history, "DELETE FROM metrics_rollup WHERE step = " + coarseS + " AND ts < ?", nil},
	{"usage_samples", detail, "DELETE FROM usage_samples WHERE ts < ?", nil},
	{"container_metrics", detail, "DELETE FROM container_metrics WHERE ts < ?", nil},
	// After a day, one minute in ten keeps the processes in detail. Every
	// minute keeps its biggest groups, for history.
	{"usage_samples", fixed(24 * time.Hour),
		"UPDATE usage_samples SET detail = NULL WHERE ts < ? AND detail IS NOT NULL AND ts / 60000 % 10 != 0", nil},
	// The checks' results and the health score are kept alike.
	{"check_results", checks, "DELETE FROM check_results WHERE ts < ?", nil},
	{"health", checks, "DELETE FROM health WHERE ts < ?", nil},
	// A resolved alert, and its notes, are kept like the activity log.
	{"alerts", activity, "DELETE FROM alerts WHERE resolved_at < ?", nil},
	// An enrollment code goes once it expires.
	{"enrollment_codes", fixed(0), "DELETE FROM enrollment_codes WHERE expires_at < ?", nil},
}

// maintain sums up history into buckets now and then every rollupEvery, and
// deletes expired rows now and then every pruneInterval, until ctx ends. It
// sums up before it deletes, so nothing goes before it's in a bucket.
func (s *Store) maintain(ctx context.Context) {
	rollups := time.NewTicker(rollupEvery)
	defer rollups.Stop()
	prunes := time.NewTicker(pruneInterval)
	defer prunes.Stop()
	pruneDue := true
	for {
		if err := s.RollUp(ctx, time.Now()); err != nil && ctx.Err() == nil {
			s.log.Warn("sum up history", "error", err)
		}
		if pruneDue {
			s.prune(ctx, time.Now())
			pruneDue = false
		}
		select {
		case <-ctx.Done():
			return
		case <-rollups.C:
		case <-prunes.C:
			pruneDue = true
		}
	}
}

// retentionNow returns how long to keep history now, and whether the store
// should prune at all: not if it can't tell what an admin chose.
func (s *Store) retentionNow(ctx context.Context) (Retention, bool) {
	if s.retention == nil {
		return DefaultRetention(), true
	}
	keep, err := s.retention(ctx, s)
	switch {
	case err != nil && ctx.Err() == nil && s.retentionErr == nil:
		s.log.Warn("read how long to keep history, so keeping all of it", "error", err)
	case err == nil && s.retentionErr != nil:
		s.log.Info("read how long to keep history again")
	}
	s.retentionErr = err
	if err == nil && !keep.valid() {
		s.log.Warn("how long to keep history isn't above zero, so keeping all of it", "retention", keep)
		return Retention{}, false
	}
	return keep, err == nil
}

func (s *Store) prune(ctx context.Context, now time.Time) {
	keep, ok := s.retentionNow(ctx)
	if !ok {
		return
	}
	for _, r := range retention {
		before := now.Add(-r.keep(keep))
		if r.rolled != nil {
			// Raw samples go once they're in buckets, which Bield has made up
			// to the watermark.
			done, ok, err := s.watermark(ctx, *r.rolled, FineStep)
			if err != nil || !ok {
				continue
			}
			before = minTime(before, done)
		}
		res, err := s.db.ExecContext(ctx, r.query, before.UnixMilli())
		if err != nil {
			if ctx.Err() == nil {
				s.log.Warn("prune expired rows", "table", r.table, "error", err)
			}
			continue
		}
		if n, err := res.RowsAffected(); err == nil && n > 0 {
			s.log.Debug("pruned expired rows", "table", r.table, "rows", n)
		}
	}
}

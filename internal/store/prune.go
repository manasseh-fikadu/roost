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
	// History is how long it keeps what devices and containers used, and
	// the round-trip times to devices.
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
}{
	{"events", activity, "DELETE FROM events WHERE ts < ?"},
	// Round-trip history, metrics and usage go back a week unless an admin
	// says otherwise. Rollups (#17) can keep a summary for longer.
	{"pings", history, "DELETE FROM pings WHERE ts < ?"},
	{"metrics", history, "DELETE FROM metrics WHERE ts < ?"},
	{"usage_samples", history, "DELETE FROM usage_samples WHERE ts < ?"},
	// So do the containers' metrics.
	{"container_metrics", history, "DELETE FROM container_metrics WHERE ts < ?"},
	// After a day, one minute in ten keeps the processes in detail. Every
	// minute keeps its biggest groups, for history.
	{"usage_samples", fixed(24 * time.Hour),
		"UPDATE usage_samples SET detail = NULL WHERE ts < ? AND detail IS NOT NULL AND ts / 60000 % 10 != 0"},
	// The checks' results and the health score are kept alike.
	{"check_results", checks, "DELETE FROM check_results WHERE ts < ?"},
	{"health", checks, "DELETE FROM health WHERE ts < ?"},
	// A resolved alert, and its notes, are kept like the activity log.
	{"alerts", activity, "DELETE FROM alerts WHERE resolved_at < ?"},
	// An enrollment code goes once it expires.
	{"enrollment_codes", fixed(0), "DELETE FROM enrollment_codes WHERE expires_at < ?"},
}

// pruneHourly deletes expired rows now and then once every pruneInterval, until
// ctx ends.
func (s *Store) pruneHourly(ctx context.Context) {
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()
	for {
		s.prune(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
		res, err := s.db.ExecContext(ctx, r.query, now.Add(-r.keep(keep)).UnixMilli())
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

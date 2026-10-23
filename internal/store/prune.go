package store

import (
	"context"
	"time"
)

// pruneInterval is how often the store deletes expired history.
const pruneInterval = time.Hour

// retention lists the tables that keep history and how long their rows last. A
// migration that adds such a table adds it here too. Each query deletes the rows
// older than its one parameter, a time in Unix milliseconds, or the parts of
// them that aren't kept that long.
var retention = []struct {
	table string
	keep  time.Duration
	query string
}{
	// The activity log is small, so it's kept for a month.
	{"events", 30 * 24 * time.Hour, "DELETE FROM events WHERE ts < ?"},
	// Round-trip history goes back a week, as far as the device page shows.
	{"pings", 7 * 24 * time.Hour, "DELETE FROM pings WHERE ts < ?"},
	// Metrics and usage go back a week too. Rollups (#17) can keep a summary
	// for longer.
	{"metrics", 7 * 24 * time.Hour, "DELETE FROM metrics WHERE ts < ?"},
	{"usage_samples", 7 * 24 * time.Hour, "DELETE FROM usage_samples WHERE ts < ?"},
	// After a day, one minute in ten keeps the processes in detail. Every
	// minute keeps its biggest groups, for history.
	{"usage_samples", 24 * time.Hour,
		"UPDATE usage_samples SET detail = NULL WHERE ts < ? AND detail IS NOT NULL AND ts / 60000 % 10 != 0"},
	// An enrollment code goes once it expires.
	{"enrollment_codes", 0, "DELETE FROM enrollment_codes WHERE expires_at < ?"},
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

func (s *Store) prune(ctx context.Context, now time.Time) {
	for _, r := range retention {
		res, err := s.db.ExecContext(ctx, r.query, now.Add(-r.keep).UnixMilli())
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

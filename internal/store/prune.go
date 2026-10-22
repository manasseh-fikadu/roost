package store

import (
	"context"
	"time"
)

// pruneInterval is how often the store deletes expired history.
const pruneInterval = time.Hour

// retention lists the tables that keep history and how long their rows last. A
// migration that adds such a table adds it here too. Each query deletes the rows
// older than its one parameter, a time in Unix milliseconds.
var retention = []struct {
	table string
	keep  time.Duration
	query string
}{
	// The activity log is small, so it's kept for a month.
	{"events", 30 * 24 * time.Hour, "DELETE FROM events WHERE ts < ?"},
	// Round-trip history goes back a week, as far as the device page shows.
	{"pings", 7 * 24 * time.Hour, "DELETE FROM pings WHERE ts < ?"},
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

package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// HubID is the device ID that the history of the machine Roost runs on is
// kept under. That machine can be Roost's own node, or a peer in embedded
// mode, and the node can change, but its history stays in one place.
const HubID = "hub"

// A MetricsRow is what a device was doing at one moment. A value is nil if
// the device didn't report it.
type MetricsRow struct {
	DeviceID string
	At       time.Time
	// CPU is the percentage of time the CPUs were busy.
	CPU *float64
	// Mem and Disk are the percentages of memory and of / that were used.
	Mem, Disk *float64
	// Load1 is the 1-minute load average.
	Load1 *float64
	// NetRx and NetTx are bytes per second received and sent.
	NetRx, NetTx *float64
	// Temp is in degrees Celsius.
	Temp *float64
	// Battery is how charged the battery was, in percent.
	Battery *float64
}

// AddMetrics records rows for their devices' history.
func (s *Store) AddMetrics(ctx context.Context, rows []MetricsRow) error {
	if len(rows) == 0 {
		return nil
	}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO metrics (ts, device_id, cpu, mem, disk, load1, net_rx, net_tx, temp, battery)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, r := range rows {
			_, err := stmt.ExecContext(ctx, r.At.UnixMilli(), r.DeviceID,
				nullFloat(r.CPU), nullFloat(r.Mem), nullFloat(r.Disk), nullFloat(r.Load1),
				nullFloat(r.NetRx), nullFloat(r.NetTx), nullFloat(r.Temp), nullFloat(r.Battery))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record metrics: %w", err)
	}
	return nil
}

// A MetricsBucket averages a device's metrics over one span of time. A value
// is nil if no row in the span had it.
type MetricsBucket struct {
	// Start is when the span starts.
	Start time.Time
	// The values are as in MetricsRow.
	CPU, Mem, Disk, NetRx, NetTx *float64
}

// MetricsHistory averages a device's metrics since a time, in buckets of the
// given size, oldest first. Buckets with no rows are left out. Each bucket
// starts at a multiple of size since the Unix epoch.
//
// Raw samples only go back RawKeep. What's older comes from the buckets that
// Roost folds them into, if size is a multiple of their length: five minutes,
// or an hour.
func (s *Store) MetricsHistory(ctx context.Context, deviceID string, since time.Time, size time.Duration) ([]MetricsBucket, error) {
	if size.Milliseconds() <= 0 {
		return nil, fmt.Errorf("bucket size %v is under a millisecond", size)
	}
	sp := spanFor(time.Now(), since, size)
	var out []MetricsBucket
	if sp.step > 0 && since.Before(sp.cut) {
		var err error
		out, err = s.metricsBuckets(ctx, `
			SELECT ts / ?1 * ?1 AS bucket, `+weighted("cpu")+`, `+weighted("mem")+`, `+weighted("disk")+`,
				`+weighted("net_rx")+`, `+weighted("net_tx")+`
			FROM metrics_rollup WHERE device_id = ?2 AND step = ?3 AND ts >= ?4 AND ts < ?5
			GROUP BY bucket ORDER BY bucket`,
			size.Milliseconds(), deviceID, int64(sp.step/time.Second), since.Truncate(sp.step).UnixMilli(), sp.cut.UnixMilli())
		if err != nil {
			return nil, err
		}
	}
	raw, err := s.metricsBuckets(ctx, `
		SELECT ts / ?1 * ?1 AS bucket, AVG(cpu), AVG(mem), AVG(disk), AVG(net_rx), AVG(net_tx)
		FROM metrics WHERE device_id = ?2 AND ts >= ?3
		GROUP BY bucket ORDER BY bucket`,
		size.Milliseconds(), deviceID, sp.cut.UnixMilli())
	if err != nil {
		return nil, err
	}
	return append(out, raw...), nil
}

// metricsBuckets runs a query for buckets: each has its start and the five
// values of a MetricsBucket.
func (s *Store) metricsBuckets(ctx context.Context, query string, args ...any) ([]MetricsBucket, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read the metrics history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []MetricsBucket
	for rows.Next() {
		var (
			start                        int64
			cpu, mem, disk, netRx, netTx sql.NullFloat64
		)
		if err := rows.Scan(&start, &cpu, &mem, &disk, &netRx, &netTx); err != nil {
			return nil, fmt.Errorf("read the metrics history: %w", err)
		}
		out = append(out, MetricsBucket{
			Start: time.UnixMilli(start),
			CPU:   floatPtr(cpu), Mem: floatPtr(mem), Disk: floatPtr(disk),
			NetRx: floatPtr(netRx), NetTx: floatPtr(netTx),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the metrics history: %w", err)
	}
	return out, nil
}

func nullFloat(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

func floatPtr(f sql.NullFloat64) *float64 {
	if !f.Valid {
		return nil
	}
	return &f.Float64
}

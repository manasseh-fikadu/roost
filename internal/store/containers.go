package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// A ContainerMetricsRow is what a container used over a minute. A value is nil
// if Docker didn't say it.
type ContainerMetricsRow struct {
	// DeviceID is as in MetricsRow, and Name is the container's name.
	DeviceID, Name string
	At             time.Time
	// CPU is the share of one CPU it used, in percent, and Mem the bytes of
	// memory it used.
	CPU, Mem *float64
	// NetRx and NetTx are the bytes per second it received and sent, and
	// BlkRead and BlkWrite the bytes per second it read from and wrote to
	// disks.
	NetRx, NetTx, BlkRead, BlkWrite *float64
}

// AddContainerMetrics records rows for their containers' history.
func (s *Store) AddContainerMetrics(ctx context.Context, rows []ContainerMetricsRow) error {
	if len(rows) == 0 {
		return nil
	}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO container_metrics (ts, device_id, name, cpu, mem, net_rx, net_tx, blk_read, blk_write)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, r := range rows {
			_, err := stmt.ExecContext(ctx, r.At.UnixMilli(), r.DeviceID, r.Name,
				nullFloat(r.CPU), nullFloat(r.Mem), nullFloat(r.NetRx), nullFloat(r.NetTx),
				nullFloat(r.BlkRead), nullFloat(r.BlkWrite))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record container metrics: %w", err)
	}
	return nil
}

// A ContainerBucket averages what a container used over one span of time. A
// value is nil if no row in the span had it.
type ContainerBucket struct {
	// Start is when the span starts.
	Start time.Time
	// The values are as in ContainerMetricsRow.
	CPU, Mem, NetRx, NetTx, BlkRead, BlkWrite *float64
}

// ContainerHistory averages what the container with a name on a device used
// since a time, in buckets of the given size, oldest first. Buckets with no
// rows are left out. Each bucket starts at a multiple of size since the Unix
// epoch.
func (s *Store) ContainerHistory(ctx context.Context, deviceID, name string, since time.Time, size time.Duration) ([]ContainerBucket, error) {
	step := size.Milliseconds()
	if step <= 0 {
		return nil, fmt.Errorf("bucket size %v is under a millisecond", size)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts / ?1 * ?1 AS bucket, AVG(cpu), AVG(mem), AVG(net_rx), AVG(net_tx), AVG(blk_read), AVG(blk_write)
		FROM container_metrics WHERE device_id = ?2 AND name = ?3 AND ts >= ?4
		GROUP BY bucket ORDER BY bucket`,
		step, deviceID, name, since.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read the container history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ContainerBucket
	for rows.Next() {
		var (
			start                                     int64
			cpu, mem, netRx, netTx, blkRead, blkWrite sql.NullFloat64
		)
		if err := rows.Scan(&start, &cpu, &mem, &netRx, &netTx, &blkRead, &blkWrite); err != nil {
			return nil, fmt.Errorf("read the container history: %w", err)
		}
		out = append(out, ContainerBucket{
			Start: time.UnixMilli(start),
			CPU:   floatPtr(cpu), Mem: floatPtr(mem), NetRx: floatPtr(netRx), NetTx: floatPtr(netTx),
			BlkRead: floatPtr(blkRead), BlkWrite: floatPtr(blkWrite),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the container history: %w", err)
	}
	return out, nil
}

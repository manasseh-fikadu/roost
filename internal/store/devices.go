package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DeviceSettings are what admins set for a device. The zero value is the
// defaults.
type DeviceSettings struct {
	// DisplayName is shown instead of the name the device reports, unless
	// it's "".
	DisplayName string
	// Kind is what sort of machine the device is, such as "laptop", or "" to
	// let Roost guess.
	Kind string
	// AlertOffline says whether to alert when the device goes offline, or is
	// nil for the default.
	AlertOffline *bool
	// Thresholds replace the settings' for this device.
	Thresholds ThresholdOverrides
	Notes      string
}

// ThresholdOverrides are the thresholds an admin set for one device. A nil
// one is the settings'.
type ThresholdOverrides struct {
	// DiskWarn, DiskCritical, MemoryWarn and CPUWarn are in percent.
	DiskWarn, DiskCritical, MemoryWarn, CPUWarn *float64
	// OfflineMinutes is how long the device can be offline before an alert.
	OfflineMinutes *int
}

// DeviceSettings returns the settings of every device that has any, by
// device ID.
func (s *Store) DeviceSettings(ctx context.Context) (map[string]DeviceSettings, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT device_id, display_name, kind, alert_offline, notes,
			disk_warn, disk_critical, memory_warn, cpu_warn, offline_minutes FROM device_settings`)
	if err != nil {
		return nil, fmt.Errorf("read device settings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]DeviceSettings{}
	for rows.Next() {
		var (
			id       string
			ds       DeviceSettings
			alert    sql.NullBool
			diskW    sql.NullFloat64
			diskC    sql.NullFloat64
			memory   sql.NullFloat64
			cpu      sql.NullFloat64
			offlineM sql.NullInt64
		)
		if err := rows.Scan(&id, &ds.DisplayName, &ds.Kind, &alert, &ds.Notes, &diskW, &diskC, &memory, &cpu, &offlineM); err != nil {
			return nil, fmt.Errorf("read device settings: %w", err)
		}
		if alert.Valid {
			ds.AlertOffline = &alert.Bool
		}
		ds.Thresholds = ThresholdOverrides{
			DiskWarn: floatPtr(diskW), DiskCritical: floatPtr(diskC), MemoryWarn: floatPtr(memory), CPUWarn: floatPtr(cpu),
		}
		if offlineM.Valid {
			ds.Thresholds.OfflineMinutes = new(int(offlineM.Int64))
		}
		out[id] = ds
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read device settings: %w", err)
	}
	return out, nil
}

// SetDeviceSettings replaces a device's settings.
func (s *Store) SetDeviceSettings(ctx context.Context, deviceID string, ds DeviceSettings) error {
	var err error
	t := ds.Thresholds
	if ds == (DeviceSettings{}) {
		_, err = s.db.ExecContext(ctx, "DELETE FROM device_settings WHERE device_id = ?", deviceID)
	} else {
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO device_settings (device_id, display_name, kind, alert_offline, notes,
				disk_warn, disk_critical, memory_warn, cpu_warn, offline_minutes)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (device_id) DO UPDATE SET display_name = excluded.display_name,
				kind = excluded.kind, alert_offline = excluded.alert_offline, notes = excluded.notes,
				disk_warn = excluded.disk_warn, disk_critical = excluded.disk_critical,
				memory_warn = excluded.memory_warn, cpu_warn = excluded.cpu_warn,
				offline_minutes = excluded.offline_minutes`,
			deviceID, ds.DisplayName, ds.Kind, nullBool(ds.AlertOffline), ds.Notes,
			nullFloat(t.DiskWarn), nullFloat(t.DiskCritical), nullFloat(t.MemoryWarn), nullFloat(t.CPUWarn), nullInt(t.OfflineMinutes))
	}
	if err != nil {
		return fmt.Errorf("save device settings: %w", err)
	}
	return nil
}

func nullBool(b *bool) sql.NullBool {
	if b == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: *b, Valid: true}
}

func nullInt(n *int) sql.NullInt64 {
	if n == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*n), Valid: true}
}

// A Ping is the outcome of pinging a device once.
type Ping struct {
	DeviceID string
	At       time.Time
	// Replied reports whether a reply came back.
	Replied bool
	// RTT is the round trip, if a reply came back.
	RTT time.Duration
}

// AddPings records pings for their devices' history.
func (s *Store) AddPings(ctx context.Context, pings []Ping) error {
	if len(pings) == 0 {
		return nil
	}
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, "INSERT INTO pings (ts, device_id, rtt_ms) VALUES (?, ?, ?)")
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for _, p := range pings {
			rtt := sql.NullFloat64{Float64: ms(p.RTT), Valid: p.Replied}
			if _, err := stmt.ExecContext(ctx, p.At.UnixMilli(), p.DeviceID, rtt); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record pings: %w", err)
	}
	return nil
}

// RTTBucket sums up the pings of a device in one span of time.
type RTTBucket struct {
	// Start is when the span starts.
	Start time.Time
	// Pings is how many pings were sent, and Replies how many got a reply.
	Pings, Replies int
	// RTT is the average round trip of the pings that got a reply.
	RTT time.Duration
}

// RTTHistory sums up the pings of a device since a time, in buckets of the
// given size, oldest first. Buckets with no pings are left out. Each bucket
// starts at a multiple of size since the Unix epoch.
func (s *Store) RTTHistory(ctx context.Context, deviceID string, since time.Time, size time.Duration) ([]RTTBucket, error) {
	step := size.Milliseconds()
	if step <= 0 {
		return nil, fmt.Errorf("bucket size %v is under a millisecond", size)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ts / ?1 * ?1 AS bucket, COUNT(*), COUNT(rtt_ms), COALESCE(AVG(rtt_ms), 0)
		FROM pings WHERE device_id = ?2 AND ts >= ?3
		GROUP BY bucket ORDER BY bucket`,
		step, deviceID, since.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("read the round-trip history: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RTTBucket
	for rows.Next() {
		var (
			start int64
			b     RTTBucket
			avgMS float64
		)
		if err := rows.Scan(&start, &b.Pings, &b.Replies, &avgMS); err != nil {
			return nil, fmt.Errorf("read the round-trip history: %w", err)
		}
		b.Start = time.UnixMilli(start)
		b.RTT = time.Duration(avgMS * float64(time.Millisecond))
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the round-trip history: %w", err)
	}
	return out, nil
}

// ms returns d in milliseconds, with fractions.
func ms(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
)

// benchDevices is how many devices the benchmarks' database has history of.
const benchDevices = 200

// The benchmarks' database holds what Bield has after running for 90 days with
// 200 devices: 48 hours of samples every 30 seconds, buckets of 5 minutes for
// 14 days and of an hour for 90. They share it, and every benchmark that
// writes leaves it as it was or newer.
var (
	benchOnce  sync.Once
	benchStore *store.Store
	benchNow   time.Time
	errBench   error
)

func benchDB(b *testing.B) *store.Store {
	b.Helper()
	benchOnce.Do(func() {
		dir, err := testingTempDir()
		if err != nil {
			errBench = err
			return
		}
		benchNow = time.Now().Truncate(time.Hour)
		benchStore, errBench = store.OpenWith(b.Context(), filepath.Join(dir, "bield.db"), store.Options{NoPrune: true}, store.Migrations())
		if errBench != nil {
			return
		}
		errBench = fillBench(b.Context(), benchStore, benchNow)
	})
	if errBench != nil {
		b.Fatal(errBench)
	}
	return benchStore
}

func fillBench(ctx context.Context, s *store.Store, now time.Time) error {
	const raw = 48 * time.Hour
	var rows []store.MetricsRow
	var pings []store.Ping
	for at := now.Add(-raw); at.Before(now); at = at.Add(30 * time.Second) {
		for i := range benchDevices {
			rows = append(rows, store.MetricsRow{
				DeviceID: fmt.Sprintf("device-%03d", i), At: at, CPU: new(float64(i%60) + 10), Mem: new(50.0), Disk: new(60.0),
				Load1: new(1.0), NetRx: new(1e5), NetTx: new(5e4), Temp: new(45.0),
			})
			if at.Second() == 0 {
				pings = append(pings, store.Ping{DeviceID: fmt.Sprintf("device-%03d", i), At: at, Replied: true, RTT: 12 * time.Millisecond})
			}
		}
		if len(rows) >= 60000 {
			if err := s.AddMetrics(ctx, rows); err != nil {
				return err
			}
			if err := s.AddPings(ctx, pings); err != nil {
				return err
			}
			rows, pings = rows[:0], pings[:0]
		}
	}
	if err := s.AddMetrics(ctx, rows); err != nil {
		return err
	}
	if err := s.AddPings(ctx, pings); err != nil {
		return err
	}
	// Folds the last 48 hours, and everything older is buckets.
	if err := s.RollUp(ctx, now); err != nil {
		return err
	}
	for _, step := range []time.Duration{store.FineStep, store.CoarseStep} {
		keep := 90 * 24 * time.Hour
		if step == store.FineStep {
			keep = store.FineKeep
		}
		var buckets []store.MetricsRollup
		var pingBuckets []store.PingRollup
		for at := now.Add(-keep).Truncate(step); at.Before(now.Add(-raw)); at = at.Add(step) {
			for i := range benchDevices {
				id := fmt.Sprintf("device-%03d", i)
				buckets = append(buckets, store.MetricsRollup{
					DeviceID: id, Step: step, At: at, N: int(step / (30 * time.Second)),
					CPU: new(30.0), Mem: new(50.0), Disk: new(60.0), Load1: new(1.0), NetRx: new(1e5), NetTx: new(5e4), Temp: new(45.0),
				})
				pingBuckets = append(pingBuckets, store.PingRollup{DeviceID: id, Step: step, At: at, Pings: int(step / time.Minute), Replies: int(step / time.Minute), RTT: 12 * time.Millisecond})
			}
			if len(buckets) >= 60000 {
				if err := s.AddMetricsRollups(ctx, buckets); err != nil {
					return err
				}
				if err := s.AddPingRollups(ctx, pingBuckets); err != nil {
					return err
				}
				buckets, pingBuckets = buckets[:0], pingBuckets[:0]
			}
		}
		if err := s.AddMetricsRollups(ctx, buckets); err != nil {
			return err
		}
		if err := s.AddPingRollups(ctx, pingBuckets); err != nil {
			return err
		}
	}
	return nil
}

// BenchmarkMetricsHistory reads one device's history, as the device page does.
func BenchmarkMetricsHistory(b *testing.B) {
	s := benchDB(b)
	for _, r := range []struct {
		name       string
		span, size time.Duration
	}{
		{"1h", time.Hour, time.Minute},
		{"24h", 24 * time.Hour, 5 * time.Minute},
		{"7d", 7 * 24 * time.Hour, time.Hour},
		{"30d", 30 * 24 * time.Hour, 2 * time.Hour},
		{"90d", 90 * 24 * time.Hour, 6 * time.Hour},
	} {
		b.Run(r.name, func(b *testing.B) {
			for b.Loop() {
				if _, err := s.MetricsHistory(b.Context(), "device-042", time.Now().Add(-r.span).Truncate(r.size), r.size); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkRTTHistory is the same for the round-trip times.
func BenchmarkRTTHistory(b *testing.B) {
	s := benchDB(b)
	for _, r := range []struct {
		name       string
		span, size time.Duration
	}{{"24h", 24 * time.Hour, 5 * time.Minute}, {"90d", 90 * 24 * time.Hour, 6 * time.Hour}} {
		b.Run(r.name, func(b *testing.B) {
			for b.Loop() {
				if _, err := s.RTTHistory(b.Context(), "device-042", time.Now().Add(-r.span).Truncate(r.size), r.size); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkAddMetrics writes what the feed writes every 30 seconds: a row for
// each device.
func BenchmarkAddMetrics(b *testing.B) {
	s := benchDB(b)
	batch := make([]store.MetricsRow, benchDevices)
	at := benchNow.Add(24 * time.Hour)
	for b.Loop() {
		at = at.Add(30 * time.Second)
		for i := range batch {
			batch[i] = store.MetricsRow{DeviceID: fmt.Sprintf("device-%03d", i), At: at, CPU: new(10.0), Mem: new(50.0)}
		}
		if err := s.AddMetrics(b.Context(), batch); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRollUp folds what has come in the last 5 minutes, as the store
// does every 5 minutes.
func BenchmarkRollUp(b *testing.B) {
	s := benchDB(b)
	now := benchNow
	for b.Loop() {
		b.StopTimer()
		next := now.Add(5 * time.Minute)
		rows := make([]store.MetricsRow, 0, benchDevices*10)
		for at := now; at.Before(next); at = at.Add(30 * time.Second) {
			for i := range benchDevices {
				rows = append(rows, store.MetricsRow{DeviceID: fmt.Sprintf("device-%03d", i), At: at, CPU: new(10.0), Mem: new(50.0)})
			}
		}
		if err := s.AddMetrics(b.Context(), rows); err != nil {
			b.Fatal(err)
		}
		now = next
		b.StartTimer()
		if err := s.RollUp(b.Context(), now.Add(2*time.Minute)); err != nil {
			b.Fatal(err)
		}
	}
	benchNow = now
}

// BenchmarkPrune deletes what has expired, as the store does every hour.
func BenchmarkPrune(b *testing.B) {
	s := benchDB(b)
	for b.Loop() {
		s.Prune(b.Context(), benchNow.Add(time.Hour))
	}
}

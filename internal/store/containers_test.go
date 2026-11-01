package store_test

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
)

func TestStore_ContainerHistory(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "bield.db"), nil)
	start := time.UnixMilli(1_800_000_000_000).Truncate(5 * time.Minute)
	row := func(device, name string, at time.Duration, cpu, mem *float64) store.ContainerMetricsRow {
		return store.ContainerMetricsRow{DeviceID: device, Name: name, At: start.Add(at), CPU: cpu, Mem: mem, NetRx: new(100.0)}
	}
	err := s.AddContainerMetrics(t.Context(), []store.ContainerMetricsRow{
		row("hub", "web", -time.Minute, new(99.0), new(99.0)), // before the history starts
		row("hub", "web", 0, new(10.0), new(50.0)),
		row("hub", "web", 30*time.Second, new(20.0), nil),
		// The same name on another device, and another name on this one.
		row("laptop", "web", time.Minute, new(80.0), new(80.0)),
		row("hub", "db", time.Minute, new(70.0), new(70.0)),
		// Nothing from 5 to 10 minutes, then a row without CPU or memory.
		row("hub", "web", 10*time.Minute, nil, nil),
	})
	if err != nil {
		t.Fatalf("AddContainerMetrics() error = %v", err)
	}

	got, err := s.ContainerHistory(t.Context(), "hub", "web", start, 5*time.Minute)
	if err != nil {
		t.Fatalf("ContainerHistory() error = %v", err)
	}
	want := []store.ContainerBucket{
		{Start: start, CPU: new(15.0), Mem: new(50.0), NetRx: new(100.0)},
		{Start: start.Add(10 * time.Minute), NetRx: new(100.0)},
	}
	if len(got) != len(want) {
		t.Fatalf("ContainerHistory() = %+v, want %+v", got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		g.Start, w.Start = g.Start.UTC(), w.Start.UTC()
		if !reflect.DeepEqual(g, w) {
			t.Errorf("bucket %d = %+v, want %+v", i, g, w)
		}
	}
	if _, err := s.ContainerHistory(t.Context(), "hub", "web", start, time.Microsecond); err == nil {
		t.Error("ContainerHistory() with buckets under a millisecond didn't fail")
	}
	if err := s.AddContainerMetrics(t.Context(), nil); err != nil {
		t.Errorf("AddContainerMetrics(nil) error = %v", err)
	}
}

func TestStore_prunesContainerMetrics(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const week = 7 * 24 * time.Hour
		s := openStore(t, filepath.Join(t.TempDir(), "bield.db"), nil)
		synctest.Wait()
		now := time.Now()
		err := s.AddContainerMetrics(t.Context(), []store.ContainerMetricsRow{
			{DeviceID: "hub", Name: "expired", At: now.Add(-week - time.Minute)},
			{DeviceID: "hub", Name: "kept", At: now.Add(-week + 90*time.Minute)},
			{DeviceID: "hub", Name: "new", At: now},
		})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(store.PruneInterval)
		synctest.Wait()
		if got, want := firstColumn(t, s.DB(), "SELECT name FROM container_metrics ORDER BY ts"), []string{"kept", "new"}; !slices.Equal(got, want) {
			t.Errorf("container metrics after an hour = %q, want %q", got, want)
		}
	})
}

package store_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

func TestStore_MetricsHistory(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	start := time.UnixMilli(1_800_000_000_000).Truncate(5 * time.Minute)
	row := func(device string, at time.Duration, cpu, mem *float64) store.MetricsRow {
		return store.MetricsRow{DeviceID: device, At: start.Add(at), CPU: cpu, Mem: mem, NetRx: new(100.0), Temp: new(40.0)}
	}
	err := s.AddMetrics(t.Context(), []store.MetricsRow{
		row("laptop", -time.Minute, new(99.0), new(99.0)), // before the history starts
		row("laptop", 0, new(10.0), new(50.0)),
		row("laptop", 30*time.Second, new(20.0), nil),
		row("phone", time.Minute, new(80.0), new(80.0)),
		// Nothing from 5 to 10 minutes, then a row without CPU or memory.
		row("laptop", 10*time.Minute, nil, nil),
	})
	if err != nil {
		t.Fatalf("AddMetrics() error = %v", err)
	}

	got, err := s.MetricsHistory(t.Context(), "laptop", start, 5*time.Minute)
	if err != nil {
		t.Fatalf("MetricsHistory() error = %v", err)
	}
	want := []store.MetricsBucket{
		{Start: start, CPU: new(15.0), Mem: new(50.0), NetRx: new(100.0)},
		{Start: start.Add(10 * time.Minute), NetRx: new(100.0)},
	}
	if len(got) != len(want) {
		t.Fatalf("MetricsHistory() = %+v, want %+v", got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		g.Start, w.Start = g.Start.UTC(), w.Start.UTC()
		if !reflect.DeepEqual(g, w) {
			t.Errorf("bucket %d = %s, want %s", i, bucketString(g), bucketString(w))
		}
	}
	if _, err := s.MetricsHistory(t.Context(), "laptop", start, time.Microsecond); err == nil {
		t.Error("MetricsHistory() with buckets under a millisecond didn't fail")
	}
	if err := s.AddMetrics(t.Context(), nil); err != nil {
		t.Errorf("AddMetrics(nil) error = %v", err)
	}
}

func bucketString(b store.MetricsBucket) string {
	v := func(f *float64) any {
		if f == nil {
			return nil
		}
		return *f
	}
	return fmt.Sprint(map[string]any{
		"start": b.Start, "cpu": v(b.CPU), "mem": v(b.Mem), "disk": v(b.Disk), "rx": v(b.NetRx), "tx": v(b.NetTx),
	})
}

func TestStore_Agents(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	at := time.UnixMilli(1_800_000_000_000)
	for _, rec := range []store.AgentRecord{
		{DeviceID: "phone", ReceivedAt: at, Report: []byte(`{"v":1}`)},
		{DeviceID: "laptop", ReceivedAt: at, Report: []byte(`{"v":1}`)},
		{DeviceID: "laptop", ReceivedAt: at.Add(time.Minute), Report: []byte(`{"v":2}`)},
	} {
		if err := s.SaveAgent(t.Context(), rec); err != nil {
			t.Fatalf("SaveAgent(%s) error = %v", rec.DeviceID, err)
		}
	}
	if err := s.ForgetAgent(t.Context(), "phone"); err != nil {
		t.Errorf("ForgetAgent(phone) error = %v", err)
	}
	if err := s.ForgetAgent(t.Context(), "phone"); !errors.Is(err, store.ErrNoAgent) {
		t.Errorf("ForgetAgent(phone) again: error = %v, want ErrNoAgent", err)
	}
	got, err := s.Agents(t.Context())
	if err != nil {
		t.Fatalf("Agents() error = %v", err)
	}
	if len(got) != 1 || got[0].DeviceID != "laptop" || !got[0].ReceivedAt.Equal(at.Add(time.Minute)) || string(got[0].Report) != `{"v":2}` {
		t.Errorf("Agents() = %+v, want the laptop's second report", got)
	}
}

func TestStore_Usage(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	start := time.UnixMilli(1_800_000_000_000)
	add := func(device string, at time.Duration, detail string) {
		t.Helper()
		row := store.UsageRow{DeviceID: device, At: start.Add(at), Source: "agent", Cores: 4, Groups: `[]`}
		if detail != "" {
			row.Detail = []byte(detail)
		}
		if err := s.AddUsage(t.Context(), row); err != nil {
			t.Fatalf("AddUsage() error = %v", err)
		}
	}
	add("laptop", 0, "first")
	add("laptop", time.Minute, "")
	add("laptop", 2*time.Minute, "third")
	add("phone", 90*time.Second, "phone")

	since, err := s.UsageSince(t.Context(), "laptop", start.Add(time.Second))
	if err != nil {
		t.Fatalf("UsageSince() error = %v", err)
	}
	if len(since) != 2 || !since[0].At.Equal(start.Add(time.Minute)) || since[0].Cores != 4 || since[0].Source != "agent" || since[1].Detail != nil {
		t.Errorf("UsageSince() = %+v, want the last two minutes without detail", since)
	}

	for _, tt := range []struct {
		name   string
		at     time.Duration
		within time.Duration
		want   string
	}{
		{"exactly", 0, time.Minute, "first"},
		{"the nearer before", 50 * time.Second, time.Minute, "first"},
		// The minute between has no detail, so the third is nearer.
		{"the nearer after", 70 * time.Second, time.Minute, "third"},
		{"after the last", 3 * time.Minute, 2 * time.Minute, "third"},
		{"too far", 5 * time.Minute, time.Minute, ""},
	} {
		got, ok, err := s.UsageNear(t.Context(), "laptop", start.Add(tt.at), tt.within)
		if err != nil {
			t.Fatalf("%s: UsageNear() error = %v", tt.name, err)
		}
		if string(got.Detail) != tt.want || ok != (tt.want != "") {
			t.Errorf("%s: UsageNear() = %q, %v, want %q", tt.name, got.Detail, ok, tt.want)
		}
	}
}

func TestStore_prunesMetricsAndUsage(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const (
			day  = 24 * time.Hour
			week = 7 * day
		)
		s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
		synctest.Wait()
		now := time.Now()
		err := s.AddMetrics(t.Context(), []store.MetricsRow{
			{DeviceID: "expired", At: now.Add(-week - time.Minute)},
			{DeviceID: "kept", At: now.Add(-week + 2*time.Hour)},
		})
		if err != nil {
			t.Fatal(err)
		}
		// Minutes of usage two days ago, each with detail, and one that's expired.
		twoDaysAgo := now.Add(-2 * day).Truncate(10 * time.Minute)
		for i, at := range []time.Time{now.Add(-week - time.Minute), twoDaysAgo, twoDaysAgo.Add(time.Minute), now} {
			row := store.UsageRow{DeviceID: "laptop", At: at, Source: "hub", Groups: "[]", Detail: []byte{byte(i)}}
			if err := s.AddUsage(t.Context(), row); err != nil {
				t.Fatal(err)
			}
		}

		time.Sleep(store.PruneInterval)
		synctest.Wait()
		if got := firstColumn(t, s.DB(), "SELECT device_id FROM metrics"); len(got) != 0 {
			t.Errorf("raw metrics after an hour = %q, want none, since they're all older than two days", got)
		}
		if got := firstColumn(t, s.DB(), "SELECT DISTINCT device_id FROM metrics_rollup"); !slices.Equal(got, []string{"kept"}) {
			t.Errorf("metrics buckets after an hour = %q, want only the one that's under a week old", got)
		}
		rows, err := s.UsageSince(t.Context(), "laptop", time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 3 {
			t.Fatalf("usage after an hour = %d minutes, want the 3 under a week old", len(rows))
		}
		// Past a day, only one minute in ten keeps its detail.
		withDetail := firstColumn(t, s.DB(), "SELECT ts FROM usage_samples WHERE detail IS NOT NULL ORDER BY ts")
		if want := []string{strconv.FormatInt(twoDaysAgo.UnixMilli(), 10), strconv.FormatInt(now.UnixMilli(), 10)}; !slices.Equal(withDetail, want) {
			t.Errorf("minutes with detail = %q, want %q", withDetail, want)
		}
	})
}

package store_test

import (
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

// openQuiet opens a store that neither sums up nor prunes by itself, so a test
// decides when it does.
func openQuiet(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.OpenWith(t.Context(), filepath.Join(t.TempDir(), "roost.db"), store.Options{NoPrune: true}, store.Migrations())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func near(got, want *float64) bool {
	if got == nil || want == nil {
		return got == want
	}
	return math.Abs(*got-*want) < 1e-6
}

func TestStore_RollUp(t *testing.T) {
	t.Parallel()
	s := openQuiet(t)
	start := time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)
	// Every 30 seconds for 3 and a half hours, the CPU is the minutes since
	// the start, and the temperature is there for the first hour only.
	var rows []store.MetricsRow
	var pings []store.Ping
	for at := time.Duration(0); at < 3*time.Hour+30*time.Minute; at += 30 * time.Second {
		row := store.MetricsRow{DeviceID: "laptop", At: start.Add(at), CPU: new(at.Minutes())}
		if at < time.Hour {
			row.Temp = new(40.0)
		}
		rows = append(rows, row)
		// A ping each minute, which gets no reply in the first five.
		if at%time.Minute == 0 {
			pings = append(pings, store.Ping{DeviceID: "laptop", At: start.Add(at), Replied: at >= 5*time.Minute, RTT: 20 * time.Millisecond})
		}
	}
	if err := s.AddMetrics(t.Context(), rows); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPings(t.Context(), pings); err != nil {
		t.Fatal(err)
	}

	now := start.Add(3*time.Hour + 30*time.Minute)
	if err := s.RollUp(t.Context(), now); err != nil {
		t.Fatalf("RollUp() error = %v", err)
	}

	// The last complete 5 minutes ended at 3:25, an hour that's over ends at 3:00.
	if got := count(t, s, "SELECT COUNT(*) FROM metrics_rollup WHERE step = 300"); got != 41 {
		t.Errorf("%d buckets of 5 minutes, want 41", got)
	}
	if got := count(t, s, "SELECT COUNT(*) FROM metrics_rollup WHERE step = 3600"); got != 3 {
		t.Errorf("%d buckets of an hour, want 3", got)
	}
	// The second bucket of 5 minutes has the samples from 5:00 to 9:30.
	var n int
	var cpu, temp *float64
	if err := s.DB().QueryRowContext(t.Context(),
		"SELECT n, cpu, temp FROM metrics_rollup WHERE step = 300 AND ts = ?", start.Add(5*time.Minute).UnixMilli()).Scan(&n, &cpu, &temp); err != nil {
		t.Fatal(err)
	}
	if n != 10 || !near(cpu, new(7.25)) || !near(temp, new(40.0)) {
		t.Errorf("second bucket = %d samples, CPU %v, temperature %v, want 10, 7.25 and 40", n, cpu, temp)
	}
	// The hours are averages of what's in them, whatever the buckets in them
	// had: the second hour has no temperature, and the first only 12 buckets
	// of it.
	for hour, want := range []struct{ cpu, temp *float64 }{
		{new(29.75), new(40.0)}, {new(89.75), nil}, {new(149.75), nil},
	} {
		if err := s.DB().QueryRowContext(t.Context(),
			"SELECT n, cpu, temp FROM metrics_rollup WHERE step = 3600 AND ts = ?", start.Add(time.Duration(hour)*time.Hour).UnixMilli()).Scan(&n, &cpu, &temp); err != nil {
			t.Fatal(err)
		}
		if n != 120 || !near(cpu, want.cpu) || !near(temp, want.temp) {
			t.Errorf("hour %d = %d samples, CPU %v, temperature %v, want 120, %v and %v", hour, n, deref(cpu), deref(temp), deref(want.cpu), deref(want.temp))
		}
	}
	// The pings of the first hour: 60 sent, 55 answered.
	var sent, replies int
	var rtt *float64
	if err := s.DB().QueryRowContext(t.Context(),
		"SELECT pings, replies, rtt_ms FROM pings_rollup WHERE step = 3600 AND ts = ?", start.UnixMilli()).Scan(&sent, &replies, &rtt); err != nil {
		t.Fatal(err)
	}
	if sent != 60 || replies != 55 || !near(rtt, new(20.0)) {
		t.Errorf("first hour of pings = %d sent, %d answered, %v ms, want 60, 55 and 20", sent, replies, deref(rtt))
	}
	var silent *float64
	if err := s.DB().QueryRowContext(t.Context(),
		"SELECT rtt_ms FROM pings_rollup WHERE step = 300 AND ts = ?", start.UnixMilli()).Scan(&silent); err != nil || silent != nil {
		t.Errorf("RTT of the 5 minutes with no replies = %v, %v, want none", deref(silent), err)
	}

	// Again, the same, and then a little later, with a new bucket.
	if err := s.RollUp(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if got := count(t, s, "SELECT COUNT(*) FROM metrics_rollup"); got != 44 {
		t.Errorf("%d buckets after folding again, want the same 44", got)
	}
	if err := s.RollUp(t.Context(), now.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// The raw samples stop at 3:30, so the buckets from 3:30 on are empty, and
	// the bucket from 3:25 is new.
	if got := count(t, s, "SELECT COUNT(*) FROM metrics_rollup WHERE step = 300"); got != 42 {
		t.Errorf("%d buckets of 5 minutes after 10 minutes more, want 42", got)
	}
	if got := count(t, s, "SELECT COUNT(*) FROM metrics_rollup WHERE step = 300 AND ts = "+strconv.FormatInt(start.Add(3*time.Hour+25*time.Minute).UnixMilli(), 10)); got != 1 {
		t.Errorf("the bucket from 3:25 was folded %d times, want once", got)
	}
}

func deref(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func count(t *testing.T, s *store.Store, query string) int {
	t.Helper()
	var n int
	if err := s.DB().QueryRowContext(t.Context(), query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// TestStore_historyAcrossBuckets checks that a history that reaches back past
// the raw samples is made of the buckets they were folded into, and it
// continues into the raw ones: the same numbers as if every sample were kept.
func TestStore_historyAcrossBuckets(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
		synctest.Wait()
		now := time.Now()
		// A sample a minute for 5 days: the CPU is the hour of the day, and the RTT
		// is the day's minute, and a ping in every 10 gets no reply.
		cpu := func(at time.Time) float64 { return float64(at.UTC().Hour()) }
		first := now.Add(-5 * 24 * time.Hour).Truncate(time.Minute)
		var rows []store.MetricsRow
		var pings []store.Ping
		for at := first; at.Before(now); at = at.Add(time.Minute) {
			rows = append(rows, store.MetricsRow{DeviceID: "laptop", At: at, CPU: new(cpu(at)), Mem: new(50.0)})
			pings = append(pings, store.Ping{DeviceID: "laptop", At: at, Replied: at.Minute()%10 != 0, RTT: time.Duration(at.Hour()+1) * time.Millisecond})
		}
		if err := s.AddMetrics(t.Context(), rows); err != nil {
			t.Fatal(err)
		}
		if err := s.AddPings(t.Context(), pings); err != nil {
			t.Fatal(err)
		}
		// Folds the samples, and then deletes the old ones.
		time.Sleep(store.PruneInterval)
		synctest.Wait()
		if got := count(t, s, "SELECT COUNT(*) FROM metrics WHERE ts < "+strconv.FormatInt(now.Add(-48*time.Hour).UnixMilli(), 10)); got != 0 {
			t.Fatalf("%d raw samples older than 48 hours are left", got)
		}
		if got := count(t, s, "SELECT COUNT(*) FROM metrics"); got < 47*60 {
			t.Fatalf("%d raw samples are left, want the last 47 hours that are left of the 48 after the hour that passed", got)
		}

		for _, size := range []time.Duration{time.Hour, 5 * time.Minute, 2 * time.Hour} {
			since := now.Add(-4 * 24 * time.Hour).Truncate(size)
			got, err := s.MetricsHistory(t.Context(), "laptop", since, size)
			if err != nil {
				t.Fatal(err)
			}
			// Every bucket has what the samples in it say, from the buckets or
			// from the samples, and none is twice.
			seen := map[time.Time]bool{}
			for _, b := range got {
				if seen[b.Start] {
					t.Fatalf("size %v: bucket %v twice", size, b.Start)
				}
				seen[b.Start] = true
				if b.CPU == nil || !near(b.Mem, new(50.0)) {
					t.Fatalf("size %v: bucket %v = %+v, want CPU and 50%% memory", size, b.Start, b)
				}
				// The average of the hours of the day in a bucket, which is just its hour
				// when it's an hour or less, and two hours' average for 2 hours.
				want := cpu(b.Start)
				if size == 2*time.Hour {
					want = float64(b.Start.UTC().Hour()) + 0.5
				}
				if math.Abs(*b.CPU-want) > 1e-6 {
					t.Fatalf("size %v: bucket %v CPU = %v, want %v", size, b.Start, *b.CPU, want)
				}
			}
			// From 4 days ago on there's a bucket for every span, but the one the
			// 5-minute buckets can't give for a 2-hour size... they can: it's a multiple.
			if want := int(now.Sub(since) / size); len(got) < want-1 || len(got) > want+1 {
				t.Errorf("size %v: %d buckets in 4 days, want about %d", size, len(got), want)
			}
		}

		// A size the buckets don't fill gets only the raw samples.
		got, err := s.MetricsHistory(t.Context(), "laptop", now.Add(-4*24*time.Hour), 2*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 || got[0].Start.Before(now.Add(-49*time.Hour)) {
			t.Errorf("2-minute history starts at %v, want only the raw samples of 48 hours", got[0].Start)
		}

		rtt, err := s.RTTHistory(t.Context(), "laptop", now.Add(-4*24*time.Hour).Truncate(time.Hour), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range rtt {
			if b.Pings < 55 && b.Start.Before(now.Add(-time.Hour)) {
				t.Fatalf("hour %v has %d pings, want 60", b.Start, b.Pings)
			}
			if b.Replies != 54 && b.Start.Before(now.Add(-time.Hour)) {
				t.Fatalf("hour %v has %d replies, want 54", b.Start, b.Replies)
			}
			if want := time.Duration(b.Start.UTC().Hour()+1) * time.Millisecond; b.Replies > 0 && b.RTT != want && b.Start.Before(now.Add(-time.Hour)) {
				t.Fatalf("hour %v RTT = %v, want %v", b.Start, b.RTT, want)
			}
		}
		if first := rtt[0].Start; first.Before(now.Add(-4 * 24 * time.Hour).Truncate(time.Hour)) {
			t.Errorf("RTT history starts at %v, before it was asked to", first)
		}
	})
}

func TestStore_bucketsLastAsLongAsHistory(t *testing.T) {
	t.Parallel()
	const day = 24 * time.Hour
	synctest.Test(t, func(t *testing.T) {
		s, err := store.OpenWith(t.Context(), filepath.Join(t.TempDir(), "roost.db"),
			store.Options{Retention: fixedRetention(store.Retention{Activity: day, History: 90 * day, Checks: day})}, store.Migrations())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		synctest.Wait()
		now := time.Now()
		// A bucket of 5 minutes and of an hour, 10, 20 and 100 days old.
		var rows []store.MetricsRollup
		for _, age := range []time.Duration{10 * day, 20 * day, 100 * day} {
			for _, step := range []time.Duration{store.FineStep, store.CoarseStep} {
				rows = append(rows, store.MetricsRollup{
					DeviceID: "laptop", Step: step, At: now.Add(-age).Truncate(step), N: 1, CPU: new(1.0),
				})
			}
		}
		if err := s.AddMetricsRollups(t.Context(), rows); err != nil {
			t.Fatal(err)
		}
		if err := s.AddPingRollups(t.Context(), []store.PingRollup{
			{DeviceID: "laptop", Step: store.CoarseStep, At: now.Add(-20 * day).Truncate(time.Hour), Pings: 60, Replies: 60, RTT: time.Millisecond},
			{DeviceID: "laptop", Step: store.CoarseStep, At: now.Add(-100 * day).Truncate(time.Hour), Pings: 60},
		}); err != nil {
			t.Fatal(err)
		}

		time.Sleep(store.PruneInterval)
		synctest.Wait()

		// The buckets of 5 minutes go after 14 days, and the hours after 90.
		if got, want := firstColumn(t, s.DB(), "SELECT step || ':' || COUNT(*) FROM metrics_rollup GROUP BY step ORDER BY step"),
			[]string{"300:1", "3600:2"}; !slices.Equal(got, want) {
			t.Errorf("metrics buckets after an hour = %q, want %q", got, want)
		}
		if got := count(t, s, "SELECT COUNT(*) FROM pings_rollup"); got != 1 {
			t.Errorf("%d ping buckets after an hour, want the one that's 20 days old", got)
		}
	})
}

func TestStore_rollupsRoundTrip(t *testing.T) {
	t.Parallel()
	s := openQuiet(t)
	// A demo's history, which ends 3 days ago.
	end := time.Now().Add(-3 * 24 * time.Hour).Truncate(time.Hour)
	var rows []store.MetricsRollup
	var pings []store.PingRollup
	for i := range 48 {
		at := end.Add(-time.Duration(i+1) * time.Hour)
		rows = append(rows, store.MetricsRollup{DeviceID: "laptop", Step: store.CoarseStep, At: at, N: 120, CPU: new(float64(i)), Mem: new(50.0)})
		pings = append(pings, store.PingRollup{DeviceID: "laptop", Step: store.CoarseStep, At: at, Pings: 60, Replies: 30, RTT: 8 * time.Millisecond})
	}
	if err := s.AddMetricsRollups(t.Context(), rows); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPingRollups(t.Context(), pings); err != nil {
		t.Fatal(err)
	}

	got, err := s.MetricsHistory(t.Context(), "laptop", end.Add(-48*time.Hour), 2*time.Hour)

	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 24 {
		t.Fatalf("%d buckets of 2 hours in 48 hours, want 24", len(got))
	}
	// The newest bucket has the hours i = 0 and 1, which averaged 0.5.
	if last := got[len(got)-1]; !near(last.CPU, new(0.5)) || !near(last.Mem, new(50.0)) {
		t.Errorf("newest bucket = %+v, want CPU 0.5 and memory 50", last)
	}
	rtt, err := s.RTTHistory(t.Context(), "laptop", end.Add(-48*time.Hour), time.Hour)
	if err != nil || len(rtt) != 48 || rtt[0].Pings != 60 || rtt[0].Replies != 30 || rtt[0].RTT != 8*time.Millisecond {
		t.Errorf("RTTHistory() = %d buckets, %+v, %v, want 48 hours of 60 pings, 30 answered, 8 ms", len(rtt), rtt, err)
	}
	if err := s.AddMetricsRollups(t.Context(), nil); err != nil {
		t.Errorf("AddMetricsRollups(nil) error = %v", err)
	}
	if err := s.AddPingRollups(t.Context(), nil); err != nil {
		t.Errorf("AddPingRollups(nil) error = %v", err)
	}
}

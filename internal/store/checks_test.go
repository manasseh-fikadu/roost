package store_test

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

func TestStore_defaultChecks(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	got, err := s.Checks(t.Context())
	if err != nil {
		t.Fatalf("Checks() error = %v", err)
	}
	want := []store.Check{
		{Name: "Cloudflare edge", Kind: "tcp", Target: "1.1.1.1:443", Group: "Internet", Interval: 30 * time.Second, Timeout: 3 * time.Second},
		{Name: "DNS resolution", Kind: "dns", Target: "cloudflare.com", Group: "Internet", Interval: time.Minute, Timeout: 3 * time.Second},
		{Name: "HTTPS egress", Kind: "http", Target: "https://www.google.com/generate_204", Group: "Internet", Interval: time.Minute, Timeout: 5 * time.Second, ExpectStatus: 204},
	}
	if len(got) != len(want) {
		t.Fatalf("Checks() = %+v, want the %d default checks", got, len(want))
	}
	ids := map[string]bool{}
	for i, c := range got {
		if len(c.ID) != 16 || ids[c.ID] || time.Since(c.Created) > time.Hour {
			t.Errorf("default check %d has ID %q and was created %v, want a new ID and now", i, c.ID, c.Created)
		}
		ids[c.ID] = true
		c.ID, c.Created = "", time.Time{}
		if c != want[i] {
			t.Errorf("default check %d = %+v, want %+v", i, c, want[i])
		}
	}
}

func TestStore_Checks(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()
	created := time.UnixMilli(1_800_000_000_000)
	c, err := s.AddCheck(ctx, store.Check{
		Name: "API", Kind: "http", Target: "https://api.example.com/health", Group: "Apps",
		Interval: 30 * time.Second, Timeout: 2 * time.Second, Insecure: true, Created: created,
	})
	if err != nil {
		t.Fatalf("AddCheck() error = %v", err)
	}
	other, err := s.AddCheck(ctx, store.Check{Name: "Postgres", Kind: "tcp", Target: "db:5432", Group: "Apps", Interval: time.Minute, Timeout: time.Second, Created: created})
	if err != nil {
		t.Fatalf("AddCheck() error = %v", err)
	}
	if c.ID == "" || c.ID == other.ID {
		t.Fatalf("AddCheck() gave IDs %q and %q, want two different ones", c.ID, other.ID)
	}

	c.Name, c.ExpectStatus, c.Paused, c.Created = "API health", 204, true, time.Time{}
	if err := s.UpdateCheck(ctx, c); err != nil {
		t.Fatalf("UpdateCheck() error = %v", err)
	}
	c.Created = created
	got := checksByID(t, s)
	if got[c.ID] != c {
		t.Errorf("after UpdateCheck(), the check is %+v, want %+v, created as before", got[c.ID], c)
	}

	if err := s.DeleteCheck(ctx, other.ID); err != nil {
		t.Fatalf("DeleteCheck() error = %v", err)
	}
	if _, ok := checksByID(t, s)[other.ID]; ok {
		t.Error("the check is still there after DeleteCheck()")
	}
	for name, err := range map[string]error{
		"UpdateCheck":    s.UpdateCheck(ctx, other),
		"DeleteCheck":    s.DeleteCheck(ctx, other.ID),
		"AddCheckResult": s.AddCheckResult(ctx, store.CheckResult{CheckID: other.ID, At: created, OK: true}),
	} {
		if !errors.Is(err, store.ErrNoCheck) {
			t.Errorf("%s() of a deleted check: error = %v, want ErrNoCheck", name, err)
		}
	}
}

func checksByID(t *testing.T, s *store.Store) map[string]store.Check {
	t.Helper()
	checks, err := s.Checks(t.Context())
	if err != nil {
		t.Fatalf("Checks() error = %v", err)
	}
	out := map[string]store.Check{}
	for _, c := range checks {
		out[c.ID] = c
	}
	return out
}

func TestStore_checkResults(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()
	start := time.UnixMilli(1_800_000_000_000).Truncate(5 * time.Minute)
	api := addCheck(t, s, "API")
	db := addCheck(t, s, "DB")
	for _, r := range []store.CheckResult{
		{CheckID: api, At: start.Add(-time.Minute), OK: true, Latency: 99 * time.Millisecond, Detail: "before"},
		{CheckID: api, At: start, OK: true, Latency: 10 * time.Millisecond, Detail: "HTTP 200"},
		{CheckID: api, At: start.Add(time.Minute), OK: false, Latency: 30 * time.Millisecond, Detail: "HTTP 500"},
		{CheckID: api, At: start.Add(2 * time.Minute), OK: false, Detail: "Timed out after 5s"},
		{CheckID: api, At: start.Add(11 * time.Minute), OK: true, Latency: 20 * time.Millisecond, Detail: "HTTP 200"},
		{CheckID: db, At: start, OK: false, Detail: "connection refused"},
	} {
		if err := s.AddCheckResult(ctx, r); err != nil {
			t.Fatalf("AddCheckResult(%s) error = %v", r.Detail, err)
		}
	}

	recent, err := s.RecentCheckResults(ctx, start, 2)
	if err != nil {
		t.Fatalf("RecentCheckResults() error = %v", err)
	}
	details := map[string][]string{}
	for id, rs := range recent {
		for _, r := range rs {
			details[id] = append(details[id], r.Detail)
		}
	}
	if want := map[string][]string{api: {"Timed out after 5s", "HTTP 200"}, db: {"connection refused"}}; !maps.EqualFunc(details, want, slices.Equal) {
		t.Errorf("RecentCheckResults(2) = %q, want %q", details, want)
	}
	if r := recent[api][1]; !r.At.Equal(start.Add(11*time.Minute)) || !r.OK || r.Latency != 20*time.Millisecond {
		t.Errorf("RecentCheckResults() latest = %+v, want the 20 ms pass", r)
	}

	history, err := s.CheckHistory(ctx, api, start, 5*time.Minute)
	if err != nil {
		t.Fatalf("CheckHistory() error = %v", err)
	}
	want := []store.CheckBucket{
		{Start: start, Runs: 3, Passed: 1, Latency: 20 * time.Millisecond},
		{Start: start.Add(10 * time.Minute), Runs: 1, Passed: 1, Latency: 20 * time.Millisecond},
	}
	if len(history) != len(want) {
		t.Fatalf("CheckHistory() = %+v, want %+v", history, want)
	}
	for i := range want {
		if g, w := history[i], want[i]; !g.Start.Equal(w.Start) || g.Runs != w.Runs || g.Passed != w.Passed || g.Latency != w.Latency {
			t.Errorf("bucket %d = %+v, want %+v", i, g, w)
		}
	}
	if _, err := s.CheckHistory(ctx, api, start, time.Microsecond); err == nil {
		t.Error("CheckHistory() with buckets under a millisecond didn't fail")
	}

	// Deleting a check deletes its results.
	if err := s.DeleteCheck(ctx, api); err != nil {
		t.Fatal(err)
	}
	if got := firstColumn(t, s.DB(), "SELECT detail FROM check_results"); !slices.Equal(got, []string{"connection refused"}) {
		t.Errorf("results after deleting a check = %q, want only the other check's", got)
	}
}

// addCheck adds a TCP check called name, and returns its ID.
func addCheck(t *testing.T, s *store.Store, name string) string {
	t.Helper()
	c, err := s.AddCheck(t.Context(), store.Check{Name: name, Kind: "tcp", Target: "db:5432", Group: "Test", Interval: time.Minute, Timeout: time.Second})
	if err != nil {
		t.Fatalf("AddCheck(%s) error = %v", name, err)
	}
	return c.ID
}

func TestStore_RecentPingsAndTallies(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()
	start := time.UnixMilli(1_800_000_000_000)
	api := addCheck(t, s, "API")
	err := s.AddPings(ctx, []store.Ping{
		{DeviceID: "laptop", At: start.Add(-time.Minute), Replied: false},
		{DeviceID: "laptop", At: start, Replied: true, RTT: 12 * time.Millisecond},
		{DeviceID: "laptop", At: start.Add(time.Minute), Replied: false},
		{DeviceID: "laptop", At: start.Add(2 * time.Minute), Replied: true, RTT: 8 * time.Millisecond},
		{DeviceID: "laptop", At: start.Add(3 * time.Minute), Replied: true, RTT: 9 * time.Millisecond},
		{DeviceID: "phone", At: start.Add(time.Minute), Replied: true, RTT: 50 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, ok := range []bool{true, true, false, true} {
		if err := s.AddCheckResult(ctx, store.CheckResult{CheckID: api, At: start.Add(time.Duration(i) * time.Minute), OK: ok}); err != nil {
			t.Fatal(err)
		}
	}

	pings, err := s.RecentPings(ctx, start, 3)
	if err != nil {
		t.Fatalf("RecentPings() error = %v", err)
	}
	if got := pings["laptop"]; len(got) != 3 || got[0].Replied || !got[1].Replied || got[1].RTT != 8*time.Millisecond || !got[2].At.Equal(start.Add(3*time.Minute)) {
		t.Errorf("RecentPings(laptop) = %+v, want its last 3 pings, oldest first", got)
	}
	if got := pings["phone"]; len(got) != 1 || got[0].RTT != 50*time.Millisecond {
		t.Errorf("RecentPings(phone) = %+v, want its one ping", got)
	}

	// In 2-minute spans from the start.
	checks, pings2, err := s.Tallies(ctx, start, 2*time.Minute)
	if err != nil {
		t.Fatalf("Tallies() error = %v", err)
	}
	if got, want := tallies(checks, start), []string{api + " +0s 2/2", api + " +2m0s 1/2"}; !slices.Equal(got, want) {
		t.Errorf("Tallies() checks = %q, want %q", got, want)
	}
	if got, want := tallies(pings2, start), []string{"laptop +0s 1/2", "laptop +2m0s 2/2", "phone +0s 1/1"}; !slices.Equal(got, want) {
		t.Errorf("Tallies() pings = %q, want %q, without the ping before the start", got, want)
	}
	if _, _, err := s.Tallies(ctx, start, 0); err == nil {
		t.Error("Tallies() with spans under a millisecond didn't fail")
	}
}

// tallies writes each tally as "ID +offset ok/total", sorted.
func tallies(ts []store.Tally, start time.Time) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = fmt.Sprintf("%s +%v %d/%d", t.ID, t.Start.Sub(start), t.OK, t.Total)
	}
	slices.Sort(out)
	return out
}

func TestStore_HealthHistory(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	start := time.UnixMilli(1_800_000_000_000).Truncate(5 * time.Minute)
	for _, h := range []store.HealthSample{
		{At: start.Add(-time.Minute), Score: 0},
		{At: start, Score: 100, Passing: 4},
		{At: start.Add(time.Minute), Score: 50, Passing: 2, Failing: 2},
		{At: start.Add(time.Minute), Score: 75, Passing: 3, Failing: 1}, // replaces the one before
		{At: start.Add(12 * time.Minute), Score: 80, Passing: 4, Failing: 1, Unavailable: 2},
	} {
		if err := s.AddHealth(t.Context(), h); err != nil {
			t.Fatalf("AddHealth() error = %v", err)
		}
	}
	got, err := s.HealthHistory(t.Context(), start, 5*time.Minute)
	if err != nil {
		t.Fatalf("HealthHistory() error = %v", err)
	}
	want := []store.HealthBucket{{Start: start, Score: 87.5}, {Start: start.Add(10 * time.Minute), Score: 80}}
	if len(got) != len(want) {
		t.Fatalf("HealthHistory() = %+v, want %+v", got, want)
	}
	for i := range want {
		if !got[i].Start.Equal(want[i].Start) || got[i].Score != want[i].Score {
			t.Errorf("bucket %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if _, err := s.HealthHistory(t.Context(), start, 0); err == nil {
		t.Error("HealthHistory() with buckets under a millisecond didn't fail")
	}
}

func TestStore_prunesCheckResultsAndHealth(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const week = 7 * 24 * time.Hour
		s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
		synctest.Wait()
		now := time.Now()
		id := addCheck(t, s, "API")
		for _, at := range []time.Time{now.Add(-week - time.Minute), now.Add(-week + 2*time.Hour)} {
			if err := s.AddCheckResult(t.Context(), store.CheckResult{CheckID: id, At: at, OK: true}); err != nil {
				t.Fatal(err)
			}
			if err := s.AddHealth(t.Context(), store.HealthSample{At: at, Score: 100}); err != nil {
				t.Fatal(err)
			}
		}

		time.Sleep(store.PruneInterval)
		synctest.Wait()
		for _, table := range []string{"check_results", "health"} {
			if got := firstColumn(t, s.DB(), "SELECT COUNT(*) FROM "+table); !slices.Equal(got, []string{"1"}) {
				t.Errorf("%s after an hour has %s rows, want only the one under a week old", table, got)
			}
		}
	})
}

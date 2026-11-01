package demo

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

// TestSeed_olderHistoryIsInBuckets checks that the history that reaches back
// past the raw samples of the last 48 hours is made of buckets, and that
// the buckets and the samples make an unbroken run.
func TestSeed_olderHistoryIsInBuckets(t *testing.T) {
	t.Parallel()
	// Roost's own machine and one more are enough to see it.
	d := New(Options{Devices: 2, Listen: "127.0.0.1:0"})
	t.Cleanup(d.Close)
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "roost.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const span = 3 * 24 * time.Hour
	if err := d.seedMetrics(t.Context(), st, span); err != nil {
		t.Fatal(err)
	}
	if err := d.seedPings(t.Context(), st, span); err != nil {
		t.Fatal(err)
	}
	if err := st.RollUp(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	since := d.start.Add(-60 * time.Hour)

	// Buckets of 5 minutes, from the buckets and then from the samples of the
	// last 48 hours, with nothing missing.
	fine, err := st.MetricsHistory(t.Context(), store.HubID, since, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(fine); i++ {
		if gap := fine[i].Start.Sub(fine[i-1].Start); gap != 5*time.Minute {
			t.Fatalf("buckets %v and %v are %v apart, want 5 minutes", fine[i-1].Start, fine[i].Start, gap)
		}
	}
	if len(fine) < 60*12-2 {
		t.Errorf("%d buckets of 5 minutes in 60 hours, want %d", len(fine), 60*12)
	}

	// A history of 60 hours is an unbroken run of hours: buckets, and then samples.
	hist, err := st.MetricsHistory(t.Context(), store.HubID, since, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(hist); i++ {
		if gap := hist[i].Start.Sub(hist[i-1].Start); gap != time.Hour {
			t.Fatalf("buckets %v and %v are %v apart, want an hour", hist[i-1].Start, hist[i].Start, gap)
		}
	}
	if len(hist) < 59 || hist[0].CPU == nil || hist[0].Disk == nil {
		t.Errorf("the hub's history has %d hours, the first %+v, want 60 hours", len(hist), hist[0])
	}
	rtt, err := st.RTTHistory(t.Context(), d.machines[1].id, since, time.Hour)
	if err != nil || len(rtt) < 59 {
		t.Errorf("RTTHistory() = %d hours, %v, want 60 hours of them", len(rtt), err)
	}
}

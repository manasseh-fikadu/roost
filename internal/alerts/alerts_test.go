package alerts_test

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/alerts"
	"github.com/manasseh-fikadu/roost/internal/store"
)

// newStore opens a store. Call it in a synctest bubble.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "roost.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	synctest.Wait()
	return st
}

func newEngine(t *testing.T, st *store.Store) *alerts.Engine {
	t.Helper()
	e := alerts.NewEngine(st, nil)
	if err := e.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return e
}

func always(string, string) bool { return true }

func disk(pct string, sev alerts.Severity) alerts.Condition {
	return alerts.Condition{
		Key: "disk:laptop", Source: "metrics", DeviceID: "laptop", Severity: sev,
		Title: "Disk " + pct + "% full on laptop", Detail: "Free some space",
	}
}

var check = alerts.Condition{Key: "check:abc", Source: "check", CheckID: "abc", Severity: alerts.Critical, Title: "API is failing"}

// titles returns the titles of the open alerts, in order.
func titles(e *alerts.Engine) []string {
	var out []string
	for _, a := range e.Open() {
		out = append(out, a.Title)
	}
	return out
}

// messages returns the activity log's messages, oldest first.
func messages(t *testing.T, st *store.Store) []string {
	t.Helper()
	events, err := st.Events(t.Context(), store.EventQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ev := range slices.Backward(events) {
		out = append(out, ev.Level+" "+ev.Message)
	}
	return out
}

func TestEngine_opensAndResolves(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := newStore(t)
		e := newEngine(t, st)
		ctx := t.Context()

		if !e.Apply(ctx, []alerts.Condition{disk("86", alerts.Warning), check, check}, always, time.Now()) {
			t.Error("Apply() opening two alerts reported no change")
		}
		opened := time.Now()
		if got, want := titles(e), []string{"API is failing", "Disk 86% full on laptop"}; !slices.Equal(got, want) {
			t.Errorf("open alerts = %q, want %q, the critical one first", got, want)
		}

		// The same conditions keep the same alerts, with what's new about them.
		time.Sleep(time.Minute)
		if e.Apply(ctx, []alerts.Condition{disk("88", alerts.Warning), check}, always, time.Now()) {
			t.Error("Apply() with the same conditions reported a change")
		}
		if !e.Apply(ctx, []alerts.Condition{disk("96", alerts.Critical), check}, always, time.Now()) {
			t.Error("Apply() with a condition that got worse reported no change")
		}
		a := e.Open()[1]
		if a.Title != "Disk 96% full on laptop" || a.Severity != "critical" || !a.OpenedAt.Equal(opened) || !a.LastSeen.Equal(time.Now()) {
			t.Errorf("the disk alert = %+v, want it critical, opened as before and seen now", a)
		}

		// A condition that's gone for less than a minute, and comes back,
		// stays one alert.
		e.Apply(ctx, []alerts.Condition{check}, always, time.Now())
		time.Sleep(30 * time.Second)
		e.Apply(ctx, []alerts.Condition{check}, always, time.Now())
		e.Apply(ctx, []alerts.Condition{disk("96", alerts.Critical), check}, always, time.Now())
		time.Sleep(30 * time.Second)
		e.Apply(ctx, []alerts.Condition{check}, always, time.Now())
		if got := titles(e); len(got) != 2 {
			t.Errorf("after the disk condition flickered, open alerts = %q, want both still", got)
		}

		// Gone for a minute, it resolves, from when it went.
		cleared := time.Now()
		time.Sleep(alerts.ResolveAfter)
		if !e.Apply(ctx, []alerts.Condition{check}, always, time.Now()) {
			t.Error("Apply() resolving an alert reported no change")
		}
		if got := titles(e); !slices.Equal(got, []string{"API is failing"}) {
			t.Errorf("after the disk condition cleared, open alerts = %q, want only the check's", got)
		}
		resolved, err := st.ResolvedAlerts(ctx, 0, 10)
		if err != nil || len(resolved) != 1 || !resolved[0].ResolvedAt.Equal(cleared) || resolved[0].Title != "Disk 96% full on laptop" {
			t.Errorf("resolved alerts = %+v, %v, want the disk alert, resolved when it cleared", resolved, err)
		}

		// It comes back as a new alert.
		e.Apply(ctx, []alerts.Condition{disk("90", alerts.Warning), check}, always, time.Now())
		if open := e.Open(); len(open) != 2 || open[1].ID == resolved[0].ID {
			t.Errorf("open alerts = %+v, want a new disk alert", open)
		}
		want := []string{
			"warning Disk 86% full on laptop",
			"error API is failing",
			"success Resolved: Disk 96% full on laptop",
			"warning Disk 90% full on laptop",
		}
		got := messages(t, st)
		slices.Sort(got[:2])
		slices.Sort(want[:2])
		if !slices.Equal(got, want) {
			t.Errorf("the activity log = %q, want %q", got, want)
		}
	})
}

func TestEngine_onlyResolvesWithFreshData(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := newStore(t)
		e := newEngine(t, st)
		ctx := t.Context()
		e.Apply(ctx, []alerts.Condition{disk("90", alerts.Warning), check}, always, time.Now())

		// The laptop's agent went quiet, so its metrics say nothing.
		stale := func(source, device string) bool { return source != "metrics" || device != "laptop" }
		for range 10 {
			time.Sleep(time.Minute)
			e.Apply(ctx, nil, stale, time.Now())
		}
		if got := titles(e); !slices.Equal(got, []string{"Disk 90% full on laptop"}) {
			t.Errorf("open alerts = %q, want only the disk alert, whose data is stale", got)
		}

		// It must be gone for a minute of fresh data.
		e.Apply(ctx, nil, always, time.Now())
		time.Sleep(alerts.ResolveAfter - time.Second)
		e.Apply(ctx, nil, always, time.Now())
		if got := titles(e); len(got) != 1 {
			t.Errorf("open alerts = %q, want the disk alert, not gone a minute yet", got)
		}
		time.Sleep(time.Second)
		e.Apply(ctx, nil, always, time.Now())
		if got := titles(e); len(got) != 0 {
			t.Errorf("open alerts = %q, want none", got)
		}
	})
}

func TestEngine_Load(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := newStore(t)
		e := newEngine(t, st)
		e.Apply(t.Context(), []alerts.Condition{disk("90", alerts.Warning)}, always, time.Now())
		// After a restart, the alert is still open, and the same condition
		// doesn't open another.
		again := newEngine(t, st)
		if got := titles(again); !slices.Equal(got, []string{"Disk 90% full on laptop"}) {
			t.Errorf("after Load(), open alerts = %q, want the disk alert", got)
		}
		if again.Apply(t.Context(), []alerts.Condition{disk("91", alerts.Warning)}, always, time.Now()) {
			t.Error("Apply() after Load() opened another alert")
		}
	})
}

func TestEngine_ackSnoozeAndNotes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := newStore(t)
		e := newEngine(t, st)
		ctx := t.Context()
		e.Apply(ctx, []alerts.Condition{disk("90", alerts.Warning)}, always, time.Now())
		id := e.Open()[0].ID
		const alice = "alice@example.com"

		a, err := e.Ack(ctx, id, alice, true, time.Now())
		if err != nil || !a.AckedAt.Equal(time.Now()) || a.AckedBy != alice {
			t.Errorf("Ack() = %+v, %v, want it acknowledged by alice now", a, err)
		}
		if a, err := e.Ack(ctx, id, alice, false, time.Now()); err != nil || !a.AckedAt.IsZero() || a.AckedBy != "" {
			t.Errorf("Ack(false) = %+v, %v, want it unacknowledged", a, err)
		}
		if a, err := e.Snooze(ctx, id, alice, time.Now().Add(4*time.Hour), time.Now()); err != nil || !a.SnoozedUntil.Equal(time.Now().Add(4*time.Hour)) {
			t.Errorf("Snooze(4h) = %+v, %v, want it snoozed for 4 hours", a, err)
		}
		if a, err := e.Snooze(ctx, id, alice, time.Now().Add(365*24*time.Hour), time.Now()); err != nil || !a.SnoozedUntil.Equal(time.Now().Add(alerts.MaxSnooze)) {
			t.Errorf("Snooze(a year) = %+v, %v, want it snoozed for the longest", a, err)
		}
		if _, err := e.Snooze(ctx, id, alice, time.Now().Add(-time.Minute), time.Now()); err == nil {
			t.Error("Snooze() until a time gone didn't fail")
		}
		if a, err := e.Snooze(ctx, id, alice, time.Time{}, time.Now()); err != nil || !a.SnoozedUntil.IsZero() {
			t.Errorf("Snooze(zero) = %+v, %v, want it awake", a, err)
		}
		for name, err := range map[string]error{
			"Ack":    func() error { _, err := e.Ack(ctx, 999, alice, true, time.Now()); return err }(),
			"Snooze": func() error { _, err := e.Snooze(ctx, 999, alice, time.Now().Add(time.Hour), time.Now()); return err }(),
		} {
			if !errors.Is(err, alerts.ErrNotOpen) {
				t.Errorf("%s() of a missing alert: error = %v, want ErrNotOpen", name, err)
			}
		}

		n, err := e.AddNote(ctx, id, alice, "  Cleaning up logs \n", time.Now().Add(1234567*time.Nanosecond))
		if err != nil || n.Text != "Cleaning up logs" || n.Author != alice {
			t.Errorf("AddNote() = %+v, %v, want the note, trimmed", n, err)
		}
		// The note is the same as the store reads back, to the millisecond it
		// keeps.
		if a, err := st.AlertByID(ctx, id); err != nil || len(a.Notes) != 1 || !a.Notes[0].At.Equal(n.At) {
			t.Errorf("the stored notes = %+v, %v, want one at %v", a.Notes, err, n.At)
		}
		if notes := e.Open()[0].Notes; len(notes) != 1 {
			t.Errorf("the open alert's notes = %+v, want the one", notes)
		}
		for _, text := range []string{" ", strings.Repeat("x", alerts.MaxNote+1)} {
			if _, err := e.AddNote(ctx, id, alice, text, time.Now()); err == nil {
				t.Errorf("AddNote(%d characters) didn't fail", len(text))
			}
		}
		if _, err := e.AddNote(ctx, 999, alice, "x", time.Now()); !errors.Is(err, store.ErrNoAlert) {
			t.Errorf("AddNote() of a missing alert: error = %v, want ErrNoAlert", err)
		}

		// A resolved alert takes notes too.
		e.Apply(ctx, nil, always, time.Now())
		time.Sleep(alerts.ResolveAfter)
		e.Apply(ctx, nil, always, time.Now())
		if _, err := e.AddNote(ctx, id, alice, "It was the logs", time.Now()); err != nil {
			t.Errorf("AddNote() of a resolved alert: error = %v", err)
		}
		if _, err := e.Ack(ctx, id, alice, true, time.Now()); !errors.Is(err, alerts.ErrNotOpen) {
			t.Errorf("Ack() of a resolved alert: error = %v, want ErrNotOpen", err)
		}

		events, err := st.Events(ctx, store.EventQuery{Kind: "alert", Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		var byAlice []string
		for _, ev := range slices.Backward(events) {
			if ev.Actor == alice {
				byAlice = append(byAlice, ev.Message)
			}
		}
		want := []string{
			"Acknowledged: Disk 90% full on laptop",
			"Unacknowledged: Disk 90% full on laptop",
			"Snoozed for 4h: Disk 90% full on laptop",
			"Snoozed for 30d: Disk 90% full on laptop",
			"Woke: Disk 90% full on laptop",
			"Wrote a note on: Disk 90% full on laptop",
			"Wrote a note on: Disk 90% full on laptop",
		}
		if !slices.Equal(byAlice, want) {
			t.Errorf("alice's events = %q, want %q", byAlice, want)
		}
	})
}

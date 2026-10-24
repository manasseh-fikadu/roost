package store_test

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

func TestStore_Events(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()
	at := time.UnixMilli(1_800_000_000_000)
	if id, err := s.LastEventID(ctx); err != nil || id != 0 {
		t.Fatalf("LastEventID() of an empty log = %d, %v, want 0", id, err)
	}
	err := s.AddEvents(ctx,
		store.Event{At: at, Level: "info", Kind: "device", Message: "laptop joined the tailnet", DeviceID: "laptop"},
		store.Event{At: at, Level: "warning", Kind: "device", Message: "phone went offline", DeviceID: "phone"},
		store.Event{At: at, Level: "info", Kind: "access", Message: "Made bob a viewer", Actor: "alice@example.com"},
		store.Event{At: at, Level: "success", Kind: "device", Message: "phone came online", DeviceID: "phone"},
	)
	if err != nil {
		t.Fatalf("AddEvents() error = %v", err)
	}
	if err := s.AddEvents(ctx); err != nil {
		t.Errorf("AddEvents() of nothing: error = %v", err)
	}
	last, err := s.LastEventID(ctx)
	if err != nil || last == 0 {
		t.Fatalf("LastEventID() = %d, %v, want the last event's", last, err)
	}

	messages := func(q store.EventQuery) []string {
		t.Helper()
		events, err := s.Events(ctx, q)
		if err != nil {
			t.Fatalf("Events(%+v) error = %v", q, err)
		}
		var out []string
		for _, e := range events {
			out = append(out, e.Message)
		}
		return out
	}
	for _, tt := range []struct {
		name string
		q    store.EventQuery
		want []string
	}{
		{"the latest", store.EventQuery{Limit: 2}, []string{"phone came online", "Made bob a viewer"}},
		{"before", store.EventQuery{Before: last - 1, Limit: 10}, []string{"phone went offline", "laptop joined the tailnet"}},
		{"after", store.EventQuery{After: last - 2, Limit: 10}, []string{"phone came online", "Made bob a viewer"}},
		{"of a kind", store.EventQuery{Kind: "access", Limit: 10}, []string{"Made bob a viewer"}},
		{"about a device", store.EventQuery{DeviceID: "phone", Limit: 10}, []string{"phone came online", "phone went offline"}},
	} {
		if got := messages(tt.q); !slices.Equal(got, tt.want) {
			t.Errorf("%s: Events() = %q, want %q", tt.name, got, tt.want)
		}
	}
	events, err := s.Events(ctx, store.EventQuery{Kind: "access", Limit: 1})
	if err != nil || len(events) != 1 || events[0].Actor != "alice@example.com" || events[0].DeviceID != "" || !events[0].At.Equal(at) {
		t.Errorf("Events() = %+v, %v, want the event with its actor and time", events, err)
	}
}

func TestStore_KV(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	if _, ok, err := s.KV(t.Context(), "settings"); ok || err != nil {
		t.Errorf("KV() of a missing key = %v, %v, want nothing", ok, err)
	}
	for _, v := range []string{`{"a":1}`, `{"a":2}`} {
		if err := s.SetKV(t.Context(), "settings", v); err != nil {
			t.Fatal(err)
		}
	}
	if v, ok, err := s.KV(t.Context(), "settings"); v != `{"a":2}` || !ok || err != nil {
		t.Errorf("KV() = %q, %v, %v, want the last value", v, ok, err)
	}
}

func TestStore_alerts(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()
	at := time.UnixMilli(1_800_000_000_000)
	disk, err := s.AddAlert(ctx, store.Alert{
		Key: "disk:hub", Source: "metrics", DeviceID: "roost", Severity: "warning",
		Title: "Disk 86% full on roost", Detail: "12 GB left", OpenedAt: at, LastSeen: at,
	})
	if err != nil {
		t.Fatalf("AddAlert() error = %v", err)
	}
	check, err := s.AddAlert(ctx, store.Alert{
		Key: "check:abc", Source: "check", CheckID: "abc", Severity: "critical", Title: "API is failing", OpenedAt: at, LastSeen: at,
	})
	if err != nil {
		t.Fatalf("AddAlert() error = %v", err)
	}
	// A condition has at most one open alert.
	if _, err := s.AddAlert(ctx, store.Alert{Key: "disk:hub", Source: "metrics", OpenedAt: at, LastSeen: at}); err == nil {
		t.Error("AddAlert() of a second open alert for a condition didn't fail")
	}

	disk.Severity, disk.Title, disk.LastSeen = "critical", "Disk 96% full on roost", at.Add(time.Minute)
	disk.AckedAt, disk.AckedBy = at.Add(2*time.Minute), "alice@example.com"
	disk.SnoozedUntil, disk.SnoozedBy = at.Add(time.Hour), "bob@example.com"
	if err := s.SaveAlert(ctx, disk); err != nil {
		t.Fatalf("SaveAlert() error = %v", err)
	}
	for _, text := range []string{"Cleaning up logs", "Done"} {
		if _, err := s.AddAlertNote(ctx, disk.ID, store.AlertNote{At: at, Author: "alice@example.com", Text: text}); err != nil {
			t.Fatalf("AddAlertNote() error = %v", err)
		}
	}
	if _, err := s.AddAlertNote(ctx, 999, store.AlertNote{At: at, Text: "x"}); !errors.Is(err, store.ErrNoAlert) {
		t.Errorf("AddAlertNote() of a missing alert: error = %v, want ErrNoAlert", err)
	}
	if err := s.SaveAlert(ctx, store.Alert{ID: 999}); !errors.Is(err, store.ErrNoAlert) {
		t.Errorf("SaveAlert() of a missing alert: error = %v, want ErrNoAlert", err)
	}

	open, err := s.OpenAlerts(ctx)
	if err != nil || len(open) != 2 {
		t.Fatalf("OpenAlerts() = %+v, %v, want both alerts", open, err)
	}
	got := open[0]
	if got.ID != disk.ID || got.Severity != "critical" || got.Title != "Disk 96% full on roost" || got.Detail != "12 GB left" ||
		!got.LastSeen.Equal(disk.LastSeen) || !got.AckedAt.Equal(disk.AckedAt) || got.AckedBy != "alice@example.com" ||
		!got.SnoozedUntil.Equal(disk.SnoozedUntil) || got.SnoozedBy != "bob@example.com" || !got.ResolvedAt.IsZero() ||
		got.DeviceID != "roost" || got.CheckID != "" {
		t.Errorf("OpenAlerts()[0] = %+v, want the saved disk alert", got)
	}
	if len(got.Notes) != 2 || got.Notes[0].Text != "Cleaning up logs" || got.Notes[1].Author != "alice@example.com" {
		t.Errorf("the disk alert's notes = %+v, want both, oldest first", got.Notes)
	}
	if open[1].CheckID != "abc" || open[1].Notes != nil {
		t.Errorf("OpenAlerts()[1] = %+v, want the check's alert, without notes", open[1])
	}

	// Once it's resolved, the condition can open another.
	disk.ResolvedAt = at.Add(3 * time.Minute)
	if err := s.SaveAlert(ctx, disk); err != nil {
		t.Fatal(err)
	}
	again, err := s.AddAlert(ctx, store.Alert{Key: "disk:hub", Source: "metrics", Severity: "warning", OpenedAt: at, LastSeen: at})
	if err != nil {
		t.Fatalf("AddAlert() after the first resolved: error = %v", err)
	}
	check.ResolvedAt = at.Add(4 * time.Minute)
	if err := s.SaveAlert(ctx, check); err != nil {
		t.Fatal(err)
	}
	resolved, err := s.ResolvedAlerts(ctx, 0, 10)
	if err != nil || len(resolved) != 2 || resolved[0].ID != check.ID || resolved[1].ID != disk.ID || len(resolved[1].Notes) != 2 {
		t.Errorf("ResolvedAlerts() = %+v, %v, want the check's then the disk's, with notes", resolved, err)
	}
	if page, err := s.ResolvedAlerts(ctx, check.ID, 10); err != nil || len(page) != 1 || page[0].ID != disk.ID {
		t.Errorf("ResolvedAlerts(before the check's) = %+v, %v, want the disk's", page, err)
	}
	if open, err := s.OpenAlerts(ctx); err != nil || len(open) != 1 || open[0].ID != again.ID {
		t.Errorf("OpenAlerts() = %+v, %v, want only the new disk alert", open, err)
	}
}

func TestStore_prunesResolvedAlerts(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const month = 30 * 24 * time.Hour
		s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
		synctest.Wait()
		now := time.Now()
		for _, a := range []store.Alert{
			{Key: "old", ResolvedAt: now.Add(-month - time.Minute)},
			{Key: "recent", ResolvedAt: now.Add(-month + 2*time.Hour)},
			{Key: "open"},
		} {
			a.Source, a.Severity, a.OpenedAt, a.LastSeen = "metrics", "warning", now.Add(-2*month), now
			added, err := s.AddAlert(t.Context(), a)
			if err != nil {
				t.Fatal(err)
			}
			if !a.ResolvedAt.IsZero() {
				added.ResolvedAt = a.ResolvedAt
				if err := s.SaveAlert(t.Context(), added); err != nil {
					t.Fatal(err)
				}
			}
		}
		if _, err := s.AddAlertNote(t.Context(), 1, store.AlertNote{At: now, Text: "goes with its alert"}); err != nil {
			t.Fatal(err)
		}

		time.Sleep(store.PruneInterval)
		synctest.Wait()
		if got := firstColumn(t, s.DB(), "SELECT key FROM alerts ORDER BY id"); !slices.Equal(got, []string{"recent", "open"}) {
			t.Errorf("alerts after an hour = %q, want the recent and the open one", got)
		}
		if got := firstColumn(t, s.DB(), "SELECT COUNT(*) FROM alert_notes"); !slices.Equal(got, []string{"0"}) {
			t.Errorf("notes after an hour = %q, want the old alert's gone", got)
		}
	})
}

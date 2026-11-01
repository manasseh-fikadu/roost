package store_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
)

func TestStore_Channels(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "bield.db"), nil)
	ctx := t.Context()
	at := time.UnixMilli(1_800_000_000_000)

	if got, err := s.Channels(ctx); err != nil || len(got) != 0 {
		t.Fatalf("Channels() of a new hub = %+v, %v, want none", got, err)
	}
	phone, err := s.AddChannel(ctx, store.Channel{
		Name: "Phone", Kind: "ntfy", Enabled: true, MinSeverity: "warning", OnResolve: true,
		Config: `{"topic":"bield"}`, Created: at,
	})
	if err != nil || phone.ID == 0 {
		t.Fatalf("AddChannel() = %+v, %v, want a channel with an ID", phone, err)
	}
	team, err := s.AddChannel(ctx, store.Channel{Name: "team chat", Kind: "slack", MinSeverity: "critical", Config: `{}`, Created: at})
	if err != nil {
		t.Fatalf("AddChannel() error = %v", err)
	}
	for _, name := range []string{"Phone", "PHONE", "Team Chat"} {
		_, err := s.AddChannel(ctx, store.Channel{Name: name, Kind: "ntfy", MinSeverity: "info", Config: `{}`, Created: at})
		if !errors.Is(err, store.ErrChannelNameTaken) {
			t.Errorf("AddChannel(%q) of a name that's taken: error = %v, want ErrChannelNameTaken", name, err)
		}
	}

	got, err := s.Channels(ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("Channels() = %+v, %v, want both, by name", got, err)
	}
	if want := phone; got[0].ID != want.ID || got[0].Name != "Phone" || got[0].Kind != "ntfy" || !got[0].Enabled ||
		got[0].MinSeverity != "warning" || !got[0].OnResolve || got[0].Config != `{"topic":"bield"}` || !got[0].Created.Equal(at) {
		t.Errorf("Channels()[0] = %+v, want %+v", got[0], want)
	}
	if got[1].ID != team.ID || got[1].Enabled || got[1].OnResolve {
		t.Errorf("Channels()[1] = %+v, want the disabled team chat", got[1])
	}

	phone.Name, phone.Enabled, phone.Config = "My phone", false, `{"topic":"other"}`
	if err := s.UpdateChannel(ctx, phone); err != nil {
		t.Fatalf("UpdateChannel() error = %v", err)
	}
	// A channel can keep its own name, and change its case.
	team.Name = "TEAM CHAT"
	if err := s.UpdateChannel(ctx, team); err != nil {
		t.Errorf("UpdateChannel() with the channel's own name in another case: error = %v", err)
	}
	team.Name = "my PHONE"
	if err := s.UpdateChannel(ctx, team); !errors.Is(err, store.ErrChannelNameTaken) {
		t.Errorf("UpdateChannel() to a name that's taken: error = %v, want ErrChannelNameTaken", err)
	}
	if err := s.UpdateChannel(ctx, store.Channel{ID: 999, Name: "x", Kind: "ntfy", MinSeverity: "info", Config: `{}`}); !errors.Is(err, store.ErrNoChannel) {
		t.Errorf("UpdateChannel() of a missing channel: error = %v, want ErrNoChannel", err)
	}
	got, _ = s.Channels(ctx)
	if got[0].Name != "My phone" || got[0].Enabled || got[0].Config != `{"topic":"other"}` || !got[0].Created.Equal(at) || got[1].Name != "TEAM CHAT" {
		t.Errorf("Channels() after the changes = %+v", got)
	}

	if err := s.DeleteChannel(ctx, phone.ID); err != nil {
		t.Fatalf("DeleteChannel() error = %v", err)
	}
	if err := s.DeleteChannel(ctx, phone.ID); !errors.Is(err, store.ErrNoChannel) {
		t.Errorf("DeleteChannel() twice: error = %v, want ErrNoChannel", err)
	}
	if got, _ := s.Channels(ctx); len(got) != 1 {
		t.Errorf("Channels() after a delete = %+v, want one", got)
	}
}

func TestStore_Notified(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "bield.db"), nil)
	ctx := t.Context()
	at := time.UnixMilli(1_800_000_000_000)
	alert, err := s.AddAlert(ctx, store.Alert{Key: "disk:hub", Source: "metrics", Severity: "warning", Title: "Disk", OpenedAt: at, LastSeen: at})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.AddAlert(ctx, store.Alert{Key: "cpu:hub", Source: "metrics", Severity: "warning", Title: "CPU", OpenedAt: at, LastSeen: at})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.AddChannel(ctx, store.Channel{Name: "Phone", Kind: "ntfy", MinSeverity: "info", Config: `{}`, Created: at})
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range []store.Notified{
		{AlertID: alert.ID, ChannelID: ch.ID, Severity: "warning", At: at},
		// It tells again, with the severity and time it has by then.
		{AlertID: alert.ID, ChannelID: ch.ID, Severity: "critical", At: at.Add(time.Hour)},
		{AlertID: other.ID, ChannelID: ch.ID, Severity: "warning", At: at},
		// An alert or a channel that's gone is ignored.
		{AlertID: 999, ChannelID: ch.ID, Severity: "warning", At: at},
		{AlertID: alert.ID, ChannelID: 999, Severity: "warning", At: at},
	} {
		if err := s.SetNotified(ctx, n); err != nil {
			t.Fatalf("SetNotified(%+v) error = %v", n, err)
		}
	}
	got, err := s.Notified(ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("Notified() = %+v, %v, want two", got, err)
	}
	byAlert := map[int64]store.Notified{}
	for _, n := range got {
		byAlert[n.AlertID] = n
	}
	if n := byAlert[alert.ID]; n.ChannelID != ch.ID || n.Severity != "critical" || !n.At.Equal(at.Add(time.Hour)) {
		t.Errorf("Notified() of the alert = %+v, want the second message's", n)
	}

	if err := s.ClearNotified(ctx, alert.ID, ch.ID); err != nil {
		t.Fatalf("ClearNotified() error = %v", err)
	}
	if got, _ := s.Notified(ctx); len(got) != 1 || got[0].AlertID != other.ID {
		t.Errorf("Notified() after ClearNotified = %+v, want the other alert's", got)
	}

	// Deleting the channel forgets what it was told.
	if err := s.DeleteChannel(ctx, ch.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Notified(ctx); len(got) != 0 {
		t.Errorf("Notified() after deleting the channel = %+v, want none", got)
	}
}

package store_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

func TestStore_SSHAccess(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()
	created := time.UnixMilli(1_700_000_000_000)
	laptop := store.SSHAccess{DeviceID: "laptop", User: "alice", Port: 22, CreatedAt: created, CreatedBy: "alice@example.com"}
	hub := store.SSHAccess{DeviceID: store.HubID, User: "roost", Port: 2222, CreatedAt: created}
	for _, a := range []store.SSHAccess{laptop, hub} {
		if err := s.SetSSHAccess(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	get := func() map[string]store.SSHAccess {
		t.Helper()
		got, err := s.SSHAccess(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := get(); len(got) != 2 || got["laptop"] != laptop || got[store.HubID] != hub {
		t.Fatalf("SSHAccess = %+v, want the laptop's and the hub's", got)
	}

	// The first key pins, and the next doesn't replace it.
	pinnedAt := created.Add(time.Minute)
	for i, tt := range []struct {
		key  string
		want bool
	}{{"ssh-ed25519 AAAAfirst", true}, {"ssh-ed25519 AAAAsecond", false}} {
		pinned, err := s.PinHostKey(ctx, "laptop", tt.key, pinnedAt.Add(time.Duration(i)*time.Minute))
		if err != nil || pinned != tt.want {
			t.Errorf("PinHostKey(%s) = %v, %v, want %v", tt.key, pinned, err, tt.want)
		}
	}
	laptop.HostKey, laptop.PinnedAt = "ssh-ed25519 AAAAfirst", pinnedAt
	if got := get()["laptop"]; got != laptop {
		t.Errorf("after pinning, %+v, want %+v", got, laptop)
	}
	if _, err := s.PinHostKey(ctx, "phone", "ssh-ed25519 AAAA", pinnedAt); !errors.Is(err, store.ErrNoSSHAccess) {
		t.Errorf("PinHostKey on a device without access = %v, want ErrNoSSHAccess", err)
	}

	if err := s.ResetHostKey(ctx, "laptop"); err != nil {
		t.Fatal(err)
	}
	laptop.HostKey, laptop.PinnedAt = "", time.Time{}
	if got := get()["laptop"]; got != laptop {
		t.Errorf("after the reset, %+v, want %+v", got, laptop)
	}

	if err := s.DeleteSSHAccess(ctx, "laptop"); err != nil {
		t.Fatal(err)
	}
	if got := get(); len(got) != 1 {
		t.Errorf("after the delete, %+v, want only the hub's", got)
	}
	for name, err := range map[string]error{
		"ResetHostKey":    s.ResetHostKey(ctx, "laptop"),
		"DeleteSSHAccess": s.DeleteSSHAccess(ctx, "laptop"),
	} {
		if !errors.Is(err, store.ErrNoSSHAccess) {
			t.Errorf("%s on a device without access = %v, want ErrNoSSHAccess", name, err)
		}
	}
}

package store_test

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestStore_admins(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()

	if added, err := s.AddAdmin(ctx, " alice@example.com "); err != nil || !added {
		t.Fatalf("AddAdmin(alice) = %v, %v; want true", added, err)
	}
	if added, err := s.AddAdmin(ctx, "Alice@Example.com"); err != nil || added {
		t.Errorf("AddAdmin(Alice) again = %v, %v; want false, since logins ignore case", added, err)
	}
	if _, err := s.AddAdmin(ctx, "bob@github"); err != nil {
		t.Fatal(err)
	}

	got, err := s.Admins(ctx)
	if want := []string{"alice@example.com", "bob@github"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("Admins() = %q, %v; want %q", got, err, want)
	}
	for login, want := range map[string]bool{
		"alice@example.com": true,
		"ALICE@EXAMPLE.COM": true,
		"bob@github":        true,
		"carol@example.com": false,
		"alice":             false,
		"":                  false,
	} {
		if is, err := s.IsAdmin(ctx, login); err != nil || is != want {
			t.Errorf("IsAdmin(%q) = %v, %v; want %v", login, is, err, want)
		}
	}
}

func TestStore_AddAdmin_badLogin(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	for _, login := range []string{"", "alice", "@example.com", "alice@", "alice @example.com"} {
		if _, err := s.AddAdmin(t.Context(), login); err == nil {
			t.Errorf("AddAdmin(%q) succeeded, want an error", login)
		}
	}
	if got, err := s.Admins(t.Context()); err != nil || len(got) != 0 {
		t.Errorf("Admins() = %q, %v; want none", got, err)
	}
}

func TestStore_AddFirstAdmins(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()

	if added, err := s.AddFirstAdmins(ctx, nil); err != nil || added {
		t.Errorf("AddFirstAdmins(nil) = %v, %v; want false", added, err)
	}
	if _, err := s.AddFirstAdmins(ctx, []string{"alice@example.com", "nobody"}); err == nil {
		t.Error("AddFirstAdmins() with a bad login succeeded")
	}
	if added, err := s.AddFirstAdmins(ctx, []string{"alice@example.com", "bob@github"}); err != nil || !added {
		t.Fatalf("AddFirstAdmins() on no admins = %v, %v; want true", added, err)
	}
	// Once there are admins, it does nothing.
	if added, err := s.AddFirstAdmins(ctx, []string{"mallory@example.com"}); err != nil || added {
		t.Errorf("AddFirstAdmins() with admins = %v, %v; want false", added, err)
	}

	got, err := s.Admins(ctx)
	if want := []string{"alice@example.com", "bob@github"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("Admins() = %q, %v; want %q", got, err, want)
	}
}

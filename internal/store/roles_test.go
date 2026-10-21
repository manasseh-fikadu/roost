package store_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/manasseh-fikadu/roost/internal/store"
)

// users returns each user as "login role".
func users(t *testing.T, s *store.Store) []string {
	t.Helper()
	us, err := s.Users(t.Context())
	if err != nil {
		t.Fatalf("Users() error = %v", err)
	}
	var got []string
	for _, u := range us {
		if u.Since.IsZero() {
			t.Errorf("user %s has no Since", u.Login)
		}
		got = append(got, u.Login+" "+string(u.Role))
	}
	return got
}

func setRole(t *testing.T, s *store.Store, login string, role store.Role) {
	t.Helper()
	if _, err := s.SetRole(t.Context(), login, role); err != nil {
		t.Fatalf("SetRole(%s, %s) error = %v", login, role, err)
	}
}

func TestStore_SetRole(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()

	steps := []struct {
		login string
		role  store.Role
		want  bool // changed
	}{
		{" alice@example.com ", store.Admin, true},
		{"Alice@Example.com", store.Admin, false}, // logins ignore case
		{"bob@github", store.Viewer, true},
		{"bob@github", store.Viewer, false},
		{"bob@github", store.Admin, true},
		{"BOB@github", store.Viewer, true},
	}
	for _, st := range steps {
		if changed, err := s.SetRole(ctx, st.login, st.role); err != nil || changed != st.want {
			t.Errorf("SetRole(%q, %s) = %v, %v; want %v", st.login, st.role, changed, err, st.want)
		}
	}
	if got, want := users(t, s), []string{"alice@example.com admin", "bob@github viewer"}; !slices.Equal(got, want) {
		t.Errorf("Users() = %q, want %q", got, want)
	}

	if _, err := s.SetRole(ctx, "carol@example.com", "owner"); err == nil {
		t.Error("SetRole() with an unknown role succeeded")
	}
	for _, login := range []string{"", "alice", "@example.com", "alice@", "alice @example.com"} {
		if _, err := s.SetRole(ctx, login, store.Viewer); err == nil {
			t.Errorf("SetRole(%q) succeeded, want an error", login)
		}
		if _, err := s.RemoveUser(ctx, login); err == nil {
			t.Errorf("RemoveUser(%q) succeeded, want an error", login)
		}
	}
}

func TestStore_RemoveUser(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()
	setRole(t, s, "alice@example.com", store.Admin)
	setRole(t, s, "bob@example.com", store.Viewer)

	if removed, err := s.RemoveUser(ctx, "BOB@example.com"); err != nil || !removed {
		t.Errorf("RemoveUser(bob) = %v, %v; want true", removed, err)
	}
	if removed, err := s.RemoveUser(ctx, "bob@example.com"); err != nil || removed {
		t.Errorf("RemoveUser(bob) again = %v, %v; want false", removed, err)
	}
	if got, want := users(t, s), []string{"alice@example.com admin"}; !slices.Equal(got, want) {
		t.Errorf("Users() = %q, want %q", got, want)
	}
}

// Roost always keeps an admin once it has one, so it's never locked.
func TestStore_lastAdmin(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()
	setRole(t, s, "alice@example.com", store.Admin)
	setRole(t, s, "bob@example.com", store.Viewer)

	if _, err := s.SetRole(ctx, "alice@example.com", store.Viewer); !errors.Is(err, store.ErrLastAdmin) {
		t.Errorf("SetRole(alice, viewer) error = %v, want ErrLastAdmin", err)
	}
	if _, err := s.RemoveUser(ctx, "Alice@example.com"); !errors.Is(err, store.ErrLastAdmin) {
		t.Errorf("RemoveUser(alice) error = %v, want ErrLastAdmin", err)
	}
	if got, want := users(t, s), []string{"alice@example.com admin", "bob@example.com viewer"}; !slices.Equal(got, want) {
		t.Errorf("Users() = %q, want %q", got, want)
	}

	// With a second admin, either one can go.
	setRole(t, s, "bob@example.com", store.Admin)
	setRole(t, s, "alice@example.com", store.Viewer)
	if _, err := s.RemoveUser(ctx, "alice@example.com"); err != nil {
		t.Errorf("RemoveUser(alice) with bob an admin: %v", err)
	}
	if _, err := s.RemoveUser(ctx, "bob@example.com"); !errors.Is(err, store.ErrLastAdmin) {
		t.Errorf("RemoveUser(bob) error = %v, want ErrLastAdmin", err)
	}
}

func TestStore_Role(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()
	setRole(t, s, "alice@example.com", store.Admin)
	setRole(t, s, "bob@example.com", store.Viewer)

	tests := []struct {
		login             string
		tailnet, onlyList store.Role
	}{
		{"alice@example.com", store.Admin, store.Admin},
		{"ALICE@EXAMPLE.COM", store.Admin, store.Admin},
		{"bob@example.com", store.Viewer, store.Viewer},
		{"carol@example.com", store.Viewer, ""},
		{"", "", ""},
	}
	check := func(who string) {
		t.Helper()
		for _, tt := range tests {
			want := tt.tailnet
			if who == store.Listed {
				want = tt.onlyList
			}
			if got, err := s.Role(ctx, tt.login); err != nil || got != want {
				t.Errorf("with %s: Role(%q) = %q, %v; want %q", who, tt.login, got, err, want)
			}
		}
	}

	// Roost starts closed.
	if who, err := s.WhoCanView(ctx); err != nil || who != store.Listed {
		t.Errorf("WhoCanView() on a new database = %q, %v; want %q", who, err, store.Listed)
	}
	check(store.Listed)
	steps := []struct {
		who  string
		want bool // changed
	}{{store.Listed, false}, {store.Tailnet, true}, {store.Tailnet, false}}
	for _, step := range steps {
		if changed, err := s.SetWhoCanView(ctx, step.who); err != nil || changed != step.want {
			t.Errorf("SetWhoCanView(%q) = %v, %v; want %v", step.who, changed, err, step.want)
		}
	}
	check(store.Tailnet)
	if changed, err := s.SetWhoCanView(ctx, store.Listed); err != nil || !changed {
		t.Errorf("SetWhoCanView(listed) = %v, %v; want true", changed, err)
	}
	check(store.Listed)
	if _, err := s.SetWhoCanView(ctx, "everyone"); err == nil {
		t.Error("SetWhoCanView(everyone) succeeded")
	}

	// A value Roost doesn't know, even an empty one, lets no one else in,
	// and WhoCanView says so.
	for _, bad := range []string{"Tailnet", ""} {
		if _, err := s.DB().ExecContext(ctx, "UPDATE kv SET value = ? WHERE key = 'who_can_view'", bad); err != nil {
			t.Fatal(err)
		}
		if who, err := s.WhoCanView(ctx); err != nil || who != store.Listed {
			t.Errorf("WhoCanView() with %q stored = %q, %v; want %q", bad, who, err, store.Listed)
		}
		check(store.Listed)
	}
}

func TestStore_Access(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	ctx := t.Context()
	setRole(t, s, "bob@example.com", store.Viewer)
	setRole(t, s, "alice@example.com", store.Admin)
	if _, err := s.SetWhoCanView(ctx, store.Listed); err != nil {
		t.Fatal(err)
	}

	who, us, err := s.Access(ctx)
	if err != nil {
		t.Fatalf("Access() error = %v", err)
	}
	var got []string
	for _, u := range us {
		got = append(got, u.Login+" "+string(u.Role))
	}
	if want := []string{"alice@example.com admin", "bob@example.com viewer"}; who != store.Listed || !slices.Equal(got, want) {
		t.Errorf("Access() = %q, %q; want %q, %q", who, got, store.Listed, want)
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
	// Viewers don't count, and one can become an admin.
	setRole(t, s, "bob@github", store.Viewer)
	setRole(t, s, "carol@example.com", store.Viewer)
	if added, err := s.AddFirstAdmins(ctx, []string{"alice@example.com", "bob@github"}); err != nil || !added {
		t.Fatalf("AddFirstAdmins() on no admins = %v, %v; want true", added, err)
	}
	// Once there are admins, it does nothing.
	if added, err := s.AddFirstAdmins(ctx, []string{"mallory@example.com"}); err != nil || added {
		t.Errorf("AddFirstAdmins() with admins = %v, %v; want false", added, err)
	}

	want := []string{"alice@example.com admin", "bob@github admin", "carol@example.com viewer"}
	if got := users(t, s); !slices.Equal(got, want) {
		t.Errorf("Users() = %q, want %q", got, want)
	}
}

// Version 3 turned the admins table into roles.
func TestOpen_adminsBecomeRoles(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "roost.db")
	v2 := fstest.MapFS{}
	for _, name := range []string{"0001_kv_and_events.sql", "0002_admins.sql"} {
		data, err := fs.ReadFile(store.Migrations(), name)
		if err != nil {
			t.Fatal(err)
		}
		v2[name] = &fstest.MapFile{Data: data}
	}
	old := openStore(t, path, v2)
	if _, err := old.DB().ExecContext(t.Context(),
		"INSERT INTO admins (login, added_at) VALUES ('alice@example.com', 1), ('bob@github', 2)"); err != nil {
		t.Fatal(err)
	}
	closeStore(t, old)

	s := openStore(t, path, nil)

	got, err := s.Users(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []store.User{
		{Login: "alice@example.com", Role: store.Admin},
		{Login: "bob@github", Role: store.Admin},
	}
	if len(got) != len(want) {
		t.Fatalf("Users() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Login != want[i].Login || got[i].Role != want[i].Role || got[i].Since.UnixMilli() != int64(i+1) {
			t.Errorf("user %d = %+v, want %+v since %dms", i, got[i], want[i], i+1)
		}
	}
}

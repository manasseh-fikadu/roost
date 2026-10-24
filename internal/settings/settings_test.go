package settings_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/settings"
	"github.com/manasseh-fikadu/roost/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "roost.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestLoadAndSave(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	got, err := settings.Load(t.Context(), st)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if want := settings.Defaults(); got.Thresholds != want.Thresholds || !slices.Equal(got.WatchedUnits, want.WatchedUnits) {
		t.Errorf("Load() of a new store = %+v, want the defaults", got)
	}

	got.Thresholds.DiskWarn, got.WatchedUnits = 80, []string{"nginx.service", "user:app.service"}
	if err := settings.Save(t.Context(), st, got); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	again, err := settings.Load(t.Context(), st)
	if err != nil || again.Thresholds != got.Thresholds || !slices.Equal(again.WatchedUnits, got.WatchedUnits) {
		t.Errorf("Load() after Save() = %+v, %v, want %+v", again, err, got)
	}

	// A setting that wasn't saved, such as one a later version adds, gets
	// its default.
	if err := st.SetKV(t.Context(), "settings", `{"thresholds":{"diskWarn":70}}`); err != nil {
		t.Fatal(err)
	}
	partial, err := settings.Load(t.Context(), st)
	if err != nil || partial.Thresholds.DiskWarn != 70 || partial.Thresholds.MemoryWarn != 92 || len(partial.WatchedUnits) == 0 {
		t.Errorf("Load() of partial settings = %+v, %v, want the defaults for the rest", partial, err)
	}
	if err := st.SetKV(t.Context(), "settings", `{`); err != nil {
		t.Fatal(err)
	}
	if bad, err := settings.Load(t.Context(), st); err == nil || bad.Thresholds != settings.Defaults().Thresholds {
		t.Errorf("Load() of broken settings = %+v, %v, want the defaults and an error", bad, err)
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		change  func(s *settings.Settings)
		wantErr bool
	}{
		{"defaults", func(*settings.Settings) {}, false},
		{"no disk threshold", func(s *settings.Settings) { s.Thresholds.DiskWarn = 0 }, true},
		{"memory over 100%", func(s *settings.Settings) { s.Thresholds.MemoryWarn = 101 }, true},
		{"critical before warning", func(s *settings.Settings) { s.Thresholds.DiskCritical = 80 }, true},
		{"offline for no time", func(s *settings.Settings) { s.Thresholds.OfflineMinutes = 0 }, true},
		{"no key warning", func(s *settings.Settings) { s.Thresholds.KeyExpiryDays = 0 }, true},
		{"no failures", func(s *settings.Settings) { s.Thresholds.CheckFailures = 0 }, true},
		{"a user unit", func(s *settings.Settings) { s.WatchedUnits = []string{"user:app.service"} }, false},
		{"a unit with a space", func(s *settings.Settings) { s.WatchedUnits = []string{"my app"} }, true},
		{"a flag for a unit", func(s *settings.Settings) { s.WatchedUnits = []string{"--all"} }, true},
		{"an empty user unit", func(s *settings.Settings) { s.WatchedUnits = []string{"user:"} }, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := settings.Defaults()
			tt.change(&s)
			if err := s.Check(); (err != nil) != tt.wantErr {
				t.Errorf("Check() error = %v, want an error: %v", err, tt.wantErr)
			}
		})
	}
}

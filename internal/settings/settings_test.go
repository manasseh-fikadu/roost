package settings_test

import (
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

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
	if want := settings.Defaults(); !reflect.DeepEqual(got, want) {
		t.Errorf("Load() of a new store = %+v, want the defaults", got)
	}

	got.General = settings.General{Name: "Home lab", PublicURL: "https://roost.example.com"}
	got.Thresholds.DiskWarn, got.Thresholds.CPUWindowMinutes = 80, 15
	got.Rules.CPU = false
	got.WatchedUnits = []string{"nginx.service", "user:app.service"}
	got.Retention.HistoryDays = 14
	if err := settings.Save(t.Context(), st, got); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	again, err := settings.Load(t.Context(), st)
	if err != nil || !reflect.DeepEqual(again, got) {
		t.Errorf("Load() after Save() = %+v, %v, want %+v", again, err, got)
	}
	if keep, err := settings.StoreRetention(t.Context(), st); err != nil || keep.History != 14*24*time.Hour || keep.Activity != 30*24*time.Hour {
		t.Errorf("StoreRetention() = %+v, %v, want 14 days of history and 30 of activity", keep, err)
	}

	// A setting that wasn't saved, such as one a later version adds, gets
	// its default.
	// This is how #26 saved them, before Roost had a Settings page.
	if err := st.SetKV(t.Context(), "settings", `{"thresholds":{"diskWarn":70}}`); err != nil {
		t.Fatal(err)
	}
	partial, err := settings.Load(t.Context(), st)
	want := settings.Defaults()
	want.Thresholds.DiskWarn = 70
	if err != nil || !reflect.DeepEqual(partial, want) {
		t.Errorf("Load() of partial settings = %+v, %v, want the defaults for the rest", partial, err)
	}
	if err := st.SetKV(t.Context(), "settings", `{`); err != nil {
		t.Fatal(err)
	}
	if bad, err := settings.Load(t.Context(), st); err == nil || !reflect.DeepEqual(bad, settings.Defaults()) {
		t.Errorf("Load() of broken settings = %+v, %v, want the defaults and an error", bad, err)
	}
	if keep, err := settings.StoreRetention(t.Context(), st); err == nil {
		t.Errorf("StoreRetention() of broken settings = %+v, want an error, so the store keeps everything", keep)
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
		{"not a number", func(s *settings.Settings) { s.Thresholds.CPUWarn = math.NaN() }, true},
		{"CPU for no time", func(s *settings.Settings) { s.Thresholds.CPUWindowMinutes = 0 }, true},
		{"CPU for a day", func(s *settings.Settings) { s.Thresholds.CPUWindowMinutes = 24 * 60 }, true},
		{"CPU for an hour", func(s *settings.Settings) { s.Thresholds.CPUWindowMinutes = 60 }, false},
		{"CPU for longer than a duration holds", func(s *settings.Settings) { s.Thresholds.CPUWindowMinutes = 3_000_000_000_000_000 }, true},
		{"offline for no time", func(s *settings.Settings) { s.Thresholds.OfflineMinutes = 0 }, true},
		{"no key warning", func(s *settings.Settings) { s.Thresholds.KeyExpiryDays = 0 }, true},
		{"no failures", func(s *settings.Settings) { s.Thresholds.CheckFailures = 0 }, true},
		{"a user unit", func(s *settings.Settings) { s.WatchedUnits = []string{"user:app.service"} }, false},
		{"a unit with a space", func(s *settings.Settings) { s.WatchedUnits = []string{"my app"} }, true},
		{"a unit with a character systemd refuses", func(s *settings.Settings) { s.WatchedUnits = []string{"a!b.service"} }, true},
		{"a unit template", func(s *settings.Settings) { s.WatchedUnits = []string{"getty@.service"} }, true},
		{"a unit instance", func(s *settings.Settings) { s.WatchedUnits = []string{"getty@tty1.service"} }, false},
		{"a unit with an escape", func(s *settings.Settings) { s.WatchedUnits = []string{`dev-disk-by\x2did.device`} }, false},
		{"a long unit name", func(s *settings.Settings) { s.WatchedUnits = []string{strings.Repeat("a", 256) + ".service"} }, true},
		{"a flag for a unit", func(s *settings.Settings) { s.WatchedUnits = []string{"--all"} }, true},
		{"an empty user unit", func(s *settings.Settings) { s.WatchedUnits = []string{"user:"} }, true},
		{"no units", func(s *settings.Settings) { s.WatchedUnits = nil }, false},
		{"no name", func(s *settings.Settings) { s.General.Name = "" }, true},
		{"a name with spaces around it", func(s *settings.Settings) { s.General.Name = " Roost" }, true},
		{"a two line name", func(s *settings.Settings) { s.General.Name = "Home\nlab" }, true},
		{"a long name", func(s *settings.Settings) { s.General.Name = strings.Repeat("é", settings.MaxName) }, false},
		{"a name that is too long", func(s *settings.Settings) { s.General.Name = strings.Repeat("é", settings.MaxName+1) }, true},
		{"a public URL", func(s *settings.Settings) { s.General.PublicURL = "https://roost.example.com:8443" }, false},
		{"a public URL with a path", func(s *settings.Settings) { s.General.PublicURL = "https://example.com/roost" }, true},
		{"a public URL with a slash", func(s *settings.Settings) { s.General.PublicURL = "https://example.com/" }, true},
		{"a public URL with a login", func(s *settings.Settings) { s.General.PublicURL = "https://alice:secret@example.com" }, true},
		{"a public URL without a scheme", func(s *settings.Settings) { s.General.PublicURL = "roost.example.com" }, true},
		{"a public URL with another scheme", func(s *settings.Settings) { s.General.PublicURL = "ftp://roost.example.com" }, true},
		{"a public URL with a query", func(s *settings.Settings) { s.General.PublicURL = "https://example.com?a=b" }, true},
		{"a public URL with an IPv6 address", func(s *settings.Settings) { s.General.PublicURL = "http://[fd7a:115c:a1e0::1]:4700" }, false},
		{"a public URL with an IP address", func(s *settings.Settings) { s.General.PublicURL = "http://100.64.0.1" }, false},
		{"a public URL with a shell word in it", func(s *settings.Settings) { s.General.PublicURL = "https://a$(id).example.com" }, true},
		{"a public URL with a semicolon", func(s *settings.Settings) { s.General.PublicURL = "https://a;reboot" }, true},
		{"a public URL with a quote", func(s *settings.Settings) { s.General.PublicURL = "https://a'b.example.com" }, true},
		{"a public URL with an invisible character", func(s *settings.Settings) { s.General.PublicURL = "https://a\u200b.example.com" }, true},
		{"a public URL with a port that is too big", func(s *settings.Settings) { s.General.PublicURL = "https://example.com:99999" }, true},
		{"a public URL with an empty port", func(s *settings.Settings) { s.General.PublicURL = "https://example.com:" }, true},
		{"a public URL with a zone", func(s *settings.Settings) { s.General.PublicURL = "http://[fe80::1%25eth0]" }, true},
		{"no activity log", func(s *settings.Settings) { s.Retention.ActivityDays = 0 }, true},
		{"a year of activity", func(s *settings.Settings) { s.Retention.ActivityDays = 365 }, false},
		{"over a year of activity", func(s *settings.Settings) { s.Retention.ActivityDays = 366 }, true},
		{"a long history", func(s *settings.Settings) { s.Retention.HistoryDays = settings.MaxHistoryDays + 1 }, true},
		{"long check results", func(s *settings.Settings) { s.Retention.ChecksDays = settings.MaxChecksDays + 1 }, true},
		{"daily reminders", func(s *settings.Settings) { s.Notifications.RemindHours = 24 }, false},
		{"a reminder every week", func(s *settings.Settings) { s.Notifications.RemindHours = settings.MaxRemindHours }, false},
		{"a reminder less often than that", func(s *settings.Settings) { s.Notifications.RemindHours = settings.MaxRemindHours + 1 }, true},
		{"a reminder in the past", func(s *settings.Settings) { s.Notifications.RemindHours = -1 }, true},
		{"quiet hours in a time zone", func(s *settings.Settings) {
			s.Notifications.Quiet.Enabled, s.Notifications.Quiet.Timezone = true, "Europe/Berlin"
		}, false},
		{"quiet hours in UTC", func(s *settings.Settings) { s.Notifications.Quiet.Timezone = "UTC" }, false},
		{"quiet hours in a time zone that doesn't exist", func(s *settings.Settings) { s.Notifications.Quiet.Timezone = "Mars/Olympus" }, true},
		{"quiet hours that start late", func(s *settings.Settings) { s.Notifications.Quiet.Start = "25:00" }, true},
		{"quiet hours without a minute", func(s *settings.Settings) { s.Notifications.Quiet.End = "7" }, true},
		{"quiet hours with a space", func(s *settings.Settings) { s.Notifications.Quiet.End = " 07:00" }, true},
		{"quiet hours that never end", func(s *settings.Settings) { s.Notifications.Quiet.Start, s.Notifications.Quiet.End = "07:00", "07:00" }, true},
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

func TestQuietHours_Active(t *testing.T) {
	t.Parallel()
	night := settings.QuietHours{Enabled: true, Start: "22:00", End: "07:00", Timezone: "UTC"}
	day := settings.QuietHours{Enabled: true, Start: "09:00", End: "17:30", Timezone: "UTC"}
	berlin := settings.QuietHours{Enabled: true, Start: "22:00", End: "07:00", Timezone: "Europe/Berlin"}
	at := func(s string) time.Time {
		t.Helper()
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tt := range []struct {
		name string
		q    settings.QuietHours
		at   time.Time
		want bool
	}{
		{"before the night", night, at("2026-11-01T21:59:00Z"), false},
		{"as the night starts", night, at("2026-11-01T22:00:00Z"), true},
		{"after midnight", night, at("2026-11-02T03:15:00Z"), true},
		{"just before it ends", night, at("2026-11-02T06:59:00Z"), true},
		{"as it ends", night, at("2026-11-02T07:00:00Z"), false},
		{"midday", night, at("2026-11-02T12:00:00Z"), false},
		{"a day's hours, inside", day, at("2026-11-02T17:29:00Z"), true},
		{"a day's hours, as they end", day, at("2026-11-02T17:30:00Z"), false},
		{"a day's hours, before", day, at("2026-11-02T08:59:00Z"), false},
		// 21:30 UTC is 22:30 in Berlin in winter and 23:30 in summer.
		{"another time zone in winter", berlin, at("2026-11-02T21:30:00Z"), true},
		{"another time zone, an hour earlier", berlin, at("2026-11-02T20:30:00Z"), false},
		{"another time zone in summer", berlin, at("2026-07-02T20:30:00Z"), true},
		{"another time zone in summer, just before", berlin, at("2026-07-02T19:30:00Z"), false},
		{"off", settings.QuietHours{Start: "22:00", End: "07:00", Timezone: "UTC"}, at("2026-11-01T23:00:00Z"), false},
		{"hours that can't be read", settings.QuietHours{Enabled: true, Start: "late", End: "07:00"}, at("2026-11-01T23:00:00Z"), false},
		{"hours that never end", settings.QuietHours{Enabled: true, Start: "07:00", End: "07:00"}, at("2026-11-01T07:00:00Z"), false},
	} {
		if got := tt.q.Active(tt.at); got != tt.want {
			t.Errorf("%s: Active(%s) = %v, want %v", tt.name, tt.at.Format(time.RFC3339), got, tt.want)
		}
	}
}

func TestCleanPublicURL(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"":                               "",
		"  https://roost.example.com/ ":  "https://roost.example.com",
		"http://100.64.0.1:4700":         "http://100.64.0.1:4700",
		"https://roost.example.com":      "https://roost.example.com",
		"https://example.com/roost":      "!",
		"https://example.com//":          "!",
		"mailto:alice@example.com":       "!",
		"https://exa mple.com":           "!",
		"https://example.com/\nhttp://x": "!",
	} {
		got, err := settings.CleanPublicURL(in)
		if want == "!" {
			if err == nil {
				t.Errorf("CleanPublicURL(%q) = %q, want an error", in, got)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("CleanPublicURL(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
}

func TestThresholds_For(t *testing.T) {
	t.Parallel()
	base := settings.Defaults().Thresholds // disk 85/95, memory 92, CPU 90, offline 10
	for _, tt := range []struct {
		name    string
		o       store.ThresholdOverrides
		want    func(t *settings.Thresholds)
		wantErr bool
	}{
		{"none", store.ThresholdOverrides{}, func(*settings.Thresholds) {}, false},
		{"a big disk", store.ThresholdOverrides{DiskWarn: new(97.0), DiskCritical: new(99.0)},
			func(t *settings.Thresholds) { t.DiskWarn, t.DiskCritical = 97, 99 }, false},
		{"everything", store.ThresholdOverrides{DiskWarn: new(70.0), DiskCritical: new(80.0), MemoryWarn: new(50.0), CPUWarn: new(100.0), OfflineMinutes: new(60)},
			func(t *settings.Thresholds) {
				t.DiskWarn, t.DiskCritical, t.MemoryWarn, t.CPUWarn, t.OfflineMinutes = 70, 80, 50, 100, 60
			}, false},
		// The critical level follows the warning level up, so the device
		// isn't critical before it needs a warning, but a pair of overrides
		// that's backwards is a mistake.
		{"a warning past the settings' critical", store.ThresholdOverrides{DiskWarn: new(97.0)},
			func(t *settings.Thresholds) { t.DiskWarn, t.DiskCritical = 97, 97 }, false},
		{"a backwards pair", store.ThresholdOverrides{DiskWarn: new(97.0), DiskCritical: new(96.0)}, nil, true},
		{"memory over 100%", store.ThresholdOverrides{MemoryWarn: new(101.0)}, nil, true},
		{"never offline", store.ThresholdOverrides{OfflineMinutes: new(0)}, nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := base.CheckOverrides(tt.o)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckOverrides() error = %v, want an error: %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			want := base
			tt.want(&want)
			if got := base.For(tt.o); got != want {
				t.Errorf("For() = %+v, want %+v", got, want)
			}
		})
	}
	if base.DiskWarn != 85 {
		t.Error("For() changed the thresholds it was called on")
	}
}

func TestChanges(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		change func(s *settings.Settings)
		want   []string
	}{
		{"nothing", func(*settings.Settings) {}, nil},
		{"a name", func(s *settings.Settings) { s.General.Name = "Home lab" }, []string{`Renamed Roost to "Home lab"`}},
		{"a public URL, whose value stays out", func(s *settings.Settings) { s.General.PublicURL = "https://secret.example.com" },
			[]string{"Changed the public URL"}},
		{"finishing the setup", func(s *settings.Settings) { s.General.SetupDone = true },
			[]string{"Finished the first-run setup"}},
		{"thresholds", func(s *settings.Settings) {
			s.Thresholds.DiskWarn, s.Thresholds.CPUWindowMinutes, s.Thresholds.CheckFailures = 80.5, 1, 3
		}, []string{"Changed the alert thresholds: disk warning 85% → 80.5%, CPU window 5 minutes → 1 minute, failing checks 2 failures → 3 failures"}},
		{"rules", func(s *settings.Settings) { s.Rules.CPU, s.Rules.Units = false, false },
			[]string{"Turned off alerts for CPU, systemd units"}},
		{"units", func(s *settings.Settings) {
			s.WatchedUnits = []string{"ssh.service", "nginx.service", "a.service", "b.service", "c.service", "d.service"}
		}, []string{
			"Changed the watched systemd units: started watching nginx.service, a.service, b.service, c.service and 1 more, " +
				"stopped watching sshd.service, docker.service, tailscaled.service, cron.service",
		}},
		{"retention", func(s *settings.Settings) { s.Retention.HistoryDays, s.Retention.ActivityDays = 1, 90 },
			[]string{"Changed how long Roost keeps history: activity log 30 days → 90 days, device history 7 days → 1 day"}},
		{"reminders", func(s *settings.Settings) { s.Notifications.RemindHours = 6 },
			[]string{"Set reminders about open alerts to every 6 hours"}},
		{"an hourly reminder", func(s *settings.Settings) { s.Notifications.RemindHours = 1 },
			[]string{"Set reminders about open alerts to every hour"}},
		{"quiet hours", func(s *settings.Settings) {
			s.Notifications.Quiet = settings.QuietHours{Enabled: true, Start: "23:00", End: "06:30", Timezone: "Europe/Berlin", AllowCritical: true}
		}, []string{"Set quiet hours to 23:00–06:30 Europe/Berlin, with critical alerts getting through"}},
		{"several parts", func(s *settings.Settings) { s.General.Name, s.Rules.Disk = "Lab", false },
			[]string{`Renamed Roost to "Lab"`, "Turned off alerts for disk"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			after := settings.Defaults()
			tt.change(&after)
			got := settings.Changes(settings.Defaults(), after)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Changes() = %q, want %q", got, tt.want)
			}
		})
	}
	off := settings.Defaults()
	off.Rules.CPU = false
	if got := settings.Changes(off, settings.Defaults()); !reflect.DeepEqual(got, []string{"Turned on alerts for CPU"}) {
		t.Errorf("Changes() of turning a rule on = %q", got)
	}
	done := settings.Defaults()
	done.General.SetupDone = true
	if got := settings.Changes(done, settings.Defaults()); !reflect.DeepEqual(got, []string{"Turned the first-run setup back on"}) {
		t.Errorf("Changes() of turning the setup back on = %q", got)
	}
}

func TestCache(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := openStore(t)
		c := settings.NewCache(st, nil)
		if got := c.Get(t.Context()); !reflect.DeepEqual(got, settings.Defaults()) {
			t.Errorf("Get() of a new store = %+v, want the defaults", got)
		}

		// A change through the cache is there at once.
		before, after, err := c.Update(t.Context(), func(s *settings.Settings) error {
			s.Thresholds.MemoryWarn = 80
			return nil
		})
		if err != nil || before.Thresholds.MemoryWarn != 92 || after.Thresholds.MemoryWarn != 80 {
			t.Fatalf("Update() = %v, %v, %v, want memory 92 → 80", before.Thresholds.MemoryWarn, after.Thresholds.MemoryWarn, err)
		}
		if got := c.Get(t.Context()).Thresholds.MemoryWarn; got != 80 {
			t.Errorf("Get() after Update() = memory %v, want 80", got)
		}

		// A change that fails Check, or that change itself refuses, saves nothing.
		bad := errors.New("no")
		for name, change := range map[string]func(*settings.Settings) error{
			"a failing check": func(s *settings.Settings) error { s.Thresholds.MemoryWarn = 0; return nil },
			"a refusal":       func(*settings.Settings) error { return bad },
		} {
			if _, _, err := c.Update(t.Context(), change); err == nil {
				t.Errorf("Update(%s) didn't fail", name)
			}
		}
		if got := c.Get(t.Context()).Thresholds.MemoryWarn; got != 80 {
			t.Errorf("Get() after failed updates = memory %v, want 80", got)
		}

		// A change made behind its back shows up once the cache is old.
		if err := st.SetKV(t.Context(), "settings", `{"thresholds":{"memoryWarn":70}}`); err != nil {
			t.Fatal(err)
		}
		if got := c.Get(t.Context()).Thresholds.MemoryWarn; got != 80 {
			t.Errorf("Get() right after an edit of the database = memory %v, want 80 still", got)
		}
		time.Sleep(3 * time.Second)
		if got := c.Get(t.Context()).Thresholds.MemoryWarn; got != 70 {
			t.Errorf("Get() 3 seconds after an edit of the database = memory %v, want 70", got)
		}

		// Whoever gets the settings can change them without changing the cache's.
		got := c.Get(t.Context())
		got.WatchedUnits[0] = "changed"
		if again := c.Get(t.Context()); again.WatchedUnits[0] == "changed" {
			t.Error("changing the settings Get() returned changed the cache's")
		}

		// A store that can't be read leaves the last settings.
		if err := st.SetKV(t.Context(), "settings", `{`); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Second)
		if got := c.Get(t.Context()).Thresholds.MemoryWarn; got != 70 {
			t.Errorf("Get() with broken settings in the database = memory %v, want the last, 70", got)
		}
	})
}

func TestCache_Update_corruptSettings(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	c := settings.NewCache(st, nil)
	if err := st.SetKV(t.Context(), "settings", `{"retention":{"activityDays":"x"}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Load(t.Context(), st); !errors.Is(err, settings.ErrCorrupt) {
		t.Fatalf("Load() of settings that can't be read = %v, want ErrCorrupt", err)
	}
	// An admin can write over them, from the defaults.
	_, after, err := c.Update(t.Context(), func(s *settings.Settings) error { s.General.Name = "Lab"; return nil })
	if err != nil || after.General.Name != "Lab" || after.Retention != settings.Defaults().Retention {
		t.Fatalf("Update() over broken settings = %+v, %v, want the defaults and the change", after, err)
	}
	if got, err := settings.Load(t.Context(), st); err != nil || got.General.Name != "Lab" {
		t.Errorf("Load() after = %+v, %v", got, err)
	}
}

package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/alerts"
	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/hostmetrics"
	"github.com/manasseh-fikadu/roost/internal/settings"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/systemd"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

func getSettings(t *testing.T, h http.Handler) api.SettingsView {
	t.Helper()
	var v api.SettingsView
	get(t, h, "/api/settings", laptopIP, &v)
	return v
}

func patchSettings(t *testing.T, h http.Handler, body string) api.SettingsView {
	t.Helper()
	rec := send(t, h, http.MethodPatch, "/api/settings", laptopIP, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /api/settings %s = %d %s, want 200", body, rec.Code, rec.Body)
	}
	var v api.SettingsView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSettings(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	opts := withStore(t, testTailnet(), st, api.Options{Tailscale: api.TailscaleModeHost})
	h := api.New(opts)

	// Only admins see the settings.
	for from, want := range map[netip.Addr]int{laptopIP: http.StatusOK, phoneIP: http.StatusForbidden, tabletIP: http.StatusForbidden} {
		if rec := serve(h, newRequest(t, http.MethodGet, "/api/settings", from)); rec.Code != want {
			t.Errorf("GET /api/settings from %v = %d, want %d", from, rec.Code, want)
		}
	}
	v := getSettings(t, h)
	defaults := settings.Defaults()
	if v.Tailscale != api.TailscaleModeHost {
		t.Errorf("tailscale = %q, want host", v.Tailscale)
	}
	if v.Settings.General.Name != "Roost" || v.Settings.Thresholds.DiskWarn != defaults.Thresholds.DiskWarn ||
		!v.Settings.Rules.CPU || v.Settings.Retention.HistoryDays != 7 || !slices.Equal(v.Settings.WatchedUnits, defaults.WatchedUnits) {
		t.Errorf("settings of a new Roost = %+v, want the defaults", v.Settings)
	}
	if fmt.Sprint(v.Defaults) != fmt.Sprint(v.Settings) {
		t.Errorf("defaults = %+v, want %+v", v.Defaults, v.Settings)
	}

	// A request changes the fields it has, and no others.
	v = patchSettings(t, h, `{"general": {"name": "  Home lab ", "publicUrl": "https://roost.example.com/"},
		"thresholds": {"diskWarn": 70, "diskCritical": 90, "memoryWarn": 80, "cpuWarn": 75, "cpuWindowMinutes": 15,
			"offlineMinutes": 30, "keyExpiryDays": 7, "checkFailures": 3}}`)
	if v.Settings.General != (api.GeneralSettings{Name: "Home lab", PublicURL: "https://roost.example.com"}) {
		t.Errorf("general = %+v, want the name trimmed and the URL without its slash", v.Settings.General)
	}
	if got := v.Settings.Thresholds; got.CPUWindowMinutes != 15 || got.DiskWarn != 70 || got.CheckFailures != 3 {
		t.Errorf("thresholds = %+v, want what was sent", got)
	}
	v = patchSettings(t, h, `{"rules": {"cpu": false}}`)
	if want := (api.AlertRules{Offline: true, KeyExpiry: true, AgentStopped: true, Disk: true, Memory: true, Checks: true, Containers: true, Units: true, Collectors: true}); v.Settings.Rules != want ||
		v.Settings.General.Name != "Home lab" || v.Settings.Thresholds.DiskWarn != 70 {
		t.Errorf("settings after turning off one rule = %+v, want the other rules and the earlier changes kept", v.Settings)
	}
	v = patchSettings(t, h, `{"general": {"name": "Lab"}, "thresholds": {"memoryWarn": 60}}`)
	if v.Settings.General != (api.GeneralSettings{Name: "Lab", PublicURL: "https://roost.example.com"}) || v.Settings.Thresholds.MemoryWarn != 60 ||
		v.Settings.Thresholds.DiskWarn != 70 {
		t.Errorf("settings after changing two fields = %+v, want the rest kept", v.Settings)
	}
	patchSettings(t, h, `{"general": {"name": "Home lab"}, "thresholds": {"memoryWarn": 80}}`)
	v = patchSettings(t, h, `{"watchedUnits": [" nginx.service", "user:app.service", "nginx.service", ""],
		"retention": {"activityDays": 90, "historyDays": 14, "checksDays": 30}}`)
	if want := []string{"nginx.service", "user:app.service"}; !slices.Equal(v.Settings.WatchedUnits, want) {
		t.Errorf("watched units = %q, want %q", v.Settings.WatchedUnits, want)
	}
	v = patchSettings(t, h, `{"watchedUnits": []}`)
	if len(v.Settings.WatchedUnits) != 0 || v.Settings.Retention.ActivityDays != 90 {
		t.Errorf("settings after clearing the units = %+v, want none watched and the retention kept", v.Settings)
	}

	// What it saved is what everything else reads.
	saved, err := settings.Load(t.Context(), st)
	if err != nil || saved.General.Name != "Home lab" || saved.Retention.HistoryDays != 14 || saved.Rules.CPU {
		t.Errorf("saved settings = %+v, %v", saved, err)
	}
	if keep, err := settings.StoreRetention(t.Context(), st); err != nil || keep.History != 14*24*time.Hour {
		t.Errorf("retention the store prunes by = %+v, %v, want 14 days of history", keep, err)
	}

	// Each change is in the activity log, and nothing else is.
	if v := patchSettings(t, h, `{}`); v.Settings.General.Name != "Home lab" {
		t.Errorf("an empty change = %+v", v.Settings)
	}
	patchSettings(t, h, `{"rules": {"cpu": false}}`)
	var events api.EventList
	get(t, h, "/api/events?limit=50", laptopIP, &events)
	var got []string
	for _, e := range events.Events {
		if e.Kind == api.EventKindSettings {
			got = append(got, e.Message+" by "+e.Actor)
		}
	}
	slices.Reverse(got)
	want := []string{
		`Renamed Roost to "Home lab" by ` + alice,
		"Changed the public URL by " + alice,
		"Changed the alert thresholds: disk warning 85% → 70%, critical disk 95% → 90%, memory 92% → 80%, CPU 90% → 75%, " +
			"CPU window 5 minutes → 15 minutes, offline after 10 minutes → 30 minutes, key expiry warning 14 days → 7 days, " +
			"failing checks 2 failures → 3 failures by " + alice,
		"Turned off alerts for CPU by " + alice,
		`Renamed Roost to "Lab" by ` + alice,
		"Changed the alert thresholds: memory 80% → 60% by " + alice,
		`Renamed Roost to "Home lab" by ` + alice,
		"Changed the alert thresholds: memory 60% → 80% by " + alice,
		"Changed the watched systemd units: started watching nginx.service, user:app.service, stopped watching " +
			"ssh.service, sshd.service, docker.service, tailscaled.service and 1 more by " + alice,
		"Changed how long Roost keeps history: activity log 30 days → 90 days, device history 7 days → 14 days, " +
			"check results 7 days → 30 days by " + alice,
		"Changed the watched systemd units: stopped watching nginx.service, user:app.service by " + alice,
	}
	if !slices.Equal(got, want) {
		t.Errorf("settings events =\n%q\nwant\n%q", got, want)
	}
	// The public URL's value stays out of the log, which viewers can read.
	for _, e := range events.Events {
		if strings.Contains(e.Message, "example.com") {
			t.Errorf("event %q has the public URL", e.Message)
		}
	}
}

func TestSettings_errors(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{}))
	goodThresholds := `{"diskWarn": 85, "diskCritical": 95, "memoryWarn": 92, "cpuWarn": 90, "cpuWindowMinutes": 5,
		"offlineMinutes": 10, "keyExpiryDays": 14, "checkFailures": 2}`
	replace := func(from, to string) string { return strings.Replace(goodThresholds, from, to, 1) }
	for _, tt := range []struct {
		name, from, body string
		wantStatus       int
		wantError        string
	}{
		{"a viewer", "phone", `{}`, http.StatusForbidden, ""},
		{"no name", "laptop", `{"general": {"name": "   "}}`, http.StatusBadRequest, "the name can't be empty"},
		{"a long name", "laptop", `{"general": {"name": "` + strings.Repeat("x", 41) + `"}}`, http.StatusBadRequest, "up to 40 characters"},
		{"a public URL with a path", "laptop", `{"general": {"name": "Roost", "publicUrl": "https://example.com/roost"}}`, http.StatusBadRequest, "public URL"},
		{"a public URL that isn't one", "laptop", `{"general": {"name": "Roost", "publicUrl": "roost"}}`, http.StatusBadRequest, "public URL"},
		{"a threshold of nothing", "laptop", `{"thresholds": {"diskWarn": 0}}`, http.StatusBadRequest, "percentage above 0"},
		{"a warning past the critical level", "laptop", `{"thresholds": {"diskWarn": 99}}`, http.StatusBadRequest, "can't be critical"},
		{"a critical disk before the warning", "laptop", `{"thresholds": ` + replace(`"diskCritical": 95`, `"diskCritical": 80`) + `}`, http.StatusBadRequest, "can't be critical"},
		{"a CPU window of a day", "laptop", `{"thresholds": ` + replace(`"cpuWindowMinutes": 5`, `"cpuWindowMinutes": 1440`) + `}`, http.StatusBadRequest, "minute to an hour"},
		{"a unit with a space", "laptop", `{"watchedUnits": ["my app"]}`, http.StatusBadRequest, "isn't the name of a systemd unit"},
		{"a unit systemd refuses", "laptop", `{"watchedUnits": ["ssh.service", "a!b.service"]}`, http.StatusBadRequest, "isn't the name of a systemd unit"},
		{"a year and a day of activity", "laptop", `{"retention": {"activityDays": 366, "historyDays": 7, "checksDays": 7}}`, http.StatusBadRequest, "from 1 to 365"},
		{"a field Roost doesn't have", "laptop", `{"thresholds": ` + replace(`"checkFailures": 2`, `"checkFailures": 2, "bogus": 1`) + `}`, http.StatusBadRequest, "can't read the request"},
		{"a name of the wrong type", "laptop", `{"general": {"name": 7, "publicUrl": ""}}`, http.StatusBadRequest, "can't read the request"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			from := map[string]netip.Addr{"laptop": laptopIP, "phone": phoneIP}[tt.from]
			rec := send(t, h, http.MethodPatch, "/api/settings", from, tt.body)
			if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), tt.wantError) {
				t.Errorf("PATCH %s = %d %s, want %d with %q", tt.body, rec.Code, rec.Body, tt.wantStatus, tt.wantError)
			}
		})
	}
	// Nothing above saved anything.
	if v := getSettings(t, h); fmt.Sprint(v.Settings) != fmt.Sprint(v.Defaults) {
		t.Errorf("settings after the failed changes = %+v, want the defaults", v.Settings)
	}
}

func TestSettings_publicURL(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{AppURL: "https://" + roostHost}))
	uninstall := func() string {
		var setup api.AgentSetup
		get(t, h, "/api/agent", laptopIP, &setup)
		return setup.UninstallCommand
	}
	if got := uninstall(); !strings.Contains(got, "https://"+roostHost+"/api/agent/uninstall.sh") {
		t.Fatalf("uninstall command = %q, want Roost's address on the tailnet", got)
	}
	patchSettings(t, h, `{"general": {"name": "Roost", "publicUrl": "https://roost.example.com:8443"}}`)
	if got := uninstall(); !strings.Contains(got, "https://roost.example.com:8443/api/agent/uninstall.sh") {
		t.Errorf("uninstall command = %q, want the public URL", got)
	}
	patchSettings(t, h, `{"general": {"name": "Roost", "publicUrl": ""}}`)
	if got := uninstall(); !strings.Contains(got, "https://"+roostHost+"/api/agent/uninstall.sh") {
		t.Errorf("uninstall command after clearing the public URL = %q, want Roost's address on the tailnet", got)
	}
}

func TestSettings_units(t *testing.T) {
	t.Parallel()
	// Without systemd, there's nothing to pick from.
	h := api.New(withTailnet(t, testTailnet(), api.Options{}))
	var none api.InstalledServices
	get(t, h, "/api/settings/units", laptopIP, &none)
	if none.Available || len(none.Services) != 0 || fmt.Sprint(none) != "{false []}" {
		t.Errorf("services without systemd = %+v, want none, and not available", none)
	}
	if rec := serve(h, newRequest(t, http.MethodGet, "/api/settings/units", phoneIP)); rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/settings/units as a viewer = %d, want 403", rec.Code)
	}

	run := func(_ context.Context, args ...string) ([]byte, error) {
		user := args[0] == "--user"
		if user {
			args = args[1:]
		}
		switch {
		case args[0] == "list-units" && user:
			return []byte("app.service loaded active running My app\n"), nil
		case args[0] == "list-units":
			return []byte("ssh.service loaded active running OpenBSD Secure Shell server\nnginx.service loaded failed dead nginx\n"), nil
		case args[0] == "list-unit-files" && user:
			return nil, nil
		}
		return []byte("ssh.service enabled disabled\npostgresql.service disabled enabled\n"), nil
	}
	collector := systemd.NewCollector(systemd.Options{Run: run, UserScope: true})
	h = api.New(withTailnet(t, testTailnet(), api.Options{Systemd: collector}))
	var got api.InstalledServices
	get(t, h, "/api/settings/units", laptopIP, &got)
	var keys []string
	for _, s := range got.Services {
		keys = append(keys, fmt.Sprintf("%s %s %s", s.Key, s.Scope, s.Active))
	}
	want := []string{"nginx.service system failed", "postgresql.service system ", "ssh.service system active", "user:app.service user active"}
	if !got.Available || !slices.Equal(keys, want) {
		t.Errorf("services = %v, %q, want available, %q", got.Available, keys, want)
	}
	if got.Services[2].Description != "OpenBSD Secure Shell server" {
		t.Errorf("ssh.service's description = %q", got.Services[2].Description)
	}

	// systemd not answering is an error for the admin, not an empty list.
	broken := systemd.NewCollector(systemd.Options{Run: func(context.Context, ...string) ([]byte, error) { return nil, errors.New("no bus") }})
	h = api.New(withTailnet(t, testTailnet(), api.Options{Systemd: broken}))
	if rec := serve(h, newRequest(t, http.MethodGet, "/api/settings/units", laptopIP)); rec.Code != http.StatusBadGateway || strings.Contains(rec.Body.String(), "no bus") {
		t.Errorf("services while systemd fails = %d %s, want 502 without its error", rec.Code, rec.Body)
	}
}

// ruleFeed returns a feed of a tailnet with the hub, which has a full disk, a
// busy CPU and little memory, and a server that has been offline for 30
// minutes, and its settings are in st.
func ruleFeed(t *testing.T, st *store.Store) *api.Feed {
	t.Helper()
	now := time.Now()
	status := &tailnet.Status{
		State: tailnet.StateRunning,
		Self:  tailnet.Device{ID: "roost", HostName: "roost", DNSName: roostHost, IPs: []netip.Addr{roostIP}, OS: "linux", Online: true},
		Peers: []tailnet.Device{
			{ID: "server", HostName: "server", OS: "linux", LastSeen: now.Add(-30 * time.Minute)},
			{ID: "nas", HostName: "nas", OS: "linux", Online: true, KeyExpiry: now.Add(10 * 24 * time.Hour)},
		},
	}
	cpu := 97.0
	host := hostmetrics.Reading{Sample: &hostmetrics.Sample{
		At: now, Mem: hostmetrics.Usage{Used: 95, Total: 100}, Disk: &hostmetrics.Usage{Used: 96 << 30, Total: 100 << 30}, CPUSustained: &cpu,
	}}
	return api.NewFeed(api.FeedOptions{
		Tailnet: func() tailnet.Snapshot { return tailnet.Snapshot{Status: status, StatusAt: now} },
		Host:    func() hostmetrics.Reading { return host },
		Alerts:  alerts.NewEngine(st, nil),
		Agents:  agents.NewRegistry(st, nil),
		Store:   st,
	})
}

func TestFeed_settingsChangeAlerts(t *testing.T) {
	t.Parallel()
	cfg := settings.Defaults()
	own := map[string]store.ThresholdOverrides{}
	// The snapshot of a new feed, on a new store that has cfg and the devices'
	// own thresholds, since a store keeps the alerts that opened.
	look := func() api.Snapshot {
		t.Helper()
		st := testStore(t)
		if err := settings.Save(t.Context(), st, cfg); err != nil {
			t.Fatal(err)
		}
		for id, o := range own {
			if err := st.SetDeviceSettings(t.Context(), id, store.DeviceSettings{Thresholds: o}); err != nil {
				t.Fatal(err)
			}
		}
		return ruleFeed(t, st).Latest()
	}
	statuses := func(snap api.Snapshot) map[string]string {
		out := map[string]string{}
		for _, d := range snap.Devices {
			out[d.ID] = string(d.Status) + ": " + d.StatusReason
		}
		return out
	}

	snap := look()
	want := []string{
		"critical: Disk 96% full on roost",
		"warning: CPU 97% busy on roost",
		"warning: Memory 95% used on roost",
		"warning: Tailscale key of nas expires in 10 days",
		"warning: server is offline",
	}
	if got := alertSummary(snap.Alerts); !slices.Equal(got, want) {
		t.Fatalf("alerts by default =\n%q\nwant\n%q", got, want)
	}
	if s := statuses(snap)["roost"]; !strings.Contains(s, "Disk 96% full") || !strings.Contains(s, "CPU 97% for 5m") {
		t.Errorf("the hub's status = %q, want its disk and CPU, over 5m", s)
	}

	// Thresholds can go up, and the window shows in what the alert says.
	cfg.Thresholds.DiskWarn, cfg.Thresholds.DiskCritical, cfg.Thresholds.MemoryWarn = 97, 99, 99
	cfg.Thresholds.CPUWindowMinutes, cfg.Thresholds.KeyExpiryDays, cfg.Thresholds.OfflineMinutes = 15, 7, 20
	snap = look()
	want = []string{"warning: CPU 97% busy on roost", "warning: server is offline"}
	if got := alertSummary(snap.Alerts); !slices.Equal(got, want) {
		t.Errorf("alerts after raising the thresholds =\n%q\nwant\n%q", got, want)
	}
	if s := statuses(snap)["roost"]; s != "attention: CPU 97% for 15m" {
		t.Errorf("the hub's status = %q, want only CPU, over 15m", s)
	}
	for _, a := range snap.Alerts {
		if strings.HasPrefix(a.Title, "CPU") && !strings.Contains(a.Detail, "last 15m") {
			t.Errorf("CPU alert detail = %q, want the window", a.Detail)
		}
	}
	if snap.Hub.Thresholds.CPUWindowMinutes != 15 || snap.Hub.Name != "Roost" {
		t.Errorf("hub = %+v, want the settings' thresholds", snap.Hub)
	}

	// A device's own thresholds beat the settings'. The server has been gone
	// 30 minutes: the settings say 20 and its own say an hour.
	own["server"] = store.ThresholdOverrides{OfflineMinutes: new(60)}
	own["roost"] = store.ThresholdOverrides{CPUWarn: new(99.0), DiskWarn: new(50.0), DiskCritical: new(90.0)}
	snap = look()
	want = []string{"critical: Disk 96% full on roost"}
	if got := alertSummary(snap.Alerts); !slices.Equal(got, want) {
		t.Errorf("alerts with a device's own thresholds =\n%q\nwant\n%q", got, want)
	}
	if got := statuses(snap)["server"]; got != "offline: Not connected to the tailnet" {
		t.Errorf("server's status = %q, want offline whatever its alert says", got)
	}
	for _, d := range snap.Devices {
		if d.ID == "roost" && !reflect.DeepEqual(d.Settings.Thresholds, api.DeviceThresholds{CPUWarn: new(99.0), DiskWarn: new(50.0), DiskCritical: new(90.0)}) {
			t.Errorf("the hub's own thresholds = %+v, want what was set", d.Settings.Thresholds)
		}
	}

	// A rule that's off raises no alert, and the dashboard doesn't flag it.
	clear(own)
	cfg.Rules.Disk, cfg.Rules.CPU, cfg.Rules.Offline = false, false, false
	snap = look()
	if got := alertSummary(snap.Alerts); len(got) != 0 {
		t.Errorf("alerts with the rules off = %q, want none", got)
	}
	if got := statuses(snap)["roost"]; got != "online: This hub" {
		t.Errorf("the hub's status with its rules off = %q, want it online", got)
	}
	cfg.Rules = settings.Defaults().Rules
	cfg.Rules.KeyExpiry = false
	cfg.Thresholds.KeyExpiryDays = 14
	if got := alertSummary(look().Alerts); slices.Contains(got, "warning: Tailscale key of nas expires in 10 days") {
		t.Errorf("alerts with key expiry off = %q, want no key alert", got)
	}
}

func TestSetDeviceSettings_thresholds(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))
	put := func(body string) api.DeviceSettings {
		t.Helper()
		rec := send(t, h, http.MethodPut, "/api/devices/laptop/settings", laptopIP, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s = %d %s", body, rec.Code, rec.Body)
		}
		var ds api.DeviceSettings
		if err := json.Unmarshal(rec.Body.Bytes(), &ds); err != nil {
			t.Fatal(err)
		}
		return ds
	}
	ds := put(`{"thresholds": {"diskWarn": 97, "offlineMinutes": 60, "cpuWarn": 100}}`)
	want := store.ThresholdOverrides{DiskWarn: new(97.0), OfflineMinutes: new(60), CPUWarn: new(100.0)}
	if got := ds.Thresholds; !reflect.DeepEqual(store.ThresholdOverrides(got), want) {
		t.Errorf("thresholds = %+v, want %+v", got, want)
	}
	if saved, err := st.DeviceSettings(t.Context()); err != nil || !reflect.DeepEqual(saved["laptop"].Thresholds, want) {
		t.Errorf("saved thresholds = %+v, %v, want %+v", saved["laptop"].Thresholds, err, want)
	}

	// They go with the rest of the settings: saving without them clears them.
	if ds := put(`{"displayName": "Work"}`); ds.Thresholds != (api.DeviceThresholds{}) {
		t.Errorf("thresholds after saving without them = %+v, want none", ds.Thresholds)
	}

	for name, body := range map[string]string{
		"memory over 100":      `{"thresholds": {"memoryWarn": 101}}`,
		"no offline time":      `{"thresholds": {"offlineMinutes": 0}}`,
		"offline over a week":  `{"thresholds": {"offlineMinutes": 100000}}`,
		"a backwards disk":     `{"thresholds": {"diskWarn": 90, "diskCritical": 80}}`,
		"a negative threshold": `{"thresholds": {"cpuWarn": -5}}`,
		"an unknown threshold": `{"thresholds": {"keyExpiryDays": 3}}`,
	} {
		if rec := send(t, h, http.MethodPut, "/api/devices/laptop/settings", laptopIP, body); rec.Code != http.StatusBadRequest {
			t.Errorf("PUT with %s = %d %s, want 400", name, rec.Code, rec.Body)
		}
	}
	// A warning above the settings' critical level is fine: the device is
	// critical at its warning level.
	put(`{"thresholds": {"diskWarn": 99}}`)
}

func TestSettings_corrupt(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))
	if err := st.SetKV(t.Context(), "settings", `{"retention":{"activityDays":"x"}}`); err != nil {
		t.Fatal(err)
	}
	// Roost carries on with the defaults, and an admin can write over what it
	// can't read.
	if v := getSettings(t, h); v.Settings.Retention.ActivityDays != 30 {
		t.Errorf("settings that can't be read = %+v, want the defaults", v.Settings)
	}
	if v := patchSettings(t, h, `{"general": {"name": "Lab"}}`); v.Settings.General.Name != "Lab" {
		t.Errorf("settings after writing over = %+v", v.Settings)
	}
	if saved, err := settings.Load(t.Context(), st); err != nil || saved.General.Name != "Lab" {
		t.Errorf("saved settings = %+v, %v", saved, err)
	}
}

// The machine Roost runs on is two nodes in embedded mode, Roost's own and a
// peer. Roost measures it once, so what an admin set on either node counts
// for both.
func TestFeed_settingsOfTheHubMachine(t *testing.T) {
	t.Parallel()
	now := time.Now()
	status := &tailnet.Status{
		State: tailnet.StateRunning,
		Self:  tailnet.Device{ID: "roost", HostName: "roost", DNSName: roostHost, IPs: []netip.Addr{roostIP}, OS: "linux", Online: true},
		Peers: []tailnet.Device{{ID: "host", HostName: "host", OS: "linux", Online: true, SameMachine: true}},
	}
	cpu := 97.0
	host := hostmetrics.Reading{Sample: &hostmetrics.Sample{
		At: now, Mem: hostmetrics.Usage{Used: 50, Total: 100}, Disk: &hostmetrics.Usage{Used: 96 << 30, Total: 100 << 30}, CPUSustained: &cpu,
	}}
	look := func(own map[string]store.ThresholdOverrides) api.Snapshot {
		t.Helper()
		st := testStore(t)
		for id, o := range own {
			if err := st.SetDeviceSettings(t.Context(), id, store.DeviceSettings{Thresholds: o}); err != nil {
				t.Fatal(err)
			}
		}
		return api.NewFeed(api.FeedOptions{
			Tailnet: func() tailnet.Snapshot { return tailnet.Snapshot{Status: status, StatusAt: now} },
			Host:    func() hostmetrics.Reading { return host },
			Alerts:  alerts.NewEngine(st, nil),
			Store:   st,
		}).Latest()
	}
	for name, tt := range map[string]struct {
		own  map[string]store.ThresholdOverrides
		want []string
	}{
		"nothing of its own": {nil, []string{"critical: Disk 96% full on roost", "warning: CPU 97% busy on roost"}},
		"on the peer":        {map[string]store.ThresholdOverrides{"host": {DiskWarn: new(99.0), CPUWarn: new(99.0)}}, nil},
		"on Roost's node":    {map[string]store.ThresholdOverrides{"roost": {DiskWarn: new(99.0), CPUWarn: new(99.0)}}, nil},
		// Roost's own node wins where both say something.
		"on both": {map[string]store.ThresholdOverrides{
			"roost": {DiskWarn: new(50.0), DiskCritical: new(60.0)}, "host": {DiskWarn: new(99.0), CPUWarn: new(99.0)},
		}, []string{"critical: Disk 96% full on roost"}},
	} {
		snap := look(tt.own)
		if got := alertSummary(snap.Alerts); !slices.Equal(got, tt.want) {
			t.Errorf("%s: alerts = %q, want %q", name, got, tt.want)
		}
		for _, d := range snap.Devices {
			flagged := strings.Contains(d.StatusReason, "Disk")
			if wantFlagged := len(tt.want) > 0; flagged != wantFlagged {
				t.Errorf("%s: %s's status = %q, want its disk flagged: %v, as the alerts say", name, d.ID, d.StatusReason, wantFlagged)
			}
		}
	}
}

// An alert of a rule that's turned off resolves even when the data it was
// raised from isn't fresh, since there's nothing left to wait for.
func TestFeed_turningARuleOffResolvesItsAlerts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := testStore(t)
		engine := alerts.NewEngine(st, nil)
		build := func(hostAge time.Duration) api.Snapshot {
			t.Helper()
			now := time.Now()
			status := &tailnet.Status{
				State: tailnet.StateRunning,
				Self:  tailnet.Device{ID: "roost", HostName: "roost", DNSName: roostHost, IPs: []netip.Addr{roostIP}, OS: "linux", Online: true},
			}
			host := hostmetrics.Reading{Sample: &hostmetrics.Sample{
				At: now.Add(-hostAge), Mem: hostmetrics.Usage{Used: 50, Total: 100}, Disk: &hostmetrics.Usage{Used: 96 << 30, Total: 100 << 30},
			}}
			return api.NewFeed(api.FeedOptions{
				Tailnet: func() tailnet.Snapshot { return tailnet.Snapshot{Status: status, StatusAt: now} },
				Host:    func() hostmetrics.Reading { return host },
				Alerts:  engine, Store: st,
			}).Latest()
		}
		if got := alertSummary(build(0).Alerts); !slices.Equal(got, []string{"critical: Disk 96% full on roost"}) {
			t.Fatalf("alerts = %q, want the disk's", got)
		}

		// The hub's reading goes stale, and an admin turns the disk rule off.
		s := settings.Defaults()
		s.Rules.Disk = false
		if err := settings.Save(t.Context(), st, s); err != nil {
			t.Fatal(err)
		}
		build(10 * time.Minute)
		time.Sleep(alerts.ResolveAfter + time.Second)
		if got := alertSummary(build(10 * time.Minute).Alerts); len(got) != 0 {
			t.Errorf("alerts a minute after turning the rule off = %q, want none", got)
		}

		// With the rule on again and the reading still stale, an alert that
		// opens again waits for fresh data to resolve.
		s.Rules.Disk = true
		if err := settings.Save(t.Context(), st, s); err != nil {
			t.Fatal(err)
		}
		if got := alertSummary(build(0).Alerts); !slices.Equal(got, []string{"critical: Disk 96% full on roost"}) {
			t.Errorf("alerts with the rule on again = %q, want the disk's", got)
		}
	})
}

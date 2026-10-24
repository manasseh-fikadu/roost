package api_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/alerts"
	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/docker"
	"github.com/manasseh-fikadu/roost/internal/hostmetrics"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/systemd"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// alertSummary writes an alert as "severity: title".
func alertSummary(as []api.Alert) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = string(a.Severity) + ": " + a.Title
	}
	slices.Sort(out)
	return out
}

func TestSnapshot_alertRules(t *testing.T) {
	t.Parallel()
	st := checksStore(t)
	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	status := &tailnet.Status{
		State: tailnet.StateRunning,
		Self:  tailnet.Device{ID: "roost", HostName: "roost", DNSName: roostHost, IPs: []netip.Addr{roostIP}, OS: "linux", Online: true},
		Peers: []tailnet.Device{
			{ID: "laptop", HostName: "laptop", OS: "macOS", Online: true},
			{ID: "phone", HostName: "phone", OS: "android", LastSeen: ago(time.Hour)},
			{ID: "server", HostName: "server", OS: "linux", LastSeen: ago(30 * time.Minute), KeyExpiry: now.Add(40 * time.Hour)},
			{ID: "tablet", HostName: "tablet", OS: "linux", LastSeen: ago(5 * time.Minute)},
			{ID: "nas", HostName: "nas", OS: "linux", Online: true, KeyExpiry: now.Add(10 * 24 * time.Hour)},
		},
	}
	reg := agents.NewRegistry(st, nil)
	var hb agents.Heartbeat
	if err := json.Unmarshal([]byte(`{"v":1,"metrics":{"cpu":99,"diskUsed":99,"diskTotal":100}}`), &hb); err != nil {
		t.Fatal(err)
	}
	// The laptop's agent stopped 5 minutes ago, so its metrics are old.
	if _, err := reg.Record(t.Context(), "laptop", hb, ago(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	runner := loadedRunner(t, st, testTailnet())
	api1, err := runner.Add(t.Context(), store.Check{Name: "API", Kind: "tcp", Target: "127.0.0.1:8080", Group: "g", Interval: time.Hour, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	addResults(t, st, api1.ID, ago(3*time.Minute), false, false)
	if err := runner.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	cpu := 97.0
	host := hostmetrics.Reading{Sample: &hostmetrics.Sample{
		At: now, Mem: hostmetrics.Usage{Used: 95, Total: 100}, Disk: &hostmetrics.Usage{Used: 96 << 30, Total: 100 << 30}, CPUSustained: &cpu,
	}}
	containers := docker.Reading{At: now, Containers: []docker.Container{
		{ID: "a1", Name: "web", State: "running", Health: "unhealthy", Status: "Up 1 hour (unhealthy)", Image: "nginx",
			Ports: []docker.Port{{IP: "0.0.0.0", Private: 80, Public: 8080, Type: "tcp"}}},
		{ID: "a2", Name: "job", State: "exited", ExitCode: 0, RestartPolicy: "no", Status: "Exited (0)", Image: "job"},
		{ID: "a3", Name: "api", State: "exited", ExitCode: 1, RestartPolicy: "always", Status: "Exited (1)", Image: "api"},
		{ID: "a4", Name: "db", State: "exited", ExitCode: 143, RestartPolicy: "unless-stopped", Status: "Exited (143)", Image: "postgres"},
		{ID: "a5", Name: "cache", State: "exited", ExitCode: 137, OOMKilled: true, Status: "Exited (137)", Image: "redis"},
	}}
	units := systemd.Reading{At: now, Units: []systemd.Unit{
		{Name: "cron.service", Scope: systemd.System, Active: "inactive", Sub: "dead", Watched: true, Description: "Cron"},
		{Name: "nginx.service", Scope: systemd.System, Active: "failed", Sub: "failed", Description: "nginx"},
		{Name: "ssh.service", Scope: systemd.System, Active: "active", Sub: "running", Watched: true},
		{Name: "backup.service", Scope: systemd.User, Active: "failed", Sub: "failed", Description: "Backup"},
	}}
	feed := api.NewFeed(api.FeedOptions{
		Tailnet: func() tailnet.Snapshot { return tailnet.Snapshot{Status: status, StatusAt: now} },
		Host:    func() hostmetrics.Reading { return host },
		Agents:  reg,
		Checks:  runner,
		Alerts:  alerts.NewEngine(st, nil),
		Docker:  func() docker.Reading { return containers },
		Systemd: func() systemd.Reading { return units },
		Store:   st,
	})
	snap := feed.Latest()

	want := []string{
		"critical: API is failing",
		"critical: Container api crashed (exit 1)",
		"critical: Container cache was killed for using too much memory",
		"critical: Disk 96% full on roost",
		"critical: Tailscale key of server expires in 2 days",
		"warning: Agent on laptop stopped reporting",
		"warning: CPU 97% busy on roost",
		"warning: Container db stopped",
		"warning: Container web is unhealthy",
		"warning: Memory 95% used on roost",
		"warning: Tailscale key of nas expires in 10 days",
		"warning: User unit backup.service failed",
		"warning: cron.service isn't running",
		"warning: nginx.service failed",
		"warning: server is offline",
	}
	if got := alertSummary(snap.Alerts); !slices.Equal(got, want) {
		t.Errorf("alerts =\n%q\nwant\n%q", got, want)
	}
	if snap.Alerts[0].Severity != api.AlertSeverityCritical || snap.Alerts[len(snap.Alerts)-1].Severity != api.AlertSeverityWarning {
		t.Errorf("alerts aren't the most severe first: %q", alertSummary(snap.Alerts))
	}
	for _, a := range snap.Alerts {
		switch a.Source {
		case api.AlertSourceMetrics, api.AlertSourceContainer, api.AlertSourceUnit:
			if a.DeviceID != "roost" {
				t.Errorf("%q is on device %q, want the hub's", a.Title, a.DeviceID)
			}
		case api.AlertSourceCheck:
			if a.CheckID != api1.ID || !strings.Contains(a.Detail, "2 failures in a row") {
				t.Errorf("the check's alert = %+v, want its check and failures", a)
			}
		case api.AlertSourceDevice, api.AlertSourceAgent, api.AlertSourceCollector:
		}
		if a.State != api.AlertStateOpen || a.Notes == nil {
			t.Errorf("%q has state %s and notes %v, want open with no notes", a.Title, a.State, a.Notes)
		}
	}

	byName := map[string]api.Container{}
	for _, c := range snap.Containers {
		byName[c.Name] = c
	}
	if web := byName["web"]; web.Problem != "is unhealthy" || !slices.Equal(web.CheckIDs, []string{api1.ID}) || web.DeviceID != "roost" || web.ID != "a1" {
		t.Errorf("web = %+v, want it unhealthy, with the check of its port", web)
	}
	if job := byName["job"]; job.Problem != "" || len(job.CheckIDs) != 0 {
		t.Errorf("job = %+v, want no problem: it finished, and needn't run", job)
	}
	var unitNames []string
	for _, u := range snap.Units {
		unitNames = append(unitNames, string(u.Scope)+"/"+u.Name)
	}
	if want := []string{"system/cron.service", "system/nginx.service", "system/ssh.service", "user/backup.service"}; !slices.Equal(unitNames, want) {
		t.Errorf("units = %q, want %q", unitNames, want)
	}
	var collectors []string
	for _, c := range snap.Collectors {
		collectors = append(collectors, c.Name)
	}
	if !slices.Contains(collectors, "Docker") || !slices.Contains(collectors, "systemd units") {
		t.Errorf("collectors = %q, want Docker and systemd units among them", collectors)
	}
	if snap.LastEventID == 0 {
		t.Error("LastEventID = 0, want the opened alerts' events")
	}
	// The hub has no thresholds of its own yet, so the device's status uses
	// the same.
	for _, d := range snap.Devices {
		if d.ID == "roost" && (d.Status != api.StatusAttention || !strings.Contains(d.StatusReason, "Disk 96% full")) {
			t.Errorf("the hub's status = %s %q, want attention for its disk", d.Status, d.StatusReason)
		}
	}
}

// fakeSources holds what a feed reads, and lets a test change it.
type fakeSources struct {
	mu         sync.Mutex
	status     *tailnet.Status
	statusAt   time.Time
	containers docker.Reading
	changed    chan struct{}
}

func (f *fakeSources) tailnet() tailnet.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return tailnet.Snapshot{Status: f.status, StatusAt: f.statusAt}
}

func (f *fakeSources) docker() docker.Reading {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.containers
}

func (f *fakeSources) set(change func(f *fakeSources)) {
	f.mu.Lock()
	change(f)
	f.mu.Unlock()
	select {
	case f.changed <- struct{}{}:
	default:
	}
}

// eventMessages returns the activity log's messages, oldest first.
func eventMessages(t *testing.T, st *store.Store) []string {
	t.Helper()
	events, err := st.Events(t.Context(), store.EventQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range slices.Backward(events) {
		out = append(out, e.Kind+": "+e.Message)
	}
	return out
}

// A container that crashes opens an alert, and one that's fixed resolves it
// a minute later. The activity log has both, and the container's starts and
// stops.
func TestFeed_alertsOpenAndResolve(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := checksStore(t)
		synctest.Wait()
		src := &fakeSources{
			status:     &tailnet.Status{State: tailnet.StateRunning, Self: tailnet.Device{ID: "roost", HostName: "roost", Online: true}},
			statusAt:   time.Now(),
			containers: docker.Reading{At: time.Now(), Containers: []docker.Container{{ID: "a1", Name: "api", State: "running", RestartPolicy: "always"}}},
			changed:    make(chan struct{}, 1),
		}
		feed := api.NewFeed(api.FeedOptions{
			Tailnet: src.tailnet, Docker: src.docker, DockerChanged: src.changed,
			Alerts: alerts.NewEngine(st, nil), Store: st,
		})
		go feed.Run(t.Context())
		synctest.Wait()

		src.set(func(f *fakeSources) {
			f.containers = docker.Reading{At: time.Now(), Containers: []docker.Container{{ID: "a1", Name: "api", State: "exited", ExitCode: 2, RestartPolicy: "always"}}}
		})
		time.Sleep(2 * time.Second)
		if got := alertSummary(feed.Latest().Alerts); !slices.Equal(got, []string{"critical: Container api crashed (exit 2)"}) {
			t.Fatalf("after the crash, alerts = %q", got)
		}

		// While Docker can't be read, Roost can't say it's fixed.
		src.set(func(f *fakeSources) {
			f.containers = docker.Reading{At: f.containers.At, Err: errors.New("connection refused")}
		})
		time.Sleep(3 * time.Minute)
		if got := feed.Latest().Alerts; len(got) != 2 {
			t.Errorf("while Docker failed, alerts = %q, want the crash, and Docker keeps failing", alertSummary(got))
		}

		src.set(func(f *fakeSources) {
			f.containers = docker.Reading{At: time.Now(), Containers: []docker.Container{{ID: "a1", Name: "api", State: "running", RestartPolicy: "always"}}}
		})
		time.Sleep(30 * time.Second)
		if got := feed.Latest().Alerts; len(got) != 2 {
			t.Errorf("30 seconds after the fix, alerts = %q, want them still open", alertSummary(got))
		}
		time.Sleep(time.Minute)
		if got := feed.Latest().Alerts; len(got) != 0 {
			t.Errorf("a minute after the fix, alerts = %q, want none", alertSummary(got))
		}
		want := []string{
			"container: Container api crashed (exit 2)",
			"alert: Container api crashed (exit 2)",
			"alert: Docker keeps failing",
			"container: Container api started",
			"alert: Resolved: Container api crashed (exit 2)",
			"alert: Resolved: Docker keeps failing",
		}
		got := eventMessages(t, st)
		// Events at the same moment can come in either order.
		slices.Sort(got[:2])
		slices.Sort(want[:2])
		slices.Sort(got[4:])
		slices.Sort(want[4:])
		if !slices.Equal(got, want) {
			t.Errorf("the activity log =\n%q\nwant\n%q", got, want)
		}
	})
}

// Once the tailnet's status has been known for a minute, devices joining,
// leaving, going offline and coming online are in the activity log.
func TestFeed_deviceActivity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := checksStore(t)
		synctest.Wait()
		peers := func(ps ...tailnet.Device) *tailnet.Status {
			return &tailnet.Status{State: tailnet.StateRunning, Self: tailnet.Device{ID: "roost", HostName: "roost", Online: true}, Peers: ps}
		}
		laptop := tailnet.Device{ID: "laptop", HostName: "laptop", Online: true}
		phone := tailnet.Device{ID: "phone", HostName: "phone", Online: true}
		src := &fakeSources{status: peers(), statusAt: time.Now(), changed: make(chan struct{}, 1)}
		feed := api.NewFeed(api.FeedOptions{Tailnet: src.tailnet, TailnetChanged: src.changed, Store: st})
		go feed.Run(t.Context())
		synctest.Wait()

		// Right after Roost starts, peers show up, which isn't news.
		src.set(func(f *fakeSources) { f.status, f.statusAt = peers(laptop, phone), time.Now() })
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := eventMessages(t, st); len(got) != 0 {
			t.Errorf("while Roost warmed up, the activity log = %q, want nothing", got)
		}

		offline := phone
		offline.Online = false
		tablet := tailnet.Device{ID: "tablet", HostName: "tablet", Online: true}
		src.set(func(f *fakeSources) { f.status, f.statusAt = peers(laptop, offline, tablet), time.Now() })
		time.Sleep(2 * time.Second)
		src.set(func(f *fakeSources) { f.status, f.statusAt = peers(phone, tablet), time.Now() })
		time.Sleep(2 * time.Second)
		want := []string{
			"device: phone went offline",
			"device: tablet joined the tailnet",
			"device: phone came online",
			"device: laptop left the tailnet",
		}
		got := eventMessages(t, st)
		slices.Sort(got[:2])
		slices.Sort(got[2:])
		slices.Sort(want[:2])
		slices.Sort(want[2:])
		if !slices.Equal(got, want) {
			t.Errorf("the activity log = %q, want %q", got, want)
		}
	})
}

// openAlert returns a handler, its store and feed, and the ID of an open
// alert, of the disk on the hub.
func openAlert(t *testing.T) (http.Handler, *store.Store, *api.Feed, int64) {
	t.Helper()
	st := checksStore(t)
	tn := testTailnet()
	engine := alerts.NewEngine(st, nil)
	engine.Apply(t.Context(), []alerts.Condition{{
		Key: "disk:roost", Source: "metrics", DeviceID: "roost", Severity: alerts.Warning, Title: "Disk 90% full on roost",
	}}, func(string, string) bool { return true }, time.Now())
	opts := withStore(tn, st, api.Options{Alerts: engine})
	return api.New(opts), st, opts.Feed, engine.Open()[0].ID
}

func decodeAlert(t *testing.T, body []byte) api.Alert {
	t.Helper()
	var a api.Alert
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAlertRoutes(t *testing.T) {
	t.Parallel()
	h, st, feed, id := openAlert(t)
	path := fmt.Sprintf("/api/alerts/%d", id)

	rec := send(t, h, http.MethodPut, path+"/ack", laptopIP, "")
	if a := decodeAlert(t, rec.Body.Bytes()); rec.Code != http.StatusOK || a.State != api.AlertStateAcknowledged || a.AckedBy != alice || a.AckedAt == nil {
		t.Errorf("PUT ack = %d %+v, want it acknowledged by alice", rec.Code, a)
	}
	rec = send(t, h, http.MethodPut, path+"/snooze", laptopIP, `{"minutes":240}`)
	a := decodeAlert(t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || a.State != api.AlertStateSnoozed || a.SnoozedUntil == nil || time.Until(*a.SnoozedUntil) < 239*time.Minute {
		t.Errorf("PUT snooze = %d %+v, want it snoozed for 4 hours", rec.Code, a)
	}
	rec = send(t, h, http.MethodPost, path+"/notes", laptopIP, `{"text":"  Cleaning up the logs "}`)
	var note api.AlertNote
	if err := json.Unmarshal(rec.Body.Bytes(), &note); err != nil || rec.Code != http.StatusCreated || note.Text != "Cleaning up the logs" || note.By != alice {
		t.Errorf("POST notes = %d %s, want the note by alice", rec.Code, rec.Body)
	}

	// Viewers see the alert, but not who acknowledged or snoozed it.
	ev, ok := api.Redact(feed.Latest()).(api.Snapshot)
	if !ok {
		t.Fatal("Redact() didn't return a snapshot")
	}
	if len(ev.Alerts) != 1 || ev.Alerts[0].AckedBy != "" || ev.Alerts[0].SnoozedBy != "" || len(ev.Alerts[0].Notes) != 1 || ev.Alerts[0].Notes[0].By != "" {
		t.Errorf("a viewer's alerts = %+v, want the alert with its note, and no logins", ev.Alerts)
	}

	rec = send(t, h, http.MethodDelete, path+"/snooze", laptopIP, "")
	if a := decodeAlert(t, rec.Body.Bytes()); rec.Code != http.StatusOK || a.State != api.AlertStateAcknowledged || a.SnoozedUntil != nil {
		t.Errorf("DELETE snooze = %d %+v, want it awake, still acknowledged", rec.Code, a)
	}
	rec = send(t, h, http.MethodDelete, path+"/ack", laptopIP, "")
	if a := decodeAlert(t, rec.Body.Bytes()); rec.Code != http.StatusOK || a.State != api.AlertStateOpen {
		t.Errorf("DELETE ack = %d %+v, want it open", rec.Code, a)
	}

	for _, tt := range []struct {
		name, method, target, body string
		want                       int
	}{
		{"ack a missing alert", http.MethodPut, "/api/alerts/999/ack", "", http.StatusNotFound},
		{"ack an alert with a bad ID", http.MethodPut, "/api/alerts/disk/ack", "", http.StatusNotFound},
		{"snooze for no time", http.MethodPut, path + "/snooze", `{"minutes":0}`, http.StatusBadRequest},
		{"snooze for over a month", http.MethodPut, path + "/snooze", `{"minutes":50000}`, http.StatusBadRequest},
		{"an empty note", http.MethodPost, path + "/notes", `{"text":" "}`, http.StatusBadRequest},
		{"a note on a missing alert", http.MethodPost, "/api/alerts/999/notes", `{"text":"x"}`, http.StatusNotFound},
		{"a bad page", http.MethodGet, "/api/alerts?before=x", "", http.StatusBadRequest},
		{"a big page", http.MethodGet, "/api/alerts?limit=1000", "", http.StatusBadRequest},
	} {
		if rec := send(t, h, tt.method, tt.target, laptopIP, tt.body); rec.Code != tt.want {
			t.Errorf("%s: %s %s = %d %s, want %d", tt.name, tt.method, tt.target, rec.Code, rec.Body, tt.want)
		}
	}

	// Resolved alerts are listed, the latest opened first, a page at a time.
	for i := range 3 {
		a, err := st.AddAlert(t.Context(), store.Alert{Key: fmt.Sprint("old", i), Source: "check", Severity: "warning", Title: fmt.Sprint("Old ", i), OpenedAt: time.Now(), LastSeen: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		a.ResolvedAt = time.Now()
		if err := st.SaveAlert(t.Context(), a); err != nil {
			t.Fatal(err)
		}
	}
	var list api.AlertList
	get(t, h, "/api/alerts?limit=2", phoneIP, &list)
	if len(list.Alerts) != 2 || !list.More || list.Alerts[0].Title != "Old 2" || list.Alerts[0].State != api.AlertStateResolved {
		t.Errorf("GET /api/alerts?limit=2 = %+v, want the 2 latest resolved, and more", list)
	}
	get(t, h, fmt.Sprintf("/api/alerts?before=%d", list.Alerts[1].ID), phoneIP, &list)
	if len(list.Alerts) != 1 || list.More || list.Alerts[0].Title != "Old 0" {
		t.Errorf("the next page = %+v, want the oldest, and no more", list)
	}
}

func TestEventRoutes(t *testing.T) {
	t.Parallel()
	st := checksStore(t)
	tn := testTailnet()
	h := api.New(withStore(tn, st, api.Options{}))
	// What admins do is in the activity log.
	if rec := send(t, h, http.MethodPut, "/api/access/users/carol@example.com", laptopIP, `{"role":"viewer"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT access = %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodPost, "/api/checks", laptopIP, `{"name":"DB","kind":"tcp","target":"db:5432"}`); rec.Code != http.StatusCreated {
		t.Fatalf("POST checks = %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, h, http.MethodPut, "/api/devices/laptop/settings", laptopIP, `{"displayName":"Work laptop"}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT settings = %d %s", rec.Code, rec.Body)
	}

	var admin api.EventList
	get(t, h, "/api/events", laptopIP, &admin)
	var got []string
	for _, e := range admin.Events {
		got = append(got, string(e.Kind)+": "+e.Message+" by "+e.Actor)
	}
	want := []string{
		"device: Changed the settings of Work laptop by " + alice,
		"check: Added the check DB by " + alice,
		"access: Made carol@example.com a viewer by " + alice,
	}
	if !slices.Equal(got, want) || admin.More {
		t.Errorf("an admin's events = %q, more %v, want %q", got, admin.More, want)
	}
	if admin.Events[0].DeviceID != "laptop" {
		t.Errorf("the settings event is about %q, want the laptop", admin.Events[0].DeviceID)
	}

	// Viewers don't see who did what, or who has a role.
	var viewer api.EventList
	get(t, h, "/api/events", phoneIP, &viewer)
	if len(viewer.Events) != 2 || viewer.Events[0].Actor != "" || viewer.Events[1].Kind != api.EventKindCheck {
		t.Errorf("a viewer's events = %+v, want the device and check events, without actors", viewer.Events)
	}

	var page api.EventList
	get(t, h, "/api/events?limit=1", laptopIP, &page)
	if len(page.Events) != 1 || !page.More {
		t.Errorf("a page of 1 = %+v, want one event and more", page)
	}
	get(t, h, fmt.Sprintf("/api/events?before=%d", page.Events[0].ID), laptopIP, &page)
	if len(page.Events) != 2 || page.More {
		t.Errorf("the next page = %+v, want the 2 older events", page)
	}
	get(t, h, fmt.Sprintf("/api/events?after=%d", admin.Events[1].ID), laptopIP, &page)
	if len(page.Events) != 1 || page.Events[0].ID != admin.Events[0].ID {
		t.Errorf("events after the second = %+v, want the newest", page)
	}
	get(t, h, "/api/events?kind=check", laptopIP, &page)
	if len(page.Events) != 1 || page.Events[0].Kind != api.EventKindCheck {
		t.Errorf("check events = %+v, want the one", page)
	}
	get(t, h, "/api/events?device=laptop", laptopIP, &page)
	if len(page.Events) != 1 || page.Events[0].DeviceID != "laptop" {
		t.Errorf("the laptop's events = %+v, want the one", page)
	}
}

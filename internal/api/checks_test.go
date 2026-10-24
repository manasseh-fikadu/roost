package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/checks"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/tailnet/tailnettest"
)

// checksStore returns a testStore without its default checks.
func checksStore(t *testing.T) *store.Store {
	t.Helper()
	st := testStore(t)
	defaults, err := st.Checks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range defaults {
		if err := st.DeleteCheck(t.Context(), c.ID); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// loadedRunner returns a runner for st that has loaded its checks.
func loadedRunner(t *testing.T, st *store.Store, tn *tailnettest.Tailnet) *checks.Runner {
	t.Helper()
	r := checks.NewRunner(st, checks.Options{Dial: tn.Dial})
	if err := r.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r
}

func addStoredCheck(t *testing.T, st *store.Store, c store.Check) store.Check {
	t.Helper()
	c, err := st.AddCheck(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func addResults(t *testing.T, st *store.Store, id string, start time.Time, ok ...bool) {
	t.Helper()
	for i, pass := range ok {
		res := store.CheckResult{CheckID: id, At: start.Add(time.Duration(i) * time.Minute), OK: pass, Latency: 12 * time.Millisecond, Detail: "HTTP 500 Internal Server Error"}
		if pass {
			res.Detail = "HTTP 200 OK"
		}
		if err := st.AddCheckResult(t.Context(), res); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSnapshot_checks(t *testing.T) {
	t.Parallel()
	st := checksStore(t)
	now := time.Now()
	minute := time.Minute
	api1 := addStoredCheck(t, st, store.Check{
		Name: "API", Kind: "http", Target: "https://api.example.com:8443/private/health?item=42", Group: "Apps",
		Interval: minute, Timeout: 5 * time.Second, ExpectStatus: 204, Insecure: true,
	})
	addResults(t, st, api1.ID, now.Add(-3*minute), true, true, false)
	db := addStoredCheck(t, st, store.Check{Name: "DB", Kind: "tcp", Target: "db.example.com:5432", Group: "Apps", Interval: minute, Timeout: time.Second})
	addResults(t, st, db.ID, now.Add(-2*minute), false, true)
	addStoredCheck(t, st, store.Check{Name: "Cache", Kind: "tcp", Target: "cache:6379", Group: "Apps", Interval: minute, Timeout: time.Second})
	addStoredCheck(t, st, store.Check{Name: "Old", Kind: "dns", Target: "old.example.com", Group: "Apps", Interval: minute, Timeout: time.Second, Paused: true})

	tn := testTailnet()
	status, _ := tn.Status(t.Context())
	status.Peers[1].Online = false // the phone
	pings := map[string]tailnet.PingRecord{
		"laptop": {At: now, Result: &tailnet.PingResult{Latency: 4 * time.Millisecond, DERPRegion: "fra"}},
		"server": {At: now, Err: tailnet.ErrNoReply, Misses: 2},
		"tablet": {At: now, Err: tailnet.ErrNoReply, Misses: 1},
	}
	runner := loadedRunner(t, st, tn)
	feed := api.NewFeed(api.FeedOptions{
		Tailnet: func() tailnet.Snapshot { return tailnet.Snapshot{Status: status, Pings: pings} },
		Checks:  runner,
		Store:   st,
	})
	snap := feed.Latest()

	type summary struct {
		id, name, group string
		state           api.CheckState
		reason          string
		results         int
	}
	// The snapshot has each check's last result, and GET /api/checks/recent
	// has the ones before.
	var recent api.RecentResults
	get(t, api.New(withStore(t, tn, st, api.Options{Checks: runner})), "/api/checks/recent", phoneIP, &recent)
	var got []summary
	for _, c := range snap.Checks {
		results := recent.Checks[c.ID]
		if n := len(results); n > 0 && jsonOf(t, c.Last) != jsonOf(t, results[n-1]) {
			t.Errorf("%s's last result = %s, want %s", c.Name, jsonOf(t, c.Last), jsonOf(t, results[n-1]))
		}
		got = append(got, summary{c.ID, c.Name, c.Group, c.State, c.StateReason, len(results)})
	}
	want := []summary{
		{api1.ID, "API", "Apps", api.CheckStateFailing, "HTTP 500 Internal Server Error", 3},
		{"", "Cache", "Apps", api.CheckStatePending, "Waiting for the first result", 0},
		{db.ID, "DB", "Apps", api.CheckStatePassing, "HTTP 200 OK", 2},
		{"", "Old", "Apps", api.CheckStatePaused, "Paused", 0},
		// Then a ping check for each peer, in the order of the devices.
		{"ping:server", "Ping server", "Tailnet", api.CheckStateFailing, "Missed 2 pings in a row", 1},
		{"ping:laptop", "Ping laptop", "Tailnet", api.CheckStatePassing, "Relayed through DERP fra", 1},
		{"ping:phone", "Ping phone", "Tailnet", api.CheckStateUnavailable, "Offline", 0},
		{"ping:tablet", "Ping tablet", "Tailnet", api.CheckStatePassing, "Missed the latest ping", 1},
	}
	for i := range got {
		if want[i].id == "" {
			got[i].id = ""
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("checks =\n%+v\nwant\n%+v", got, want)
	}

	a := snap.Checks[0]
	if a.Kind != api.CheckKindHTTP || a.IntervalSec != 60 || a.TimeoutMs != 5000 || a.ExpectStatus != 204 || !a.Insecure || a.Failures != 1 {
		t.Errorf("the API check = %+v, want its settings and 1 failure", a)
	}
	if a.Uptime == nil || *a.Uptime != 66.67 {
		t.Errorf("the API check's uptime = %v, want 66.67", a.Uptime)
	}
	if r := a.Last; r == nil || r.OK || r.LatencyMs == nil || *r.LatencyMs != 12 || !r.At.Equal(now.Add(-minute).Truncate(time.Millisecond)) {
		t.Errorf("the API check's latest result = %+v, want the failure a minute ago", r)
	}
	if a.Target != api1.Target || a.Endpoint != "https://api.example.com:8443" {
		t.Errorf("the API check's target = %q and endpoint %q, want the URL and only its scheme, host and port", a.Target, a.Endpoint)
	}
	laptop := snap.Checks[5]
	if laptop.Kind != api.CheckKindPing || laptop.Target != laptopIP.String() || laptop.DeviceID != "laptop" ||
		laptop.IntervalSec != 60 || laptop.TimeoutMs != 5000 {
		t.Errorf("the laptop's ping check = %+v, want a ping of its IP every minute", laptop)
	}

	// Three of the five live checks pass. The others aren't live.
	if score := snap.Health.Score; score == nil || *score != 60 {
		t.Errorf("health score = %v, want 60", score)
	}
	if h := snap.Health; h.Label != api.HealthLabelCritical || h.Passing != 3 || h.Failing != 2 || h.Unavailable != 3 || h.Total != 8 {
		t.Errorf("health = %+v, want critical with 3 passing, 2 failing and 3 unavailable of 8", h)
	}

	// Only admins get a target, which can hold a secret.
	viewer, ok := api.Redact(snap).(api.Snapshot)
	if !ok {
		t.Fatalf("Redact() returned a %T", viewer)
	}
	if c := viewer.Checks[0]; c.Target != "" || c.Endpoint != "https://api.example.com:8443" {
		t.Errorf("a viewer gets target %q and endpoint %q, want no target", c.Target, c.Endpoint)
	}
}

func TestHealthScore(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name             string
		passing, failing int
		want             *int
		label            api.HealthLabel
	}{
		{"no live checks", 0, 0, nil, api.HealthLabelUnknown},
		{"every check passes", 3, 0, new(100), api.HealthLabelHealthy},
		{"one in two hundred fails", 199, 1, new(99), api.HealthLabelHealthy},
		{"nine in ten pass", 9, 1, new(90), api.HealthLabelHealthy},
		{"seven in ten pass", 7, 3, new(70), api.HealthLabelDegraded},
		{"two in three pass", 2, 1, new(66), api.HealthLabelCritical},
		{"every check fails", 0, 2, new(0), api.HealthLabelCritical},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := checksStore(t)
			for i := range tt.passing + tt.failing {
				c := addStoredCheck(t, st, store.Check{Name: "c", Kind: "tcp", Target: "db:5432", Group: "g", Interval: time.Minute, Timeout: time.Second})
				addResults(t, st, c.ID, time.Now(), i < tt.passing)
			}
			tn := testTailnet()
			feed := api.NewFeed(api.FeedOptions{
				Tailnet: func() tailnet.Snapshot { return tailnet.Snapshot{} },
				Checks:  loadedRunner(t, st, tn),
				Store:   st,
			})
			h := feed.Latest().Health
			if (h.Score == nil) != (tt.want == nil) || (h.Score != nil && *h.Score != *tt.want) || h.Label != tt.label {
				t.Errorf("health = %v %s, want %v %s", h.Score, h.Label, tt.want, tt.label)
			}
		})
	}
}

func TestChecks_addChangeDelete(t *testing.T) {
	t.Parallel()
	st := checksStore(t)
	tn := testTailnet()
	h := api.New(withStore(t, tn, st, api.Options{Checks: loadedRunner(t, st, tn)}))

	rec := send(t, h, http.MethodPost, "/api/checks", laptopIP,
		`{"name":"API","kind":"http","target":"https://api.example.com/health","group":"Apps","intervalSec":30,"expectStatus":204}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s, want %d", rec.Code, rec.Body, http.StatusCreated)
	}
	var added api.Check
	if err := json.Unmarshal(rec.Body.Bytes(), &added); err != nil {
		t.Fatal(err)
	}
	if added.ID == "" || added.Name != "API" || added.IntervalSec != 30 || added.TimeoutMs != 5000 ||
		added.ExpectStatus != 204 || added.State != api.CheckStatePending || added.Last != nil {
		t.Errorf("POST answered %+v, want the new check, waiting for its first result", added)
	}

	rec = send(t, h, http.MethodPut, "/api/checks/"+added.ID, laptopIP,
		`{"name":"API health","kind":"http","target":"https://api.example.com/healthz","paused":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s, want %d", rec.Code, rec.Body, http.StatusOK)
	}
	var changed api.Check
	if err := json.Unmarshal(rec.Body.Bytes(), &changed); err != nil {
		t.Fatal(err)
	}
	if changed.ID != added.ID || changed.Name != "API health" || changed.Group != "Custom" || changed.IntervalSec != 60 ||
		changed.ExpectStatus != 0 || changed.State != api.CheckStatePaused {
		t.Errorf("PUT answered %+v, want the check replaced, with defaults for what was left out", changed)
	}
	stored, err := st.Checks(t.Context())
	if err != nil || len(stored) != 1 || stored[0].Target != "https://api.example.com/healthz" || !stored[0].Paused {
		t.Errorf("the store has %+v, %v, want the changed check", stored, err)
	}

	if rec := send(t, h, http.MethodDelete, "/api/checks/"+added.ID, laptopIP, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s, want %d", rec.Code, rec.Body, http.StatusNoContent)
	}
	if stored, err := st.Checks(t.Context()); err != nil || len(stored) != 0 {
		t.Errorf("after DELETE, the store has %+v, %v, want no checks", stored, err)
	}

	for _, tt := range []struct {
		name, method, target, body string
		wantCode                   int
		wantErr                    string
	}{
		{"add without a name", http.MethodPost, "/api/checks", `{"kind":"tcp","target":"db:5432"}`, http.StatusBadRequest, "the check needs a name"},
		{"add a ping check", http.MethodPost, "/api/checks", `{"name":"p","kind":"ping","target":"100.64.0.2"}`,
			http.StatusBadRequest, "the check must be an HTTP, TCP or DNS check"},
		{"add with an unknown field", http.MethodPost, "/api/checks", `{"name":"p","method":"HEAD"}`, http.StatusBadRequest, ""},
		{"change a deleted check", http.MethodPut, "/api/checks/" + added.ID, `{"name":"DB","kind":"tcp","target":"db:5432"}`,
			http.StatusNotFound, "no check has this ID"},
		{"change a bad check", http.MethodPut, "/api/checks/" + added.ID, `{"name":"DB","kind":"tcp","target":"db"}`,
			http.StatusBadRequest, "the target must be a host and a port, such as db.example.com:5432"},
		{"change a ping check", http.MethodPut, "/api/checks/ping:laptop", `{"name":"DB","kind":"tcp","target":"db:5432"}`,
			http.StatusBadRequest, "Roost pings every peer on its own, so its ping checks can't be changed or run"},
		{"delete a deleted check", http.MethodDelete, "/api/checks/" + added.ID, "", http.StatusNotFound, "no check has this ID"},
		{"delete a ping check", http.MethodDelete, "/api/checks/ping:laptop", "", http.StatusBadRequest,
			"Roost pings every peer on its own, so its ping checks can't be changed or run"},
		{"run a deleted check", http.MethodPost, "/api/checks/" + added.ID + "/run", "", http.StatusNotFound, "no check has this ID"},
		{"run a ping check", http.MethodPost, "/api/checks/ping:laptop/run", "", http.StatusBadRequest,
			"Roost pings every peer on its own, so its ping checks can't be changed or run"},
	} {
		rec := send(t, h, tt.method, tt.target, laptopIP, tt.body)
		var body api.ErrorBody
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != tt.wantCode || (tt.wantErr != "" && body.Error != tt.wantErr) {
			t.Errorf("%s: %s %s = %d %q, want %d %q", tt.name, tt.method, tt.target, rec.Code, body.Error, tt.wantCode, tt.wantErr)
		}
	}
}

func TestChecks_run(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := checksStore(t)
		synctest.Wait()
		tn := testTailnet()
		tn.SetDialer(func(_ context.Context, _, addr string) (net.Conn, error) {
			if addr != "db.example.com:5432" {
				return nil, errors.New("connection refused")
			}
			client, server := net.Pipe()
			_ = server.Close()
			return client, nil
		})
		runner := loadedRunner(t, st, tn)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			runner.Run(ctx)
		}()
		t.Cleanup(func() {
			cancel()
			<-done
		})
		c, err := runner.Add(t.Context(), store.Check{
			Name: "DB", Kind: "tcp", Target: "db.example.com:5432", Group: "g", Interval: time.Hour, Timeout: time.Second, Paused: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		h := api.New(withStore(t, tn, st, api.Options{Checks: runner}))

		rec := send(t, h, http.MethodPost, "/api/checks/"+c.ID+"/run", laptopIP, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("POST run = %d %s, want %d", rec.Code, rec.Body, http.StatusOK)
		}
		var res api.CheckResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if !res.OK || res.Detail != "Connected" || !res.At.Equal(time.Now()) {
			t.Errorf("POST run answered %+v, want a pass now", res)
		}
	})
}

func TestChecks_history(t *testing.T) {
	t.Parallel()
	st := checksStore(t)
	tn := testTailnet()
	now := time.Now()
	c := addStoredCheck(t, st, store.Check{Name: "API", Kind: "tcp", Target: "db:5432", Group: "g", Interval: time.Minute, Timeout: time.Second})
	// Four results in one 5-minute step.
	addResults(t, st, c.ID, now.Add(-90*time.Minute).Truncate(5*time.Minute), true, false, true, true)
	err := st.AddPings(t.Context(), []store.Ping{
		{DeviceID: "laptop", At: now.Add(-30 * time.Minute), Replied: true, RTT: 8 * time.Millisecond},
		{DeviceID: "laptop", At: now.Add(-30 * time.Minute), Replied: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []float64{100, 50} {
		if err := st.AddHealth(t.Context(), store.HealthSample{At: now.Add(-10 * time.Minute), Score: s}); err != nil {
			t.Fatal(err)
		}
	}
	h := api.New(withStore(t, tn, st, api.Options{Checks: loadedRunner(t, st, tn)}))

	var history api.CheckHistory
	get(t, h, "/api/checks/"+c.ID+"/history?range=24h", phoneIP, &history)
	if history.Step != (5*time.Minute).Milliseconds() || len(history.Uptime) != 1 || len(history.Latency) != 1 {
		t.Fatalf("history = %+v, want one 5-minute step", history)
	}
	if up, latency := history.Uptime[0].V, history.Latency[0].V; up == nil || *up != 75 || latency == nil || *latency != 12 {
		t.Errorf("the step has uptime %v and latency %v, want 75%% and 12 ms", up, latency)
	}

	var pings api.CheckHistory
	get(t, h, "/api/checks/ping:laptop/history?range=1h", phoneIP, &pings)
	if len(pings.Uptime) != 1 || *pings.Uptime[0].V != 50 || *pings.Latency[0].V != 8 {
		t.Errorf("the laptop's ping history = %+v, want one step, half answered in 8 ms", pings)
	}

	var health api.HealthHistory
	get(t, h, "/api/health/history?range=1h", phoneIP, &health)
	if len(health.Score) != 1 || *health.Score[0].V != 50 {
		t.Errorf("health history = %+v, want the one sample", health)
	}

	for _, target := range []string{"/api/checks/missing/history", "/api/checks/ping:toaster/history"} {
		if rec := serve(h, newRequest(t, http.MethodGet, target, phoneIP)); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want %d", target, rec.Code, http.StatusNotFound)
		}
	}
	if rec := serve(h, newRequest(t, http.MethodGet, "/api/health/history?range=2d", phoneIP)); rec.Code != http.StatusBadRequest {
		t.Errorf("GET a health history of 2d = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// get sends a GET request and decodes the JSON it answers with into v.
func get(t *testing.T, h http.Handler, target string, from netip.Addr, v any) {
	t.Helper()
	rec := serve(h, newRequest(t, http.MethodGet, target, from))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s, want %d", target, rec.Code, rec.Body, http.StatusOK)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatal(err)
	}
}

// The feed stores the health score once a minute.
func TestFeed_recordsHealth(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := checksStore(t)
		synctest.Wait()
		tn := testTailnet()
		c := addStoredCheck(t, st, store.Check{Name: "DB", Kind: "tcp", Target: "db:5432", Group: "g", Interval: time.Hour, Timeout: time.Second})
		addResults(t, st, c.ID, time.Now(), true)
		feed := api.NewFeed(api.FeedOptions{
			Tailnet: func() tailnet.Snapshot { return tailnet.Snapshot{} },
			Checks:  loadedRunner(t, st, tn),
			Store:   st,
		})
		go feed.Run(t.Context())
		start := time.Now()
		time.Sleep(3*time.Minute + 30*time.Second)
		synctest.Wait()

		buckets, err := st.HealthHistory(t.Context(), start, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		var minutes []string
		for _, b := range buckets {
			minutes = append(minutes, fmt.Sprintf("%v=%g", b.Start.Sub(start), b.Score))
		}
		if want := []string{"1m0s=100", "2m0s=100", "3m0s=100"}; !slices.Equal(minutes, want) {
			t.Errorf("health samples = %q, want one a minute", minutes)
		}
	})
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

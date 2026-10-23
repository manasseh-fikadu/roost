package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/agent"
	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/hostmetrics"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

const agentToken = "test-agent-token"

// laptopBeat is a heartbeat as the agent sends it, with its processes.
var laptopBeat = `{"v":1,"agentVersion":"` + agent.Version + `","hostname":"laptop","os":"macOS","osVersion":"macOS 26.1",
	"kernel":"25.1.0","model":"MacBookPro18,1","arch":"arm64",
	"metrics":{"cpu":12.5,"cores":10,"load":[1.5,1.25,1],"memUsed":8000,"memTotal":16000,"diskUsed":100,"diskTotal":1000,
	"netRxTotal":1000,"netTxTotal":500,"battery":{"pct":81,"charging":false,"onAc":false},"uptime":7200,"procs":512},
	"usage":{"groups":[["app:Safari",10.5,4000,6,["Safari"]]],"procs":[[42,"Safari","alice",10.5,4000,"app:Safari","/Applications/Safari.app/Contents/MacOS/Safari --profile work"]],"total":[12.5,6000,512]}}`

// sendBeat sends a heartbeat from the device at ip with a token.
func sendBeat(t *testing.T, h http.Handler, from netip.Addr, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequestBody(t, http.MethodPost, "/api/agent/heartbeat", from, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	return serve(h, req)
}

func TestHeartbeat(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		from     netip.Addr
		token    string
		body     string
		measured bool // Roost measures its own machine
		wantCode int
		// wantID is the ID the report is kept under, if it's taken.
		wantID string
	}{
		{name: "a laptop", from: laptopIP, token: agentToken, body: laptopBeat, wantCode: http.StatusNoContent, wantID: "laptop"},
		{name: "a tagged server", from: serverIP, token: agentToken, body: laptopBeat, wantCode: http.StatusNoContent, wantID: "server"},
		{
			name: "the hub, which Roost measures", from: roostIP, token: agentToken, body: laptopBeat, measured: true,
			wantCode: http.StatusConflict,
		},
		{name: "the hub, which Roost can't measure", from: roostIP, token: agentToken, body: laptopBeat, wantCode: http.StatusNoContent, wantID: store.HubID},
		{name: "a stranger", from: strangerIP, token: agentToken, body: laptopBeat, wantCode: http.StatusForbidden},
		{name: "the wrong token", from: laptopIP, token: "nope", body: laptopBeat, wantCode: http.StatusUnauthorized},
		{name: "another format", from: laptopIP, token: agentToken, body: `{"v":2}`, wantCode: http.StatusBadRequest},
		{name: "not JSON", from: laptopIP, token: agentToken, body: `{"v":1`, wantCode: http.StatusBadRequest},
		{name: "a field Roost doesn't know", from: laptopIP, token: agentToken, body: `{"v":1,"color":"red"}`, wantCode: http.StatusNoContent, wantID: "laptop"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := testStore(t)
			tn := testTailnet()
			opts := withStore(tn, st, api.Options{AgentToken: agentToken})
			if tt.measured {
				opts.Feed = api.NewFeed(api.FeedOptions{
					Tailnet: opts.Snapshot, Store: st, Agents: opts.Agents,
					Host: func() hostmetrics.Reading { return hostmetrics.Reading{} },
				})
			}
			h := api.New(opts)
			rec := sendBeat(t, h, tt.from, tt.token, tt.body)
			if rec.Code != tt.wantCode {
				t.Fatalf("POST = %d %s, want %d", rec.Code, rec.Body, tt.wantCode)
			}
			reports := opts.Agents.Reports()
			if tt.wantID == "" {
				if len(reports) != 0 {
					t.Errorf("reports = %v, want none", reports)
				}
				return
			}
			if _, ok := reports[tt.wantID]; !ok || len(reports) != 1 {
				t.Errorf("reports = %v, want one for %s", reports, tt.wantID)
			}
		})
	}
}

// Without a token, Roost takes no heartbeats.
func TestHeartbeat_noToken(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{}))
	if rec := sendBeat(t, h, laptopIP, "", laptopBeat); rec.Code != http.StatusUnauthorized {
		t.Errorf("POST with an empty token = %d, want 401", rec.Code)
	}
}

// A device whose node Tailscale knows, but that wasn't in Roost's last
// status, tries again later.
func TestHeartbeat_unknownDevice(t *testing.T) {
	t.Parallel()
	tn := testTailnet()
	opts := withTailnet(t, tn, api.Options{AgentToken: agentToken})
	opts.Snapshot = func() tailnet.Snapshot {
		return tailnet.Snapshot{Status: &tailnet.Status{
			State: tailnet.StateRunning,
			Self:  tailnet.Device{ID: "roost", DNSName: roostHost, IPs: []netip.Addr{roostIP}, Online: true},
		}}
	}
	if rec := sendBeat(t, api.New(opts), laptopIP, agentToken, laptopBeat); rec.Code != http.StatusNotFound {
		t.Errorf("POST = %d %s, want 404", rec.Code, rec.Body)
	}
}

// A heartbeat shows up in the snapshot, and its processes in the device's
// usage, with command lines only for admins.
func TestHeartbeat_shows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := testStore(t)
		synctest.Wait()
		tn := testTailnet()
		opts := withStore(tn, st, api.Options{AgentToken: agentToken})
		go opts.Feed.Run(t.Context())
		h := api.New(opts)
		events := openStream(t, tn, h, laptopIP, true)
		synctest.Wait()
		nextSnapshot(t, events)

		if rec := sendBeat(t, h, laptopIP, agentToken, laptopBeat); rec.Code != http.StatusNoContent {
			t.Fatalf("POST = %d %s", rec.Code, rec.Body)
		}
		time.Sleep(api.FeedGap)
		synctest.Wait()
		d := findDevice(t, nextSnapshot(t, events), "laptop")
		if d.MeasuredBy != api.MeasuredByAgent || d.Agent == nil || d.Agent.Version != agent.Version || d.Agent.Stale || d.Agent.Outdated {
			t.Errorf("laptop measured by %q, agent %+v; want a fresh agent of version %s", d.MeasuredBy, d.Agent, agent.Version)
		}
		if m := d.Metrics; m == nil || *m.CPU != 12.5 || m.Battery == nil || m.Battery.Percent != 81 || m.Memory.Total != 16000 {
			t.Errorf("laptop metrics = %+v, want the heartbeat's", d.Metrics)
		}
		if d.Info == nil || d.Info.Model != "MacBookPro18,1" || d.Info.Cores != 10 {
			t.Errorf("laptop info = %+v, want the heartbeat's", d.Info)
		}

		for _, tt := range []struct {
			from netip.Addr
			cmd  string
		}{
			{laptopIP, "/Applications/Safari.app/Contents/MacOS/Safari --profile work"},
			{phoneIP, ""},
		} {
			rec := serve(h, newRequest(t, http.MethodGet, "/api/devices/laptop/usage", tt.from))
			var got api.DeviceUsage
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
				t.Fatalf("GET usage = %d %s", rec.Code, rec.Body)
			}
			s := got.Sample
			if s == nil || len(s.Groups) != 1 || s.Groups[0].Name != "Safari" || s.Cores != 10 || len(s.Procs) != 1 || s.Procs[0].Cmd != tt.cmd {
				t.Errorf("usage from %v = %+v, want Safari, with command %q", tt.from, s, tt.cmd)
			}
		}
	})
}

func TestSnapshot_agents(t *testing.T) {
	t.Parallel()
	now := time.Now()
	beat := func(reg *agents.Registry, id, version string, at time.Time, metrics string) {
		t.Helper()
		var hb agents.Heartbeat
		body := `{"v":1,"agentVersion":"` + version + `","model":"Model","metrics":` + metrics + `}`
		if err := json.Unmarshal([]byte(body), &hb); err != nil {
			t.Fatal(err)
		}
		if _, err := reg.Record(t.Context(), id, hb, at); err != nil {
			t.Fatal(err)
		}
	}
	status := &tailnet.Status{
		State: tailnet.StateRunning,
		Self:  tailnet.Device{ID: "roost", DNSName: roostHost, OS: "macOS", Online: true},
		Peers: []tailnet.Device{
			{ID: "fresh", DNSName: "fresh.example.ts.net", OS: "linux", Online: true},
			{ID: "stale", DNSName: "stale.example.ts.net", OS: "linux", Online: true},
			{ID: "asleep", DNSName: "asleep.example.ts.net", OS: "macOS"},
			{ID: "old", DNSName: "old.example.ts.net", OS: "linux", Online: true},
			{ID: "host", DNSName: "host.example.ts.net", OS: "macOS", Online: true, SameMachine: true},
			{ID: "none", DNSName: "none.example.ts.net", OS: "linux", Online: true},
		},
	}
	for _, measured := range []bool{false, true} {
		st := testStore(t)
		reg := agents.NewRegistry(st, nil)
		beat(reg, "fresh", agent.Version, now.Add(-10*time.Second),
			`{"cpu":5,"diskUsed":90,"diskTotal":100,"battery":{"pct":50,"charging":true}}`)
		beat(reg, "stale", agent.Version, now.Add(-2*time.Minute), `{"cpu":99}`)
		beat(reg, "asleep", agent.Version, now.Add(-time.Hour), `{"cpu":1}`)
		beat(reg, "old", "1.0.0", now, `{"cpu":1}`)
		// An agent on the machine Roost runs on, which Roost can't measure
		// on macOS.
		beat(reg, store.HubID, agent.Version, now, `{"cpu":42}`)
		opts := api.FeedOptions{Tailnet: func() tailnet.Snapshot { return tailnet.Snapshot{Status: status} }, Store: st, Agents: reg}
		if measured {
			opts.Host = func() hostmetrics.Reading {
				return hostmetrics.Reading{Sample: &hostmetrics.Sample{At: now, CPU: new(7.0)}}
			}
		}
		snap := api.NewFeed(opts).Latest()

		for _, tt := range []struct {
			id       string
			by       api.MeasuredBy
			cpu      float64
			stale    bool
			outdated bool
			status   api.Status
			reason   string
		}{
			{id: "fresh", by: api.MeasuredByAgent, cpu: 5, status: api.StatusAttention, reason: "Disk 90% full"},
			{id: "stale", by: api.MeasuredByAgent, cpu: 99, stale: true, status: api.StatusAttention, reason: "Agent stopped reporting"},
			{id: "asleep", by: api.MeasuredByAgent, cpu: 1, status: api.StatusOffline, reason: "Not connected to the tailnet"},
			{id: "old", by: api.MeasuredByAgent, cpu: 1, outdated: true, status: api.StatusOnline},
			{id: "roost", by: api.MeasuredByAgent, cpu: 42, status: api.StatusOnline},
			{id: "host", by: api.MeasuredByAgent, cpu: 42, status: api.StatusOnline},
			{id: "none", status: api.StatusOnline},
		} {
			if measured && (tt.id == "roost" || tt.id == "host") {
				tt.by, tt.cpu = api.MeasuredByHub, 7
			}
			d := findDevice(t, snap, tt.id)
			if d.MeasuredBy != tt.by || d.Status != tt.status || (tt.reason != "" && d.StatusReason != tt.reason) {
				t.Errorf("measured %v: %s is measured by %q, %s %q; want %q, %s %q",
					measured, tt.id, d.MeasuredBy, d.Status, d.StatusReason, tt.by, tt.status, tt.reason)
			}
			if tt.by == "" {
				if d.Metrics != nil || d.Agent != nil {
					t.Errorf("%s has metrics %+v and agent %+v, want neither", tt.id, d.Metrics, d.Agent)
				}
				continue
			}
			if d.Metrics == nil || *d.Metrics.CPU != tt.cpu {
				t.Errorf("measured %v: %s metrics = %+v, want CPU %v", measured, tt.id, d.Metrics, tt.cpu)
			}
			if (d.Agent != nil) != (tt.by == api.MeasuredByAgent) {
				t.Errorf("measured %v: %s agent = %+v, want one only if it's measured by an agent", measured, tt.id, d.Agent)
			}
			if a := d.Agent; a != nil && (a.Stale != tt.stale || a.Outdated != tt.outdated) {
				t.Errorf("%s agent = %+v, want stale %v, outdated %v", tt.id, a, tt.stale, tt.outdated)
			}
		}
		if b := findDevice(t, snap, "fresh").Metrics.Battery; b == nil || b.Percent != 50 || !b.Charging {
			t.Errorf("fresh battery = %+v, want 50%% and charging", b)
		}
	}
}

func TestAgentSetup(t *testing.T) {
	t.Parallel()
	opts := withTailnet(t, testTailnet(), api.Options{AgentToken: agentToken, AppURL: "https://" + roostHost + "/"})
	h := api.New(opts)
	rec := serve(h, newRequest(t, http.MethodGet, "/api/agent", laptopIP))
	var got api.AgentSetup
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("GET /api/agent = %d %s", rec.Code, rec.Body)
	}
	script := "https://" + roostHost + "/api/agent/install.sh"
	want := api.AgentSetup{
		Version:          agent.Version,
		InstallCommand:   "curl -fsSL " + script + " | ROOST_AGENT_TOKEN=" + agentToken + " bash",
		UninstallCommand: "curl -fsSL " + script + " | bash -s -- --uninstall",
	}
	if got != want {
		t.Errorf("GET /api/agent = %+v, want %+v", got, want)
	}
}

func TestInstallScript(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, appURL, want string
	}{
		{"with the app's URL", "https://" + roostHost, "\nSERVER_URL='https://" + roostHost + "'\n"},
		// The request came over plain HTTP.
		{"from the request", "", "\nSERVER_URL='http://" + roostHost + "'\n"},
	} {
		h := api.New(withTailnet(t, testTailnet(), api.Options{AppURL: tt.appURL}))
		// A tagged server can't sign in, but can get the script.
		rec := serve(h, newRequest(t, http.MethodGet, "/api/agent/install.sh", serverIP))
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/x-shellscript") {
			t.Fatalf("%s: GET = %d %s", tt.name, rec.Code, rec.Header().Get("Content-Type"))
		}
		if body := rec.Body.String(); !strings.Contains(body, tt.want) || strings.Contains(body, agentToken) {
			t.Errorf("%s: the script doesn't set %q, or holds the token", tt.name, tt.want)
		}
	}
}

func TestForgetAgent(t *testing.T) {
	t.Parallel()
	opts := withTailnet(t, testTailnet(), api.Options{AgentToken: agentToken})
	h := api.New(opts)
	if rec := sendBeat(t, h, laptopIP, agentToken, laptopBeat); rec.Code != http.StatusNoContent {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	for _, tt := range []struct {
		device string
		want   int
	}{
		{"laptop", http.StatusNoContent},
		{"laptop", http.StatusNotFound},
		{"toaster", http.StatusNotFound},
	} {
		if rec := send(t, h, http.MethodDelete, "/api/devices/"+tt.device+"/agent", laptopIP, ""); rec.Code != tt.want {
			t.Errorf("DELETE %s's agent = %d %s, want %d", tt.device, rec.Code, rec.Body, tt.want)
		}
	}
	if _, ok := opts.Agents.Reports()["laptop"]; ok {
		t.Error("the laptop's agent is still there after it was forgotten")
	}
	if _, _, ok := opts.Usage.Latest("laptop"); ok {
		t.Error("the laptop's usage is still there after its agent was forgotten")
	}
}

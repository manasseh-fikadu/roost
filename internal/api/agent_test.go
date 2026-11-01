package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/agent"
	"github.com/manasseh-fikadu/bield/internal/agents"
	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/hostmetrics"
	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
	"github.com/manasseh-fikadu/bield/internal/tailnet/tailnettest"
	"github.com/manasseh-fikadu/bield/internal/usage"
)

// laptopBeat is a heartbeat as the agent sends it, with its processes.
var laptopBeat = `{"v":1,"agentVersion":"` + agent.Version + `","hostname":"laptop","os":"macOS","osVersion":"macOS 26.1",
	"kernel":"25.1.0","model":"MacBookPro18,1","arch":"arm64",
	"metrics":{"cpu":12.5,"cores":10,"load":[1.5,1.25,1],"memUsed":8000,"memTotal":16000,"diskUsed":100,"diskTotal":1000,
	"netRxTotal":1000,"netTxTotal":500,"battery":{"pct":81,"charging":false,"onAc":false},"uptime":7200,"procs":512},
	"usage":{"groups":[["app:Safari",10.5,4000,6,["Safari"]]],"procs":[[42,"Safari","alice",10.5,4000,"app:Safari","/Applications/Safari.app/Contents/MacOS/Safari --profile work"]],"total":[12.5,6000,512]}}`

// newCode returns a new enrollment code from alice, made at.
func newCode(t *testing.T, st *store.Store, at time.Time) string {
	t.Helper()
	code, _, err := st.NewEnrollmentCode(t.Context(), alice, at)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// enrolled returns the secret of a new agent on the machine with node ID
// node.
func enrolled(t *testing.T, st *store.Store, node string) string {
	t.Helper()
	secret, _, err := st.Enroll(t.Context(), newCode(t, st, time.Now()), node, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

// sendAs sends a request for an agent from the device at ip, with a bearer
// token.
func sendAs(t *testing.T, h http.Handler, method, target string, from netip.Addr, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequestBody(t, method, target, from, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	return serve(h, req)
}

// sendBeat sends a heartbeat from the device at ip with an agent's secret.
func sendBeat(t *testing.T, h http.Handler, from netip.Addr, secret, body string) *httptest.ResponseRecorder {
	t.Helper()
	return sendAs(t, h, http.MethodPost, "/api/agent/heartbeat", from, secret, body)
}

func TestHeartbeat(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		from netip.Addr
		// enrolledOn is the node whose agent's secret the heartbeat has, or
		// "" for a secret Bield doesn't know.
		enrolledOn string
		body       string
		measured   bool // Bield measures its own machine
		wantCode   int
		// wantID is the ID the report is kept under, if it's taken.
		wantID string
	}{
		{name: "a laptop", from: laptopIP, enrolledOn: "laptop", body: laptopBeat, wantCode: http.StatusNoContent, wantID: "laptop"},
		{name: "a tagged server", from: serverIP, enrolledOn: "server", body: laptopBeat, wantCode: http.StatusNoContent, wantID: "server"},
		{
			name: "the hub, which Bield measures", from: bieldIP, enrolledOn: "bield", body: laptopBeat, measured: true,
			wantCode: http.StatusConflict,
		},
		{
			name: "the hub, which Bield can't measure", from: bieldIP, enrolledOn: "bield", body: laptopBeat,
			wantCode: http.StatusNoContent, wantID: store.HubID,
		},
		{name: "another machine's secret", from: phoneIP, enrolledOn: "laptop", body: laptopBeat, wantCode: http.StatusForbidden},
		{name: "a stranger", from: strangerIP, enrolledOn: "laptop", body: laptopBeat, wantCode: http.StatusForbidden},
		{name: "a secret Bield doesn't know", from: laptopIP, body: laptopBeat, wantCode: http.StatusUnauthorized},
		{name: "another format", from: laptopIP, enrolledOn: "laptop", body: `{"v":2}`, wantCode: http.StatusBadRequest},
		{name: "not JSON", from: laptopIP, enrolledOn: "laptop", body: `{"v":1`, wantCode: http.StatusBadRequest},
		{
			name: "a field Bield doesn't know", from: laptopIP, enrolledOn: "laptop", body: `{"v":1,"color":"red"}`,
			wantCode: http.StatusNoContent, wantID: "laptop",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := testStore(t)
			opts := withStore(t, testTailnet(), st, api.Options{})
			if tt.measured {
				opts.Feed = api.NewFeed(api.FeedOptions{
					Tailnet: opts.Snapshot, Store: st, Agents: opts.Agents,
					Host: func() hostmetrics.Reading { return hostmetrics.Reading{} },
				})
			}
			secret := "unknown"
			if tt.enrolledOn != "" {
				secret = enrolled(t, st, tt.enrolledOn)
			}
			rec := sendBeat(t, api.New(opts), tt.from, secret, tt.body)
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

// A device whose node Tailscale knows, but that wasn't in Bield's last
// status, tries again later.
func TestHeartbeat_unknownDevice(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	opts := withStore(t, testTailnet(), st, api.Options{})
	opts.Snapshot = func() tailnet.Snapshot {
		return tailnet.Snapshot{Status: &tailnet.Status{
			State: tailnet.StateRunning,
			Self:  tailnet.Device{ID: "bield", DNSName: bieldHost, IPs: []netip.Addr{bieldIP}, Online: true},
		}}
	}
	if rec := sendBeat(t, api.New(opts), laptopIP, enrolled(t, st, "laptop"), laptopBeat); rec.Code != http.StatusNotFound {
		t.Errorf("POST = %d %s, want 404", rec.Code, rec.Body)
	}
}

// A heartbeat that was on its way when its agent was revoked doesn't bring
// the agent's report back.
func TestHeartbeat_revokedOnTheWay(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	opts := withStore(t, testTailnet(), st, api.Options{})
	h := api.New(opts)
	body := &gatedReader{r: strings.NewReader(laptopBeat), reading: make(chan struct{}), release: make(chan struct{})}
	req := newRequestBody(t, http.MethodPost, "/api/agent/heartbeat", laptopIP, body)
	req.Header.Set("Authorization", "Bearer "+enrolled(t, st, "laptop"))
	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- serve(h, req) }()

	// The router has taken the secret, and the handler is reading the
	// heartbeat.
	<-body.reading
	if rec := send(t, h, http.MethodDelete, "/api/devices/laptop/agent", laptopIP, ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE the laptop's agent = %d %s, want 204", rec.Code, rec.Body)
	}
	close(body.release)
	if rec := <-done; rec.Code != http.StatusUnauthorized {
		t.Errorf("the heartbeat = %d %s, want 401", rec.Code, rec.Body)
	}
	if reports := opts.Agents.Reports(); len(reports) != 0 {
		t.Errorf("reports = %v, want none after the revoke", reports)
	}
}

// A gatedReader closes reading when it's first read, and then waits for
// release before it reads from r.
type gatedReader struct {
	r                io.Reader
	reading, release chan struct{}
	once             sync.Once
}

func (g *gatedReader) Read(p []byte) (int, error) {
	g.once.Do(func() {
		close(g.reading)
		<-g.release
	})
	return g.r.Read(p)
}

// A heartbeat shows up in the snapshot, and its processes in the device's
// usage, with command lines only for admins.
func TestHeartbeat_shows(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := testStore(t)
		synctest.Wait()
		tn := testTailnet()
		opts := withStore(t, tn, st, api.Options{})
		go opts.Feed.Run(t.Context())
		h := api.New(opts)
		events := openStream(t, tn, h, laptopIP, true)
		synctest.Wait()
		nextSnapshot(t, events)

		if rec := sendBeat(t, h, laptopIP, enrolled(t, st, "laptop"), laptopBeat); rec.Code != http.StatusNoContent {
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
		Self:  tailnet.Device{ID: "bield", DNSName: bieldHost, OS: "macOS", Online: true},
		Peers: []tailnet.Device{
			{ID: "fresh", DNSName: "fresh.example.ts.net", OS: "linux", Online: true},
			{ID: "stale", DNSName: "stale.example.ts.net", OS: "linux", Online: true},
			{ID: "asleep", DNSName: "asleep.example.ts.net", OS: "macOS"},
			{ID: "old", DNSName: "old.example.ts.net", OS: "linux", Online: true},
			{ID: "host", DNSName: "host.example.ts.net", OS: "macOS", Online: true, SameMachine: true},
			{ID: "waiting", DNSName: "waiting.example.ts.net", OS: "linux", Online: true},
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
		// An agent on the machine Bield runs on, which Bield can't measure
		// on macOS. It enrolled from the machine's own node, a peer.
		beat(reg, store.HubID, agent.Version, now, `{"cpu":42}`)
		// The stale agent is from before enrollment, and the waiting one
		// hasn't reported yet.
		for _, node := range []string{"fresh", "asleep", "old", "host", "waiting"} {
			enrolled(t, st, node)
		}
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
			agent    bool // it has an agent, even if it hasn't reported
			cpu      float64
			stale    bool
			outdated bool
			enrolled bool
			status   api.Status
			reason   string
		}{
			{id: "fresh", by: api.MeasuredByAgent, cpu: 5, enrolled: true, status: api.StatusAttention, reason: "Disk 90% full"},
			{id: "stale", by: api.MeasuredByAgent, cpu: 99, stale: true, status: api.StatusAttention, reason: "Agent stopped reporting"},
			{id: "asleep", by: api.MeasuredByAgent, cpu: 1, enrolled: true, status: api.StatusOffline, reason: "Not connected to the tailnet"},
			{id: "old", by: api.MeasuredByAgent, cpu: 1, outdated: true, enrolled: true, status: api.StatusOnline},
			{id: "bield", by: api.MeasuredByAgent, cpu: 42, enrolled: true, status: api.StatusOnline},
			{id: "host", by: api.MeasuredByAgent, cpu: 42, enrolled: true, status: api.StatusOnline},
			{id: "waiting", agent: true, enrolled: true, status: api.StatusOnline},
			{id: "none", status: api.StatusOnline},
		} {
			if measured && (tt.id == "bield" || tt.id == "host") {
				tt.by, tt.cpu, tt.enrolled = api.MeasuredByHub, 7, false
			}
			d := findDevice(t, snap, tt.id)
			if d.MeasuredBy != tt.by || d.Status != tt.status || (tt.reason != "" && d.StatusReason != tt.reason) {
				t.Errorf("measured %v: %s is measured by %q, %s %q; want %q, %s %q",
					measured, tt.id, d.MeasuredBy, d.Status, d.StatusReason, tt.by, tt.status, tt.reason)
			}
			if wantAgent := tt.agent || tt.by == api.MeasuredByAgent; (d.Agent != nil) != wantAgent {
				t.Fatalf("measured %v: %s agent = %+v, want one: %v", measured, tt.id, d.Agent, wantAgent)
			}
			if a := d.Agent; a != nil {
				if a.Stale != tt.stale || a.Outdated != tt.outdated || (a.EnrolledAt != nil) != tt.enrolled {
					t.Errorf("%s agent = %+v, want stale %v, outdated %v, enrolled %v", tt.id, a, tt.stale, tt.outdated, tt.enrolled)
				}
				if tt.enrolled && a.EnrolledBy != alice {
					t.Errorf("%s agent enrolled by %q, want %q", tt.id, a.EnrolledBy, alice)
				}
				if (a.LastSeen != nil) != (tt.by == api.MeasuredByAgent) {
					t.Errorf("%s agent last seen %v, want a time only if it reported", tt.id, a.LastSeen)
				}
			}
			if tt.by == "" {
				if d.Metrics != nil {
					t.Errorf("%s has metrics %+v, want none", tt.id, d.Metrics)
				}
				continue
			}
			if d.Metrics == nil || *d.Metrics.CPU != tt.cpu {
				t.Errorf("measured %v: %s metrics = %+v, want CPU %v", measured, tt.id, d.Metrics, tt.cpu)
			}
		}
		if b := findDevice(t, snap, "fresh").Metrics.Battery; b == nil || b.Percent != 50 || !b.Charging {
			t.Errorf("fresh battery = %+v, want 50%% and charging", b)
		}
	}
}

func TestAgentSetup(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{AppURL: "https://" + bieldHost + "/"}))
	rec := serve(h, newRequest(t, http.MethodGet, "/api/agent", laptopIP))
	var got api.AgentSetup
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("GET /api/agent = %d %s", rec.Code, rec.Body)
	}
	want := api.AgentSetup{
		Version:          agent.Version,
		UninstallCommand: "curl -fsSL https://" + bieldHost + "/api/agent/uninstall.sh | bash",
	}
	if got != want {
		t.Errorf("GET /api/agent = %+v, want %+v", got, want)
	}

	// The script holds no secret, so even a tagged server can get it.
	rec = serve(h, newRequest(t, http.MethodGet, "/api/agent/uninstall.sh", serverIP))
	if rec.Code != http.StatusOK || rec.Body.String() != agent.Uninstall() {
		t.Errorf("GET /api/agent/uninstall.sh = %d, want 200 and the script", rec.Code)
	}
}

// An admin's install command works on one machine. That machine trades it
// for a secret it then sends its heartbeats with.
func TestEnrollmentCode(t *testing.T) {
	t.Parallel()
	opts := withTailnet(t, testTailnet(), api.Options{AppURL: "https://" + bieldHost})
	h := api.New(opts)
	before := time.Now()
	rec := send(t, h, http.MethodPost, "/api/agent/codes", laptopIP, "")
	var got api.AgentCode
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/agent/codes = %d %s", rec.Code, rec.Body)
	}
	prefix := "curl -fsSL https://" + bieldHost + "/api/agent/install/"
	code, ok := strings.CutPrefix(got.InstallCommand, prefix)
	code, ok2 := strings.CutSuffix(code, " | bash")
	if !ok || !ok2 || len(code) < 20 {
		t.Fatalf("install command = %q, want %s<code> | bash", got.InstallCommand, prefix)
	}
	if got.CreatedAt.Before(before) || !got.ExpiresAt.Equal(got.CreatedAt.Add(store.EnrollmentCodeLife)) {
		t.Errorf("code made at %v and expires at %v, want an hour after it was made, after %v", got.CreatedAt, got.ExpiresAt, before)
	}

	script := serve(h, newRequest(t, http.MethodGet, "/api/agent/install/"+code, serverIP)).Body.String()
	if !strings.Contains(script, "\nCODE='"+code+"'\n") || !strings.Contains(script, "\nSERVER_URL='https://"+bieldHost+"'\n") {
		t.Fatalf("the install script doesn't have the code and the hub's URL:\n%.300s", script)
	}
	rec = sendAs(t, h, http.MethodPost, "/api/agent/enroll", serverIP, code, "")
	var secret api.AgentSecret
	if err := json.Unmarshal(rec.Body.Bytes(), &secret); err != nil || rec.Code != http.StatusOK || secret.Secret == "" {
		t.Fatalf("POST /api/agent/enroll = %d %s, want a secret", rec.Code, rec.Body)
	}
	if rec := sendBeat(t, h, serverIP, secret.Secret, laptopBeat); rec.Code != http.StatusNoContent {
		t.Errorf("a heartbeat with the secret = %d %s, want 204", rec.Code, rec.Body)
	}

	// Only admins make codes.
	if rec := send(t, h, http.MethodPost, "/api/agent/codes", phoneIP, ""); rec.Code != http.StatusForbidden {
		t.Errorf("POST /api/agent/codes from a viewer = %d, want 403", rec.Code)
	}
}

func TestEnroll(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		from     netip.Addr
		code     string // "fresh", "expired", "laptop's" (used by the laptop) or "unknown"
		measured bool
		// missing leaves the laptop out of Bield's last status.
		missing bool
		// whoIsFails makes Tailscale fail to say who sent the request.
		whoIsFails bool
		wantCode   int
	}{
		{name: "a fresh code", from: laptopIP, code: "fresh", wantCode: http.StatusOK},
		{name: "a tagged server", from: serverIP, code: "fresh", wantCode: http.StatusOK},
		{name: "again from the same machine", from: laptopIP, code: "laptop's", wantCode: http.StatusOK},
		{name: "from another machine", from: phoneIP, code: "laptop's", wantCode: http.StatusForbidden},
		{name: "an expired code", from: laptopIP, code: "expired", wantCode: http.StatusUnauthorized},
		{name: "an unknown code", from: laptopIP, code: "unknown", wantCode: http.StatusUnauthorized},
		{name: "a stranger", from: strangerIP, code: "fresh", wantCode: http.StatusForbidden},
		{name: "the hub, which Bield measures", from: bieldIP, code: "fresh", measured: true, wantCode: http.StatusConflict},
		{name: "the hub, which Bield can't measure", from: bieldIP, code: "fresh", wantCode: http.StatusOK},
		{name: "a machine Bield hasn't seen yet", from: laptopIP, code: "fresh", missing: true, wantCode: http.StatusNotFound},
		// The laptop could use its code again once Tailscale answers.
		{name: "Tailscale fails", from: laptopIP, code: "laptop's", whoIsFails: true, wantCode: http.StatusServiceUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := testStore(t)
			opts := withStore(t, testTailnet(), st, api.Options{})
			if tt.measured {
				opts.Feed = api.NewFeed(api.FeedOptions{
					Tailnet: opts.Snapshot, Store: st, Agents: opts.Agents,
					Host: func() hostmetrics.Reading { return hostmetrics.Reading{} },
				})
			}
			if tt.whoIsFails {
				opts.WhoIs = func(context.Context, string) (*tailnet.Identity, error) {
					return nil, errors.New("tailscaled went away")
				}
			}
			if tt.missing {
				full := opts.Snapshot
				opts.Snapshot = func() tailnet.Snapshot {
					s := full()
					status := *s.Status
					status.Peers = nil
					s.Status = &status
					return s
				}
			}
			h := api.New(opts)
			code := map[string]string{
				"fresh":   newCode(t, st, time.Now()),
				"expired": newCode(t, st, time.Now().Add(-store.EnrollmentCodeLife)),
				"unknown": "UNKNOWN",
			}
			code["laptop's"] = code["fresh"]
			if tt.code == "laptop's" {
				if _, _, err := st.Enroll(t.Context(), code["fresh"], "laptop", time.Now()); err != nil {
					t.Fatal(err)
				}
			}

			rec := sendAs(t, h, http.MethodPost, "/api/agent/enroll", tt.from, code[tt.code], "")
			if rec.Code != tt.wantCode {
				t.Fatalf("POST = %d %s, want %d", rec.Code, rec.Body, tt.wantCode)
			}
			enrollments, err := st.Enrollments(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			wantEnrolled := 0
			if tt.code == "laptop's" {
				wantEnrolled++
			}
			if rec.Code != http.StatusOK {
				if len(enrollments) != wantEnrolled {
					t.Errorf("enrollments = %v, want %d", enrollments, wantEnrolled)
				}
				// A machine that can't enroll yet, or at all, doesn't use the
				// code up.
				if tt.code == "fresh" {
					if err := st.CheckEnrollmentCode(t.Context(), code["fresh"], "tablet", time.Now()); err != nil {
						t.Errorf("the code after a refused enrollment: %v, want it unused", err)
					}
				}
				return
			}
			var got api.AgentSecret
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Secret == "" {
				t.Fatalf("POST = %s, want a secret", rec.Body)
			}
			node, err := st.EnrolledNode(t.Context(), got.Secret)
			if err != nil || len(enrollments) != 1 {
				t.Errorf("the secret is for %q, %v, and there are %d enrollments; want one, for the machine", node, err, len(enrollments))
			}
		})
	}
}

func TestInstallScript(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))
	fresh := newCode(t, st, time.Now())
	used := newCode(t, st, time.Now())
	if _, _, err := st.Enroll(t.Context(), used, "laptop", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		code string
		from netip.Addr
		// want is in the script, unless it refuses with a message that has
		// refusal in it.
		want, refusal string
	}{
		{name: "a fresh code", code: fresh, from: serverIP, want: "\nCODE='" + fresh + "'\n"},
		// The request came over plain HTTP.
		{name: "the hub's URL", code: fresh, from: serverIP, want: "\nSERVER_URL='http://" + bieldHost + "'\n"},
		{name: "a code the machine used", code: used, from: laptopIP, want: "\nCODE='" + used + "'\n"},
		{name: "a code another machine used", code: used, from: phoneIP, refusal: "another machine used this install command"},
		{name: "an expired code", code: newCode(t, st, time.Now().Add(-time.Hour)), from: laptopIP, refusal: "expired"},
		{name: "an unknown code", code: "UNKNOWN", from: laptopIP, refusal: "doesn't know the code"},
	} {
		rec := serve(h, newRequest(t, http.MethodGet, "/api/agent/install/"+tt.code, tt.from))
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/x-shellscript") {
			t.Fatalf("%s: GET = %d %s", tt.name, rec.Code, rec.Header().Get("Content-Type"))
		}
		script := rec.Body.String()
		if tt.refusal == "" {
			if !strings.Contains(script, tt.want) {
				t.Errorf("%s: the script doesn't have %q", tt.name, tt.want)
			}
			continue
		}
		if strings.Contains(script, "agent.sh") {
			t.Errorf("%s: GET answered with the install script, want a script that refuses", tt.name)
		}
		if _, err := exec.LookPath("sh"); err != nil {
			continue
		}
		cmd := exec.CommandContext(t.Context(), "sh")
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		if code := cmd.ProcessState.ExitCode(); code != 1 || !strings.Contains(string(out), tt.refusal) {
			t.Errorf("%s: the script exits %d (%v) and says %q, want 1 and a message with %q", tt.name, code, err, out, tt.refusal)
		}
	}

	// Without the machine, Bield can't tell that the laptop used the code,
	// and asks it to try again rather than saying another machine did.
	opts := withStore(t, testTailnet(), st, api.Options{})
	opts.WhoIs = func(context.Context, string) (*tailnet.Identity, error) {
		return nil, errors.New("tailscaled went away")
	}
	script := serve(api.New(opts), newRequest(t, http.MethodGet, "/api/agent/install/"+used, laptopIP)).Body.String()
	if want := "Tailscale which machine this is, so run the command again"; !strings.Contains(script, want) || strings.Contains(script, "agent.sh") {
		t.Errorf("the install script when Tailscale fails doesn't refuse with %q:\n%.300s", want, script)
	}
}

// Uninstalling the agent unenrolls it, and Bield forgets what it reported.
func TestUnenroll(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	opts := withStore(t, testTailnet(), st, api.Options{})
	h := api.New(opts)
	code := newCode(t, st, time.Now())
	secret, _, err := st.Enroll(t.Context(), code, "laptop", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rec := sendBeat(t, h, laptopIP, secret, laptopBeat); rec.Code != http.StatusNoContent {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	if rec := sendAs(t, h, http.MethodDelete, "/api/agent/enrollment", phoneIP, secret, ""); rec.Code != http.StatusForbidden {
		t.Errorf("DELETE from another machine = %d %s, want 403", rec.Code, rec.Body)
	}
	if rec := sendAs(t, h, http.MethodDelete, "/api/agent/enrollment", laptopIP, secret, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d %s, want 204", rec.Code, rec.Body)
	}
	if _, ok := opts.Agents.Reports()["laptop"]; ok {
		t.Error("the laptop's agent is still there after it was uninstalled")
	}
	if _, _, ok := opts.Usage.Latest("laptop"); ok {
		t.Error("the laptop's usage is still there after its agent was uninstalled")
	}
	if rec := sendBeat(t, h, laptopIP, secret, laptopBeat); rec.Code != http.StatusUnauthorized {
		t.Errorf("a heartbeat after uninstalling = %d %s, want 401", rec.Code, rec.Body)
	}
	// The machine can install the agent again with the same command.
	if rec := sendAs(t, h, http.MethodPost, "/api/agent/enroll", laptopIP, code, ""); rec.Code != http.StatusOK {
		t.Errorf("enrolling again after uninstalling = %d %s, want 200", rec.Code, rec.Body)
	}
}

func TestRevokeAgent(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	opts := withStore(t, testTailnet(), st, api.Options{})
	h := api.New(opts)
	code := newCode(t, st, time.Now())
	secret, _, err := st.Enroll(t.Context(), code, "laptop", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if rec := sendBeat(t, h, laptopIP, secret, laptopBeat); rec.Code != http.StatusNoContent {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	// The server's agent is from before enrollment: it reported, but has no
	// secret.
	var hb agents.Heartbeat
	if err := json.Unmarshal([]byte(laptopBeat), &hb); err != nil {
		t.Fatal(err)
	}
	if _, err := opts.Agents.Record(t.Context(), "server", hb, time.Now()); err != nil {
		t.Fatal(err)
	}
	// The phone's enrolled, and hasn't reported yet.
	enrolled(t, st, "phone")
	// Bield measures its own machine, which has no agent.
	opts.Usage.Record(t.Context(), store.HubID, usage.Sample{At: time.Now(), Source: usage.SourceHub})

	for _, tt := range []struct {
		device string
		want   int
	}{
		{"laptop", http.StatusNoContent},
		{"laptop", http.StatusNotFound},
		{"server", http.StatusNoContent},
		{"phone", http.StatusNoContent},
		{"bield", http.StatusNotFound},
		{"tablet", http.StatusNotFound},
		{"toaster", http.StatusNotFound},
	} {
		if rec := send(t, h, http.MethodDelete, "/api/devices/"+tt.device+"/agent", laptopIP, ""); rec.Code != tt.want {
			t.Errorf("DELETE %s's agent = %d %s, want %d", tt.device, rec.Code, rec.Body, tt.want)
		}
	}
	if reports := opts.Agents.Reports(); len(reports) != 0 {
		t.Errorf("reports after revoking = %v, want none", reports)
	}
	if _, _, ok := opts.Usage.Latest("laptop"); ok {
		t.Error("the laptop's usage is still there after its agent was revoked")
	}
	if _, _, ok := opts.Usage.Latest(store.HubID); !ok {
		t.Error("revoking the agent of Bield's own machine, which has none, dropped the usage Bield measured")
	}
	if enrollments, err := st.Enrollments(t.Context()); err != nil || len(enrollments) != 0 {
		t.Errorf("enrollments after revoking = %v, %v, want none", enrollments, err)
	}
	if rec := sendBeat(t, h, laptopIP, secret, laptopBeat); rec.Code != http.StatusUnauthorized {
		t.Errorf("a heartbeat after revoking = %d %s, want 401", rec.Code, rec.Body)
	}
	// Running the install command again doesn't undo the revoke.
	if rec := sendAs(t, h, http.MethodPost, "/api/agent/enroll", laptopIP, code, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("enrolling again with the old code after revoking = %d %s, want 401", rec.Code, rec.Body)
	}
}

// In embedded mode, the machine Bield runs on has Bield's node and its own,
// and revoking the agent from either revokes it.
func TestRevokeAgent_hub(t *testing.T) {
	t.Parallel()
	hostIP := netip.MustParseAddr("100.64.0.10")
	for _, device := range []string{"bield", "host"} {
		tn := tailnettest.New(tailnet.Status{
			State: tailnet.StateRunning,
			Self:  tailnet.Device{ID: "bield", DNSName: bieldHost, IPs: []netip.Addr{bieldIP}, Owner: alice, Online: true},
			Peers: []tailnet.Device{
				{ID: "laptop", DNSName: "laptop.example.ts.net", IPs: []netip.Addr{laptopIP}, Owner: alice, Online: true},
				{ID: "host", DNSName: "host.example.ts.net", IPs: []netip.Addr{hostIP}, Owner: alice, Online: true, SameMachine: true},
			},
		})
		st := testStore(t)
		opts := withStore(t, tn, st, api.Options{})
		h := api.New(opts)
		secret := enrolled(t, st, "host")
		if rec := sendBeat(t, h, hostIP, secret, laptopBeat); rec.Code != http.StatusNoContent {
			t.Fatalf("POST = %d %s", rec.Code, rec.Body)
		}
		if rec := send(t, h, http.MethodDelete, "/api/devices/"+device+"/agent", laptopIP, ""); rec.Code != http.StatusNoContent {
			t.Errorf("DELETE %s's agent = %d %s, want 204", device, rec.Code, rec.Body)
		}
		if rec := sendBeat(t, h, hostIP, secret, laptopBeat); rec.Code != http.StatusUnauthorized {
			t.Errorf("a heartbeat after revoking %s's agent = %d %s, want 401", device, rec.Code, rec.Body)
		}
	}
}

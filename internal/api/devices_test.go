package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/hostmetrics"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/usage"
)

func TestSetDeviceSettings(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", 65)
	for _, tt := range []struct {
		name, device, body string
		wantCode           int
		want               api.DeviceSettings
		// stored is what the store keeps, if the request works.
		stored store.DeviceSettings
	}{
		{
			name:     "every setting",
			device:   "laptop",
			body:     `{"displayName":"  Work laptop ","kind":"laptop","alertOffline":false,"notes":"Desk drawer\n\n"}`,
			wantCode: http.StatusOK,
			want:     api.DeviceSettings{DisplayName: "Work laptop", Kind: api.DeviceKindLaptop, AlertOffline: false, Notes: "Desk drawer"},
			stored:   store.DeviceSettings{DisplayName: "Work laptop", Kind: "laptop", AlertOffline: new(false), Notes: "Desk drawer"},
		},
		{
			// Alerting is on by default for a laptop, so that isn't stored.
			name:     "defaults",
			device:   "laptop",
			body:     `{"displayName":"","alertOffline":true,"notes":""}`,
			wantCode: http.StatusOK,
			want:     api.DeviceSettings{AlertOffline: true},
		},
		{
			// Leaving alerting out means the default, not off.
			name:     "alerting left out",
			device:   "laptop",
			body:     `{"displayName":"Work laptop"}`,
			wantCode: http.StatusOK,
			want:     api.DeviceSettings{DisplayName: "Work laptop", AlertOffline: true},
			stored:   store.DeviceSettings{DisplayName: "Work laptop"},
		},
		{
			name:     "Roost's own node",
			device:   "roost",
			body:     `{"displayName":"Hub","alertOffline":true,"notes":""}`,
			wantCode: http.StatusOK,
			want:     api.DeviceSettings{DisplayName: "Hub", AlertOffline: true},
			stored:   store.DeviceSettings{DisplayName: "Hub"},
		},
		{name: "unknown device", device: "toaster", body: `{"alertOffline":true}`, wantCode: http.StatusNotFound},
		{name: "unknown kind", device: "laptop", body: `{"kind":"toaster"}`, wantCode: http.StatusBadRequest},
		{name: "name too long", device: "laptop", body: `{"displayName":"` + long + `"}`, wantCode: http.StatusBadRequest},
		{name: "name with a line break", device: "laptop", body: `{"displayName":"a\nb"}`, wantCode: http.StatusBadRequest},
		{name: "notes too long", device: "laptop", body: `{"notes":"` + strings.Repeat("x", 4001) + `"}`, wantCode: http.StatusBadRequest},
		{name: "unknown field", device: "laptop", body: `{"color":"red"}`, wantCode: http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := testStore(t)
			h := api.New(withStore(t, testTailnet(), st, api.Options{}))
			rec := send(t, h, http.MethodPut, "/api/devices/"+tt.device+"/settings", laptopIP, tt.body)
			if rec.Code != tt.wantCode {
				t.Fatalf("PUT = %d %s, want %d", rec.Code, rec.Body, tt.wantCode)
			}
			if tt.wantCode != http.StatusOK {
				return
			}
			var got api.DeviceSettings
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("response = %+v, want %+v", got, tt.want)
			}
			all, err := st.DeviceSettings(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			stored := all[tt.device]
			if stored.DisplayName != tt.stored.DisplayName || stored.Kind != tt.stored.Kind || stored.Notes != tt.stored.Notes ||
				(stored.AlertOffline == nil) != (tt.stored.AlertOffline == nil) ||
				stored.AlertOffline != nil && *stored.AlertOffline != *tt.stored.AlertOffline {
				t.Errorf("stored %+v, want %+v", stored, tt.stored)
			}
		})
	}
}

// A change to a device's settings shows up in the next snapshot straight
// away.
func TestSetDeviceSettings_publishes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// The request writes, so the store opens in the bubble, as in
		// TestFeed_recordsPings.
		st := testStore(t)
		synctest.Wait()
		tn := testTailnet()
		opts := withStore(t, tn, st, api.Options{})
		go opts.Feed.Run(t.Context())
		h := api.New(opts)
		events := openStream(t, tn, h, laptopIP, true)
		synctest.Wait()
		nextSnapshot(t, events)

		rec := send(t, h, http.MethodPut, "/api/devices/phone/settings", laptopIP, `{"displayName":"Bob's phone","alertOffline":false}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT = %d %s, want 200", rec.Code, rec.Body)
		}
		time.Sleep(api.FeedGap)
		synctest.Wait()
		if d := findDevice(t, nextSnapshot(t, events), "phone"); d.Name != "Bob's phone" {
			t.Errorf("phone's name = %q, want Bob's phone", d.Name)
		}
	})
}

func TestDeviceHistory(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))
	now := time.Now()
	minute := now.Truncate(time.Minute)
	err := st.AddPings(t.Context(), []store.Ping{
		{DeviceID: "laptop", At: now.Add(-2 * time.Hour), Replied: true, RTT: time.Second}, // before the last hour
		{DeviceID: "laptop", At: minute.Add(-30 * time.Minute), Replied: true, RTT: 10 * time.Millisecond},
		{DeviceID: "laptop", At: minute.Add(-30*time.Minute + time.Second), Replied: true, RTT: 20 * time.Millisecond},
		{DeviceID: "laptop", At: minute.Add(-10 * time.Minute)},
		{DeviceID: "phone", At: minute.Add(-5 * time.Minute), Replied: true, RTT: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = st.AddMetrics(t.Context(), []store.MetricsRow{
		{DeviceID: "laptop", At: minute.Add(-20 * time.Minute), CPU: new(10.0), Mem: new(40.0), NetRx: new(100.0)},
		{DeviceID: "laptop", At: minute.Add(-20*time.Minute + 30*time.Second), CPU: new(20.0), Mem: new(60.0), NetRx: new(300.0)},
		// Roost's own machine.
		{DeviceID: store.HubID, At: minute.Add(-5 * time.Minute), CPU: new(50.0)},
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := serve(h, newRequest(t, http.MethodGet, "/api/devices/laptop/history?range=1h", phoneIP))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s, want 200", rec.Code, rec.Body)
	}
	var got api.DeviceHistory
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Step != time.Minute.Milliseconds() || got.From.After(now.Add(-time.Hour)) || got.From.Before(now.Add(-time.Hour-time.Minute)) {
		t.Errorf("history covers %v to %v in steps of %dms, want the last hour in minutes", got.From, got.To, got.Step)
	}
	want := []string{
		pointString(minute.Add(-30*time.Minute), new(15.0)),
		pointString(minute.Add(-10*time.Minute), nil),
	}
	if len(got.RTT) != len(want) {
		t.Fatalf("RTT = %+v, want %q", got.RTT, want)
	}
	for i, p := range got.RTT {
		if s := pointString(time.UnixMilli(p.TS), p.V); s != want[i] {
			t.Errorf("RTT point %d = %s, want %s", i, s, want[i])
		}
	}
	at := minute.Add(-20 * time.Minute)
	for name, tt := range map[string]struct {
		points []api.Point
		want   string
	}{
		"CPU":      {got.CPU, pointString(at, new(15.0))},
		"memory":   {got.Memory, pointString(at, new(50.0))},
		"received": {got.NetRx, pointString(at, new(200.0))},
		"sent":     {got.NetTx, pointString(at, nil)},
	} {
		if len(tt.points) != 1 || pointString(time.UnixMilli(tt.points[0].TS), tt.points[0].V) != tt.want {
			t.Errorf("%s = %+v, want one point, %s", name, tt.points, tt.want)
		}
	}

	// The hub's metrics are kept apart from its node's ID.
	rec = serve(h, newRequest(t, http.MethodGet, "/api/devices/roost/history?range=1h", phoneIP))
	var hub api.DeviceHistory
	if err := json.Unmarshal(rec.Body.Bytes(), &hub); err != nil {
		t.Fatal(err)
	}
	if len(hub.CPU) != 1 || *hub.CPU[0].V != 50 {
		t.Errorf("hub CPU = %+v, want the hub's one point", hub.CPU)
	}

	for target, code := range map[string]int{
		"/api/devices/laptop/history":            http.StatusOK,
		"/api/devices/laptop/history?range=7d":   http.StatusOK,
		"/api/devices/laptop/history?range=30d":  http.StatusOK,
		"/api/devices/laptop/history?range=90d":  http.StatusOK,
		"/api/devices/laptop/history?range=year": http.StatusBadRequest,
		"/api/devices/toaster/history":           http.StatusNotFound,
	} {
		if rec := serve(h, newRequest(t, http.MethodGet, target, phoneIP)); rec.Code != code {
			t.Errorf("GET %s = %d, want %d", target, rec.Code, code)
		}
	}
}

// A history of a month or three months is made of the buckets that older
// metrics and pings are folded into.
func TestDeviceHistory_longRanges(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))
	// Older than the raw samples, and within the week that history lasts unless an admin says more.
	at := time.Now().Add(-5 * 24 * time.Hour).Truncate(time.Hour)
	err := st.AddMetricsRollups(t.Context(), []store.MetricsRollup{
		{DeviceID: "laptop", Step: store.CoarseStep, At: at, N: 120, CPU: new(30.0), Mem: new(40.0)},
		{DeviceID: "laptop", Step: store.CoarseStep, At: at.Add(time.Hour), N: 120, CPU: new(50.0), Mem: new(60.0)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddPingRollups(t.Context(), []store.PingRollup{
		{DeviceID: "laptop", Step: store.CoarseStep, At: at, Pings: 60, Replies: 60, RTT: 12 * time.Millisecond},
	}); err != nil {
		t.Fatal(err)
	}

	for rg, step := range map[string]time.Duration{"30d": 2 * time.Hour, "90d": 6 * time.Hour} {
		rec := serve(h, newRequest(t, http.MethodGet, "/api/devices/laptop/history?range="+rg, phoneIP))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s, want 200", rg, rec.Code, rec.Body)
		}
		var got api.DeviceHistory
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Step != step.Milliseconds() {
			t.Errorf("%s: steps of %dms, want %v", rg, got.Step, step)
		}
		// Both hours are in one bucket of 2 hours or more, or in two, with the
		// same average either way.
		var sum float64
		for _, p := range got.CPU {
			if p.V != nil {
				sum += *p.V
			}
		}
		if len(got.CPU) == 0 || len(got.CPU) > 2 || sum < 40 {
			t.Errorf("%s: CPU = %+v, want the average of 30 and 50 in one or two points", rg, got.CPU)
		}
		if len(got.RTT) != 1 || got.RTT[0].V == nil || *got.RTT[0].V != 12 {
			t.Errorf("%s: RTT = %+v, want the one point of 12 ms", rg, got.RTT)
		}
	}
}

func pointString(at time.Time, v *float64) string {
	if v == nil {
		return at.Format(time.TimeOnly) + " null"
	}
	return at.Format(time.TimeOnly) + " " + strconv.FormatFloat(*v, 'f', -1, 64)
}

// The feed records each ping once.
func TestFeed_recordsPings(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// The feed writes, so the store opens in the bubble, and its first
		// prune ends before the feed starts. Otherwise the feed waits for its
		// lock in fake time, and gives up at once.
		st := testStore(t)
		synctest.Wait()
		var (
			mu    sync.Mutex
			pings = map[string]tailnet.PingRecord{}
		)
		changed := make(chan struct{}, 1)
		ping := func(rec map[string]tailnet.PingRecord) {
			mu.Lock()
			pings = rec
			mu.Unlock()
			changed <- struct{}{}
		}
		feed := api.NewFeed(api.FeedOptions{
			Tailnet: func() tailnet.Snapshot {
				mu.Lock()
				defer mu.Unlock()
				return tailnet.Snapshot{Pings: pings}
			},
			TailnetChanged: changed,
			Store:          st,
		})
		go feed.Run(t.Context())
		synctest.Wait()

		// The server's ping failed on Roost's side, so it isn't a miss.
		first := time.Now()
		ping(map[string]tailnet.PingRecord{
			"laptop": {At: first, Result: &tailnet.PingResult{Latency: 10 * time.Millisecond}},
			"phone":  {At: first, Err: tailnet.ErrNoReply, Misses: 1},
			"server": {At: first, Err: errors.New("the local API went away")},
		})
		time.Sleep(time.Minute)
		// The phone went offline, so its last ping stays.
		ping(map[string]tailnet.PingRecord{
			"laptop": {At: time.Now(), Result: &tailnet.PingResult{Latency: 30 * time.Millisecond}},
			"phone":  {At: first, Err: tailnet.ErrNoReply},
			"server": {At: time.Now(), Result: &tailnet.PingResult{Latency: 20 * time.Millisecond}},
		})
		time.Sleep(time.Minute)
		synctest.Wait()

		for id, want := range map[string][2]int{"laptop": {2, 2}, "phone": {1, 0}, "server": {1, 1}} {
			buckets, err := st.RTTHistory(t.Context(), id, first.Add(-time.Hour), 24*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if len(buckets) != 1 || buckets[0].Pings != want[0] || buckets[0].Replies != want[1] {
				t.Errorf("%s history = %+v, want %d pings and %d replies", id, buckets, want[0], want[1])
			}
		}
	})
}

// The feed stores each device's metrics about every 30 seconds.
func TestFeed_recordsMetrics(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := testStore(t)
		synctest.Wait()
		var (
			mu     sync.Mutex
			sample *hostmetrics.Sample
		)
		changed := make(chan struct{}, 1)
		measure := func(cpu float64) {
			mu.Lock()
			sample = &hostmetrics.Sample{At: time.Now(), CPU: &cpu, Mem: hostmetrics.Usage{Used: 1, Total: 4}, Load: &[3]float64{2, 1, 0}}
			mu.Unlock()
			changed <- struct{}{}
		}
		reg := agents.NewRegistry(st, nil)
		feed := api.NewFeed(api.FeedOptions{
			Tailnet: func() tailnet.Snapshot { return tailnet.Snapshot{} },
			Host: func() hostmetrics.Reading {
				mu.Lock()
				defer mu.Unlock()
				return hostmetrics.Reading{Sample: sample}
			},
			HostChanged: changed,
			Agents:      reg,
			Store:       st,
		})
		go feed.Run(t.Context())
		start := time.Now()
		// The hub measures itself every 5 seconds for 2 minutes, and an agent
		// sends a heartbeat every 15 seconds.
		var hb agents.Heartbeat
		if err := json.Unmarshal([]byte(`{"v":1,"metrics":{"cpu":3,"battery":{"pct":70}}}`), &hb); err != nil {
			t.Fatal(err)
		}
		for i := range 24 {
			measure(float64(i))
			if i%3 == 0 {
				if _, err := reg.Record(t.Context(), "laptop", hb, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			time.Sleep(5 * time.Second)
			synctest.Wait()
		}

		rows := func(id string) []store.MetricsBucket {
			t.Helper()
			b, err := st.MetricsHistory(t.Context(), id, start, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
		hub := rows(store.HubID)
		if len(hub) != 4 {
			t.Fatalf("hub rows = %d, want 4 in 2 minutes", len(hub))
		}
		for i, b := range hub {
			if at := b.Start.Sub(start); at != time.Duration(i)*30*time.Second || *b.CPU != float64(i*6) || *b.Mem != 25 {
				t.Errorf("hub row %d at %v = CPU %v, memory %v; want one every 30s", i, at, *b.CPU, *b.Mem)
			}
		}
		if laptop := rows("laptop"); len(laptop) != 4 || *laptop[0].CPU != 3 {
			t.Errorf("laptop rows = %d, want 4 with the agent's CPU", len(laptop))
		}
	})
}

func TestDeviceUsage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := testStore(t)
		synctest.Wait()
		opts := withStore(t, testTailnet(), st, api.Options{})
		h := api.New(opts)
		get := func(target string) (api.DeviceUsage, int) {
			t.Helper()
			rec := serve(h, newRequest(t, http.MethodGet, target, laptopIP))
			var got api.DeviceUsage
			if rec.Code == http.StatusOK {
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
			}
			return got, rec.Code
		}
		if got, code := get("/api/devices/laptop/usage"); code != http.StatusOK || got.Sample != nil {
			t.Errorf("usage before any sample = %d %+v, want 200 and no sample", code, got)
		}

		// Three minutes of samples, one every 15 seconds.
		start := time.Now()
		for i := range 13 {
			opts.Usage.Record(t.Context(), "laptop", usage.Sample{
				At: time.Now(), Source: usage.SourceAgent,
				Groups: []usage.Group{{Key: "service:db", Kind: usage.KindService, Name: "db", CPU: float64(i), Mem: 100}},
			})
			time.Sleep(15 * time.Second)
		}
		got, code := get("/api/devices/laptop/usage")
		if code != http.StatusOK || got.Sample == nil || got.Sample.Groups[0].CPU != 12 || got.Interval != 15 {
			t.Errorf("latest usage = %d %+v, want the last sample, every 15s", code, got)
		}
		// The first minute's average.
		got, code = get("/api/devices/laptop/usage?at=" + strconv.FormatInt(start.Add(30*time.Second).UnixMilli(), 10))
		if code != http.StatusOK || got.Sample == nil || got.Sample.Groups[0].CPU != 1.5 {
			t.Errorf("usage at 30s = %d %+v, want the first minute", code, got.Sample)
		}
		if got, _ := get("/api/devices/laptop/usage?at=" + strconv.FormatInt(start.Add(-time.Hour).UnixMilli(), 10)); got.Sample != nil {
			t.Errorf("usage an hour before = %+v, want none", got.Sample)
		}
		for _, target := range []string{"/api/devices/laptop/usage?at=soon", "/api/devices/laptop/usage?at=-5"} {
			if _, code := get(target); code != http.StatusBadRequest {
				t.Errorf("GET %s = %d, want 400", target, code)
			}
		}
		if _, code := get("/api/devices/toaster/usage"); code != http.StatusNotFound {
			t.Errorf("GET a toaster's usage = %d, want 404", code)
		}

		rec := serve(h, newRequest(t, http.MethodGet, "/api/devices/laptop/usage/history?range=1h", laptopIP))
		var hist api.UsageHistory
		if err := json.Unmarshal(rec.Body.Bytes(), &hist); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("GET usage history = %d %s", rec.Code, rec.Body)
		}
		if hist.Minutes != 3 || len(hist.Consumers) != 1 || hist.Consumers[0].Name != "db" || len(hist.CPU) != 1 || len(hist.Mem) != 1 {
			t.Errorf("usage history = %+v, want 3 minutes of db", hist)
		}
		if rec := serve(h, newRequest(t, http.MethodGet, "/api/devices/laptop/usage/history?range=year", laptopIP)); rec.Code != http.StatusBadRequest {
			t.Errorf("GET usage history for a year = %d, want 400", rec.Code)
		}
	})
}

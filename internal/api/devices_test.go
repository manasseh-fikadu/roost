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

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
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
			h := api.New(withStore(testTailnet(), st, api.Options{}))
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
		opts := withStore(tn, st, api.Options{})
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
	h := api.New(withStore(testTailnet(), st, api.Options{}))
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

	for target, code := range map[string]int{
		"/api/devices/laptop/history":            http.StatusOK,
		"/api/devices/laptop/history?range=7d":   http.StatusOK,
		"/api/devices/laptop/history?range=year": http.StatusBadRequest,
		"/api/devices/toaster/history":           http.StatusNotFound,
	} {
		if rec := serve(h, newRequest(t, http.MethodGet, target, phoneIP)); rec.Code != code {
			t.Errorf("GET %s = %d, want %d", target, rec.Code, code)
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

		first := time.Now()
		ping(map[string]tailnet.PingRecord{
			"laptop": {At: first, Result: &tailnet.PingResult{Latency: 10 * time.Millisecond}},
			"phone":  {At: first, Err: errors.New("no reply")},
		})
		time.Sleep(time.Minute)
		// The phone went offline, so its last ping stays.
		ping(map[string]tailnet.PingRecord{
			"laptop": {At: time.Now(), Result: &tailnet.PingResult{Latency: 30 * time.Millisecond}},
			"phone":  {At: first, Err: errors.New("no reply")},
		})
		time.Sleep(time.Minute)
		synctest.Wait()

		for id, want := range map[string][2]int{"laptop": {2, 2}, "phone": {1, 0}} {
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

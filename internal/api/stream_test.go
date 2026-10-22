package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/tailnet/tailnettest"
)

// streamInterval is how often the stream sends a snapshot.
const streamInterval = 10 * time.Second

func TestStream(t *testing.T) {
	for _, tt := range []struct {
		name string
		from netip.Addr
		// What the caller gets of the tailnet's name and of the failing
		// collector's error.
		tailnet, netcheckErr string
	}{
		{name: "admin", from: laptopIP, tailnet: "example.com", netcheckErr: "no DERP map"},
		// A personal tailnet is named after its owner's email address, and an
		// error can name paths and addresses on Roost's machine.
		{name: "viewer", from: phoneIP},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// The store's goroutines start outside the bubble, so it can end without them.
			st := testStore(t)
			synctest.Test(t, func(t *testing.T) {
				tn := testTailnet()
				polledAt := time.Now()
				h := api.New(api.Options{
					Version: "v1.2.3",
					WhoIs:   tn.WhoIs,
					Snapshot: func() tailnet.Snapshot {
						status, _ := tn.Status(context.Background())
						status.Tailnet = "example.com"
						return tailnet.Snapshot{Status: status, StatusAt: polledAt, ConnectivityErr: errors.New("no DERP map")}
					},
					Store: st,
				})
				events := openStream(t, tn, h, tt.from)

				synctest.Wait()
				first := nextSnapshot(t, events)
				if want := (api.Hub{Version: "v1.2.3", DNSName: roostHost, Tailnet: tt.tailnet}); first.Hub != want {
					t.Errorf("hub = %+v, want %+v", first.Hub, want)
				}
				if !first.GeneratedAt.Equal(time.Now()) {
					t.Errorf("generatedAt = %v, want %v", first.GeneratedAt, time.Now())
				}
				wantCollectors := []api.Collector{
					{Name: "Tailscale status", State: api.CollectorStateOK, LastOK: &polledAt},
					{Name: "Tailscale netcheck", State: api.CollectorStateFailing, Error: tt.netcheckErr},
				}
				if len(first.Collectors) != len(wantCollectors) {
					t.Fatalf("collectors = %+v, want %+v", first.Collectors, wantCollectors)
				}
				for i, c := range first.Collectors {
					want := wantCollectors[i]
					if c.Name != want.Name || c.State != want.State || c.Error != want.Error ||
						(c.LastOK == nil) != (want.LastOK == nil) || c.LastOK != nil && !c.LastOK.Equal(*want.LastOK) {
						t.Errorf("collector %d = %+v, want %+v", i, c, want)
					}
				}
				wantDevice := api.Device{
					ID: "roost", Name: "roost", DNSName: roostHost, IPs: []string{roostIP.String(), roostIPv6.String()},
					Online: true, Self: true,
				}
				if len(first.Devices) != 5 {
					t.Fatalf("got %d devices, want Roost's node and its 4 peers", len(first.Devices))
				}
				if d := first.Devices[0]; d.ID != wantDevice.ID || d.Name != wantDevice.Name || !d.Self ||
					strings.Join(d.IPs, ",") != strings.Join(wantDevice.IPs, ",") {
					t.Errorf("first device = %+v, want %+v", d, wantDevice)
				}
				if d := first.Devices[1]; d.ID != "laptop" || d.Self {
					t.Errorf("second device = %+v, want the laptop, which isn't Roost's node", d)
				}

				// Nothing more until the interval passes.
				select {
				case e := <-events:
					t.Fatalf("got event %+v before the interval passed", e)
				default:
				}
				time.Sleep(streamInterval)
				synctest.Wait()
				second := nextSnapshot(t, events)
				if got := second.GeneratedAt.Sub(first.GeneratedAt); got != streamInterval {
					t.Errorf("second snapshot came %v after the first, want %v", got, streamInterval)
				}
			})
		})
	}
}

// A caller whose role changes is cut off, so that their client connects
// again and Roost checks them again.
func TestStream_roleChange(t *testing.T) {
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		tn := testTailnet()
		h := api.New(withStore(tn, st, api.Options{}))
		events := openStream(t, tn, h, phoneIP)
		synctest.Wait()
		nextSnapshot(t, events)

		if _, err := st.RemoveUser(context.Background(), bob); err != nil {
			t.Fatal(err)
		}
		time.Sleep(streamInterval)
		synctest.Wait()
		if e, ok := <-events; ok {
			t.Errorf("got event %+v after bob lost his role, want the stream to end", e)
		}
	})
}

// A role the store can't look up isn't a role taken away, so the stream
// carries on.
func TestStream_roleLookupFails(t *testing.T) {
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		tn := testTailnet()
		h := api.New(withStore(tn, st, api.Options{}))
		events := openStream(t, tn, h, phoneIP)
		synctest.Wait()
		nextSnapshot(t, events)

		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(streamInterval)
		synctest.Wait()
		nextSnapshot(t, events)
	})
}

func TestStream_stopping(t *testing.T) {
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		tn := testTailnet()
		stopping := make(chan struct{})
		h := api.New(withStore(tn, st, api.Options{Stopping: stopping}))
		events := openStream(t, tn, h, laptopIP)
		synctest.Wait()
		nextSnapshot(t, events)

		close(stopping)
		synctest.Wait()
		if e, ok := <-events; ok {
			t.Errorf("got event %+v after the server began to stop, want the stream to end", e)
		}
	})
}

// An event is a server-sent event.
type event struct {
	name, data string
}

// openStream serves h on tn, opens GET /api/stream from the device at from,
// and returns the events it sends. The channel is closed when the stream
// ends.
func openStream(t *testing.T, tn *tailnettest.Tailnet, h http.Handler, from netip.Addr) <-chan event {
	t.Helper()
	ln, _, err := tn.Listen(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	client := tn.Client(from)
	t.Cleanup(client.CloseIdleConnections)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, tailnettest.URL+"/api/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("GET /api/stream = %d %s, want 200", res.StatusCode, body)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	events := make(chan event, 10)
	go func() {
		defer close(events)
		var e event
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			switch line := sc.Text(); {
			case line == "":
				events <- e
				e = event{}
			case strings.HasPrefix(line, "event: "):
				e.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				e.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return events
}

// nextSnapshot takes a snapshot event that has already arrived.
func nextSnapshot(t *testing.T, events <-chan event) api.Snapshot {
	t.Helper()
	var e event
	select {
	case got, ok := <-events:
		if !ok {
			t.Fatal("the stream ended, want a snapshot")
		}
		e = got
	default:
		t.Fatal("no event yet, want a snapshot")
	}
	if e.name != "snapshot" {
		t.Fatalf("event = %q, want snapshot", e.name)
	}
	var s api.Snapshot
	if err := json.Unmarshal([]byte(e.data), &s); err != nil {
		t.Fatalf("decode snapshot %q: %v", e.data, err)
	}
	return s
}

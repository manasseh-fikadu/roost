package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/checks"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/tailnet/tailnettest"
	"github.com/manasseh-fikadu/roost/internal/usage"
)

func TestStream(t *testing.T) {
	for _, tt := range []struct {
		name string
		from netip.Addr
		gzip bool
		// What the caller gets of the tailnet's name and of the failing
		// collector's error.
		tailnet, netcheckErr string
	}{
		{name: "admin", from: laptopIP, gzip: true, tailnet: "example.com", netcheckErr: "no DERP map"},
		// A personal tailnet is named after its owner's email address, and an
		// error can name paths and addresses on Roost's machine.
		{name: "viewer", from: phoneIP, gzip: true},
		{name: "without gzip", from: laptopIP, tailnet: "example.com", netcheckErr: "no DERP map"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// The store's goroutines start outside the bubble, so it can end without them.
			st := testStore(t)
			synctest.Test(t, func(t *testing.T) {
				tn := testTailnet()
				polledAt := time.Now()
				snapshot := func() tailnet.Snapshot {
					status, _ := tn.Status(context.Background())
					status.Tailnet = "example.com"
					return tailnet.Snapshot{Status: status, StatusAt: polledAt, ConnectivityErr: errors.New("no DERP map")}
				}
				changed := make(chan struct{}, 1)
				feed := api.NewFeed(api.FeedOptions{Version: "v1.2.3", Tailnet: snapshot, TailnetChanged: changed, Store: st})
				go feed.Run(t.Context())
				h := api.New(api.Options{
					Version: "v1.2.3", WhoIs: tn.WhoIs, Snapshot: snapshot, Feed: feed, Store: st,
					Agents: agents.NewRegistry(st, nil), Usage: usage.NewRecorder(st, nil),
					Checks: checks.NewRunner(st, checks.Options{Dial: tn.Dial}),
				})
				events := openStream(t, tn, h, tt.from, tt.gzip)

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
				// Roost's node, then servers, laptops and the rest, by name.
				if got, want := deviceIDs(first), []string{"roost", "server", "laptop", "phone", "tablet"}; !slices.Equal(got, want) {
					t.Fatalf("devices = %q, want %q", got, want)
				}
				if d := first.Devices[0]; d.Name != "roost" || !d.Self || strings.Join(d.IPs, ",") != roostIP.String()+","+roostIPv6.String() {
					t.Errorf("first device = %+v, want Roost's node", d)
				}

				// Nothing more until something changes.
				time.Sleep(api.FeedHeartbeat / 2)
				synctest.Wait()
				select {
				case e := <-events:
					t.Fatalf("got event %+v before anything changed", e)
				default:
				}
				status, _ := tn.Status(context.Background())
				status.Peers[0].Online = false // the laptop
				tn.SetStatus(*status)
				changed <- struct{}{}
				synctest.Wait()
				second := nextSnapshot(t, events)
				if d := findDevice(t, second, "laptop"); d.Online || d.Status != api.StatusOffline {
					t.Errorf("laptop after it left = %+v, want it offline", d)
				}

				// With nothing changing, a snapshot still comes now and then,
				// so the client knows the stream is alive.
				time.Sleep(api.FeedHeartbeat)
				synctest.Wait()
				third := nextSnapshot(t, events)
				if got := third.GeneratedAt.Sub(second.GeneratedAt); got != api.FeedHeartbeat {
					t.Errorf("the next snapshot came %v later, want %v", got, api.FeedHeartbeat)
				}
			})
		})
	}
}

// Changes that come close together share one snapshot.
func TestStream_gathersChanges(t *testing.T) {
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		tn := testTailnet()
		changed := make(chan struct{}, 1)
		opts := withStore(tn, st, api.Options{})
		opts.Feed = api.NewFeed(api.FeedOptions{Tailnet: opts.Snapshot, TailnetChanged: changed, Store: st})
		go opts.Feed.Run(t.Context())
		events := openStream(t, tn, api.New(opts), laptopIP, true)
		synctest.Wait()
		first := nextSnapshot(t, events)

		for range 5 {
			// Like the poller, tell of a change without waiting.
			select {
			case changed <- struct{}{}:
			default:
			}
			time.Sleep(api.FeedGap / 10)
		}
		time.Sleep(api.FeedGap)
		synctest.Wait()
		second := nextSnapshot(t, events)
		if got := second.GeneratedAt.Sub(first.GeneratedAt); got != api.FeedGap {
			t.Errorf("the second snapshot came %v after the first, want %v", got, api.FeedGap)
		}
		select {
		case e := <-events:
			t.Errorf("got a third event %+v, want the changes to share the second", e)
		default:
		}
	})
}

// A caller whose role changes is cut off, so that their client connects
// again and Roost checks them again.
func TestStream_roleChange(t *testing.T) {
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		tn := testTailnet()
		h := api.New(withStore(tn, st, api.Options{}))
		events := openStream(t, tn, h, phoneIP, true)
		synctest.Wait()
		nextSnapshot(t, events)

		if _, err := st.RemoveUser(context.Background(), bob); err != nil {
			t.Fatal(err)
		}
		time.Sleep(api.RoleCheckInterval)
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
		events := openStream(t, tn, h, phoneIP, true)
		synctest.Wait()
		nextSnapshot(t, events)

		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(api.RoleCheckInterval)
		synctest.Wait()
		select {
		case e, ok := <-events:
			if !ok {
				t.Error("the stream ended when the store couldn't say what bob's role is")
			} else {
				t.Errorf("got event %+v, want nothing", e)
			}
		default:
		}
	})
}

func TestStream_stopping(t *testing.T) {
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		tn := testTailnet()
		stopping := make(chan struct{})
		h := api.New(withStore(tn, st, api.Options{Stopping: stopping}))
		events := openStream(t, tn, h, laptopIP, true)
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
// ends. If gzip is true, the client asks for gzip, and the stream must use
// it.
func openStream(t *testing.T, tn *tailnettest.Tailnet, h http.Handler, from netip.Addr, gzip bool) <-chan event {
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
	if !gzip {
		// The client asks for gzip and decodes it unless told otherwise.
		req.Header.Set("Accept-Encoding", "identity")
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.Uncompressed != gzip {
		t.Errorf("the stream used gzip: %v, want %v", res.Uncompressed, gzip)
	}
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

func deviceIDs(s api.Snapshot) []string {
	ids := make([]string, len(s.Devices))
	for i, d := range s.Devices {
		ids[i] = d.ID
	}
	return ids
}

func findDevice(t *testing.T, s api.Snapshot, id string) api.Device {
	t.Helper()
	for _, d := range s.Devices {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("no device %q in the snapshot", id)
	return api.Device{}
}

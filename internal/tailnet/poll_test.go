package tailnet_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/tailnet/tailnettest"
)

var (
	selfIP    = netip.MustParseAddr("100.64.0.1")
	directIP  = netip.MustParseAddr("100.64.0.2")
	derpIP    = netip.MustParseAddr("100.64.0.3")
	silentIP  = netip.MustParseAddr("100.64.0.4")
	offlineIP = netip.MustParseAddr("100.64.0.5")
)

func testStatus(state string) tailnet.Status {
	return tailnet.Status{
		State: state,
		Self:  tailnet.Device{ID: "self", DNSName: "roost.example.ts.net", IPs: []netip.Addr{selfIP}, Online: true},
		Peers: []tailnet.Device{
			{ID: "direct", IPs: []netip.Addr{netip.MustParseAddr("fd7a:115c:a1e0::2"), directIP}, Online: true},
			{ID: "derp", IPs: []netip.Addr{derpIP}, Online: true, Relay: "fra"},
			{ID: "silent", IPs: []netip.Addr{silentIP}, Online: true},
			{ID: "offline", IPs: []netip.Addr{offlineIP}},
		},
	}
}

// runPoller runs a Poller on tn until the test ends. A nil log discards its
// messages.
func runPoller(t *testing.T, tn tailnet.Tailnet, log *slog.Logger) *tailnet.Poller {
	t.Helper()
	p := tailnet.NewPoller(tn, log)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return p
}

// logBuffer collects log lines from several goroutines.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// count returns how many lines contain msg.
func (b *logBuffer) count(msg string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Count(b.buf.String(), msg)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestPoller(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tn := tailnettest.New(testStatus(tailnet.StateRunning))
		direct := &tailnet.PingResult{Latency: 20 * time.Millisecond, Endpoint: "203.0.113.2:41641"}
		derp := &tailnet.PingResult{Latency: 90 * time.Millisecond, DERPRegion: "fra"}
		tn.SetPing(directIP, direct, nil)
		tn.SetPing(derpIP, derp, nil)
		conn := &tailnet.Connectivity{UDP: true, IPv4: true}
		tn.SetConnectivity(conn, nil)
		start := time.Now()

		p := runPoller(t, tn, nil)
		// Long enough for the silent peer's ping to time out, but before the
		// next status poll.
		time.Sleep(6 * time.Second)
		synctest.Wait()

		snap := p.Snapshot()
		if snap.Status == nil || len(snap.Status.Peers) != 4 || !snap.StatusAt.Equal(start) {
			t.Fatalf("Status = %+v at %v, want the fake's, polled at the start", snap.Status, snap.StatusAt)
		}
		if got := snap.Pings["direct"].Result; !reflect.DeepEqual(got, direct) {
			t.Errorf("ping of direct = %+v, want %+v (pinged at its IPv4 address)", got, direct)
		}
		if got := snap.Pings["derp"].Result; !reflect.DeepEqual(got, derp) {
			t.Errorf("ping of derp = %+v, want %+v", got, derp)
		}
		if got := snap.Pings["silent"]; got.Result != nil || !errors.Is(got.Err, tailnet.ErrNoReply) || got.Misses != 1 {
			t.Errorf("ping of silent = %+v, want its first miss", got)
		}
		if _, ok := snap.Pings["offline"]; ok {
			t.Error("an offline peer was pinged")
		}
		if !reflect.DeepEqual(snap.Connectivity, conn) || !snap.ConnectivityAt.Equal(start) {
			t.Errorf("Connectivity = %+v at %v, want %+v at the start", snap.Connectivity, snap.ConnectivityAt, conn)
		}

		// The status is polled again, and a peer that leaves loses its ping
		// at the next round, which ends when the silent peer's ping times out.
		st := testStatus(tailnet.StateRunning)
		st.Peers = st.Peers[1:]
		tn.SetStatus(st)
		time.Sleep(tailnet.PingInterval + 6*time.Second)
		synctest.Wait()

		snap = p.Snapshot()
		if len(snap.Status.Peers) != 3 {
			t.Errorf("Status has %d peers, want 3", len(snap.Status.Peers))
		}
		if _, ok := snap.Pings["direct"]; ok {
			t.Error("the ping of a peer that left is still there")
		}
		if _, ok := snap.Pings["derp"]; !ok {
			t.Error("the ping of a peer that's still there is gone")
		}
	})
}

func TestPoller_waitsForLogin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tn := tailnettest.New(testStatus(tailnet.StateNeedsLogin))
		tn.SetPing(directIP, &tailnet.PingResult{Endpoint: "203.0.113.2:41641"}, nil)
		tn.SetConnectivity(&tailnet.Connectivity{UDP: true}, nil)

		p := runPoller(t, tn, nil)
		time.Sleep(time.Minute)
		synctest.Wait()

		snap := p.Snapshot()
		if len(snap.Pings) != 0 || snap.Connectivity != nil {
			t.Fatalf("before login, Pings = %v and Connectivity = %+v, want none", snap.Pings, snap.Connectivity)
		}

		// Soon after login, both run. The ping round ends when the silent
		// peer's ping times out.
		tn.SetStatus(testStatus(tailnet.StateRunning))
		time.Sleep(2*tailnet.StatusInterval + 6*time.Second)
		synctest.Wait()

		snap = p.Snapshot()
		if snap.Pings["direct"].Result == nil || snap.Connectivity == nil {
			t.Errorf("after login, Pings = %v and Connectivity = %+v, want both", snap.Pings, snap.Connectivity)
		}
	})
}

func TestPoller_Polled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := tailnet.NewPoller(tailnettest.New(testStatus(tailnet.StateRunning)), nil)
		select {
		case <-p.Polled():
			t.Fatal("Polled() is closed before Run")
		default:
		}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go p.Run(ctx)
		<-p.Polled()

		if p.Snapshot().Status == nil {
			t.Error("Snapshot() has no status once Polled() is closed")
		}
	})
}

// Right after a node starts, its peers can look offline. They're pinged as
// soon as they're online, not a whole interval later.
func TestPoller_pingsPeersThatComeOnline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := testStatus(tailnet.StateRunning)
		for i := range st.Peers {
			st.Peers[i].Online = false
		}
		tn := tailnettest.New(st)
		tn.SetPing(directIP, &tailnet.PingResult{Endpoint: "203.0.113.2:41641"}, nil)
		p := runPoller(t, tn, nil)
		synctest.Wait()

		// The next status poll sees them online, and the round of pings
		// after it ends when the silent peer's ping times out, well before a
		// whole PingInterval.
		tn.SetStatus(testStatus(tailnet.StateRunning))
		wait := 2*tailnet.StatusInterval + 6*time.Second
		time.Sleep(wait)
		synctest.Wait()
		if p.Snapshot().Pings["direct"].Result == nil {
			t.Errorf("a peer that came online wasn't pinged within %v", wait)
		}
	})
}

// A peer counts as not answering once it misses two pings in a row. A ping
// that fails for another reason isn't a miss, and a peer that was offline
// starts again from zero.
func TestPoller_countsMisses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tn := tailnettest.New(testStatus(tailnet.StateRunning))
		tn.SetPing(directIP, &tailnet.PingResult{Endpoint: "203.0.113.2:41641"}, nil)
		tn.SetPing(derpIP, nil, errors.New("the local API went away"))
		p := runPoller(t, tn, nil)
		check := func(when string, wantMisses int, wantUnanswered bool) {
			t.Helper()
			snap := p.Snapshot()
			if got := snap.Pings["derp"]; got.Err == nil || got.Misses != 0 {
				t.Errorf("%s, derp's ping = %+v, want an error that isn't a miss", when, got)
			}
			silent := snap.Pings["silent"]
			if silent.Misses != wantMisses {
				t.Errorf("%s, silent has %d misses, want %d", when, silent.Misses, wantMisses)
			}
			for _, d := range snap.Status.Peers {
				if got := snap.PathTo(d, time.Now()).Unanswered; got != (d.ID == "silent" && wantUnanswered) {
					t.Errorf("%s, PathTo(%s).Unanswered = %v", when, d.ID, got)
				}
			}
		}

		// A round that pings the silent peer ends when its ping times out, 5s
		// after the round starts. The next round starts a minute after that.
		time.Sleep(6 * time.Second)
		synctest.Wait()
		check("after one round", 1, false)
		time.Sleep(tailnet.PingInterval + 5*time.Second)
		synctest.Wait()
		check("after two rounds", 2, true)

		// The silent peer is offline for a round, which ends at once.
		st := testStatus(tailnet.StateRunning)
		st.Peers[2].Online = false
		tn.SetStatus(st)
		time.Sleep(tailnet.PingInterval)
		synctest.Wait()
		check("while it's offline", 0, false)

		tn.SetStatus(testStatus(tailnet.StateRunning))
		time.Sleep(tailnet.PingInterval + 5*time.Second)
		synctest.Wait()
		check("once it's back", 1, false)
	})
}

// Changes tells its receiver each time the snapshot changes, and never
// blocks the poller.
func TestPoller_Changes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tn := tailnettest.New(testStatus(tailnet.StateRunning))
		tn.SetConnectivity(&tailnet.Connectivity{UDP: true}, nil)
		p := runPoller(t, tn, nil)

		// The first status poll and connectivity check come at once, and a
		// receiver that's busy hears of them once.
		synctest.Wait()
		<-p.Changes()
		select {
		case <-p.Changes():
			t.Fatal("Changes() has a second value for changes made while no one received")
		default:
		}

		// The next status poll.
		time.Sleep(tailnet.StatusInterval)
		synctest.Wait()
		select {
		case <-p.Changes():
		default:
			t.Fatal("Changes() has nothing after the second status poll")
		}
	})
}

func TestPoller_errors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tn := tailnettest.New(testStatus(tailnet.StateRunning))
		tn.SetConnectivity(nil, errors.New("no UDP"))
		log := &logBuffer{}

		p := runPoller(t, tn, slog.New(slog.NewTextHandler(log, nil)))
		synctest.Wait()
		statusAt := p.Snapshot().StatusAt
		tn.FailStatus(errors.New("tailscaled went away"))
		time.Sleep(tailnet.StatusInterval)
		synctest.Wait()

		snap := p.Snapshot()
		if snap.StatusErr == nil || snap.Status == nil || !snap.StatusAt.Equal(statusAt) {
			t.Errorf("after a failed poll, Status = %v at %v, StatusErr = %v; want the last status and the error",
				snap.Status, snap.StatusAt, snap.StatusErr)
		}
		if snap.ConnectivityErr == nil || snap.Connectivity != nil {
			t.Errorf("Connectivity = %+v, ConnectivityErr = %v; want only the error", snap.Connectivity, snap.ConnectivityErr)
		}

		// Both recover: the status at the next poll, and connectivity a
		// minute after it failed.
		tn.FailStatus(nil)
		tn.SetConnectivity(&tailnet.Connectivity{UDP: true}, nil)
		time.Sleep(time.Minute)
		synctest.Wait()

		snap = p.Snapshot()
		if snap.StatusErr != nil || snap.ConnectivityErr != nil || snap.Connectivity == nil {
			t.Errorf("StatusErr = %v, Connectivity = %+v, ConnectivityErr = %v; want no errors",
				snap.StatusErr, snap.Connectivity, snap.ConnectivityErr)
		}
		// Each failure and recovery is logged once, not at every poll.
		for _, msg := range []string{
			`msg="get the tailnet status"`, `msg="got the tailnet status again"`,
			`msg="check connectivity"`, `msg="checked connectivity again"`,
		} {
			if n := log.count(msg); n != 1 {
				t.Errorf("logged %s %d times, want once; log:\n%s", msg, n, log)
			}
		}
	})
}

func TestSnapshot_PathTo(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	recent, stale := now.Add(-time.Minute), now.Add(-10*time.Minute)
	snap := tailnet.Snapshot{
		Status: &tailnet.Status{Self: tailnet.Device{ID: "self"}},
		Pings: map[string]tailnet.PingRecord{
			"direct":       {At: recent, Result: &tailnet.PingResult{Latency: 5 * time.Millisecond, Endpoint: "203.0.113.2:41641"}},
			"derp":         {At: recent, Result: &tailnet.PingResult{Latency: 40 * time.Millisecond, DERPRegion: "fra"}},
			"peerrelay":    {At: recent, Result: &tailnet.PingResult{Latency: 9 * time.Millisecond, PeerRelay: "100.64.0.9:7777:vni:3"}},
			"stale":        {At: stale, Result: &tailnet.PingResult{Latency: 5 * time.Millisecond, Endpoint: "203.0.113.4:41641"}},
			"failed":       {At: recent, Err: tailnet.ErrNoReply, Misses: 2},
			"missed-once":  {At: recent, Err: tailnet.ErrNoReply, Misses: 1},
			"stale-failed": {At: stale, Err: tailnet.ErrNoReply, Misses: 2},
		},
	}
	tests := []struct {
		name   string
		device tailnet.Device
		want   tailnet.Path
	}{
		{"self", tailnet.Device{ID: "self", Online: true}, tailnet.Path{Kind: tailnet.PathSelf}},
		{"offline", tailnet.Device{ID: "failed", Relay: "nyc"}, tailnet.Path{Kind: tailnet.PathOffline, PingedAt: recent}},
		{
			"direct ping", tailnet.Device{ID: "direct", Online: true, Relay: "nyc"},
			tailnet.Path{Kind: tailnet.PathDirect, Endpoint: "203.0.113.2:41641", Latency: 5 * time.Millisecond, PingedAt: recent},
		},
		{
			"DERP ping", tailnet.Device{ID: "derp", Online: true, CurAddr: "203.0.113.3:41641"},
			tailnet.Path{Kind: tailnet.PathDERP, Relay: "fra", Latency: 40 * time.Millisecond, PingedAt: recent},
		},
		{
			"peer relay ping", tailnet.Device{ID: "peerrelay", Online: true},
			tailnet.Path{Kind: tailnet.PathPeerRelay, Relay: "100.64.0.9:7777:vni:3", Latency: 9 * time.Millisecond, PingedAt: recent},
		},
		{
			"stale ping falls back to the status", tailnet.Device{ID: "stale", Online: true, Relay: "nyc"},
			tailnet.Path{Kind: tailnet.PathDERP, Relay: "nyc", PingedAt: stale},
		},
		{
			"failed ping falls back to the status", tailnet.Device{ID: "failed", Online: true, CurAddr: "203.0.113.5:41641"},
			tailnet.Path{Kind: tailnet.PathDirect, Endpoint: "203.0.113.5:41641", PingedAt: recent, Unanswered: true},
		},
		{
			"one missed ping isn't unanswered", tailnet.Device{ID: "missed-once", Online: true, Relay: "nyc"},
			tailnet.Path{Kind: tailnet.PathDERP, Relay: "nyc", PingedAt: recent},
		},
		{
			"stale failed ping isn't unanswered", tailnet.Device{ID: "stale-failed", Online: true, Relay: "nyc"},
			tailnet.Path{Kind: tailnet.PathDERP, Relay: "nyc", PingedAt: stale},
		},
		{
			"never pinged, through a peer relay", tailnet.Device{ID: "new", Online: true, PeerRelay: "100.64.0.9:7777:vni:4"},
			tailnet.Path{Kind: tailnet.PathPeerRelay, Relay: "100.64.0.9:7777:vni:4"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := snap.PathTo(tt.device, now); got != tt.want {
				t.Errorf("PathTo() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

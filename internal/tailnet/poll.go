package tailnet

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"sync"
	"time"
)

// How often a Poller asks the tailnet, as the old dashboard did.
const (
	StatusInterval       = 10 * time.Second
	PingInterval         = time.Minute
	ConnectivityInterval = 10 * time.Minute
)

const (
	pingTimeout         = 5 * time.Second
	connectivityTimeout = 45 * time.Second
	// maxPings is how many peers are pinged at once.
	maxPings = 16
	// retryInterval is how soon a failed connectivity check runs again.
	retryInterval = time.Minute
	// pingFresh is how long a ping's path is trusted over the status's.
	pingFresh = 5 * time.Minute
)

// Poller asks a Tailnet for its status, pings and connectivity in the
// background and keeps the latest answers, so readers don't wait on
// Tailscale. It's safe for concurrent use.
type Poller struct {
	tn  Tailnet
	log *slog.Logger

	mu   sync.Mutex
	snap Snapshot
}

// Snapshot is what a Poller knows at one moment.
type Snapshot struct {
	// Status is the latest status that was polled, or nil before the first.
	Status *Status
	// StatusAt is when Status was polled.
	StatusAt time.Time
	// StatusErr is why the latest poll failed, if it did. Status is then the
	// last one that worked.
	StatusErr error

	// Pings holds the latest ping of each peer, by device ID.
	Pings map[string]PingRecord

	// Connectivity is the latest connectivity check, or nil before the first.
	Connectivity *Connectivity
	// ConnectivityAt is when Connectivity was checked.
	ConnectivityAt time.Time
	// ConnectivityErr is why the latest check failed, if it did.
	ConnectivityErr error
}

// PingRecord is the outcome of pinging a peer.
type PingRecord struct {
	At time.Time
	// Result is the reply, or nil if there was none.
	Result *PingResult
	// Err is why there was no reply.
	Err error
}

// NewPoller returns a Poller for tn. Nothing is polled until Run.
func NewPoller(tn Tailnet, log *slog.Logger) *Poller {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Poller{tn: tn, log: log, snap: Snapshot{Pings: map[string]PingRecord{}}}
}

// Run polls until ctx ends: the status every StatusInterval, each online peer
// every PingInterval, and connectivity every ConnectivityInterval.
func (p *Poller) Run(ctx context.Context) {
	// Pings need the peers, so the status comes first.
	p.pollStatus(ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		repeat(ctx, StatusInterval, func() time.Duration {
			p.pollStatus(ctx)
			return StatusInterval
		})
	})
	wg.Go(func() { repeat(ctx, 0, func() time.Duration { return p.pingPeers(ctx) }) })
	wg.Go(func() { repeat(ctx, 0, func() time.Duration { return p.checkConnectivity(ctx) }) })
	wg.Wait()
}

// Snapshot returns what the poller knows now.
func (p *Poller) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.snap
	s.Pings = maps.Clone(s.Pings)
	return s
}

// repeat calls f after first, and then again after each duration f returns,
// until ctx ends.
func repeat(ctx context.Context, first time.Duration, f func() time.Duration) {
	t := time.NewTimer(first)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		t.Reset(f())
	}
}

func (p *Poller) pollStatus(ctx context.Context) {
	st, err := p.tn.Status(ctx)
	if ctx.Err() != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		if p.snap.StatusErr == nil {
			p.log.Warn("get the tailnet status", "error", err)
		}
		p.snap.StatusErr = err
		return
	}
	if p.snap.StatusErr != nil {
		p.log.Info("got the tailnet status again")
	}
	p.snap.Status, p.snap.StatusAt, p.snap.StatusErr = st, time.Now(), nil
}

// pingPeers pings every online peer and returns when to do it again.
func (p *Poller) pingPeers(ctx context.Context) time.Duration {
	if !p.running() {
		// Try again soon after the node is on the tailnet.
		return StatusInterval
	}
	p.mu.Lock()
	st := p.snap.Status
	p.mu.Unlock()

	var (
		mu      sync.Mutex
		results = make(map[string]PingRecord)
		wg      sync.WaitGroup
		slots   = make(chan struct{}, maxPings)
	)
	for _, d := range st.Peers {
		ip, ok := pingAddr(d)
		if !d.Online || !ok {
			continue
		}
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
			defer cancel()
			res, err := p.tn.Ping(pingCtx, ip)
			if err != nil && pingCtx.Err() != nil {
				err = errNoReply
			}
			mu.Lock()
			results[d.ID] = PingRecord{At: time.Now(), Result: res, Err: err}
			mu.Unlock()
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		return PingInterval
	}

	// Keep the last ping of a peer that's offline now, and forget peers that
	// have left.
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, d := range st.Peers {
		if _, ok := results[d.ID]; !ok {
			if old, ok := p.snap.Pings[d.ID]; ok {
				results[d.ID] = old
			}
		}
	}
	p.snap.Pings = results
	return PingInterval
}

// running reports whether the latest status says the node is on the tailnet.
func (p *Poller) running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snap.Status != nil && p.snap.Status.State == StateRunning
}

// pingAddr picks the address to ping a device at, preferring IPv4.
func pingAddr(d Device) (netip.Addr, bool) {
	for _, ip := range d.IPs {
		if ip.Is4() {
			return ip, true
		}
	}
	if len(d.IPs) > 0 {
		return d.IPs[0], true
	}
	return netip.Addr{}, false
}

// errNoReply is the error of a ping that timed out.
var errNoReply = fmt.Errorf("no reply within %v", pingTimeout)

// checkConnectivity runs a connectivity check and returns when to do it again.
func (p *Poller) checkConnectivity(ctx context.Context) time.Duration {
	// Before the node is on the tailnet, it has no DERP map to check against.
	if !p.running() {
		return StatusInterval
	}
	checkCtx, cancel := context.WithTimeout(ctx, connectivityTimeout)
	defer cancel()
	c, err := p.tn.Connectivity(checkCtx)
	if ctx.Err() != nil {
		return ConnectivityInterval
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		if p.snap.ConnectivityErr == nil {
			p.log.Warn("check connectivity", "error", err)
		}
		p.snap.ConnectivityErr = err
		return retryInterval
	}
	p.snap.Connectivity, p.snap.ConnectivityAt, p.snap.ConnectivityErr = c, time.Now(), nil
	return ConnectivityInterval
}

// PathKind says how traffic gets from Roost's node to a device.
type PathKind string

// The kinds of Path.
const (
	// PathSelf is Roost's own node.
	PathSelf PathKind = "self"
	// PathDirect is a peer-to-peer WireGuard connection.
	PathDirect PathKind = "direct"
	// PathDERP is relayed through a DERP server.
	PathDERP PathKind = "derp"
	// PathPeerRelay is relayed through another node on the tailnet.
	PathPeerRelay PathKind = "peer-relay"
	// PathOffline means the device isn't connected.
	PathOffline PathKind = "offline"
)

// Path is how traffic gets from Roost's node to a device.
type Path struct {
	Kind PathKind
	// Endpoint is the device's ip:port on a direct path.
	Endpoint string
	// Relay is the DERP region's code on a DERP path, or the relay's
	// ip:port:vni on a peer relay path.
	Relay string
	// Latency is the round trip of the latest ping, if it's fresh and worked,
	// or zero.
	Latency time.Duration
	// PingedAt is when the device was last pinged, or zero if it hasn't been.
	PingedAt time.Time
}

// PathTo works out the path to d at now: from its latest ping, if that's
// fresh and worked, or else from the status.
func (s Snapshot) PathTo(d Device, now time.Time) Path {
	if s.Status != nil && d.ID == s.Status.Self.ID {
		return Path{Kind: PathSelf}
	}
	ping, pinged := s.Pings[d.ID]
	path := Path{PingedAt: ping.At}
	switch res := ping.Result; {
	case !d.Online:
		path.Kind = PathOffline
	case pinged && res != nil && now.Sub(ping.At) < pingFresh:
		path.Latency = res.Latency
		switch {
		case res.DERPRegion != "":
			path.Kind, path.Relay = PathDERP, res.DERPRegion
		case res.PeerRelay != "":
			path.Kind, path.Relay = PathPeerRelay, res.PeerRelay
		default:
			path.Kind, path.Endpoint = PathDirect, res.Endpoint
		}
	case d.CurAddr != "":
		path.Kind, path.Endpoint = PathDirect, d.CurAddr
	case d.PeerRelay != "":
		path.Kind, path.Relay = PathPeerRelay, d.PeerRelay
	default:
		path.Kind, path.Relay = PathDERP, d.Relay
	}
	return path
}

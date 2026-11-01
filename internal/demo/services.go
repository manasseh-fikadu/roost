package demo

import (
	"cmp"
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

// A probe is a check of the demo, and how its target behaves. The same
// numbers make the target answer now and the check's history, so a failure
// that started last night is in both.
type probe struct {
	name, group, kind, target string
	interval, timeout         time.Duration
	// expect is the HTTP status the check wants, or 0.
	expect int
	// latency is how long the target takes to answer, usually.
	latency time.Duration
	// outages are the spans when it's down, measured from the start of the
	// demo. A span with no end is still going.
	outages []span
	// flaky is the share of runs that fail for no reason.
	flaky float64
	// onHost is the machine the target runs on, which doesn't answer while
	// it's offline.
	onHost string
}

type span struct{ from, to time.Duration }

// open means the outage hasn't ended.
const open = time.Duration(1 << 62)

// probes are the checks of the base demo.
var probes = []probe{
	{name: "Home Assistant", group: "Home", kind: "http", target: "http://homelab.tail-demo.ts.net:8123/", expect: 200, latency: 38 * time.Millisecond, onHost: "homelab"},
	{name: "Pi-hole", group: "Home", kind: "http", target: "http://homelab.tail-demo.ts.net:8081/admin/login", expect: 200, latency: 22 * time.Millisecond, onHost: "homelab"},
	{name: "Garage camera", group: "Home", kind: "http", target: "http://pi-garage.tail-demo.ts.net:8081/", latency: 70 * time.Millisecond, onHost: "pi-garage",
		outages: []span{{-3*time.Hour - 20*time.Minute, open}}},
	{name: "MQTT broker", group: "Home", kind: "tcp", target: "homelab.tail-demo.ts.net:1883", latency: 6 * time.Millisecond, onHost: "homelab"},
	{name: "Jellyfin", group: "Media", kind: "http", target: "http://nas.tail-demo.ts.net:8096/health", expect: 200, latency: 31 * time.Millisecond, onHost: "nas"},
	{name: "Photos", group: "Media", kind: "http", target: "http://nas.tail-demo.ts.net:2283/api/server/ping", expect: 200, latency: 54 * time.Millisecond, onHost: "nas", flaky: 0.015},
	{name: "Paperless", group: "Media", kind: "http", target: "http://nas.tail-demo.ts.net:8000/", expect: 302, latency: 47 * time.Millisecond, onHost: "nas"},
	{name: "Blog", group: "Public", kind: "http", target: "http://vps-fra.tail-demo.ts.net:2368/", expect: 200, latency: 83 * time.Millisecond, onHost: "vps-fra",
		outages: []span{{-31 * time.Hour, -30*time.Hour - 40*time.Minute}}},
	{name: "Status page", group: "Public", kind: "http", target: "http://vps-nyc.tail-demo.ts.net:3001/", expect: 200, latency: 151 * time.Millisecond, onHost: "vps-nyc",
		outages: []span{{-27 * time.Hour, -25*time.Hour - 20*time.Minute}}},
	{name: "Dev database", group: "Dev", kind: "tcp", target: "desktop.tail-demo.ts.net:5432", latency: 4 * time.Millisecond, onHost: "desktop"},
	{name: "Grafana", group: "Dev", kind: "http", target: "http://homelab.tail-demo.ts.net:3000/api/health", expect: 200, latency: 29 * time.Millisecond, onHost: "homelab"},
	{name: "DNS for example.com", group: "Public", kind: "dns", target: "example.com", latency: 17 * time.Millisecond},
	{name: "Cloudflare edge", group: "Internet", kind: "tcp", target: "1.1.1.1:443", interval: 30 * time.Second, timeout: 3 * time.Second, latency: 12 * time.Millisecond},
	{name: "DNS resolution", group: "Internet", kind: "dns", target: "cloudflare.com", timeout: 3 * time.Second, latency: 9 * time.Millisecond},
}

// downAt reports whether the probe's target is down at t.
func (p probe) downAt(t, start time.Time) bool {
	for _, o := range p.outages {
		if !t.Before(start.Add(o.from)) && (o.to == open || t.Before(start.Add(o.to))) {
			return true
		}
	}
	return false
}

// flakyAt reports whether a run at t fails for no reason.
func (p probe) flakyAt(t time.Time, seed uint64) bool {
	return p.flaky > 0 && unit(seed^hashString(p.name), t.Unix()/30) < p.flaky
}

// latencyAt returns how long the target takes at t.
func (p probe) latencyAt(t time.Time, seed uint64) time.Duration {
	return time.Duration(float64(p.latency) * (1 + 0.35*wave(seed^hashString(p.name), t, 3*time.Minute)))
}

// services are the demo's servers that the checks and notifications reach:
// the ones the probes name, and the places the channels send to.
type services struct {
	d *Demo
	// byAddr holds the probes by the address they name, as "host:port".
	byAddr map[string]probe
	// sinks are the addresses that take notifications.
	sinks map[string]bool
}

func newServices(d *Demo) *services {
	s := &services{d: d, byAddr: map[string]probe{}, sinks: map[string]bool{}}
	for _, p := range probes {
		addr := p.target
		if p.kind == "http" {
			addr = strings.TrimPrefix(addr, "http://")
			addr, _, _ = strings.Cut(addr, "/")
		}
		if p.kind != "dns" {
			s.byAddr[strings.ToLower(addr)] = p
		}
	}
	s.sinks["homelab.tail-demo.ts.net:8090"] = true
	s.sinks["homelab.tail-demo.ts.net:1880"] = true
	return s
}

// dial connects to a made-up server by the address a check names. It
// refuses what isn't one, as a network with nothing else on it would.
func (s *services) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	key := strings.ToLower(strings.TrimSuffix(host, ".")) + ":" + port
	if s.sinks[key] {
		return serveHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})), nil
	}
	p, ok := s.byAddr[key]
	if !ok {
		return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
	}
	now := time.Now()
	m := s.d.machineNamed(p.onHost, nil)
	switch {
	case m != nil && m.offlineFor > 0:
		// A machine that is off doesn't answer, so the check waits.
		<-ctx.Done()
		return nil, &net.OpError{Op: "dial", Net: network, Err: context.DeadlineExceeded}
	case p.downAt(now, s.d.start) || p.flakyAt(now, s.d.seed):
		if p.kind == "http" {
			return serveHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
			})), nil
		}
		return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
	}
	latency := p.latencyAt(now, s.d.seed)
	select {
	case <-time.After(latency / 2):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if p.kind == "tcp" {
		client, server := net.Pipe()
		go func() { _, _ = io.Copy(io.Discard, server); _ = server.Close() }()
		return client, nil
	}
	return serveHTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(latency / 2)
		if p.expect >= 300 && p.expect < 400 {
			w.Header().Set("Location", "/login")
		}
		w.WriteHeader(cmpStatus(p.expect))
	})), nil
}

// cmpStatus returns code, or 200 for no code.
func cmpStatus(code int) int {
	if code == 0 {
		return http.StatusOK
	}
	return code
}

// serveHTTP returns a connection to an in-memory HTTP server that answers with h.
func serveHTTP(h http.Handler) net.Conn {
	client, server := net.Pipe()
	go func() {
		done := make(chan struct{})
		srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
		_ = srv.Serve(&oneConn{conn: &notifyConn{Conn: server, done: done}, done: done})
	}()
	return client
}

// lookup resolves the names of the demo's DNS checks.
func (s *services) lookup(_ context.Context, host string) ([]netip.Addr, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "example.com" || host == "cloudflare.com" {
		return []netip.Addr{netip.MustParseAddr("203.0.113.10"), netip.MustParseAddr("2001:db8::10")}, nil
	}
	for _, m := range s.d.machines {
		if host == m.dns() {
			return []netip.Addr{m.ip4}, nil
		}
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// Lookup resolves a DNS check's name, for checks.Options.
func (d *Demo) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return d.services.lookup(ctx, host)
}

// storeChecks returns the checks to put in the store.
func (d *Demo) storeChecks() []store.Check {
	out := make([]store.Check, 0, len(probes))
	for i, p := range probes {
		out = append(out, store.Check{
			Name: p.name, Group: p.group, Kind: p.kind, Target: p.target, Interval: cmp.Or(p.interval, time.Minute),
			Timeout: cmp.Or(p.timeout, 10*time.Second), ExpectStatus: p.expect, Created: d.start.Add(-time.Duration(40-i) * 24 * time.Hour),
		})
	}
	return out
}

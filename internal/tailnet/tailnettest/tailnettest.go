// Package tailnettest provides an in-memory tailnet for tests, so they never
// need a real one. Its connections are net.Pipe pairs, which block on
// channels rather than sockets, so it works inside synctest.
package tailnettest

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sync"

	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// URL is where a Tailnet says the app is served. Requests from Client reach
// the app whatever host they name.
const URL = "http://roost.example.ts.net"

// Tailnet is a fake tailnet.Node whose answers the test sets. Create one with
// New.
type Tailnet struct {
	mu        sync.Mutex
	status    tailnet.Status
	statusErr error
	pings     map[netip.Addr]pingReply
	conn      *tailnet.Connectivity
	connErr   error
	regions   []tailnet.DERPRegion
	dial      func(ctx context.Context, network, addr string) (net.Conn, error)
	running   chan struct{} // closed once the state is Running
	ln        *listener
	closed    bool
	port      uint16 // the last port Client dialed from
}

type pingReply struct {
	res *tailnet.PingResult
	err error
}

var _ tailnet.Node = (*Tailnet)(nil)

// New returns a fake tailnet in status st. Set st.State to
// tailnet.StateRunning for a node that's already logged in.
func New(st tailnet.Status) *Tailnet {
	t := &Tailnet{pings: map[netip.Addr]pingReply{}, running: make(chan struct{})}
	t.SetStatus(st)
	return t
}

// SetStatus changes the status. Setting the state to tailnet.StateRunning
// lets Listen return.
func (t *Tailnet) SetStatus(st tailnet.Status) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status = cloneStatus(st)
	if st.State == tailnet.StateRunning {
		select {
		case <-t.running:
		default:
			close(t.running)
		}
	}
}

// FailStatus makes Status return err, until it's called again with nil.
func (t *Tailnet) FailStatus(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.statusErr = err
}

// SetPing sets the reply to pings of ip: res, or err if res is nil. A ping
// of an address with no reply set waits until its context ends, like a ping
// that gets no pong.
func (t *Tailnet) SetPing(ip netip.Addr, res *tailnet.PingResult, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pings[ip] = pingReply{res: res, err: err}
}

// SetConnectivity sets what Connectivity returns.
func (t *Tailnet) SetConnectivity(c *tailnet.Connectivity, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.conn, t.connErr = c, err
}

// SetDERPMap sets what DERPMap returns.
func (t *Tailnet) SetDERPMap(regions []tailnet.DERPRegion) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.regions = slices.Clone(regions)
}

// SetDialer makes Dial call dial. Until it's set, Dial fails.
func (t *Tailnet) SetDialer(dial func(ctx context.Context, network, addr string) (net.Conn, error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.dial = dial
}

// Status returns the status the test set.
func (t *Tailnet) Status(context.Context) (*tailnet.Status, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.statusErr != nil {
		return nil, t.statusErr
	}
	st := cloneStatus(t.status)
	return &st, nil
}

// Ping returns the reply set with SetPing.
func (t *Tailnet) Ping(ctx context.Context, ip netip.Addr) (*tailnet.PingResult, error) {
	t.mu.Lock()
	reply, ok := t.pings[ip]
	t.mu.Unlock()
	if !ok {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if reply.res == nil {
		return nil, reply.err
	}
	res := *reply.res
	return &res, nil
}

// WhoIs identifies remoteAddr by the device in the status that has its IP.
// The device's Owner is the identity's LoginName and DisplayName, unless the
// device is tagged: like Tailscale, WhoIs names no user for a tagged device.
func (t *Tailnet) WhoIs(_ context.Context, remoteAddr string) (*tailnet.Identity, error) {
	ap, err := netip.ParseAddrPort(remoteAddr)
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, d := range append([]tailnet.Device{t.status.Self}, t.status.Peers...) {
		if slices.Contains(d.IPs, ap.Addr().Unmap()) {
			id := &tailnet.Identity{NodeID: d.ID, NodeName: d.DNSName, Tags: slices.Clone(d.Tags)}
			if len(d.Tags) == 0 {
				id.LoginName, id.DisplayName = d.Owner, d.Owner
			}
			return id, nil
		}
	}
	return nil, tailnet.ErrUnknownPeer
}

// DERPMap returns the regions set with SetDERPMap.
func (t *Tailnet) DERPMap(context.Context) ([]tailnet.DERPRegion, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.regions), nil
}

// Connectivity returns what SetConnectivity set, or an error if nothing was.
func (t *Tailnet) Connectivity(context.Context) (*tailnet.Connectivity, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil {
		return nil, cmp.Or(t.connErr, errors.New("tailnettest: no connectivity set"))
	}
	c := *t.conn
	return &c, nil
}

// Dial calls the function set with SetDialer.
func (t *Tailnet) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	t.mu.Lock()
	dial := t.dial
	t.mu.Unlock()
	if dial == nil {
		return nil, fmt.Errorf("tailnettest: dial %s %s: no dialer set", network, addr)
	}
	return dial(ctx, network, addr)
}

// Listen waits until the state is tailnet.StateRunning, then returns an
// in-memory listener that Client connects to, and URL.
func (t *Tailnet) Listen(ctx context.Context) (net.Listener, string, error) {
	select {
	case <-t.running:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, "", net.ErrClosed
	}
	if t.ln != nil {
		return nil, "", errors.New("tailnettest: Listen was already called")
	}
	addr := netip.AddrPortFrom(netip.IPv4Unspecified(), 80)
	if len(t.status.Self.IPs) > 0 {
		addr = netip.AddrPortFrom(t.status.Self.IPs[0], 80)
	}
	t.ln = newListener(addr)
	return t.ln, URL, nil
}

// Close closes the listener.
func (t *Tailnet) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	if t.ln != nil {
		return t.ln.Close()
	}
	return nil
}

// Client returns an HTTP client whose requests reach the app as if they came
// from the tailnet address from, so WhoIs identifies them as that device.
func (t *Tailnet) Client(from netip.Addr) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			t.mu.Lock()
			ln := t.ln
			t.port++
			port := 40000 + t.port
			t.mu.Unlock()
			if ln == nil {
				return nil, errors.New("tailnettest: the app isn't listening")
			}
			return ln.dial(ctx, netip.AddrPortFrom(from, port))
		},
	}}
}

func cloneStatus(st tailnet.Status) tailnet.Status {
	st.Self = cloneDevice(st.Self)
	st.Peers = slices.Clone(st.Peers)
	for i := range st.Peers {
		st.Peers[i] = cloneDevice(st.Peers[i])
	}
	return st
}

func cloneDevice(d tailnet.Device) tailnet.Device {
	d.IPs = slices.Clone(d.IPs)
	d.Tags = slices.Clone(d.Tags)
	return d
}

// listener is an in-memory net.Listener.
type listener struct {
	addr  net.Addr
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newListener(addr netip.AddrPort) *listener {
	return &listener{
		addr:  net.TCPAddrFromAddrPort(addr),
		conns: make(chan net.Conn),
		done:  make(chan struct{}),
	}
}

func (l *listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *listener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *listener) Addr() net.Addr { return l.addr }

// dial connects to the listener from the address from.
func (l *listener) dial(ctx context.Context, from netip.AddrPort) (net.Conn, error) {
	client, server := net.Pipe()
	fromAddr := net.TCPAddrFromAddrPort(from)
	select {
	case l.conns <- &conn{Conn: server, local: l.addr, remote: fromAddr}:
		return &conn{Conn: client, local: fromAddr, remote: l.addr}, nil
	case <-l.done:
		return nil, errors.Join(net.ErrClosed, client.Close(), server.Close())
	case <-ctx.Done():
		return nil, errors.Join(ctx.Err(), client.Close(), server.Close())
	}
}

// conn is one end of a net.Pipe with TCP addresses.
type conn struct {
	net.Conn
	local, remote net.Addr
}

func (c *conn) LocalAddr() net.Addr  { return c.local }
func (c *conn) RemoteAddr() net.Addr { return c.remote }

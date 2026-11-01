package demo

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"time"

	"github.com/manasseh-fikadu/bield/internal/tailnet"
	"github.com/manasseh-fikadu/bield/internal/tailnet/tailnettest"
)

// Node is the demo's Tailscale layer. It is a tailnettest.Tailnet, which
// answers for a tailnet that doesn't exist, that the demo keeps up to date,
// except that it serves the app on a TCP port of this machine, where people
// can open it, and takes everyone who does for Alex.
type Node struct {
	*tailnettest.Tailnet
	listen  string
	visitor tailnet.Identity

	ln net.Listener
}

var _ tailnet.Node = (*Node)(nil)

// Listen serves the app on the demo's address. It returns the URL people open
// it at.
func (n *Node) Listen(ctx context.Context) (net.Listener, string, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", n.listen)
	if err != nil {
		return nil, "", err
	}
	n.ln = ln
	return ln, localURL(ln.Addr()), nil
}

// Close stops serving the app. The server that serves it closes the listener
// too, and a second close doesn't matter.
func (n *Node) Close() error {
	if n.ln == nil {
		return nil
	}
	if err := n.ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

// WhoIs says everyone is Alex, on Alex's laptop. There's no tailnet to say who
// someone is, and a demo has nothing worth keeping from them.
func (n *Node) WhoIs(context.Context, string) (*tailnet.Identity, error) {
	who := n.visitor
	return &who, nil
}

// localURL returns the http URL of a local listener's address, naming localhost
// if it listens on every address.
func localURL(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok || !tcp.IP.IsUnspecified() {
		return "http://" + addr.String()
	}
	return "http://" + net.JoinHostPort("localhost", strconv.Itoa(tcp.Port))
}

// derpRegions are the relay regions of the demo's tailnet, which the machines'
// paths can name.
var derpRegions = []tailnet.DERPRegion{
	{ID: 1, Code: "nyc", Name: "New York City"},
	{ID: 2, Code: "sfo", Name: "San Francisco"},
	{ID: 4, Code: "fra", Name: "Frankfurt"},
	{ID: 5, Code: "sin", Name: "Singapore"},
	{ID: 9, Code: "ams", Name: "Amsterdam"},
	{ID: 10, Code: "lhr", Name: "London"},
}

// status builds the tailnet's status at now.
func (d *Demo) status(now time.Time) tailnet.Status {
	st := tailnet.Status{State: tailnet.StateRunning, Tailnet: tailnetName}
	for i, m := range d.machines {
		dev := d.device(m, now)
		if i == 0 {
			st.Self = dev
			continue
		}
		st.Peers = append(st.Peers, dev)
	}
	return st
}

// device returns machine m as a device of the tailnet at now.
func (d *Demo) device(m *machine, now time.Time) tailnet.Device {
	// The traffic counts since the hub started.
	seen := now.Sub(d.start.Add(-72 * time.Hour)).Seconds()
	dev := tailnet.Device{
		ID: m.id, HostName: m.host, DNSName: m.dns(), OS: m.os,
		IPs:   []netip.Addr{m.ip4, m.ip6},
		Owner: m.owner, Tags: m.tags,
		Online:  m.offlineFor == 0,
		Relay:   m.relay,
		RxBytes: int64(m.rx * 0.35 * seen), TxBytes: int64(m.tx * 0.35 * seen),
		Created:      d.start.Add(-m.joined),
		ExitNode:     m.exitNode,
		TailscaleSSH: m.tailscaleSSH,
	}
	if dev.Relay == "" {
		dev.Relay = "fra"
	}
	if m.keyIn > 0 {
		dev.KeyExpiry = d.start.Add(m.keyIn)
	}
	switch {
	case m.index == 0:
		dev.Relay = "fra"
	case !dev.Online:
		dev.LastSeen = d.start.Add(-m.offlineFor)
	case m.direct && m.endpoint.IsValid():
		dev.CurAddr = m.endpoint.String()
	case m.direct:
		// A machine out on the internet, with an address of its own.
		dev.CurAddr = netip.AddrPortFrom(documentationAddr(m.index), 41641).String()
	}
	return dev
}

// documentationAddr returns an address from the range set aside for examples
// in documentation (TEST-NET-3), so a demo never names a real machine.
func documentationAddr(i int) netip.Addr {
	return netip.AddrFrom4([4]byte{203, 0, 113, byte(i%250 + 1)}) //nolint:gosec // G115: the modulus is under 256.
}

// refreshPings sets the reply to a ping of each online machine at now: a
// little slower or faster each time.
func (d *Demo) refreshPings(now time.Time) {
	for _, m := range d.machines[1:] {
		if m.offlineFor > 0 {
			continue
		}
		rtt := m.rtt
		if rtt == 0 {
			rtt = 40 * time.Millisecond
		}
		jitter := 1 + 0.18*wave(m.seed(11), now, 45*time.Second)
		res := &tailnet.PingResult{Latency: time.Duration(float64(rtt) * jitter)}
		switch {
		case m.direct && m.endpoint.IsValid():
			res.Endpoint = m.endpoint.String()
		case m.direct:
			res.Endpoint = netip.AddrPortFrom(documentationAddr(m.index), 41641).String()
		default:
			res.DERPRegion = m.relay
			if res.DERPRegion == "" {
				res.DERPRegion = "fra"
			}
		}
		d.node.SetPing(m.ip4, res, nil)
		d.node.SetPing(m.ip6, res, nil)
	}
}

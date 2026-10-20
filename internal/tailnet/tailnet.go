// Package tailnet is Roost's connection to the tailnet. Roost either joins the
// tailnet as its own node with tsnet (Embedded), or uses the tailscaled that
// already runs on the machine (Host). Both answer the same questions through
// the Tailnet interface, and package tailnettest fakes it for tests.
package tailnet

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"time"
)

// Tailnet answers Roost's questions about the tailnet. Embedded, Host and
// tailnettest.Tailnet implement it.
type Tailnet interface {
	// Status returns this node, its peers and whether it's logged in.
	Status(ctx context.Context) (*Status, error)
	// Ping sends one disco ping to ip and reports the path it took. It fails
	// if no pong comes back before ctx ends.
	Ping(ctx context.Context, ip netip.Addr) (*PingResult, error)
	// WhoIs returns the node and user at remoteAddr, an ip:port such as an
	// http.Request's RemoteAddr. It returns ErrUnknownPeer if no node on the
	// tailnet has that address.
	WhoIs(ctx context.Context, remoteAddr string) (*Identity, error)
	// DERPMap returns the DERP relay regions, ordered by ID.
	DERPMap(ctx context.Context) ([]DERPRegion, error)
	// Connectivity checks this machine's network, as `tailscale netcheck` does.
	// It takes a few seconds.
	Connectivity(ctx context.Context) (*Connectivity, error)
	// Dial connects to addr on the tailnet.
	Dial(ctx context.Context, network, addr string) (net.Conn, error)
}

// Node is a Tailnet that Roost serves its app on.
type Node interface {
	Tailnet
	// Listen waits until the node is on the tailnet, then returns a listener
	// for the app and the URL people open it at. Call it once.
	Listen(ctx context.Context) (ln net.Listener, url string, err error)
	// Close disconnects from the tailnet and closes the listener.
	Close() error
}

// ErrUnknownPeer means no node on the tailnet has the address given to WhoIs.
var ErrUnknownPeer = errors.New("no node on the tailnet has this address")

// Backend states that Status reports. Tailscale has a few others, such as
// Starting, Stopped and NeedsMachineAuth.
const (
	// StateRunning means the node is on the tailnet.
	StateRunning = "Running"
	// StateNeedsLogin means someone has to log the node in, at Status.LoginURL.
	StateNeedsLogin = "NeedsLogin"
)

// Status is the node's view of the tailnet at one moment.
type Status struct {
	// State is the node's backend state, such as StateRunning.
	State string
	// LoginURL is where to log the node in while State is StateNeedsLogin.
	// It's empty until Tailscale has made one.
	LoginURL string
	// Tailnet is the tailnet's name, such as example.com or user@github.
	Tailnet string
	// Self is the node Roost runs as: its own node in embedded mode, or the
	// machine's in host mode.
	Self Device
	// Peers are the other nodes this one can see, ordered by DNS name.
	Peers []Device
}

// Device is one node on the tailnet.
type Device struct {
	// ID is the node's stable ID.
	ID string
	// HostName is the name the machine reports, which may not be unique.
	HostName string
	// DNSName is the node's MagicDNS name without the final dot, such as
	// laptop.example.ts.net.
	DNSName string
	OS      string
	IPs     []netip.Addr
	// Owner is the login name of the user who owns the node, or "" for a
	// tagged node, which belongs to no user.
	Owner string
	Tags  []string
	// Online reports whether the node is connected to the control server.
	Online bool
	// LastSeen is when an offline node was last connected to the control
	// server. It's zero while the node is online.
	LastSeen time.Time
	// CurAddr is the ip:port of the direct path to the node, or "" if traffic
	// to it is relayed or there's been none lately.
	CurAddr string
	// Relay is the code of the node's home DERP region, such as "fra".
	Relay string
	// PeerRelay is the ip:port:vni of the peer relay that carries traffic to
	// the node, if one does.
	PeerRelay string
	// RxBytes and TxBytes count the traffic with the node since this node
	// started.
	RxBytes, TxBytes int64
	// Created is when the node joined the tailnet.
	Created time.Time
	// KeyExpiry is when the node's key expires, or zero if it doesn't.
	KeyExpiry time.Time
	// ExitNode reports whether this node routes our internet traffic.
	ExitNode bool
	// SameMachine marks the peer that is the machine Roost runs on. Only an
	// Embedded node sets it, since Roost is a separate node there.
	SameMachine bool
}

// PingResult is the reply to one Ping.
type PingResult struct {
	// Latency is the round-trip time.
	Latency time.Duration
	// Endpoint is the ip:port the pong came from on a direct path, or "" if
	// the ping was relayed.
	Endpoint string
	// DERPRegion is the code of the DERP region that relayed the ping, or ""
	// if it wasn't relayed through DERP.
	DERPRegion string
	// PeerRelay is the ip:port:vni of the peer relay that carried the ping, if
	// one did.
	PeerRelay string
}

// Identity is who is at the other end of a connection.
type Identity struct {
	// NodeID is the stable ID of the node the connection comes from.
	NodeID string
	// NodeName is the node's MagicDNS name without the final dot.
	NodeName string
	// Tags are the node's ACL tags. A tagged node belongs to no user, so
	// LoginName is empty when there are tags.
	Tags []string
	// LoginName is the login of the user who owns the node, such as
	// alice@example.com, or "" if it has none.
	LoginName string
	// DisplayName is the user's name, for showing in the UI.
	DisplayName string
}

// DERPRegion is a region of DERP relay servers.
type DERPRegion struct {
	ID int64
	// Code is the region's short name, such as "fra".
	Code string
	// Name is the region's full name, such as "Frankfurt".
	Name string
}

// Connectivity is what a netcheck found out about this machine's network.
type Connectivity struct {
	// UDP reports whether UDP gets out, which direct connections need.
	UDP bool
	// IPv4 and IPv6 report whether a STUN round trip worked over each.
	IPv4, IPv6 bool
	// PublicIPv4 and PublicIPv6 are the addresses the internet sees this
	// machine at. Each is the zero Addr if unknown.
	PublicIPv4, PublicIPv6 netip.Addr
	// PreferredDERP is the DERP region with the lowest latency, or the zero
	// DERPRegion if none answered.
	PreferredDERP DERPRegion
	// DERPLatency is the latency to PreferredDERP.
	DERPLatency time.Duration
}

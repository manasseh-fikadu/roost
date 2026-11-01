package tailnet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/tsnet"
)

// Embedded is a Tailscale node that runs inside Roost with tsnet, so Roost is
// its own machine on the tailnet, with its own name and HTTPS certificate. It
// needs nothing from the machine it runs on, so it works in a container.
//
// The machine Roost runs on may also be on the tailnet as a separate node.
// Status marks that peer as SameMachine when one of its tailnet IPs is on a
// local network interface, which is the case when the machine's tailscaled
// uses a TUN device (the default). In a container, or with userspace
// networking, the addresses aren't visible, so the machine shows up as an
// ordinary peer. Host names aren't compared, since Roost's node is named
// after Roost, not the machine.
type Embedded struct {
	localAPI
	srv *tsnet.Server
	log *slog.Logger

	mu       sync.Mutex
	redirect *http.Server // serves port 80 when the app is on HTTPS
}

// EmbeddedOptions configures NewEmbedded.
type EmbeddedOptions struct {
	// Dir is the directory that keeps the node's keys and state.
	Dir string
	// Hostname is the node's name on the tailnet, and the first label of its
	// MagicDNS name.
	Hostname string
	// ControlURL is the control server the node logs in to, such as a
	// Headscale server's. If it's empty, the node uses TS_CONTROL_URL, and
	// then Tailscale's.
	ControlURL string
	// Logger receives tsnet's messages at debug level. Nil discards them.
	Logger *slog.Logger
}

// NewEmbedded starts a tsnet node. It returns before the node is on the
// tailnet: until someone logs it in, Status reports StateNeedsLogin and a
// LoginURL. On its first start, the node logs in by itself if TS_AUTHKEY holds
// an auth key.
func NewEmbedded(opts EmbeddedOptions) (*Embedded, error) {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	// AuthKey is left empty, so tsnet reads TS_AUTHKEY (or TS_AUTH_KEY) itself.
	srv := &tsnet.Server{
		Dir:        opts.Dir,
		Hostname:   opts.Hostname,
		ControlURL: opts.ControlURL,
		// tsnet repeats the login URL every few seconds; Roost reports it once.
		UserLogf: func(format string, args ...any) {
			log.Debug("tsnet", "message", fmt.Sprintf(format, args...))
		},
	}
	if err := srv.Start(); err != nil {
		return nil, err
	}
	lc, err := srv.LocalClient()
	if err != nil {
		return nil, errors.Join(err, srv.Close())
	}
	return &Embedded{lc: lc, srv: srv, log: log}, nil
}

// Status is localAPI.Status, plus marking the peer that is this machine.
func (e *Embedded) Status(ctx context.Context) (*Status, error) {
	st, err := e.localAPI.Status(ctx)
	if err != nil {
		return nil, err
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		e.log.Debug("list local addresses", "error", err)
	}
	markSameMachine(st.Peers, addrs)
	return st, nil
}

// markSameMachine sets SameMachine on each peer with an IP in addrs.
func markSameMachine(peers []Device, addrs []net.Addr) {
	local := make(map[netip.Addr]bool, len(addrs))
	for _, a := range addrs {
		if p, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(p.IP); ok {
				local[ip.Unmap()] = true
			}
		}
	}
	for i := range peers {
		for _, ip := range peers[i].IPs {
			if local[ip] {
				peers[i].SameMachine = true
			}
		}
	}
}

// Dial connects to addr through the node.
func (e *Embedded) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	return e.srv.Dial(ctx, network, addr)
}

// Listen waits until the node is on the tailnet. If the tailnet has HTTPS
// certificates turned on, the app is served on port 443 and port 80 redirects
// to it. Otherwise the app is served over plain HTTP on port 80, which
// WireGuard still encrypts.
func (e *Embedded) Listen(ctx context.Context) (net.Listener, string, error) {
	st, err := waitRunning(ctx, e.lc, func(ipn.State) {})
	if err != nil {
		return nil, "", err
	}
	domain, ok := certDomain(st)
	if !ok {
		e.log.Warn("HTTPS is off for this tailnet, so Roost is served over plain HTTP; " +
			"turn on MagicDNS and HTTPS certificates in the Tailscale admin console to fix it")
		ln, err := e.srv.Listen("tcp", ":80")
		if err != nil {
			return nil, "", err
		}
		return ln, "http://" + hostForURL(st), nil
	}

	ln, err := e.srv.Listen("tcp", ":443")
	if err != nil {
		return nil, "", err
	}
	plain, err := e.srv.Listen("tcp", ":80")
	if err != nil {
		return nil, "", errors.Join(err, ln.Close())
	}
	url := "https://" + domain
	redirect := &http.Server{Handler: redirectTo(url), ReadHeaderTimeout: 10 * time.Second}
	e.mu.Lock()
	e.redirect = redirect
	e.mu.Unlock()
	go func() { _ = redirect.Serve(plain) }()

	// Get the certificate now, so the first visitor doesn't wait for it.
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), certTimeout)
		defer cancel()
		if _, _, err := e.lc.CertPair(ctx, domain); err != nil {
			e.log.Warn("get an HTTPS certificate", "domain", domain, "error", err)
		}
	}()
	return tlsListener(ln, e.lc.GetCertificate), url, nil
}

// Close shuts the node down, which closes its listeners.
func (e *Embedded) Close() error {
	e.mu.Lock()
	if e.redirect != nil {
		_ = e.redirect.Close()
	}
	e.mu.Unlock()
	return e.srv.Close()
}

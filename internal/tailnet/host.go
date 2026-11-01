package tailnet

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
)

// Host uses the tailscaled that runs on the machine, through its local
// socket. Bield listens on the machine's tailnet IPs, so tailscaled has to use
// a TUN device (the default) rather than userspace networking.
type Host struct {
	localAPI
	port int
	log  *slog.Logger

	mu sync.Mutex
	ln net.Listener
}

// HostOptions configures NewHost.
type HostOptions struct {
	// Port is the port to serve the app on, on each of the machine's tailnet
	// IPs.
	Port int
	// Socket is tailscaled's LocalAPI socket. If it's empty, the platform's
	// default is used.
	Socket string
	// Logger receives progress while Listen waits for Tailscale. Nil discards
	// it.
	Logger *slog.Logger
}

// NewHost connects to the machine's tailscaled. It fails if tailscaled isn't
// running, but not if it's logged out: Listen waits for that.
func NewHost(ctx context.Context, opts HostOptions) (*Host, error) {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	lc := &local.Client{Socket: opts.Socket}
	if _, err := lc.StatusWithoutPeers(ctx); err != nil {
		return nil, fmt.Errorf("connect to Tailscale on this machine: %w", err)
	}
	return &Host{lc: lc, port: opts.Port, log: log}, nil
}

// Dial connects to addr through the machine's network stack, which routes
// tailnet addresses to tailscaled.
func (h *Host) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

// Listen waits until the machine is on the tailnet and listens on its tailnet
// IPs. The app is served over HTTPS with the machine's certificate if the
// tailnet has HTTPS turned on and tailscaled lets Bield fetch it, and over
// plain HTTP otherwise.
func (h *Host) Listen(ctx context.Context) (net.Listener, string, error) {
	st, err := waitRunning(ctx, h.lc, func(s ipn.State) {
		if s == ipn.NeedsLogin || s == ipn.Stopped {
			h.log.Warn("waiting for Tailscale on this machine to connect; run `tailscale up`", "state", s.String())
		}
	})
	if err != nil {
		return nil, "", err
	}

	var lns []net.Listener
	var lc net.ListenConfig
	for _, ip := range st.TailscaleIPs {
		addr := netip.AddrPortFrom(ip, uint16(h.port)) //nolint:gosec // G115: config checks the port is in range.
		ln, err := lc.Listen(ctx, "tcp", addr.String())
		if err != nil {
			// An IPv6 address can fail on a machine with IPv6 turned off.
			h.log.Warn("listen on a tailnet address", "address", addr.String(), "error", err)
			continue
		}
		lns = append(lns, ln)
	}
	if len(lns) == 0 {
		return nil, "", fmt.Errorf("can't listen on any of this machine's tailnet IPs on port %d; "+
			"Bield's host mode needs tailscaled to use a TUN device, not userspace networking", h.port)
	}
	var ln net.Listener = newMultiListener(lns)

	url := appURL("http", hostForURL(st), h.port)
	if domain, ok := certDomain(st); ok {
		err := h.fetchCert(ctx, domain)
		switch {
		case local.IsAccessDeniedError(err):
			h.log.Warn("tailscaled won't give Bield this machine's HTTPS certificate, so Bield is served over plain HTTP; "+
				"run Bield as root, or make its user the Tailscale operator with `sudo tailscale set --operator=$USER`",
				"domain", domain)
		case err != nil:
			// The tailnet has HTTPS on, so keep to it, as embedded mode does:
			// each TLS handshake asks for the certificate again, and a failed
			// fetch from Let's Encrypt shouldn't move the app to another URL.
			h.log.Warn("couldn't get this machine's HTTPS certificate yet; Bield serves HTTPS anyway "+
				"and asks again on each connection", "domain", domain, "error", err)
			fallthrough
		default:
			ln = tlsListener(ln, h.lc.GetCertificate)
			url = appURL("https", domain, h.port)
		}
	}

	h.mu.Lock()
	h.ln = ln
	h.mu.Unlock()
	return ln, url, nil
}

// fetchCert gets the certificate for domain now, which also finds out whether
// tailscaled lets Bield have it.
func (h *Host) fetchCert(ctx context.Context, domain string) error {
	ctx, cancel := context.WithTimeout(ctx, certTimeout)
	defer cancel()
	_, _, err := h.lc.CertPair(ctx, domain)
	return err
}

// Close closes the listener. It leaves the machine's Tailscale alone.
func (h *Host) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ln == nil {
		return nil
	}
	return h.ln.Close()
}

package tailnet

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"tailscale.com/ipn/ipnstate"
)

// certTimeout is how long getting an HTTPS certificate may take. The first
// one for a name comes from Let's Encrypt, which takes a while.
const certTimeout = 2 * time.Minute

// tlsListener serves TLS on ln with the certificates that getCert returns. It
// offers HTTP/2, so the browser can keep many event streams open to Roost:
// net/http only speaks HTTP/2 on a TLS listener it didn't make itself if the
// listener offers it.
func tlsListener(ln net.Listener, getCert func(*tls.ClientHelloInfo) (*tls.Certificate, error)) net.Listener {
	return tls.NewListener(ln, &tls.Config{
		GetCertificate: getCert,
		MinVersion:     tls.VersionTLS12,
		NextProtos:     []string{"h2", "http/1.1"},
	})
}

// redirectTo redirects every request to the same path under base.
func redirectTo(base string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//nolint:gosec // G710: base names the host, so the request's path can't change it.
		http.Redirect(w, r, base+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
}

// certDomain returns the name the node can get an HTTPS certificate for, if
// the tailnet allows it.
func certDomain(st *ipnstate.Status) (string, bool) {
	if st.CurrentTailnet == nil || !st.CurrentTailnet.MagicDNSEnabled || len(st.CertDomains) == 0 {
		return "", false
	}
	return st.CertDomains[0], true
}

// noHTTPSMessage says why Roost serves plain HTTP when the tailnet has no
// HTTPS certificates, and what to do about it. controlURL is the control
// server if it isn't Tailscale's.
func noHTTPSMessage(controlURL string) string {
	if controlURL != "" {
		// Headscale doesn't make certificates for tailnet names.
		return "this tailnet's control server gives Roost no HTTPS certificate, as Headscale doesn't, " +
			"so Roost is served over plain HTTP, which WireGuard still encrypts"
	}
	return "HTTPS is off for this tailnet, so Roost is served over plain HTTP; " +
		"turn on MagicDNS and HTTPS certificates in the Tailscale admin console to fix it"
}

// appURL returns the app's URL at host and port, leaving the port out if it's
// the scheme's default.
func appURL(scheme, host string, port int) string {
	if (scheme == "https" && port == 443) || (scheme == "http" && port == 80) {
		return scheme + "://" + host
	}
	return scheme + "://" + host + ":" + strconv.Itoa(port)
}

// hostForURL returns the node's MagicDNS name, or one of its tailnet IPs if
// MagicDNS is off, as a URL's host. st must have an IP.
func hostForURL(st *ipnstate.Status) string {
	if st.Self != nil && st.CurrentTailnet != nil && st.CurrentTailnet.MagicDNSEnabled {
		if name := strings.TrimSuffix(st.Self.DNSName, "."); name != "" {
			return name
		}
	}
	ip := st.TailscaleIPs[0]
	for _, a := range st.TailscaleIPs {
		if a.Is4() {
			return a.String()
		}
	}
	return "[" + ip.String() + "]"
}

// multiListener accepts connections from several listeners as one. An error
// from any of them is returned from Accept.
type multiListener struct {
	lns   []net.Listener
	conns chan net.Conn
	errs  chan error
	done  chan struct{}
	once  sync.Once
}

func newMultiListener(lns []net.Listener) *multiListener {
	m := &multiListener{
		lns:   lns,
		conns: make(chan net.Conn),
		errs:  make(chan error),
		done:  make(chan struct{}),
	}
	for _, ln := range lns {
		go m.accept(ln)
	}
	return m
}

func (m *multiListener) accept(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			// http.Server retries an error that's temporary, such as running
			// out of file descriptors, and stops on any other.
			select {
			case m.errs <- err:
			case <-m.done:
				return
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		select {
		case m.conns <- c:
		case <-m.done:
			_ = c.Close()
			return
		}
	}
}

func (m *multiListener) Accept() (net.Conn, error) {
	select {
	case c := <-m.conns:
		return c, nil
	case err := <-m.errs:
		return nil, err
	case <-m.done:
		return nil, net.ErrClosed
	}
}

func (m *multiListener) Close() error {
	var err error
	m.once.Do(func() {
		close(m.done)
		for _, ln := range m.lns {
			err = errors.Join(err, ln.Close())
		}
	})
	return err
}

// Addr returns the first listener's address.
func (m *multiListener) Addr() net.Addr { return m.lns[0].Addr() }

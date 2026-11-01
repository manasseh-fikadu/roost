package checks

import (
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/manasseh-fikadu/roost/internal/netguard"
	"github.com/manasseh-fikadu/roost/internal/store"
)

// The kinds of check an admin can set up.
const (
	// KindHTTP sends a GET request to a URL, and passes if the status is the
	// one expected, or below 400 if none is.
	KindHTTP = "http"
	// KindTCP passes if a TCP connection to host:port opens.
	KindTCP = "tcp"
	// KindDNS passes if a host name resolves to an address.
	KindDNS = "dns"
)

// maxDetail is the longest a result's detail can be, in bytes.
const maxDetail = 300

// userAgent is what an HTTP check says it is.
const userAgent = "Roost-Check/1"

// DialFunc connects to addr on network, as net.Dialer.DialContext does.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// LookupFunc returns the addresses of a host name, as net.Resolver's
// LookupNetIP does with the network "ip".
type LookupFunc func(ctx context.Context, host string) ([]netip.Addr, error)

// A Result is what happened when a check ran once.
type Result struct {
	// At is when it started.
	At time.Time
	OK bool
	// Latency is how long the target took to answer, or 0 if nothing did.
	Latency time.Duration
	// Detail says what happened, such as "HTTP 200 OK", or why it failed.
	Detail string
}

// A prober runs checks.
type prober struct {
	dial   DialFunc
	lookup LookupFunc
}

// guarded returns the dial that checks connect with: p.dial, but not to
// addresses that serve credentials to whoever asks (see netguard). A DNS check
// doesn't dial, and a name only resolves.
func (p prober) guarded() DialFunc {
	return DialFunc(netguard.Dial(netguard.DialFunc(p.dial)))
}

// run runs c once, giving up after its timeout.
func (p prober) run(ctx context.Context, c store.Check) Result {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	start := time.Now()
	var r Result
	switch c.Kind {
	case KindHTTP:
		r = p.http(ctx, c)
	case KindTCP:
		r = p.tcp(ctx, c)
	case KindDNS:
		r = p.dns(ctx, c)
	default:
		r = Result{Detail: "Roost doesn't know how to run a check of kind " + c.Kind}
	}
	r.At = start
	if !r.OK && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		r.Detail = "Timed out after " + shortDuration(c.Timeout)
	}
	r.Detail = truncate(r.Detail, maxDetail)
	return r
}

func (p prober) http(ctx context.Context, c store.Check) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Target, nil)
	if err != nil {
		return failed(err)
	}
	req.Header.Set("User-Agent", userAgent)
	// A new transport for each run doesn't reuse connections, so each run
	// connects and shakes hands afresh, as a new visitor would.
	transport := &http.Transport{
		DialContext:       p.guarded(),
		DisableKeepAlives: true,
		// An admin chose to skip it, for a server with a certificate of its own.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: c.Insecure}, //nolint:gosec // G402: see above.
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		// A redirect is an answer, and following it could leave the host.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return failed(err)
	}
	latency := time.Since(start)
	_ = resp.Body.Close()

	code := resp.StatusCode
	ok := code < http.StatusBadRequest
	detail := strings.TrimSpace(fmt.Sprintf("HTTP %d %s", code, http.StatusText(code)))
	if c.ExpectStatus != 0 {
		ok = code == c.ExpectStatus
		if !ok {
			detail += fmt.Sprintf(" (expected %d)", c.ExpectStatus)
		}
	}
	return Result{OK: ok, Latency: latency, Detail: detail}
}

func (p prober) tcp(ctx context.Context, c store.Check) Result {
	start := time.Now()
	conn, err := p.guarded()(ctx, "tcp", c.Target)
	if err != nil {
		return failed(err)
	}
	latency := time.Since(start)
	_ = conn.Close()
	return Result{OK: true, Latency: latency, Detail: "Connected"}
}

func (p prober) dns(ctx context.Context, c store.Check) Result {
	start := time.Now()
	addrs, err := p.lookup(ctx, c.Target)
	if err != nil {
		return failed(err)
	}
	latency := time.Since(start)
	// IPv4 first, as most people read them.
	slices.SortStableFunc(addrs, func(a, b netip.Addr) int {
		return cmp.Compare(family(a), family(b))
	})
	switch len(addrs) {
	case 0:
		return Result{Latency: latency, Detail: "No addresses"}
	case 1:
		return Result{OK: true, Latency: latency, Detail: addrs[0].String()}
	}
	return Result{OK: true, Latency: latency, Detail: fmt.Sprintf("%v and %d more", addrs[0], len(addrs)-1)}
}

// family is 0 for an IPv4 address and 1 for an IPv6 one.
func family(a netip.Addr) int {
	if a.Unmap().Is4() {
		return 0
	}
	return 1
}

// failed returns the result of a check that failed with err. An HTTP
// check's error names the URL, which can hold a secret, so that part is
// left out.
func failed(err error) Result {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		err = ue.Err
	}
	msg := err.Error()
	// Go quotes what a server that is not HTTP said, such as the banner of an
	// SSH server on the port of an HTTP check. Viewers read results, and a
	// banner says what's running there and which version.
	if strings.Contains(msg, "malformed HTTP") {
		msg = "the server didn't answer in HTTP"
	}
	return Result{Detail: msg}
}

// resolverLookup returns a LookupFunc that asks the name servers in
// /etc/resolv.conf, as the old dashboard's checks did, connecting to them
// with dial.
func resolverLookup(dial DialFunc) LookupFunc {
	resolver := &net.Resolver{PreferGo: true, Dial: dial}
	return func(ctx context.Context, host string) ([]netip.Addr, error) {
		return resolver.LookupNetIP(ctx, "ip", host)
	}
}

// shortDuration writes d as a number of seconds, such as 5s or 1.5s, or of
// milliseconds if it's under a second.
func shortDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%gs", d.Seconds())
}

// truncate shortens s to at most n bytes, ending with an ellipsis, without
// splitting a character.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

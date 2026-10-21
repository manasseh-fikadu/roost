package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/tailnet/tailnettest"
)

// The test tailnet: Roost's node, alice's laptop (alice is an admin), bob's
// phone (bob isn't), and a tagged server. strangerIP isn't on it.
const (
	roostHost = "roost.example.ts.net"
	alice     = "alice@example.com"
	bob       = "bob@example.com"
)

var (
	roostIP    = netip.MustParseAddr("100.64.0.1")
	roostIPv6  = netip.MustParseAddr("fd7a:115c:a1e0::1")
	laptopIP   = netip.MustParseAddr("100.64.0.2")
	phoneIP    = netip.MustParseAddr("100.64.0.3")
	serverIP   = netip.MustParseAddr("100.64.0.4")
	strangerIP = netip.MustParseAddr("100.64.0.99")
)

func testTailnet() *tailnettest.Tailnet {
	return tailnettest.New(tailnet.Status{
		State: tailnet.StateRunning,
		Self:  tailnet.Device{ID: "roost", DNSName: roostHost, IPs: []netip.Addr{roostIP, roostIPv6}, Owner: alice, Online: true},
		Peers: []tailnet.Device{
			{ID: "laptop", DNSName: "laptop.example.ts.net", IPs: []netip.Addr{laptopIP}, Owner: alice, Online: true},
			{ID: "phone", DNSName: "phone.example.ts.net", IPs: []netip.Addr{phoneIP}, Owner: bob, Online: true},
			{ID: "server", DNSName: "server.example.ts.net", IPs: []netip.Addr{serverIP}, Tags: []string{"tag:server"}, Online: true},
		},
	})
}

// isAdmin says only alice is an admin.
func isAdmin(_ context.Context, login string) (bool, error) { return login == alice, nil }

// withTailnet fills in opts' required fields with tn, as of its current status.
func withTailnet(tn *tailnettest.Tailnet, opts api.Options) api.Options {
	opts.WhoIs = tn.WhoIs
	opts.Snapshot = func() tailnet.Snapshot {
		st, err := tn.Status(context.Background())
		return tailnet.Snapshot{Status: st, StatusErr: err}
	}
	opts.IsAdmin = isAdmin
	return opts
}

// newRequest returns a request for target on Roost, from the device at ip.
func newRequest(t *testing.T, method, target string, from netip.Addr) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	req.Host = roostHost
	req.RemoteAddr = netip.AddrPortFrom(from, 40000).String()
	return req
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

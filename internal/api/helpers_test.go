package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/tailnet/tailnettest"
)

// The test tailnet: Roost's node, alice's laptop (alice is an admin), bob's
// phone (bob is a viewer), carol's tablet (carol has no role), and a tagged
// server. strangerIP isn't on it.
const (
	roostHost = "roost.example.ts.net"
	alice     = "alice@example.com"
	bob       = "bob@example.com"
	carol     = "carol@example.com"
)

var (
	roostIP    = netip.MustParseAddr("100.64.0.1")
	roostIPv6  = netip.MustParseAddr("fd7a:115c:a1e0::1")
	laptopIP   = netip.MustParseAddr("100.64.0.2")
	phoneIP    = netip.MustParseAddr("100.64.0.3")
	serverIP   = netip.MustParseAddr("100.64.0.4")
	tabletIP   = netip.MustParseAddr("100.64.0.5")
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
			{ID: "tablet", DNSName: "tablet.example.ts.net", IPs: []netip.Addr{tabletIP}, Owner: carol, Online: true},
		},
	})
}

// testStore returns a new store in which alice is an admin, bob is a viewer,
// and only they can view Roost.
func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "roost.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for login, role := range map[string]store.Role{alice: store.Admin, bob: store.Viewer} {
		if _, err := st.SetRole(t.Context(), login, role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.SetWhoCanView(t.Context(), store.Listed); err != nil {
		t.Fatal(err)
	}
	return st
}

// withTailnet fills in opts' required fields: tn, as of its current status,
// and a testStore.
func withTailnet(t *testing.T, tn *tailnettest.Tailnet, opts api.Options) api.Options {
	t.Helper()
	return withStore(tn, testStore(t), opts)
}

// withStore fills in opts' required fields with tn, as of its current
// status, and st.
func withStore(tn *tailnettest.Tailnet, st *store.Store, opts api.Options) api.Options {
	opts.WhoIs = tn.WhoIs
	opts.Snapshot = func() tailnet.Snapshot {
		status, err := tn.Status(context.Background())
		return tailnet.Snapshot{Status: status, StatusErr: err}
	}
	opts.Store = st
	return opts
}

// newRequest returns a request for target on Roost, from the device at ip.
func newRequest(t *testing.T, method, target string, from netip.Addr) *http.Request {
	t.Helper()
	return newRequestBody(t, method, target, from, nil)
}

// newRequestBody returns a request with body for target on Roost, from the
// device at ip. A request that changes things gets the CSRF header.
func newRequestBody(t *testing.T, method, target string, from netip.Addr, body io.Reader) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, body)
	req.Host = roostHost
	req.RemoteAddr = netip.AddrPortFrom(from, 40000).String()
	if body != nil {
		req.Header.Set(api.CSRFHeader, "1")
	}
	return req
}

// send serves a request with a JSON body, which may be empty.
func send(t *testing.T, h http.Handler, method, target string, from netip.Addr, body string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(h, newRequestBody(t, method, target, from, strings.NewReader(body)))
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

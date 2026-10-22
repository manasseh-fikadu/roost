package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

func getMe(t *testing.T, h http.Handler, req *http.Request) api.Me {
	t.Helper()
	rec := serve(h, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/me = %d %q, want 200", rec.Code, rec.Body)
	}
	var me api.Me
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode %q: %v", rec.Body, err)
	}
	return me
}

func TestMe(t *testing.T) {
	t.Parallel()
	opts := withTailnet(t, testTailnet(), api.Options{})
	h := api.New(opts)
	tests := []struct {
		name string
		from netip.Addr
		want api.Me
	}{
		{"admin", laptopIP, api.Me{SignedIn: true, Login: alice, Name: alice, Device: "laptop.example.ts.net", Role: api.RoleAdmin}},
		{"viewer", phoneIP, api.Me{SignedIn: true, Login: bob, Name: bob, Device: "phone.example.ts.net", Role: api.RoleViewer}},
		// Only the listed users can view Roost, so carol can't.
		{"no role", tabletIP, api.Me{SignedIn: true, Login: carol, Name: carol, Device: "tablet.example.ts.net"}},
		{"tagged device", serverIP, api.Me{Reason: api.ReasonTaggedDevice}},
		{"unknown address", strangerIP, api.Me{Reason: api.ReasonUnknownDevice}},
		// In host mode, Roost's node is the machine Roost runs on. Its owner
		// is alice, but any process there could be asking.
		{"Roost's own node", roostIP, api.Me{Reason: api.ReasonRoostMachine}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := getMe(t, h, newRequest(t, http.MethodGet, "/api/me", tt.from)); got != tt.want {
				t.Errorf("GET /api/me = %+v, want %+v", got, tt.want)
			}
		})
	}

	// When anyone on the tailnet can view Roost, carol is a viewer, but
	// devices that belong to no one still aren't.
	t.Run("anyone on the tailnet", func(t *testing.T) {
		t.Parallel()
		opts := withTailnet(t, testTailnet(), api.Options{})
		if _, err := opts.Store.SetWhoCanView(t.Context(), store.Tailnet); err != nil {
			t.Fatal(err)
		}
		h := api.New(opts)
		if got := getMe(t, h, newRequest(t, http.MethodGet, "/api/me", tabletIP)); got.Role != api.RoleViewer {
			t.Errorf("GET /api/me from carol = %+v, want a viewer", got)
		}
		if got := getMe(t, h, newRequest(t, http.MethodGet, "/api/me", serverIP)); got.Role != "" {
			t.Errorf("GET /api/me from a tagged device = %+v, want no role", got)
		}
	})
}

// Identity comes only from Tailscale, never from headers, which anyone can send.
func TestMe_spoofedHeaders(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{}))
	spoof := func(req *http.Request) *http.Request {
		req.Header.Set("Tailscale-User-Login", alice)
		req.Header.Set("Tailscale-User-Name", "Alice")
		req.Header.Set("X-Forwarded-For", laptopIP.String())
		req.Header.Set("X-Real-IP", laptopIP.String())
		req.Header.Set("Forwarded", "for="+laptopIP.String())
		return req
	}

	if got := getMe(t, h, spoof(newRequest(t, http.MethodGet, "/api/me", strangerIP))); got.SignedIn || got.Role != "" {
		t.Errorf("unknown address with alice's headers = %+v, want anonymous", got)
	}
	if got := getMe(t, h, spoof(newRequest(t, http.MethodGet, "/api/me", phoneIP))); got.Login != bob || got.Role != api.RoleViewer {
		t.Errorf("bob with alice's headers = %+v, want bob, a viewer", got)
	}
	rec := serve(h, spoof(newRequest(t, http.MethodGet, "/api/debug/tailnet", strangerIP)))
	if rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/debug/tailnet from an unknown address with alice's headers = %d, want 403", rec.Code)
	}
}

func TestMe_failures(t *testing.T) {
	t.Parallel()
	tn := testTailnet()

	t.Run("Tailscale fails", func(t *testing.T) {
		t.Parallel()
		opts := withTailnet(t, tn, api.Options{})
		opts.WhoIs = func(context.Context, string) (*tailnet.Identity, error) {
			return nil, errors.New("tailscaled went away")
		}
		got := getMe(t, api.New(opts), newRequest(t, http.MethodGet, "/api/me", laptopIP))
		if want := (api.Me{Reason: api.ReasonWhoIsFailed}); got != want {
			t.Errorf("GET /api/me = %+v, want %+v", got, want)
		}
	})

	t.Run("no user", func(t *testing.T) {
		t.Parallel()
		opts := withTailnet(t, tn, api.Options{})
		opts.WhoIs = func(context.Context, string) (*tailnet.Identity, error) {
			return &tailnet.Identity{NodeID: "laptop", NodeName: "laptop.example.ts.net"}, nil
		}
		got := getMe(t, api.New(opts), newRequest(t, http.MethodGet, "/api/me", laptopIP))
		if want := (api.Me{Reason: api.ReasonNoUser}); got != want {
			t.Errorf("GET /api/me = %+v, want %+v", got, want)
		}
	})

	// If Roost can't tell what someone's role is, they have none, and Roost
	// says why, so the web app doesn't take it for a role taken away.
	t.Run("role lookup fails", func(t *testing.T) {
		t.Parallel()
		opts := withTailnet(t, tn, api.Options{})
		if err := opts.Store.Close(); err != nil {
			t.Fatal(err)
		}
		h := api.New(opts)
		got := getMe(t, h, newRequest(t, http.MethodGet, "/api/me", laptopIP))
		if !got.SignedIn || got.Role != "" || got.Reason != api.ReasonRoleLookupFailed {
			t.Errorf("GET /api/me = %+v, want alice with no role, because the lookup failed", got)
		}
		if rec := serve(h, newRequest(t, http.MethodGet, "/api/debug/tailnet", laptopIP)); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("GET /api/debug/tailnet = %d, want 503", rec.Code)
		}
	})
}

func TestDebugTailnet_onlyAdmins(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{}))
	for from, want := range map[netip.Addr]int{
		laptopIP:   http.StatusOK,
		phoneIP:    http.StatusForbidden,
		tabletIP:   http.StatusForbidden,
		serverIP:   http.StatusForbidden,
		strangerIP: http.StatusForbidden,
		roostIP:    http.StatusForbidden,
	} {
		rec := serve(h, newRequest(t, http.MethodGet, "/api/debug/tailnet", from))
		if rec.Code != want {
			t.Errorf("GET /api/debug/tailnet from %v = %d %q, want %d", from, rec.Code, rec.Body, want)
		}
		if want != http.StatusOK && strings.Contains(rec.Body.String(), "laptop") {
			t.Errorf("GET /api/debug/tailnet from %v leaked the device list: %q", from, rec.Body)
		}
	}
}

// A page that rebinds its own name to Roost's address would otherwise talk to
// Roost as the browser's device, with the page's own origin.
func TestGuard_host(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{}))
	for host, want := range map[string]int{
		roostHost:                  http.StatusOK,
		roostHost + ":4700":        http.StatusOK,
		roostHost + ".":            http.StatusOK,
		"ROOST.Example.ts.net":     http.StatusOK,
		"roost":                    http.StatusOK, // MagicDNS's short name
		"100.64.0.1:4700":          http.StatusOK,
		"[fd7a:115c:a1e0::1]:4700": http.StatusOK,
		"attacker.example":         http.StatusMisdirectedRequest,
		"roost.attacker.example":   http.StatusMisdirectedRequest,
		"laptop.example.ts.net":    http.StatusMisdirectedRequest,
		"100.64.0.2":               http.StatusMisdirectedRequest,
		"localhost:4700":           http.StatusMisdirectedRequest,
		"":                         http.StatusMisdirectedRequest,
	} {
		req := newRequest(t, http.MethodGet, "/api/me", laptopIP)
		req.Host = host
		if rec := serve(h, req); rec.Code != want {
			t.Errorf("GET /api/me with Host %q = %d %q, want %d", host, rec.Code, rec.Body, want)
		}
	}
}

func TestGuard_csrf(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{}))
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		rec := serve(h, newRequest(t, method, "/api/nope", laptopIP))
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), api.CSRFHeader) {
			t.Errorf("%s without %s = %d %q, want 403 naming the header", method, api.CSRFHeader, rec.Code, rec.Body)
		}

		req := newRequest(t, method, "/api/nope", laptopIP)
		req.Header.Set(api.CSRFHeader, "1")
		if rec := serve(h, req); rec.Code != http.StatusNotFound {
			t.Errorf("%s with %s = %d %q, want it to reach the router", method, api.CSRFHeader, rec.Code, rec.Body)
		}
	}
	if rec := serve(h, newRequest(t, http.MethodGet, "/api/me", laptopIP)); rec.Code != http.StatusOK {
		t.Errorf("GET without %s = %d, want 200", api.CSRFHeader, rec.Code)
	}
}

func TestGuard_webSocketOrigin(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{}))
	for origin, ok := range map[string]bool{
		"http://" + roostHost:             true,
		"https://" + roostHost:            false, // this request arrived without TLS
		"http://" + roostHost + ":4700":   false,
		"http://attacker.example":         false,
		"http://laptop.example.ts.net":    false,
		"null":                            false,
		"":                                false,
		"http://" + roostHost + "/x":      false,
		"http://user@" + roostHost + "/x": false,
	} {
		// HTTP/1.1 upgrades, and HTTP/2's extended CONNECT, which needs no
		// CSRF header since browsers can't add one to a WebSocket.
		http1 := newRequest(t, http.MethodGet, "/api/nope", laptopIP)
		http1.Header.Set("Connection", "Upgrade")
		http1.Header.Set("Upgrade", "websocket")
		http2 := newRequest(t, http.MethodConnect, "/api/nope", laptopIP)
		http2.Header.Set(":protocol", "websocket")
		for _, req := range []*http.Request{http1, http2} {
			if origin != "" {
				req.Header.Set("Origin", origin)
			}
			rec := serve(h, req)
			if got := rec.Code != http.StatusForbidden; got != ok {
				t.Errorf("%s WebSocket from Origin %q = %d %q; allowed = %v, want %v",
					req.Method, origin, rec.Code, rec.Body, got, ok)
			}
		}
	}
}

func TestGuard_starting(t *testing.T) {
	t.Parallel()
	opts := withTailnet(t, testTailnet(), api.Options{})
	opts.Snapshot = func() tailnet.Snapshot { return tailnet.Snapshot{} }
	rec := serve(api.New(opts), newRequest(t, http.MethodGet, "/api/me", laptopIP))
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Errorf("GET /api/me before the first status = %d with Retry-After %q, want 503 and a retry",
			rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestNew_requiresTailnet(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New() without WhoIs, Snapshot and Store didn't panic")
		}
	}()
	api.New(api.Options{})
}

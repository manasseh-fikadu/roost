package api_test

import (
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/api"
)

// TestRoutes walks every route New registers. Each one must say who can use
// it, a route that changes things must be for admins only, unless it's for
// agents, and each must let exactly those callers through.
func TestRoutes(t *testing.T) {
	t.Parallel()
	const (
		anyone = iota + 1
		viewers
		admins
		agentToken
	)
	const token = "test-agent-token"
	levels := map[string]int{"anyone": anyone, "viewers": viewers, "admins": admins, "agent token": agentToken}
	callers := []struct {
		name  string
		from  netip.Addr
		level int
	}{
		{"an unknown device", strangerIP, anyone},
		{"a tagged device", serverIP, anyone},
		{"Roost's own node", roostIP, anyone},
		{"carol, who has no role", tabletIP, anyone},
		{"bob, a viewer", phoneIP, viewers},
		{"alice, an admin", laptopIP, admins},
	}
	wildcard := regexp.MustCompile(`\{[^}]*\}`)

	for _, devUI := range []bool{false, true} {
		opts := withTailnet(t, testTailnet(), api.Options{AgentToken: token})
		if devUI {
			opts.DevUI = &url.URL{Scheme: "http", Host: "127.0.0.1:5173"}
		}
		h, routes := api.StubRoutes(opts)
		if !slices.ContainsFunc(routes, func(r api.Route) bool { return r.Pattern == "GET /api/me" }) {
			t.Fatalf("StubRoutes() = %v, want GET /api/me among them", routes)
		}

		for _, r := range routes {
			need, ok := levels[r.Level]
			if !ok {
				t.Errorf("%s has no level, so no one can use it; register it with a level", r.Pattern)
				continue
			}
			method, path, ok := strings.Cut(r.Pattern, " ")
			methods := []string{method}
			if !ok {
				// A pattern without a method matches all of them.
				path = r.Pattern
				methods = []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete}
			} else if method != http.MethodGet && need != admins && need != agentToken {
				t.Errorf("%s changes things, so it must be for admins, not %s", r.Pattern, r.Level)
			}
			target := wildcard.ReplaceAllString(path, "x")

			if need == agentToken {
				// Only the token counts, from anyone, whatever their role.
				for _, c := range callers {
					for _, auth := range []string{"", "Bearer wrong", "Bearer " + token} {
						req := newRequest(t, method, target, c.from)
						req.Header.Set(api.CSRFHeader, "1")
						if auth != "" {
							req.Header.Set("Authorization", auth)
						}
						want := http.StatusUnauthorized
						if auth == "Bearer "+token {
							want = http.StatusNoContent
						}
						if rec := serve(h, req); rec.Code != want {
							t.Errorf("%s from %s with %q = %d, want %d", r.Pattern, c.name, auth, rec.Code, want)
						}
					}
				}
				continue
			}

			for _, m := range methods {
				want := need
				if m != http.MethodGet && m != http.MethodHead {
					want = admins
				}
				for _, c := range callers {
					req := newRequest(t, m, target, c.from)
					req.Header.Set(api.CSRFHeader, "1")
					rec := serve(h, req)
					switch allowed := c.level >= want; {
					case allowed && rec.Code != http.StatusNoContent:
						t.Errorf("dev UI %v: %s %s from %s = %d %q, want it let through",
							devUI, m, target, c.name, rec.Code, rec.Body)
					case !allowed && rec.Code != http.StatusForbidden:
						t.Errorf("dev UI %v: %s %s from %s = %d, want 403", devUI, m, target, c.name, rec.Code)
					}
				}
			}
		}
	}
}

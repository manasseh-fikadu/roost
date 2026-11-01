package api_test

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/api"
)

// TestDemoRefusals checks that a demo refuses the routes demoRefusals lists,
// with a notice that says why, and does what every other route does, and that
// every pattern on the list is a route.
func TestDemoRefusals(t *testing.T) {
	t.Parallel()
	opts := withStore(t, testTailnet(), testStore(t), api.Options{Demo: true})
	h, routes := api.StubRoutes(opts)
	refused := api.DemoRefusals()
	wildcard := regexp.MustCompile(`\{[^}]*\}`)

	for _, pattern := range refused {
		if !slices.ContainsFunc(routes, func(r api.Route) bool { return r.Pattern == pattern }) {
			t.Errorf("a demo refuses %q, which is no route", pattern)
		}
	}
	for _, r := range routes {
		method, path, ok := strings.Cut(r.Pattern, " ")
		if !ok || r.Level == "agent secret" || r.Level == "enrollment code" {
			continue
		}
		req := newRequest(t, method, wildcard.ReplaceAllString(path, "x"), laptopIP)
		req.Header.Set(api.CSRFHeader, "1")
		rec := serve(h, req)
		if slices.Contains(refused, r.Pattern) {
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "turned off in this demo") {
				t.Errorf("%s in a demo = %d %q, want 403 and a notice", r.Pattern, rec.Code, rec.Body)
			}
			continue
		}
		if rec.Code != http.StatusNoContent {
			t.Errorf("%s in a demo = %d %q, want it let through", r.Pattern, rec.Code, rec.Body)
		}
	}
}

// TestDemoHost checks that a demo answers to any host name, since there's no
// tailnet name to hold requests to, and a real Bield does not.
func TestDemoHost(t *testing.T) {
	t.Parallel()
	for _, demo := range []bool{false, true} {
		h := api.New(withTailnet(t, testTailnet(), api.Options{Demo: demo}))
		req := newRequest(t, http.MethodGet, "/api/health", laptopIP)
		req.Host = "demo.example.org"

		rec := serve(h, req)

		want := http.StatusMisdirectedRequest
		if demo {
			want = http.StatusOK
		}
		if rec.Code != want {
			t.Errorf("GET /api/health for another host with Demo = %v: %d, want %d", demo, rec.Code, want)
		}
	}
}

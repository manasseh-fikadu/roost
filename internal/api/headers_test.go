package api_test

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/manasseh-fikadu/bield/internal/api"
)

// A page like SvelteKit's: one inline script that starts the app, and some
// that load their code from files.
const pageWithScripts = `<!doctype html>
<link rel="stylesheet" href="/_app/a.css">
<script src="/_app/start.js"></script>
<div style="display: contents">
	<script>
		{ import("/_app/start.js").then((kit) => kit.start()); }
	</script>
</div>
`

// directive returns what the policy says about name, or "" if it doesn't.
func directive(policy, name string) string {
	for part := range strings.SplitSeq(policy, ";") {
		part = strings.TrimSpace(part)
		if part == name || strings.HasPrefix(part, name+" ") {
			return part
		}
	}
	return ""
}

func TestHeaders_api(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{}))

	// Every kind of answer has them: data, an error, a refusal and a missing page.
	for _, tt := range []struct {
		name, target string
		from         netip.Addr
	}{
		{"data", "/api/health", laptopIP},
		{"a refusal", "/api/settings", strangerIP},
		{"an unknown route", "/api/nope", laptopIP},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := serve(h, newRequest(t, http.MethodGet, tt.target, tt.from))
			for name, want := range map[string]string{
				"X-Content-Type-Options":       "nosniff",
				"Referrer-Policy":              "no-referrer",
				"X-Frame-Options":              "SAMEORIGIN",
				"Content-Security-Policy":      "frame-ancestors 'self'",
				"Cross-Origin-Resource-Policy": "same-origin",
				"Cross-Origin-Opener-Policy":   "same-origin",
			} {
				if got := rec.Header().Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
			if pp := rec.Header().Get("Permissions-Policy"); !strings.Contains(pp, "camera=()") {
				t.Errorf("Permissions-Policy = %q, want the camera turned off", pp)
			}
		})
	}

	t.Run("a wrong host", func(t *testing.T) {
		t.Parallel()
		req := newRequest(t, http.MethodGet, "/api/me", laptopIP)
		req.Host = "evil.example.com"
		rec := serve(h, req)
		if rec.Code != http.StatusMisdirectedRequest || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("got %d with nosniff %q, want the 421 to have the headers", rec.Code, rec.Header().Get("X-Content-Type-Options"))
		}
	})
}

func TestHeaders_ui(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{UI: fstest.MapFS{
		"200.html":              {Data: []byte(pageWithScripts)},
		"robots.txt":            {Data: []byte("User-agent: *")},
		"_app/immutable/a.js":   {Data: []byte("a()")},
		"_app/immutable/b.html": {Data: []byte("<p>b</p>")},
	}}))

	inline := "\n\t\t{ import(\"/_app/start.js\").then((kit) => kit.start()); }\n\t"
	sum := sha256.Sum256([]byte(inline))
	hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"

	t.Run("the app", func(t *testing.T) {
		t.Parallel()
		// Any route of the app gets the fallback page.
		for _, target := range []string{"/", "/terminal", "/devices/box.tail1234.ts.net", "/_app/immutable/b.html"} {
			rec := serve(h, newRequest(t, http.MethodGet, target, laptopIP))
			csp := rec.Header().Get("Content-Security-Policy")

			if got, want := directive(csp, "script-src"), "script-src 'self' "+hash; got != want {
				t.Errorf("GET %s: script-src = %q, want %q", target, got, want)
			}
			for _, bad := range []string{"unsafe-inline", "unsafe-eval", "http:", "https:", "*"} {
				if strings.Contains(directive(csp, "script-src"), bad) {
					t.Errorf("GET %s: script-src allows %q: %s", target, bad, csp)
				}
			}
			if got := directive(csp, "frame-ancestors"); got != "frame-ancestors 'none'" {
				t.Errorf("GET %s: %q, want nothing to frame the app", target, got)
			}
			if got := directive(csp, "default-src"); got != "default-src 'none'" {
				t.Errorf("GET %s: %q, want the default to be nothing", target, got)
			}
			if got := directive(csp, "object-src"); got != "object-src 'none'" {
				t.Errorf("GET %s: %q, want no plugins", target, got)
			}
			if got := directive(csp, "img-src"); strings.Contains(got, "http") {
				t.Errorf("GET %s: %q, want no remote images", target, got)
			}
			if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
				t.Errorf("GET %s: X-Frame-Options = %q, want DENY", target, got)
			}
			if rec.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Errorf("GET %s: no Referrer-Policy", target)
			}
		}
	})

	t.Run("the terminal's socket", func(t *testing.T) {
		t.Parallel()
		rec := serve(h, newRequest(t, http.MethodGet, "/terminal", laptopIP))
		want := "connect-src 'self' ws://" + bieldHost + " wss://" + bieldHost
		if got := directive(rec.Header().Get("Content-Security-Policy"), "connect-src"); got != want {
			t.Errorf("connect-src = %q, want %q", got, want)
		}
	})

	t.Run("a host with more than a name in it", func(t *testing.T) {
		t.Parallel()
		// The demo answers to any host, and a header must not be built from one
		// that has a semicolon in it.
		ui := api.New(withTailnet(t, testTailnet(), api.Options{Demo: true, UI: fstest.MapFS{"200.html": {Data: []byte(pageWithScripts)}}}))
		req := newRequest(t, http.MethodGet, "/", laptopIP)
		req.Host = "a;script-src *"
		csp := serve(ui, req).Header().Get("Content-Security-Policy")
		if got := directive(csp, "connect-src"); got != "connect-src 'self'" || strings.Contains(csp, "script-src *") {
			t.Errorf("CSP = %q, want the host left out", csp)
		}
	})

	t.Run("files of the build", func(t *testing.T) {
		t.Parallel()
		// Scripts and styles need no policy of their own, but still have the
		// headers every answer has.
		rec := serve(h, newRequest(t, http.MethodGet, "/_app/immutable/a.js", laptopIP))
		if got := rec.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'self'" {
			t.Errorf("CSP of a script = %q, want only the default", got)
		}
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("a script has no nosniff")
		}
	})

	t.Run("a build without inline scripts", func(t *testing.T) {
		t.Parallel()
		ui := api.New(withTailnet(t, testTailnet(), api.Options{UI: fstest.MapFS{"200.html": {Data: []byte("<p>no script</p>")}}}))
		csp := serve(ui, newRequest(t, http.MethodGet, "/", laptopIP)).Header().Get("Content-Security-Policy")
		if got := directive(csp, "script-src"); got != "script-src 'self'" {
			t.Errorf("script-src = %q, want only 'self'", got)
		}
	})
}

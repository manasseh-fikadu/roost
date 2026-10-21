package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/manasseh-fikadu/roost/internal/api"
)

func TestUI(t *testing.T) {
	t.Parallel()
	const app = "<!doctype html><title>app</title>"
	h := api.New(withTailnet(t, testTailnet(), api.Options{UI: fstest.MapFS{
		"200.html":                             {Data: []byte(app)},
		"robots.txt":                           {Data: []byte("User-agent: *")},
		"_app/version.json":                    {Data: []byte(`{"version":"1"}`)},
		"_app/immutable/entry/start.abc123.js": {Data: []byte("start()")},
	}}))

	const (
		noCache  = "no-cache"
		forever  = "public, max-age=31536000, immutable"
		notFound = "404 page not found\n"
	)
	tests := []struct {
		name      string
		method    string
		target    string
		wantCode  int
		wantBody  string
		wantCache string
	}{
		{"root serves the app", http.MethodGet, "/", http.StatusOK, app, noCache},
		{"client route serves the app", http.MethodGet, "/devices/box.tail1234.ts.net", http.StatusOK, app, noCache},
		{"static file", http.MethodGet, "/robots.txt", http.StatusOK, "User-agent: *", noCache},
		{"version file isn't cached", http.MethodGet, "/_app/version.json", http.StatusOK, `{"version":"1"}`, noCache},
		{"hashed build file is cached", http.MethodGet, "/_app/immutable/entry/start.abc123.js", http.StatusOK, "start()", forever},
		{"missing build file is a 404", http.MethodGet, "/_app/immutable/entry/gone.js", http.StatusNotFound, notFound, ""},
		{"build directory is a 404", http.MethodGet, "/_app/immutable/", http.StatusNotFound, notFound, ""},
		{"HEAD gets headers only", http.MethodHead, "/devices", http.StatusOK, "", noCache},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// The app's files are no secret, so anyone on the tailnet gets them.
			rec := serve(h, newRequest(t, tt.method, tt.target, strangerIP))

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
		})
	}

	t.Run("the app is HTML", func(t *testing.T) {
		t.Parallel()
		rec := serve(h, newRequest(t, http.MethodGet, "/devices", laptopIP))
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}
	})

	t.Run("other methods aren't allowed", func(t *testing.T) {
		t.Parallel()
		req := newRequest(t, http.MethodPost, "/devices", laptopIP)
		req.Header.Set(api.CSRFHeader, "1")
		rec := serve(h, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
		if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
			t.Errorf("Allow = %q, want %q", allow, "GET, HEAD")
		}
	})
}

func TestUI_notBuilt(t *testing.T) {
	t.Parallel()
	h := api.New(withTailnet(t, testTailnet(), api.Options{}))

	rec := serve(h, newRequest(t, http.MethodGet, "/", laptopIP))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if body := rec.Body.String(); !strings.Contains(body, "make build") {
		t.Errorf("body = %q, want it to explain how to build the UI", body)
	}
}

func TestUI_devProxy(t *testing.T) {
	t.Parallel()
	// The proxy dials Vite itself, so this needs a real loopback server.
	vite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//nolint:gosec // G705: the test reads what it sent back.
		_, _ = io.WriteString(w, r.Host+" "+r.URL.RequestURI())
	}))
	t.Cleanup(vite.Close)
	viteURL, err := url.Parse(vite.URL)
	if err != nil {
		t.Fatal(err)
	}
	h := api.New(withTailnet(t, testTailnet(), api.Options{DevUI: viteURL}))

	t.Run("UI routes go to Vite", func(t *testing.T) {
		t.Parallel()
		rec := serve(h, newRequest(t, http.MethodGet, "/src/routes/+page.svelte?t=1", laptopIP))

		// Vite checks the Host header, so it must be Vite's own.
		if want := viteURL.Host + " /src/routes/+page.svelte?t=1"; rec.Body.String() != want {
			t.Errorf("Vite got %q, want %q", rec.Body, want)
		}
	})

	// Vite serves files from the workspace, not just the app.
	t.Run("only for admins", func(t *testing.T) {
		t.Parallel()
		for _, from := range []netip.Addr{phoneIP, serverIP, strangerIP} {
			rec := serve(h, newRequest(t, http.MethodGet, "/src/lib/index.ts", from))
			if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), viteURL.Host) {
				t.Errorf("GET from %v = %d %q, want 403 without reaching Vite", from, rec.Code, rec.Body)
			}
		}
	})

	t.Run("API routes don't", func(t *testing.T) {
		t.Parallel()
		rec := serve(h, newRequest(t, http.MethodGet, "/api/health", laptopIP))
		if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
			t.Errorf("GET /api/health = %q, want the hub's health", rec.Body)
		}
	})
}

func TestUI_devProxyDown(t *testing.T) {
	t.Parallel()
	vite := httptest.NewServer(http.NotFoundHandler())
	viteURL, err := url.Parse(vite.URL)
	if err != nil {
		t.Fatal(err)
	}
	vite.Close()
	h := api.New(withTailnet(t, testTailnet(), api.Options{DevUI: viteURL}))

	rec := serve(h, newRequest(t, http.MethodGet, "/", laptopIP))

	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "make dev") {
		t.Errorf("status %d, body %q; want 502 and a hint to run make dev", rec.Code, rec.Body)
	}
}

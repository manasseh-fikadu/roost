package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/manasseh-fikadu/roost/internal/api"
)

func TestUI(t *testing.T) {
	t.Parallel()
	const app = "<!doctype html><title>app</title>"
	h := api.New(api.Options{UI: fstest.MapFS{
		"200.html":                             {Data: []byte(app)},
		"robots.txt":                           {Data: []byte("User-agent: *")},
		"_app/version.json":                    {Data: []byte(`{"version":"1"}`)},
		"_app/immutable/entry/start.abc123.js": {Data: []byte("start()")},
	}})

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
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, nil))

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
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/devices", nil))
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}
	})

	t.Run("other methods aren't allowed", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/devices", nil))
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
	h := api.New(api.Options{})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if body := rec.Body.String(); !strings.Contains(body, "make build") {
		t.Errorf("body = %q, want it to explain how to build the UI", body)
	}
}

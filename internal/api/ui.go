package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
)

// fallbackPage is the SvelteKit SPA entry point, set as adapter-static's fallback
// in web/vite.config.ts.
const fallbackPage = "200.html"

const noUIMessage = "This Roost binary was built without its web UI. Build one with `make build`, " +
	"or run `make dev` and open the Vite dev server.\n"

// uiHandler serves the built web app from fsys. Paths that name a file get that
// file; any other path gets the SPA fallback page, so client-side routes survive
// a reload.
func uiHandler(fsys fs.FS) http.Handler {
	if fsys == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, noUIMessage, http.StatusNotFound)
		})
	}
	csp := uiPolicy(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if isFile(fsys, name) {
			if strings.HasPrefix(name, "_app/immutable/") {
				// File names under _app/immutable/ contain a content hash.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			if strings.HasSuffix(name, ".html") {
				csp.apply(w, r)
			}
			//nolint:gosec // G703: an fs.FS only opens valid paths inside its root, so name can't escape it.
			http.ServeFileFS(w, r, fsys, name)
			return
		}

		// A missing build file is a 404, not the app. After an upgrade, a tab that's
		// still open may ask for a chunk that no longer exists, and HTML in place of
		// JavaScript fails in confusing ways.
		if strings.HasPrefix(name, "_app/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		csp.apply(w, r)
		http.ServeFileFS(w, r, fsys, fallbackPage)
	})
}

func isFile(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && info.Mode().IsRegular()
}

// devUIProxy proxies the web UI to the Vite dev server at target, for `make
// dev`. The browser stays on Roost's tailnet address, so requests reach the API
// the same way they do in production, and Vite's hot reloading works through
// the proxy.
func devUIProxy(target *url.URL, log *slog.Logger) http.Handler {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// SetURL also sets the Host header to Vite's, which Vite checks.
			pr.SetURL(target)
		},
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelDebug),
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Warn("proxy to the Vite dev server", "url", target.String(), "error", err)
			http.Error(w, "Can't reach the Vite dev server at "+target.String()+". Is `make dev` running it?",
				http.StatusBadGateway)
		},
	}
}

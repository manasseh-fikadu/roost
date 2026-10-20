package api

import (
	"io/fs"
	"net/http"
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
		http.ServeFileFS(w, r, fsys, fallbackPage)
	})
}

func isFile(fsys fs.FS, name string) bool {
	info, err := fs.Stat(fsys, name)
	return err == nil && info.Mode().IsRegular()
}

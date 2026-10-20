// Package api serves Roost's HTTP API and its web UI.
package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
)

// Options configures the handler returned by New.
type Options struct {
	// Version is reported by GET /api/health.
	Version string
	// UI holds the built web app. If it's nil, UI routes explain how to build it.
	UI fs.FS
	// Logger receives errors that can't be reported to the client. Nil discards them.
	Logger *slog.Logger
}

// New returns the hub's HTTP handler.
func New(opts Options) http.Handler {
	h := &handler{opts: opts, log: opts.Logger}
	if h.log == nil {
		h.log = slog.New(slog.DiscardHandler)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", h.health)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		h.writeJSON(w, http.StatusNotFound, ErrorBody{Error: "not found"})
	})
	mux.Handle("/", uiHandler(opts.UI))
	return mux
}

type handler struct {
	opts Options
	log  *slog.Logger
}

func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, Health{Status: "ok", Version: h.opts.Version})
}

func (h *handler) writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		h.log.Error("encode JSON response", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	// A failed write means the client went away, and there's no one left to tell.
	_, _ = w.Write(append(body, '\n'))
}

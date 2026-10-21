// Package api serves Roost's HTTP API and its web UI.
package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// Options configures the handler returned by New.
type Options struct {
	// Version is reported by GET /api/health.
	Version string
	// UI holds the built web app. If it's nil, UI routes explain how to build it.
	UI fs.FS
	// DevUI, if set, is a Vite dev server that UI routes are proxied to
	// instead of serving UI.
	DevUI *url.URL
	// WhoIs asks Tailscale who is at a connection's remote address. Required.
	WhoIs func(ctx context.Context, remoteAddr string) (*tailnet.Identity, error)
	// Snapshot returns what Roost last learned about the tailnet, including
	// its own node. It's reported by GET /api/debug/tailnet. Required.
	Snapshot func() tailnet.Snapshot
	// IsAdmin reports whether a Tailscale login is one of Roost's admins.
	// Required.
	IsAdmin func(ctx context.Context, login string) (bool, error)
	// Logger receives errors that can't be reported to the client. Nil discards them.
	Logger *slog.Logger
}

// New returns the hub's HTTP handler, which serves only the tailnet: every
// request is checked and identified with Tailscale (see guard). It panics if
// opts lacks a required field.
func New(opts Options) http.Handler {
	if opts.WhoIs == nil || opts.Snapshot == nil || opts.IsAdmin == nil {
		panic("api.New: Options.WhoIs, Snapshot and IsAdmin are required")
	}
	h := &handler{opts: opts, log: opts.Logger}
	if h.log == nil {
		h.log = slog.New(slog.DiscardHandler)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", h.health)
	mux.HandleFunc("GET /api/me", h.me)
	mux.Handle("GET /api/debug/tailnet", h.adminOnly(http.HandlerFunc(h.debugTailnet)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		h.writeJSON(w, http.StatusNotFound, ErrorBody{Error: "not found"})
	})
	if opts.DevUI != nil {
		// Vite serves files from the workspace, so only admins get them.
		mux.Handle("/", h.adminOnly(devUIProxy(opts.DevUI, h.log)))
	} else {
		mux.Handle("/", uiHandler(opts.UI))
	}
	return h.guard(mux)
}

type handler struct {
	opts Options
	log  *slog.Logger
}

func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, Health{Status: "ok", Version: h.opts.Version})
}

func (h *handler) me(w http.ResponseWriter, r *http.Request) {
	c := callerOf(r)
	if c.who == nil {
		h.writeJSON(w, http.StatusOK, Me{Reason: c.reason})
		return
	}
	h.writeJSON(w, http.StatusOK, Me{
		SignedIn: true,
		Login:    c.who.LoginName,
		Name:     c.who.DisplayName,
		Device:   c.who.NodeName,
		Admin:    c.admin,
	})
}

func (h *handler) debugTailnet(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, newDebugTailnet(h.opts.Snapshot(), time.Now()))
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

// Package api serves Roost's HTTP API and its web UI.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// maxBody is the largest request body the API reads.
const maxBody = 1 << 20

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
	// Store holds who has a role in Roost. Required.
	Store *store.Store
	// Stopping is closed when the server starts to shut down, which ends
	// responses that would otherwise never finish, such as GET /api/stream.
	// If it's nil, they end only when the client goes away.
	Stopping <-chan struct{}
	// Logger receives errors that can't be reported to the client. Nil discards them.
	Logger *slog.Logger
}

// New returns the hub's HTTP handler, which serves only the tailnet: every
// request is checked and identified with Tailscale (see guard), and every
// route says who can use it (see routes). It panics if opts lacks a required
// field.
func New(opts Options) http.Handler {
	h := newHandler(opts)
	rt := newRouter(h)
	h.routes(rt)
	return h.guard(rt.mux)
}

func newHandler(opts Options) *handler {
	if opts.WhoIs == nil || opts.Snapshot == nil || opts.Store == nil {
		panic("api.New: Options.WhoIs, Snapshot and Store are required")
	}
	h := &handler{opts: opts, log: opts.Logger}
	if h.log == nil {
		h.log = slog.New(slog.DiscardHandler)
	}
	return h
}

// routes registers every route with the level a caller needs to use it:
// anyone, viewers or admins. A route that reads says which. A route that
// changes something is always for admins.
func (h *handler) routes(rt *router) {
	rt.read("GET /api/health", anyone, h.health)
	rt.read("GET /api/me", anyone, h.me)
	rt.read("GET /api/stream", viewers, h.stream)

	rt.read("GET /api/access", admins, h.access)
	rt.write("PATCH /api/access", h.changeAccess)
	rt.write("PUT /api/access/users/{login}", h.setRole)
	rt.write("DELETE /api/access/users/{login}", h.removeUser)

	rt.read("GET /api/debug/tailnet", admins, h.debugTailnet)

	rt.anyMethod("/api/", anyone, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: "not found"})
	}))
	if h.opts.DevUI != nil {
		// Vite serves files from the workspace, so only admins get them.
		rt.anyMethod("/", admins, devUIProxy(h.opts.DevUI, h.log))
	} else {
		// The app itself holds nothing secret. It asks /api/me what to show.
		rt.anyMethod("/", anyone, uiHandler(h.opts.UI))
	}
}

type handler struct {
	opts Options
	log  *slog.Logger
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, r, http.StatusOK, Health{Status: "ok", Version: h.opts.Version})
}

func (h *handler) me(w http.ResponseWriter, r *http.Request) {
	c := callerOf(r)
	if c.who == nil {
		h.writeJSON(w, r, http.StatusOK, Me{Reason: c.reason})
		return
	}
	h.writeJSON(w, r, http.StatusOK, Me{
		SignedIn: true,
		Login:    c.who.LoginName,
		Name:     c.who.DisplayName,
		Device:   c.who.NodeName,
		Role:     c.role(),
	})
}

func (h *handler) debugTailnet(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, r, http.StatusOK, newDebugTailnet(h.opts.Snapshot(), time.Now()))
}

// writeJSON answers r with v as JSON. Unless the caller is an admin, fields
// tagged roost:"secret" are left empty (see redact).
func (h *handler) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	if callerOf(r).level < admins {
		v = redact(v)
	}
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

// readJSON decodes r's JSON body into v. If it can't, it answers with an
// error and returns false.
func (h *handler) readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		if _, extra := dec.Token(); !errors.Is(extra, io.EOF) {
			err = errors.New("there's more after the JSON value")
		}
	}
	if err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: "can't read the request: " + err.Error()})
		return false
	}
	return true
}

// fail answers with a 500 for an error the caller can't do anything about,
// and logs it.
func (h *handler) fail(w http.ResponseWriter, r *http.Request, msg string, err error) {
	h.log.Error(msg, "method", r.Method, "path", r.URL.Path, "error", err)
	h.writeJSON(w, r, http.StatusInternalServerError, ErrorBody{Error: "internal error"})
}

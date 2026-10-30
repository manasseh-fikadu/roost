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
	"sync"
	"time"

	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/alerts"
	"github.com/manasseh-fikadu/roost/internal/checks"
	"github.com/manasseh-fikadu/roost/internal/files"
	"github.com/manasseh-fikadu/roost/internal/ssh"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/usage"
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
	// Feed builds the snapshots GET /api/stream sends. Required.
	Feed *Feed
	// Store holds who has a role in Roost. Required.
	Store *store.Store
	// Agents keeps what agents report in their heartbeats. Required.
	Agents *agents.Registry
	// Usage keeps what uses each device's CPU and memory. Required.
	Usage *usage.Recorder
	// Checks runs the checks. Required.
	Checks *checks.Runner
	// Alerts keeps the open alerts. Required.
	Alerts *alerts.Engine
	// SSH logs in to the tailnet's machines, for terminals. Required.
	SSH *ssh.Manager
	// AppURL is where the app is on the tailnet, such as
	// https://roost.example.ts.net, which the agent sends its heartbeats to.
	// If it's empty, it's worked out from each request.
	AppURL string
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
	if opts.WhoIs == nil || opts.Snapshot == nil || opts.Feed == nil || opts.Store == nil || opts.Agents == nil ||
		opts.Usage == nil || opts.Checks == nil || opts.Alerts == nil || opts.SSH == nil {
		panic("api.New: Options.WhoIs, Snapshot, Feed, Store, Agents, Usage, Checks, Alerts and SSH are required")
	}
	h := &handler{opts: opts, log: opts.Logger}
	if h.log == nil {
		h.log = slog.New(slog.DiscardHandler)
	}
	if opts.Stopping != nil {
		// Uploads wait for a quiet moment to write their line.
		go func() {
			<-opts.Stopping
			h.audit.flushAll(h)
		}()
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

	rt.read("GET /api/devices/{id}/history", viewers, h.deviceHistory)
	rt.read("GET /api/devices/{id}/usage", viewers, h.deviceUsage)
	rt.read("GET /api/devices/{id}/usage/history", viewers, h.usageHistory)
	rt.write("PUT /api/devices/{id}/settings", h.setDeviceSettings)
	rt.write("DELETE /api/devices/{id}/agent", h.revokeAgent)

	rt.read("GET /api/devices/{id}/ssh", admins, h.sshSetup)
	rt.write("PUT /api/devices/{id}/ssh", h.setSSH)
	rt.write("DELETE /api/devices/{id}/ssh", h.removeSSH)
	rt.write("POST /api/devices/{id}/ssh/check", h.checkSSH)
	rt.write("POST /api/devices/{id}/ssh/authorize", h.authorizeSSH)
	rt.write("PUT /api/devices/{id}/ssh/host-key", h.trustHostKey)
	rt.write("DELETE /api/devices/{id}/ssh/host-key", h.resetHostKey)
	// A WebSocket, which guard only lets in from Roost's own pages.
	rt.read("GET /api/devices/{id}/terminal", admins, h.terminal)
	rt.read("GET /api/devices/{id}/terminal/sessions", admins, h.terminalSessions)
	rt.write("DELETE /api/devices/{id}/terminal/sessions/{name}", h.killSession)

	// A file's content is served from Roost's own origin, so the raw route
	// sandboxes it. All of these are for admins, who can open shells already.
	rt.read("GET /api/devices/{id}/files/list", admins, h.listFiles)
	rt.read("GET /api/devices/{id}/files/stat", admins, h.statFile)
	rt.read("GET /api/devices/{id}/files/raw", admins, h.rawFile)
	rt.read("GET /api/devices/{id}/files/archive", admins, h.archive)
	rt.read("GET /api/devices/{id}/files/search", admins, h.searchFiles)
	rt.write("POST /api/devices/{id}/files/archive", h.parkArchive)
	rt.write("POST /api/devices/{id}/files/exists", h.namesExist)
	rt.write("PUT /api/devices/{id}/files/upload", h.uploadFile)
	rt.write("PUT /api/devices/{id}/files/write", h.writeFile)
	rt.write("POST /api/devices/{id}/files/mkdir", h.makeFolder)
	rt.write("POST /api/devices/{id}/files/rename", h.renameFile)
	rt.write("POST /api/devices/{id}/files/move", h.transferFiles("Moved", (*files.Remote).Move))
	rt.write("POST /api/devices/{id}/files/copy", h.transferFiles("Copied", (*files.Remote).Copy))
	rt.write("POST /api/devices/{id}/files/delete", h.deleteFiles)

	rt.read("GET /api/checks/recent", viewers, h.recentResults)
	rt.read("GET /api/checks/{id}/history", viewers, h.checkHistory)
	rt.write("POST /api/checks", h.addCheck)
	rt.write("PUT /api/checks/{id}", h.changeCheck)
	rt.write("DELETE /api/checks/{id}", h.deleteCheck)
	rt.write("POST /api/checks/{id}/run", h.runCheck)
	rt.read("GET /api/health/history", viewers, h.healthHistory)

	rt.read("GET /api/alerts", viewers, h.listAlerts)
	rt.write("PUT /api/alerts/{id}/ack", h.ackAlert)
	rt.write("DELETE /api/alerts/{id}/ack", h.ackAlert)
	rt.write("PUT /api/alerts/{id}/snooze", h.snoozeAlert)
	rt.write("DELETE /api/alerts/{id}/snooze", h.snoozeAlert)
	rt.write("POST /api/alerts/{id}/notes", h.addAlertNote)
	rt.read("GET /api/events", viewers, h.listEvents)

	rt.read("GET /api/agent", admins, h.agentSetup)
	rt.write("POST /api/agent/codes", h.newEnrollmentCode)
	// installScript checks the code, and answers a bad one with a script
	// that says why.
	rt.read("GET /api/agent/install/{code}", anyone, h.installScript)
	rt.read("GET /api/agent/uninstall.sh", anyone, h.uninstallScript)
	rt.enroll("POST /api/agent/enroll", h.enroll)
	rt.agent("POST /api/agent/heartbeat", h.heartbeat)
	rt.agent("DELETE /api/agent/enrollment", h.unenroll)

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
	// agentsMu is held to write while an agent is revoked or unenrolled, and
	// to read while a heartbeat is recorded, so a heartbeat that was on its
	// way can't bring the agent's report back.
	agentsMu sync.RWMutex
	// archives are the selections parked for a download, and audit what the
	// activity log is waiting to write about files.
	archives files.Archives
	audit    fileAudit
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
		Reason:   c.reason,
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
	return h.readJSONUpTo(w, r, v, maxBody)
}

// readJSONUpTo is readJSON for a body of up to limit bytes.
func (h *handler) readJSONUpTo(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
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

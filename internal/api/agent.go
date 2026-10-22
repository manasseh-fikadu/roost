package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/manasseh-fikadu/roost/agent"
	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/usage"
)

// maxHeartbeat is the largest heartbeat Roost reads. The agent's are a few
// kilobytes.
const maxHeartbeat = 256 << 10

// heartbeat answers POST /api/agent/heartbeat, which an agent sends every 15
// seconds with its machine's metrics. The router has checked its token. The
// heartbeat counts for the device it came from, as Tailscale says, whatever
// the heartbeat itself claims.
func (h *handler) heartbeat(w http.ResponseWriter, r *http.Request) {
	c := callerOf(r)
	if c.node == "" {
		status, msg := http.StatusForbidden, "Tailscale doesn't know the machine this heartbeat came from"
		if c.reason == ReasonWhoIsFailed {
			status, msg = http.StatusServiceUnavailable, "Roost couldn't ask Tailscale which machine this is"
		}
		h.writeJSON(w, r, status, ErrorBody{Error: msg})
		return
	}
	d, historyID, ok := h.deviceByID(c.node)
	if !ok {
		h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: "this machine isn't on the tailnet as Roost last saw it; it tries again with the next heartbeat"})
		return
	}
	if historyID == store.HubID && h.opts.Feed.measuresHub() {
		h.writeJSON(w, r, http.StatusConflict, ErrorBody{Error: "Roost runs on this machine and measures it itself, so it doesn't need the agent"})
		return
	}

	var hb agents.Heartbeat
	// Unlike the requests from the web app, a heartbeat can have fields this
	// version of Roost doesn't know, from a newer agent.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxHeartbeat)).Decode(&hb); err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: "can't read the heartbeat: " + err.Error()})
		return
	}
	report, err := h.opts.Agents.Record(r.Context(), historyID, hb, time.Now())
	if err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	if s, ok := usage.ParseAgent(hb.Usage, report.ReceivedAt, report.Info.Cores); ok {
		h.opts.Usage.Record(r.Context(), historyID, s)
	}
	h.log.Debug("took a heartbeat", "device", d.ID)
	w.WriteHeader(http.StatusNoContent)
}

// installScript answers GET /api/agent/install.sh with the script that
// installs the agent.
func (h *handler) installScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	// A failed write means the client went away, and there's no one left to
	// tell.
	//nolint:gosec // G705: it's a shell script that browsers don't render, and the guard checked the host in its URL.
	_, _ = w.Write([]byte(agent.Install(h.appURL(r))))
}

// agentSetup answers GET /api/agent with the commands that install and
// remove the agent.
func (h *handler) agentSetup(w http.ResponseWriter, r *http.Request) {
	script := shellWord(h.appURL(r) + "/api/agent/install.sh")
	h.writeJSON(w, r, http.StatusOK, AgentSetup{
		Version:          agent.Version,
		InstallCommand:   "curl -fsSL " + script + " | ROOST_AGENT_TOKEN=" + shellWord(h.opts.AgentToken) + " bash",
		UninstallCommand: "curl -fsSL " + script + " | bash -s -- --uninstall",
	})
}

// forgetAgent answers DELETE /api/devices/{id}/agent, which drops what Roost
// knows from a device's agent, such as after it's uninstalled. An agent that
// still runs shows up again with its next heartbeat.
func (h *handler) forgetAgent(w http.ResponseWriter, r *http.Request) {
	d, historyID, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	err := h.opts.Agents.Forget(r.Context(), historyID)
	switch {
	case errors.Is(err, store.ErrNoAgent):
		h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: "no agent has reported from this device"})
		return
	case err != nil:
		h.fail(w, r, "forget an agent", err)
		return
	}
	h.opts.Usage.Forget(historyID)
	h.log.Info("forgot a device's agent", "device", d.ID, "by", callerOf(r).who.LoginName)
	w.WriteHeader(http.StatusNoContent)
}

// appURL returns where the app is on the tailnet, without a trailing slash.
func (h *handler) appURL(r *http.Request) string {
	if h.opts.AppURL != "" {
		return strings.TrimSuffix(h.opts.AppURL, "/")
	}
	// The guard has checked that the host is one of Roost's own names.
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

var plainWord = regexp.MustCompile(`^[\w@%+=:,./-]+$`)

// shellWord quotes s for a POSIX shell, unless it's plain.
func shellWord(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

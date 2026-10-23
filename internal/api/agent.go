package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/manasseh-fikadu/roost/agent"
	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/usage"
)

// maxHeartbeat is the largest heartbeat Roost reads. The agent's are a few
// kilobytes.
const maxHeartbeat = 256 << 10

// heartbeat answers POST /api/agent/heartbeat, which an agent sends every 15
// seconds with its machine's metrics. The router has checked its secret, and
// that it came from the machine the agent enrolled on, so it counts for that
// machine, whatever the heartbeat itself claims.
func (h *handler) heartbeat(w http.ResponseWriter, r *http.Request) {
	d, historyID, ok := h.deviceByID(callerOf(r).node)
	if !ok {
		h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: notOnTailnet + "; it tries again with the next heartbeat"})
		return
	}
	if historyID == store.HubID && h.opts.Feed.measuresHub() {
		h.writeJSON(w, r, http.StatusConflict, ErrorBody{Error: measuresItself})
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

// Why a machine can't have an agent, or not yet.
const (
	notOnTailnet   = "this machine isn't on the tailnet as Roost last saw it"
	measuresItself = "Roost runs on this machine and measures it itself, so it doesn't need the agent"
)

// newEnrollmentCode answers POST /api/agent/codes with a command that
// installs the agent on one machine, with a new enrollment code in it.
func (h *handler) newEnrollmentCode(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	code, expires, err := h.opts.Store.NewEnrollmentCode(r.Context(), callerOf(r).who.LoginName, now)
	if err != nil {
		h.fail(w, r, "make an enrollment code", err)
		return
	}
	h.writeJSON(w, r, http.StatusCreated, AgentCode{
		InstallCommand: "curl -fsSL " + shellWord(h.appURL(r)+"/api/agent/install/"+code) + " | bash",
		CreatedAt:      now,
		ExpiresAt:      expires,
	})
}

// installScript answers GET /api/agent/install/{code} with the script that
// installs the agent, if the code is good for the machine that asks. If it
// isn't, it answers with a script that says why and installs nothing. That
// script comes with a 200, since the install command pipes curl -f into
// bash, so bash would see nothing at all of an error status.
func (h *handler) installScript(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	script := agent.Install(h.appURL(r), code)
	if err := h.opts.Store.CheckEnrollmentCode(r.Context(), code, callerOf(r).node, time.Now()); err != nil {
		_, msg := h.codeDenial(err)
		script = "#!/bin/sh\necho " + shellWord("Roost won't install the agent: "+msg+".") + " >&2\nexit 1\n"
	}
	writeScript(w, script)
}

// uninstallScript answers GET /api/agent/uninstall.sh with the script that
// removes the agent.
func (h *handler) uninstallScript(w http.ResponseWriter, _ *http.Request) {
	writeScript(w, agent.Uninstall())
}

func writeScript(w http.ResponseWriter, script string) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	// A failed write means the client went away, and there's no one left to
	// tell.
	//nolint:gosec // G705: it's a shell script that browsers don't render, and the guard checked the host in its URL.
	_, _ = w.Write([]byte(script))
}

// enroll answers POST /api/agent/enroll, which install.sh sends with its
// enrollment code, with a secret for the agent on the machine it came from.
// The router has checked the code.
func (h *handler) enroll(w http.ResponseWriter, r *http.Request) {
	node := callerOf(r).node
	d, historyID, ok := h.deviceByID(node)
	if !ok {
		// The code isn't used, so the same command works in a moment.
		h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: notOnTailnet + "; try again in a few seconds"})
		return
	}
	if historyID == store.HubID && h.opts.Feed.measuresHub() {
		h.writeJSON(w, r, http.StatusConflict, ErrorBody{Error: measuresItself})
		return
	}
	secret, e, err := h.opts.Store.Enroll(r.Context(), bearer(r), node, time.Now())
	if err != nil {
		status, msg := h.codeDenial(err)
		h.writeJSON(w, r, status, ErrorBody{Error: msg})
		return
	}
	h.opts.Feed.Poke()
	h.log.Info("enrolled an agent", "device", d.ID, "code_by", e.By)
	h.writeJSON(w, r, http.StatusOK, AgentSecret{Secret: secret})
}

// unenroll answers DELETE /api/agent/enrollment, which uninstall.sh sends
// with the agent's secret. Roost refuses the secret from then on, and forgets
// what the agent reported.
func (h *handler) unenroll(w http.ResponseWriter, r *http.Request) {
	node := callerOf(r).node
	if _, err := h.opts.Store.Unenroll(r.Context(), node); err != nil {
		h.fail(w, r, "unenroll an agent", err)
		return
	}
	historyID := node
	if _, id, ok := h.deviceByID(node); ok {
		historyID = id
	}
	if _, err := h.forgetReports(r.Context(), historyID); err != nil {
		h.fail(w, r, "forget an agent", err)
		return
	}
	h.opts.Feed.Poke()
	h.log.Info("an agent was uninstalled", "device", node)
	w.WriteHeader(http.StatusNoContent)
}

// agentSetup answers GET /api/agent with the agent's version and the command
// that removes it.
func (h *handler) agentSetup(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, r, http.StatusOK, AgentSetup{
		Version:          agent.Version,
		UninstallCommand: "curl -fsSL " + shellWord(h.appURL(r)+"/api/agent/uninstall.sh") + " | bash",
	})
}

// revokeAgent answers DELETE /api/devices/{id}/agent. Roost refuses the
// secret of the device's agent from then on, and forgets what it reported.
func (h *handler) revokeAgent(w http.ResponseWriter, r *http.Request) {
	d, historyID, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	revoked, err := h.opts.Store.Unenroll(r.Context(), h.nodesOf(historyID)...)
	if err != nil {
		h.fail(w, r, "revoke an agent", err)
		return
	}
	forgot, err := h.forgetReports(r.Context(), historyID)
	if err != nil {
		h.fail(w, r, "forget an agent", err)
		return
	}
	if !revoked && !forgot {
		h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: "this device has no agent"})
		return
	}
	h.opts.Feed.Poke()
	h.log.Info("revoked a device's agent", "device", d.ID, "by", callerOf(r).who.LoginName)
	w.WriteHeader(http.StatusNoContent)
}

// forgetReports drops what an agent reported for the device whose history is
// kept under historyID, and reports whether there was anything. Without an
// agent report, the usage under historyID is Roost's own, of the machine it
// runs on, and stays.
func (h *handler) forgetReports(ctx context.Context, historyID string) (bool, error) {
	switch err := h.opts.Agents.Forget(ctx, historyID); {
	case errors.Is(err, store.ErrNoAgent):
		return false, nil
	case err != nil:
		return false, err
	}
	h.opts.Usage.Forget(historyID)
	return true, nil
}

// nodesOf returns the IDs of the nodes on the tailnet whose history is kept
// under id: a device's own ID, or for store.HubID, each node of the machine
// Roost runs on.
func (h *handler) nodesOf(id string) []string {
	st := h.opts.Snapshot().Status
	if st == nil || id != store.HubID {
		return []string{id}
	}
	var ids []string
	for i, d := range append([]tailnet.Device{st.Self}, st.Peers...) {
		if historyID(d, i == 0) == store.HubID {
			ids = append(ids, d.ID)
		}
	}
	return ids
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

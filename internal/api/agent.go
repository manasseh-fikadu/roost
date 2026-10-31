package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/manasseh-fikadu/roost/agent"
	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/ssh"
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
	// An admin may have revoked the agent while its heartbeat arrived. If the
	// revoke landed between the router's check and Record, the agent would
	// have a report again and no enrollment, so the secret is checked again
	// while revokes wait.
	h.agentsMu.RLock()
	defer h.agentsMu.RUnlock()
	switch _, err := h.opts.Store.EnrolledNode(r.Context(), bearer(r)); {
	case errors.Is(err, store.ErrUnknownSecret):
		w.Header().Set("WWW-Authenticate", "Bearer")
		h.writeJSON(w, r, http.StatusUnauthorized, ErrorBody{Error: unknownSecret})
		return
	case err != nil:
		h.fail(w, r, "look up an agent's secret", err)
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
	h.logAction(r, EventKindAgent, "", "Made an install command for the agent")
	h.writeJSON(w, r, http.StatusCreated, AgentCode{
		InstallCommand: "curl -fsSL " + shellWord(h.appURL(r)+"/api/agent/install/"+code) + " | bash",
		CreatedAt:      now,
		ExpiresAt:      expires,
	})
}

// remoteTimeout is how long an install or uninstall over SSH can take.
const remoteTimeout = 2 * time.Minute

// maxRunOutput is how much of a script's output Roost sends back.
const maxRunOutput = 16 << 10

// runRemote is the sh script that Roost runs on a device over SSH to install
// or remove the agent. It does what the one-line commands do, which pipe curl
// into bash, but it fails if the download does, and it sets up what a
// session without a login lacks: a user manager to talk to and a $USER.
// $1 is where to download the script from, and $2 is Roost's address. It has
// no single quote or backslash, which a command can't carry (see ssh.Quote).
const runRemote = `{
if [ -z "${XDG_RUNTIME_DIR:-}" ] && [ -d "/run/user/$(id -u)" ]; then XDG_RUNTIME_DIR="/run/user/$(id -u)"; export XDG_RUNTIME_DIR; fi
USER="${USER:-$(id -un)}"; export USER
command -v curl >/dev/null 2>&1 || { echo "curl is not installed on this machine" >&2; exit 1; }
command -v bash >/dev/null 2>&1 || { echo "bash is not installed on this machine" >&2; exit 1; }
tmp=$(mktemp) || exit 1
if ! curl -fsSL -o "$tmp" "$1"; then rm -f "$tmp"; echo "This machine could not download the script from $2" >&2; exit 1; fi
bash "$tmp"; code=$?
rm -f "$tmp"
exit "$code"
} 2>&1`

// installAgent answers POST /api/devices/{id}/agent/install, which installs
// the agent on the device over SSH with a new enrollment code, as the user
// Roost logs in as. The device enrolls from its own node like any other, so
// it has to reach Roost on the tailnet.
func (h *handler) installAgent(w http.ResponseWriter, r *http.Request) {
	d, historyID, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	if historyID == store.HubID && h.opts.Feed.measuresHub() {
		h.writeJSON(w, r, http.StatusConflict, ErrorBody{Error: measuresItself})
		return
	}
	if h.sshOf(d.ID).State == SSHStateNotSetUp {
		// Before it makes a code that nothing would use.
		h.sshError(w, r, ssh.ErrNotSetUp)
		return
	}
	code, _, err := h.opts.Store.NewEnrollmentCode(r.Context(), callerOf(r).who.LoginName, time.Now())
	if err != nil {
		h.fail(w, r, "make an enrollment code", err)
		return
	}
	h.runOnDevice(w, r, d, "install", "/api/agent/install/"+code)
}

// uninstallAgent answers POST /api/devices/{id}/agent/uninstall, which
// removes the agent from the device over SSH, for the user Roost logs in as.
func (h *handler) uninstallAgent(w http.ResponseWriter, r *http.Request) {
	d, _, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	h.runOnDevice(w, r, d, "uninstall", "/api/agent/uninstall.sh")
}

// runOnDevice downloads the script at path from Roost on d, and runs it,
// over SSH. what is "install" or "uninstall".
func (h *handler) runOnDevice(w http.ResponseWriter, r *http.Request, d tailnet.Device, what, path string) {
	base := h.appURL(r)
	ctx, cancel := context.WithTimeout(r.Context(), remoteTimeout)
	defer cancel()
	res, err := h.opts.SSH.Exec(ctx, d.ID, runRemote, base+path, base)
	if errors.Is(err, context.DeadlineExceeded) && r.Context().Err() == nil {
		err = fmt.Errorf("the %s didn't finish in %v: %w", what, remoteTimeout, err)
	}
	if h.sshError(w, r, err) {
		return
	}
	name := h.deviceName(r, d.ID)
	out := AgentRun{OK: res.Code == 0, Output: tail(strings.TrimSpace(string(res.Stdout)), maxRunOutput)}
	switch {
	case out.OK:
		h.logAction(r, EventKindAgent, d.ID, "Ran the agent "+what+" on "+name+" over SSH")
		h.opts.Feed.Poke()
	default:
		h.logEvent(r, store.Event{
			Level: string(EventLevelError), Kind: string(EventKindAgent), DeviceID: d.ID, Actor: callerOf(r).who.LoginName,
			Message: "The agent " + what + " on " + name + " over SSH failed",
		})
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// tail returns the last limit bytes of s, from the start of a character.
func tail(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	s = s[len(s)-limit:]
	for !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return "…" + s
}

// installScript answers GET /api/agent/install/{code} with the script that
// installs the agent, if the code is good for the machine that asks. If it
// isn't, it answers with a script that says why and installs nothing. That
// script comes with a 200, since the install command pipes curl -f into
// bash, so bash would see nothing at all of an error status.
func (h *handler) installScript(w http.ResponseWriter, r *http.Request) {
	c, code := callerOf(r), r.PathValue("code")
	refusal := ""
	if c.reason == ReasonWhoIsFailed {
		// Without the machine, a code it used would look like another
		// machine's.
		refusal = whoIsFailed + ", so run the command again in a moment"
	} else if err := h.opts.Store.CheckEnrollmentCode(r.Context(), code, c.node, time.Now()); err != nil {
		_, refusal = h.codeDenial(err)
	}
	if refusal != "" {
		writeScript(w, "#!/bin/sh\necho "+shellWord("Roost won't install the agent: "+refusal+".")+" >&2\nexit 1\n")
		return
	}
	writeScript(w, agent.Install(h.appURL(r), code))
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
	// The admin who made the code let it enroll.
	h.logEvent(r, store.Event{
		Level: string(EventLevelSuccess), Kind: string(EventKindAgent), DeviceID: d.ID, Actor: e.By,
		Message: "The agent on " + h.deviceName(r, d.ID) + " enrolled",
	})
	h.writeJSON(w, r, http.StatusOK, AgentSecret{Secret: secret})
}

// unenroll answers DELETE /api/agent/enrollment, which uninstall.sh sends
// with the agent's secret. Roost refuses the secret from then on, and forgets
// what the agent reported.
func (h *handler) unenroll(w http.ResponseWriter, r *http.Request) {
	node := callerOf(r).node
	h.agentsMu.Lock()
	defer h.agentsMu.Unlock()
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
	h.logEvent(r, store.Event{Kind: string(EventKindAgent), DeviceID: node, Message: "The agent on " + h.deviceName(r, node) + " was uninstalled"})
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
	h.agentsMu.Lock()
	defer h.agentsMu.Unlock()
	// Unlike uninstalling, revoking deletes the code the agent enrolled with,
	// so running its install command again doesn't undo the revoke.
	revoked, err := h.opts.Store.Revoke(r.Context(), h.nodesOf(historyID)...)
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
	h.logAction(r, EventKindAgent, d.ID, "Revoked the agent on "+h.deviceName(r, d.ID))
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

// appURL returns where the app is, without a trailing slash: the public URL
// if an admin set one, else where Roost listens on the tailnet.
func (h *handler) appURL(r *http.Request) string {
	if u := h.opts.Settings.Get(r.Context()).General.PublicURL; u != "" {
		return u
	}
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

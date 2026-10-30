package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/manasseh-fikadu/roost/internal/ssh"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// newDeviceSSH returns how Roost logs in to d, from what the SSH manager
// says of each machine. self marks Roost's own node.
func newDeviceSSH(d tailnet.Device, self bool, access map[string]ssh.Access) *DeviceSSH {
	id := ssh.AccessID(d, self)
	out := &DeviceSSH{State: SSHStateNotSetUp, Hub: id == store.HubID}
	a, ok := access[id]
	if !ok {
		return out
	}
	out.State, out.User, out.Port, out.HostKey = SSHState(a.State), a.User, a.Port, a.HostKey
	out.Error, out.OfferedKey, out.CheckedAt = a.Error, a.OfferedKey, timePtr(a.CheckedAt)
	out.Tmux, out.Docker = a.Tmux, a.Docker
	return out
}

// sshOf returns how Roost logs in to the device with an ID, as it is now.
func (h *handler) sshOf(id string) *DeviceSSH {
	d, _, ok := h.deviceByID(id)
	if !ok {
		return &DeviceSSH{State: SSHStateNotSetUp}
	}
	self := h.opts.Snapshot().Status.Self.ID == d.ID
	return newDeviceSSH(d, self, h.opts.SSH.Access())
}

// sshError answers with the status and message for an error from the SSH
// manager, and reports whether there was one.
func (h *handler) sshError(w http.ResponseWriter, r *http.Request, err error) bool {
	var ce *ssh.ConnectError
	status := http.StatusBadRequest
	switch {
	case err == nil:
		return false
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		// The caller went away.
		return true
	case errors.Is(err, ssh.ErrNoDevice), errors.Is(err, ssh.ErrNoSession):
		status = http.StatusNotFound
	case errors.Is(err, ssh.ErrStarting):
		status = http.StatusServiceUnavailable
	case errors.Is(err, ssh.ErrNotSetUp), errors.Is(err, ssh.ErrOffline), errors.Is(err, ssh.ErrHostKeyChange),
		errors.Is(err, ssh.ErrNoTmux), errors.Is(err, ssh.ErrNoChangedKey), errors.Is(err, ssh.ErrOfferedKeyChanged),
		errors.Is(err, ssh.ErrNoSFTP):
		status = http.StatusConflict
	case errors.As(err, &ce), errors.Is(err, ssh.ErrTmux), errors.Is(err, context.DeadlineExceeded):
		status = http.StatusBadGateway
	case errors.Is(err, ssh.ErrUnsupported), errors.Is(err, ssh.ErrNotHub), errors.Is(err, ssh.ErrBadUser),
		errors.Is(err, ssh.ErrBadPort), errors.Is(err, ssh.ErrBadSession), errors.Is(err, ssh.ErrBadDir), errors.Is(err, ssh.ErrBadArg):
	default:
		h.fail(w, r, "use SSH", err)
		return true
	}
	h.writeJSON(w, r, status, ErrorBody{Error: err.Error()})
	return true
}

// sshSetup answers GET /api/devices/{id}/ssh with how to let Roost log in
// to the device.
func (h *handler) sshSetup(w http.ResponseWriter, r *http.Request) {
	s, err := h.opts.SSH.Setup(r.PathValue("id"))
	if h.sshError(w, r, err) {
		return
	}
	out := SSHSetup{Command: s.Command, AuthorizedKey: s.AuthorizedKey}
	if s.Hub {
		out.HubUser = s.HubUser
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// setSSH answers PUT /api/devices/{id}/ssh, which sets up how Roost logs in
// to the device.
func (h *handler) setSSH(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body SetSSH
	if !h.readJSON(w, r, &body) {
		return
	}
	if body.Port == 0 {
		body.Port = ssh.DefaultPort
	}
	before := h.sshOf(id)
	_, err := h.opts.SSH.SetUp(r.Context(), id, body.User, body.Port, callerOf(r).who.LoginName)
	if h.sshError(w, r, err) {
		return
	}
	name := h.deviceName(r, id)
	msg := "Set up SSH access to " + name + " as " + body.User
	if before.State != SSHStateNotSetUp {
		msg = "Changed SSH access to " + name + ": " + body.User + ", port " + strconv.Itoa(body.Port)
	}
	h.logAction(r, EventKindSSH, id, msg)
	h.writeJSON(w, r, http.StatusOK, h.sshOf(id))
}

// authorizeSSH answers POST /api/devices/{id}/ssh/authorize, which lets
// Roost log in to the machine it runs on as the user it runs as.
func (h *handler) authorizeSSH(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, added, err := h.opts.SSH.AuthorizeHere(r.Context(), id, callerOf(r).who.LoginName)
	if h.sshError(w, r, err) {
		return
	}
	msg := "Set up SSH access to the machine Roost runs on, as " + a.User
	if added {
		msg = "Added Roost's SSH key to the authorized_keys of " + a.User + " on the machine Roost runs on"
	}
	h.logAction(r, EventKindSSH, id, msg)
	h.writeJSON(w, r, http.StatusOK, h.sshOf(id))
}

// removeSSH answers DELETE /api/devices/{id}/ssh.
func (h *handler) removeSSH(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.sshError(w, r, h.opts.SSH.Remove(r.Context(), id)) {
		return
	}
	h.logAction(r, EventKindSSH, id, "Removed SSH access to "+h.deviceName(r, id))
	w.WriteHeader(http.StatusNoContent)
}

// checkSSH answers POST /api/devices/{id}/ssh/check, which logs in to the
// device afresh and finds out what it offers.
func (h *handler) checkSSH(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.opts.SSH.Check(r.Context(), id); h.sshError(w, r, err) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, h.sshOf(id))
}

// trustHostKey answers PUT /api/devices/{id}/ssh/host-key, which pins the
// host key the device offered, if it's the one the admin looked at.
func (h *handler) trustHostKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body TrustHostKey
	if !h.readJSON(w, r, &body) {
		return
	}
	if _, err := h.opts.SSH.TrustHostKey(r.Context(), id, body.Fingerprint); h.sshError(w, r, err) {
		return
	}
	h.logEvent(r, store.Event{
		Level: string(EventLevelWarning), Kind: string(EventKindSSH), DeviceID: id, Actor: callerOf(r).who.LoginName,
		Message: "Trusted the new SSH host key of " + h.deviceName(r, id) + ", " + body.Fingerprint,
	})
	h.writeJSON(w, r, http.StatusOK, h.sshOf(id))
}

// resetHostKey answers DELETE /api/devices/{id}/ssh/host-key, which forgets
// the device's pinned host key.
func (h *handler) resetHostKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.opts.SSH.ResetHostKey(r.Context(), id); h.sshError(w, r, err) {
		return
	}
	h.logEvent(r, store.Event{
		Level: string(EventLevelWarning), Kind: string(EventKindSSH), DeviceID: id, Actor: callerOf(r).who.LoginName,
		Message: "Reset the SSH host key of " + h.deviceName(r, id) + ", so Roost trusts the key it offers next",
	})
	h.writeJSON(w, r, http.StatusOK, h.sshOf(id))
}

// terminalSessions answers GET /api/devices/{id}/terminal/sessions.
func (h *handler) terminalSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.opts.SSH.Sessions(r.Context(), r.PathValue("id"))
	if errors.Is(err, ssh.ErrNoTmux) {
		h.writeJSON(w, r, http.StatusOK, TerminalSessions{Sessions: []TerminalSession{}})
		return
	}
	if h.sshError(w, r, err) {
		return
	}
	out := TerminalSessions{Tmux: true, Sessions: make([]TerminalSession, len(sessions))}
	for i, s := range sessions {
		out.Sessions[i] = TerminalSession{
			Name: s.Name, CreatedAt: timePtr(s.Created), ActiveAt: timePtr(s.Activity),
			Attached: s.Attached, Windows: s.Windows, Command: s.Command,
		}
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// killSession answers DELETE /api/devices/{id}/terminal/sessions/{name},
// which ends a tmux session and whatever runs in it.
func (h *handler) killSession(w http.ResponseWriter, r *http.Request) {
	id, name := r.PathValue("id"), r.PathValue("name")
	if h.sshError(w, r, h.opts.SSH.KillSession(r.Context(), id, name)) {
		return
	}
	h.logAction(r, EventKindTerminal, id, "Ended tmux session "+name+" on "+h.deviceName(r, id))
	w.WriteHeader(http.StatusNoContent)
}

package api

import (
	"errors"
	"net/http"

	"github.com/manasseh-fikadu/roost/internal/store"
)

// access answers GET /api/access.
func (h *handler) access(w http.ResponseWriter, r *http.Request) {
	h.writeAccess(w, r)
}

// changeAccess answers PATCH /api/access, which changes who can view Roost.
func (h *handler) changeAccess(w http.ResponseWriter, r *http.Request) {
	var body ChangeAccess
	if !h.readJSON(w, r, &body) {
		return
	}
	if body.WhoCanView != "" {
		var who string
		switch body.WhoCanView {
		case WhoCanViewTailnet:
			who = store.Tailnet
		case WhoCanViewListed:
			who = store.Listed
		default:
			h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{
				Error: `whoCanView is "` + string(WhoCanViewTailnet) + `" or "` + string(WhoCanViewListed) + `"`,
			})
			return
		}
		changed, err := h.opts.Store.SetWhoCanView(r.Context(), who)
		if err != nil {
			h.fail(w, r, "change who can view Roost", err)
			return
		}
		if changed {
			h.log.Info("changed who can view Roost", "by", callerOf(r).who.LoginName, "who_can_view", who)
		}
	}
	h.writeAccess(w, r)
}

// setRole answers PUT /api/access/users/{login}, which gives a login a role.
func (h *handler) setRole(w http.ResponseWriter, r *http.Request) {
	login, ok := h.loginParam(w, r)
	if !ok {
		return
	}
	var body SetRole
	if !h.readJSON(w, r, &body) {
		return
	}
	var role store.Role
	switch body.Role {
	case RoleAdmin:
		role = store.Admin
	case RoleViewer:
		role = store.Viewer
	default:
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{
			Error: `role is "` + string(RoleAdmin) + `" or "` + string(RoleViewer) + `"`,
		})
		return
	}
	changed, err := h.opts.Store.SetRole(r.Context(), login, role)
	if !h.roleChanged(w, r, err) {
		return
	}
	if changed {
		h.log.Info("gave a user a role", "by", callerOf(r).who.LoginName, "login", login, "role", string(role))
	}
	h.writeAccess(w, r)
}

// removeUser answers DELETE /api/access/users/{login}, which takes a login's
// role away.
func (h *handler) removeUser(w http.ResponseWriter, r *http.Request) {
	login, ok := h.loginParam(w, r)
	if !ok {
		return
	}
	removed, err := h.opts.Store.RemoveUser(r.Context(), login)
	if !h.roleChanged(w, r, err) {
		return
	}
	if removed {
		h.log.Info("took a user's role away", "by", callerOf(r).who.LoginName, "login", login)
	}
	h.writeAccess(w, r)
}

// loginParam returns the {login} in r's path. If it isn't a Tailscale login,
// it answers with an error and returns false.
func (h *handler) loginParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	login, err := store.CleanLogin(r.PathValue("login"))
	if err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return "", false
	}
	return login, true
}

// roleChanged reports whether a change to a role succeeded. If it didn't, it
// answers with why.
func (h *handler) roleChanged(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case errors.Is(err, store.ErrLastAdmin):
		h.writeJSON(w, r, http.StatusConflict, ErrorBody{Error: store.ErrLastAdmin.Error()})
		return false
	case err != nil:
		h.fail(w, r, "change a role", err)
		return false
	}
	return true
}

// writeAccess answers with who has a role in Roost and who else can view it.
func (h *handler) writeAccess(w http.ResponseWriter, r *http.Request) {
	who, users, err := h.opts.Store.Access(r.Context())
	if err != nil {
		h.fail(w, r, "look up who can view Roost", err)
		return
	}
	a := Access{WhoCanView: WhoCanViewListed, Users: make([]User, len(users))}
	if who == store.Tailnet {
		a.WhoCanView = WhoCanViewTailnet
	}
	for i, u := range users {
		a.Users[i] = User{Login: u.Login, Role: RoleViewer, Since: u.Since}
		if u.Role == store.Admin {
			a.Users[i].Role = RoleAdmin
		}
	}
	h.writeJSON(w, r, http.StatusOK, a)
}

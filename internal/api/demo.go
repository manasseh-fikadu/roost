package api

import (
	"net/http"
	"slices"
)

// A demoRefusal is a group of routes that a demo turns off.
type demoRefusal struct {
	// notice says which in a sentence of its own, such as "Terminals are".
	notice   string
	patterns []string
}

// demoRefusals lists the routes that a demo answers with a notice instead of
// doing anything. A demo has no machines to open a shell on, a file system to
// browse or a Docker to change, and Alex, who everyone is, must not lose
// their own role or the demo's SSH setup to the first visitor to click.
// TestDemoRefusals checks every pattern here is a route.
var demoRefusals = []demoRefusal{
	{"Terminals are", []string{
		"GET /api/devices/{id}/terminal",
		"GET /api/devices/{id}/terminal/sessions",
		"DELETE /api/devices/{id}/terminal/sessions/{name}",
	}},
	{"Files are", []string{
		"GET /api/devices/{id}/files/list",
		"GET /api/devices/{id}/files/stat",
		"GET /api/devices/{id}/files/raw",
		"GET /api/devices/{id}/files/archive",
		"GET /api/devices/{id}/files/search",
		"POST /api/devices/{id}/files/archive",
		"POST /api/devices/{id}/files/exists",
		"PUT /api/devices/{id}/files/upload",
		"PUT /api/devices/{id}/files/write",
		"POST /api/devices/{id}/files/mkdir",
		"POST /api/devices/{id}/files/rename",
		"POST /api/devices/{id}/files/move",
		"POST /api/devices/{id}/files/copy",
		"POST /api/devices/{id}/files/delete",
	}},
	{"Starting, stopping and restarting containers is", []string{
		"POST /api/devices/{id}/docker/containers/{cid}/start",
		"POST /api/devices/{id}/docker/containers/{cid}/stop",
		"POST /api/devices/{id}/docker/containers/{cid}/restart",
	}},
	{"Changing SSH access is", []string{
		"PUT /api/devices/{id}/ssh",
		"DELETE /api/devices/{id}/ssh",
		"POST /api/devices/{id}/ssh/check",
		"POST /api/devices/{id}/ssh/authorize",
		"PUT /api/devices/{id}/ssh/host-key",
		"DELETE /api/devices/{id}/ssh/host-key",
	}},
	{"Installing and removing agents is", []string{
		"POST /api/devices/{id}/agent/install",
		"POST /api/devices/{id}/agent/uninstall",
		"DELETE /api/devices/{id}/agent",
	}},
	{"Changing who can use Bield is", []string{
		"PATCH /api/access",
		"PUT /api/access/users/{login}",
		"DELETE /api/access/users/{login}",
	}},
}

// demoNotice returns what to tell whoever uses the route at pattern in a
// demo, or "" if a demo does what the route does.
func demoNotice(pattern string) string {
	for _, r := range demoRefusals {
		if slices.Contains(r.patterns, pattern) {
			return r.notice + " turned off in this demo, since its machines are made up. Run Bield on your own tailnet to use them."
		}
	}
	return ""
}

// refuseInDemo answers with a notice that a demo doesn't do this.
func (h *handler) refuseInDemo(notice string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.writeJSON(w, r, http.StatusForbidden, ErrorBody{Error: notice})
	})
}

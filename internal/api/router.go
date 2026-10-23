package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// A level is what a caller may do. Every route needs one, and a caller can
// use the route if theirs is at least as high.
type level int

const (
	// noLevel is the level of a route that doesn't say what it needs. No one
	// can use it, and TestRoutes fails.
	noLevel level = iota
	// anyone can use a route at this level, even anonymous callers.
	anyone
	// viewers and admins can use a route at this level.
	viewers
	// admins can use a route at this level.
	admins
	// agentToken is the level of a route for agents: a caller can use it if
	// they send the agents' token, and a role doesn't let anyone use it.
	agentToken
)

func (l level) String() string {
	switch l {
	case anyone:
		return "anyone"
	case viewers:
		return "viewers"
	case admins:
		return "admins"
	case agentToken:
		return "agent token"
	default:
		return "no level"
	}
}

// A route is a pattern on the router and the level a caller needs to use it.
type route struct {
	pattern string
	need    level
}

// anyMethod reports whether the route's pattern matches every method, as
// "/api/" does, and "GET /api/me" doesn't.
func (r route) anyMethod() bool { return !strings.Contains(r.pattern, " ") }

// needs returns the level a caller needs for a request with method. A route
// that matches every method needs an admin for anything but a GET, HEAD or
// OPTIONS, so it can't let a viewer change things.
func (r route) needs(method string) level {
	if r.anyMethod() && !safeMethod(method) {
		return max(r.need, admins)
	}
	return r.need
}

// router registers the API's routes on a ServeMux, each with the level it
// needs, and keeps a table of them for TestRoutes.
type router struct {
	h      *handler
	mux    *http.ServeMux
	routes []route
}

func newRouter(h *handler) *router {
	return &router{h: h, mux: http.NewServeMux()}
}

// read registers a GET route, which answers HEAD too, for callers at level
// need or above.
func (rt *router) read(pattern string, need level, next http.HandlerFunc) {
	if !strings.HasPrefix(pattern, http.MethodGet+" ") {
		panic("api: read route " + pattern + " isn't a GET")
	}
	rt.add(route{pattern, need}, next)
}

// write registers a route that changes something, such as "PUT
// /api/access/users/{login}". Only admins can use it.
func (rt *router) write(pattern string, next http.HandlerFunc) {
	switch method, _, _ := strings.Cut(pattern, " "); method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		panic("api: write route " + pattern + " needs a POST, PUT, PATCH or DELETE")
	}
	rt.add(route{pattern, admins}, next)
}

// agent registers a route for agents, such as "POST /api/agent/heartbeat".
// Anyone who sends the agents' token as a bearer token can use it, and no one
// else.
func (rt *router) agent(pattern string, next http.HandlerFunc) {
	if !strings.Contains(pattern, " ") {
		panic("api: agent route " + pattern + " needs a method")
	}
	rt.add(route{pattern, agentToken}, next)
}

// anyMethod registers a pattern without a method, such as "/" for the web UI,
// for callers at level need or above. Anything but a GET, HEAD or OPTIONS
// needs an admin.
func (rt *router) anyMethod(pattern string, need level, next http.Handler) {
	r := route{pattern, need}
	if !r.anyMethod() {
		panic("api: route " + pattern + " names a method, so register it with read or write")
	}
	rt.add(r, next)
}

func (rt *router) add(r route, next http.Handler) {
	rt.routes = append(rt.routes, r)
	rt.mux.Handle(r.pattern, rt.h.allow(r, next))
}

// allow lets a request through to next if its caller has the level that r
// needs for it.
func (h *handler) allow(r route, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.need == agentToken {
			if !h.fromAgent(req) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				h.deny(w, req, http.StatusUnauthorized, "only Roost's agents can do this, with the token from the install command")
				return
			}
			next.ServeHTTP(w, req)
			return
		}
		c := callerOf(req)
		need := r.needs(req.Method)
		if need != noLevel && c.level >= need {
			next.ServeHTTP(w, req)
			return
		}
		status := http.StatusForbidden
		if c.reason == ReasonRoleLookupFailed {
			// The caller may well have the level. The store couldn't say.
			status = http.StatusServiceUnavailable
		}
		h.deny(w, req, status, denial(c, need, req.Method))
	})
}

// fromAgent reports whether req carries the agents' token.
func (h *handler) fromAgent(req *http.Request) bool {
	token, ok := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	want := h.opts.AgentToken
	return ok && want != "" && subtle.ConstantTimeCompare([]byte(token), []byte(want)) == 1
}

// denial says why c can't make a request with method that needs a level.
func denial(c caller, need level, method string) string {
	var msg string
	switch {
	case need == viewers && c.who != nil:
		return "you can't view Roost until one of its admins makes you a viewer"
	case need == viewers:
		msg = "only Roost's viewers can see this"
	case safeMethod(method):
		msg = "only Roost's admins can see this"
	default:
		msg = "only Roost's admins can do this"
	}
	if c.who == nil {
		msg += ", and Tailscale didn't say who you are"
	}
	return msg
}

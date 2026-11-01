package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
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
	// agentSecret is the level of a route for enrolled agents: a caller can
	// use it if they send an agent's secret from the machine it enrolled on.
	// A role doesn't let anyone use it.
	agentSecret
	// enrollCode is the level of the route that enrolls an agent: a caller
	// can use it if they send an enrollment code that's good for their
	// machine. A role doesn't let anyone use it.
	enrollCode
)

func (l level) String() string {
	switch l {
	case anyone:
		return "anyone"
	case viewers:
		return "viewers"
	case admins:
		return "admins"
	case agentSecret:
		return "agent secret"
	case enrollCode:
		return "enrollment code"
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

// agent registers a route for enrolled agents, such as "POST
// /api/agent/heartbeat". A machine can use it if it sends its agent's secret
// as a bearer token, and no one else can.
func (rt *router) agent(pattern string, next http.HandlerFunc) {
	if !strings.Contains(pattern, " ") {
		panic("api: agent route " + pattern + " needs a method")
	}
	rt.add(route{pattern, agentSecret}, next)
}

// enroll registers a route that enrolls agents. A machine can use it if it
// sends an enrollment code that's good for it as a bearer token, and no one
// else can.
func (rt *router) enroll(pattern string, next http.HandlerFunc) {
	if !strings.Contains(pattern, " ") {
		panic("api: enroll route " + pattern + " needs a method")
	}
	rt.add(route{pattern, enrollCode}, next)
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
	if notice := demoNotice(r.pattern); notice != "" && rt.h.opts.Demo {
		next = rt.h.refuseInDemo(notice)
	}
	rt.mux.Handle(r.pattern, rt.h.allow(r, next))
}

// allow lets a request through to next if its caller has the level that r
// needs for it.
func (h *handler) allow(r route, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.need == agentSecret || r.need == enrollCode {
			// Before the secret or code is looked at, so guessing one is slow too.
			limit := h.limits.agent
			if r.need == enrollCode {
				limit = h.limits.enroll
			}
			if h.tooMany(w, req, limit) {
				return
			}
			if status, msg := h.checkMachine(req, r.need); status != 0 {
				if status == http.StatusUnauthorized {
					w.Header().Set("WWW-Authenticate", "Bearer")
				}
				h.deny(w, req, status, msg)
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

// checkMachine checks a request to a route for agents, which needs
// agentSecret or enrollCode. Its bearer token must be an agent's secret, or
// an enrollment code, that's good for the machine it came from, as Tailscale
// says. It returns the status and message to deny the request with, or 0.
func (h *handler) checkMachine(req *http.Request, need level) (int, string) {
	ctx, c, token := req.Context(), callerOf(req), bearer(req)
	// Without the machine, a code it used would look like another machine's.
	if c.reason == ReasonWhoIsFailed {
		return http.StatusServiceUnavailable, whoIsFailed
	}
	switch need {
	case agentSecret:
		node, err := h.opts.Store.EnrolledNode(ctx, token)
		switch {
		case errors.Is(err, store.ErrUnknownSecret):
			return http.StatusUnauthorized, unknownSecret
		case err != nil:
			h.log.Error("look up an agent's secret", "error", err)
			return http.StatusServiceUnavailable, "Bield couldn't check this agent's secret"
		case c.node != "" && node != c.node:
			h.log.Warn("refused an agent's secret from another machine", "enrolled", node, "from", c.node)
			return http.StatusForbidden, "this agent's secret belongs to another machine, so Bield refuses it from this one"
		}
	case enrollCode:
		if err := h.opts.Store.CheckEnrollmentCode(ctx, token, c.node, time.Now()); err != nil {
			return h.codeDenial(err)
		}
	default:
		panic("api: checkMachine for a route at level " + need.String())
	}
	if c.node == "" {
		return http.StatusForbidden, "Tailscale doesn't know the machine this came from"
	}
	return 0, ""
}

// Why Bield refuses a request from a machine.
const (
	whoIsFailed   = "Bield couldn't ask Tailscale which machine this is"
	unknownSecret = "Bield doesn't know this agent's secret, which an admin may have revoked; " +
		"to report again, run a new install command from Add agent on Bield's Agents page"
)

// codeDenial returns the status and message to refuse an enrollment code
// with, for an error from the store.
func (h *handler) codeDenial(err error) (int, string) {
	const next = "get a new one from Add agent on Bield's Agents page"
	switch {
	case errors.Is(err, store.ErrUnknownCode):
		return http.StatusUnauthorized, "Bield doesn't know the code in this install command: it expired a while ago, " +
			"or an admin revoked the agent it enrolled; " + next
	case errors.Is(err, store.ErrCodeExpired):
		return http.StatusUnauthorized, "this install command expired, since each one works for an hour; " + next
	case errors.Is(err, store.ErrCodeUsed):
		return http.StatusForbidden, "another machine used this install command, and each one works on one machine; " + next
	default:
		h.log.Error("check an enrollment code", "error", err)
		return http.StatusServiceUnavailable, "Bield couldn't check the code in this install command"
	}
}

// bearer returns the bearer token in req's Authorization header, or "".
func bearer(req *http.Request) string {
	token, _ := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")
	return token
}

// denial says why c can't make a request with method that needs a level.
func denial(c caller, need level, method string) string {
	var msg string
	switch {
	case need == viewers && c.who != nil:
		return "you can't view Bield until one of its admins makes you a viewer"
	case need == viewers:
		msg = "only Bield's viewers can see this"
	case safeMethod(method):
		msg = "only Bield's admins can see this"
	default:
		msg = "only Bield's admins can do this"
	}
	if c.who == nil {
		msg += ", and Tailscale didn't say who you are"
	}
	return msg
}

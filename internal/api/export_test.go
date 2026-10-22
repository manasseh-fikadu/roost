package api

import (
	"context"
	"net/http"
)

// Hooks for the api_test package.

// A Route is a pattern New registers and the level a caller needs to use it:
// "anyone", "viewers" or "admins".
type Route struct {
	Pattern string
	Level   string
}

// StubRoutes returns the handler New would, except that each route answers
// 204 No Content when it lets a request through, and the routes New
// registers.
func StubRoutes(opts Options) (http.Handler, []Route) {
	h := newHandler(opts)
	rt := newRouter(h)
	h.routes(rt)
	stub := newRouter(h)
	routes := make([]Route, len(rt.routes))
	for i, r := range rt.routes {
		stub.add(r, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		routes[i] = Route{Pattern: r.pattern, Level: r.need.String()}
	}
	return h.guard(stub.mux), routes
}

// ServeValue returns a handler with New's checks that answers GET /value
// with v, for viewers and admins, as any route's handler would.
func ServeValue(opts Options, v any) http.Handler {
	h := newHandler(opts)
	rt := newRouter(h)
	rt.read("GET /value", viewers, func(w http.ResponseWriter, r *http.Request) {
		h.writeJSON(w, r, http.StatusOK, v)
	})
	return h.guard(rt.mux)
}

// Redact is redact, for any value.
func Redact(v any) any { return redact(v) }

// Latest returns the feed's latest snapshot, as admins see it, building the
// first one if there's none yet.
func (f *Feed) Latest() Snapshot { return f.latest(context.Background()).snap }

const (
	FeedGap           = feedGap
	FeedHeartbeat     = feedHeartbeat
	RoleCheckInterval = roleCheckInterval
)

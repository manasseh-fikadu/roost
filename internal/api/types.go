package api

// The types in this file are API responses. `make types` turns them into
// TypeScript in web/src/lib/api/types.ts, so the web app and the server can't drift.

// Health is the response to GET /api/health.
type Health struct {
	// Status is "ok" whenever the hub can answer.
	Status string `json:"status"`
	// Version is the hub's build version.
	Version string `json:"version"`
}

// ErrorBody is the body of every API error response.
type ErrorBody struct {
	Error string `json:"error"`
}

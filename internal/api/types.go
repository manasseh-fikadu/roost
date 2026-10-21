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

// Me is the response to GET /api/me: who Roost thinks is asking.
type Me struct {
	// SignedIn is false for an anonymous caller.
	SignedIn bool `json:"signedIn"`
	// Login is the caller's Tailscale login, such as alice@example.com.
	Login string `json:"login,omitempty"`
	// Name is the caller's display name in Tailscale.
	Name string `json:"name,omitempty"`
	// Device is the MagicDNS name of the device the request came from.
	Device string `json:"device,omitempty"`
	// Admin reports whether the caller is one of Roost's admins.
	Admin bool `json:"admin"`
	// Reason says why an anonymous caller isn't signed in: one of the
	// Reason constants.
	Reason string `json:"reason,omitempty"`
}

// Why a caller is anonymous, in Me.Reason.
const (
	// ReasonUnknownDevice means the request didn't come from a device that
	// Tailscale knows.
	ReasonUnknownDevice = "unknown-device"
	// ReasonTaggedDevice means the device is tagged, so it belongs to no one.
	ReasonTaggedDevice = "tagged-device"
	// ReasonNoUser means Tailscale named the device but no user.
	ReasonNoUser = "no-user"
	// ReasonRoostMachine means the request came from Roost's own node, which
	// in host mode is the machine Roost runs on. Any process there could
	// send it, so it doesn't count as the machine's owner.
	ReasonRoostMachine = "roost-machine"
	// ReasonWhoIsFailed means Roost couldn't ask Tailscale.
	ReasonWhoIsFailed = "whois-failed"
)

package api

import "time"

// The types in this file are the API's requests and responses. `make types`
// turns them into TypeScript in web/src/lib/api/types.ts, so the web app and
// the server can't drift.
//
// A field that can carry a secret, such as a container's environment or a
// process's command line, is tagged roost:"secret", and only admins get its
// value. Everyone else gets the field's zero value. Say so in its comment.

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
	// Role is what the caller may do. It's empty if they can't view Roost:
	// anonymous callers can't, and neither can signed-in callers without a
	// role unless anyone on the tailnet can view it.
	Role Role `json:"role,omitempty"`
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

// Role is what a signed-in caller may do in Roost.
type Role string

// The roles a caller can have.
const (
	// RoleAdmin can do everything: open terminals, change files, control
	// Docker and change Roost's settings, including who has a role.
	RoleAdmin Role = "admin"
	// RoleViewer can see the read-only dashboard, without logs, files,
	// process command lines or other secrets.
	RoleViewer Role = "viewer"
)

// Access is the response to GET /api/access and to the requests that change
// it: who has a role in Roost, and who else can view it. Only admins can see
// or change it.
type Access struct {
	WhoCanView WhoCanView `json:"whoCanView"`
	// Users are the Tailscale logins that have a role, in order.
	Users []User `json:"users"`
}

// WhoCanView is who can view Roost, other than the users with a role.
type WhoCanView string

// Who can view Roost.
const (
	// WhoCanViewTailnet makes anyone who signs in from the tailnet a viewer,
	// unless they have a role of their own.
	WhoCanViewTailnet WhoCanView = "tailnet"
	// WhoCanViewListed lets only the users with a role view Roost.
	WhoCanViewListed WhoCanView = "listed"
)

// A User is a Tailscale login with a role in Roost.
type User struct {
	Login string `json:"login"`
	Role  Role   `json:"role"`
	// Since is when the login got its role.
	Since time.Time `json:"since"`
}

// ChangeAccess is the body of PATCH /api/access.
type ChangeAccess struct {
	// WhoCanView, if it's set, changes who can view Roost.
	WhoCanView WhoCanView `json:"whoCanView,omitempty"`
}

// SetRole is the body of PUT /api/access/users/{login}, which gives a
// Tailscale login a role. DELETE takes the role away. Roost always keeps an
// admin, so the last one can't be made a viewer or removed.
type SetRole struct {
	Role Role `json:"role"`
}

// Snapshot is the data of each "snapshot" event on GET /api/stream: what
// Roost knows at one moment. The stream sends one as soon as it opens and
// another every few seconds, and the web app keeps the latest.
type Snapshot struct {
	// GeneratedAt is when the hub made the snapshot.
	GeneratedAt time.Time `json:"generatedAt"`
	Hub         Hub       `json:"hub"`
	// Collectors are the jobs that gather what Roost knows, in a fixed order.
	Collectors []Collector `json:"collectors"`
	// Devices are the nodes on the tailnet: Roost's own, then its peers in
	// the order of their DNS names.
	Devices []Device `json:"devices"`
}

// Hub is the Roost server itself.
type Hub struct {
	// Version is the hub's build version.
	Version string `json:"version"`
	// DNSName is the MagicDNS name of Roost's node, such as
	// roost.example.ts.net.
	DNSName string `json:"dnsName"`
	// Tailnet is the tailnet's name, such as example.com.
	Tailnet string `json:"tailnet"`
}

// A Collector is a job that gathers what Roost knows, such as polling
// Tailscale for the tailnet's status.
type Collector struct {
	// Name says what the collector gathers, such as "Tailscale status".
	Name  string         `json:"name"`
	State CollectorState `json:"state"`
	// Error is why the latest run failed, while State is CollectorStateFailing.
	Error string `json:"error,omitempty"`
	// LastOK is when a run last worked, if one has.
	LastOK *time.Time `json:"lastOk,omitempty"`
}

// CollectorState says whether a collector works.
type CollectorState string

// The states of a Collector.
const (
	// CollectorStateOK means the latest run worked.
	CollectorStateOK CollectorState = "ok"
	// CollectorStateFailing means the latest run failed.
	CollectorStateFailing CollectorState = "failing"
	// CollectorStateWaiting means the collector hasn't finished a run yet.
	CollectorStateWaiting CollectorState = "waiting"
)

// A Device is a node on the tailnet.
type Device struct {
	// ID is the node's stable ID.
	ID string `json:"id"`
	// Name is the name the machine reports, or the first part of its DNS
	// name if it reports none. It may not be unique.
	Name string `json:"name"`
	// DNSName is the node's MagicDNS name, such as laptop.example.ts.net.
	DNSName string `json:"dnsName"`
	// OS is the node's operating system, as Tailscale names it, such as
	// "linux" or "macOS".
	OS  string   `json:"os"`
	IPs []string `json:"ips"`
	// Online reports whether the node is connected to Tailscale.
	Online bool `json:"online"`
	// Self marks Roost's own node.
	Self bool `json:"self,omitempty"`
}

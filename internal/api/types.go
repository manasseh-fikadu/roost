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
	// Reason says why an anonymous caller isn't signed in, or why Roost
	// doesn't know a signed-in caller's role: one of the Reason constants.
	Reason string `json:"reason,omitempty"`
}

// Why a caller is anonymous, or has no role, in Me.Reason.
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
	// ReasonRoleLookupFailed means the caller is signed in, but Roost
	// couldn't look up their role, so for now they have none.
	ReasonRoleLookupFailed = "role-lookup-failed"
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
// Roost knows at one moment. The hub builds one each time something changes,
// at most once a second, and at least every few seconds. The stream sends the
// latest as soon as it opens, then each new one, and the web app keeps the
// latest.
type Snapshot struct {
	// GeneratedAt is when the hub made the snapshot.
	GeneratedAt time.Time `json:"generatedAt"`
	Hub         Hub       `json:"hub"`
	// Collectors are the jobs that gather what Roost knows, in a fixed order.
	Collectors []Collector `json:"collectors"`
	Uplink     Uplink      `json:"uplink"`
	// Devices are the nodes on the tailnet: Roost's own first, then servers,
	// laptops, desktops, tablets, phones and others, each by name.
	Devices []Device `json:"devices"`
}

// Hub is the Roost server itself.
type Hub struct {
	// Version is the hub's build version.
	Version string `json:"version"`
	// DNSName is the MagicDNS name of Roost's node, such as
	// roost.example.ts.net.
	DNSName string `json:"dnsName"`
	// Tailnet is the tailnet's name, such as example.com. Only admins get
	// it: a personal tailnet is named after its owner's email address.
	Tailnet string `json:"tailnet" roost:"secret"`
}

// A Collector is a job that gathers what Roost knows, such as polling
// Tailscale for the tailnet's status.
type Collector struct {
	// Name says what the collector gathers, such as "Tailscale status".
	Name  string         `json:"name"`
	State CollectorState `json:"state"`
	// Error is why the latest run failed, while State is
	// CollectorStateFailing. Only admins get it, since it can name paths and
	// addresses on Roost's machine.
	Error string `json:"error,omitempty" roost:"secret"`
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

// Uplink is how Roost's machine reaches the internet and Tailscale's DERP
// relays, from Tailscale's latest connectivity check.
type Uplink struct {
	// CheckedAt is when connectivity was last checked, if it has been. The
	// other fields are empty until then.
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	// UDP reports whether UDP gets out, which direct connections need.
	UDP bool `json:"udp"`
	// IPv4 and IPv6 report whether the internet answered over each.
	IPv4 bool `json:"ipv4"`
	IPv6 bool `json:"ipv6"`
	// PublicIP is the address the internet sees Roost's machine at, IPv4 if
	// it has one. Only admins get it.
	PublicIP string `json:"publicIp,omitempty" roost:"secret"`
	// DERP is the relay region with the lowest latency, if any answered.
	DERP *DERPRegion `json:"derp,omitempty"`
}

// DERPRegion is a region of Tailscale's DERP relay servers.
type DERPRegion struct {
	// Code is the region's short name, such as "fra".
	Code string `json:"code"`
	// Name is the region's full name, such as "Frankfurt".
	Name string `json:"name"`
	// LatencyMs is the round trip to the region, in milliseconds.
	LatencyMs float64 `json:"latencyMs"`
}

// A Device is a node on the tailnet.
type Device struct {
	// ID is the node's stable ID.
	ID string `json:"id"`
	// Name is the display name an admin gave the device, or else HostName,
	// or else the first part of DNSName. It may not be unique.
	Name string `json:"name"`
	// HostName is the name the machine reports.
	HostName string `json:"hostName"`
	// DNSName is the node's MagicDNS name, such as laptop.example.ts.net.
	DNSName string `json:"dnsName"`
	// OS is the node's operating system, as Tailscale names it, such as
	// "linux" or "macOS".
	OS string `json:"os"`
	// Kind is what sort of machine it is: the kind an admin set, or else
	// Roost's guess.
	Kind DeviceKind `json:"kind"`
	IPs  []string   `json:"ips"`
	// Online reports whether the node is connected to Tailscale.
	Online bool `json:"online"`
	// LastSeen is when an offline node was last connected, if Tailscale
	// knows.
	LastSeen *time.Time `json:"lastSeen,omitempty"`
	// Self marks Roost's own node.
	Self   bool   `json:"self,omitempty"`
	Status Status `json:"status"`
	// StatusReason says why the device has its status, such as
	// "Disk 91% full".
	StatusReason string `json:"statusReason"`
	Path         Path   `json:"path"`
	// RxBytes and TxBytes count the traffic between Roost's node and this
	// one since Roost's node started.
	RxBytes int64 `json:"rxBytes"`
	TxBytes int64 `json:"txBytes"`
	// KeyExpiry is when the node's Tailscale key expires, unless expiry is
	// off for it.
	KeyExpiry *time.Time `json:"keyExpiry,omitempty"`
	// Created is when the node joined the tailnet.
	Created *time.Time `json:"created,omitempty"`
	// ExitNode reports whether Roost's traffic to the internet goes through
	// this node.
	ExitNode bool `json:"exitNode,omitempty"`
	// Metrics are the device's latest metrics, if Roost measures it. Roost
	// measures its own machine on Linux. Other machines need the agent.
	Metrics *Metrics `json:"metrics,omitempty"`
	// Info is what the machine is, if Roost measures it.
	Info     *DeviceInfo    `json:"info,omitempty"`
	Settings DeviceSettings `json:"settings"`
}

// Status is the state of something Roost watches, such as a device.
type Status string

// The states of a Status.
const (
	StatusOnline Status = "online"
	// StatusAttention means it works, but something needs a look, such as a
	// disk that's nearly full.
	StatusAttention Status = "attention"
	StatusOffline   Status = "offline"
	StatusUnknown   Status = "unknown"
)

// DeviceKind is what sort of machine a device is.
type DeviceKind string

// The kinds of Device, in the order the devices are listed.
const (
	DeviceKindServer  DeviceKind = "server"
	DeviceKindLaptop  DeviceKind = "laptop"
	DeviceKindDesktop DeviceKind = "desktop"
	DeviceKindTablet  DeviceKind = "tablet"
	DeviceKindPhone   DeviceKind = "phone"
	DeviceKindOther   DeviceKind = "other"
)

// Path is how traffic gets from Roost's node to a device.
type Path struct {
	Kind PathKind `json:"kind"`
	// Endpoint is the device's public ip:port on a direct path. Only admins
	// get it, since it says where the device is on the internet.
	Endpoint string `json:"endpoint,omitempty" roost:"secret"`
	// DERP is the code of the DERP region that relays traffic, on a DERP
	// path.
	DERP string `json:"derp,omitempty"`
	// PeerRelay is the ip:port:vni of the node that relays traffic, on a
	// peer relay path. Only admins get it.
	PeerRelay string `json:"peerRelay,omitempty" roost:"secret"`
	// HomeDERP is the code of the device's home DERP region, such as "fra".
	HomeDERP string `json:"homeDerp,omitempty"`
	// RTTMs is the round trip of the latest ping in milliseconds, if that
	// ping is recent and got a reply.
	RTTMs *float64 `json:"rttMs,omitempty"`
	// PingedAt is when the device was last pinged, if it has been.
	PingedAt *time.Time `json:"pingedAt,omitempty"`
	// Unanswered reports whether the latest ping is recent and got no reply.
	Unanswered bool `json:"unanswered,omitempty"`
}

// PathKind says how traffic gets from Roost's node to a device.
type PathKind string

// The kinds of Path.
const (
	// PathKindSelf is Roost's own node.
	PathKindSelf PathKind = "self"
	// PathKindDirect is a peer-to-peer WireGuard connection.
	PathKindDirect PathKind = "direct"
	// PathKindDERP is relayed through one of Tailscale's DERP servers.
	PathKindDERP PathKind = "derp"
	// PathKindPeerRelay is relayed through another node on the tailnet.
	PathKindPeerRelay PathKind = "peer-relay"
	// PathKindOffline means the device isn't connected.
	PathKindOffline PathKind = "offline"
)

// Metrics are what a machine was doing at one moment. A field is missing if
// the machine doesn't say.
type Metrics struct {
	At time.Time `json:"at"`
	// CPU is the percentage of time the CPUs were busy lately.
	CPU *float64 `json:"cpu,omitempty"`
	// Load is the 1, 5 and 15 minute load averages.
	Load []float64 `json:"load,omitempty"`
	// Procs is how many processes there are.
	Procs  int    `json:"procs,omitempty"`
	Memory *Usage `json:"memory,omitempty"`
	Swap   *Usage `json:"swap,omitempty"`
	// Disk is the file system that holds /.
	Disk *Usage `json:"disk,omitempty"`
	// NetRx and NetTx are bytes per second received and sent lately.
	NetRx *float64 `json:"netRx,omitempty"`
	NetTx *float64 `json:"netTx,omitempty"`
	// Temp is the hottest sensor, in degrees Celsius.
	Temp *float64 `json:"temp,omitempty"`
	// Uptime is how long since the machine booted, in seconds.
	Uptime int64 `json:"uptime,omitempty"`
}

// Usage is how much of something is used, in bytes.
type Usage struct {
	Used  uint64 `json:"used"`
	Total uint64 `json:"total"`
}

// DeviceInfo is what a machine is.
type DeviceInfo struct {
	Hostname string `json:"hostname,omitempty"`
	// OSVersion is the operating system's name and version, such as
	// "Debian GNU/Linux 13 (trixie)".
	OSVersion string `json:"osVersion,omitempty"`
	Kernel    string `json:"kernel,omitempty"`
	// Model is the machine's maker and product, if it says.
	Model string `json:"model,omitempty"`
	// Arch is the CPU architecture, such as x86_64.
	Arch  string `json:"arch,omitempty"`
	Cores int    `json:"cores,omitempty"`
}

// DeviceSettings are what admins set for a device. They're part of each
// Device, and PUT /api/devices/{id}/settings replaces them.
type DeviceSettings struct {
	// DisplayName is shown instead of the name the machine reports, unless
	// it's empty.
	DisplayName string `json:"displayName"`
	// Kind is what sort of machine an admin says the device is. If it's
	// empty, Roost guesses.
	Kind DeviceKind `json:"kind,omitempty"`
	// AlertOffline says whether to raise an alert when the device goes
	// offline. It's on by default, except for phones and tablets, which
	// leave the tailnet all the time.
	AlertOffline bool `json:"alertOffline"`
	// Notes are anything admins want to remember about the device. Viewers
	// can read them too.
	Notes string `json:"notes"`
}

// DeviceHistory is the response to GET /api/devices/{id}/history?range=,
// where the range is 1h, 6h, 24h or 7d.
type DeviceHistory struct {
	// From and To are when the history starts and ends.
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Step is the time each point sums up, in milliseconds.
	Step int64 `json:"step"`
	// RTT is the average round trip from Roost's node in milliseconds, for
	// each step in which the device was pinged. The value is null if no
	// ping in the step got a reply.
	RTT []Point `json:"rtt"`
}

// Point is a value at a time.
type Point struct {
	// TS is the start of the time the point sums up, in Unix milliseconds.
	TS int64 `json:"ts"`
	// V is the value, or null if there's none.
	V *float64 `json:"v" tstype:"number | null,required"`
}

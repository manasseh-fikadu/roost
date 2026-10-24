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
	// Checks are the checks admins set up, by group and name, and then a
	// ping check for each peer, in the order of Devices.
	Checks []Check `json:"checks"`
	// Health sums up the checks.
	Health HealthScore `json:"health"`
	// Alerts are the alerts that are open, acknowledged or snoozed, the most
	// severe first. GET /api/alerts has the resolved ones.
	Alerts []Alert `json:"alerts"`
	// Containers are the Docker containers on the machine Roost runs on, by
	// name. Other machines' come with #29.
	Containers []Container `json:"containers"`
	// Units are the systemd units Roost watches on the machine it runs on,
	// and the ones that failed, the system's first, by name.
	Units []Unit `json:"units"`
	// LastEventID is the ID of the activity log's latest event, so the app
	// can tell when GET /api/events has more.
	LastEventID int64 `json:"lastEventId"`
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
	// MeasuredBy says where Metrics and Info come from. It's empty if Roost
	// doesn't measure the device.
	MeasuredBy MeasuredBy `json:"measuredBy,omitempty"`
	// Metrics are the device's latest metrics, if Roost measures it. Roost
	// measures its own machine on Linux. Other machines need the agent.
	Metrics *Metrics `json:"metrics,omitempty"`
	// Info is what the machine is, if Roost measures it.
	Info *DeviceInfo `json:"info,omitempty"`
	// Agent is the device's agent, if one enrolled or reported. Roost
	// measures the device with it once it reports.
	Agent    *Agent         `json:"agent,omitempty"`
	Settings DeviceSettings `json:"settings"`
	// SSH is how Roost logs in to the device, if it can: Linux, macOS and
	// FreeBSD machines. Only admins get it.
	SSH *DeviceSSH `json:"ssh,omitempty" roost:"secret"`
}

// MeasuredBy says how Roost measures a device.
type MeasuredBy string

// The ways Roost measures a device.
const (
	// MeasuredByHub means the device is the machine Roost runs on, which
	// Roost measures itself.
	MeasuredByHub MeasuredBy = "hub"
	// MeasuredByAgent means the device's agent reports its metrics.
	MeasuredByAgent MeasuredBy = "agent"
)

// Agent is a device's agent, which sends a heartbeat with the machine's
// metrics every 15 seconds.
type Agent struct {
	// Version is the agent's version, such as 1.3.0, from its latest
	// heartbeat.
	Version string `json:"version,omitempty"`
	// LastSeen is when the agent's latest heartbeat arrived. It's missing
	// before the first.
	LastSeen *time.Time `json:"lastSeen,omitempty"`
	// Stale reports whether the device is online, but the agent stopped
	// sending heartbeats.
	Stale bool `json:"stale,omitempty"`
	// Outdated reports whether the agent is older than the one Roost serves.
	// A new install command updates it.
	Outdated bool `json:"outdated,omitempty"`
	// EnrolledAt is when the agent enrolled. It's missing if it isn't
	// enrolled, such as an agent from before enrollment, whose heartbeats
	// Roost refuses.
	EnrolledAt *time.Time `json:"enrolledAt,omitempty"`
	// EnrolledBy is the login of the admin who made the code it enrolled
	// with.
	EnrolledBy string `json:"enrolledBy,omitempty" roost:"secret"`
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
	// Unanswered reports whether the device is online but didn't answer the
	// last two pings, and the latest of them is recent.
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
	// Battery is missing if the machine has none.
	Battery *Battery `json:"battery,omitempty"`
	// Uptime is how long since the machine booted, in seconds.
	Uptime int64 `json:"uptime,omitempty"`
}

// Battery is how charged a machine's battery is.
type Battery struct {
	Percent  float64 `json:"percent"`
	Charging bool    `json:"charging"`
	// OnAC reports whether the machine runs on mains power.
	OnAC bool `json:"onAc"`
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
	// Kernel is the kernel's release, such as 6.12.48+deb13-amd64. Only
	// admins get it, since it says which known bugs the machine has.
	Kernel string `json:"kernel,omitempty" roost:"secret"`
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

// SetDeviceSettings is the body of PUT /api/devices/{id}/settings, which
// replaces a device's settings. A field that's left out gets its default.
type SetDeviceSettings struct {
	DisplayName string     `json:"displayName,omitempty"`
	Kind        DeviceKind `json:"kind,omitempty"`
	// AlertOffline is on if it's left out, except for phones and tablets.
	AlertOffline *bool  `json:"alertOffline,omitempty"`
	Notes        string `json:"notes,omitempty"`
}

// DeviceHistory is the response to GET /api/devices/{id}/history?range=,
// where the range is 1h, 6h, 24h or 7d.
type DeviceHistory struct {
	// From and To are when the history starts and ends.
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Step is the time each point sums up, in milliseconds.
	Step int64 `json:"step"`
	// CPU is the average percentage of time the CPUs were busy, and Memory
	// the average percentage of memory used, for each step in which Roost
	// measured the device. A value is null if the device didn't report it.
	CPU    []Point `json:"cpu"`
	Memory []Point `json:"memory"`
	// NetRx and NetTx are the average bytes per second received and sent,
	// for the same steps.
	NetRx []Point `json:"netRx"`
	NetTx []Point `json:"netTx"`
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

// AgentSetup is the response to GET /api/agent. Only admins can see it.
type AgentSetup struct {
	// Version is the version of the agent that Roost serves.
	Version string `json:"version"`
	// UninstallCommand removes the agent from the machine it runs on, for
	// the user who runs it.
	UninstallCommand string `json:"uninstallCommand"`
}

// AgentCode is the response to POST /api/agent/codes: a command that
// installs the agent on a macOS or Linux machine, or updates it, for the user
// who runs it. It holds a new enrollment code, which works on one machine:
// the first that uses it. That machine can use it again until it expires,
// such as after a failed install, unless an admin revokes its agent.
type AgentCode struct {
	InstallCommand string `json:"installCommand" roost:"secret"`
	// CreatedAt and ExpiresAt are by the hub's clock.
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// AgentSecret is the response to POST /api/agent/enroll: the secret the
// agent sends with each heartbeat from then on. It's not tagged
// roost:"secret", since only the machine that enrolled gets it.
type AgentSecret struct {
	Secret string `json:"secret"`
}

// DeviceUsage is the response to GET /api/devices/{id}/usage: what uses the
// device's CPU and memory now. With ?at= and a time in Unix milliseconds,
// it's the stored minute nearest that time instead.
type DeviceUsage struct {
	// Sample is missing if Roost has none.
	Sample *UsageSample `json:"sample,omitempty"`
	// Interval is how often the device sends a new sample, in seconds.
	Interval int `json:"interval"`
}

// UsageSample is what used a device's CPU and memory at one moment, or over
// a stored minute, in which case CPU is the minute's average.
type UsageSample struct {
	At time.Time `json:"at"`
	// Cores is how many CPU cores the device has, if it said.
	Cores int `json:"cores,omitempty"`
	// Groups are the biggest groups of processes, by CPU and then memory.
	Groups []UsageGroup `json:"groups"`
	// Procs are the biggest processes, by CPU and then memory.
	Procs []UsageProc `json:"procs"`
	// Total sums up every process, including the ones not listed, if the
	// device said.
	Total *UsageTotal `json:"total,omitempty"`
}

// UsageKind is what a group of processes is.
type UsageKind string

// The kinds of UsageGroup.
const (
	// UsageKindContainer is a Docker or Podman container.
	UsageKindContainer UsageKind = "container"
	// UsageKindService is a systemd system service.
	UsageKindService UsageKind = "service"
	// UsageKindUserService is a systemd user service, or an app a desktop
	// started.
	UsageKindUserService UsageKind = "user-service"
	// UsageKindSession is a user's login sessions, such as SSH.
	UsageKindSession UsageKind = "session"
	// UsageKindKernel is the kernel's own threads.
	UsageKindKernel UsageKind = "kernel"
	// UsageKindApp is a macOS app.
	UsageKindApp UsageKind = "app"
	// UsageKindOther is anything else, grouped by its program's name.
	UsageKindOther UsageKind = "other"
)

// UsageGroup is the processes of one service, container, app or the like.
type UsageGroup struct {
	// Key is the group's kind and name, as "kind:name". It's the same from
	// one sample to the next.
	Key  string    `json:"key"`
	Kind UsageKind `json:"kind"`
	Name string    `json:"name"`
	// CPU is the group's share of the machine's CPU time, all cores
	// together, in percent, so the groups add up to the machine's CPU.
	CPU float64 `json:"cpu"`
	// Mem is the group's resident memory in bytes, summed over its
	// processes.
	Mem uint64 `json:"mem"`
	// Procs is how many processes the group has.
	Procs int `json:"procs"`
	// Top names the group's busiest processes.
	Top []string `json:"top"`
}

// UsageProc is one process.
type UsageProc struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
	// User is who runs it, if the device said.
	User string `json:"user"`
	// Cmd is the command line. Only admins get it, since command lines can
	// hold passwords and tokens.
	Cmd string `json:"cmd" roost:"secret"`
	// CPU and Mem are as in UsageGroup.
	CPU float64 `json:"cpu"`
	Mem uint64  `json:"mem"`
	// Group is the key of the group the process is in.
	Group string `json:"group"`
}

// UsageTotal sums up every process on a device.
type UsageTotal struct {
	CPU   float64 `json:"cpu"`
	Mem   uint64  `json:"mem"`
	Procs int     `json:"procs"`
}

// UsageHistory is the response to GET /api/devices/{id}/usage/history?range=,
// where the range is 1h, 6h, 24h or 7d: which groups of processes used the
// device's CPU and memory.
type UsageHistory struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Step is the time each point sums up, in milliseconds.
	Step int64 `json:"step"`
	// Minutes is how many minutes Roost stored in the range. It stores one a
	// minute while it gets samples from the device.
	Minutes int `json:"minutes"`
	// Consumers are the biggest groups by average CPU, and by average
	// memory, biggest CPU first.
	Consumers []UsageConsumer `json:"consumers"`
	// CPU and Mem chart the few biggest groups by each.
	CPU []UsageSeries `json:"cpu"`
	Mem []UsageSeries `json:"mem"`
}

// UsageConsumer is what a group of processes used over a range. A minute
// in which the group was too small to store counts as zero.
type UsageConsumer struct {
	Key  string    `json:"key"`
	Kind UsageKind `json:"kind"`
	Name string    `json:"name"`
	// CPUAvg and CPUPeak are the group's average and highest CPU of the
	// minutes, in percent.
	CPUAvg  float64 `json:"cpuAvg"`
	CPUPeak float64 `json:"cpuPeak"`
	// MemAvg and MemPeak are its average and most memory, in bytes.
	MemAvg  uint64 `json:"memAvg"`
	MemPeak uint64 `json:"memPeak"`
}

// UsageSeries is a group's CPU, in percent, or memory, in bytes, averaged
// over each step that has stored minutes.
type UsageSeries struct {
	Key    string    `json:"key"`
	Kind   UsageKind `json:"kind"`
	Name   string    `json:"name"`
	Points []Point   `json:"points"`
}

// A Check probes something from the hub every so often: a URL, a TCP port or
// a host name that an admin set up, or a peer, which Roost pings.
type Check struct {
	// ID is a ping check's "ping:" and its device's ID.
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Kind CheckKind `json:"kind"`
	// Target is what the check probes: a URL, host:port, host name, or a
	// peer's IP address. Only admins get it, since a URL can hold a secret.
	Target string `json:"target" roost:"secret"`
	// Endpoint is the part of Target that holds no secret, for everyone: a
	// URL's scheme, host and port, or else all of Target.
	Endpoint string `json:"endpoint"`
	// Group is the group the check is listed in. Ping checks are in
	// "Tailnet".
	Group string `json:"group"`
	// IntervalSec is how often the check runs, in seconds.
	IntervalSec int `json:"intervalSec"`
	// TimeoutMs is how long the target has to answer, in milliseconds.
	TimeoutMs int `json:"timeoutMs"`
	// ExpectStatus is the status an HTTP check expects. If it's missing, any
	// status below 400 passes.
	ExpectStatus int `json:"expectStatus,omitempty"`
	// Insecure reports whether an HTTP check skips checking the server's
	// TLS certificate.
	Insecure bool `json:"insecure,omitempty"`
	// Paused reports whether an admin paused the check, so it only runs when
	// they ask.
	Paused bool `json:"paused,omitempty"`
	// DeviceID is the device a ping check pings. Roost makes a ping check for
	// each peer, and they can't be changed.
	DeviceID string     `json:"deviceId,omitempty"`
	State    CheckState `json:"state"`
	// StateReason says why the check has its state, such as "HTTP 200 OK" or
	// why it failed.
	StateReason string `json:"stateReason"`
	// Last is the check's latest result, if it has one. GET
	// /api/checks/recent has the ones before.
	Last *CheckResult `json:"last,omitempty"`
	// Failures is how many of its latest results failed in a row.
	Failures int `json:"failures"`
	// Uptime is the percentage of its results in the last day that passed,
	// or null if it has none.
	Uptime *float64 `json:"uptime" tstype:"number | null,required"`
}

// CheckKind is what a check probes.
type CheckKind string

// The kinds of Check.
const (
	// CheckKindHTTP sends a GET request to a URL. It passes if the status is
	// the one expected, or below 400 if none is. It doesn't follow
	// redirects.
	CheckKindHTTP CheckKind = "http"
	// CheckKindTCP passes if a TCP connection to host:port opens.
	CheckKindTCP CheckKind = "tcp"
	// CheckKindDNS passes if a host name resolves, with the hub's resolver.
	CheckKindDNS CheckKind = "dns"
	// CheckKindPing is Roost's ping of a peer over the tailnet, every
	// minute. It fails when the peer misses two pings in a row, as on the
	// device's page, since a peer can miss one now and then.
	CheckKindPing CheckKind = "ping"
)

// CheckState says whether a check passes.
type CheckState string

// The states of a Check.
const (
	CheckStatePassing CheckState = "passing"
	CheckStateFailing CheckState = "failing"
	// CheckStatePending means the check hasn't got a result yet.
	CheckStatePending CheckState = "pending"
	// CheckStatePaused means an admin paused the check.
	CheckStatePaused CheckState = "paused"
	// CheckStateUnavailable means the check can't run now, such as a ping
	// check of a device that's offline.
	CheckStateUnavailable CheckState = "unavailable"
)

// CheckResult is what happened when a check ran once.
type CheckResult struct {
	// At is when it started.
	At time.Time `json:"at"`
	OK bool      `json:"ok"`
	// LatencyMs is how long the target took to answer, in milliseconds. It's
	// missing if nothing answered.
	LatencyMs *float64 `json:"latencyMs,omitempty"`
	// Detail says what happened, such as "HTTP 200 OK", or why it failed.
	Detail string `json:"detail"`
}

// RecentResults is the response to GET /api/checks/recent: the latest
// results of every check, up to 30 of each, so the app can draw them. The
// snapshots only have each check's last one.
type RecentResults struct {
	// Checks holds each check's results, oldest first, by check ID.
	Checks map[string][]CheckResult `json:"checks"`
}

// HealthScore sums up the checks: the share of the checks that run and
// pass.
type HealthScore struct {
	// Score is the percentage of the live checks, the ones passing or
	// failing, that pass, rounded down, so it's 100 only if all of them
	// pass. It's null if none are live.
	Score *int        `json:"score" tstype:"number | null,required"`
	Label HealthLabel `json:"label"`
	// Passing and Failing count the live checks.
	Passing int `json:"passing"`
	Failing int `json:"failing"`
	// Unavailable counts the other checks: pending, paused or unavailable.
	Unavailable int `json:"unavailable"`
	Total       int `json:"total"`
}

// HealthLabel puts a health score in words.
type HealthLabel string

// The labels of a HealthScore.
const (
	// HealthLabelHealthy is a score of 90 or more.
	HealthLabelHealthy HealthLabel = "healthy"
	// HealthLabelDegraded is a score of 70 or more.
	HealthLabelDegraded HealthLabel = "degraded"
	// HealthLabelCritical is a score below 70.
	HealthLabelCritical HealthLabel = "critical"
	// HealthLabelUnknown means there's no score, since no check is live.
	HealthLabelUnknown HealthLabel = "unknown"
)

// CheckInput is the body of POST /api/checks, which adds a check, and of PUT
// /api/checks/{id}, which replaces one. Both answer with the Check. A field
// that's left out gets its default.
type CheckInput struct {
	// Name is required.
	Name string `json:"name"`
	// Kind is http, tcp or dns. Roost makes the ping checks.
	Kind CheckKind `json:"kind"`
	// Target is a full http or https URL for an HTTP check, host:port for a
	// TCP check, or a host name for a DNS check.
	Target string `json:"target"`
	// Group is "Custom" if it's left out.
	Group string `json:"group,omitempty"`
	// IntervalSec is from 10 seconds to a day, and 60 if it's left out.
	IntervalSec int `json:"intervalSec,omitempty"`
	// TimeoutMs is from 200 ms to a minute, and shorter than the interval.
	// It's 5000 if it's left out.
	TimeoutMs    int  `json:"timeoutMs,omitempty"`
	ExpectStatus int  `json:"expectStatus,omitempty"`
	Insecure     bool `json:"insecure,omitempty"`
	Paused       bool `json:"paused,omitempty"`
}

// CheckHistory is the response to GET /api/checks/{id}/history?range=, where
// the range is 1h, 6h, 24h or 7d.
type CheckHistory struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Step is the time each point sums up, in milliseconds.
	Step int64 `json:"step"`
	// Latency is the average time the target took to answer, in
	// milliseconds, for each step in which the check ran. The value is null
	// if nothing answered.
	Latency []Point `json:"latency"`
	// Uptime is the percentage of the runs that passed, for the same steps.
	Uptime []Point `json:"uptime"`
}

// HealthHistory is the response to GET /api/health/history?range=, where the
// range is 1h, 6h, 24h or 7d. GET /api/health itself only says the hub is
// up.
type HealthHistory struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Step is the time each point sums up, in milliseconds.
	Step int64 `json:"step"`
	// Score is the average health score for each step that has one. Roost
	// samples it every minute.
	Score []Point `json:"score"`
}

// An Alert is a condition that needs attention, such as a disk that's nearly
// full, from when Roost saw it until it cleared. It resolves by itself when
// the condition clears.
type Alert struct {
	ID     int64       `json:"id"`
	Source AlertSource `json:"source"`
	// DeviceID is the device it's on, if any.
	DeviceID string `json:"deviceId,omitempty"`
	// CheckID is the check it's about, if any.
	CheckID  string        `json:"checkId,omitempty"`
	Severity AlertSeverity `json:"severity"`
	State    AlertState    `json:"state"`
	// Title and Detail say what's wrong, such as "Disk 91% full on laptop".
	// While the alert is open, they say what Roost sees now.
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// OpenedAt is when Roost first saw the condition, and LastSeen when it
	// last did.
	OpenedAt time.Time `json:"openedAt"`
	LastSeen time.Time `json:"lastSeen"`
	// ResolvedAt is when the condition cleared, if it has.
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
	// AckedAt is when an admin acknowledged the alert, if one did, and
	// AckedBy their login, which only admins get.
	AckedAt *time.Time `json:"ackedAt,omitempty"`
	AckedBy string     `json:"ackedBy,omitempty" roost:"secret"`
	// SnoozedUntil is when a snoozed alert wakes, and SnoozedBy the login of
	// the admin who snoozed it, which only admins get.
	SnoozedUntil *time.Time `json:"snoozedUntil,omitempty"`
	SnoozedBy    string     `json:"snoozedBy,omitempty" roost:"secret"`
	// Notes are what admins wrote on it, oldest first.
	Notes []AlertNote `json:"notes"`
}

// AlertNote is what an admin wrote on an alert.
type AlertNote struct {
	At time.Time `json:"at"`
	// By is the admin's login. Only admins get it.
	By   string `json:"by,omitempty" roost:"secret"`
	Text string `json:"text"`
}

// AlertSource says what an alert is about.
type AlertSource string

// The sources of an Alert.
const (
	// AlertSourceDevice is a device that's offline, or whose Tailscale key
	// expires soon.
	AlertSourceDevice AlertSource = "device"
	// AlertSourceAgent is an agent that stopped reporting.
	AlertSourceAgent AlertSource = "agent"
	// AlertSourceMetrics is a device's disk, memory or CPU.
	AlertSourceMetrics AlertSource = "metrics"
	// AlertSourceCheck is a check that's failing.
	AlertSourceCheck AlertSource = "check"
	// AlertSourceContainer is a Docker container that crashed, stopped or
	// is unhealthy.
	AlertSourceContainer AlertSource = "container"
	// AlertSourceUnit is a systemd unit that failed, or a watched one that
	// isn't running.
	AlertSourceUnit AlertSource = "unit"
	// AlertSourceCollector is one of Roost's collectors that keeps failing.
	AlertSourceCollector AlertSource = "collector"
)

// AlertSeverity says how much an alert matters.
type AlertSeverity string

// The severities of an Alert, most first.
const (
	AlertSeverityCritical AlertSeverity = "critical"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityInfo     AlertSeverity = "info"
)

// AlertState says where an alert is.
type AlertState string

// The states of an Alert.
const (
	AlertStateOpen AlertState = "open"
	// AlertStateAcknowledged means an admin is on it.
	AlertStateAcknowledged AlertState = "acknowledged"
	// AlertStateSnoozed means an admin hid it until SnoozedUntil.
	AlertStateSnoozed  AlertState = "snoozed"
	AlertStateResolved AlertState = "resolved"
)

// AlertList is the response to GET /api/alerts?before=&limit=: resolved
// alerts, the latest opened first, kept for 30 days. Before is the ID to
// list from, for the next page.
type AlertList struct {
	Alerts []Alert `json:"alerts"`
	// More reports whether there are older ones.
	More bool `json:"more"`
}

// SnoozeAlert is the body of PUT /api/alerts/{id}/snooze, which snoozes an
// open alert. DELETE wakes it.
type SnoozeAlert struct {
	// Minutes is how long to snooze it for, up to 30 days.
	Minutes int `json:"minutes"`
}

// AddNote is the body of POST /api/alerts/{id}/notes, which adds a note to
// an alert, open or resolved, and answers with the AlertNote.
type AddNote struct {
	Text string `json:"text"`
}

// Event is a line in the activity log: something Roost saw happen, or
// something someone did.
type Event struct {
	ID      int64      `json:"id"`
	At      time.Time  `json:"at"`
	Level   EventLevel `json:"level"`
	Kind    EventKind  `json:"kind"`
	Message string     `json:"message"`
	// DeviceID is the device it's about, if any.
	DeviceID string `json:"deviceId,omitempty"`
	// Actor is the login of who did it, if someone did. Only admins get it.
	Actor string `json:"actor,omitempty" roost:"secret"`
}

// EventLevel says how an event went.
type EventLevel string

// The levels of an Event.
const (
	EventLevelInfo    EventLevel = "info"
	EventLevelSuccess EventLevel = "success"
	EventLevelWarning EventLevel = "warning"
	EventLevelError   EventLevel = "error"
)

// EventKind says what an event is about.
type EventKind string

// The kinds of Event.
const (
	// EventKindDevice is a device that joined, left, went offline or came
	// online, or whose settings an admin changed.
	EventKindDevice EventKind = "device"
	// EventKindAgent is an agent that enrolled, was revoked or uninstalled,
	// or started or stopped reporting.
	EventKindAgent EventKind = "agent"
	// EventKindAlert is an alert that opened or resolved, or that an admin
	// acknowledged, snoozed or wrote on.
	EventKindAlert EventKind = "alert"
	// EventKindCheck is a check an admin added, changed or deleted.
	EventKindCheck EventKind = "check"
	// EventKindContainer is a container that started, stopped or was
	// removed.
	EventKindContainer EventKind = "container"
	// EventKindAccess is a change to who has a role in Roost.
	EventKindAccess EventKind = "access"
	// EventKindSSH is SSH access to a device that an admin set up, changed
	// or removed, or a host key that changed.
	EventKindSSH EventKind = "ssh"
	// EventKindTerminal is a terminal an admin opened, or a tmux session
	// they ended.
	EventKindTerminal EventKind = "terminal"
)

// EventList is the response to GET /api/events?before=&after=&limit=&kind=&device=:
// events, newest first. Before and After are event IDs.
type EventList struct {
	Events []Event `json:"events"`
	// More reports whether there are older ones than the last.
	More bool `json:"more"`
}

// A Container is a Docker container.
type Container struct {
	// ID is the first 12 characters of Docker's ID.
	ID    string `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
	// Project and Service are its Docker Compose project and service, if
	// it has them.
	Project string         `json:"project,omitempty"`
	Service string         `json:"service,omitempty"`
	State   ContainerState `json:"state"`
	// Status says it in Docker's words, such as "Up 2 hours (healthy)".
	Status string `json:"status"`
	// Health is "healthy", "unhealthy" or "starting", or missing if it has
	// no health check.
	Health   string `json:"health,omitempty"`
	ExitCode int    `json:"exitCode"`
	// OOMKilled reports whether it was killed for using too much memory.
	OOMKilled    bool `json:"oomKilled,omitempty"`
	RestartCount int  `json:"restartCount"`
	// RestartPolicy is "no", "always", "unless-stopped" or "on-failure".
	RestartPolicy string     `json:"restartPolicy"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
	FinishedAt    *time.Time `json:"finishedAt,omitempty"`
	// Ports are the ports it listens on.
	Ports []ContainerPort `json:"ports"`
	// Problem says what's wrong with it, if anything, such as "crashed
	// (exit 1)". Then it has an alert.
	Problem string `json:"problem,omitempty"`
	// DeviceID is the device it runs on.
	DeviceID string `json:"deviceId"`
	// CheckIDs are the checks of a port it publishes on its machine.
	CheckIDs []string `json:"checkIds"`
}

// ContainerState is the state Docker says a container is in.
type ContainerState string

// The states of a Container.
const (
	ContainerStateCreated    ContainerState = "created"
	ContainerStateRunning    ContainerState = "running"
	ContainerStatePaused     ContainerState = "paused"
	ContainerStateRestarting ContainerState = "restarting"
	ContainerStateRemoving   ContainerState = "removing"
	ContainerStateExited     ContainerState = "exited"
	ContainerStateDead       ContainerState = "dead"
)

// ContainerPort is a port a container listens on.
type ContainerPort struct {
	// IP is the host address it's published on, if it is.
	IP string `json:"ip,omitempty"`
	// Private is the container's port, and Public the host's, if it's
	// published.
	Private int    `json:"private"`
	Public  int    `json:"public,omitempty"`
	Type    string `json:"type"`
}

// A Unit is a systemd unit.
type Unit struct {
	// Name is its ID, such as "ssh.service".
	Name        string    `json:"name"`
	Scope       UnitScope `json:"scope"`
	Description string    `json:"description"`
	// Active is systemd's ActiveState, such as "active", "inactive" or
	// "failed", and Sub its SubState, such as "running".
	Active string `json:"active"`
	Sub    string `json:"sub"`
	// Since is when it entered its active state, if systemd says.
	Since   *time.Time `json:"since,omitempty"`
	MainPID int        `json:"mainPid,omitempty"`
	// Memory is the memory it uses, in bytes, if systemd counts it.
	Memory uint64 `json:"memory,omitempty"`
	// Watched reports whether Roost watches it. Other units are only
	// listed while they've failed.
	Watched bool `json:"watched"`
	// DeviceID is the device it runs on.
	DeviceID string `json:"deviceId"`
}

// UnitScope says whose a unit is.
type UnitScope string

// The scopes of a Unit.
const (
	// UnitScopeSystem is the machine's.
	UnitScopeSystem UnitScope = "system"
	// UnitScopeUser is the user Roost runs as.
	UnitScopeUser UnitScope = "user"
)

// DeviceSSH is how Roost logs in to a device over SSH, with its own key,
// for terminals, files and Docker. The machine Roost runs on can be two
// nodes, which share it.
type DeviceSSH struct {
	State SSHState `json:"state"`
	// User is the account Roost logs in as, and Port where sshd listens.
	// They're empty until an admin sets up access.
	User string `json:"user,omitempty"`
	Port int    `json:"port,omitempty"`
	// Hub marks the machine Roost runs on, where Roost logs in on the
	// loopback address, and can add its key for the user it runs as.
	Hub bool `json:"hub,omitempty"`
	// HostKey is the SHA256 fingerprint of the machine's host key, which
	// Roost pinned the first time it logged in.
	HostKey string `json:"hostKey,omitempty"`
	// OfferedKey is the fingerprint of the key the machine offered instead,
	// while the State is SSHStateHostKeyChanged.
	OfferedKey string `json:"offeredKey,omitempty"`
	// Error says why Roost can't log in, while the State is
	// SSHStateFailing or SSHStateHostKeyChanged.
	Error string `json:"error,omitempty"`
	// CheckedAt is when Roost last checked the machine, if it has.
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	// Tmux says whether the machine has tmux, which keeps terminals running
	// after their tab closes, and Docker whether Roost's user can open its
	// Docker socket. Each is missing until a check finds out.
	Tmux   *bool `json:"tmux,omitempty"`
	Docker *bool `json:"docker,omitempty"`
}

// SSHState says whether Roost can log in to a device.
type SSHState string

// The states of a DeviceSSH.
const (
	// SSHStateNotSetUp means no admin has set up access yet.
	SSHStateNotSetUp SSHState = "not-set-up"
	// SSHStateUnchecked means Roost hasn't logged in since it started, or
	// since the access changed.
	SSHStateUnchecked SSHState = "unchecked"
	SSHStateOK        SSHState = "ok"
	SSHStateFailing   SSHState = "failing"
	// SSHStateHostKeyChanged means the machine offered another host key
	// than the one Roost pinned, so Roost won't log in until an admin
	// resets it, such as after the machine was reinstalled.
	SSHStateHostKeyChanged SSHState = "host-key-changed"
)

// SSHSetup is the response to GET /api/devices/{id}/ssh: how to let Roost
// log in to a device. Only admins can see it.
type SSHSetup struct {
	// Command adds AuthorizedKey to the authorized_keys of the user who
	// runs it on the device.
	Command string `json:"command"`
	// AuthorizedKey is Roost's key, which only works from Roost's tailnet
	// addresses, and on the machine Roost runs on, from the loopback
	// addresses too. Agent and X11 forwarding are off for it.
	AuthorizedKey string `json:"authorizedKey"`
	// HubUser is the user Roost runs as, on the machine Roost runs on,
	// which POST /api/devices/{id}/ssh/authorize logs in as.
	HubUser string `json:"hubUser,omitempty"`
}

// SetSSH is the body of PUT /api/devices/{id}/ssh, which sets up how Roost
// logs in to a device and answers with its DeviceSSH. Changing the port
// forgets the pinned host key. DELETE removes the access.
type SetSSH struct {
	User string `json:"user"`
	// Port is 22 if it's left out.
	Port int `json:"port,omitempty"`
}

// TrustHostKey is the body of PUT /api/devices/{id}/ssh/host-key, which
// pins the host key a device offered in place of the one Roost pinned, and
// answers with the device's DeviceSSH. Fingerprint is DeviceSSH.OfferedKey,
// the one an admin looked at: if the device offers another by then, Roost
// refuses with a 409, so it pins exactly the key the admin saw. DELETE
// forgets the pinned key instead, so Roost pins whichever key the device
// offers next.
type TrustHostKey struct {
	Fingerprint string `json:"fingerprint"`
}

// TerminalSessions is the response to GET
// /api/devices/{id}/terminal/sessions: the tmux sessions on a device.
type TerminalSessions struct {
	// Tmux says whether the device has tmux. Without it, there are no
	// sessions, and a terminal ends when its tab closes.
	Tmux bool `json:"tmux"`
	// Sessions are by name.
	Sessions []TerminalSession `json:"sessions"`
}

// A TerminalSession is a tmux session, which keeps running after the
// terminals attached to it close. DELETE
// /api/devices/{id}/terminal/sessions/{name} ends one.
type TerminalSession struct {
	Name string `json:"name"`
	// CreatedAt is when it started, and ActiveAt when something last
	// happened in it.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	ActiveAt  *time.Time `json:"activeAt,omitempty"`
	// Attached is how many terminals are attached, in Roost or not.
	Attached int `json:"attached"`
	Windows  int `json:"windows"`
	// Command is what runs in its active pane, such as bash or htop.
	Command string `json:"command,omitempty"`
}

// TerminalWindow is how many bytes of output the hub sends a terminal
// ahead of what the browser says it has written to the screen.
const TerminalWindow = 1 << 20

// TerminalMessage is a JSON text frame on the WebSocket of GET
// /api/devices/{id}/terminal?session=&cwd=&cols=&rows=, which only admins
// can open. Without a session, the terminal is a login shell that ends when
// the WebSocket closes. The shell's output comes in binary frames.
//
// The browser sends input, a new size, and, as it writes output to the
// screen, how many bytes it wrote: the hub sends at most TerminalWindow
// bytes ahead, and waits for that before it sends more, so a command that
// floods the terminal can't flood the browser. The hub sends ready once the
// shell starts, exit with its status when it ends, and error if it can't
// start one.
type TerminalMessage struct {
	Type TerminalMessageType `json:"t"`
	// Data is the input, for TerminalMessageTypeInput.
	Data string `json:"d,omitempty"`
	// Cols and Rows are the size, for TerminalMessageTypeResize.
	Cols int `json:"c,omitempty"`
	Rows int `json:"r,omitempty"`
	// Bytes is how much output the browser wrote, for
	// TerminalMessageTypeAck.
	Bytes int `json:"n,omitempty"`
	// Session is the tmux session, for TerminalMessageTypeReady.
	Session string `json:"session,omitempty"`
	// Code is the exit status, for TerminalMessageTypeExit. It's missing if
	// the shell didn't say one, because the connection to the machine
	// dropped. The shell, and the tmux session it was attached to, may
	// still be running then, and a tmux client that exits with 0 may only
	// have detached.
	Code *int `json:"code,omitempty"`
	// Message says what went wrong, for TerminalMessageTypeError.
	Message string `json:"message,omitempty"`
}

// TerminalMessageType says what a TerminalMessage is.
type TerminalMessageType string

// The types of TerminalMessage.
const (
	// The browser sends these.
	TerminalMessageTypeInput  TerminalMessageType = "i"
	TerminalMessageTypeResize TerminalMessageType = "r"
	TerminalMessageTypeAck    TerminalMessageType = "a"
	// The hub sends these.
	TerminalMessageTypeReady TerminalMessageType = "ready"
	TerminalMessageTypeExit  TerminalMessageType = "exit"
	TerminalMessageTypeError TerminalMessageType = "error"
)

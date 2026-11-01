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

// Settings are what admins can change about how Roost works. GET
// /api/settings has them, and PATCH changes them. They're live: Roost uses a
// change as soon as it's saved.
type Settings struct {
	General    GeneralSettings `json:"general"`
	Thresholds Thresholds      `json:"thresholds"`
	// Rules say which alerts Roost raises.
	Rules AlertRules `json:"rules"`
	// WatchedUnits are the systemd units on the machine Roost runs on that
	// it watches, such as "ssh.service", or "user:app.service" for a unit of
	// the user Roost runs as. Roost always shows failed units too.
	WatchedUnits  []string             `json:"watchedUnits"`
	Retention     RetentionSettings    `json:"retention"`
	Notifications NotificationSettings `json:"notifications"`
}

// GeneralSettings are how Roost presents itself.
type GeneralSettings struct {
	// Name is shown in the app's header and in the title of every page.
	Name string `json:"name"`
	// PublicURL is the address that people and agents reach Roost at, such
	// as https://roost.example.ts.net, if it isn't the one Roost knows. It
	// is used in the links and commands Roost writes, such as the agent's
	// install command. It's empty to use the address of the request.
	PublicURL string `json:"publicUrl"`
	// SetupDone is whether an admin finished or skipped the first-run setup,
	// which the app shows admins until then.
	SetupDone bool `json:"setupDone"`
}

// Thresholds are when a device needs attention, and when an alert opens.
type Thresholds struct {
	// DiskWarn and DiskCritical are how full a disk can get, in percent,
	// before a warning, and before a critical alert.
	DiskWarn     float64 `json:"diskWarn"`
	DiskCritical float64 `json:"diskCritical"`
	// MemoryWarn is how much memory can be used, in percent.
	MemoryWarn float64 `json:"memoryWarn"`
	// CPUWarn is how busy the CPUs can be, in percent, on average over the
	// last CPUWindowMinutes.
	CPUWarn          float64 `json:"cpuWarn"`
	CPUWindowMinutes int     `json:"cpuWindowMinutes"`
	// OfflineMinutes is how long a device can be offline before an alert,
	// if it alerts when it's offline.
	OfflineMinutes int `json:"offlineMinutes"`
	// KeyExpiryDays is how soon before a device's Tailscale key expires to
	// warn about it.
	KeyExpiryDays int `json:"keyExpiryDays"`
	// CheckFailures is how many times in a row a check must fail before an
	// alert.
	CheckFailures int `json:"checkFailures"`
}

// AlertRules say which alerts Roost raises. A device that a rule is off for
// isn't flagged for it on the dashboard either, and turning a rule off
// resolves its open alerts.
type AlertRules struct {
	// Offline is a device that has been off the tailnet for too long.
	Offline bool `json:"offline"`
	// KeyExpiry is a Tailscale key that is about to expire.
	KeyExpiry bool `json:"keyExpiry"`
	// AgentStopped is an agent that stopped reporting.
	AgentStopped bool `json:"agentStopped"`
	// Disk, Memory and CPU are a device that is past its threshold.
	Disk   bool `json:"disk"`
	Memory bool `json:"memory"`
	CPU    bool `json:"cpu"`
	// Checks is a check that keeps failing.
	Checks bool `json:"checks"`
	// Containers is a Docker container on Roost's machine that stopped
	// unexpectedly.
	Containers bool `json:"containers"`
	// Units is a systemd unit that failed, or a watched one that isn't
	// running.
	Units bool `json:"units"`
	// Collectors is one of Roost's own jobs that keeps failing.
	Collectors bool `json:"collectors"`
}

// RetentionSettings are how many days Roost keeps each kind of history.
// Roost deletes what is older within the hour.
type RetentionSettings struct {
	// ActivityDays is how long the activity log and resolved alerts last.
	ActivityDays int `json:"activityDays"`
	// HistoryDays is how long what devices and containers used, and the
	// round-trip times to devices, last.
	HistoryDays int `json:"historyDays"`
	// ChecksDays is how long the results of checks, and the health score,
	// last.
	ChecksDays int `json:"checksDays"`
}

// NotificationSettings are when Roost sends notifications. Which alerts each
// channel hears about is up to the channel.
type NotificationSettings struct {
	// RemindHours is how many hours after the last notification about an
	// alert Roost sends another, while the alert is open and nobody has
	// acknowledged or snoozed it. 0 sends none.
	RemindHours int `json:"remindHours"`
	// Quiet holds notifications back at night.
	Quiet QuietHours `json:"quiet"`
}

// QuietHours are the hours when Roost holds notifications back. Those that
// are still due when the hours end are sent then.
type QuietHours struct {
	Enabled bool `json:"enabled"`
	// Start and End are times of the day, such as "22:00" and "07:00". The
	// hours run from Start until End, through midnight if End is earlier.
	Start string `json:"start"`
	End   string `json:"end"`
	// Timezone is the name of the time zone the times are in, such as
	// "Europe/Berlin". It's empty for the one Roost's machine is in.
	Timezone string `json:"timezone"`
	// AllowCritical sends notifications about critical alerts during the
	// quiet hours too.
	AllowCritical bool `json:"allowCritical"`
}

// SettingsView is the response to GET /api/settings and to the request that
// changes them. Only admins can see or change them.
type SettingsView struct {
	Settings Settings `json:"settings"`
	// Defaults are the settings of a new Roost, so the app can say what
	// resetting one would do.
	Defaults Settings `json:"defaults"`
	// Tailscale is how Roost joins the tailnet. It's set when Roost starts.
	Tailscale TailscaleMode `json:"tailscale"`
}

// TailscaleMode is how Roost joins the tailnet.
type TailscaleMode string

// The ways Roost can join the tailnet.
const (
	// TailscaleModeEmbedded runs a Tailscale node inside Roost, so Roost is
	// its own machine on the tailnet.
	TailscaleModeEmbedded TailscaleMode = "embedded"
	// TailscaleModeHost uses the Tailscale already running on Roost's
	// machine.
	TailscaleModeHost TailscaleMode = "host"
)

// ChangeSettings is the body of PATCH /api/settings. Each setting it has
// changes, and the ones it leaves out stay as they are, so two admins who
// change different settings don't undo each other, and a request can say
// just what it means. The fields are those of Settings.
type ChangeSettings struct {
	General    *ChangeGeneral    `json:"general,omitempty"`
	Thresholds *ChangeThresholds `json:"thresholds,omitempty"`
	Rules      *ChangeRules      `json:"rules,omitempty"`
	Retention  *ChangeRetention  `json:"retention,omitempty"`
	// Notifications changes when notifications are sent. The channels they
	// go to are changed by their own requests.
	Notifications *ChangeNotifications `json:"notifications,omitempty"`
	// WatchedUnits replaces the whole list when it's there, even if it's
	// empty.
	WatchedUnits []string `json:"watchedUnits,omitempty"`
}

// ChangeGeneral changes GeneralSettings.
type ChangeGeneral struct {
	Name      *string `json:"name,omitempty"`
	PublicURL *string `json:"publicUrl,omitempty"`
	SetupDone *bool   `json:"setupDone,omitempty"`
}

// ChangeThresholds changes Thresholds.
type ChangeThresholds struct {
	DiskWarn         *float64 `json:"diskWarn,omitempty"`
	DiskCritical     *float64 `json:"diskCritical,omitempty"`
	MemoryWarn       *float64 `json:"memoryWarn,omitempty"`
	CPUWarn          *float64 `json:"cpuWarn,omitempty"`
	CPUWindowMinutes *int     `json:"cpuWindowMinutes,omitempty"`
	OfflineMinutes   *int     `json:"offlineMinutes,omitempty"`
	KeyExpiryDays    *int     `json:"keyExpiryDays,omitempty"`
	CheckFailures    *int     `json:"checkFailures,omitempty"`
}

// ChangeRules turns AlertRules on or off.
type ChangeRules struct {
	Offline      *bool `json:"offline,omitempty"`
	KeyExpiry    *bool `json:"keyExpiry,omitempty"`
	AgentStopped *bool `json:"agentStopped,omitempty"`
	Disk         *bool `json:"disk,omitempty"`
	Memory       *bool `json:"memory,omitempty"`
	CPU          *bool `json:"cpu,omitempty"`
	Checks       *bool `json:"checks,omitempty"`
	Containers   *bool `json:"containers,omitempty"`
	Units        *bool `json:"units,omitempty"`
	Collectors   *bool `json:"collectors,omitempty"`
}

// ChangeRetention changes RetentionSettings.
type ChangeRetention struct {
	ActivityDays *int `json:"activityDays,omitempty"`
	HistoryDays  *int `json:"historyDays,omitempty"`
	ChecksDays   *int `json:"checksDays,omitempty"`
}

// ChangeNotifications changes NotificationSettings.
type ChangeNotifications struct {
	RemindHours *int         `json:"remindHours,omitempty"`
	Quiet       *ChangeQuiet `json:"quiet,omitempty"`
}

// ChangeQuiet changes QuietHours.
type ChangeQuiet struct {
	Enabled       *bool   `json:"enabled,omitempty"`
	Start         *string `json:"start,omitempty"`
	End           *string `json:"end,omitempty"`
	Timezone      *string `json:"timezone,omitempty"`
	AllowCritical *bool   `json:"allowCritical,omitempty"`
}

// InstalledServices is the response to GET /api/settings/units: the services
// on the machine Roost runs on, to pick the ones to watch from. Only admins
// can see it.
type InstalledServices struct {
	// Available is false if Roost can't ask systemd on its machine, which
	// is the case on a machine that doesn't run it.
	Available bool `json:"available"`
	// Services are the system's first, by name.
	Services []InstalledService `json:"services"`
}

// InstalledService is a systemd service that the machine has.
type InstalledService struct {
	// Key is how the settings name it: its name, or "user:" and its name for
	// a unit of the user Roost runs as.
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Scope       UnitScope `json:"scope"`
	Description string    `json:"description,omitempty"`
	// Active is systemd's state for it, such as "active" or "failed", or
	// empty if it is installed and not loaded.
	Active string `json:"active,omitempty"`
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
	// name. GET /api/devices/{id}/docker/containers has any machine's.
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
	// Name is what Roost is called, which the app shows in its header. Admins
	// can change it in the settings.
	Name string `json:"name"`
	// SetupDone is whether an admin finished or skipped the first-run setup.
	// Until then, the app shows it to admins.
	SetupDone bool `json:"setupDone"`
	// Version is the hub's build version.
	Version string `json:"version"`
	// DNSName is the MagicDNS name of Roost's node, such as
	// roost.example.ts.net.
	DNSName string `json:"dnsName"`
	// Tailnet is the tailnet's name, such as example.com. Only admins get
	// it: a personal tailnet is named after its owner's email address.
	Tailnet string `json:"tailnet" roost:"secret"`
	// Thresholds are when a device needs attention, unless it has some of
	// its own, as the settings have them now.
	Thresholds Thresholds `json:"thresholds"`
	// Demo is whether this Roost runs a demo, whose tailnet is made up. It
	// can't open terminals or files, or change Docker, and the app says so.
	Demo bool `json:"demo"`
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
	// TailscaleSSH reports whether the device runs Tailscale SSH, which Roost
	// can log in to without a key, if the tailnet policy lets it.
	TailscaleSSH bool `json:"tailscaleSsh,omitempty"`
	// Docker reports whether Roost can read the Docker on the device: on the
	// machine Roost runs on, if it has a Docker socket, and on another, if
	// Roost has SSH access and found one there. Viewers get it too, since
	// they can see Docker's overview, but not how Roost logs in.
	Docker bool `json:"docker"`
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
	// Thresholds are the thresholds an admin set for this device in place of
	// the settings'.
	Thresholds DeviceThresholds `json:"thresholds"`
	// Notes are anything admins want to remember about the device. Viewers
	// can read them too.
	Notes string `json:"notes"`
}

// DeviceThresholds are thresholds that an admin set for one device. One that
// is left out is the settings'.
type DeviceThresholds struct {
	// DiskWarn, DiskCritical, MemoryWarn and CPUWarn are in percent.
	DiskWarn     *float64 `json:"diskWarn,omitempty"`
	DiskCritical *float64 `json:"diskCritical,omitempty"`
	MemoryWarn   *float64 `json:"memoryWarn,omitempty"`
	CPUWarn      *float64 `json:"cpuWarn,omitempty"`
	// OfflineMinutes is how long the device can be offline before an alert.
	OfflineMinutes *int `json:"offlineMinutes,omitempty"`
}

// SetDeviceSettings is the body of PUT /api/devices/{id}/settings, which
// replaces a device's settings. A field that's left out gets its default.
type SetDeviceSettings struct {
	DisplayName string     `json:"displayName,omitempty"`
	Kind        DeviceKind `json:"kind,omitempty"`
	// AlertOffline is on if it's left out, except for phones and tablets.
	AlertOffline *bool `json:"alertOffline,omitempty"`
	// Thresholds are the device's own, and none if it's left out.
	Thresholds DeviceThresholds `json:"thresholds,omitzero"`
	Notes      string           `json:"notes,omitempty"`
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

// AgentRun is the response to POST /api/devices/{id}/agent/install and
// POST /api/devices/{id}/agent/uninstall, which run the script over SSH on
// the device. A script that fails is still a 200, with OK false.
type AgentRun struct {
	OK bool `json:"ok"`
	// Output is the end of what the script printed, errors included.
	Output string `json:"output"`
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
	// EventKindSettings is a change an admin made to Roost's settings.
	EventKindSettings EventKind = "settings"
	// EventKindSSH is SSH access to a device that an admin set up, changed
	// or removed, or a host key that changed.
	EventKindSSH EventKind = "ssh"
	// EventKindTerminal is a terminal an admin opened, or a tmux session
	// they ended.
	EventKindTerminal EventKind = "terminal"
	// EventKindFiles is a change an admin made to a device's files, or a
	// folder they downloaded.
	EventKindFiles EventKind = "files"
	// EventKindNotification is a notification Roost sent to a channel, or
	// couldn't, and a channel an admin added, changed or deleted.
	EventKindNotification EventKind = "notification"
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
	// Method is how Roost logged in the last time it did, while the State is
	// SSHStateOK.
	Method SSHMethod `json:"method,omitempty"`
	// Policy is the change to the tailnet policy that Tailscale SSH on the
	// machine needs before it lets Roost in, while the State is
	// SSHStateFailing and the policy is why.
	Policy *SSHPolicy `json:"policy,omitempty"`
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

// SSHMethod says how Roost logged in to a device.
type SSHMethod string

// The ways Roost logs in.
const (
	// SSHMethodKey is a login with Roost's key, which an admin put in the
	// machine's authorized_keys.
	SSHMethodKey SSHMethod = "key"
	// SSHMethodTailscale is a login to the machine's Tailscale SSH, which
	// let Roost in without a key.
	SSHMethodTailscale SSHMethod = "tailscale"
)

// SSHPolicy is what the tailnet policy needs before a device's Tailscale SSH
// lets Roost in.
type SSHPolicy struct {
	// Refusal says what the policy does now.
	Refusal SSHRefusal `json:"refusal"`
	// Snippet is a rule for the "ssh" section of the policy that lets Roost
	// log in as the user an admin set up, without a browser check.
	Snippet string `json:"snippet"`
}

// SSHRefusal says why Tailscale SSH turned Roost away.
type SSHRefusal string

// The ways the tailnet policy can turn Roost away.
const (
	// SSHRefusalCheck means the rule that covers Roost has the action
	// "check", which asks for a visit to a URL that Roost can't make.
	SSHRefusalCheck SSHRefusal = "check"
	// SSHRefusalNode means no rule lets Roost SSH to the machine.
	SSHRefusalNode SSHRefusal = "node"
	// SSHRefusalUser means a rule lets Roost in, but not as the user an
	// admin set up.
	SSHRefusalUser SSHRefusal = "user"
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

// FileType says what a file is.
type FileType string

// The types of file.
const (
	FileTypeFile    FileType = "file"
	FileTypeDir     FileType = "dir"
	FileTypeSymlink FileType = "symlink"
	FileTypeOther   FileType = "other"
)

// FileEntry is a file, folder or link on a device.
type FileEntry struct {
	Name string `json:"name"`
	// Path is absolute on the device.
	Path string `json:"path"`
	// Type is what the entry itself is, without following a link.
	Type FileType `json:"type"`
	// TargetType is what a link points to. It's left out for a broken link
	// and for what isn't a link.
	TargetType FileType `json:"targetType,omitempty"`
	// Target is where a link points, as written.
	Target string `json:"target,omitempty"`
	// Size is in bytes. For a link to a file, it's the file's.
	Size int64 `json:"size"`
	// ModifiedAt is whole seconds, as SFTP reports it.
	ModifiedAt time.Time `json:"modifiedAt"`
	// Mode is the permissions as ls shows them, such as "drwxr-xr-x".
	Mode string `json:"mode"`
	// Owner and Group are names, or numbers where the device has none.
	Owner string `json:"owner"`
	Group string `json:"group"`
}

// FileListing is the response to GET /api/devices/{id}/files/list?path=: a
// folder and what's in it, in no order. Without a path, it's the home folder
// of the user Roost logs in as.
type FileListing struct {
	// Path is the folder with links resolved, and Parent the folder above
	// it, which the root doesn't have.
	Path   string `json:"path"`
	Parent string `json:"parent,omitempty"`
	// Home is the home folder of the user Roost logs in as.
	Home    string      `json:"home"`
	Entries []FileEntry `json:"entries"`
	// Space is the room on the file system that holds the folder, if the
	// device says.
	Space *FileSpace `json:"space,omitempty"`
}

// FileSpace is the free and total bytes of a file system.
type FileSpace struct {
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

// FileStat is the response to GET /api/devices/{id}/files/stat?path=.
type FileStat struct {
	Entry FileEntry `json:"entry"`
	Home  string    `json:"home"`
}

// FileSearch is the response to GET
// /api/devices/{id}/files/search?path=&q=&limit=&depth=: the files under a
// folder whose names match. A plain word matches anywhere in a name, ignoring
// case, and one with * ? or [ is a pattern. Limit is 300 by default and 1000
// at most, and depth 12 and 30.
type FileSearch struct {
	Root    string      `json:"root"`
	Query   string      `json:"query"`
	Entries []FileEntry `json:"entries"`
	// Truncated says there were more than the limit, or the search ran out
	// of its 20 seconds.
	Truncated bool `json:"truncated"`
}

// ConflictMode says what to do when a file moved, copied or uploaded to a
// folder has the name of one that's there.
type ConflictMode string

// The ways to settle a conflict. A folder is never replaced.
const (
	// ConflictModeFail refuses, with a 409. It's the default.
	ConflictModeFail ConflictMode = "fail"
	// ConflictModeOverwrite replaces the file.
	ConflictModeOverwrite ConflictMode = "overwrite"
	// ConflictModeRename gives the new file a free name, such as "a (1).txt".
	ConflictModeRename ConflictMode = "rename"
)

// FileBulk is the response to the requests that change several paths at once.
// One failing doesn't stop the rest, but if there's only one path, its failure
// is the response.
type FileBulk struct {
	Done   int           `json:"done"`
	Failed []FileFailure `json:"failed"`
	// Paths are where the ones that worked ended up, in order. A copy or move
	// with ConflictModeRename may name them differently.
	Paths []string `json:"paths"`
}

// FileFailure is a path that a request couldn't change, and why.
type FileFailure struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// FileUpload is the response to PUT
// /api/devices/{id}/files/upload?dir=&name=&conflict=, whose body is the
// file. The name may be a relative path from a folder upload, such as
// "photos/2024/a.jpg", whose folders are made.
type FileUpload struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// ArchiveFiles is the body of POST /api/devices/{id}/files/archive, which
// parks a selection of names in a folder, for a selection too big for a URL.
// GET /api/devices/{id}/files/archive?token= downloads it as a .tar.gz, for
// five minutes, and so does GET ?dir=&name=&name= for a short one.
type ArchiveFiles struct {
	Dir   string   `json:"dir"`
	Names []string `json:"names"`
}

// ArchiveToken is the response to ArchiveFiles.
type ArchiveToken struct {
	Token string `json:"token"`
}

// CheckNames is the body of POST /api/devices/{id}/files/exists, which asks
// which names are in a folder already. A name may be a relative path.
type CheckNames struct {
	Dir   string   `json:"dir"`
	Names []string `json:"names"`
}

// ExistingNames is the response to CheckNames.
type ExistingNames struct {
	Existing []string `json:"existing"`
}

// MakeFolder is the body of POST /api/devices/{id}/files/mkdir, and answers
// with the new folder's FileEntry.
type MakeFolder struct {
	Dir  string `json:"dir"`
	Name string `json:"name"`
}

// RenameFile is the body of POST /api/devices/{id}/files/rename, and answers
// with the FileEntry as it is then.
type RenameFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// MoveFiles is the body of POST /api/devices/{id}/files/move and .../copy,
// which put paths in the folder Dest, and answer with a FileBulk.
type MoveFiles struct {
	Paths []string `json:"paths"`
	Dest  string   `json:"dest"`
	// Conflict is ConflictModeFail if it's left out.
	Conflict ConflictMode `json:"conflict,omitempty"`
}

// DeleteFiles is the body of POST /api/devices/{id}/files/delete, which
// deletes the paths and what's in the folders among them, and answers with a
// FileBulk. A link goes without what it points to.
type DeleteFiles struct {
	Paths []string `json:"paths"`
}

// DockerOverview is the response to GET /api/devices/{id}/docker/overview:
// the Docker daemon on a device, and what it holds.
type DockerOverview struct {
	// Name is the machine's name as Docker knows it.
	Name    string `json:"name"`
	Version string `json:"version"`
	// APIVersion is the Engine API version the daemon speaks.
	APIVersion string `json:"apiVersion,omitempty"`
	OS         string `json:"os"`
	// Kernel is the kernel's version. Only admins get it, as with a device's.
	Kernel string `json:"kernel" roost:"secret"`
	Arch   string `json:"arch"`
	// CPUs and MemTotal are the machine's CPU cores and bytes of memory.
	CPUs          int    `json:"cpus"`
	MemTotal      int64  `json:"memTotal"`
	StorageDriver string `json:"storageDriver"`
	LoggingDriver string `json:"loggingDriver"`
	// Cgroup is the cgroup version and driver, such as "v2 (systemd)".
	Cgroup string `json:"cgroup,omitempty"`
	// RootDir is where Docker keeps its data. Only admins get it.
	RootDir    string       `json:"rootDir" roost:"secret"`
	Containers DockerCounts `json:"containers"`
	Images     int          `json:"images"`
	Warnings   []string     `json:"warnings,omitempty" roost:"secret"`
	// Disk is the disk space Docker uses. It's null until the daemon has
	// worked it out, which takes a while on a big machine, so ask again soon.
	Disk *DockerDisk `json:"disk" tstype:"DockerDisk | null,required"`
}

// DockerCounts is how many containers there are, by what they're doing.
type DockerCounts struct {
	Total   int `json:"total"`
	Running int `json:"running"`
	Paused  int `json:"paused"`
	Stopped int `json:"stopped"`
}

// DockerDisk is the disk space Docker uses, in bytes.
type DockerDisk struct {
	Images     DockerDiskGroup `json:"images"`
	Containers DockerDiskGroup `json:"containers"`
	Volumes    DockerDiskGroup `json:"volumes"`
	BuildCache DockerDiskGroup `json:"buildCache"`
	Total      int64           `json:"total"`
	// At is when the daemon worked it out.
	At time.Time `json:"at"`
}

// DockerDiskGroup is the disk space one kind of thing uses.
type DockerDiskGroup struct {
	Size int64 `json:"size"`
	// Count is how many there are, and Active how many are in use.
	Count  int64 `json:"count"`
	Active int64 `json:"active"`
	// Reclaimable is the space that removing what isn't in use would free.
	Reclaimable int64 `json:"reclaimable"`
}

// DockerStats is what a container uses at a moment. CPU is a percentage of
// one CPU, as `docker stats` says it. A value is null if the container's
// sample can't say it, such as a rate from its first sample.
type DockerStats struct {
	// TS is when Docker took the sample, in Unix milliseconds.
	TS  int64    `json:"ts"`
	CPU *float64 `json:"cpu" tstype:"number | null,required"`
	// MemUsed is the bytes of memory in use, without the page cache that the
	// kernel can drop. MemLimit is the container's limit, or the machine's
	// memory if it has none.
	MemUsed  *float64 `json:"memUsed" tstype:"number | null,required"`
	MemLimit *float64 `json:"memLimit" tstype:"number | null,required"`
	// NetRx and NetTx are the bytes per second received and sent, and
	// NetRxTotal and NetTxTotal the bytes since it started.
	NetRx      *float64 `json:"netRx" tstype:"number | null,required"`
	NetTx      *float64 `json:"netTx" tstype:"number | null,required"`
	NetRxTotal *float64 `json:"netRxTotal" tstype:"number | null,required"`
	NetTxTotal *float64 `json:"netTxTotal" tstype:"number | null,required"`
	// BlkRead and BlkWrite are the bytes read from and written to disks since
	// it started, and BlkReadRate and BlkWriteRate the bytes per second.
	BlkRead      *float64 `json:"blkRead" tstype:"number | null,required"`
	BlkWrite     *float64 `json:"blkWrite" tstype:"number | null,required"`
	BlkReadRate  *float64 `json:"blkReadRate" tstype:"number | null,required"`
	BlkWriteRate *float64 `json:"blkWriteRate" tstype:"number | null,required"`
	// Pids is how many processes run in it.
	Pids *float64 `json:"pids" tstype:"number | null,required"`
	// CPUs is how many CPUs it can see: 100% is one of them.
	CPUs int `json:"cpus"`
}

// DockerContainerList is the response to GET
// /api/devices/{id}/docker/containers: every container on the device, by
// name.
type DockerContainerList struct {
	FetchedAt  time.Time            `json:"fetchedAt"`
	Containers []DockerContainerRow `json:"containers"`
}

// DockerContainerRow is a container in the list.
type DockerContainerRow struct {
	// ID is Docker's full ID.
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Image   string         `json:"image"`
	ImageID string         `json:"imageId"`
	Created time.Time      `json:"created"`
	State   ContainerState `json:"state"`
	// Status says it in Docker's words, such as "Up 2 hours (healthy)".
	Status string `json:"status"`
	// Health is "healthy", "unhealthy" or "starting", or missing if it has
	// no health check.
	Health     string     `json:"health,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	// RestartCount is how many times Docker restarted it.
	RestartCount int `json:"restartCount"`
	// ExitCode is how it last exited, if it isn't running.
	ExitCode *int `json:"exitCode,omitempty"`
	// OOMKilled reports whether it was killed for using too much memory.
	OOMKilled bool `json:"oomKilled,omitempty"`
	// Error is Docker's error from starting it, if it had one.
	Error         string `json:"error,omitempty"`
	RestartPolicy string `json:"restartPolicy,omitempty"`
	// Project and Service are its Docker Compose project and service, if
	// it has them.
	Project string          `json:"project,omitempty"`
	Service string          `json:"service,omitempty"`
	Ports   []ContainerPort `json:"ports"`
	// Networks are the names of the networks it is on.
	Networks []string `json:"networks"`
	// Mounts is how many mounts it has.
	Mounts int `json:"mounts"`
	// MemLimitSet reports whether it has a memory limit of its own. If it
	// hasn't, the limit in its Stats is the machine's memory.
	MemLimitSet bool `json:"memLimitSet"`
	// Stats is what it uses now, if it's running.
	Stats *DockerStats `json:"stats" tstype:"DockerStats | null,required"`
}

// DockerContainerDetail is the response to GET
// /api/devices/{id}/docker/containers/{id}: one container, inspected. What
// can carry a secret is only for admins: environment variables, commands,
// labels that look like credentials, and the output of health checks.
type DockerContainerDetail struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Image is the image as it was named when the container was made, and
	// ImageID its ID.
	Image        string             `json:"image"`
	ImageID      string             `json:"imageId"`
	ImageDigest  string             `json:"imageDigest,omitempty"`
	ImageCreated *time.Time         `json:"imageCreated,omitempty"`
	ImageSize    *int64             `json:"imageSize,omitempty"`
	Created      time.Time          `json:"created"`
	Platform     string             `json:"platform,omitempty"`
	Driver       string             `json:"driver,omitempty"`
	State        DockerState        `json:"state"`
	RestartCount int                `json:"restartCount"`
	Health       *DockerHealth      `json:"health,omitempty"`
	Healthcheck  *DockerHealthcheck `json:"healthcheck,omitempty"`
	// Path and Args are what it runs, and Cmd and Entrypoint what it was
	// told to. A command line can have credentials in it, so admins get
	// them all.
	Path       string   `json:"path" roost:"secret"`
	Args       []string `json:"args,omitempty" roost:"secret"`
	Cmd        []string `json:"cmd,omitempty" roost:"secret"`
	Entrypoint []string `json:"entrypoint,omitempty" roost:"secret"`
	WorkingDir string   `json:"workingDir,omitempty"`
	User       string   `json:"user,omitempty"`
	Hostname   string   `json:"hostname,omitempty"`
	TTY        bool     `json:"tty"`
	// Env are its environment variables. Only admins get them.
	Env []DockerEnvVar `json:"env,omitempty" roost:"secret"`
	// Labels are its labels, except those that look like credentials, which
	// are in SecretLabels, for admins.
	Labels       map[string]string `json:"labels"`
	SecretLabels map[string]string `json:"secretLabels,omitempty" roost:"secret"`
	// Project and Service are its Docker Compose project and service, and
	// ComposeDir and ComposeFiles where the project's files are.
	Project       string               `json:"project,omitempty"`
	Service       string               `json:"service,omitempty"`
	ComposeDir    string               `json:"composeDir,omitempty"`
	ComposeFiles  string               `json:"composeFiles,omitempty"`
	RestartPolicy *DockerRestartPolicy `json:"restartPolicy,omitempty"`
	AutoRemove    bool                 `json:"autoRemove"`
	Resources     DockerResources      `json:"resources"`
	Security      DockerSecurity       `json:"security"`
	LogDriver     string               `json:"logDriver,omitempty"`
	NetworkMode   string               `json:"networkMode,omitempty"`
	Ports         []ContainerPort      `json:"ports"`
	// Exposed are the ports the image says it listens on, such as 80/tcp.
	Exposed    []string                 `json:"exposed"`
	Networks   []DockerContainerNetwork `json:"networks"`
	Mounts     []DockerMount            `json:"mounts"`
	DNS        []string                 `json:"dns"`
	ExtraHosts []string                 `json:"extraHosts"`
}

// DockerState is what a container is doing, in detail.
type DockerState struct {
	// Status is "created", "running", "paused", "restarting", "removing",
	// "exited" or "dead".
	Status     string `json:"status"`
	Running    bool   `json:"running"`
	Paused     bool   `json:"paused"`
	Restarting bool   `json:"restarting"`
	OOMKilled  bool   `json:"oomKilled"`
	Dead       bool   `json:"dead"`
	// Pid is its main process's ID on the machine, if it runs.
	Pid int `json:"pid,omitempty"`
	// ExitCode is how it last exited, if it isn't running.
	ExitCode   *int       `json:"exitCode,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// DockerHealth is the result of a container's health checks so far.
type DockerHealth struct {
	// Status is "healthy", "unhealthy" or "starting".
	Status string `json:"status"`
	// FailingStreak is how many checks in a row failed.
	FailingStreak int `json:"failingStreak"`
	// Log is the latest checks, oldest first.
	Log []DockerHealthResult `json:"log"`
}

// DockerHealthResult is one run of a container's health check.
type DockerHealthResult struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	ExitCode int       `json:"exitCode"`
	// Output is what the check printed. Only admins get it.
	Output string `json:"output" roost:"secret"`
}

// DockerHealthcheck is how Docker checks a container's health. The
// intervals are in milliseconds.
type DockerHealthcheck struct {
	// Test is the command, such as ["CMD-SHELL", "curl -f localhost"]. Only
	// admins get it.
	Test        []string `json:"test,omitempty" roost:"secret"`
	Interval    int64    `json:"interval,omitempty"`
	Timeout     int64    `json:"timeout,omitempty"`
	StartPeriod int64    `json:"startPeriod,omitempty"`
	Retries     int      `json:"retries,omitempty"`
}

// DockerEnvVar is an environment variable of a container.
type DockerEnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// DockerRestartPolicy says when Docker restarts a container.
type DockerRestartPolicy struct {
	// Name is "no", "always", "unless-stopped" or "on-failure".
	Name       string `json:"name"`
	MaxRetries int    `json:"maxRetries,omitempty"`
}

// DockerResources are the limits on a container. A limit is missing if there
// is none.
type DockerResources struct {
	// Memory, MemoryReservation and MemorySwap are in bytes. A MemorySwap of
	// -1 means unlimited.
	Memory            int64   `json:"memory,omitempty"`
	MemoryReservation int64   `json:"memoryReservation,omitempty"`
	MemorySwap        int64   `json:"memorySwap,omitempty"`
	CPUs              float64 `json:"cpus,omitempty"`
	CPUShares         int64   `json:"cpuShares,omitempty"`
	Cpuset            string  `json:"cpuset,omitempty"`
	PidsLimit         int64   `json:"pidsLimit,omitempty"`
	ShmSize           int64   `json:"shmSize,omitempty"`
}

// DockerSecurity is how a container is confined.
type DockerSecurity struct {
	Privileged     bool     `json:"privileged"`
	ReadonlyRootfs bool     `json:"readonlyRootfs"`
	CapAdd         []string `json:"capAdd"`
	CapDrop        []string `json:"capDrop"`
	SecurityOpt    []string `json:"securityOpt"`
}

// DockerContainerNetwork is a container's place on a network.
type DockerContainerNetwork struct {
	Name        string   `json:"name"`
	ID          string   `json:"id"`
	IP          string   `json:"ip,omitempty"`
	PrefixLen   int      `json:"prefixLen,omitempty"`
	Gateway     string   `json:"gateway,omitempty"`
	MAC         string   `json:"mac,omitempty"`
	IPv6        string   `json:"ipv6,omitempty"`
	IPv6Gateway string   `json:"ipv6Gateway,omitempty"`
	Aliases     []string `json:"aliases"`
	DNSNames    []string `json:"dnsNames"`
}

// DockerMount is something mounted into a container.
type DockerMount struct {
	// Type is "volume", "bind" or "tmpfs".
	Type string `json:"type"`
	// Name is a volume's name.
	Name        string `json:"name,omitempty"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
	RW          bool   `json:"rw"`
	Propagation string `json:"propagation,omitempty"`
	Driver      string `json:"driver,omitempty"`
	// Size is a volume's size in bytes, if the daemon has measured it.
	Size *int64 `json:"size,omitempty"`
}

// DockerProcesses is the response to GET
// /api/devices/{id}/docker/containers/{id}/top: a container's processes, as
// ps writes them. Only admins get it.
type DockerProcesses struct {
	Titles    []string   `json:"titles"`
	Processes [][]string `json:"processes"`
}

// DockerRef is a container that uses a volume, image or network.
type DockerRef struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Running bool   `json:"running"`
}

// DockerVolume is a volume.
type DockerVolume struct {
	Name       string     `json:"name"`
	Driver     string     `json:"driver"`
	Mountpoint string     `json:"mountpoint"`
	Created    *time.Time `json:"created,omitempty"`
	Scope      string     `json:"scope"`
	// Labels are its labels, except those that look like credentials, which
	// are in SecretLabels, for admins.
	Labels       map[string]string `json:"labels"`
	SecretLabels map[string]string `json:"secretLabels,omitempty" roost:"secret"`
	// Project is its Docker Compose project, if it has one.
	Project string `json:"project,omitempty"`
	// Size is in bytes, if the daemon has measured it.
	Size   *int64      `json:"size,omitempty"`
	UsedBy []DockerRef `json:"usedBy"`
}

// DockerImage is an image.
type DockerImage struct {
	ID      string    `json:"id"`
	Tags    []string  `json:"tags"`
	Digests []string  `json:"digests"`
	Created time.Time `json:"created"`
	Size    int64     `json:"size"`
	// Dangling reports whether it has no tag.
	Dangling bool        `json:"dangling"`
	UsedBy   []DockerRef `json:"usedBy"`
}

// DockerNetwork is a network.
type DockerNetwork struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Driver     string         `json:"driver"`
	Scope      string         `json:"scope"`
	Internal   bool           `json:"internal"`
	Attachable bool           `json:"attachable"`
	IPv6       bool           `json:"ipv6"`
	Created    *time.Time     `json:"created,omitempty"`
	Subnets    []DockerSubnet `json:"subnets"`
	// Project is its Docker Compose project, if it has one.
	Project    string                   `json:"project,omitempty"`
	Containers []DockerNetworkContainer `json:"containers"`
}

// DockerSubnet is a range of addresses on a network.
type DockerSubnet struct {
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway,omitempty"`
}

// DockerNetworkContainer is a container on a network, with its address there.
type DockerNetworkContainer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Running bool   `json:"running"`
	IP      string `json:"ip,omitempty"`
	MAC     string `json:"mac,omitempty"`
}

// DockerHistory is the response to GET
// /api/devices/{id}/docker/containers/{id}/history?range=, where the range
// is 1h, 6h, 24h or 7d: what the container used, averaged over each step.
// Roost records it once a minute, by the container's name.
type DockerHistory struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Step is the time each point sums up, in milliseconds.
	Step int64 `json:"step"`
	// CPU is in percent of one CPU, Mem in bytes, and the rest in bytes per
	// second.
	CPU      []Point `json:"cpu"`
	Mem      []Point `json:"mem"`
	NetRx    []Point `json:"netRx"`
	NetTx    []Point `json:"netTx"`
	BlkRead  []Point `json:"blkRead"`
	BlkWrite []Point `json:"blkWrite"`
}

// DockerActionResult is the response to POST
// /api/devices/{id}/docker/containers/{id}/{action}, where the action is
// start, stop or restart.
type DockerActionResult struct {
	// Changed is false if the container already was as asked, such as a
	// started one that was told to start.
	Changed bool `json:"changed"`
}

// The events of GET /api/devices/{id}/docker/containers/{id}/logs, which is
// a stream of server-sent events: one "start", then any number of "lines",
// and then an "end" or a "failure".

// DockerLogStart is the first event of a container's logs.
type DockerLogStart struct {
	// TTY reports whether the container has a terminal, which merges its
	// standard output and error.
	TTY     bool `json:"tty"`
	Running bool `json:"running"`
}

// DockerLogLine is a line of a container's logs.
type DockerLogLine struct {
	// S is 1 for standard output and 2 for standard error.
	S int `json:"s"`
	// T is the RFC 3339 time Docker stamped the line with, if it did.
	T string `json:"t,omitempty"`
	M string `json:"m"`
}

// DockerStreamEnd is the last event of a stream that ended as it should.
type DockerStreamEnd struct {
	Reason string `json:"reason"`
}

// DockerStreamFailure is the last event of a stream that Docker or the
// connection to it broke.
type DockerStreamFailure struct {
	Message string `json:"message"`
}

// ChannelKind is a sort of place that Roost sends notifications to.
type ChannelKind string

// The kinds of NotificationChannel.
const (
	// ChannelKindNtfy publishes to a topic on ntfy.sh or on an ntfy server
	// of one's own.
	ChannelKindNtfy ChannelKind = "ntfy"
	// ChannelKindDiscord posts to a Discord channel through a webhook.
	ChannelKindDiscord ChannelKind = "discord"
	// ChannelKindSlack posts to a Slack channel through an incoming webhook.
	ChannelKindSlack ChannelKind = "slack"
	// ChannelKindTelegram sends a message from a Telegram bot.
	ChannelKindTelegram ChannelKind = "telegram"
	// ChannelKindEmail sends mail through an SMTP server.
	ChannelKindEmail ChannelKind = "email"
	// ChannelKindWebhook posts JSON to any web address.
	ChannelKindWebhook ChannelKind = "webhook"
)

// NotificationChannels is the response to GET /api/notifications/channels.
// Only admins can see them.
type NotificationChannels struct {
	Channels []NotificationChannel `json:"channels"`
}

// NotificationChannel is a place where Roost sends notifications. It leaves
// out its secrets, such as a webhook's address or a password, and says which
// of them are saved.
type NotificationChannel struct {
	ID      int64       `json:"id"`
	Name    string      `json:"name"`
	Kind    ChannelKind `json:"kind"`
	Enabled bool        `json:"enabled"`
	// MinSeverity is the least severe alert it hears about.
	MinSeverity AlertSeverity `json:"minSeverity"`
	// OnResolve is whether it also hears when an alert resolves.
	OnResolve bool          `json:"onResolve"`
	Config    ChannelConfig `json:"config"`
	// Secrets are the secret fields that have a value: "url", "token" or
	// "password". Roost never sends their values.
	Secrets []string `json:"secrets"`
	// Summary says where the channel sends, without a secret, such as
	// "ntfy.sh/roost-alerts".
	Summary string `json:"summary"`
	// Last is the latest time Roost sent to it since Roost started.
	Last *ChannelDelivery `json:"last,omitempty"`
}

// ChannelConfig is what a channel needs to send, except its secrets. Each
// kind uses some of the fields.
type ChannelConfig struct {
	// Server is the address of an ntfy server. It's empty for ntfy.sh.
	Server string `json:"server,omitempty"`
	// Topic is the ntfy topic.
	Topic string `json:"topic,omitempty"`
	// ChatID is the Telegram chat to send to.
	ChatID string `json:"chatId,omitempty"`
	// Host, Port and Security say how to reach the mail server. A Port of 0
	// is the one Security uses, and Security is starttls, tls or none.
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Security string `json:"security,omitempty"`
	// Username signs in to the mail server or the ntfy server.
	Username string `json:"username,omitempty"`
	// From and To are the addresses mail comes from and goes to.
	From string   `json:"from,omitempty"`
	To   []string `json:"to,omitempty"`
}

// ChannelDelivery is what happened when Roost last sent to a channel.
type ChannelDelivery struct {
	At time.Time `json:"at"`
	// Event is why it sent: opened, escalated, reminder, resolved or test.
	Event string `json:"event"`
	OK    bool   `json:"ok"`
	// Error says what went wrong, if something did.
	Error string `json:"error,omitempty"`
}

// ChannelInput is the body of POST /api/notifications/channels, which adds a
// channel, and of PUT /api/notifications/channels/{id}, which replaces one. A
// channel's kind can't change. Both answer with the NotificationChannel.
type ChannelInput struct {
	Name string      `json:"name"`
	Kind ChannelKind `json:"kind"`
	// Enabled is whether Roost sends to it.
	Enabled bool `json:"enabled"`
	// MinSeverity is the least severe alert it hears about: info, warning or
	// critical.
	MinSeverity AlertSeverity      `json:"minSeverity"`
	OnResolve   bool               `json:"onResolve"`
	Config      ChannelConfigInput `json:"config"`
}

// ChannelConfigInput is the ChannelConfig of a request, with the secrets. A
// secret that's left out stays as it is, which is how a channel is changed
// without sending its secrets back, and an empty one takes it away.
type ChannelConfigInput struct {
	ChannelConfig `tstype:",extends"`
	// URL is the webhook's address, for Discord, Slack and generic webhooks.
	URL *string `json:"url,omitempty"`
	// Token is an ntfy access token, a Telegram bot's token, or the bearer
	// token that a generic webhook is sent.
	Token *string `json:"token,omitempty"`
	// Password signs in to the mail server or the ntfy server.
	Password *string `json:"password,omitempty"`
}

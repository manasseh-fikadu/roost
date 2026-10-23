package api

import (
	"cmp"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/manasseh-fikadu/roost/agent"
	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/hostmetrics"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// When a device needs attention. The Settings page (#10) makes these
// settings.
const (
	// diskWarn is how full a disk can get, in percent.
	diskWarn = 85
	// memoryWarn is how much memory can be used, in percent.
	memoryWarn = 92
	// cpuWarn is how busy the CPUs can be, in percent, on average over
	// hostmetrics.SustainedWindow.
	cpuWarn = 90
	// keyExpiryWarn is how soon before a device's Tailscale key expires to
	// warn about it.
	keyExpiryWarn = 14 * 24 * time.Hour
)

// sources is what a Snapshot is built from.
type sources struct {
	version string
	tailnet tailnet.Snapshot
	// host is what Roost measured of its own machine, or nil if it can't.
	host *hostmetrics.Reading
	// agents holds what agents reported, by device ID, or store.HubID for
	// the machine Roost runs on.
	agents map[string]agents.Report
	// settings holds the settings of the devices that have any, by ID.
	settings map[string]store.DeviceSettings
	// enrolled holds the enrollments of the agents, by the ID of the node
	// each runs on.
	enrolled map[string]store.Enrollment
}

// newSnapshot builds a Snapshot of what Roost knows at now.
func newSnapshot(src sources, now time.Time) Snapshot {
	s := src.tailnet
	out := Snapshot{
		GeneratedAt: now,
		Hub:         Hub{Version: src.version},
		Collectors:  []Collector{newCollector("Tailscale status", s.StatusAt, s.StatusErr)},
		Uplink:      newUplink(s),
		Devices:     []Device{},
	}
	if h := src.host; h != nil {
		var at time.Time
		if h.Sample != nil {
			at = h.Sample.At
		}
		out.Collectors = append(out.Collectors, newCollector("Hub metrics", at, h.Err))
	}
	out.Collectors = append(out.Collectors, newCollector("Tailscale netcheck", s.ConnectivityAt, s.ConnectivityErr))

	st := s.Status
	if st == nil {
		return out
	}
	out.Hub.DNSName, out.Hub.Tailnet = st.Self.DNSName, st.Tailnet
	devices := append([]tailnet.Device{st.Self}, st.Peers...)
	// An agent enrolls from its machine's node, and its reports are kept
	// under the machine's history ID. The machine Roost runs on can have two
	// nodes, which both show its agent.
	enrolled := map[string]store.Enrollment{}
	for i, d := range devices {
		id := historyID(d, i == 0)
		if e, ok := src.enrolled[d.ID]; ok && e.At.After(enrolled[id].At) {
			enrolled[id] = e
		}
	}
	for i, d := range devices {
		dev := deviceInput{device: d, self: i == 0, settings: src.settings[d.ID]}
		// Roost measures the machine it runs on, and on a machine it can't
		// measure, an agent can. In embedded mode, that machine's own node
		// is a peer.
		if dev.self || d.SameMachine {
			dev.host = src.host
		}
		id := historyID(d, dev.self)
		if r, ok := src.agents[id]; ok {
			dev.agent = &r
		}
		if e, ok := enrolled[id]; ok {
			dev.enrollment = &e
		}
		out.Devices = append(out.Devices, newDevice(s, dev, now))
	}
	slices.SortStableFunc(out.Devices, func(a, b Device) int {
		switch {
		case a.Self != b.Self:
			if a.Self {
				return -1
			}
			return 1
		case a.Kind != b.Kind:
			return cmp.Compare(kindOrder(a.Kind), kindOrder(b.Kind))
		}
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.DNSName, b.DNSName),
		)
	})
	return out
}

// newCollector reports on a collector that last worked at lastOK, if that
// isn't zero, and failed with err, if it isn't nil.
func newCollector(name string, lastOK time.Time, err error) Collector {
	c := Collector{Name: name, State: CollectorStateOK, LastOK: timePtr(lastOK)}
	switch {
	case err != nil:
		c.State, c.Error = CollectorStateFailing, err.Error()
	case lastOK.IsZero():
		c.State = CollectorStateWaiting
	}
	return c
}

func newUplink(s tailnet.Snapshot) Uplink {
	c := s.Connectivity
	if c == nil {
		return Uplink{}
	}
	u := Uplink{CheckedAt: timePtr(s.ConnectivityAt), UDP: c.UDP, IPv4: c.IPv4, IPv6: c.IPv6}
	switch {
	case c.PublicIPv4.IsValid():
		u.PublicIP = c.PublicIPv4.String()
	case c.PublicIPv6.IsValid():
		u.PublicIP = c.PublicIPv6.String()
	}
	if d := c.PreferredDERP; d.Code != "" {
		u.DERP = &DERPRegion{Code: d.Code, Name: d.Name, LatencyMs: ms(c.DERPLatency)}
	}
	return u
}

// deviceInput is what a Device is built from.
type deviceInput struct {
	device tailnet.Device
	// self is true for Roost's own node.
	self     bool
	settings store.DeviceSettings
	// host is what Roost measured of the device, if it's the machine Roost
	// runs on.
	host *hostmetrics.Reading
	// agent is what the device's agent last reported, and enrollment is
	// when it enrolled, if it has. They're only used if host is nil.
	agent      *agents.Report
	enrollment *store.Enrollment
}

func newDevice(s tailnet.Snapshot, in deviceInput, now time.Time) Device {
	d := in.device
	ips := make([]string, len(d.IPs))
	for i, ip := range d.IPs {
		ips[i] = ip.String()
	}
	out := Device{
		ID:        d.ID,
		Name:      cmp.Or(in.settings.DisplayName, d.HostName, firstLabel(d.DNSName)),
		HostName:  d.HostName,
		DNSName:   d.DNSName,
		OS:        d.OS,
		IPs:       ips,
		Online:    d.Online,
		LastSeen:  timePtr(d.LastSeen),
		Self:      in.self,
		Path:      newPath(s.PathTo(d, now), d),
		RxBytes:   d.RxBytes,
		TxBytes:   d.TxBytes,
		KeyExpiry: timePtr(d.KeyExpiry),
		Created:   timePtr(d.Created),
		ExitNode:  d.ExitNode,
	}
	var sustained *float64
	switch {
	case in.host != nil:
		h := in.host
		out.MeasuredBy = MeasuredByHub
		if h.Sample != nil {
			out.Metrics = newMetrics(*h.Sample)
			sustained = h.Sample.CPUSustained
		}
		out.Info = newInfo(h.Info)
	case in.agent != nil || in.enrollment != nil:
		out.Agent = &Agent{}
		if e := in.enrollment; e != nil {
			out.Agent.EnrolledAt, out.Agent.EnrolledBy = timePtr(e.At), e.By
		}
		a := in.agent
		if a == nil {
			// It enrolled, and hasn't reported yet.
			break
		}
		out.MeasuredBy = MeasuredByAgent
		out.Metrics = newMetrics(a.Sample)
		if b := a.Battery; b != nil {
			out.Metrics.Battery = &Battery{Percent: b.Percent, Charging: b.Charging, OnAC: b.OnAC}
		}
		out.Info = newInfo(a.Info)
		out.Agent.Version = a.Version
		out.Agent.LastSeen = timePtr(a.ReceivedAt)
		out.Agent.Stale = d.Online && a.Stale(now)
		out.Agent.Outdated = agent.Older(a.Version)
		// The CPU of an agent that stopped is out of date.
		if !out.Agent.Stale {
			sustained = a.Sample.CPUSustained
		}
	}
	model := ""
	if out.Info != nil {
		model = out.Info.Model
	}
	out.Kind = DeviceKind(in.settings.Kind)
	if !validKind(out.Kind) {
		out.Kind = inferKind(d, in.self || d.SameMachine, model)
	}
	out.Settings = DeviceSettings{
		DisplayName:  in.settings.DisplayName,
		Kind:         DeviceKind(in.settings.Kind),
		AlertOffline: alertOffline(d, in.settings),
		Notes:        in.settings.Notes,
	}
	if !validKind(out.Settings.Kind) {
		out.Settings.Kind = ""
	}
	out.Status, out.StatusReason = deviceStatus(out, sustained, now)
	return out
}

func newPath(p tailnet.Path, d tailnet.Device) Path {
	out := Path{Kind: PathKind(p.Kind), HomeDERP: d.Relay, PingedAt: timePtr(p.PingedAt), Unanswered: p.Unanswered}
	switch p.Kind {
	case tailnet.PathDirect:
		out.Endpoint = p.Endpoint
	case tailnet.PathDERP:
		out.DERP = p.Relay
	case tailnet.PathPeerRelay:
		out.PeerRelay = p.Relay
	case tailnet.PathSelf, tailnet.PathOffline:
	}
	if p.Latency > 0 {
		rtt := ms(p.Latency)
		out.RTTMs = &rtt
	}
	return out
}

func newMetrics(s hostmetrics.Sample) *Metrics {
	m := &Metrics{
		At:     s.At,
		CPU:    s.CPU,
		Procs:  s.Procs,
		NetRx:  s.NetRx,
		NetTx:  s.NetTx,
		Temp:   s.Temp,
		Uptime: int64(s.Uptime / time.Second),
	}
	if s.Mem.Total > 0 {
		m.Memory = &Usage{Used: s.Mem.Used, Total: s.Mem.Total}
	}
	if s.Load != nil {
		m.Load = s.Load[:]
	}
	if s.Swap != nil {
		m.Swap = &Usage{Used: s.Swap.Used, Total: s.Swap.Total}
	}
	if s.Disk != nil {
		m.Disk = &Usage{Used: s.Disk.Used, Total: s.Disk.Total}
	}
	return m
}

func newInfo(i hostmetrics.Info) *DeviceInfo {
	return &DeviceInfo{
		Hostname: i.Hostname, OSVersion: i.OSVersion, Kernel: i.Kernel,
		Model: i.Model, Arch: i.Arch, Cores: i.Cores,
	}
}

// historyID returns the ID that d's history is kept under: store.HubID if d
// is the machine Roost runs on, as Roost's own node or as the peer that is
// the same machine, or else d's ID.
func historyID(d tailnet.Device, self bool) string {
	if self || d.SameMachine {
		return store.HubID
	}
	return d.ID
}

// deviceStatus works out whether d needs attention at now, and why. sustained
// is its CPU averaged over hostmetrics.SustainedWindow, if Roost knows it.
func deviceStatus(d Device, sustained *float64, now time.Time) (Status, string) {
	if !d.Online {
		return StatusOffline, "Not connected to the tailnet"
	}
	var reasons []string
	if m := d.Metrics; m != nil {
		if pct, ok := percent(m.Disk); ok && pct >= diskWarn {
			reasons = append(reasons, fmt.Sprintf("Disk %.0f%% full", pct))
		}
		if pct, ok := percent(m.Memory); ok && pct >= memoryWarn {
			reasons = append(reasons, fmt.Sprintf("Memory %.0f%% used", pct))
		}
	}
	if sustained != nil && *sustained >= cpuWarn {
		reasons = append(reasons, fmt.Sprintf("CPU %.0f%% for %v", *sustained, shortDuration(hostmetrics.SustainedWindow)))
	}
	if d.KeyExpiry != nil {
		switch left := d.KeyExpiry.Sub(now); {
		case left <= 0:
			reasons = append(reasons, "Tailscale key expired")
		case left < keyExpiryWarn:
			reasons = append(reasons, fmt.Sprintf("Tailscale key expires in %dd", int(math.Ceil(left.Hours()/24))))
		}
	}
	if d.Path.Unanswered {
		reasons = append(reasons, "Not answering pings")
	}
	if d.Agent != nil && d.Agent.Stale {
		reasons = append(reasons, "Agent stopped reporting")
	}
	if len(reasons) > 0 {
		return StatusAttention, strings.Join(reasons, " · ")
	}

	p := d.Path
	switch p.Kind {
	case PathKindSelf:
		return StatusOnline, "This hub"
	case PathKindDirect:
		return StatusOnline, "Direct connection"
	case PathKindDERP:
		if p.DERP == "" {
			return StatusOnline, "Relayed through DERP"
		}
		return StatusOnline, "Relayed through DERP " + p.DERP
	case PathKindPeerRelay:
		return StatusOnline, "Relayed through a peer relay"
	case PathKindOffline:
	}
	return StatusOnline, "Online"
}

// percent returns how much of u is used, in percent.
func percent(u *Usage) (float64, bool) {
	if u == nil || u.Total == 0 {
		return 0, false
	}
	return float64(u.Used) / float64(u.Total) * 100, true
}

// shortDuration writes a whole number of minutes or hours as 5m or 2h.
func shortDuration(d time.Duration) string {
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return fmt.Sprintf("%dm", d/time.Minute)
}

// "tab", "nas" and "pi" only count as words of their own, and "nas" can
// have a number after it, so "tabithas-pixel", "jonas", "nasser-macbook" and
// "pixel" don't match.
var (
	tabletName = regexp.MustCompile(`ipad|tablet|\btab\b`)
	serverName = regexp.MustCompile(`vps|server|srv|raspberry|\bnas(\b|\d)|\bpi\b`)
	laptopName = regexp.MustCompile(`book|laptop|thinkpad|surface|xps`)
)

// inferKind guesses what sort of machine d is, from its OS, its host name
// and the first label of its DNS name. The rest of the DNS name is the
// tailnet's, the same for every device. hub is true for the machine Roost
// runs on, and model is the machine's maker and product, if Roost knows them.
func inferKind(d tailnet.Device, hub bool, model string) DeviceKind {
	os := strings.ToLower(d.OS)
	name := strings.ToLower(d.HostName + " " + firstLabel(d.DNSName) + " " + model)
	switch {
	case os == "android" || os == "ios":
		if tabletName.MatchString(name) {
			return DeviceKindTablet
		}
		return DeviceKindPhone
	case hub || serverName.MatchString(name):
		return DeviceKindServer
	case os == "macos":
		if strings.Contains(name, "macbook") {
			return DeviceKindLaptop
		}
		return DeviceKindDesktop
	case laptopName.MatchString(name):
		return DeviceKindLaptop
	case os == "windows":
		return DeviceKindDesktop
	case os == "linux":
		return DeviceKindServer
	}
	return DeviceKindOther
}

var kinds = []DeviceKind{DeviceKindServer, DeviceKindLaptop, DeviceKindDesktop, DeviceKindTablet, DeviceKindPhone, DeviceKindOther}

func validKind(k DeviceKind) bool { return slices.Contains(kinds, k) }

func kindOrder(k DeviceKind) int { return slices.Index(kinds, k) }

// alertOffline reports whether to alert when d goes offline: what an admin
// set, or else yes, unless d is a phone or tablet.
func alertOffline(d tailnet.Device, ds store.DeviceSettings) bool {
	if ds.AlertOffline != nil {
		return *ds.AlertOffline
	}
	return defaultAlertOffline(d)
}

// defaultAlertOffline reports whether to alert when d goes offline, if no
// admin said. Phones and tablets leave the tailnet all the time.
func defaultAlertOffline(d tailnet.Device) bool {
	os := strings.ToLower(d.OS)
	return os != "android" && os != "ios"
}

// firstLabel returns the first part of a DNS name, such as laptop for
// laptop.example.ts.net.
func firstLabel(name string) string {
	label, _, _ := strings.Cut(name, ".")
	return label
}

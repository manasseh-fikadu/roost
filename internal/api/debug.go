package api

import (
	"net/netip"
	"time"

	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

// The types in this file are the response to GET /api/debug/tailnet, which is
// for people checking the tailnet layer with curl, not for the web app, so
// `make types` leaves them out.

// DebugTailnet is what the tailnet poller knows.
type DebugTailnet struct {
	State   string `json:"state,omitempty"`
	Tailnet string `json:"tailnet,omitempty"`
	// PolledAt is when the status was last polled.
	PolledAt *time.Time `json:"polledAt,omitempty"`
	// Error is why the latest status poll failed, if it did.
	Error        string             `json:"error,omitempty"`
	Self         *DebugDevice       `json:"self,omitempty"`
	Devices      []DebugDevice      `json:"devices"`
	Connectivity *DebugConnectivity `json:"connectivity,omitempty"`
}

// DebugDevice is a node and the path to it.
type DebugDevice struct {
	ID          string     `json:"id"`
	HostName    string     `json:"hostName"`
	DNSName     string     `json:"dnsName"`
	OS          string     `json:"os"`
	IPs         []string   `json:"ips"`
	Owner       string     `json:"owner,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Online      bool       `json:"online"`
	LastSeen    *time.Time `json:"lastSeen,omitempty"`
	SameMachine bool       `json:"sameMachine,omitempty"`
	Path        DebugPath  `json:"path"`
	// PingError is why the latest ping got no reply, if it didn't.
	PingError string `json:"pingError,omitempty"`
}

// DebugPath is how traffic gets to a device.
type DebugPath struct {
	Kind     string     `json:"kind"`
	Endpoint string     `json:"endpoint,omitempty"`
	Relay    string     `json:"relay,omitempty"`
	RTTMs    float64    `json:"rttMs,omitempty"`
	PingedAt *time.Time `json:"pingedAt,omitempty"`
}

// DebugConnectivity is the latest connectivity check.
type DebugConnectivity struct {
	CheckedAt  *time.Time `json:"checkedAt,omitempty"`
	Error      string     `json:"error,omitempty"`
	UDP        bool       `json:"udp"`
	IPv4       bool       `json:"ipv4"`
	IPv6       bool       `json:"ipv6"`
	PublicIPv4 string     `json:"publicIPv4,omitempty"`
	PublicIPv6 string     `json:"publicIPv6,omitempty"`
	DERP       *DebugDERP `json:"derp,omitempty"`
}

// DebugDERP is the nearest DERP region.
type DebugDERP struct {
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	LatencyMs float64 `json:"latencyMs"`
}

func newDebugTailnet(s tailnet.Snapshot, now time.Time) DebugTailnet {
	out := DebugTailnet{PolledAt: timePtr(s.StatusAt), Error: errString(s.StatusErr), Devices: []DebugDevice{}}
	if st := s.Status; st != nil {
		out.State, out.Tailnet = st.State, st.Tailnet
		self := newDebugDevice(s, st.Self, now)
		out.Self = &self
		for _, d := range st.Peers {
			out.Devices = append(out.Devices, newDebugDevice(s, d, now))
		}
	}
	if c := s.Connectivity; c != nil || s.ConnectivityErr != nil {
		out.Connectivity = &DebugConnectivity{CheckedAt: timePtr(s.ConnectivityAt), Error: errString(s.ConnectivityErr)}
		if c != nil {
			out.Connectivity.UDP, out.Connectivity.IPv4, out.Connectivity.IPv6 = c.UDP, c.IPv4, c.IPv6
			out.Connectivity.PublicIPv4 = addrString(c.PublicIPv4)
			out.Connectivity.PublicIPv6 = addrString(c.PublicIPv6)
			if c.PreferredDERP.ID != 0 {
				out.Connectivity.DERP = &DebugDERP{
					Code:      c.PreferredDERP.Code,
					Name:      c.PreferredDERP.Name,
					LatencyMs: ms(c.DERPLatency),
				}
			}
		}
	}
	return out
}

func newDebugDevice(s tailnet.Snapshot, d tailnet.Device, now time.Time) DebugDevice {
	path := s.PathTo(d, now)
	out := DebugDevice{
		ID:          d.ID,
		HostName:    d.HostName,
		DNSName:     d.DNSName,
		OS:          d.OS,
		IPs:         make([]string, 0, len(d.IPs)),
		Owner:       d.Owner,
		Tags:        d.Tags,
		Online:      d.Online,
		LastSeen:    timePtr(d.LastSeen),
		SameMachine: d.SameMachine,
		Path: DebugPath{
			Kind:     string(path.Kind),
			Endpoint: path.Endpoint,
			Relay:    path.Relay,
			RTTMs:    ms(path.Latency),
			PingedAt: timePtr(path.PingedAt),
		},
		PingError: errString(s.Pings[d.ID].Err),
	}
	for _, ip := range d.IPs {
		out.IPs = append(out.IPs, ip.String())
	}
	return out
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func addrString(a netip.Addr) string {
	if !a.IsValid() {
		return ""
	}
	return a.String()
}

func ms(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

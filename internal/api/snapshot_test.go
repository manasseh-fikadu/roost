package api_test

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/hostmetrics"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// snapshotOf builds a snapshot of s and host, as the feed would at now.
func snapshotOf(t *testing.T, st *store.Store, s tailnet.Snapshot, host *hostmetrics.Reading) api.Snapshot {
	t.Helper()
	opts := api.FeedOptions{Tailnet: func() tailnet.Snapshot { return s }, Store: st}
	if host != nil {
		opts.Host = func() hostmetrics.Reading { return *host }
	}
	return api.NewFeed(opts).Latest()
}

func TestSnapshot_devices(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	now := time.Now()
	recent := now.Add(-time.Minute)
	created := time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC)
	self := tailnet.Device{
		ID: "roost", HostName: "hub", DNSName: roostHost, OS: "linux", IPs: []netip.Addr{roostIP}, Online: true,
		Relay: "fra", Created: created,
	}
	peers := []tailnet.Device{
		{
			ID: "mac", HostName: "Alice's MacBook Pro", DNSName: "mac.example.ts.net", OS: "macOS", Online: true,
			Relay: "fra", RxBytes: 100, TxBytes: 200, KeyExpiry: now.Add(30 * 24 * time.Hour), ExitNode: true,
		},
		{ID: "imac", HostName: "imac", DNSName: "imac.example.ts.net", OS: "macOS", Online: true, Relay: "nyc"},
		{ID: "pixel", HostName: "pixel", DNSName: "pixel.example.ts.net", OS: "android", LastSeen: recent, Relay: "fra"},
		{ID: "ipad", HostName: "iPad", DNSName: "ipad.example.ts.net", OS: "iOS", Online: true, PeerRelay: "100.64.0.9:7777:vni:1"},
		{ID: "desk", HostName: "desk", DNSName: "desk.example.ts.net", OS: "windows", Online: true, KeyExpiry: now.Add(36 * time.Hour)},
		{ID: "pi", HostName: "pi", DNSName: "pi.example.ts.net", OS: "linux", Online: true, KeyExpiry: now.Add(-time.Hour)},
		{ID: "thinkpad", HostName: "thinkpad", DNSName: "thinkpad.example.ts.net", OS: "linux", Online: true},
		{ID: "surface", HostName: "jonas-surface-book", DNSName: "jonas-surface-book.example.ts.net", OS: "linux", Online: true},
		{ID: "nas", HostName: "nas01", DNSName: "nas01.example.ts.net", OS: "linux", Online: true},
		{ID: "host", HostName: "host", DNSName: "host.example.ts.net", OS: "linux", Online: true, SameMachine: true},
		{ID: "tv", DNSName: "tv.example.ts.net", OS: "tvOS", Online: true},
	}
	snap := tailnet.Snapshot{
		Status: &tailnet.Status{State: tailnet.StateRunning, Self: self, Peers: peers},
		Pings: map[string]tailnet.PingRecord{
			"mac":  {At: recent, Result: &tailnet.PingResult{Latency: 1500 * time.Microsecond, Endpoint: "203.0.113.2:41641"}},
			"imac": {At: recent, Result: &tailnet.PingResult{Latency: 80 * time.Millisecond, DERPRegion: "nyc"}},
			"desk": {At: recent, Err: tailnet.ErrNoReply, Misses: 2},
		},
	}
	host := &hostmetrics.Reading{
		Sample: &hostmetrics.Sample{
			At: now, CPU: new(95.0), CPUSustained: new(93.4), Load: &[3]float64{1, 2, 3}, Procs: 300,
			Mem:  hostmetrics.Usage{Used: 95, Total: 100},
			Disk: &hostmetrics.Usage{Used: 90, Total: 100},
		},
		Info: hostmetrics.Info{Hostname: "hub", Kernel: "6.12", Model: "QEMU Standard PC", Arch: "x86_64", Cores: 4},
	}
	if err := st.SetDeviceSettings(t.Context(), "imac", store.DeviceSettings{DisplayName: "Studio", Kind: "server", Notes: "Upstairs"}); err != nil {
		t.Fatal(err)
	}
	got := snapshotOf(t, st, snap, host)

	tests := []struct {
		id     string
		name   string
		kind   api.DeviceKind
		status api.Status
		reason string
		path   api.PathKind
	}{
		{"roost", "hub", api.DeviceKindServer, api.StatusAttention, "Disk 90% full · Memory 95% used · CPU 93% for 5m", api.PathKindSelf},
		{"host", "host", api.DeviceKindServer, api.StatusAttention, "Disk 90% full · Memory 95% used · CPU 93% for 5m", api.PathKindDERP},
		{"nas", "nas01", api.DeviceKindServer, api.StatusOnline, "Relayed through DERP", api.PathKindDERP},
		{"pi", "pi", api.DeviceKindServer, api.StatusAttention, "Tailscale key expired", api.PathKindDERP},
		{"imac", "Studio", api.DeviceKindServer, api.StatusOnline, "Relayed through DERP nyc", api.PathKindDERP},
		{"mac", "Alice's MacBook Pro", api.DeviceKindLaptop, api.StatusOnline, "Direct connection", api.PathKindDirect},
		{"surface", "jonas-surface-book", api.DeviceKindLaptop, api.StatusOnline, "Relayed through DERP", api.PathKindDERP},
		{"thinkpad", "thinkpad", api.DeviceKindLaptop, api.StatusOnline, "Relayed through DERP", api.PathKindDERP},
		{"desk", "desk", api.DeviceKindDesktop, api.StatusAttention, "Tailscale key expires in 2d · Not answering pings", api.PathKindDERP},
		{"ipad", "iPad", api.DeviceKindTablet, api.StatusOnline, "Relayed through a peer relay", api.PathKindPeerRelay},
		{"pixel", "pixel", api.DeviceKindPhone, api.StatusOffline, "Not connected to the tailnet", api.PathKindOffline},
		{"tv", "tv", api.DeviceKindOther, api.StatusOnline, "Relayed through DERP", api.PathKindDERP},
	}
	if len(got.Devices) != len(tests) {
		t.Fatalf("got devices %q, want %d", deviceIDs(got), len(tests))
	}
	for i, tt := range tests {
		d := got.Devices[i]
		if d.ID != tt.id || d.Name != tt.name || d.Kind != tt.kind || d.Status != tt.status || d.StatusReason != tt.reason || d.Path.Kind != tt.path {
			t.Errorf("device %d = %s %q, %s, %s %q, path %s; want %s %q, %s, %s %q, path %s", i,
				d.ID, d.Name, d.Kind, d.Status, d.StatusReason, d.Path.Kind,
				tt.id, tt.name, tt.kind, tt.status, tt.reason, tt.path)
		}
	}

	hub := findDevice(t, got, "roost")
	if m := hub.Metrics; m == nil || *m.CPU != 95 || m.Memory.Used != 95 || len(m.Load) != 3 || m.Procs != 300 {
		t.Errorf("hub metrics = %+v, want the host's", hub.Metrics)
	}
	if hub.Info == nil || hub.Info.Model != "QEMU Standard PC" || hub.Info.Cores != 4 {
		t.Errorf("hub info = %+v, want the host's", hub.Info)
	}
	if hub.Created == nil || !hub.Created.Equal(created) || hub.KeyExpiry != nil {
		t.Errorf("hub created %v, key expiry %v; want %v and none", hub.Created, hub.KeyExpiry, created)
	}
	if findDevice(t, got, "host").Metrics == nil {
		t.Error("the peer on Roost's machine has no metrics, want the host's")
	}

	mac := findDevice(t, got, "mac")
	wantPath := api.Path{Kind: api.PathKindDirect, Endpoint: "203.0.113.2:41641", HomeDERP: "fra", RTTMs: new(1.5), PingedAt: &recent}
	if p := mac.Path; p.Kind != wantPath.Kind || p.Endpoint != wantPath.Endpoint || p.HomeDERP != wantPath.HomeDERP ||
		p.RTTMs == nil || *p.RTTMs != 1.5 || p.PingedAt == nil || !p.PingedAt.Equal(recent) {
		t.Errorf("mac path = %+v, want %+v", p, wantPath)
	}
	if !mac.ExitNode || mac.RxBytes != 100 || mac.TxBytes != 200 || mac.Metrics != nil || mac.Info != nil {
		t.Errorf("mac = %+v, want an exit node with its traffic and no metrics", mac)
	}
	if p := findDevice(t, got, "ipad").Path; p.PeerRelay != "100.64.0.9:7777:vni:1" {
		t.Errorf("ipad path = %+v, want the peer relay", p)
	}
	if d := findDevice(t, got, "pixel"); d.LastSeen == nil || !d.LastSeen.Equal(recent) {
		t.Errorf("pixel last seen = %v, want %v", d.LastSeen, recent)
	}

	// Settings, with the defaults for alerting.
	for id, want := range map[string]api.DeviceSettings{
		"imac":  {DisplayName: "Studio", Kind: api.DeviceKindServer, AlertOffline: true, Notes: "Upstairs"},
		"pixel": {AlertOffline: false},
		"ipad":  {AlertOffline: false},
		"mac":   {AlertOffline: true},
	} {
		if got := findDevice(t, got, id).Settings; got != want {
			t.Errorf("%s settings = %+v, want %+v", id, got, want)
		}
	}
}

// How Roost guesses what sort of machine a device is from its OS and name. The
// tailnet's name matches the patterns, so the guess must leave it out.
func TestSnapshot_kinds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		host, os string
		want     api.DeviceKind
	}{
		{"pixel-8", "android", api.DeviceKindPhone},
		{"tabithas-pixel", "android", api.DeviceKindPhone},
		{"galaxy-tab-s8", "android", api.DeviceKindTablet},
		{"lenovo-tablet", "android", api.DeviceKindTablet},
		{"iPad", "iOS", api.DeviceKindTablet},
		{"nasser-macbook", "macOS", api.DeviceKindLaptop},
		{"nasrin-pc", "windows", api.DeviceKindDesktop},
		{"jonas-surface-book", "linux", api.DeviceKindLaptop},
		{"nas", "freebsd", api.DeviceKindServer},
		{"nas01", "freebsd", api.DeviceKindServer},
		{"nas-1", "freebsd", api.DeviceKindServer},
		{"nasser-box", "freebsd", api.DeviceKindOther},
	}
	st := &tailnet.Status{
		State: tailnet.StateRunning,
		Self:  tailnet.Device{ID: "roost", DNSName: "roost.pi-tab.ts.net", Online: true},
	}
	for _, tt := range tests {
		st.Peers = append(st.Peers, tailnet.Device{
			ID: tt.host, HostName: tt.host, DNSName: strings.ToLower(tt.host) + ".pi-tab.ts.net", OS: tt.os, Online: true,
		})
	}
	got := snapshotOf(t, testStore(t), tailnet.Snapshot{Status: st}, nil)
	for _, tt := range tests {
		if d := findDevice(t, got, tt.host); d.Kind != tt.want {
			t.Errorf("%s on %s: kind %s, want %s", tt.host, tt.os, d.Kind, tt.want)
		}
	}
}

// What admins see of a snapshot, and what everyone else sees.
func TestSnapshot_secrets(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	now := time.Now()
	snap := tailnet.Snapshot{
		Status: &tailnet.Status{
			State: tailnet.StateRunning, Tailnet: "alice@example.com",
			Self: tailnet.Device{ID: "roost", DNSName: roostHost, Online: true},
			Peers: []tailnet.Device{
				{ID: "direct", DNSName: "direct.example.ts.net", Online: true, CurAddr: "203.0.113.2:41641"},
				{ID: "relayed", DNSName: "relayed.example.ts.net", Online: true, PeerRelay: "203.0.113.9:7777:vni:1"},
			},
		},
		Connectivity:    &tailnet.Connectivity{IPv4: true, PublicIPv4: netip.MustParseAddr("203.0.113.1")},
		ConnectivityAt:  now,
		ConnectivityErr: errors.New("open /var/lib/tailscale: permission denied"),
	}
	admin := snapshotOf(t, st, snap, nil)
	viewer, ok := api.Redact(admin).(api.Snapshot)
	if !ok {
		t.Fatal("Redact() changed the snapshot's type")
	}

	for _, tt := range []struct {
		name          string
		snap          api.Snapshot
		tailnet, ip   string
		endpoint      string
		relay, errMsg string
	}{
		{"admin", admin, "alice@example.com", "203.0.113.1", "203.0.113.2:41641", "203.0.113.9:7777:vni:1", "open /var/lib/tailscale: permission denied"},
		{"viewer", viewer, "", "", "", "", ""},
	} {
		s := tt.snap
		if s.Hub.Tailnet != tt.tailnet || s.Uplink.PublicIP != tt.ip || s.Collectors[1].Error != tt.errMsg ||
			findDevice(t, s, "direct").Path.Endpoint != tt.endpoint || findDevice(t, s, "relayed").Path.PeerRelay != tt.relay {
			t.Errorf("%s gets tailnet %q, public IP %q, error %q, endpoint %q, peer relay %q; want %q, %q, %q, %q, %q",
				tt.name, s.Hub.Tailnet, s.Uplink.PublicIP, s.Collectors[1].Error,
				findDevice(t, s, "direct").Path.Endpoint, findDevice(t, s, "relayed").Path.PeerRelay,
				tt.tailnet, tt.ip, tt.errMsg, tt.endpoint, tt.relay)
		}
	}
}

func TestSnapshot_uplinkAndCollectors(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	now := time.Now()
	snap := tailnet.Snapshot{
		Status:   &tailnet.Status{State: tailnet.StateRunning, Self: tailnet.Device{ID: "roost", Online: true}},
		StatusAt: now,
		Connectivity: &tailnet.Connectivity{
			UDP: true, IPv6: true, PublicIPv6: netip.MustParseAddr("2001:db8::1"),
			PreferredDERP: tailnet.DERPRegion{ID: 4, Code: "fra", Name: "Frankfurt"}, DERPLatency: 3800 * time.Microsecond,
		},
		ConnectivityAt: now,
	}
	host := &hostmetrics.Reading{Err: errors.New("no /proc/stat")}
	got := snapshotOf(t, st, snap, host)

	u := got.Uplink
	if u.CheckedAt == nil || !u.UDP || u.IPv4 || !u.IPv6 || u.PublicIP != "2001:db8::1" ||
		u.DERP == nil || *u.DERP != (api.DERPRegion{Code: "fra", Name: "Frankfurt", LatencyMs: 3.8}) {
		t.Errorf("uplink = %+v, want UDP, IPv6 only, and Frankfurt at 3.8ms", u)
	}
	var names []string
	for _, c := range got.Collectors {
		names = append(names, c.Name+" "+string(c.State))
	}
	want := []string{"Tailscale status ok", "Hub metrics failing", "Tailscale netcheck ok"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("collectors = %q, want %q", names, want)
	}
	if d := got.Devices[0]; d.Metrics != nil || d.Info == nil {
		t.Errorf("hub metrics %+v, info %+v; want no metrics before the first sample, but the info", d.Metrics, d.Info)
	}

	// Before the first status, there are no devices, and before the first
	// check, no uplink.
	empty := snapshotOf(t, st, tailnet.Snapshot{}, nil)
	if len(empty.Devices) != 0 || empty.Uplink.CheckedAt != nil || empty.Collectors[0].State != api.CollectorStateWaiting {
		t.Errorf("snapshot before any poll = %+v, want no devices or uplink, and waiting collectors", empty)
	}
	if empty.Devices == nil {
		t.Error("devices are null, want an empty list")
	}
}

package tailnet

import (
	"errors"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/net/netcheck"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/views"
)

func TestConvertStatus(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	lastSeen := created.Add(time.Hour)
	expiry := created.Add(180 * 24 * time.Hour)
	tags := views.SliceOf([]string{"tag:server"})
	ip := netip.MustParseAddr

	st := &ipnstate.Status{
		BackendState:   "Running",
		AuthURL:        "",
		CurrentTailnet: &ipnstate.TailnetStatus{Name: "example.com"},
		Self: &ipnstate.PeerStatus{
			ID: "self", HostName: "bield", DNSName: "bield.example.ts.net.", OS: "linux",
			// tsnet doesn't mark its own node online.
			UserID: 1, TailscaleIPs: []netip.Addr{ip("100.64.0.1")}, LastSeen: lastSeen,
		},
		User: map[tailcfg.UserID]tailcfg.UserProfile{1: {LoginName: "alice@example.com"}},
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			key.NewNode().Public(): {
				ID: "web", DNSName: "web.example.ts.net.", OS: "linux", UserID: 1, Tags: &tags,
				TailscaleIPs: []netip.Addr{ip("100.64.0.3")}, Online: true, LastSeen: lastSeen,
				CurAddr: "203.0.113.7:41641", Relay: "fra", Created: created,
				SSH_HostKeys: []string{"ssh-ed25519 AAAA"},
			},
			key.NewNode().Public(): {
				ID: "laptop", HostName: "Alice's laptop", DNSName: "laptop.example.ts.net.", OS: "macOS",
				UserID: 1, TailscaleIPs: []netip.Addr{ip("100.64.0.2")}, LastSeen: lastSeen,
				Relay: "nyc", KeyExpiry: &expiry, RxBytes: 10, TxBytes: 20,
			},
		},
	}

	got := convertStatus(st)

	want := &Status{
		State:   StateRunning,
		Tailnet: "example.com",
		Self: Device{
			ID: "self", HostName: "bield", DNSName: "bield.example.ts.net", OS: "linux",
			IPs: []netip.Addr{ip("100.64.0.1")}, Owner: "alice@example.com", Online: true,
		},
		Peers: []Device{
			{
				// Offline, owned by a user, with a key that expires.
				ID: "laptop", HostName: "Alice's laptop", DNSName: "laptop.example.ts.net", OS: "macOS",
				IPs: []netip.Addr{ip("100.64.0.2")}, Owner: "alice@example.com", LastSeen: lastSeen,
				Relay: "nyc", KeyExpiry: expiry, RxBytes: 10, TxBytes: 20,
			},
			{
				// Tagged, so no owner; online, so no last-seen time; no host
				// name, so it comes from the DNS name; it advertises SSH host
				// keys, so it runs Tailscale SSH.
				ID: "web", HostName: "web", DNSName: "web.example.ts.net", OS: "linux",
				IPs: []netip.Addr{ip("100.64.0.3")}, Tags: []string{"tag:server"}, Online: true,
				CurAddr: "203.0.113.7:41641", Relay: "fra", Created: created, TailscaleSSH: true,
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("convertStatus() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestConvertStatus_loggedOut(t *testing.T) {
	t.Parallel()
	st := &ipnstate.Status{BackendState: "NeedsLogin", AuthURL: "https://login.example.com/a/1"}

	got := convertStatus(st)

	want := &Status{State: StateNeedsLogin, LoginURL: "https://login.example.com/a/1", Peers: []Device{}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("convertStatus() = %+v, want %+v", got, want)
	}
}

func TestConvertWhoIs(t *testing.T) {
	t.Parallel()
	alice := &tailcfg.UserProfile{LoginName: "alice@example.com", DisplayName: "Alice"}
	tests := []struct {
		name string
		who  *apitype.WhoIsResponse
		want *Identity
	}{
		{
			name: "user's device",
			who: &apitype.WhoIsResponse{
				Node:        &tailcfg.Node{StableID: "n1", Name: "laptop.example.ts.net."},
				UserProfile: alice,
			},
			want: &Identity{NodeID: "n1", NodeName: "laptop.example.ts.net", LoginName: "alice@example.com", DisplayName: "Alice"},
		},
		{
			name: "tagged device has no user",
			who: &apitype.WhoIsResponse{
				Node:        &tailcfg.Node{StableID: "n2", Name: "ci.example.ts.net.", Tags: []string{"tag:ci"}},
				UserProfile: &tailcfg.UserProfile{LoginName: "tagged-devices", DisplayName: "Tagged Devices"},
			},
			want: &Identity{NodeID: "n2", NodeName: "ci.example.ts.net", Tags: []string{"tag:ci"}},
		},
		{
			name: "no node",
			who:  &apitype.WhoIsResponse{UserProfile: alice},
			want: &Identity{LoginName: "alice@example.com", DisplayName: "Alice"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := convertWhoIs(tt.who); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertWhoIs() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestConvertReport(t *testing.T) {
	t.Parallel()
	dm := &tailcfg.DERPMap{Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
		1: {RegionID: 1, RegionCode: "nyc", RegionName: "New York City"},
		4: {RegionID: 4, RegionCode: "fra", RegionName: "Frankfurt"},
	}}
	tests := []struct {
		name   string
		report *netcheck.Report
		want   *Connectivity
	}{
		{
			name: "preferred region",
			report: &netcheck.Report{
				UDP: true, IPv4: true,
				GlobalV4:      netip.MustParseAddrPort("203.0.113.7:41641"),
				PreferredDERP: 4,
				RegionLatency: netcheck.RegionLatency{1: 90 * time.Millisecond, 4: 12 * time.Millisecond},
			},
			want: &Connectivity{
				UDP: true, IPv4: true,
				PublicIPv4:    netip.MustParseAddr("203.0.113.7"),
				PreferredDERP: DERPRegion{ID: 4, Code: "fra", Name: "Frankfurt"},
				DERPLatency:   12 * time.Millisecond,
			},
		},
		{
			name:   "region missing from the map",
			report: &netcheck.Report{PreferredDERP: 9},
			want:   &Connectivity{PreferredDERP: DERPRegion{ID: 9}},
		},
		{
			name:   "nothing answered",
			report: &netcheck.Report{},
			want:   &Connectivity{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := convertReport(tt.report, dm); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertReport() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestConvertDERPMap(t *testing.T) {
	t.Parallel()
	dm := &tailcfg.DERPMap{Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
		4: {RegionID: 4, RegionCode: "fra", RegionName: "Frankfurt"},
		1: {RegionID: 1, RegionCode: "nyc", RegionName: "New York City"},
		2: nil,
	}}

	got := convertDERPMap(dm)

	want := []DERPRegion{{ID: 1, Code: "nyc", Name: "New York City"}, {ID: 4, Code: "fra", Name: "Frankfurt"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("convertDERPMap() = %+v, want %+v", got, want)
	}
}

func TestMarkSameMachine(t *testing.T) {
	t.Parallel()
	peers := []Device{
		{ID: "this", IPs: []netip.Addr{netip.MustParseAddr("100.64.0.5"), netip.MustParseAddr("fd7a:115c:a1e0::5")}},
		{ID: "other", IPs: []netip.Addr{netip.MustParseAddr("100.64.0.6")}},
	}
	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
		// net reports IPv4 addresses in 16 bytes.
		&net.IPNet{IP: net.ParseIP("100.64.0.5").To16(), Mask: net.CIDRMask(32, 32)},
	}

	markSameMachine(peers, addrs)

	if !peers[0].SameMachine || peers[1].SameMachine {
		t.Errorf("SameMachine = %v, %v; want true, false", peers[0].SameMachine, peers[1].SameMachine)
	}
}

func TestURLHost(t *testing.T) {
	t.Parallel()
	v4, v6 := netip.MustParseAddr("100.64.0.1"), netip.MustParseAddr("fd7a:115c:a1e0::1")
	self := &ipnstate.PeerStatus{DNSName: "bield.example.ts.net."}
	magicDNS := &ipnstate.TailnetStatus{MagicDNSEnabled: true}
	tests := []struct {
		name       string
		st         *ipnstate.Status
		wantHost   string
		wantDomain string
	}{
		{
			name: "HTTPS",
			st: &ipnstate.Status{Self: self, CurrentTailnet: magicDNS, TailscaleIPs: []netip.Addr{v4},
				CertDomains: []string{"bield.example.ts.net"}},
			wantHost:   "bield.example.ts.net",
			wantDomain: "bield.example.ts.net",
		},
		{
			name:     "MagicDNS without HTTPS",
			st:       &ipnstate.Status{Self: self, CurrentTailnet: magicDNS, TailscaleIPs: []netip.Addr{v6, v4}},
			wantHost: "bield.example.ts.net",
		},
		{
			name:     "no MagicDNS prefers IPv4",
			st:       &ipnstate.Status{Self: self, TailscaleIPs: []netip.Addr{v6, v4}, CertDomains: []string{"x"}},
			wantHost: "100.64.0.1",
		},
		{
			name:     "only IPv6",
			st:       &ipnstate.Status{Self: self, TailscaleIPs: []netip.Addr{v6}},
			wantHost: "[fd7a:115c:a1e0::1]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hostForURL(tt.st); got != tt.wantHost {
				t.Errorf("hostForURL() = %q, want %q", got, tt.wantHost)
			}
			domain, ok := certDomain(tt.st)
			if domain != tt.wantDomain || ok != (tt.wantDomain != "") {
				t.Errorf("certDomain() = %q, %v; want %q", domain, ok, tt.wantDomain)
			}
		})
	}
}

func TestAppURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		scheme, host string
		port         int
		want         string
	}{
		{"https", "bield.example.ts.net", 443, "https://bield.example.ts.net"},
		{"https", "bield.example.ts.net", 4700, "https://bield.example.ts.net:4700"},
		{"http", "100.64.0.1", 80, "http://100.64.0.1"},
		{"http", "100.64.0.1", 4700, "http://100.64.0.1:4700"},
		// Host mode on port 443 without a certificate serves HTTP there.
		{"http", "bield.example.ts.net", 443, "http://bield.example.ts.net:443"},
		{"http", "[fd7a:115c:a1e0::1]", 4700, "http://[fd7a:115c:a1e0::1]:4700"},
	}
	for _, tt := range tests {
		if got := appURL(tt.scheme, tt.host, tt.port); got != tt.want {
			t.Errorf("appURL(%q, %q, %d) = %q, want %q", tt.scheme, tt.host, tt.port, got, tt.want)
		}
	}
}

func TestMultiListener(t *testing.T) {
	t.Parallel()
	var lc net.ListenConfig
	var lns []net.Listener
	for range 2 {
		ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		lns = append(lns, ln)
	}
	m := newMultiListener(lns)
	t.Cleanup(func() { _ = m.Close() })

	for _, ln := range lns {
		var d net.Dialer
		c, err := d.DialContext(t.Context(), "tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		got, err := m.Accept()
		if err != nil {
			t.Fatalf("Accept() error = %v", err)
		}
		if got.LocalAddr().String() != ln.Addr().String() {
			t.Errorf("accepted a connection to %v, want %v", got.LocalAddr(), ln.Addr())
		}
		_ = got.Close()
	}

	if err := m.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := m.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Accept() after Close error = %v, want net.ErrClosed", err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("second Close() error = %v, want none", err)
	}
}

func TestCustomControlURL(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ controlURL, want string }{
		{"", ""},
		{"https://controlplane.tailscale.com", ""},
		{"https://login.tailscale.com", ""},
		{"https://hs.example.com", "https://hs.example.com"},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080"},
	} {
		if got := customControlURL(&ipn.Prefs{ControlURL: tt.controlURL}); got != tt.want {
			t.Errorf("customControlURL(%q) = %q, want %q", tt.controlURL, got, tt.want)
		}
	}
}

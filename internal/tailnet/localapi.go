package tailnet

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/net/netcheck"
	"tailscale.com/net/netmon"
	"tailscale.com/tailcfg"
	"tailscale.com/types/logger"
	"tailscale.com/util/eventbus"
)

// localAPI answers the Tailnet questions through a tailscaled's LocalAPI,
// whether that tailscaled runs inside Bield (Embedded) or on the machine
// (Host). They differ only in how they listen and dial.
type localAPI struct {
	lc *local.Client
}

func (a localAPI) Status(ctx context.Context) (*Status, error) {
	st, err := a.lc.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := convertStatus(st)
	out.ControlURL = a.controlURL(ctx)
	return out, nil
}

// controlURL returns the control server the node uses when it isn't
// Tailscale's, or "" if it is. The status doesn't say, so this reads the
// node's preferences, and a failure to read them counts as Tailscale's.
func (a localAPI) controlURL(ctx context.Context) string {
	prefs, err := a.lc.GetPrefs(ctx)
	if err != nil {
		return ""
	}
	return customControlURL(prefs)
}

func (a localAPI) Ping(ctx context.Context, ip netip.Addr) (*PingResult, error) {
	res, err := a.lc.Ping(ctx, ip, tailcfg.PingDisco)
	if err != nil {
		return nil, err
	}
	if res.Err != "" {
		return nil, errors.New(res.Err)
	}
	return &PingResult{
		Latency:    time.Duration(res.LatencySeconds * float64(time.Second)),
		Endpoint:   res.Endpoint,
		DERPRegion: res.DERPRegionCode,
		PeerRelay:  res.PeerRelay,
	}, nil
}

func (a localAPI) WhoIs(ctx context.Context, remoteAddr string) (*Identity, error) {
	who, err := a.lc.WhoIs(ctx, remoteAddr)
	if errors.Is(err, local.ErrPeerNotFound) {
		return nil, ErrUnknownPeer
	}
	if err != nil {
		return nil, err
	}
	return convertWhoIs(who), nil
}

func (a localAPI) DERPMap(ctx context.Context) ([]DERPRegion, error) {
	dm, err := a.lc.CurrentDERPMap(ctx)
	if err != nil {
		return nil, err
	}
	return convertDERPMap(dm), nil
}

// Connectivity runs a netcheck in this process with the node's DERP map, as
// the tailscale CLI does, since tailscaled's LocalAPI has no netcheck. Its
// probes use their own UDP sockets, so they see the same network as the node
// without disturbing it.
func (a localAPI) Connectivity(ctx context.Context) (*Connectivity, error) {
	dm, err := a.lc.CurrentDERPMap(ctx)
	if err != nil {
		return nil, err
	}
	if dm == nil || len(dm.Regions) == 0 {
		return nil, errors.New("no DERP map yet")
	}

	// The probe sockets stay open until ctx ends.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	bus := eventbus.New()
	defer bus.Close()
	netMon, err := netmon.New(bus, logger.Discard)
	if err != nil {
		return nil, fmt.Errorf("watch the network: %w", err)
	}
	defer netMon.Close()

	c := &netcheck.Client{NetMon: netMon, Logf: logger.Discard}
	// If UDP sockets can't be opened, the report says UDP doesn't work.
	_ = c.Standalone(ctx, "")
	report, err := c.GetReport(ctx, dm, nil)
	if err != nil {
		return nil, err
	}
	return convertReport(report, dm), nil
}

// waitRunning waits until the backend is running, calling onState with each
// state it passes through on the way, and returns its status then.
func waitRunning(ctx context.Context, lc *local.Client, onState func(ipn.State)) (*ipnstate.Status, error) {
	w, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
	if err != nil {
		return nil, err
	}
	defer func() { _ = w.Close() }()
	for {
		n, err := w.Next()
		if err != nil {
			return nil, err
		}
		if n.ErrMessage != nil {
			return nil, fmt.Errorf("tailscale: %s", *n.ErrMessage)
		}
		if n.State == nil {
			continue
		}
		if *n.State != ipn.Running {
			onState(*n.State)
			continue
		}
		st, err := lc.Status(ctx)
		if err != nil {
			return nil, err
		}
		if len(st.TailscaleIPs) == 0 {
			return nil, errors.New("tailscale is running but has no tailnet IP")
		}
		return st, nil
	}
}

// customControlURL returns the control server that prefs name when it isn't
// Tailscale's, which is any but the default and its old names.
func customControlURL(prefs *ipn.Prefs) string {
	u := prefs.ControlURL
	if u == "" || u == ipn.DefaultControlURL || ipn.IsLoginServerSynonym(u) {
		return ""
	}
	return u
}

func convertStatus(st *ipnstate.Status) *Status {
	out := &Status{State: st.BackendState, LoginURL: st.AuthURL}
	if st.CurrentTailnet != nil {
		out.Tailnet = st.CurrentTailnet.Name
	}
	if st.Self != nil {
		out.Self = convertPeer(st.Self, st.User)
		// tsnet doesn't mark its own node online, but a running node is on
		// the tailnet.
		out.Self.Online = out.State == StateRunning
		out.Self.LastSeen = time.Time{}
	}
	out.Peers = make([]Device, 0, len(st.Peer))
	for _, p := range st.Peer {
		out.Peers = append(out.Peers, convertPeer(p, st.User))
	}
	slices.SortFunc(out.Peers, func(a, b Device) int {
		return cmp.Or(cmp.Compare(a.DNSName, b.DNSName), cmp.Compare(a.ID, b.ID))
	})
	return out
}

func convertPeer(p *ipnstate.PeerStatus, users map[tailcfg.UserID]tailcfg.UserProfile) Device {
	d := Device{
		ID:        string(p.ID),
		HostName:  p.HostName,
		DNSName:   strings.TrimSuffix(p.DNSName, "."),
		OS:        p.OS,
		IPs:       slices.Clone(p.TailscaleIPs),
		Online:    p.Online,
		CurAddr:   p.CurAddr,
		Relay:     p.Relay,
		PeerRelay: p.PeerRelay,
		RxBytes:   p.RxBytes,
		TxBytes:   p.TxBytes,
		Created:   p.Created,
		ExitNode:  p.ExitNode,
		// A node advertises its SSH host keys only while it runs Tailscale SSH.
		TailscaleSSH: len(p.SSH_HostKeys) > 0,
	}
	if d.HostName == "" {
		d.HostName, _, _ = strings.Cut(d.DNSName, ".")
	}
	if p.Tags != nil && p.Tags.Len() > 0 {
		d.Tags = p.Tags.AsSlice()
	} else if u, ok := users[p.UserID]; ok {
		d.Owner = u.LoginName
	}
	if !p.Online {
		d.LastSeen = p.LastSeen
	}
	if p.KeyExpiry != nil {
		d.KeyExpiry = *p.KeyExpiry
	}
	return d
}

func convertWhoIs(who *apitype.WhoIsResponse) *Identity {
	id := &Identity{}
	if n := who.Node; n != nil {
		id.NodeID = string(n.StableID)
		id.NodeName = strings.TrimSuffix(n.Name, ".")
		id.Tags = slices.Clone(n.Tags)
	}
	// Tailscale reports a tagged node's user as a placeholder
	// "tagged-devices" profile, which isn't anyone.
	if u := who.UserProfile; u != nil && len(id.Tags) == 0 {
		id.LoginName = u.LoginName
		id.DisplayName = u.DisplayName
	}
	return id
}

func convertDERPMap(dm *tailcfg.DERPMap) []DERPRegion {
	if dm == nil {
		return nil
	}
	regions := make([]DERPRegion, 0, len(dm.Regions))
	for _, r := range dm.Regions {
		if r == nil {
			continue
		}
		regions = append(regions, DERPRegion{ID: int64(r.RegionID), Code: r.RegionCode, Name: r.RegionName})
	}
	slices.SortFunc(regions, func(a, b DERPRegion) int { return cmp.Compare(a.ID, b.ID) })
	return regions
}

func convertReport(r *netcheck.Report, dm *tailcfg.DERPMap) *Connectivity {
	c := &Connectivity{
		UDP:        r.UDP,
		IPv4:       r.IPv4,
		IPv6:       r.IPv6,
		PublicIPv4: r.GlobalV4.Addr(),
		PublicIPv6: r.GlobalV6.Addr(),
	}
	if id := r.PreferredDERP; id != 0 {
		c.PreferredDERP = DERPRegion{ID: int64(id)}
		if region := dm.Regions[id]; region != nil {
			c.PreferredDERP.Code = region.RegionCode
			c.PreferredDERP.Name = region.RegionName
		}
		c.DERPLatency = r.RegionLatency[id]
	}
	return c
}

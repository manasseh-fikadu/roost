package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/tailnet/tailnettest"
)

func TestNew_debugTailnet(t *testing.T) {
	// The store's goroutines start outside the bubble, so it can end without them.
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		laptop := laptopIP
		tn := tailnettest.New(tailnet.Status{
			State:   tailnet.StateRunning,
			Tailnet: "example.com",
			Self:    tailnet.Device{ID: "roost", DNSName: "roost.example.ts.net", Online: true},
			Peers: []tailnet.Device{
				{ID: "laptop", DNSName: "laptop.example.ts.net", OS: "macOS", IPs: []netip.Addr{laptop}, Owner: alice, Online: true},
				{ID: "phone", DNSName: "phone.example.ts.net", Relay: "fra"},
			},
		})
		tn.SetPing(laptop, &tailnet.PingResult{Latency: 1500 * time.Microsecond, Endpoint: "203.0.113.2:41641"}, nil)
		tn.SetConnectivity(&tailnet.Connectivity{
			UDP: true, IPv4: true, PublicIPv4: netip.MustParseAddr("203.0.113.1"),
			PreferredDERP: tailnet.DERPRegion{ID: 4, Code: "fra", Name: "Frankfurt"}, DERPLatency: 12 * time.Millisecond,
		}, nil)
		poller := tailnet.NewPoller(tn, nil)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go poller.Run(ctx)
		synctest.Wait()

		h := api.New(api.Options{WhoIs: tn.WhoIs, Snapshot: poller.Snapshot, Store: st})
		rec := serve(h, newRequest(t, http.MethodGet, "/api/debug/tailnet", laptop))

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		var got api.DebugTailnet
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode body %q: %v", rec.Body, err)
		}
		if got.State != tailnet.StateRunning || got.Tailnet != "example.com" || got.Self == nil || got.Self.Path.Kind != "self" {
			t.Errorf("state %q, tailnet %q, self %+v; want Running, example.com and the self path",
				got.State, got.Tailnet, got.Self)
		}
		if len(got.Devices) != 2 {
			t.Fatalf("got %d devices, want 2", len(got.Devices))
		}
		wantLaptop := api.DebugPath{Kind: "direct", Endpoint: "203.0.113.2:41641", RTTMs: 1.5, PingedAt: got.Devices[0].Path.PingedAt}
		if got.Devices[0].Path != wantLaptop || got.Devices[0].Path.PingedAt == nil {
			t.Errorf("laptop path = %+v, want %+v with a ping time", got.Devices[0].Path, wantLaptop)
		}
		if want := (api.DebugPath{Kind: "offline"}); got.Devices[1].Path != want {
			t.Errorf("phone path = %+v, want %+v", got.Devices[1].Path, want)
		}
		c := got.Connectivity
		if c == nil || !c.UDP || c.PublicIPv4 != "203.0.113.1" || c.DERP == nil || *c.DERP != (api.DebugDERP{Code: "fra", Name: "Frankfurt", LatencyMs: 12}) {
			t.Errorf("connectivity = %+v, want UDP, the public IP and Frankfurt at 12ms", c)
		}
	})
}

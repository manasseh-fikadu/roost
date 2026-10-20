package tailnettest_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/tailnet/tailnettest"
)

var (
	roostIP  = netip.MustParseAddr("100.64.0.1")
	laptopIP = netip.MustParseAddr("100.64.0.2")
	ciIP     = netip.MustParseAddr("100.64.0.3")
)

func newTailnet(state string) *tailnettest.Tailnet {
	return tailnettest.New(tailnet.Status{
		State: state,
		Self:  tailnet.Device{ID: "roost", DNSName: "roost.example.ts.net", IPs: []netip.Addr{roostIP}},
		Peers: []tailnet.Device{
			{ID: "laptop", DNSName: "laptop.example.ts.net", IPs: []netip.Addr{laptopIP}, Owner: "alice@example.com"},
			{ID: "ci", DNSName: "ci.example.ts.net", IPs: []netip.Addr{ciIP}, Tags: []string{"tag:ci"}},
		},
	})
}

func TestTailnet_WhoIs(t *testing.T) {
	t.Parallel()
	tn := newTailnet(tailnet.StateRunning)
	tests := []struct {
		name    string
		addr    string
		want    *tailnet.Identity
		wantErr error
	}{
		{
			name: "user's device",
			addr: "100.64.0.2:51234",
			want: &tailnet.Identity{
				NodeID: "laptop", NodeName: "laptop.example.ts.net",
				LoginName: "alice@example.com", DisplayName: "alice@example.com",
			},
		},
		{
			name: "tagged device",
			addr: "100.64.0.3:51234",
			want: &tailnet.Identity{NodeID: "ci", NodeName: "ci.example.ts.net", Tags: []string{"tag:ci"}},
		},
		{name: "unknown address", addr: "127.0.0.1:51234", wantErr: tailnet.ErrUnknownPeer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tn.WhoIs(t.Context(), tt.addr)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("WhoIs(%q) error = %v, want %v", tt.addr, err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WhoIs(%q) = %+v, want %+v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestTailnet_Listen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tn := newTailnet(tailnet.StateNeedsLogin)
		t.Cleanup(func() { _ = tn.Close() })
		listened := make(chan net.Listener, 1)
		go func() {
			ln, _, err := tn.Listen(t.Context())
			if err != nil {
				t.Errorf("Listen() error = %v", err)
			}
			listened <- ln
		}()

		synctest.Wait()
		select {
		case <-listened:
			t.Fatal("Listen() returned before login")
		default:
		}

		tn.SetStatus(tailnet.Status{State: tailnet.StateRunning, Self: tailnet.Device{IPs: []netip.Addr{roostIP}}})
		ln := <-listened
		srv := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				//nolint:gosec // G705: the test reads what it sent back.
				_, _ = io.WriteString(w, r.RemoteAddr)
			}),
			ReadHeaderTimeout: time.Second,
		}
		go func() { _ = srv.Serve(ln) }()
		t.Cleanup(func() { _ = srv.Close() })

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, tailnettest.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := tn.Client(laptopIP).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		got, err := netip.ParseAddrPort(string(body))
		if err != nil || got.Addr() != laptopIP {
			t.Errorf("the server saw RemoteAddr %q, want %v:port", body, laptopIP)
		}
	})
}

func TestTailnet_Ping(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tn := newTailnet(tailnet.StateRunning)
		want := &tailnet.PingResult{Latency: time.Millisecond, Endpoint: "203.0.113.2:41641"}
		tn.SetPing(laptopIP, want, nil)

		if got, err := tn.Ping(t.Context(), laptopIP); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Ping(laptop) = %+v, %v; want %+v", got, err, want)
		}

		// A ping that gets no reply waits for its context.
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		start := time.Now()
		if _, err := tn.Ping(ctx, ciIP); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Ping(ci) error = %v, want context.DeadlineExceeded", err)
		}
		if waited := time.Since(start); waited != 5*time.Second {
			t.Errorf("Ping(ci) returned after %v, want 5s", waited)
		}
	})
}

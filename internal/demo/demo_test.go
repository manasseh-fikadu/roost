package demo_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/manasseh-fikadu/roost/internal/demo"
	"github.com/manasseh-fikadu/roost/internal/docker"
	"github.com/manasseh-fikadu/roost/internal/ssh"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/systemd"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

func newDemo(t *testing.T, devices int) *demo.Demo {
	t.Helper()
	d := demo.New(demo.Options{Devices: devices, Listen: "127.0.0.1:0"})
	t.Cleanup(d.Close)
	return d
}

func status(t *testing.T, d *demo.Demo) *tailnet.Status {
	t.Helper()
	st, err := d.Node().Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestNew_tailnet(t *testing.T) {
	t.Parallel()
	for _, n := range []int{2, 12, 200} {
		t.Run(fmt.Sprintf("%d devices", n), func(t *testing.T) {
			t.Parallel()
			st := status(t, newDemo(t, n))

			if st.State != tailnet.StateRunning || len(st.Peers) != n-1 {
				t.Fatalf("status = %s with %d peers, want running with %d", st.State, len(st.Peers), n-1)
			}
			seenID, seenIP := map[string]bool{}, map[string]bool{}
			for _, dev := range append([]tailnet.Device{st.Self}, st.Peers...) {
				if seenID[dev.ID] || seenIP[dev.IPs[0].String()] {
					t.Errorf("device %s repeats an ID or an address", dev.DNSName)
				}
				seenID[dev.ID], seenIP[dev.IPs[0].String()] = true, true
				if !strings.HasSuffix(dev.DNSName, ".ts.net") || !dev.IPs[0].Is4() || !dev.IPs[1].Is6() {
					t.Errorf("device %+v isn't named and numbered like a tailnet's", dev)
				}
			}
		})
	}
}

func TestNode_whoIs(t *testing.T) {
	t.Parallel()
	d := newDemo(t, 12)

	who, err := d.Node().WhoIs(t.Context(), "203.0.113.5:1234")

	if err != nil {
		t.Fatal(err)
	}
	if who.LoginName != "alex@example.com" || !strings.HasPrefix(who.NodeName, "macbook.") {
		t.Errorf("WhoIs() = %+v, want Alex on the MacBook", who)
	}
}

func TestNode_listen(t *testing.T) {
	t.Parallel()
	d := newDemo(t, 12)

	ln, url, err := d.Node().Listen(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(url, "http://127.0.0.1:") || ln.Addr().String() != strings.TrimPrefix(url, "http://") {
		t.Errorf("Listen() = %q on %v, want the address it listens on", url, ln.Addr())
	}
	if err := d.Node().Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

func TestDial_checks(t *testing.T) {
	t.Parallel()
	d := newDemo(t, 12)
	tests := []struct {
		name, addr string
		wait       time.Duration
		wantStatus int
		wantErr    bool
	}{
		{name: "a service that is up", addr: "homelab.tail-demo.ts.net:8123", wantStatus: http.StatusOK},
		{name: "a redirect", addr: "nas.tail-demo.ts.net:8000", wantStatus: http.StatusFound},
		{name: "a machine that is off waits", addr: "pi-garage.tail-demo.ts.net:8081", wait: 200 * time.Millisecond, wantErr: true},
		{name: "somewhere that isn't there", addr: "example.org:80", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			if tt.wait > 0 {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(t.Context(), tt.wait)
				defer stop()
			}
			start := time.Now()

			conn, err := d.Node().Dial(ctx, "tcp", tt.addr)

			if tt.wantErr {
				if err == nil {
					_ = conn.Close()
					t.Fatalf("Dial(%s) worked, want an error", tt.addr)
				}
				if tt.wait > 0 && time.Since(start) < tt.wait {
					t.Errorf("Dial(%s) failed after %v, want it to wait for the check to give up", tt.addr, time.Since(start))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			client := &http.Client{
				Transport:     &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return conn, nil }},
				CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+tt.addr+"/", nil)
			if err != nil {
				t.Fatal(err)
			}
			res, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = res.Body.Close()
			if res.StatusCode != tt.wantStatus {
				t.Errorf("GET %s = %d, want %d", tt.addr, res.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()
	d := newDemo(t, 12)
	if addrs, err := d.Lookup(t.Context(), "example.com"); err != nil || len(addrs) != 2 {
		t.Errorf("Lookup(example.com) = %v, %v, want two addresses", addrs, err)
	}
	if addrs, err := d.Lookup(t.Context(), "nas.tail-demo.ts.net."); err != nil || len(addrs) != 1 {
		t.Errorf("Lookup(nas) = %v, %v, want its tailnet address", addrs, err)
	}
	if _, err := d.Lookup(t.Context(), "nowhere.example.org"); err == nil {
		t.Error("Lookup(nowhere.example.org) worked, want an error")
	}
}

func TestSystemctl(t *testing.T) {
	t.Parallel()
	d := newDemo(t, 12)
	c := systemd.NewCollector(systemd.Options{
		Run:     d.Systemctl,
		Watched: func() []string { return []string{"ssh.service", "sshd.service", "docker.service"} },
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go c.Run(ctx)
	<-c.Changes()

	r := c.Reading()

	if r.Err != nil {
		t.Fatal(r.Err)
	}
	var got []string
	for _, u := range r.Units {
		got = append(got, u.Name+" "+u.Active)
	}
	want := []string{"backup.service failed", "docker.service active", "ssh.service active"}
	if !slices.Equal(got, want) {
		t.Errorf("units = %v, want %v", got, want)
	}
	if installed, err := c.Installed(ctx); err != nil || len(installed) < 5 {
		t.Errorf("Installed() = %v, %v, want the machine's services", installed, err)
	}
}

// dockerOf returns a Docker client of the made-up machine Roost runs on.
func dockerOf(t *testing.T, d *demo.Demo) *docker.Client {
	t.Helper()
	dial := d.HubDocker()
	if dial == nil {
		t.Fatal("HubDocker() = nil, want the hub's Docker")
	}
	return docker.NewClient(dial)
}

func TestEngine(t *testing.T) {
	t.Parallel()
	c := dockerOf(t, newDemo(t, 12))

	containers, err := c.Containers(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, ct := range containers {
		names = append(names, ct.Name+" "+ct.State)
	}
	if want := []string{"caddy running", "roost running", "watchtower running"}; !slices.Equal(names, want) {
		t.Errorf("containers = %v, want %v", names, want)
	}

	// Docker counts up, so the second sample has a CPU rate to take from.
	first, err := c.Sample(t.Context(), "roost")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	second, err := c.Sample(t.Context(), "roost")
	if err != nil {
		t.Fatal(err)
	}
	if *second.CPUStats.CPUUsage.TotalUsage <= *first.CPUStats.CPUUsage.TotalUsage {
		t.Errorf("CPU time went from %d to %d, want it to count up",
			*first.CPUStats.CPUUsage.TotalUsage, *second.CPUStats.CPUUsage.TotalUsage)
	}
	if _, err := c.Inspect(t.Context(), "nope"); err == nil {
		t.Error("Inspect(nope) worked, want an error")
	}
	for name, fetch := range map[string]func(context.Context) (any, error){
		"info":     func(ctx context.Context) (any, error) { return c.Info(ctx) },
		"df":       func(ctx context.Context) (any, error) { return c.Df(ctx) },
		"volumes":  func(ctx context.Context) (any, error) { return c.Volumes(ctx) },
		"images":   func(ctx context.Context) (any, error) { return c.Images(ctx) },
		"networks": func(ctx context.Context) (any, error) { return c.Networks(ctx) },
	} {
		if _, err := fetch(t.Context()); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSSH(t *testing.T) {
	t.Parallel()
	d := newDemo(t, 12)
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "roost.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	tn := d.Node()
	snap := func() tailnet.Snapshot {
		s, _ := tn.Status(context.Background())
		return tailnet.Snapshot{Status: s}
	}
	m := ssh.NewManager(ssh.Options{
		KeyDir: t.TempDir(), Store: st, Tailnet: snap, Dial: tn.Dial, DialLocal: d.DialLocal, HostMode: true,
	})
	key, err := m.Key()
	if err != nil {
		t.Fatal(err)
	}
	d.AuthorizeSSH(key.PublicKey())
	var nas tailnet.Device
	for _, p := range status(t, d).Peers {
		if p.HostName == "nas" {
			nas = p
		}
	}
	if err := st.SetSSHAccess(t.Context(), store.SSHAccess{DeviceID: nas.ID, User: "alex", Port: 22}); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(t.Context()); err != nil {
		t.Fatal(err)
	}

	got, err := m.Check(t.Context(), nas.ID)

	if err != nil {
		t.Fatal(err)
	}
	if got.State != ssh.StateOK || got.Docker == nil || !*got.Docker || got.Tmux == nil || !*got.Tmux {
		t.Errorf("Check(nas) = %+v, want to log in and find tmux and Docker", got)
	}
	conn, err := m.DialDocker(t.Context(), nas.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "roost.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestSeed(t *testing.T) {
	t.Parallel()
	d := newDemo(t, 12)
	st := openStore(t)

	if err := d.Seed(t.Context(), st, 24*time.Hour); err != nil {
		t.Fatal(err)
	}

	checks, err := st.Checks(t.Context())
	if err != nil || len(checks) != 14 {
		t.Errorf("Checks() = %d checks, %v, want 14 and the database's own gone", len(checks), err)
	}
	open, err := st.OpenAlerts(t.Context())
	if err != nil || len(open) != 4 {
		t.Errorf("OpenAlerts() = %d alerts, %v, want 4", len(open), err)
	}
	resolved, err := st.ResolvedAlerts(t.Context(), 0, 50)
	if err != nil || len(resolved) != 6 {
		t.Errorf("ResolvedAlerts() = %d alerts, %v, want 6", len(resolved), err)
	}
	if channels, err := st.Channels(t.Context()); err != nil || len(channels) != 2 {
		t.Errorf("Channels() = %d, %v, want 2", len(channels), err)
	}
	if role, err := st.Role(t.Context(), "sam@example.com"); err != nil || role != store.Viewer {
		t.Errorf("Role(sam) = %q, %v, want viewer", role, err)
	}
	enrolled, err := st.Enrollments(t.Context())
	if err != nil || len(enrolled) != 9 {
		t.Errorf("Enrollments() = %d, %v, want the 9 machines with agents", len(enrolled), err)
	}
	if access, err := st.SSHAccess(t.Context()); err != nil || len(access) != 10 {
		t.Errorf("SSHAccess() = %d, %v, want the hub and 9 more", len(access), err)
	}
	if events, err := st.Events(t.Context(), store.EventQuery{Limit: 100}); err != nil || len(events) < 20 {
		t.Errorf("Events() = %d, %v, want a log", len(events), err)
	}

	since := d.Start().Add(-24 * time.Hour)
	hist, err := st.MetricsHistory(t.Context(), store.HubID, since, time.Hour)
	if err != nil || len(hist) < 24 {
		t.Errorf("MetricsHistory(hub) = %d buckets, %v, want a day of them", len(hist), err)
	}
	// Garage Pi went offline 3 hours before the demo started, so its history
	// stops there.
	for _, p := range status(t, d).Peers {
		if p.HostName != "pi-garage" {
			continue
		}
		hist, err := st.MetricsHistory(t.Context(), p.ID, d.Start().Add(-2*time.Hour), time.Minute)
		if err != nil || len(hist) != 0 {
			t.Errorf("MetricsHistory(pi-garage) over its time offline = %d buckets, %v, want none", len(hist), err)
		}
	}
	if score, err := st.HealthHistory(t.Context(), since, time.Hour); err != nil || len(score) < 24 {
		t.Errorf("HealthHistory() = %d buckets, %v, want a day of them", len(score), err)
	}
}

func TestSeed_olderHistoryIsInBuckets(t *testing.T) {
	t.Parallel()
	d := newDemo(t, 12)
	st := openStore(t)

	if err := d.Seed(t.Context(), st, 5*24*time.Hour); err != nil {
		t.Fatal(err)
	}

	// Buckets of 5 minutes, from the buckets and then from the samples of the
	// last 48 hours, with nothing missing.
	fine, err := st.MetricsHistory(t.Context(), store.HubID, d.Start().Add(-4*24*time.Hour), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(fine); i++ {
		if gap := fine[i].Start.Sub(fine[i-1].Start); gap != 5*time.Minute {
			t.Fatalf("buckets %v and %v are %v apart, want 5 minutes", fine[i-1].Start, fine[i].Start, gap)
		}
	}
	if len(fine) < 4*24*12-2 {
		t.Errorf("%d buckets of 5 minutes in 4 days, want %d", len(fine), 4*24*12)
	}

	// A history of 4 days is an unbroken run of hours: buckets, and then samples.
	hist, err := st.MetricsHistory(t.Context(), store.HubID, d.Start().Add(-4*24*time.Hour), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(hist); i++ {
		if gap := hist[i].Start.Sub(hist[i-1].Start); gap != time.Hour {
			t.Fatalf("buckets %v and %v are %v apart, want an hour", hist[i-1].Start, hist[i].Start, gap)
		}
	}
	if len(hist) < 4*24-1 || hist[0].CPU == nil || hist[0].Disk == nil {
		t.Errorf("the hub's history has %d hours, the first %+v, want 4 days", len(hist), hist[0])
	}
	rtt, err := st.RTTHistory(t.Context(), status(t, d).Peers[0].ID, d.Start().Add(-4*24*time.Hour), time.Hour)
	if err != nil || len(rtt) < 4*24-1 {
		t.Errorf("RTTHistory() = %d hours, %v, want 4 days of them", len(rtt), err)
	}
}

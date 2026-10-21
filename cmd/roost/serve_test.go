package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/manasseh-fikadu/roost/internal/config"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/tailnet/tailnettest"
)

var laptopIP = netip.MustParseAddr("100.64.0.2")

func testConfig(t *testing.T, mode string) config.Config {
	t.Helper()
	return config.Config{
		Tailscale:   mode,
		Hostname:    config.DefaultHostname,
		SetupListen: "127.0.0.1:0",
		Port:        config.DefaultPort,
		DataDir:     filepath.Join(t.TempDir(), "data"),
	}
}

func fakeTailnet(state string) *tailnettest.Tailnet {
	return tailnettest.New(tailnet.Status{
		State:    state,
		LoginURL: "https://login.tailscale.com/a/0123456789",
		Self:     tailnet.Device{ID: "roost", DNSName: "roost.example.ts.net", IPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")}},
		Peers: []tailnet.Device{
			{ID: "laptop", DNSName: "laptop.example.ts.net", IPs: []netip.Addr{laptopIP}, Owner: "alice@example.com", Online: true},
		},
	})
}

func openFake(tn tailnet.Node) openNodeFunc {
	return func(context.Context, config.Config, *slog.Logger) (tailnet.Node, error) { return tn, nil }
}

// startServe runs runServe until the test ends, and returns its log.
func startServe(t *testing.T, cfg config.Config, tn tailnet.Node) *logWriter {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	log := newLogWriter()
	done := make(chan error, 1)
	go func() { done <- runServe(ctx, cfg, log, openFake(tn)) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("runServe() error = %v, want none; log:\n%s", err, log)
		}
	})
	return log
}

func get(t *testing.T, client *http.Client, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return res.StatusCode, string(body)
}

func TestServe_tailnet(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{config.Embedded, config.Host} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			tn := fakeTailnet(tailnet.StateRunning)
			cfg := testConfig(t, mode)
			log := startServe(t, cfg, tn)
			log.waitFor(t, "roost is running")

			code, body := get(t, tn.Client(laptopIP), tailnettest.URL+"/api/health")

			if code != http.StatusOK || !strings.Contains(body, `"status":"ok"`) {
				t.Errorf("GET /api/health on the tailnet = %d %q, want 200 and ok", code, body)
			}
			if _, err := os.Stat(filepath.Join(cfg.DataDir, "roost.db")); err != nil {
				t.Errorf("runServe() didn't create the database: %v", err)
			}
			if !strings.Contains(log.String(), "url="+tailnettest.URL) {
				t.Errorf("log doesn't give the app's URL:\n%s", log)
			}
			// Host mode has no setup page, so nothing listens off the tailnet.
			if setup := strings.Contains(log.String(), "setup page"); setup != (mode == config.Embedded) {
				t.Errorf("serving a setup page = %v in %s mode; log:\n%s", setup, mode, log)
			}
		})
	}
}

func TestServe_login(t *testing.T) {
	t.Parallel()
	tn := fakeTailnet(tailnet.StateNeedsLogin)
	log := startServe(t, testConfig(t, config.Embedded), tn)
	line := log.waitFor(t, "log Roost in to your tailnet")
	m := regexp.MustCompile(`setup_page=(\S+)`).FindStringSubmatch(line)
	if m == nil || !strings.Contains(line, "login_url=https://login.tailscale.com/a/0123456789") {
		t.Fatalf("the login line doesn't give the setup page and the login URL: %s", line)
	}
	setupURL := m[1]
	local := &http.Client{}
	if strings.Contains(log.String(), "other machines can reach the setup page") {
		t.Errorf("warned that a setup page on 127.0.0.1 can be reached from other machines; log:\n%s", log)
	}

	if code, body := get(t, local, setupURL); code != http.StatusOK || !strings.Contains(body, "Add Roost to your tailnet") {
		t.Errorf("setup page before login = %d:\n%s", code, body)
	}
	if code, _ := get(t, local, setupURL+"/api/health"); code != http.StatusNotFound {
		t.Errorf("GET /api/health on the setup address = %d, want 404", code)
	}

	tn.SetStatus(tailnet.Status{State: tailnet.StateRunning, Self: tailnet.Device{ID: "roost"}})
	log.waitFor(t, "roost is running")

	if code, body := get(t, local, setupURL); code != http.StatusOK || !strings.Contains(body, `href="`+tailnettest.URL+`"`) {
		t.Errorf("setup page after login = %d, want a link to the app:\n%s", code, body)
	}
}

func TestServe_firstAdmins(t *testing.T) {
	t.Parallel()
	const alice, bob, carol = "alice@example.com", "bob@example.com", "carol@example.com"
	tests := []struct {
		name     string
		owner    string   // of Roost's node; "" means it's tagged
		admins   []string // ROOST_ADMINS
		existing []string // admins already in the database
		want     []string
		wantLog  string
	}{
		{name: "owner of Roost's node", owner: alice, want: []string{alice}, wantLog: "made Roost's first admins"},
		{name: "owner and ROOST_ADMINS", owner: alice, admins: []string{bob}, want: []string{alice, bob}},
		{name: "tagged node uses ROOST_ADMINS", admins: []string{bob}, want: []string{bob}},
		{name: "tagged node and no ROOST_ADMINS", wantLog: "Roost has no admins"},
		{name: "already has admins", owner: alice, admins: []string{bob}, existing: []string{carol}, want: []string{carol}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := testConfig(t, config.Host)
			cfg.Admins = tt.admins
			if len(tt.existing) > 0 {
				var out bytes.Buffer
				for _, login := range tt.existing {
					if err := run(t.Context(), []string{"admin", "add", "--data-dir", cfg.DataDir, login}, &out, &out); err != nil {
						t.Fatalf("roost admin add: %v\n%s", err, &out)
					}
				}
			}
			tn := fakeTailnet(tailnet.StateRunning)
			st, _ := tn.Status(t.Context())
			if tt.owner != "" {
				st.Self.Owner = tt.owner
			} else {
				st.Self.Tags = []string{"tag:roost"}
			}
			tn.SetStatus(*st)

			log := startServe(t, cfg, tn)
			log.waitFor(t, "roost is running")

			var out bytes.Buffer
			if err := run(t.Context(), []string{"admin", "list", "--data-dir", cfg.DataDir}, &out, io.Discard); err != nil {
				t.Fatal(err)
			}
			var got []string
			for line := range strings.Lines(out.String()) {
				if login, role, _ := strings.Cut(strings.TrimSpace(line), " "); strings.TrimSpace(role) == "admin" {
					got = append(got, login)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("admins = %q, want %q", got, tt.want)
			}
			if !strings.Contains(log.String(), tt.wantLog) {
				t.Errorf("log doesn't say %q:\n%s", tt.wantLog, log)
			}
		})
	}
}

// The owner of Roost's node signs in from their own devices, and the node
// itself is anonymous.
func TestServe_signIn(t *testing.T) {
	t.Parallel()
	tn := fakeTailnet(tailnet.StateRunning)
	st, _ := tn.Status(t.Context())
	st.Self.Owner = "alice@example.com"
	tn.SetStatus(*st)
	log := startServe(t, testConfig(t, config.Host), tn)
	log.waitFor(t, "roost is running")

	code, body := get(t, tn.Client(laptopIP), tailnettest.URL+"/api/me")
	if code != http.StatusOK || !strings.Contains(body, `"login":"alice@example.com"`) || !strings.Contains(body, `"role":"admin"`) {
		t.Errorf("GET /api/me from alice's laptop = %d %s, want alice as an admin", code, body)
	}
	if code, _ := get(t, tn.Client(laptopIP), tailnettest.URL+"/api/debug/tailnet"); code != http.StatusOK {
		t.Errorf("GET /api/debug/tailnet from alice's laptop = %d, want 200", code)
	}
	self := netip.MustParseAddr("100.64.0.1")
	if code, body := get(t, tn.Client(self), tailnettest.URL+"/api/me"); !strings.Contains(body, `"reason":"roost-machine"`) {
		t.Errorf("GET /api/me from Roost's own node = %d %s, want anonymous", code, body)
	}
}

func TestServe_errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     func(*testing.T) config.Config
		open    openNodeFunc
		wantErr string // substring
	}{
		{
			name: "bad setup address",
			cfg: func(t *testing.T) config.Config {
				t.Helper()
				cfg := testConfig(t, config.Embedded)
				cfg.SetupListen = "not-an-address"
				return cfg
			},
			wantErr: "listen on not-an-address",
		},
		{
			name: "data directory is a file",
			cfg: func(t *testing.T) config.Config {
				t.Helper()
				cfg := testConfig(t, config.Embedded)
				cfg.DataDir = filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(cfg.DataDir, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				return cfg
			},
			wantErr: "roost.db",
		},
		{
			name: "Tailscale isn't running",
			cfg:  func(t *testing.T) config.Config { t.Helper(); return testConfig(t, config.Host) },
			open: func(context.Context, config.Config, *slog.Logger) (tailnet.Node, error) {
				return nil, errors.New("no tailscaled")
			},
			wantErr: "connect to the tailnet: no tailscaled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			open := tt.open
			if open == nil {
				open = func(context.Context, config.Config, *slog.Logger) (tailnet.Node, error) {
					t.Error("runServe() connected to the tailnet before failing")
					return fakeTailnet(tailnet.StateRunning), nil
				}
			}

			err := runServe(t.Context(), tt.cfg(t), io.Discard, open)

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("runServe() error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestLoopback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		addr net.Addr
		want bool
	}{
		{&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4700}, true},
		{&net.TCPAddr{IP: net.IPv6loopback, Port: 4700}, true},
		{&net.TCPAddr{IP: net.IPv4zero, Port: 4700}, false},
		{&net.TCPAddr{IP: net.IPv6unspecified, Port: 4700}, false},
		{&net.TCPAddr{IP: net.IPv4(192, 168, 1, 20), Port: 4700}, false},
		{&net.UnixAddr{Name: "/run/roost.sock", Net: "unix"}, false},
	}
	for _, tt := range tests {
		if got := loopback(tt.addr); got != tt.want {
			t.Errorf("loopback(%v) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}

func TestServe_badFlags(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	if err := serve(t.Context(), []string{"--tailscale", "funnel"}, &stderr); err == nil {
		t.Error("serve() with an unknown mode succeeded")
	}
}

// logWriter collects a log for waitFor to watch.
type logWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	changed chan struct{} // closed and replaced on each write
}

func newLogWriter() *logWriter {
	return &logWriter{changed: make(chan struct{})}
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	close(w.changed)
	w.changed = make(chan struct{})
	return w.buf.Write(p)
}

func (w *logWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// waitFor waits until a line of the log contains marker, and returns it.
func (w *logWriter) waitFor(t *testing.T, marker string) string {
	t.Helper()
	timeout := time.After(30 * time.Second)
	for {
		w.mu.Lock()
		changed := w.changed
		for line := range strings.Lines(w.buf.String()) {
			if strings.Contains(line, marker) {
				w.mu.Unlock()
				return line
			}
		}
		w.mu.Unlock()
		select {
		case <-changed:
		case <-timeout:
			t.Fatalf("the log never said %q:\n%s", marker, w)
		}
	}
}

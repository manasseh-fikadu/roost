package ssh_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/manasseh-fikadu/roost/internal/ssh"
	"github.com/manasseh-fikadu/roost/internal/ssh/sshtest"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

const admin = "alice@example.com"

var (
	roostIP  = netip.MustParseAddr("100.64.0.1")
	laptopIP = netip.MustParseAddr("100.64.0.2")
	vpsIP    = netip.MustParseAddr("100.64.0.3")
)

// env is a Manager on a tailnet with Roost's node, a Linux laptop, an
// iPhone and a Mac that's offline, and fake sshds for the laptop and for
// Roost's own machine on its loopback address.
type env struct {
	m      *ssh.Manager
	st     *store.Store
	laptop *sshtest.Server
	hub    *sshtest.Server

	mu     sync.Mutex
	status tailnet.Status
}

type envOptions struct {
	hostMode bool
	// sameMachine adds a peer that is the machine Roost runs on, as in
	// embedded mode, at vpsIP.
	sameMachine bool
	// noRun leaves Run off until the test ends, when it runs once to close
	// the connections.
	noRun bool
	// unauthorized leaves Roost's key out of the laptop's authorized_keys.
	unauthorized bool
}

// newEnv makes an env and runs its Manager until the test ends. Call it in
// a synctest bubble.
func newEnv(t *testing.T, opts envOptions) *env {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "roost.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// Let the prune that Open starts finish, so it doesn't hold the store.
	synctest.Wait()

	e := &env{st: st, laptop: sshtest.New(), hub: sshtest.New()}
	e.status = tailnet.Status{
		State: tailnet.StateRunning,
		Self:  tailnet.Device{ID: "roost", DNSName: "roost.example.ts.net", OS: "linux", IPs: []netip.Addr{roostIP}, Online: true},
		Peers: []tailnet.Device{
			{ID: "laptop", HostName: "laptop", OS: "linux", IPs: []netip.Addr{laptopIP}, Online: true},
			{ID: "phone", HostName: "phone", OS: "iOS", IPs: []netip.Addr{netip.MustParseAddr("100.64.0.4")}, Online: true},
			{ID: "mac", HostName: "mac", OS: "macOS", IPs: []netip.Addr{netip.MustParseAddr("100.64.0.5")}},
		},
	}
	if opts.sameMachine {
		e.status.Peers = append(e.status.Peers,
			tailnet.Device{ID: "vps", HostName: "vps", OS: "linux", IPs: []netip.Addr{vpsIP}, Online: true, SameMachine: true})
	}
	t.Cleanup(e.laptop.Close)
	t.Cleanup(e.hub.Close)
	e.m = ssh.NewManager(ssh.Options{
		KeyDir: t.TempDir(),
		Store:  st,
		Tailnet: func() tailnet.Snapshot {
			e.mu.Lock()
			defer e.mu.Unlock()
			st := e.status
			return tailnet.Snapshot{Status: &st}
		},
		// On the tailnet, the hub machine answers at its tailnet address.
		Dial:      sshtest.Route(map[string]*sshtest.Server{laptopIP.String(): e.laptop, roostIP.String(): e.hub, vpsIP.String(): e.hub}),
		DialLocal: sshtest.Route(map[string]*sshtest.Server{"127.0.0.1": e.hub}),
		HostMode:  opts.hostMode,
	})
	if err := e.m.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	key, err := e.m.Key()
	if err != nil {
		t.Fatal(err)
	}
	if !opts.unauthorized {
		e.laptop.Authorize(key.PublicKey())
	}
	e.hub.Authorize(key.PublicKey())
	if opts.noRun {
		// e.m may be another Manager by then.
		t.Cleanup(func() { e.m.Run(ended(t)) })
	} else {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			e.m.Run(ctx)
			close(done)
		}()
		t.Cleanup(func() {
			cancel()
			<-done
		})
	}
	return e
}

// setOnline changes whether a peer is online.
func (e *env) setOnline(id string, online bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.status.Peers {
		if e.status.Peers[i].ID == id {
			e.status.Peers[i].Online = online
		}
	}
}

func (e *env) setUp(t *testing.T, deviceID, user string) {
	t.Helper()
	if _, err := e.m.SetUp(t.Context(), deviceID, user, ssh.DefaultPort, admin); err != nil {
		t.Fatal(err)
	}
}

// ok makes s run every command, doing nothing.
func ok(s *sshtest.Server) {
	s.Handle(func(*sshtest.Exec) int { return 0 })
}

// echo makes s answer each command with the user and the command.
func echo(s *sshtest.Server) {
	s.Handle(func(x *sshtest.Exec) int {
		_, _ = fmt.Fprintf(x.Stdout, "%s ran %s", x.User, x.Command)
		_, _ = fmt.Fprint(x.Stderr, "some warning")
		return 3
	})
}

func TestManager_exec(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{noRun: true})
		ctx := t.Context()
		echo(e.laptop)
		a, err := e.m.SetUp(ctx, "laptop", "alice", ssh.DefaultPort, admin)
		if err != nil {
			t.Fatal(err)
		}
		if a.User != "alice" || a.Port != 22 || a.State != ssh.StateUnchecked || a.HostKey != "" {
			t.Errorf("SetUp = %+v, want alice on port 22, unchecked, with no host key yet", a)
		}

		for range 2 {
			res, err := e.m.Exec(ctx, "laptop", `echo "$1"`, "it's")
			if err != nil {
				t.Fatal(err)
			}
			want := `alice ran exec sh -c 'PATH="$PATH:/usr/local/bin:/opt/homebrew/bin:/opt/local/bin:/home/linuxbrew/.linuxbrew/bin:$HOME/.nix-profile/bin"; echo "$1"' sh 'it'\''s'`
			if res.Code != 3 || string(res.Stdout) != want || string(res.Stderr) != "some warning" {
				t.Errorf("Exec = %d, %q, %q; want 3, %q, some warning", res.Code, res.Stdout, res.Stderr, want)
			}
		}
		// Both ran on one connection, which pinned the host key.
		if n := e.laptop.Accepted(); n != 1 {
			t.Errorf("%d connections, want 1", n)
		}
		if got, want := e.laptop.Dialed(), []string{"100.64.0.2:22"}; !slices.Equal(got, want) {
			t.Errorf("dialed %q, want %q", got, want)
		}
		got := e.m.Access()["laptop"]
		if want := gossh.FingerprintSHA256(e.laptop.HostKey()); got.HostKey != want || got.State != ssh.StateOK {
			t.Errorf("access = %+v, want the host key %s pinned and ok", got, want)
		}
		saved, err := e.st.SSHAccess(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if s := saved["laptop"]; s.User != "alice" || s.CreatedBy != admin || !strings.HasPrefix(s.HostKey, "ssh-ed25519 ") {
			t.Errorf("saved %+v, want alice, set up by %s, with an ed25519 host key", s, admin)
		}
	})
}

func TestManager_closesIdleConnections(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{})
		ok(e.laptop)
		e.laptop.HandleSocket("/run/app.sock", func(c net.Conn) { _, _ = io.Copy(io.Discard, c) })
		e.setUp(t, "laptop", "alice")
		synctest.Wait() // for the first check

		conn, err := e.m.DialUnix(t.Context(), "laptop", "/run/app.sock")
		if err != nil {
			t.Fatal(err)
		}
		// A socket that's open keeps the connection open.
		time.Sleep(3 * ssh.IdleTimeout)
		synctest.Wait()
		if n := e.laptop.Conns(); n != 1 {
			t.Fatalf("%d connections open while a socket is, want 1", n)
		}
		_ = conn.Close()
		time.Sleep(ssh.IdleTimeout - time.Minute)
		synctest.Wait()
		if n := e.laptop.Conns(); n != 1 {
			t.Fatalf("%d connections open after %v idle, want 1", n, ssh.IdleTimeout-time.Minute)
		}
		time.Sleep(2 * time.Minute)
		synctest.Wait()
		if n := e.laptop.Conns(); n != 0 {
			t.Errorf("%d connections open after %v idle, want 0", n, ssh.IdleTimeout+time.Minute)
		}
	})
}

func TestManager_DialUnixNoSocket(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{})
		ok(e.laptop)
		e.setUp(t, "laptop", "alice")
		synctest.Wait()

		_, err := e.m.DialUnix(t.Context(), "laptop", "/run/nothing.sock")
		var se *ssh.SocketError
		if !errors.As(err, &se) || se.Path != "/run/nothing.sock" {
			t.Fatalf("DialUnix() error = %v, want a SocketError for the path", err)
		}
		if _, err := e.m.DialUnix(t.Context(), "nobody", "/run/nothing.sock"); !errors.Is(err, ssh.ErrNoDevice) {
			t.Errorf("DialUnix() for another device error = %v, want ErrNoDevice", err)
		}
		// The failed attempt let go of the connection, so it can idle out.
		time.Sleep(2 * ssh.IdleTimeout)
		synctest.Wait()
		if n := e.laptop.Conns(); n != 0 {
			t.Errorf("%d connections open after the failed dial and %v idle, want 0", n, 2*ssh.IdleTimeout)
		}
	})
}

func TestManager_reconnectsAfterDrop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{})
		ok(e.laptop)
		e.setUp(t, "laptop", "alice")
		synctest.Wait()
		e.laptop.Drop()
		synctest.Wait()
		if _, err := e.m.Exec(t.Context(), "laptop", "true"); err != nil {
			t.Errorf("Exec after the connection dropped: %v", err)
		}
	})
}

func TestManager_keepalive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{})
		ok(e.laptop)
		e.setUp(t, "laptop", "alice")
		synctest.Wait()
		// A connection that answers its keepalives stays.
		time.Sleep(time.Minute)
		synctest.Wait()
		if n := e.laptop.Conns(); n != 1 {
			t.Errorf("%d connections, want 1", n)
		}
	})
}

func TestManager_hostKeyChanged(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{})
		ok(e.laptop)
		e.setUp(t, "laptop", "alice")
		synctest.Wait()
		first := gossh.FingerprintSHA256(e.laptop.HostKey())

		// The machine is reinstalled.
		offered := gossh.FingerprintSHA256(e.laptop.ChangeHostKey())
		e.laptop.Drop()
		synctest.Wait()
		for range 2 {
			if _, err := e.m.Exec(t.Context(), "laptop", "true"); !errors.Is(err, ssh.ErrHostKeyChange) {
				t.Fatalf("Exec = %v, want ErrHostKeyChange", err)
			}
		}
		a := e.m.Access()["laptop"]
		if a.State != ssh.StateHostKeyChanged || a.HostKey != first || a.OfferedKey != offered ||
			!strings.Contains(a.Error, first) || !strings.Contains(a.Error, offered) {
			t.Errorf("access = %+v, want the host key changed from %s to %s", a, first, offered)
		}
		events, err := e.st.Events(t.Context(), store.EventQuery{Kind: "ssh", Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 1 || events[0].Level != "error" || events[0].DeviceID != "laptop" {
			t.Errorf("events = %+v, want one error about the laptop", events)
		}
		// Run doesn't try again by itself.
		time.Sleep(time.Hour)
		synctest.Wait()
		if a := e.m.Access()["laptop"]; a.State != ssh.StateHostKeyChanged {
			t.Errorf("an hour later, the state is %s, want %s", a.State, ssh.StateHostKeyChanged)
		}

		a, err = e.m.ResetHostKey(t.Context(), "laptop")
		if err != nil {
			t.Fatal(err)
		}
		if a.HostKey != "" || a.State != ssh.StateUnchecked {
			t.Errorf("ResetHostKey = %+v, want no host key, unchecked", a)
		}
		synctest.Wait() // Run checks it again.
		if a := e.m.Access()["laptop"]; a.HostKey != offered || a.State != ssh.StateOK {
			t.Errorf("after the reset, access = %+v, want the new key %s pinned and ok", a, offered)
		}
	})
}

func TestManager_trustHostKey(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{})
		ok(e.laptop)
		e.setUp(t, "laptop", "alice")
		synctest.Wait()
		first := e.m.Access()["laptop"].HostKey

		if _, err := e.m.TrustHostKey(t.Context(), "laptop", "SHA256:anything"); !errors.Is(err, ssh.ErrNoChangedKey) {
			t.Errorf("TrustHostKey before the key changed = %v, want ErrNoChangedKey", err)
		}

		// The machine is reinstalled, and an admin looks at the key it offers.
		seen := gossh.FingerprintSHA256(e.laptop.ChangeHostKey())
		e.laptop.Drop()
		synctest.Wait()
		if _, err := e.m.Exec(t.Context(), "laptop", "true"); !errors.Is(err, ssh.ErrHostKeyChange) {
			t.Fatalf("Exec = %v, want ErrHostKeyChange", err)
		}
		if a := e.m.Access()["laptop"]; a.OfferedKey != seen {
			t.Fatalf("offered key = %s, want %s", a.OfferedKey, seen)
		}

		// It offers another key by the time Roost sees it again, so what the
		// admin looked at isn't what they'd trust.
		other := gossh.FingerprintSHA256(e.laptop.ChangeHostKey())
		if _, err := e.m.Exec(t.Context(), "laptop", "true"); !errors.Is(err, ssh.ErrHostKeyChange) {
			t.Fatalf("Exec = %v, want ErrHostKeyChange", err)
		}
		if _, err := e.m.TrustHostKey(t.Context(), "laptop", seen); !errors.Is(err, ssh.ErrOfferedKeyChanged) {
			t.Errorf("TrustHostKey of the key that was seen = %v, want ErrOfferedKeyChanged", err)
		}
		if a := e.m.Access()["laptop"]; a.HostKey != first || a.State != ssh.StateHostKeyChanged {
			t.Errorf("after the refusal, access = %+v, want the first key still pinned, and the change still refused", a)
		}

		// Trusting the key that's on the screen pins exactly that.
		a, err := e.m.TrustHostKey(t.Context(), "laptop", other)
		if err != nil {
			t.Fatal(err)
		}
		if a.HostKey != other {
			t.Errorf("TrustHostKey pinned %s, want %s", a.HostKey, other)
		}
		synctest.Wait() // Run logs in again, with the key checked against the one pinned.
		if a := e.m.Access()["laptop"]; a.State != ssh.StateOK || a.HostKey != other {
			t.Errorf("after trusting, access = %+v, want ok, with %s pinned", a, other)
		}
		saved, err := e.st.SSHAccess(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if got := saved["laptop"]; !strings.HasPrefix(got.HostKey, "ssh-ed25519 ") || fingerprintOf(t, got.HostKey) != other {
			t.Errorf("saved host key %q, want the ed25519 key %s", got.HostKey, other)
		}
		if _, err := e.m.TrustHostKey(t.Context(), "laptop", other); !errors.Is(err, ssh.ErrNoChangedKey) {
			t.Errorf("TrustHostKey again = %v, want ErrNoChangedKey", err)
		}
	})
}

// Trusting a key pins the one an admin looked at, not whichever the machine
// offers by the time Roost logs in again, which Roost then refuses.
func TestManager_trustHostKey_pinsTheKeyThatWasSeen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{})
		ok(e.laptop)
		e.setUp(t, "laptop", "alice")
		synctest.Wait()

		seen := gossh.FingerprintSHA256(e.laptop.ChangeHostKey())
		e.laptop.Drop()
		synctest.Wait()
		if _, err := e.m.Exec(t.Context(), "laptop", "true"); !errors.Is(err, ssh.ErrHostKeyChange) {
			t.Fatalf("Exec = %v, want ErrHostKeyChange", err)
		}
		// Another machine starts answering, and Roost hasn't seen its key.
		later := gossh.FingerprintSHA256(e.laptop.ChangeHostKey())

		a, err := e.m.TrustHostKey(t.Context(), "laptop", seen)
		if err != nil {
			t.Fatal(err)
		}
		if a.HostKey != seen {
			t.Errorf("TrustHostKey pinned %s, want %s, the key that was seen", a.HostKey, seen)
		}
		synctest.Wait()
		if a := e.m.Access()["laptop"]; a.State != ssh.StateHostKeyChanged || a.HostKey != seen || a.OfferedKey != later {
			t.Errorf("access = %+v, want %s pinned, and %s refused", a, seen, later)
		}
	})
}

func fingerprintOf(t *testing.T, authorizedKey string) string {
	t.Helper()
	pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(authorizedKey))
	if err != nil {
		t.Fatal(err)
	}
	return gossh.FingerprintSHA256(pub)
}

func TestManager_pinsEd25519(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{noRun: true})
		ok(e.laptop)
		// sshd has a key of each type, and Go's client would pick ECDSA.
		ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		e.laptop.AddHostKey(ecdsaKey)
		e.setUp(t, "laptop", "alice")
		if _, err := e.m.Exec(t.Context(), "laptop", "true"); err != nil {
			t.Fatal(err)
		}
		if got, want := e.m.Access()["laptop"].HostKey, gossh.FingerprintSHA256(e.laptop.HostKey()); got != want {
			t.Errorf("pinned %s, want the ed25519 key %s, as ssh would", got, want)
		}
	})
}

func TestManager_connectErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts envOptions
		// refuse makes nothing listen on the laptop.
		refuse bool
		want   string
	}{
		{
			name: "key not authorized",
			opts: envOptions{noRun: true, unauthorized: true},
			want: "Roost's key isn't in the authorized_keys of alice on laptop",
		},
		{
			name:   "nothing listening",
			opts:   envOptions{noRun: true},
			refuse: true,
			want:   "nothing is listening for SSH on port 22 of laptop",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := newEnv(t, tt.opts)
				e.laptop.Refuse(tt.refuse)
				e.setUp(t, "laptop", "alice")
				_, err := e.m.Exec(t.Context(), "laptop", "true")
				var ce *ssh.ConnectError
				if !errors.As(err, &ce) || ce.Msg != tt.want {
					t.Errorf("Exec = %v, want %q", err, tt.want)
				}
				if a := e.m.Access()["laptop"]; a.State != ssh.StateFailing || a.Error != tt.want {
					t.Errorf("access = %+v, want failing with %q", a, tt.want)
				}
			})
		})
	}
}

func TestManager_devices(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{noRun: true})
		for _, tt := range []struct {
			id   string
			want error
		}{
			{"nobody", ssh.ErrNoDevice},
			{"phone", ssh.ErrUnsupported},
			{"mac", ssh.ErrOffline},
			{"laptop", ssh.ErrNotSetUp},
		} {
			if _, err := e.m.Exec(t.Context(), tt.id, "true"); !errors.Is(err, tt.want) {
				t.Errorf("Exec on %s = %v, want %v", tt.id, err, tt.want)
			}
		}
		// A machine that's offline can be set up, and removed.
		if _, err := e.m.SetUp(t.Context(), "mac", "alice", 2222, admin); err != nil {
			t.Errorf("SetUp on an offline Mac: %v", err)
		}
		if err := e.m.Remove(t.Context(), "mac"); err != nil {
			t.Errorf("Remove on an offline Mac: %v", err)
		}
		if err := e.m.Remove(t.Context(), "mac"); !errors.Is(err, ssh.ErrNotSetUp) {
			t.Errorf("Remove again = %v, want ErrNotSetUp", err)
		}
		for _, tt := range []struct {
			user string
			port int
			want error
		}{
			{"root; reboot", 22, ssh.ErrBadUser},
			{"", 22, ssh.ErrBadUser},
			{"alice", 0, ssh.ErrBadPort},
			{"alice", 65536, ssh.ErrBadPort},
		} {
			if _, err := e.m.SetUp(t.Context(), "laptop", tt.user, tt.port, admin); !errors.Is(err, tt.want) {
				t.Errorf("SetUp(%q, %d) = %v, want %v", tt.user, tt.port, err, tt.want)
			}
		}
	})
}

func TestManager_changePort(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{})
		ok(e.laptop)
		e.setUp(t, "laptop", "alice")
		synctest.Wait()
		pinned := e.m.Access()["laptop"].HostKey
		// Another user keeps the key, and another port forgets it.
		a, err := e.m.SetUp(t.Context(), "laptop", "bob", 22, admin)
		if err != nil || a.HostKey != pinned {
			t.Errorf("SetUp as bob = %+v, %v, want the host key kept", a, err)
		}
		a, err = e.m.SetUp(t.Context(), "laptop", "bob", 2222, admin)
		if err != nil || a.HostKey != "" {
			t.Errorf("SetUp on port 2222 = %+v, %v, want no host key", a, err)
		}
		synctest.Wait()
		if got := e.laptop.Dialed(); got[len(got)-1] != "100.64.0.2:2222" {
			t.Errorf("dialed %q, want port 2222 last", got)
		}
		if got := e.laptop.Logins(); got[len(got)-1] != "bob" {
			t.Errorf("logged in as %q, want bob last", got)
		}
	})
}

func TestManager_hub(t *testing.T) {
	for _, tt := range []struct {
		name     string
		opts     envOptions
		deviceID string
		// refuse makes the loopback address refuse, so Roost tries the
		// machine's tailnet address.
		refuse bool
		want   []string
	}{
		{name: "host mode", opts: envOptions{hostMode: true}, deviceID: "roost", want: []string{"127.0.0.1:22"}},
		{
			name: "host mode, sshd on the tailnet only", opts: envOptions{hostMode: true}, deviceID: "roost", refuse: true,
			want: []string{"127.0.0.1:22", "100.64.0.1:22"},
		},
		{name: "embedded", deviceID: "roost", want: []string{"127.0.0.1:22"}},
		{name: "embedded, refused", deviceID: "roost", refuse: true, want: []string{"127.0.0.1:22"}},
		{
			name: "embedded, the machine's node", opts: envOptions{sameMachine: true}, deviceID: "vps", refuse: true,
			want: []string{"127.0.0.1:22", "100.64.0.3:22"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tt.opts.noRun = true
				e := newEnv(t, tt.opts)
				ok(e.hub)
				var dialed []string
				local := func(ctx context.Context, network, addr string) (net.Conn, error) {
					dialed = append(dialed, addr)
					if tt.refuse {
						// As tsnet's netstack says it.
						return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
					}
					return e.hub.Dial(ctx, network, addr)
				}
				e.m = ssh.NewManager(ssh.Options{
					KeyDir: t.TempDir(), Store: e.st, HostMode: tt.opts.hostMode,
					Tailnet: func() tailnet.Snapshot {
						st := e.status
						return tailnet.Snapshot{Status: &st}
					},
					Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
						dialed = append(dialed, addr)
						return e.hub.Dial(ctx, network, addr)
					},
					DialLocal: local,
				})
				key, err := e.m.Key()
				if err != nil {
					t.Fatal(err)
				}
				e.hub.Authorize(key.PublicKey())
				e.setUp(t, tt.deviceID, "me")
				_, err = e.m.Exec(t.Context(), tt.deviceID, "true")
				if wantErr := tt.refuse && len(tt.want) == 1; (err != nil) != wantErr {
					t.Errorf("Exec = %v, want an error: %v", err, wantErr)
				}
				if !slices.Equal(dialed, tt.want) {
					t.Errorf("dialed %q, want %q", dialed, tt.want)
				}
				// Both of the machine's nodes share its access.
				if _, ok := e.m.Access()[store.HubID]; !ok {
					t.Errorf("access = %v, want it under %s", e.m.Access(), store.HubID)
				}
				setup, err := e.m.Setup(tt.deviceID)
				if err != nil {
					t.Fatal(err)
				}
				if !setup.Hub || !strings.HasPrefix(setup.AuthorizedKey, `from="127.0.0.1,::1,100.64.0.1",`) {
					t.Errorf("setup = %+v, want the hub's, from the loopback and Roost's addresses", setup)
				}
			})
		})
	}
}

// ended returns a context that has ended.
func ended(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}

func TestManager_setup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{noRun: true})
		s, err := e.m.Setup("laptop")
		if err != nil {
			t.Fatal(err)
		}
		key, err := e.m.Key()
		if err != nil {
			t.Fatal(err)
		}
		want := `from="100.64.0.1",no-agent-forwarding,no-X11-forwarding ` + key.PublicKey()
		if s.Hub || s.AuthorizedKey != want || s.Command != ssh.SetupCommand(want) {
			t.Errorf("Setup = %+v, want %q for a peer", s, want)
		}
		if !strings.HasSuffix(key.PublicKey(), " roost@roost") {
			t.Errorf("key = %q, want it named after Roost's node", key.PublicKey())
		}
		if _, err := e.m.Setup("phone"); !errors.Is(err, ssh.ErrUnsupported) {
			t.Errorf("Setup on a phone = %v, want ErrUnsupported", err)
		}
		if _, err := e.m.Setup("mac"); err != nil {
			t.Errorf("Setup on an offline Mac = %v, want it to work", err)
		}
		if _, _, err := e.m.AuthorizeHere(t.Context(), "laptop", admin); !errors.Is(err, ssh.ErrNotHub) {
			t.Errorf("AuthorizeHere on the laptop = %v, want ErrNotHub", err)
		}
	})
}

// dockerPing serves Docker's /_ping on each connection.
func dockerPing(c net.Conn) {
	req, err := http.ReadRequest(bufio.NewReader(c))
	if err != nil || req.URL.Path != "/_ping" {
		return
	}
	_, _ = io.WriteString(c, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK")
}

func TestManager_check(t *testing.T) {
	for _, tt := range []struct {
		name   string
		out    string
		socket bool
		tmux   bool
		docker bool
	}{
		{name: "everything", out: "tmux\ndocker=/var/run/docker.sock\n", socket: true, tmux: true, docker: true},
		{name: "no access to the socket", out: "tmux\ndocker=/var/run/docker.sock\n", tmux: true},
		{name: "nothing", out: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := newEnv(t, envOptions{noRun: true})
				e.laptop.Handle(func(x *sshtest.Exec) int {
					if !strings.Contains(x.Command, "docker.sock") {
						return 1
					}
					_, _ = io.WriteString(x.Stdout, tt.out)
					return 0
				})
				if tt.socket {
					e.laptop.HandleSocket("/var/run/docker.sock", dockerPing)
				}
				e.setUp(t, "laptop", "alice")
				a, err := e.m.Check(t.Context(), "laptop")
				if err != nil {
					t.Fatal(err)
				}
				if a.State != ssh.StateOK || a.Tmux == nil || *a.Tmux != tt.tmux || a.Docker == nil || *a.Docker != tt.docker ||
					!a.CheckedAt.Equal(time.Now()) {
					t.Errorf("Check = %+v, want ok, tmux %v, docker %v, checked now", a, tt.tmux, tt.docker)
				}
			})
		})
	}
}

func TestManager_run(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{})
		var checks atomic.Int32
		e.laptop.Handle(func(*sshtest.Exec) int {
			checks.Add(1)
			return 0
		})
		e.laptop.Refuse(true)
		e.setOnline("laptop", false)
		e.setUp(t, "laptop", "alice")
		// An offline machine waits.
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		if n := len(e.laptop.Dialed()); n != 0 {
			t.Fatalf("dialed an offline machine %d times", n)
		}
		e.setOnline("laptop", true)
		time.Sleep(time.Minute)
		synctest.Wait()
		if a := e.m.Access()["laptop"]; a.State != ssh.StateFailing {
			t.Fatalf("access = %+v, want failing", a)
		}
		// A failed check is tried again after five minutes.
		e.laptop.Refuse(false)
		time.Sleep(4 * time.Minute)
		synctest.Wait()
		if a := e.m.Access()["laptop"]; a.State != ssh.StateFailing {
			t.Fatalf("after 4 minutes, access = %+v, want failing still", a)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if a := e.m.Access()["laptop"]; a.State != ssh.StateOK || checks.Load() != 1 {
			t.Fatalf("after 5 minutes, access = %+v and %d checks, want ok after one", a, checks.Load())
		}
		// One that works is checked again after an hour.
		time.Sleep(time.Hour)
		synctest.Wait()
		if n := checks.Load(); n != 2 {
			t.Errorf("%d checks after an hour, want 2", n)
		}
	})
}

// A terminal that logs in to a machine whose last check failed says Roost
// can log in, but not what the machine offers, so the machine is checked
// again soon, not an hour later.
func TestManager_checksAfterALoginThatMendsAFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{unauthorized: true})
		e.laptop.Handle(func(x *sshtest.Exec) int {
			if x.Pty != nil {
				_, _ = io.Copy(io.Discard, x.Stdin)
			}
			return 0
		})
		e.setUp(t, "laptop", "alice")
		synctest.Wait()
		if a := e.m.Access()["laptop"]; a.State != ssh.StateFailing || a.Tmux != nil {
			t.Fatalf("access = %+v, want failing with nothing found", a)
		}

		// The admin adds the key, and opens a terminal before Run checks again.
		key, err := e.m.Key()
		if err != nil {
			t.Fatal(err)
		}
		e.laptop.Authorize(key.PublicKey())
		term, err := e.m.OpenTerminal(t.Context(), "laptop", ssh.TerminalOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = term.Close() }()
		synctest.Wait()
		if a := e.m.Access()["laptop"]; a.State != ssh.StateOK || a.Tmux == nil || a.Docker == nil {
			t.Errorf("access = %+v, want ok, with tmux and Docker found by a check", a)
		}
	})
}

func TestManager_changes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{noRun: true})
		select {
		case <-e.m.Changes():
			t.Fatal("a change before any")
		default:
		}
		e.setUp(t, "laptop", "alice")
		select {
		case <-e.m.Changes():
		default:
			t.Error("no change after SetUp")
		}
	})
}

func TestManager_terminal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{})
		ok(e.laptop)
		e.setUp(t, "laptop", "alice")
		synctest.Wait()
		execs := make(chan *sshtest.Exec, 1)
		e.laptop.Handle(func(x *sshtest.Exec) int {
			execs <- x
			sc := bufio.NewScanner(x.Stdin)
			for sc.Scan() {
				if sc.Text() == "exit" {
					return 7
				}
				_, _ = fmt.Fprintf(x.Stdout, "you said %s\n", sc.Text())
			}
			return 0
		})

		before := e.laptop.Accepted()
		term, err := e.m.OpenTerminal(t.Context(), "laptop", ssh.TerminalOptions{Session: "web-1", Dir: "/srv/app", Cols: 100, Rows: 30})
		if err != nil {
			t.Fatal(err)
		}
		x := <-execs
		if x.Pty == nil || *x.Pty != (sshtest.Pty{Term: "xterm-256color", Cols: 100, Rows: 30}) {
			t.Errorf("pty = %+v, want xterm-256color, 100×30", x.Pty)
		}
		if !strings.Contains(x.Command, "tmux new-session") || !strings.HasSuffix(x.Command, " sh 'web-1' '/srv/app'") {
			t.Errorf("command = %s, want tmux with the session and directory", x.Command)
		}
		// Each terminal has a connection of its own.
		if n := e.laptop.Accepted() - before; n != 1 {
			t.Errorf("the terminal made %d connections, want 1", n)
		}

		if err := term.Resize(120, 1000); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		select {
		case got := <-x.Resized:
			if want := (sshtest.Size{Cols: 120, Rows: ssh.MaxRows}); got != want {
				t.Errorf("resized to %+v, want %+v", got, want)
			}
		default:
			t.Error("no resize")
		}
		if _, err := io.WriteString(term, "hello\nexit\n"); err != nil {
			t.Fatal(err)
		}
		out, err := io.ReadAll(term)
		if err != nil || string(out) != "you said hello\n" {
			t.Errorf("output = %q, %v, want %q", out, err, "you said hello\n")
		}
		if code, ok := term.Wait(); !ok || code != 7 {
			t.Errorf("exit status %d, %v, want 7", code, ok)
		}
		_ = term.Close()
	})
}

func TestManager_removeClosesTerminals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{})
		e.laptop.Handle(func(x *sshtest.Exec) int {
			_, _ = io.Copy(io.Discard, x.Stdin)
			return 0
		})
		e.setUp(t, "laptop", "alice")
		synctest.Wait()
		term, err := e.m.OpenTerminal(t.Context(), "laptop", ssh.TerminalOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := e.m.Remove(t.Context(), "laptop"); err != nil {
			t.Fatal(err)
		}
		if code, ok := term.Wait(); ok {
			t.Errorf("exit status %d, want none for a closed connection", code)
		}
		_ = term.Close()
		synctest.Wait()
		if n := e.laptop.Conns(); n != 0 {
			t.Errorf("%d connections open after Remove, want 0", n)
		}
	})
}

func TestManager_sessions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{})
		var out string
		var code int
		e.laptop.Handle(func(x *sshtest.Exec) int {
			switch {
			case strings.Contains(x.Command, "list-sessions"):
				_, _ = io.WriteString(x.Stdout, out)
				return code
			case strings.Contains(x.Command, "kill-session"):
				if !strings.HasSuffix(x.Command, " sh 'web-1'") {
					_, _ = io.WriteString(x.Stderr, "can't find session: =gone\n")
					return 1
				}
			}
			return 0
		})
		e.setUp(t, "laptop", "alice")
		synctest.Wait()

		out = "web-2:1700000000:1700000600:0:1:bash\nmain:1700000100:1700000200:2:3:ssh me@host:22\n"
		got, err := e.m.Sessions(t.Context(), "laptop")
		if err != nil {
			t.Fatal(err)
		}
		want := []ssh.Session{
			{Name: "main", Created: time.Unix(1700000100, 0), Activity: time.Unix(1700000200, 0), Attached: 2, Windows: 3, Command: "ssh me@host:22"},
			{Name: "web-2", Created: time.Unix(1700000000, 0), Activity: time.Unix(1700000600, 0), Windows: 1, Command: "bash"},
		}
		if !slices.EqualFunc(got, want, func(a, b ssh.Session) bool {
			return a.Name == b.Name && a.Created.Equal(b.Created) && a.Activity.Equal(b.Activity) &&
				a.Attached == b.Attached && a.Windows == b.Windows && a.Command == b.Command
		}) {
			t.Errorf("Sessions = %+v, want %+v", got, want)
		}
		out = ""
		if got, err := e.m.Sessions(t.Context(), "laptop"); err != nil || len(got) != 0 || got == nil {
			t.Errorf("Sessions with none = %#v, %v, want an empty list", got, err)
		}
		code = 127
		if _, err := e.m.Sessions(t.Context(), "laptop"); !errors.Is(err, ssh.ErrNoTmux) {
			t.Errorf("Sessions without tmux = %v, want ErrNoTmux", err)
		}

		if err := e.m.KillSession(t.Context(), "laptop", "web-1"); err != nil {
			t.Errorf("KillSession = %v", err)
		}
		if err := e.m.KillSession(t.Context(), "laptop", "gone"); !errors.Is(err, ssh.ErrNoSession) {
			t.Errorf("KillSession on a session that's gone = %v, want ErrNoSession", err)
		}
		if err := e.m.KillSession(t.Context(), "laptop", "a.b"); !errors.Is(err, ssh.ErrBadSession) {
			t.Errorf("KillSession with a dot = %v, want ErrBadSession", err)
		}
	})
}

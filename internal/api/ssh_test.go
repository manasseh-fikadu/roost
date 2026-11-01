package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coder/websocket"
	gossh "golang.org/x/crypto/ssh"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/ssh"
	"github.com/manasseh-fikadu/bield/internal/ssh/sshtest"
	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/tailnet/tailnettest"
)

// sshEnv is the API on the test tailnet, where alice's laptop is a Mac
// with a fake sshd that has Bield's key in its authorized_keys.
type sshEnv struct {
	tn   *tailnettest.Tailnet
	st   *store.Store
	opts api.Options
	h    http.Handler
	sshd *sshtest.Server
	// served reports whether the API is served on tn.
	served bool
}

// newSSHEnv makes an sshEnv. In a synctest bubble, call it after opening
// st there.
func newSSHEnv(t *testing.T, st *store.Store, stopping <-chan struct{}) *sshEnv {
	t.Helper()
	tn := testTailnet()
	status, err := tn.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i := range status.Peers {
		switch status.Peers[i].ID {
		case "laptop":
			status.Peers[i].OS = "macOS"
		case "phone":
			status.Peers[i].OS = "iOS"
		}
	}
	tn.SetStatus(*status)
	e := &sshEnv{tn: tn, st: st, sshd: sshtest.New()}
	tn.SetDialer(sshtest.Route(map[string]*sshtest.Server{laptopIP.String(): e.sshd}))
	t.Cleanup(e.sshd.Close)
	e.opts = withStore(t, tn, st, api.Options{Stopping: stopping})
	t.Cleanup(func() {
		// Run closes every connection when it stops.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		e.opts.SSH.Run(ctx)
	})
	key, err := e.opts.SSH.Key()
	if err != nil {
		t.Fatal(err)
	}
	e.sshd.Authorize(key.PublicKey())
	// Checks pass, and terminals echo their input until "exit".
	e.sshd.Handle(func(x *sshtest.Exec) int {
		if x.Pty == nil {
			return 0
		}
		sc := bufio.NewScanner(x.Stdin)
		for sc.Scan() {
			if sc.Text() == "exit" {
				return 5
			}
			_, _ = fmt.Fprintf(x.Stdout, "you said %s\n", sc.Text())
		}
		return 0
	})
	e.h = api.New(e.opts)
	return e
}

func (e *sshEnv) setUp(t *testing.T) {
	t.Helper()
	if _, err := e.opts.SSH.SetUp(t.Context(), "laptop", "alice", 22, alice); err != nil {
		t.Fatal(err)
	}
}

// events returns the activity log's events of a kind, newest first.
func (e *sshEnv) events(t *testing.T, kind api.EventKind) []store.Event {
	t.Helper()
	events, err := e.st.Events(t.Context(), store.EventQuery{Kind: string(kind), Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestSSH_setUp(t *testing.T) {
	t.Parallel()
	e := newSSHEnv(t, testStore(t), nil)

	rec := send(t, e.h, http.MethodPut, "/api/devices/laptop/ssh", laptopIP, `{"user":"alice"}`)
	var got api.DeviceSSH
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("PUT = %d %s, want 200", rec.Code, rec.Body)
	}
	if want := (api.DeviceSSH{State: api.SSHStateUnchecked, User: "alice", Port: 22}); got.State != want.State ||
		got.User != want.User || got.Port != want.Port || got.Hub {
		t.Errorf("PUT = %+v, want %+v", got, want)
	}
	rec = send(t, e.h, http.MethodPut, "/api/devices/laptop/ssh", laptopIP, `{"user":"bob","port":2222}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT again = %d %s, want 200", rec.Code, rec.Body)
	}
	var msgs []string
	for _, ev := range e.events(t, api.EventKindSSH) {
		if ev.Actor != alice || ev.DeviceID != "laptop" {
			t.Errorf("event %+v, want one by alice about the laptop", ev)
		}
		msgs = append(msgs, ev.Message)
	}
	if want := []string{"Changed SSH access to laptop: bob, port 2222", "Set up SSH access to laptop as alice"}; strings.Join(msgs, "|") != strings.Join(want, "|") {
		t.Errorf("events = %q, want %q", msgs, want)
	}

	for _, tt := range []struct {
		name, id, body string
		from           netip.Addr
		want           int
	}{
		{name: "viewer", id: "laptop", body: `{"user":"bob"}`, from: phoneIP, want: http.StatusForbidden},
		{name: "bad user", id: "laptop", body: `{"user":"bob; reboot"}`, from: laptopIP, want: http.StatusBadRequest},
		{name: "bad port", id: "laptop", body: `{"user":"bob","port":70000}`, from: laptopIP, want: http.StatusBadRequest},
		{name: "phone", id: "phone", body: `{"user":"bob"}`, from: laptopIP, want: http.StatusBadRequest},
		{name: "no such device", id: "nope", body: `{"user":"bob"}`, from: laptopIP, want: http.StatusNotFound},
	} {
		rec := send(t, e.h, http.MethodPut, "/api/devices/"+tt.id+"/ssh", tt.from, tt.body)
		if rec.Code != tt.want {
			t.Errorf("%s: PUT = %d %s, want %d", tt.name, rec.Code, rec.Body, tt.want)
		}
	}

	if rec := send(t, e.h, http.MethodDelete, "/api/devices/laptop/ssh", laptopIP, ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE = %d %s, want 204", rec.Code, rec.Body)
	}
	if rec := send(t, e.h, http.MethodDelete, "/api/devices/laptop/ssh", laptopIP, ""); rec.Code != http.StatusConflict {
		t.Errorf("DELETE again = %d %s, want 409", rec.Code, rec.Body)
	}
}

func TestSSH_setup(t *testing.T) {
	t.Parallel()
	e := newSSHEnv(t, testStore(t), nil)
	rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/ssh", laptopIP))
	var got api.SSHSetup
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("GET = %d %s, want 200", rec.Code, rec.Body)
	}
	key, err := e.opts.SSH.Key()
	if err != nil {
		t.Fatal(err)
	}
	want := `from="100.64.0.1,fd7a:115c:a1e0::1",no-agent-forwarding,no-X11-forwarding ` + key.PublicKey()
	if got.AuthorizedKey != want || got.Command != ssh.SetupCommand(want) || got.HubUser != "" {
		t.Errorf("GET = %+v, want the key %q for a peer", got, want)
	}
	// On the machine Bield runs on, Bield can add its key for its own user.
	rec = serve(e.h, newRequest(t, http.MethodGet, "/api/devices/bield/ssh", laptopIP))
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("GET on the hub = %d %s, want 200", rec.Code, rec.Body)
	}
	if !strings.HasPrefix(got.AuthorizedKey, `from="127.0.0.1,::1,100.64.0.1,`) || got.HubUser == "" {
		t.Errorf("GET on the hub = %+v, want the loopback addresses too, and Bield's user", got)
	}
	if rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/ssh", phoneIP)); rec.Code != http.StatusForbidden {
		t.Errorf("GET as a viewer = %d, want 403", rec.Code)
	}
}

func TestSSH_snapshot(t *testing.T) {
	t.Parallel()
	e := newSSHEnv(t, testStore(t), nil)
	e.setUp(t)
	snap := api.NewFeed(api.FeedOptions{Tailnet: e.opts.Snapshot, Store: e.st, SSH: e.opts.SSH.Access}).Latest()
	for _, tt := range []struct {
		id   string
		want *api.DeviceSSH
	}{
		{"bield", &api.DeviceSSH{State: api.SSHStateNotSetUp, Hub: true}},
		{"laptop", &api.DeviceSSH{State: api.SSHStateUnchecked, User: "alice", Port: 22}},
		// Tailscale doesn't say what the server runs, and phones have no sshd.
		{"server", nil},
		{"phone", nil},
	} {
		got := findDevice(t, snap, tt.id).SSH
		if (got == nil) != (tt.want == nil) || got != nil && *got != *tt.want {
			t.Errorf("%s: ssh = %+v, want %+v", tt.id, got, tt.want)
		}
	}
	// Only admins get it.
	if got, ok := api.Redact(findDevice(t, snap, "laptop")).(api.Device); !ok || got.SSH != nil {
		t.Errorf("a viewer gets %+v, want nothing", got.SSH)
	}
}

func TestSSH_checkAndHostKey(t *testing.T) {
	t.Parallel()
	e := newSSHEnv(t, testStore(t), nil)
	e.setUp(t)
	rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/ssh/check", laptopIP, "")
	var got api.DeviceSSH
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("POST check = %d %s, want 200", rec.Code, rec.Body)
	}
	if got.State != api.SSHStateOK || got.HostKey == "" || got.CheckedAt == nil || got.Tmux == nil || *got.Tmux {
		t.Errorf("POST check = %+v, want ok, a host key pinned, and no tmux", got)
	}

	rec = send(t, e.h, http.MethodDelete, "/api/devices/laptop/ssh/host-key", laptopIP, "")
	var reset api.DeviceSSH
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &reset) != nil {
		t.Fatalf("DELETE host-key = %d %s, want 200", rec.Code, rec.Body)
	}
	if reset.HostKey != "" || reset.State != api.SSHStateUnchecked {
		t.Errorf("DELETE host-key = %+v, want no host key, unchecked", reset)
	}
	events := e.events(t, api.EventKindSSH)
	if len(events) == 0 || events[0].Level != string(api.EventLevelWarning) || !strings.HasPrefix(events[0].Message, "Reset the SSH host key of laptop") {
		t.Errorf("events = %+v, want a warning that the host key was reset", events)
	}
	// Viewers don't see SSH events.
	rec = serve(e.h, newRequest(t, http.MethodGet, "/api/events", phoneIP))
	var list api.EventList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, ev := range list.Events {
		if ev.Kind == api.EventKindSSH {
			t.Errorf("a viewer sees %+v", ev)
		}
	}
}

func TestSSH_trustHostKey(t *testing.T) {
	t.Parallel()
	e := newSSHEnv(t, testStore(t), nil)
	e.setUp(t)
	put := func(from netip.Addr, fingerprint string) *httptest.ResponseRecorder {
		body, err := json.Marshal(api.TrustHostKey{Fingerprint: fingerprint})
		if err != nil {
			t.Fatal(err)
		}
		return send(t, e.h, http.MethodPut, "/api/devices/laptop/ssh/host-key", from, string(body))
	}
	check := func() api.DeviceSSH {
		t.Helper()
		var got api.DeviceSSH
		rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/ssh/check", laptopIP, "")
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("POST check = %d %s, want 200", rec.Code, rec.Body)
		}
		return got
	}
	pinned := check().HostKey
	if rec := put(laptopIP, "SHA256:anything"); rec.Code != http.StatusConflict {
		t.Errorf("PUT before the key changed = %d %s, want 409", rec.Code, rec.Body)
	}

	offered := gossh.FingerprintSHA256(e.sshd.ChangeHostKey())
	if got := check(); got.State != api.SSHStateHostKeyChanged || got.OfferedKey != offered || got.HostKey != pinned {
		t.Fatalf("check = %+v, want the change refused, with %s offered", got, offered)
	}
	if rec := put(phoneIP, offered); rec.Code != http.StatusForbidden {
		t.Errorf("PUT as a viewer = %d, want 403", rec.Code)
	}
	if rec := put(laptopIP, "SHA256:another"); rec.Code != http.StatusConflict ||
		!strings.Contains(rec.Body.String(), "another host key than the one you looked at") {
		t.Errorf("PUT of a key that isn't the one offered = %d %s, want 409 saying so", rec.Code, rec.Body)
	}
	rec := put(laptopIP, offered)
	var got api.DeviceSSH
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("PUT = %d %s, want 200", rec.Code, rec.Body)
	}
	if got.HostKey != offered {
		t.Errorf("PUT pinned %s, want %s", got.HostKey, offered)
	}
	if got := check(); got.State != api.SSHStateOK || got.HostKey != offered {
		t.Errorf("check after trusting = %+v, want ok, with %s pinned", got, offered)
	}
	events := e.events(t, api.EventKindSSH)
	if len(events) == 0 || events[0].Level != string(api.EventLevelWarning) || events[0].Actor != alice ||
		events[0].Message != "Trusted the new SSH host key of laptop, "+offered {
		t.Errorf("events = %+v, want a warning that alice trusted %s", events, offered)
	}
}

func TestSSH_errors(t *testing.T) {
	t.Parallel()
	e := newSSHEnv(t, testStore(t), nil)
	for _, tt := range []struct {
		name, method, path string
		want               int
	}{
		{"not set up", http.MethodPost, "/api/devices/laptop/ssh/check", http.StatusConflict},
		{"not set up", http.MethodGet, "/api/devices/laptop/terminal/sessions", http.StatusConflict},
		{"not the hub", http.MethodPost, "/api/devices/laptop/ssh/authorize", http.StatusBadRequest},
		{"no such device", http.MethodDelete, "/api/devices/nope/ssh/host-key", http.StatusNotFound},
	} {
		if rec := send(t, e.h, tt.method, tt.path, laptopIP, ""); rec.Code != tt.want {
			t.Errorf("%s: %s %s = %d %s, want %d", tt.name, tt.method, tt.path, rec.Code, rec.Body, tt.want)
		}
	}
	e.setUp(t)
	e.sshd.Refuse(true)
	rec := send(t, e.h, http.MethodGet, "/api/devices/laptop/terminal/sessions", laptopIP, "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "nothing is listening for SSH on port 22 of laptop") {
		t.Errorf("sessions with sshd down = %d %s, want 502 saying why", rec.Code, rec.Body)
	}
}

func TestSSH_sessions(t *testing.T) {
	t.Parallel()
	e := newSSHEnv(t, testStore(t), nil)
	e.setUp(t)
	e.sshd.Handle(func(x *sshtest.Exec) int {
		switch {
		case strings.Contains(x.Command, "list-sessions"):
			_, _ = io.WriteString(x.Stdout, "web-1:1700000000:1700000060:1:2:vim\n")
		case strings.Contains(x.Command, "kill-session") && !strings.HasSuffix(x.Command, " sh 'web-1'"):
			_, _ = io.WriteString(x.Stderr, "can't find session: =gone\n")
			return 1
		}
		return 0
	})
	rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/terminal/sessions", laptopIP))
	var got api.TerminalSessions
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("GET = %d %s, want 200", rec.Code, rec.Body)
	}
	created := time.Unix(1700000000, 0)
	if !got.Tmux || len(got.Sessions) != 1 || got.Sessions[0].Name != "web-1" || got.Sessions[0].Attached != 1 ||
		got.Sessions[0].Windows != 2 || got.Sessions[0].Command != "vim" || !got.Sessions[0].CreatedAt.Equal(created) {
		t.Errorf("GET = %+v, want web-1, made %v, with 2 windows, running vim", got, created)
	}

	if rec := send(t, e.h, http.MethodDelete, "/api/devices/laptop/terminal/sessions/web-1", laptopIP, ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE web-1 = %d %s, want 204", rec.Code, rec.Body)
	}
	if events := e.events(t, api.EventKindTerminal); len(events) != 1 || events[0].Message != "Ended tmux session web-1 on laptop" {
		t.Errorf("events = %+v, want one that says web-1 ended", events)
	}
	for name, want := range map[string]int{"gone": http.StatusNotFound, "a.b": http.StatusBadRequest} {
		if rec := send(t, e.h, http.MethodDelete, "/api/devices/laptop/terminal/sessions/"+name, laptopIP, ""); rec.Code != want {
			t.Errorf("DELETE %s = %d %s, want %d", name, rec.Code, rec.Body, want)
		}
	}
	// A machine without tmux has no sessions.
	e.sshd.Handle(func(*sshtest.Exec) int { return 127 })
	rec = serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/terminal/sessions", laptopIP))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"tmux":false,"sessions":[]}` {
		t.Errorf("GET without tmux = %d %s, want no tmux and no sessions", rec.Code, rec.Body)
	}
}

// wsClient is a terminal's WebSocket, as the browser has it.
type wsClient struct {
	conn   *websocket.Conn
	frames chan wsFrame
	// closed is the close status, once frames is closed.
	closed websocket.StatusCode
	// peeked is a frame that output took and didn't use.
	peeked *wsFrame
}

type wsFrame struct {
	text bool
	data []byte
}

// dialTerminal serves e on its tailnet, and opens a terminal on the laptop
// from the device at from, with query, from a page at origin. It reads
// frames as they come, as a browser does.
func dialTerminal(t *testing.T, e *sshEnv, from netip.Addr, query, origin string) (*wsClient, error) {
	t.Helper()
	if !e.served {
		ln, _, err := e.tn.Listen(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{Handler: e.h, ReadHeaderTimeout: time.Second}
		go func() { _ = srv.Serve(ln) }()
		t.Cleanup(func() { _ = srv.Close() })
		e.served = true
	}

	client := e.tn.Client(from)
	t.Cleanup(client.CloseIdleConnections)
	//nolint:bodyclose // The WebSocket owns the response's body.
	conn, res, err := websocket.Dial(t.Context(), "ws://"+bieldHost+"/api/devices/laptop/terminal"+query, &websocket.DialOptions{
		HTTPClient: client, HTTPHeader: http.Header{"Origin": {origin}},
	})
	if err != nil {
		if res != nil {
			return nil, fmt.Errorf("%w: %d", err, res.StatusCode)
		}
		return nil, err
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	c := &wsClient{conn: conn, frames: make(chan wsFrame, 1000)}
	go func() {
		defer close(c.frames)
		for {
			typ, data, err := conn.Read(context.Background())
			if err != nil {
				c.closed = websocket.CloseStatus(err)
				return
			}
			c.frames <- wsFrame{text: typ == websocket.MessageText, data: data}
		}
	}()
	return c, nil
}

// next returns the next frame, which must have come.
func (c *wsClient) next(t *testing.T) wsFrame {
	t.Helper()
	if f := c.peeked; f != nil {
		c.peeked = nil
		return *f
	}
	select {
	case f, ok := <-c.frames:
		if !ok {
			t.Fatalf("the WebSocket closed with %v, want another frame", c.closed)
		}
		return f
	default:
		t.Fatal("no frame yet")
		return wsFrame{}
	}
}

// message returns the next frame, which must be a message.
func (c *wsClient) message(t *testing.T) api.TerminalMessage {
	t.Helper()
	f := c.next(t)
	var m api.TerminalMessage
	if !f.text || json.Unmarshal(f.data, &m) != nil {
		t.Fatalf("frame %q, want a message", f.data)
	}
	return m
}

// output returns the output that has come, up to the next message.
func (c *wsClient) output() string {
	var out strings.Builder
	for c.peeked == nil {
		select {
		case f, ok := <-c.frames:
			switch {
			case !ok:
				return out.String()
			case f.text:
				c.peeked = &f
			default:
				out.Write(f.data)
			}
		default:
			return out.String()
		}
	}
	return out.String()
}

func (c *wsClient) send(t *testing.T, m api.TerminalMessage) {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.conn.Write(t.Context(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

// ended waits for the WebSocket to close and returns its status.
func (c *wsClient) ended(t *testing.T) websocket.StatusCode {
	t.Helper()
	for range c.frames {
	}
	return c.closed
}

const bieldOrigin = "http://" + bieldHost

// openStore opens a test store in a synctest bubble.
func openStore(t *testing.T) *store.Store {
	t.Helper()
	st := testStore(t)
	// Let the prune that Open starts finish, so it doesn't hold the store.
	synctest.Wait()
	return st
}

func TestTerminal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newSSHEnv(t, openStore(t), nil)
		e.setUp(t)
		resized := make(chan sshtest.Size, 10)
		var pty *sshtest.Pty
		e.sshd.Handle(func(x *sshtest.Exec) int {
			if x.Pty == nil {
				return 0
			}
			pty = x.Pty
			go func() {
				for s := range x.Resized {
					resized <- s
				}
			}()
			sc := bufio.NewScanner(x.Stdin)
			for sc.Scan() {
				switch sc.Text() {
				case "exit":
					return 5
				case "flood":
					// More than the window, in pieces the size of a pipe's.
					for range 48 {
						_, _ = x.Stdout.Write([]byte(strings.Repeat("x", 64<<10)))
					}
				default:
					_, _ = fmt.Fprintf(x.Stdout, "you said %s\n", sc.Text())
				}
			}
			return 0
		})

		c, err := dialTerminal(t, e, laptopIP, "?session=web-1&cols=100&rows=30", bieldOrigin)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if m := c.message(t); m.Type != api.TerminalMessageTypeReady || m.Session != "web-1" {
			t.Errorf("first message %+v, want ready, in web-1", m)
		}
		if pty == nil || pty.Cols != 100 || pty.Rows != 30 {
			t.Errorf("pty = %+v, want 100×30", pty)
		}
		if events := e.events(t, api.EventKindTerminal); len(events) != 1 ||
			events[0].Message != "Opened a terminal on laptop, in tmux session web-1" || events[0].Actor != alice {
			t.Errorf("events = %+v, want one that alice opened a terminal", events)
		}

		c.send(t, api.TerminalMessage{Type: api.TerminalMessageTypeInput, Data: "hello\n"})
		c.send(t, api.TerminalMessage{Type: api.TerminalMessageTypeResize, Cols: 120, Rows: 40})
		synctest.Wait()
		if out := c.output(); out != "you said hello\n" {
			t.Errorf("output = %q, want %q", out, "you said hello\n")
		}
		select {
		case s := <-resized:
			if s != (sshtest.Size{Cols: 120, Rows: 40}) {
				t.Errorf("resized to %+v, want 120×40", s)
			}
		default:
			t.Error("no resize")
		}

		// The browser acks what it writes, and the flood keeps coming.
		c.send(t, api.TerminalMessage{Type: api.TerminalMessageTypeAck, Bytes: len("you said hello\n")})
		c.send(t, api.TerminalMessage{Type: api.TerminalMessageTypeInput, Data: "flood\n"})
		for total := 0; total < 48*64<<10; {
			synctest.Wait()
			n := len(c.output())
			if n == 0 {
				t.Fatalf("stuck after %d bytes", total)
			}
			total += n
			c.send(t, api.TerminalMessage{Type: api.TerminalMessageTypeAck, Bytes: n})
		}

		c.send(t, api.TerminalMessage{Type: api.TerminalMessageTypeInput, Data: "exit\n"})
		synctest.Wait()
		if m := c.message(t); m.Type != api.TerminalMessageTypeExit || m.Code == nil || *m.Code != 5 {
			t.Errorf("message %+v, want exit 5", m)
		}
		if status := c.ended(t); status != websocket.StatusNormalClosure {
			t.Errorf("closed with %v, want normal closure", status)
		}
	})
}

func TestTerminal_connectionDropped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newSSHEnv(t, openStore(t), nil)
		e.setUp(t)
		c, err := dialTerminal(t, e, laptopIP, "?session=web-1", bieldOrigin)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		c.message(t)

		// The machine goes away, without the shell saying how it ended.
		e.sshd.Drop()
		synctest.Wait()
		if m := c.message(t); m.Type != api.TerminalMessageTypeExit || m.Code != nil {
			t.Errorf("message %+v, want exit with no status, since the shell may still run", m)
		}
		if status := c.ended(t); status != websocket.StatusNormalClosure {
			t.Errorf("closed with %v, want normal closure", status)
		}
	})
}

func TestTerminal_window(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newSSHEnv(t, openStore(t), nil)
		e.setUp(t)
		e.sshd.Handle(func(x *sshtest.Exec) int {
			if x.Pty == nil {
				return 0
			}
			for range 48 {
				if _, err := x.Stdout.Write([]byte(strings.Repeat("x", 64<<10))); err != nil {
					return 1
				}
			}
			return 0
		})
		c, err := dialTerminal(t, e, laptopIP, "", bieldOrigin)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		c.message(t)
		// Until the browser acks, it gets a window of output and no more.
		got := len(c.output())
		if got < api.TerminalWindow || got > api.TerminalWindow+32<<10 {
			t.Errorf("got %d bytes before an ack, want a window, %d, and at most a chunk more", got, api.TerminalWindow)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if more := len(c.output()); more != 0 {
			t.Errorf("got %d more bytes without an ack", more)
		}
		_ = c.conn.Close(websocket.StatusNormalClosure, "")
	})
}

func TestTerminal_refused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newSSHEnv(t, openStore(t), nil)
		for _, tt := range []struct {
			name   string
			from   netip.Addr
			origin string
			want   int
		}{
			{"viewer", phoneIP, bieldOrigin, http.StatusForbidden},
			{"another site", laptopIP, "http://evil.example", http.StatusForbidden},
		} {
			_, err := dialTerminal(t, e, tt.from, "", tt.origin)
			if err == nil || !strings.HasSuffix(err.Error(), strconv.Itoa(tt.want)) {
				t.Errorf("%s: dial = %v, want %d", tt.name, err, tt.want)
			}
		}
	})
}

func TestTerminal_errors(t *testing.T) {
	for _, tt := range []struct {
		name, query string
		setUp       bool
		want        string
	}{
		{name: "not set up", want: "SSH access isn't set up for this device"},
		{name: "bad session", query: "?session=a:b", setUp: true, want: ssh.ErrBadSession.Error()},
		{name: "bad directory", query: "?cwd=relative", setUp: true, want: ssh.ErrBadDir.Error()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := newSSHEnv(t, openStore(t), nil)
				if tt.setUp {
					e.setUp(t)
				}
				c, err := dialTerminal(t, e, laptopIP, tt.query, bieldOrigin)
				if err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				if m := c.message(t); m.Type != api.TerminalMessageTypeError || m.Message != tt.want {
					t.Errorf("message %+v, want the error %q", m, tt.want)
				}
				if status := c.ended(t); status != websocket.StatusNormalClosure {
					t.Errorf("closed with %v, want normal closure", status)
				}
				if events := e.events(t, api.EventKindTerminal); len(events) != 0 {
					t.Errorf("events = %+v, want none", events)
				}
			})
		})
	}
}

func TestTerminal_shutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stopping := make(chan struct{})
		e := newSSHEnv(t, openStore(t), stopping)
		e.setUp(t)
		c, err := dialTerminal(t, e, laptopIP, "", bieldOrigin)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		c.message(t)
		close(stopping)
		if status := c.ended(t); status != websocket.StatusGoingAway {
			t.Errorf("closed with %v, want going away", status)
		}
	})
}

func TestTerminal_pings(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newSSHEnv(t, openStore(t), nil)
		e.setUp(t)
		c, err := dialTerminal(t, e, laptopIP, "", bieldOrigin)
		if err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		c.message(t)
		// A quiet terminal stays open while the browser answers pings.
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		select {
		case _, ok := <-c.frames:
			if !ok {
				t.Fatalf("closed with %v", c.closed)
			}
		default:
		}
		_ = c.conn.Close(websocket.StatusNormalClosure, "")
	})
}

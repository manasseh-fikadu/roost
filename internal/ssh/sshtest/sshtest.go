// Package sshtest provides a fake SSH server for tests, so they never need
// a real one. Its connections are net.Pipe pairs, which block on channels
// rather than sockets, so it works inside synctest. Plug Server.Dial, or
// Route for several servers, into whatever dials the machines, such as
// tailnettest.Tailnet.SetDialer.
package sshtest

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os/exec"
	"slices"
	"sync"
	"syscall"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// Server is a fake sshd whose answers the test sets. It lets in the keys
// given to Authorize, as any user, and runs every command and shell with
// the Handler given to Handle. Create one with New.
type Server struct {
	mu      sync.Mutex
	hostKey gossh.Signer
	// others are more host keys, which the server also offers.
	others     []gossh.Signer
	authorized [][]byte
	handler    Handler
	sockets    map[string]func(net.Conn)
	refuse     bool
	dialed     []string
	logins     []string
	conns      map[net.Conn]struct{}
	accepted   int
	closed     bool
	wg         sync.WaitGroup
}

// Exec is a command, or a login shell, that a client started.
type Exec struct {
	// User is who the client logged in as.
	User string
	// Command is what the client asked to run, or "" for a login shell.
	Command string
	// Pty is the terminal the client asked for, or nil if it didn't.
	Pty *Pty
	// Stdin reads what the client sends. It ends when the client closes
	// it or goes away.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Resized receives each new size the client gives the terminal. It's
	// closed when the session ends.
	Resized <-chan Size
}

// Pty is a terminal a client asked for.
type Pty struct {
	// Term is its TERM, such as xterm-256color.
	Term string
	Size
}

// Size is a terminal's size in characters.
type Size struct {
	Cols, Rows int
}

// Handler runs a command and returns its exit status.
type Handler func(e *Exec) int

// New returns a server with a new host key, which runs nothing until Handle
// says how.
func New() *Server {
	return &Server{
		hostKey: newSigner(),
		sockets: map[string]func(net.Conn){},
		conns:   map[net.Conn]struct{}{},
		handler: func(e *Exec) int {
			_, _ = io.WriteString(e.Stderr, "sshtest: no handler\n")
			return 127
		},
	}
}

func newSigner() gossh.Signer {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		panic(err)
	}
	return signer
}

// HostKey returns the server's host key.
func (s *Server) HostKey() gossh.PublicKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hostKey.PublicKey()
}

// ChangeHostKey gives the server a new host key, as reinstalling a machine
// does, and returns it.
func (s *Server) ChangeHostKey() gossh.PublicKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hostKey = newSigner()
	return s.hostKey.PublicKey()
}

// AddHostKey gives the server another host key, of another type, as sshd
// has one of each. A client picks the type it prefers.
func (s *Server) AddHostKey(key crypto.Signer) gossh.PublicKey {
	signer, err := gossh.NewSignerFromSigner(key)
	if err != nil {
		panic(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.others = append(s.others, signer)
	return signer.PublicKey()
}

// Authorize lets clients log in with the key in an authorized_keys line,
// which may have options. It panics if the line has no key.
func (s *Server) Authorize(line string) {
	key, _, _, _, err := gossh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		panic("sshtest: authorize " + line + ": " + err.Error())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorized = append(s.authorized, key.Marshal())
}

// Handle makes h run the commands and shells that clients start from now
// on.
func (s *Server) Handle(h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = h
}

// HandleSocket makes h serve each connection a client opens to the Unix
// socket at path, as OpenSSH's direct-streamlocal does. A client can't
// open a socket that has no handler.
func (s *Server) HandleSocket(path string, h func(net.Conn)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sockets[path] = h
}

// Refuse makes Dial refuse connections while refuse is true, as if nothing
// listened.
func (s *Server) Refuse(refuse bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refuse = refuse
}

// Dialed returns the addresses Dial was called with, in order.
func (s *Server) Dialed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.dialed)
}

// Logins returns the users that clients logged in as, in order.
func (s *Server) Logins() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.logins)
}

// Conns returns how many connections are open.
func (s *Server) Conns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// Accepted returns how many connections Dial has made.
func (s *Server) Accepted() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepted
}

// Dial connects to the server, whatever addr is, unless Refuse says not to.
func (s *Server) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dialed = append(s.dialed, addr)
	if s.refuse || s.closed {
		return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
	}
	client, server := s.pipe()
	s.accepted++
	s.conns[server] = struct{}{}
	s.wg.Go(func() { s.serve(server) })
	return client, nil
}

// pipe returns the two ends of an in-memory connection. Both ends of an SSH
// connection write their version first, so a write can't wait for the other
// end to read, as it does on a net.Pipe: two pipes with a goroutine copying
// between them in each direction let each end write a little before the
// other reads.
func (s *Server) pipe() (client, server net.Conn) {
	client, clientEnd := net.Pipe()
	serverEnd, server := net.Pipe()
	copyTo := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
		_ = src.Close()
	}
	s.wg.Go(func() { copyTo(serverEnd, clientEnd) })
	s.wg.Go(func() { copyTo(clientEnd, serverEnd) })
	return client, server
}

// Route returns a dial function that connects to the server for the host
// in each address, such as "100.64.0.2", and refuses hosts it doesn't know.
func Route(servers map[string]*Server) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		s, ok := servers[host]
		if !ok {
			return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
		}
		return s.Dial(ctx, network, addr)
	}
}

// Drop closes every open connection, as a machine that goes away does.
func (s *Server) Drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		_ = c.Close()
	}
}

// Close closes every connection, refuses new ones, and waits for the
// handlers to return.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.Drop()
	s.wg.Wait()
}

func (s *Server) serve(conn net.Conn) {
	defer func() {
		_ = conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()
	s.mu.Lock()
	config := &gossh.ServerConfig{
		PublicKeyCallback: func(_ gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, k := range s.authorized {
				if slices.Equal(k, key.Marshal()) {
					return &gossh.Permissions{}, nil
				}
			}
			return nil, errors.New("sshtest: key not authorized")
		},
	}
	config.AddHostKey(s.hostKey)
	for _, k := range s.others {
		config.AddHostKey(k)
	}
	s.mu.Unlock()
	sconn, chans, reqs, err := gossh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.logins = append(s.logins, sconn.User())
	s.mu.Unlock()
	defer func() { _ = sconn.Close() }()
	go gossh.DiscardRequests(reqs)
	var wg sync.WaitGroup
	defer wg.Wait()
	for nc := range chans {
		switch nc.ChannelType() {
		case "session":
			ch, reqs, err := nc.Accept()
			if err != nil {
				continue
			}
			wg.Go(func() { s.session(sconn.User(), ch, reqs) })
		case "direct-streamlocal@openssh.com":
			var msg struct {
				SocketPath string
				Reserved0  string
				Reserved1  uint32
			}
			if err := gossh.Unmarshal(nc.ExtraData(), &msg); err != nil {
				_ = nc.Reject(gossh.ConnectionFailed, "bad request")
				continue
			}
			s.mu.Lock()
			h := s.sockets[msg.SocketPath]
			s.mu.Unlock()
			if h == nil {
				_ = nc.Reject(gossh.ConnectionFailed, "open failed")
				continue
			}
			ch, reqs, err := nc.Accept()
			if err != nil {
				continue
			}
			go gossh.DiscardRequests(reqs)
			wg.Go(func() {
				c := &chanConn{Channel: ch}
				defer func() { _ = c.Close() }()
				h(c)
			})
		default:
			_ = nc.Reject(gossh.UnknownChannelType, "unknown channel type")
		}
	}
}

// session runs the requests on one session channel.
func (s *Server) session(user string, ch gossh.Channel, reqs <-chan *gossh.Request) {
	defer func() { _ = ch.Close() }()
	resized := make(chan Size, 64)
	defer close(resized)
	var (
		pty     *Pty
		started bool
		exited  = make(chan int, 1)
	)
	for {
		var req *gossh.Request
		select {
		case r, ok := <-reqs:
			if !ok {
				return
			}
			req = r
		case code := <-exited:
			status := struct{ Status uint32 }{uint32(code)} //nolint:gosec // G115: exit statuses are small.
			_, _ = ch.SendRequest("exit-status", false, gossh.Marshal(&status))
			return
		}
		ok := false
		switch req.Type {
		case "pty-req":
			var msg struct {
				Term                      string
				Cols, Rows, Width, Height uint32
				Modes                     string
			}
			if gossh.Unmarshal(req.Payload, &msg) == nil && !started {
				pty, ok = &Pty{Term: msg.Term, Size: Size{int(msg.Cols), int(msg.Rows)}}, true
			}
		case "window-change":
			var msg struct{ Cols, Rows, Width, Height uint32 }
			if gossh.Unmarshal(req.Payload, &msg) == nil {
				select {
				case resized <- Size{int(msg.Cols), int(msg.Rows)}:
				default:
				}
			}
		case "env":
			ok = true
		case "exec", "shell":
			var msg struct{ Command string }
			if req.Type == "exec" && gossh.Unmarshal(req.Payload, &msg) != nil || started {
				break
			}
			started, ok = true, true
			s.mu.Lock()
			h := s.handler
			s.mu.Unlock()
			e := &Exec{User: user, Command: msg.Command, Pty: pty, Stdin: ch, Stdout: ch, Stderr: ch.Stderr(), Resized: resized}
			if pty != nil {
				// A terminal has one output, as with a real pty.
				e.Stderr = ch
			}
			s.wg.Go(func() { exited <- h(e) })
		}
		if req.WantReply {
			_ = req.Reply(ok, nil)
		}
	}
}

// chanConn is an SSH channel as a net.Conn.
type chanConn struct {
	gossh.Channel
}

func (c *chanConn) LocalAddr() net.Addr              { return &net.UnixAddr{Net: "unix"} }
func (c *chanConn) RemoteAddr() net.Addr             { return &net.UnixAddr{Net: "unix"} }
func (c *chanConn) SetDeadline(time.Time) error      { return nil }
func (c *chanConn) SetReadDeadline(time.Time) error  { return nil }
func (c *chanConn) SetWriteDeadline(time.Time) error { return nil }

// Shell returns a Handler that runs each command with shell -c, as sshd
// does for a user whose login shell that is, with env added to the
// environment. A login shell runs shell -l. It's for tests of what a real
// shell makes of a command, so it ignores the pty.
func Shell(shell string, env ...string) Handler {
	return func(e *Exec) int {
		args := []string{"-l"}
		if e.Command != "" {
			args = []string{"-c", e.Command}
		}
		cmd := exec.Command(shell, args...) //nolint:noctx // The handler ends when the command does.
		cmd.Env = append(cmd.Environ(), env...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = e.Stdin, e.Stdout, e.Stderr
		// A client may keep stdin open after the command exits.
		cmd.WaitDelay = time.Second
		err := cmd.Run()
		var exit *exec.ExitError
		switch {
		case err == nil || errors.Is(err, exec.ErrWaitDelay):
			return 0
		case errors.As(err, &exit):
			return exit.ExitCode()
		default:
			_, _ = io.WriteString(e.Stderr, err.Error()+"\n")
			return 127
		}
	}
}

package ssh

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/manasseh-fikadu/roost/internal/store"
)

// pooled is a machine's shared connection.
type pooled struct {
	// ready is closed once connecting ends, and client or err is set.
	ready  chan struct{}
	client *gossh.Client
	err    error
	// active counts what's using the connection, and idleSince is when
	// the last of it stopped. Manager.mu guards them.
	active    int
	idleSince time.Time
	closeOnce sync.Once
}

func (p *pooled) close() {
	p.closeOnce.Do(func() {
		go func() {
			<-p.ready
			if p.client != nil {
				_ = p.client.Close()
			}
		}()
	})
}

// sharedTo returns the shared connection to t, connecting if there's none,
// and a function to call once done with it.
func (m *Manager) sharedTo(ctx context.Context, t target) (*gossh.Client, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	m.mu.Lock()
	a, ok := m.access[t.id]
	if !ok {
		m.mu.Unlock()
		return nil, nil, ErrNotSetUp
	}
	p := m.pool[t.id]
	if p == nil {
		p = &pooled{ready: make(chan struct{})}
		m.pool[t.id] = p
		// Connecting doesn't stop if the first caller goes away, since
		// another may be waiting.
		go m.dialShared(context.WithoutCancel(ctx), t, a, m.gen[t.id], p)
	}
	p.active++
	m.mu.Unlock()
	release := sync.OnceFunc(func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		p.active--
		if p.active == 0 {
			p.idleSince = time.Now()
		}
	})
	select {
	case <-p.ready:
	case <-ctx.Done():
		release()
		return nil, nil, ctx.Err()
	}
	if p.err != nil {
		release()
		return nil, nil, p.err
	}
	return p.client, release, nil
}

// dialShared connects p to t with a, and takes it out of the pool when it
// fails or closes. gen is t's generation when a was read.
func (m *Manager) dialShared(ctx context.Context, t target, a store.SSHAccess, gen uint64, p *pooled) {
	p.client, p.err = m.connect(ctx, t, a)
	close(p.ready)
	m.record(t.id, gen, p.err)
	drop := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.pool[t.id] == p {
			delete(m.pool, t.id)
		}
	}
	if p.err != nil {
		drop()
		return
	}
	_ = p.client.Wait()
	drop()
}

// closeIdle closes the shared connections that nothing has used for
// IdleTimeout.
func (m *Manager) closeIdle(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, p := range m.pool {
		select {
		case <-p.ready:
		default:
			continue
		}
		if p.active == 0 && now.Sub(p.idleSince) >= IdleTimeout {
			delete(m.pool, id)
			p.close()
		}
	}
}

// closeAll closes every connection and terminal.
func (m *Manager) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, p := range m.pool {
		delete(m.pool, id)
		p.close()
	}
	for id, terms := range m.terminals {
		for c := range terms {
			_ = c.Close()
		}
		delete(m.terminals, id)
	}
}

// Result is what a command did.
type Result struct {
	// Code is its exit status, or -1 if it didn't exit with one.
	Code           int
	Stdout, Stderr []byte
}

// The most of a command's output that Run keeps.
const (
	maxStdout = 16 << 20
	maxStderr = 64 << 10
)

// Exec runs a sh script on a device, with args as $1, $2 and so on, over its
// shared connection, and waits for it to finish or ctx to end. The args can
// be anything but a NUL, which fails with ErrBadArg (see Quote). It runs with
// the PATH extended to where package managers put tools such as tmux.
func (m *Manager) Exec(ctx context.Context, deviceID, script string, args ...string) (Result, error) {
	t, err := m.target(deviceID)
	if err != nil {
		return Result{}, err
	}
	return m.exec(ctx, t, script, args...)
}

func (m *Manager) exec(ctx context.Context, t target, script string, args ...string) (Result, error) {
	client, release, err := m.sharedTo(ctx, t)
	if err != nil {
		return Result{}, err
	}
	defer release()
	if err := checkArgs(args); err != nil {
		return Result{}, err
	}
	return run(ctx, client, command(extraPath+script, args...))
}

func run(ctx context.Context, client *gossh.Client, cmd string) (Result, error) {
	s, err := client.NewSession()
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = s.Close() }()
	stdout, stderr := &limitedBuffer{max: maxStdout}, &limitedBuffer{max: maxStderr}
	s.Stdout, s.Stderr = stdout, stderr
	if err := s.Start(cmd); err != nil {
		return Result{}, err
	}
	// Closing the session ends Wait if ctx ends first.
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	err = s.Wait()
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var (
		exit    *gossh.ExitError
		missing *gossh.ExitMissingError
	)
	switch {
	case err == nil:
	case errors.As(err, &exit):
		res.Code = exit.ExitStatus()
	case errors.As(err, &missing):
		res.Code = -1
	default:
		return Result{}, err
	}
	return res, nil
}

// limitedBuffer keeps the first max bytes written to it, and throws the
// rest away.
type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.Len(); room > 0 {
		b.Buffer.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// DialUnix connects to a Unix socket on a device, such as Docker's, over its
// shared connection. The connection counts as in use until the returned one
// is closed.
func (m *Manager) DialUnix(ctx context.Context, deviceID, path string) (net.Conn, error) {
	t, err := m.target(deviceID)
	if err != nil {
		return nil, err
	}
	return m.dialUnix(ctx, t, path)
}

func (m *Manager) dialUnix(ctx context.Context, t target, path string) (net.Conn, error) {
	client, release, err := m.sharedTo(ctx, t)
	if err != nil {
		return nil, err
	}
	conn, err := client.DialContext(ctx, "unix", path)
	if err != nil {
		release()
		return nil, err
	}
	return &releasingConn{Conn: conn, release: release}, nil
}

// releasingConn calls release when it's closed.
type releasingConn struct {
	net.Conn
	release func()
}

func (c *releasingConn) Close() error {
	defer c.release()
	return c.Conn.Close()
}

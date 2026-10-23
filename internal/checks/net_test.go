package checks_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"testing"
	"time"
)

// netDelay is how long the fake network takes to connect or look up a name.
const netDelay = 2 * time.Millisecond

// errRefused is what dialing an address that nothing listens on fails with.
var errRefused = errors.New("connect: connection refused")

// A fakeNet is a network in memory, which works in a synctest bubble. A
// dial to an address that a test listens on gets one end of a net.Pipe, and
// the listener the other.
type fakeNet struct {
	mu        sync.Mutex
	listeners map[string]*pipeListener
	// held addresses don't answer: a dial to one waits until its channel is
	// closed, and then fails, or until it gives up.
	held map[string]chan struct{}
	// waiting counts the dials that wait on a held address.
	waiting int
	// dials counts every dial, by address.
	dials map[string]int
	// hosts are what lookup returns for each host name. A host that isn't
	// there doesn't exist.
	hosts map[string][]netip.Addr
}

func newFakeNet() *fakeNet {
	return &fakeNet{
		listeners: map[string]*pipeListener{},
		held:      map[string]chan struct{}{},
		dials:     map[string]int{},
		hosts:     map[string][]netip.Addr{},
	}
}

func (n *fakeNet) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	n.mu.Lock()
	n.dials[addr]++
	ln, held := n.listeners[addr], n.held[addr]
	if held != nil {
		n.waiting++
	}
	n.mu.Unlock()
	if held != nil {
		defer func() {
			n.mu.Lock()
			n.waiting--
			n.mu.Unlock()
		}()
		select {
		case <-held:
			return nil, &net.OpError{Op: "dial", Net: network, Err: errRefused}
		case <-ctx.Done():
			return nil, &net.OpError{Op: "dial", Net: network, Err: ctx.Err()}
		}
	}
	if ln == nil {
		return nil, &net.OpError{Op: "dial", Net: network, Err: errRefused}
	}
	// A connection takes a moment, so it has a latency.
	time.Sleep(netDelay)
	client, server := net.Pipe()
	select {
	case ln.conns <- server:
		return client, nil
	case <-ln.closed:
		return nil, &net.OpError{Op: "dial", Net: network, Err: errRefused}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (n *fakeNet) lookup(_ context.Context, host string) ([]netip.Addr, error) {
	time.Sleep(netDelay)
	n.mu.Lock()
	defer n.mu.Unlock()
	addrs, ok := n.hosts[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return addrs, nil
}

// hold makes dials to addr wait, and returns a function that lets them
// fail.
func (n *fakeNet) hold(addr string) (release func()) {
	ch := make(chan struct{})
	n.mu.Lock()
	n.held[addr] = ch
	n.mu.Unlock()
	return sync.OnceFunc(func() { close(ch) })
}

// dialsTo returns how many times addr has been dialed.
func (n *fakeNet) dialsTo(addr string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.dials[addr]
}

// waitingDials returns how many dials wait on held addresses.
func (n *fakeNet) waitingDials() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.waiting
}

// listen accepts connections to db.example.com:5432 and closes them, until
// the test ends.
func (n *fakeNet) listen(t *testing.T) {
	t.Helper()
	ln := n.listener(t, "db.example.com:5432")
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
}

// serve serves HTTP on addr with h, over TLS with a certificate no one
// trusts if secure is true, until the test ends.
func (n *fakeNet) serve(t *testing.T, addr string, secure bool, h http.Handler) {
	t.Helper()
	var ln net.Listener = n.listener(t, addr)
	if secure {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{selfSigned(t)}, MinVersion: tls.VersionTLS12})
	}
	// The server logs handshakes that fail, which some tests expect.
	srv := &http.Server{Handler: h, ReadHeaderTimeout: time.Minute, ErrorLog: log.New(io.Discard, "", 0)}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func (n *fakeNet) listener(t *testing.T, addr string) *pipeListener {
	t.Helper()
	ln := &pipeListener{addr: addr, conns: make(chan net.Conn), closed: make(chan struct{})}
	n.mu.Lock()
	n.listeners[addr] = ln
	n.mu.Unlock()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

type pipeListener struct {
	addr      string
	conns     chan net.Conn
	closed    chan struct{}
	closeOnce sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr(l.addr) }

type pipeAddr string

func (pipeAddr) Network() string  { return "pipe" }
func (a pipeAddr) String() string { return string(a) }

// selfSigned returns a certificate for api.example.com that signs itself.
func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{"api.example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

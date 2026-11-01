package docker

import (
	"context"
	"net"
	"sync"

	"github.com/manasseh-fikadu/bield/internal/store"
)

// A Dialer connects to the Docker socket of a machine that Bield reaches over
// SSH, by the ID its SSH access is kept under, such as ssh.Manager's
// DialDocker.
type Dialer func(ctx context.Context, accessID string) (net.Conn, error)

// A Fleet finds the Docker of each machine: the machine Bield runs on, on its
// own socket, and the others over their SSH access. It is safe for
// concurrent use.
type Fleet struct {
	local *Client
	dial  Dialer

	mu    sync.Mutex
	hosts map[string]*Host
}

// NewFleet returns a Fleet. local reaches the Docker of the machine Bield runs
// on, and is nil if there's none. dial reaches the others', and is nil if
// there are none.
func NewFleet(local *Client, dial Dialer) *Fleet {
	if dial == nil {
		dial = func(context.Context, string) (net.Conn, error) { return nil, ErrNoDocker }
	}
	return &Fleet{local: local, dial: dial, hosts: map[string]*Host{}}
}

// Host returns the Docker of the machine with an access ID: store.HubID for
// the machine Bield runs on, or a device's ID. It returns ErrNoDocker for the
// machine Bield runs on if it has no Docker socket.
func (f *Fleet) Host(accessID string) (*Host, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if h, ok := f.hosts[accessID]; ok {
		return h, nil
	}
	var c *Client
	switch {
	case accessID == store.HubID && f.local == nil:
		return nil, ErrNoDocker
	case accessID == store.HubID:
		c = f.local
	default:
		c = NewClient(func(ctx context.Context, _, _ string) (net.Conn, error) { return f.dial(ctx, accessID) })
	}
	h := NewHost(c)
	f.hosts[accessID] = h
	return h, nil
}

// Close ends the live stats of every machine's Docker.
func (f *Fleet) Close() {
	f.mu.Lock()
	hosts := f.hosts
	f.hosts = map[string]*Host{}
	f.mu.Unlock()
	for _, h := range hosts {
		h.Close()
	}
}

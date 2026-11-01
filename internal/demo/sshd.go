package demo

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/manasseh-fikadu/bield/internal/ssh/sshtest"
)

// dockerSocket is where the made-up machines keep Docker's socket.
const dockerSocket = "/var/run/docker.sock"

// sshd is the machines' SSH servers: a sshtest.Server for each, which lets in
// Bield's key, tells Bield what the machine has, and serves its Docker. It's
// all that Bield reaches over SSH in a demo.
type sshd struct {
	d       *Demo
	servers map[string]*sshtest.Server // by tailnet address
	hub     *sshtest.Server
}

func newSSHD(d *Demo) *sshd {
	s := &sshd{d: d, servers: map[string]*sshtest.Server{}}
	for i, m := range d.machines {
		if !m.ssh && i != 0 {
			continue
		}
		srv := sshtest.New()
		s.servers[m.ip4.String()] = srv
		s.servers[m.ip6.String()] = srv
		if i == 0 {
			s.hub = srv
		}
	}
	return s
}

// listen makes each server say what its machine has.
func (s *sshd) listen() {
	for _, m := range s.d.machines {
		srv := s.servers[m.ip4.String()]
		if srv == nil {
			continue
		}
		srv.Handle(func(e *sshtest.Exec) int {
			// The check that finds out what a machine offers is the only
			// command Bield runs by itself.
			if strings.Contains(e.Command, "command -v tmux") {
				if m.os == "linux" {
					fmt.Fprintln(e.Stdout, "tmux")
				}
				if m.hasDk {
					fmt.Fprintln(e.Stdout, "docker="+dockerSocket)
				}
			}
			return 0
		})
		if eng := s.d.engines[m.id]; eng != nil {
			srv.HandleSocket(dockerSocket, eng.serveConn)
		}
	}
}

// route connects to the SSH server of the machine with the address in addr.
func (s *sshd) route(ctx context.Context, network, addr string) (net.Conn, error) {
	return sshtest.Route(s.servers)(ctx, network, addr)
}

// dialLocal connects to the SSH server of the machine Bield runs on.
func (s *sshd) dialLocal(ctx context.Context, network, addr string) (net.Conn, error) {
	return s.hub.Dial(ctx, network, addr)
}

func (s *sshd) close() {
	seen := map[*sshtest.Server]bool{}
	for _, srv := range s.servers {
		if !seen[srv] {
			seen[srv] = true
			srv.Close()
		}
	}
}

// AuthorizeSSH lets Bield log in to every machine with the key in an
// authorized_keys line.
func (d *Demo) AuthorizeSSH(line string) {
	seen := map[*sshtest.Server]bool{}
	for _, srv := range d.sshd.servers {
		if !seen[srv] {
			seen[srv] = true
			srv.Authorize(line)
		}
	}
}

// DialLocal connects to an address on the machine Bield pretends to run on,
// such as its SSH server, for ssh.Options.
func (d *Demo) DialLocal(ctx context.Context, network, addr string) (net.Conn, error) {
	return d.sshd.dialLocal(ctx, network, addr)
}

// HubDocker connects to the Docker of the machine Bield pretends to run on,
// for docker.NewClient. It returns nil if that machine has none.
func (d *Demo) HubDocker() func(ctx context.Context, network, addr string) (net.Conn, error) {
	if eng := d.engines[d.machines[0].id]; eng != nil {
		return eng.dialer()
	}
	return nil
}

// dial is the tailnet's Dial: it reaches the machines' SSH servers, and the
// servers the checks and notifications name.
func (d *Demo) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if _, port, err := net.SplitHostPort(addr); err == nil && port == "22" {
		return d.sshd.route(ctx, network, addr)
	}
	return d.services.dial(ctx, network, addr)
}

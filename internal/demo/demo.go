// Package demo is Bield's demo mode: a tailnet that doesn't exist, with a
// dozen machines on it that are made up, and everything Bield would learn
// about them. It's for screenshots, for the landing page's live demo, for
// trying Bield out without a tailnet, and for testing the UI and Bield's
// speed with hundreds of machines.
//
// A Demo doesn't swap out any of Bield: the checks, the alerts, the Docker
// pages and the SSH layer run as they do for a real tailnet, against
// stand-ins for what's outside Bield. The tailnet is a tailnettest.Tailnet,
// the machines' agents send real heartbeats, their sshd is an sshtest.Server
// that serves a fake Docker, and the checks' targets are in-memory servers.
// What each machine does is a function of the time, so the history a Demo
// starts with is one line with what it shows live.
package demo

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/manasseh-fikadu/bield/internal/tailnet"
	"github.com/manasseh-fikadu/bield/internal/tailnet/tailnettest"
)

// Options configures New.
type Options struct {
	// Devices is how many machines the tailnet has, Bield's own included. It
	// is 12 unless a demo needs more, such as to try Bield with 200.
	Devices int
	// Listen is the TCP address to serve the app on.
	Listen string
	// Seed makes another demo of the same size: it changes the names and
	// numbers of the machines past the twelve that are always the same.
	Seed uint64
	// Logger receives what goes wrong. Nil discards it.
	Logger *slog.Logger
}

// Demo is a made-up tailnet and what runs on it. Create one with New.
type Demo struct {
	log      *slog.Logger
	start    time.Time
	seed     uint64
	machines []*machine
	byID     map[string]*machine
	node     *Node

	host     *Host
	services *services
	sshd     *sshd
	engines  map[string]*engine
	counters map[string]*counters

	mu sync.Mutex
}

// New returns a demo with a made-up tailnet. Nothing runs until Run.
func New(opts Options) *Demo {
	if opts.Devices < 2 {
		opts.Devices = 12
	}
	d := &Demo{
		log:      opts.Logger,
		start:    time.Now(),
		seed:     max(opts.Seed, 1),
		byID:     map[string]*machine{},
		engines:  map[string]*engine{},
		counters: map[string]*counters{},
	}
	if d.log == nil {
		d.log = slog.New(slog.DiscardHandler)
	}
	d.machines = newCast(opts.Devices, d.seed)
	for _, m := range d.machines {
		d.byID[m.id] = m
	}
	hub := d.machines[0]

	tn := tailnettest.New(d.status(d.start))
	tn.SetDERPMap(derpRegions)
	tn.SetConnectivity(&tailnet.Connectivity{
		UDP: true, IPv4: true, IPv6: true,
		PublicIPv4: documentationAddr(7), PreferredDERP: derpRegions[2], DERPLatency: 11 * time.Millisecond,
	}, nil)
	d.node = &Node{
		Tailnet: tn, listen: opts.Listen,
		visitor: tailnet.Identity{
			NodeID: d.machineNamed("macbook", hub).id, NodeName: d.machineNamed("macbook", hub).dns(),
			LoginName: alex, DisplayName: "Alex",
		},
	}
	d.refreshPings(d.start)

	d.host = newHost(d)
	d.services = newServices(d)
	d.sshd = newSSHD(d)
	for _, m := range d.machines {
		if m.hasDk {
			d.engines[m.id] = newEngine(d, m)
		}
	}
	d.sshd.listen()
	d.node.SetDialer(d.dial)
	return d
}

// machineNamed returns the machine with a host name, or fallback.
func (d *Demo) machineNamed(host string, fallback *machine) *machine {
	for _, m := range d.machines {
		if m.host == host {
			return m
		}
	}
	return fallback
}

// Node returns the tailnet, which Bield serves its app on.
func (d *Demo) Node() *Node { return d.node }

// Start is when the demo started, which its history ends at.
func (d *Demo) Start() time.Time { return d.start }

// Run keeps the tailnet moving until ctx ends: it brings the status and the
// pings up to date, and measures Bield's own machine. The machines' agents
// report through Report.
func (d *Demo) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Go(func() { d.host.Run(ctx) })
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case now := <-t.C:
			d.node.SetStatus(d.status(now))
			d.refreshPings(now)
		}
	}
}

// Host returns the collector for Bield's own machine, which stands in for
// the one that reads /proc.
func (d *Demo) Host() *Host { return d.host }

// Close ends the demo's servers.
func (d *Demo) Close() {
	d.sshd.close()
}

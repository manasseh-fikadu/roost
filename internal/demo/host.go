package demo

import (
	"context"
	"sync"
	"time"

	"github.com/manasseh-fikadu/roost/internal/hostmetrics"
)

// Host stands in for hostmetrics.Collector in a demo: it measures the machine
// that Roost pretends to run on, instead of the one it runs on. It answers
// to the same Reading and Changes.
type Host struct {
	d       *Demo
	changes chan struct{}

	mu      sync.Mutex
	window  func() time.Duration
	recent  hostmetrics.CPUWindow
	reading hostmetrics.Reading
}

func newHost(d *Demo) *Host {
	hub := d.machines[0]
	return &Host{
		d: d, changes: make(chan struct{}, 1),
		window: func() time.Duration { return hostmetrics.DefaultWindow },
		reading: hostmetrics.Reading{Info: hostmetrics.Info{
			Hostname: hub.host, OSVersion: hub.osVersion, Kernel: hub.kernel, Model: hub.model, Arch: hub.arch, Cores: hub.cores,
		}},
	}
}

// SetWindow makes the host average the CPU over the span that window returns,
// as hostmetrics.Collector's does. Call it before Run.
func (h *Host) SetWindow(window func() time.Duration) { h.window = window }

// Run measures the machine now and then every hostmetrics.Interval, until ctx
// ends.
func (h *Host) Run(ctx context.Context) {
	// The CPU's recent past is already there, as it is on a machine that has
	// been up for a while.
	hub := h.d.machines[0]
	for at := h.d.start.Add(-hostmetrics.DefaultWindow); at.Before(h.d.start); at = at.Add(hostmetrics.Interval) {
		h.recent.Add(at, hub.at(at, h.d.start).cpu)
	}
	t := time.NewTicker(hostmetrics.Interval)
	defer t.Stop()
	for {
		h.collect(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Reading returns what the host measured last.
func (h *Host) Reading() hostmetrics.Reading {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reading
}

// Changes receives a value after each measurement.
func (h *Host) Changes() <-chan struct{} { return h.changes }

func (h *Host) collect(now time.Time) {
	hub := h.d.machines[0]
	r := hub.at(now, h.d.start)
	h.mu.Lock()
	h.recent.Add(now, r.cpu)
	s := h.d.sample(hub, r, now)
	s.CPUSustained = h.recent.Average(now, h.window())
	h.reading.Sample = &s
	h.mu.Unlock()
	select {
	case h.changes <- struct{}{}:
	default:
	}
}

// sample returns what a machine measured of itself at now, when it was doing r.
// It leaves out what a sample of a machine over time has: CPUSustained.
func (d *Demo) sample(m *machine, r reading, now time.Time) hostmetrics.Sample {
	const gib = 1 << 30
	total := uint64(m.memGB * gib)
	disk := uint64(m.diskGB * gib)
	s := hostmetrics.Sample{
		At: now, CPU: &r.cpu, Load: &r.load, Procs: d.procs(m, r),
		Mem:    hostmetrics.Usage{Used: uint64(float64(total) * r.mem / 100), Total: total},
		Disk:   &hostmetrics.Usage{Used: uint64(float64(disk) * r.disk / 100), Total: disk},
		NetRx:  &r.rx,
		NetTx:  &r.tx,
		Uptime: m.uptime + now.Sub(d.start),
	}
	if m.swapGB > 0 {
		swap := uint64(m.swapGB * gib)
		s.Swap = &hostmetrics.Usage{Used: uint64(float64(swap) * clamp((r.mem-70)/30, 0, 0.6)), Total: swap}
	}
	if r.temp > 0 {
		s.Temp = &r.temp
	}
	return s
}

// procs returns how many processes a machine has while it does r.
func (d *Demo) procs(m *machine, r reading) int {
	n := 90 + 12*m.cores
	for _, c := range m.containers {
		if c.state == "running" {
			n += 4
		}
	}
	if m.os == "macOS" {
		n += 380
	}
	return n + int(r.cpu/4)
}

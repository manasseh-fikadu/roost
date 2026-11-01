package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/manasseh-fikadu/roost/agent"
	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/usage"
)

// agentEvery is how often an agent sends a heartbeat, as agent.sh does.
const agentEvery = 15 * time.Second

// Sinks is where the machines report to, which Roost's pieces take as they
// do from a real agent's request.
type Sinks struct {
	Agents *agents.Registry
	Usage  *usage.Recorder
}

// counters are a machine's network counters, which only go up.
type counters struct {
	at     time.Time
	rx, tx float64
}

// Report plays the machines' agents until ctx ends: each machine that has one
// and is on the tailnet sends a heartbeat every agentEvery, and Roost's own
// machine reports its processes. A machine that left the tailnet reported once,
// when it left, so Roost shows what its agent last said.
func (d *Demo) Report(ctx context.Context, s Sinks) {
	var wg sync.WaitGroup
	wg.Go(func() { d.reportHub(ctx, s) })
	for _, m := range d.machines[1:] {
		if !m.agent {
			continue
		}
		if m.offlineFor > 0 {
			at := d.start.Add(-m.offlineFor)
			d.send(ctx, s, m, at)
			continue
		}
		wg.Go(func() {
			// Every agent has reported twice by the time anyone looks, which
			// is what its network rates take, and the next reports don't all
			// come at the same moment.
			d.send(ctx, s, m, time.Now().Add(-agentEvery))
			d.send(ctx, s, m, time.Now())
			wait := time.Duration(unit(d.seed, int64(m.index)) * float64(agentEvery))
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			t := time.NewTicker(agentEvery)
			defer t.Stop()
			for {
				d.send(ctx, s, m, time.Now())
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		})
	}
	wg.Wait()
}

// reportHub records what runs on Roost's own machine, as the sampler of
// /proc does.
func (d *Demo) reportHub(ctx context.Context, s Sinks) {
	hub := d.machines[0]
	t := time.NewTicker(usage.HubInterval)
	defer t.Stop()
	for {
		now := time.Now()
		s.Usage.Record(ctx, store.HubID, d.usage(hub, hub.at(now, d.start), now, usage.SourceHub))
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// send makes machine m's agent report what the machine was doing at now.
func (d *Demo) send(ctx context.Context, s Sinks, m *machine, now time.Time) {
	var hb agents.Heartbeat
	if err := json.Unmarshal(d.heartbeat(m, now), &hb); err != nil {
		d.log.Error("read a made-up heartbeat", "machine", m.host, "error", err)
		return
	}
	if _, err := s.Agents.Record(ctx, m.id, hb, now); err != nil {
		d.log.Error("record a made-up heartbeat", "machine", m.host, "error", err)
		return
	}
	s.Usage.Record(ctx, m.id, d.usage(m, m.at(now, d.start), now, usage.SourceAgent))
}

// heartbeat returns the JSON that machine m's agent sends at now.
func (d *Demo) heartbeat(m *machine, now time.Time) []byte {
	r := m.at(now, d.start)
	s := d.sample(m, r, now)

	d.mu.Lock()
	c := d.counters[m.id]
	if c == nil {
		// The counters have counted since the machine booted.
		c = &counters{at: now, rx: m.rx * 0.35 * m.uptime.Seconds(), tx: m.tx * 0.35 * m.uptime.Seconds()}
		d.counters[m.id] = c
	}
	if dt := now.Sub(c.at).Seconds(); dt > 0 {
		c.rx += r.rx * dt
		c.tx += r.tx * dt
	}
	c.at = now
	rx, tx := c.rx, c.tx
	d.mu.Unlock()

	metrics := map[string]any{
		"cpu": r.cpu, "cores": m.cores, "load": r.load[:],
		"memUsed": s.Mem.Used, "memTotal": s.Mem.Total,
		"diskUsed": s.Disk.Used, "diskTotal": s.Disk.Total,
		"netRxTotal": uint64(rx), "netTxTotal": uint64(tx),
		"uptime": int64(s.Uptime / time.Second), "procs": s.Procs,
	}
	if s.Swap != nil {
		metrics["swapUsed"], metrics["swapTotal"] = s.Swap.Used, s.Swap.Total
	}
	if r.temp > 0 {
		metrics["temp"] = r.temp
	}
	if m.battery {
		metrics["battery"] = map[string]any{"pct": r.battery, "charging": r.charging, "onAc": r.charging || r.battery > 95}
	}
	b, err := json.Marshal(map[string]any{
		"v": 1, "agentVersion": agent.Version, "hostname": m.host, "osVersion": m.osVersion,
		"kernel": m.kernel, "model": m.model, "arch": m.arch, "metrics": metrics,
	})
	if err != nil {
		panic(fmt.Sprintf("demo: encode a heartbeat: %v", err))
	}
	return b
}

// usage returns what runs on machine m at now, while it does r, as a sample
// of its processes.
func (d *Demo) usage(m *machine, r reading, now time.Time, source usage.Source) usage.Sample {
	const mb = 1e6
	s := d.sample(m, r, now)
	var weight float64
	for _, w := range m.workloads {
		weight += w.cpu
	}
	var (
		groups  []usage.Group
		procs   []usage.Proc
		cpuSum  float64
		memSum  uint64
		pid     = 900
		counted = 0
	)
	for i, w := range m.workloads {
		pid += 211
		cpu := r.cpu * 0.82 * w.cpu / weight * (1 + 0.2*wave(m.seed(uint64(100+i)), now, 3*time.Minute))
		mem := uint64(w.memMB * mb * (1 + 0.04*wave(m.seed(uint64(200+i)), now, 20*time.Minute)))
		key := usage.Key(w.kind, w.name)
		groups = append(groups, usage.Group{Key: key, Kind: w.kind, Name: w.name, CPU: cpu, Mem: mem, Procs: w.procs, Top: w.top})
		cpuSum, memSum, counted = cpuSum+cpu, memSum+mem, counted+w.procs
		if len(w.top) > 0 {
			owner := "root"
			if w.kind == usage.KindApp || w.kind == usage.KindUserService {
				owner = "alex"
			}
			procs = append(procs, usage.Proc{
				PID: pid, Name: w.top[0], User: owner, Cmd: w.top[0], CPU: cpu * 0.7, Mem: uint64(float64(mem) * 0.6), Group: key,
			})
		}
	}
	kernel := usage.Group{Key: usage.Key(usage.KindKernel, "kernel threads"), Kind: usage.KindKernel, Name: "kernel threads", CPU: r.cpu * 0.05, Procs: 110}
	groups = append(groups, kernel)
	cpuSum += kernel.CPU
	counted += kernel.Procs
	if rest := r.cpu - cpuSum; rest > 0 {
		other := usage.Group{
			Key: usage.Key(usage.KindOther, "everything else"), Kind: usage.KindOther, Name: "everything else",
			CPU: rest, Mem: s.Mem.Used - min(s.Mem.Used, memSum), Procs: max(1, s.Procs-counted),
		}
		groups = append(groups, other)
		cpuSum, memSum = cpuSum+other.CPU, memSum+other.Mem
	}
	slices.SortFunc(groups, func(a, b usage.Group) int {
		switch {
		case a.CPU > b.CPU:
			return -1
		case a.CPU < b.CPU:
			return 1
		}
		return 0
	})
	return usage.Sample{
		At: now, Source: source, Cores: m.cores, Groups: groups, Procs: procs,
		Total: &usage.Totals{CPU: cpuSum, Mem: memSum, Procs: s.Procs},
	}
}

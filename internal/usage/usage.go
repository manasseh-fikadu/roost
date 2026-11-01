// Package usage finds out what uses a machine's CPU and memory: which
// services, containers and processes. Bield samples its own machine's
// processes from /proc (Sampler), agents report theirs (ParseAgent), and a
// Recorder keeps each device's latest sample and sums up each minute for its
// history.
package usage

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"
)

// Kind is what a group of processes is.
type Kind string

// The kinds of Group.
const (
	// KindContainer is a Docker or Podman container.
	KindContainer Kind = "container"
	// KindService is a systemd system service.
	KindService Kind = "service"
	// KindUserService is a systemd user service or an app a desktop started.
	KindUserService Kind = "user-service"
	// KindSession is a user's login sessions, such as SSH.
	KindSession Kind = "session"
	// KindKernel is the kernel's own threads.
	KindKernel Kind = "kernel"
	// KindApp is a macOS app bundle.
	KindApp Kind = "app"
	// KindOther is anything else, grouped by its program's name.
	KindOther Kind = "other"
)

var kinds = []Kind{KindContainer, KindService, KindUserService, KindSession, KindKernel, KindApp, KindOther}

// Source says where a Sample came from.
type Source string

// The sources of a Sample.
const (
	// SourceHub is Bield sampling its own machine.
	SourceHub Source = "hub"
	// SourceAgent is a device's agent.
	SourceAgent Source = "agent"
)

// A Group is the processes of one service, container, app or the like.
type Group struct {
	// Key is the group's Kind and Name, as "kind:name". It stays the same
	// from one sample to the next.
	Key  string
	Kind Kind
	Name string
	// CPU is the group's share of the machine's CPU time, all cores
	// together, in percent, so the groups add up to the machine's CPU.
	CPU float64
	// Mem is the group's resident memory in bytes, summed over its
	// processes, so pages they share count once for each.
	Mem   uint64
	Procs int
	// Top names the group's busiest processes, at most three.
	Top []string
}

// A Proc is one process.
type Proc struct {
	PID  int
	Name string
	User string
	// Cmd is the command line, cut short if it's long.
	Cmd string
	// CPU and Mem are as in Group.
	CPU float64
	Mem uint64
	// Group is the Key of the Group the process is in.
	Group string
}

// Totals sum up every process on a machine, including the ones a Sample
// doesn't list.
type Totals struct {
	CPU   float64
	Mem   uint64
	Procs int
}

// A Sample is what used a machine's CPU and memory at one moment, or over a
// minute, when a Recorder sums one up.
type Sample struct {
	At     time.Time
	Source Source
	// Cores is how many CPU cores the machine has, or 0 if it didn't say.
	Cores int
	// Groups are the biggest groups, by CPU and then memory.
	Groups []Group
	// Procs are the biggest processes, by CPU and then memory.
	Procs []Proc
	// Total is nil if the machine didn't say.
	Total *Totals
}

// Key returns the Key of the group of a kind with a name.
func Key(kind Kind, name string) string { return string(kind) + ":" + name }

// SplitKey returns the kind and name in a group's Key. A key of no known kind
// is a name of KindOther.
func SplitKey(key string) (Kind, string) {
	k, name, ok := strings.Cut(key, ":")
	if ok && slices.Contains(kinds, Kind(k)) {
		return Kind(k), name
	}
	return KindOther, key
}

// How much a live sample lists. The groups are nearly always all of them.
const (
	liveGroups = 100
	liveProcs  = 60
)

// A rawProc is a process as a sampler found it.
type rawProc struct {
	pid             int
	name, user, cmd string
	cpu             float64
	mem             uint64
	group           string
}

// newSample groups procs into a Sample.
func newSample(procs []rawProc, at time.Time, source Source, cores int) Sample {
	type entry struct {
		g     Group
		names map[string]*Totals
	}
	groups := map[string]*entry{}
	total := Totals{Procs: len(procs)}
	out := make([]Proc, 0, len(procs))
	for _, p := range procs {
		total.CPU += p.cpu
		total.Mem += p.mem
		e := groups[p.group]
		if e == nil {
			kind, name := SplitKey(p.group)
			e = &entry{g: Group{Key: p.group, Kind: kind, Name: name}, names: map[string]*Totals{}}
			groups[p.group] = e
		}
		e.g.CPU += p.cpu
		e.g.Mem += p.mem
		e.g.Procs++
		n := e.names[p.name]
		if n == nil {
			n = &Totals{}
			e.names[p.name] = n
		}
		n.CPU += p.cpu
		n.Mem += p.mem
		out = append(out, Proc{PID: p.pid, Name: p.name, User: p.user, Cmd: p.cmd, CPU: round2(p.cpu), Mem: p.mem, Group: p.group})
	}
	gs := make([]Group, 0, len(groups))
	for _, e := range groups {
		e.g.CPU = round2(e.g.CPU)
		// By CPU to a tenth of a percent, then memory, so an idle group
		// still names its biggest process.
		names := make([]string, 0, len(e.names))
		for name := range e.names {
			names = append(names, name)
		}
		slices.SortFunc(names, func(a, b string) int {
			x, y := e.names[a], e.names[b]
			return cmp.Or(
				cmp.Compare(math.Round(y.CPU*10), math.Round(x.CPU*10)),
				cmp.Compare(y.Mem, x.Mem),
				cmp.Compare(a, b),
			)
		})
		e.g.Top = names[:min(3, len(names))]
		gs = append(gs, e.g)
	}
	slices.SortFunc(gs, byUse(func(g Group) (float64, uint64) { return g.CPU, g.Mem }))
	total.CPU = round2(total.CPU)
	return Sample{
		At:     at,
		Source: source,
		Cores:  cores,
		Groups: gs[:min(liveGroups, len(gs))],
		Procs:  top(out, liveProcs, func(p Proc) (float64, uint64) { return p.CPU, p.Mem }),
		Total:  &total,
	}
}

// top returns the n biggest items by CPU, and the n biggest by memory,
// leaving out zeros, ordered by CPU and then memory. use returns an item's
// CPU and memory.
func top[T any](items []T, n int, use func(T) (float64, uint64)) []T {
	picked := make([]bool, len(items))
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	for _, by := range []func(i int) float64{
		func(i int) float64 { c, _ := use(items[i]); return c },
		func(i int) float64 { _, m := use(items[i]); return float64(m) },
	} {
		slices.SortStableFunc(idx, func(a, b int) int { return cmp.Compare(by(b), by(a)) })
		for _, i := range idx[:min(n, len(idx))] {
			if by(i) > 0 {
				picked[i] = true
			}
		}
	}
	var out []T
	for i, item := range items {
		if picked[i] {
			out = append(out, item)
		}
	}
	slices.SortStableFunc(out, byUse(use))
	return out
}

// byUse orders items by CPU and then memory, biggest first.
func byUse[T any](use func(T) (float64, uint64)) func(a, b T) int {
	return func(a, b T) int {
		ac, am := use(a)
		bc, bm := use(b)
		return cmp.Or(cmp.Compare(bc, ac), cmp.Compare(bm, am))
	}
}

// round2 rounds a percentage to two decimals.
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// Package hostmetrics measures the machine Roost runs on: CPU, memory, swap,
// disk, load, network, temperature and uptime. It reads them from /proc and
// /sys, so it only works on Linux. Other machines need the agent.
package hostmetrics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Supported reports whether a Collector can measure this machine.
const Supported = runtime.GOOS == "linux"

const (
	// Interval is how often a Collector measures the machine, as the old
	// dashboard did.
	Interval = 5 * time.Second
	// DefaultWindow is how far back Sample.CPUSustained averages unless a
	// Collector is told otherwise.
	DefaultWindow = 5 * time.Minute
	// MaxWindow is the longest span a CPUWindow averages over, and so how far
	// back it remembers.
	MaxWindow = time.Hour
)

// Sample is what the machine was doing at one moment. A field is nil, or
// zero, if the machine doesn't say.
type Sample struct {
	At time.Time
	// CPU is the percentage of time the CPUs were busy since the previous
	// sample. It's nil in the first sample.
	CPU *float64
	// CPUSustained is CPU averaged over the last window, which is
	// DefaultWindow unless the Collector is told otherwise. It's nil until
	// the samples cover most of the window.
	CPUSustained *float64
	// Load is the 1, 5 and 15 minute load averages.
	Load *[3]float64
	// Procs is how many processes there are.
	Procs int
	Mem   Usage
	// Swap is nil if the machine has none.
	Swap *Usage
	// Disk is the file system that holds /. As in df, the blocks reserved
	// for root are left out, so Total is what's used plus what's free for
	// everyone else.
	Disk *Usage
	// NetRx and NetTx are bytes per second received and sent since the
	// previous sample, on the interface of the default route.
	NetRx, NetTx *float64
	// Temp is the hottest thermal zone, in degrees Celsius. Virtual machines
	// often have none.
	Temp *float64
	// Uptime is how long since the machine booted.
	Uptime time.Duration
}

// Usage is how much of something is used, in bytes.
type Usage struct {
	Used, Total uint64
}

// Info is what the machine is. It doesn't change while Roost runs.
type Info struct {
	Hostname string
	// OSVersion is the distribution's name and version, such as
	// "Debian GNU/Linux 13 (trixie)".
	OSVersion string
	// Kernel is the kernel's release, such as "6.12.0-1-amd64".
	Kernel string
	// Model is the machine's maker and product, such as "QEMU Standard PC",
	// if the firmware says.
	Model string
	// Arch is the CPU architecture, such as x86_64.
	Arch  string
	Cores int
}

// System is where a Collector reads the machine. Local reads this machine,
// and tests fake it.
type System struct {
	// FS is the machine's root directory, for /proc, /sys and /etc.
	FS fs.FS
	// Statfs returns how much of the file system that holds path is used,
	// and how much more of it users other than root can use, in bytes, as df
	// counts them.
	Statfs func(path string) (used, avail uint64, err error)
}

// Reading is what a Collector knows at one moment.
type Reading struct {
	// Sample is the latest sample that worked, or nil before the first.
	Sample *Sample
	Info   Info
	// Err is why the latest sample failed, if it did. Sample is then the last
	// one that worked.
	Err error
}

// Collector measures the machine every Interval and keeps the latest
// sample. It's safe for concurrent use.
type Collector struct {
	r       *reader
	log     *slog.Logger
	changes chan struct{}

	mu      sync.Mutex
	reading Reading
}

// NewCollector returns a Collector of sys. Nothing is measured until Run.
func NewCollector(sys System, log *slog.Logger) *Collector {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Collector{
		r:   &reader{sys: sys, window: func() time.Duration { return DefaultWindow }},
		log: log, changes: make(chan struct{}, 1),
	}
}

// SetWindow makes the collector average the CPU over the span that window
// returns, up to MaxWindow, each time it measures. Call it before Run.
func (c *Collector) SetWindow(window func() time.Duration) { c.r.window = window }

// Run measures the machine now and then every Interval, until ctx ends.
func (c *Collector) Run(ctx context.Context) {
	info := c.r.info()
	c.mu.Lock()
	c.reading.Info = info
	c.mu.Unlock()

	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		c.collect()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Reading returns what the collector knows now.
func (c *Collector) Reading() Reading {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reading
}

// Changes receives a value after each sample, whether or not it worked. Only
// one goroutine should receive from it.
func (c *Collector) Changes() <-chan struct{} { return c.changes }

func (c *Collector) collect() {
	s, err := c.r.sample(time.Now())
	c.mu.Lock()
	switch {
	case err != nil:
		if c.reading.Err == nil {
			c.log.Warn("measure this machine", "error", err)
		}
		c.reading.Err = err
	default:
		if c.reading.Err != nil {
			c.log.Info("measured this machine again")
		}
		c.reading.Sample, c.reading.Err = &s, nil
	}
	c.mu.Unlock()
	select {
	case c.changes <- struct{}{}:
	default:
	}
}

// reader takes samples of a System. Rates come from the difference between
// one sample's counters and the previous one's, so it keeps those.
type reader struct {
	sys System

	prevCPU *cpuTimes
	prevNet *netTotals
	recent  CPUWindow
	// window is how far back CPUSustained averages.
	window func() time.Duration
}

type cpuTimes struct{ busy, total uint64 }

type netTotals struct {
	iface  string
	at     time.Time
	rx, tx uint64
}

// sample measures the machine at now. It fails only if it can't read the
// CPU or memory. Anything else it can't read is left out.
func (r *reader) sample(now time.Time) (Sample, error) {
	s := Sample{At: now}
	cpu, err := r.cpu()
	if err != nil {
		return Sample{}, err
	}
	if prev := r.prevCPU; prev != nil && cpu.total > prev.total && cpu.busy >= prev.busy {
		pct := min(100, float64(cpu.busy-prev.busy)/float64(cpu.total-prev.total)*100)
		s.CPU = &pct
		r.recent.Add(now, pct)
	}
	r.prevCPU = &cpu
	s.CPUSustained = r.recent.Average(now, r.window())

	mem, swap, err := r.memory()
	if err != nil {
		return Sample{}, err
	}
	s.Mem, s.Swap = mem, swap

	s.Load, s.Procs = r.load()
	if used, avail, err := r.sys.Statfs("/"); err == nil && used+avail > 0 {
		s.Disk = &Usage{Used: used, Total: used + avail}
	}
	s.NetRx, s.NetTx = r.network(now)
	s.Temp = r.temperature()
	if up, err := r.read("proc/uptime"); err == nil {
		if f := strings.Fields(up); len(f) > 0 {
			if secs, err := strconv.ParseFloat(f[0], 64); err == nil {
				s.Uptime = time.Duration(secs) * time.Second
			}
		}
	}
	return s, nil
}

func (r *reader) read(name string) (string, error) {
	b, err := fs.ReadFile(r.sys.FS, name)
	return string(b), err
}

// cpu reads the time all CPUs have spent busy and in total from /proc/stat.
func (r *reader) cpu() (cpuTimes, error) {
	stat, err := r.read("proc/stat")
	if err != nil {
		return cpuTimes{}, err
	}
	line, ok := findLine(stat, "cpu ")
	if !ok {
		return cpuTimes{}, errors.New("no cpu line in /proc/stat")
	}
	// user nice system idle iowait irq softirq steal. Guest time is already
	// counted in user and nice.
	f := strings.Fields(line)[1:]
	var t cpuTimes
	for i := 0; i < len(f) && i < 8; i++ {
		v, err := strconv.ParseUint(f[i], 10, 64)
		if err != nil {
			return cpuTimes{}, fmt.Errorf("read /proc/stat: %w", err)
		}
		t.total += v
		if i != 3 && i != 4 { // idle and iowait aren't busy
			t.busy += v
		}
	}
	return t, nil
}

// memory reads memory and swap from /proc/meminfo. Used memory is what isn't
// available, so the page cache doesn't count.
func (r *reader) memory() (mem Usage, swap *Usage, err error) {
	text, err := r.read("proc/meminfo")
	if err != nil {
		return Usage{}, nil, err
	}
	kb := map[string]uint64{}
	for line := range strings.Lines(text) {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		if v, err := strconv.ParseUint(f[0], 10, 64); err == nil {
			kb[key] = v
		}
	}
	total, ok := kb["MemTotal"]
	avail, ok2 := kb["MemAvailable"]
	if !ok || !ok2 || total == 0 {
		return Usage{}, nil, errors.New("no MemTotal or MemAvailable in /proc/meminfo")
	}
	mem = Usage{Used: (total - min(avail, total)) * 1024, Total: total * 1024}
	if st, sf := kb["SwapTotal"], kb["SwapFree"]; st > 0 {
		swap = &Usage{Used: (st - min(sf, st)) * 1024, Total: st * 1024}
	}
	return mem, swap, nil
}

// load reads the load averages and the number of processes from
// /proc/loadavg, which looks like "0.06 0.52 0.82 1/377 1320487".
func (r *reader) load() (*[3]float64, int) {
	text, err := r.read("proc/loadavg")
	if err != nil {
		return nil, 0
	}
	f := strings.Fields(text)
	if len(f) < 4 {
		return nil, 0
	}
	var load [3]float64
	for i := range load {
		if load[i], err = strconv.ParseFloat(f[i], 64); err != nil {
			return nil, 0
		}
	}
	_, n, _ := strings.Cut(f[3], "/")
	procs, _ := strconv.Atoi(n)
	return &load, procs
}

// network returns the bytes per second received and sent on the interface
// of the default route since the previous sample.
func (r *reader) network(now time.Time) (rx, tx *float64) {
	prev := r.prevNet
	cur, ok := r.netTotals(r.defaultInterface())
	if !ok {
		r.prevNet = nil
		return nil, nil
	}
	cur.at = now
	r.prevNet = &cur
	// A counter that went back means the interface was reset.
	if prev == nil || prev.iface != cur.iface || !now.After(prev.at) || cur.rx < prev.rx || cur.tx < prev.tx {
		return nil, nil
	}
	secs := now.Sub(prev.at).Seconds()
	rxRate, txRate := float64(cur.rx-prev.rx)/secs, float64(cur.tx-prev.tx)/secs
	return &rxRate, &txRate
}

// defaultInterface returns the interface of the IPv4 default route, from
// /proc/net/route, or "".
func (r *reader) defaultInterface() string {
	text, err := r.read("proc/net/route")
	if err != nil {
		return ""
	}
	for line := range strings.Lines(text) {
		f := strings.Fields(line)
		if len(f) > 1 && f[1] == "00000000" {
			return f[0]
		}
	}
	return ""
}

// netTotals reads the bytes iface has received and sent from /proc/net/dev.
func (r *reader) netTotals(iface string) (netTotals, bool) {
	if iface == "" {
		return netTotals{}, false
	}
	text, err := r.read("proc/net/dev")
	if err != nil {
		return netTotals{}, false
	}
	for line := range strings.Lines(text) {
		name, rest, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) != iface {
			continue
		}
		// Receive: bytes packets errs drop fifo frame compressed multicast,
		// then transmit, starting with bytes.
		f := strings.Fields(rest)
		if len(f) < 9 {
			return netTotals{}, false
		}
		rx, err1 := strconv.ParseUint(f[0], 10, 64)
		tx, err2 := strconv.ParseUint(f[8], 10, 64)
		if err1 != nil || err2 != nil {
			return netTotals{}, false
		}
		return netTotals{iface: iface, rx: rx, tx: tx}, true
	}
	return netTotals{}, false
}

// temperature returns the hottest thermal zone in degrees Celsius, or nil if
// there are none.
func (r *reader) temperature() *float64 {
	zones, err := fs.ReadDir(r.sys.FS, "sys/class/thermal")
	if err != nil {
		return nil
	}
	var hottest *float64
	for _, z := range zones {
		if !strings.HasPrefix(z.Name(), "thermal_zone") {
			continue
		}
		text, err := r.read("sys/class/thermal/" + z.Name() + "/temp")
		if err != nil {
			continue
		}
		milli, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil || milli <= 0 {
			continue
		}
		if c := milli / 1000; hottest == nil || c > *hottest {
			hottest = &c
		}
	}
	return hottest
}

// info reads what the machine is.
func (r *reader) info() Info {
	trimmed := func(name string) string {
		s, _ := r.read(name)
		return strings.TrimSpace(s)
	}
	info := Info{
		Hostname: trimmed("proc/sys/kernel/hostname"),
		Kernel:   trimmed("proc/sys/kernel/osrelease"),
		Arch:     arch(runtime.GOARCH),
	}
	if release, err := r.read("etc/os-release"); err == nil {
		info.OSVersion = osRelease(release, "PRETTY_NAME")
	}
	if info.OSVersion == "" {
		info.OSVersion = "Linux " + info.Kernel
	}
	var model []string
	for _, name := range []string{"sys_vendor", "product_name"} {
		if v := trimmed("sys/devices/virtual/dmi/id/" + name); v != "" {
			model = append(model, v)
		}
	}
	info.Model = strings.Join(model, " ")
	if stat, err := r.read("proc/stat"); err == nil {
		for line := range strings.Lines(stat) {
			if len(line) > 3 && strings.HasPrefix(line, "cpu") && line[3] >= '0' && line[3] <= '9' {
				info.Cores++
			}
		}
	}
	return info
}

// osRelease returns the value of key in the text of /etc/os-release.
func osRelease(text, key string) string {
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if ok && k == key {
			if u, err := strconv.Unquote(v); err == nil {
				return u
			}
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}

// arch names a Go architecture the way uname -m does.
func arch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "386":
		return "i686"
	case "arm64":
		return "aarch64"
	default:
		return goarch
	}
}

// findLine returns the first line of text that starts with prefix.
func findLine(text, prefix string) (string, bool) {
	for line := range strings.Lines(text) {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimRight(line, "\n"), true
		}
	}
	return "", false
}

// CPUWindow holds the CPU readings of the last MaxWindow, to tell a busy
// moment from a busy stretch. The zero value is an empty window.
type CPUWindow struct {
	points []cpuPoint
}

type cpuPoint struct {
	at  time.Time
	cpu float64
}

// Add adds the percentage of time the CPUs were busy at a moment. Moments
// must be added in order. It forgets readings older than MaxWindow.
func (w *CPUWindow) Add(at time.Time, cpu float64) {
	drop := 0
	for drop < len(w.points) && at.Sub(w.points[drop].at) > MaxWindow {
		drop++
	}
	w.points = append(w.points[drop:], cpuPoint{at, cpu})
}

// Average returns the average CPU over the span before now, which can be up
// to MaxWindow, if the readings cover most of it.
func (w *CPUWindow) Average(now time.Time, window time.Duration) *float64 {
	window = min(window, MaxWindow)
	first := len(w.points)
	for first > 0 && now.Sub(w.points[first-1].at) <= window {
		first--
	}
	recent := w.points[first:]
	if len(recent) < 3 || now.Sub(recent[0].at) < window*8/10 {
		return nil
	}
	var sum float64
	for _, p := range recent {
		sum += p.cpu
	}
	avg := sum / float64(len(recent))
	return &avg
}

// Package agents keeps what Bield's agents report. The agent is a shell
// script in agent/ that sends a heartbeat with its machine's metrics every 15
// seconds. A Registry checks each heartbeat, keeps each device's latest
// report, and stores it, so Bield still knows the device after a restart.
package agents

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/manasseh-fikadu/bield/internal/hostmetrics"
	"github.com/manasseh-fikadu/bield/internal/store"
)

const (
	// StaleAfter is how long an agent can go without a heartbeat before it
	// counts as stopped, six times the interval it sends them at.
	StaleAfter = 90 * time.Second
	// saveEvery is how often a device's latest report is stored.
	saveEvery = time.Minute
	// maxRateGap is the longest time between two heartbeats that network
	// rates are worked out over.
	maxRateGap = 10 * time.Minute
)

// Limits on what an agent can report.
const (
	maxText    = 120
	maxShort   = 32
	maxBytes   = 1 << 60 // an exabyte
	maxCores   = 4096
	maxProcs   = 1e7
	maxUptime  = 100 * 365 * 24 * time.Hour
	maxTempAbs = 1000
)

// ErrVersion means a heartbeat isn't in a format Bield knows.
var ErrVersion = errors.New("this heartbeat's format isn't one Bield knows; reinstall the agent")

// A Heartbeat is what the agent sends to POST /api/agent/heartbeat. Anyone
// with the agents' token can send one, so Record checks every value, and a
// value of the wrong type counts as missing.
type Heartbeat struct {
	// V is the heartbeat's format, which must be 1.
	V            int     `json:"v"`
	AgentVersion text    `json:"agentVersion"`
	Hostname     text    `json:"hostname"`
	OSVersion    text    `json:"osVersion"`
	Kernel       text    `json:"kernel"`
	Model        text    `json:"model"`
	Arch         text    `json:"arch"`
	Metrics      metrics `json:"metrics"`
	// Usage is the machine's processes, which package usage reads.
	Usage json.RawMessage `json:"usage"`
}

type metrics struct {
	CPU        number   `json:"cpu"`
	Cores      number   `json:"cores"`
	Load       []number `json:"load"`
	MemUsed    number   `json:"memUsed"`
	MemTotal   number   `json:"memTotal"`
	SwapUsed   number   `json:"swapUsed"`
	SwapTotal  number   `json:"swapTotal"`
	DiskUsed   number   `json:"diskUsed"`
	DiskTotal  number   `json:"diskTotal"`
	NetRxTotal number   `json:"netRxTotal"`
	NetTxTotal number   `json:"netTxTotal"`
	Temp       number   `json:"temp"`
	Battery    *battery `json:"battery"`
	Uptime     number   `json:"uptime"`
	Procs      number   `json:"procs"`
}

type battery struct {
	Pct      number `json:"pct"`
	Charging flag   `json:"charging"`
	OnAC     flag   `json:"onAc"`
}

// number is a JSON value that counts only if it's a number.
type number struct{ v *float64 }

func (n *number) UnmarshalJSON(b []byte) error {
	// Unmarshal takes null as a float64 without an error, and leaves it 0.
	var f *float64
	if json.Unmarshal(b, &f) == nil {
		n.v = f
	}
	return nil
}

// text is a JSON value that counts only if it's a string.
type text string

func (t *text) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = text(s)
	}
	return nil
}

// flag is a JSON value that's true only if it's true.
type flag bool

func (f *flag) UnmarshalJSON(b []byte) error {
	*f = string(b) == "true"
	return nil
}

// A Report is what Bield knows of a device from its agent.
type Report struct {
	// ReceivedAt is when the latest heartbeat arrived, by Bield's clock.
	ReceivedAt time.Time
	// Version is the agent's version, such as 1.2.0.
	Version string
	// Sample is the machine's metrics. Its At is ReceivedAt.
	Sample hostmetrics.Sample
	Info   hostmetrics.Info
	// Battery is nil if the machine has none.
	Battery *Battery
}

// Battery is how charged a machine's battery is.
type Battery struct {
	Percent float64
	// Charging reports whether the battery is charging, and OnAC whether
	// the machine runs on mains power.
	Charging, OnAC bool
}

// Stale reports whether the agent stopped sending heartbeats by now.
func (r Report) Stale(now time.Time) bool { return now.Sub(r.ReceivedAt) > StaleAfter }

// A Registry keeps the latest Report of each device's agent. It's safe for
// concurrent use.
type Registry struct {
	st      *store.Store
	log     *slog.Logger
	changes chan struct{}
	// window is how far back an agent's sustained CPU averages.
	window func() time.Duration

	mu     sync.Mutex
	agents map[string]*agent
}

type agent struct {
	report Report
	// net is the network counters of the previous heartbeat, if it had them.
	net *counters
	cpu hostmetrics.CPUWindow
	// savedAt is when the report was last stored.
	savedAt time.Time
}

type counters struct {
	at     time.Time
	rx, tx float64
}

// NewRegistry returns a Registry that stores reports in st.
func NewRegistry(st *store.Store, log *slog.Logger) *Registry {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Registry{
		st: st, log: log, changes: make(chan struct{}, 1), agents: map[string]*agent{},
		window: func() time.Duration { return hostmetrics.DefaultWindow },
	}
}

// SetWindow makes the registry average each agent's CPU over the span that
// window returns, up to hostmetrics.MaxWindow, as heartbeats come in. Call it
// before the first heartbeat.
func (r *Registry) SetWindow(window func() time.Duration) { r.window = window }

// Load reads the reports the store kept, so Bield knows the devices before
// their agents report again. A report it can't read is skipped.
func (r *Registry) Load(ctx context.Context) error {
	records, err := r.st.Agents(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range records {
		var s saved
		if err := json.Unmarshal(rec.Report, &s); err != nil {
			r.log.Warn("read an agent's stored report", "device", rec.DeviceID, "error", err)
			continue
		}
		r.agents[rec.DeviceID] = &agent{report: s.report(rec.ReceivedAt), savedAt: rec.ReceivedAt}
	}
	return nil
}

// Changes receives a value after each heartbeat, and when an agent is
// forgotten. Only one goroutine should receive from it.
func (r *Registry) Changes() <-chan struct{} { return r.changes }

// Reports returns the latest report of each device's agent, by device ID.
func (r *Registry) Reports() map[string]Report {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]Report, len(r.agents))
	for id, a := range r.agents {
		out[id] = a.report
	}
	return out
}

// Record takes a heartbeat that a device's agent sent at now, and returns
// the device's report. It fails only with ErrVersion. If it can't store the
// report, it logs why and keeps it anyway.
func (r *Registry) Record(ctx context.Context, deviceID string, hb Heartbeat, now time.Time) (Report, error) {
	if hb.V != 1 {
		return Report{}, ErrVersion
	}
	// The window can come from the store, so it isn't asked for under the lock.
	window := r.window()
	r.mu.Lock()
	a := r.agents[deviceID]
	switch {
	case a == nil:
		a = &agent{}
		r.agents[deviceID] = a
		r.log.Info("an agent started reporting", "device", deviceID, "version", hb.AgentVersion)
	case a.report.Stale(now):
		r.log.Info("an agent is reporting again", "device", deviceID, "after", now.Sub(a.report.ReceivedAt).Round(time.Second))
	}
	a.report = a.parse(hb, now, window)
	var (
		save    []byte
		saveErr error
	)
	if now.Sub(a.savedAt) >= saveEvery {
		a.savedAt = now
		save, saveErr = json.Marshal(newSaved(a.report))
	}
	report := a.report
	r.mu.Unlock()

	if saveErr != nil {
		r.log.Warn("encode an agent's report", "device", deviceID, "error", saveErr)
	}
	if save != nil {
		rec := store.AgentRecord{DeviceID: deviceID, ReceivedAt: now, Report: save}
		if err := r.st.SaveAgent(context.WithoutCancel(ctx), rec); err != nil {
			r.log.Warn("store an agent's report", "device", deviceID, "error", err)
		}
	}
	r.changed()
	return report, nil
}

// Forget drops a device's agent, which shows up again if it sends another
// heartbeat. It returns store.ErrNoAgent if the device has none.
func (r *Registry) Forget(ctx context.Context, deviceID string) error {
	r.mu.Lock()
	_, known := r.agents[deviceID]
	delete(r.agents, deviceID)
	r.mu.Unlock()
	err := r.st.ForgetAgent(ctx, deviceID)
	if known && errors.Is(err, store.ErrNoAgent) {
		// It hadn't been stored yet.
		err = nil
	}
	if err == nil {
		r.changed()
	}
	return err
}

func (r *Registry) changed() {
	select {
	case r.changes <- struct{}{}:
	default:
	}
}

// parse checks a heartbeat that arrived at now, and works out network rates
// from the counters in it and the previous one.
func (a *agent) parse(hb Heartbeat, now time.Time, window time.Duration) Report {
	m := hb.Metrics
	s := hostmetrics.Sample{At: now, CPU: percent(m.CPU), Procs: int(upTo(m.Procs, maxProcs))}
	if s.CPU != nil {
		a.cpu.Add(now, *s.CPU)
	}
	s.CPUSustained = a.cpu.Average(now, window)
	if len(m.Load) == 3 && m.Load[0].v != nil && m.Load[1].v != nil && m.Load[2].v != nil {
		s.Load = &[3]float64{max(0, *m.Load[0].v), max(0, *m.Load[1].v), max(0, *m.Load[2].v)}
	}
	if u := usage(m.MemUsed, m.MemTotal); u != nil {
		s.Mem = *u
	}
	s.Swap = usage(m.SwapUsed, m.SwapTotal)
	s.Disk = usage(m.DiskUsed, m.DiskTotal)
	if t := m.Temp.v; t != nil && math.Abs(*t) <= maxTempAbs {
		s.Temp = t
	}
	s.Uptime = time.Duration(upTo(m.Uptime, maxUptime.Seconds())) * time.Second

	rx, tx := m.NetRxTotal.v, m.NetTxTotal.v
	if rx != nil && tx != nil && *rx >= 0 && *tx >= 0 {
		// Counters start again when a machine boots or changes interfaces,
		// so a heartbeat whose counters went back has no rates.
		if p := a.net; p != nil && now.After(p.at) && now.Sub(p.at) < maxRateGap && *rx >= p.rx && *tx >= p.tx {
			secs := now.Sub(p.at).Seconds()
			rxRate, txRate := (*rx-p.rx)/secs, (*tx-p.tx)/secs
			s.NetRx, s.NetTx = &rxRate, &txRate
		}
		a.net = &counters{at: now, rx: *rx, tx: *tx}
	} else {
		a.net = nil
	}

	report := Report{
		ReceivedAt: now,
		Version:    clean(hb.AgentVersion, maxShort),
		Sample:     s,
		Info: hostmetrics.Info{
			Hostname:  clean(hb.Hostname, maxText),
			OSVersion: clean(hb.OSVersion, maxText),
			Kernel:    clean(hb.Kernel, maxText),
			Model:     clean(hb.Model, maxText),
			Arch:      clean(hb.Arch, maxShort),
			Cores:     int(upTo(m.Cores, maxCores)),
		},
	}
	if b := m.Battery; b != nil {
		if pct := percent(b.Pct); pct != nil {
			report.Battery = &Battery{Percent: *pct, Charging: bool(b.Charging), OnAC: bool(b.OnAC)}
		}
	}
	return report
}

// percent returns n as a percentage from 0 to 100, or nil if it's missing.
func percent(n number) *float64 {
	if n.v == nil {
		return nil
	}
	p := upTo(n, 100)
	return &p
}

// usage returns how much of something is used, if the total is known.
func usage(used, total number) *hostmetrics.Usage {
	if total.v == nil || *total.v <= 0 {
		return nil
	}
	t := uint64(upTo(total, maxBytes))
	return &hostmetrics.Usage{Used: min(t, uint64(upTo(used, maxBytes))), Total: t}
}

// upTo returns n between 0 and most, or 0 if it's missing.
func upTo(n number, most float64) float64 {
	if n.v == nil {
		return 0
	}
	return min(most, max(0, *n.v))
}

// clean returns t on one line, trimmed and cut to at most n characters.
func clean(t text, n int) string {
	s := strings.TrimSpace(strings.Join(strings.FieldsFunc(string(t), unicode.IsControl), " "))
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:n]))
	}
	return s
}

// saved is a Report as the store keeps it. Network rates and sustained CPU
// aren't kept, since they're out of date by the time anyone reads them.
type saved struct {
	Version  string        `json:"version"`
	CPU      *float64      `json:"cpu,omitempty"`
	Load     *[3]float64   `json:"load,omitempty"`
	Procs    int           `json:"procs,omitempty"`
	Mem      *savedUsage   `json:"mem,omitempty"`
	Swap     *savedUsage   `json:"swap,omitempty"`
	Disk     *savedUsage   `json:"disk,omitempty"`
	Temp     *float64      `json:"temp,omitempty"`
	Uptime   int64         `json:"uptime,omitempty"` // seconds
	Battery  *savedBattery `json:"battery,omitempty"`
	Hostname string        `json:"hostname,omitempty"`
	OS       string        `json:"osVersion,omitempty"`
	Kernel   string        `json:"kernel,omitempty"`
	Model    string        `json:"model,omitempty"`
	Arch     string        `json:"arch,omitempty"`
	Cores    int           `json:"cores,omitempty"`
}

type savedUsage struct {
	Used  uint64 `json:"used"`
	Total uint64 `json:"total"`
}

type savedBattery struct {
	Percent  float64 `json:"percent"`
	Charging bool    `json:"charging,omitempty"`
	OnAC     bool    `json:"onAc,omitempty"`
}

func newSaved(r Report) saved {
	s := r.Sample
	out := saved{
		Version: r.Version, CPU: s.CPU, Load: s.Load, Procs: s.Procs, Temp: s.Temp,
		Swap: saveUsage(s.Swap), Disk: saveUsage(s.Disk), Uptime: int64(s.Uptime / time.Second),
		Hostname: r.Info.Hostname, OS: r.Info.OSVersion, Kernel: r.Info.Kernel, Model: r.Info.Model,
		Arch: r.Info.Arch, Cores: r.Info.Cores,
	}
	if s.Mem.Total > 0 {
		out.Mem = saveUsage(&s.Mem)
	}
	if b := r.Battery; b != nil {
		out.Battery = &savedBattery{Percent: b.Percent, Charging: b.Charging, OnAC: b.OnAC}
	}
	return out
}

func saveUsage(u *hostmetrics.Usage) *savedUsage {
	if u == nil {
		return nil
	}
	return &savedUsage{Used: u.Used, Total: u.Total}
}

func (s saved) report(at time.Time) Report {
	usage := func(u *savedUsage) *hostmetrics.Usage {
		if u == nil {
			return nil
		}
		return &hostmetrics.Usage{Used: u.Used, Total: u.Total}
	}
	r := Report{
		ReceivedAt: at,
		Version:    s.Version,
		Sample: hostmetrics.Sample{
			At: at, CPU: s.CPU, Load: s.Load, Procs: s.Procs, Temp: s.Temp,
			Swap: usage(s.Swap), Disk: usage(s.Disk), Uptime: time.Duration(s.Uptime) * time.Second,
		},
		Info: hostmetrics.Info{
			Hostname: s.Hostname, OSVersion: s.OS, Kernel: s.Kernel, Model: s.Model, Arch: s.Arch, Cores: s.Cores,
		},
	}
	if m := usage(s.Mem); m != nil {
		r.Sample.Mem = *m
	}
	if b := s.Battery; b != nil {
		r.Battery = &Battery{Percent: b.Percent, Charging: b.Charging, OnAC: b.OnAC}
	}
	return r
}

package usage

import (
	"bytes"
	"cmp"
	"compress/flate"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
)

const (
	// minute is how long a stored sample sums up.
	minute = time.Minute
	// idleAfter is how long a device can go without a sample before its
	// minute so far is stored anyway.
	idleAfter = 2 * time.Minute
	// A stored minute keeps fewer groups and processes than a live sample,
	// since history only needs the big ones.
	storeGroups = 15
	storeProcs  = 12
	maxStoreCmd = 200
	// gapAfter is the longest wait between samples that counts as the
	// device's interval, rather than as a gap.
	gapAfter = 5 * time.Minute
	// minInterval is the shortest interval a device reports.
	minInterval = 5 * time.Second
	// stopTimeout is how long Run takes, at most, to store the minutes so
	// far when it stops.
	stopTimeout = 5 * time.Second
)

// A Recorder keeps each device's latest Sample, and stores a sum of each
// minute's samples for history: the average CPU, and the memory at the end
// of the minute. It's safe for concurrent use.
type Recorder struct {
	st  *store.Store
	log *slog.Logger

	mu        sync.Mutex
	latest    map[string]Sample
	intervals map[string]time.Duration
	minutes   map[string]*minuteSum
}

// minuteSum is a device's samples so far in a minute.
type minuteSum struct {
	since time.Time
	n     int
	last  Sample
	// total is the latest sample's totals, totals counts the samples that
	// had them, and totalCPU adds up their CPU.
	total    *Totals
	totalCPU float64
	totals   int
	groups   map[string]*sum[Group]
	procs    map[procID]*sum[Proc]
}

type procID struct {
	pid  int
	name string
}

type sum[T any] struct {
	latest T
	cpu    float64
	seenAt time.Time
}

// NewRecorder returns a Recorder that stores minutes in st.
func NewRecorder(st *store.Store, log *slog.Logger) *Recorder {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Recorder{
		st: st, log: log,
		latest: map[string]Sample{}, intervals: map[string]time.Duration{}, minutes: map[string]*minuteSum{},
	}
}

// Run stores the minutes of devices that stopped sending samples, until ctx
// ends. Then it stores every device's minute so far, so a restart doesn't
// lose it.
func (r *Recorder) Run(ctx context.Context) {
	t := time.NewTicker(minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
			r.saveWhere(stopCtx, func(*minuteSum) bool { return true })
			cancel()
			return
		case <-t.C:
		}
		now := time.Now()
		r.saveWhere(ctx, func(m *minuteSum) bool { return now.Sub(m.last.At) > idleAfter })
	}
}

// saveWhere takes the minutes that done reports true for out of r.minutes,
// and stores them.
func (r *Recorder) saveWhere(ctx context.Context, done func(*minuteSum) bool) {
	r.mu.Lock()
	taken := map[string]*minuteSum{}
	for id, m := range r.minutes {
		if done(m) {
			taken[id] = m
			delete(r.minutes, id)
		}
	}
	r.mu.Unlock()
	for id, m := range taken {
		r.save(ctx, id, m)
	}
}

// Record makes s the latest sample of a device, and adds it to the minute.
// Once a minute's worth is in, it stores the minute.
func (r *Recorder) Record(ctx context.Context, deviceID string, s Sample) {
	r.mu.Lock()
	if prev, ok := r.latest[deviceID]; ok {
		if gap := s.At.Sub(prev.At); gap > 0 && gap < gapAfter {
			r.intervals[deviceID] = max(minInterval, gap.Round(time.Second))
		}
	}
	r.latest[deviceID] = s
	var done *minuteSum
	m := r.minutes[deviceID]
	// The sample that ends a minute starts the next.
	if m != nil && s.At.Sub(m.since) >= minute-time.Second {
		done, m = m, nil
	}
	if m == nil {
		m = &minuteSum{since: s.At, groups: map[string]*sum[Group]{}, procs: map[procID]*sum[Proc]{}}
		r.minutes[deviceID] = m
	}
	m.add(s)
	r.mu.Unlock()
	if done != nil {
		r.save(context.WithoutCancel(ctx), deviceID, done)
	}
}

// Latest returns a device's latest sample, and how often it sends one.
func (r *Recorder) Latest(deviceID string) (Sample, time.Duration, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.latest[deviceID]
	interval, known := r.intervals[deviceID]
	switch {
	case known:
	case s.Source == SourceHub:
		interval = HubInterval
	default:
		interval = 15 * time.Second
	}
	return s, interval, ok
}

// Forget drops what the recorder holds of a device, without storing it.
func (r *Recorder) Forget(deviceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.latest, deviceID)
	delete(r.intervals, deviceID)
	delete(r.minutes, deviceID)
}

// At returns the stored minute of a device that's nearest to a moment, if
// one is within the given distance. Its CPU is the minute's average.
func (r *Recorder) At(ctx context.Context, deviceID string, at time.Time, within time.Duration) (Sample, bool, error) {
	row, ok, err := r.st.UsageNear(ctx, deviceID, at, within)
	if err != nil || !ok {
		return Sample{}, false, err
	}
	s, err := decodeDetail(row)
	if err != nil {
		return Sample{}, false, fmt.Errorf("read the usage at %v: %w", row.At, err)
	}
	return s, true, nil
}

// save stores a device's minute. The minute is no longer in r.minutes, so
// nothing else touches it.
func (r *Recorder) save(ctx context.Context, deviceID string, m *minuteSum) {
	row, err := m.row(deviceID)
	if err == nil {
		err = r.st.AddUsage(ctx, row)
	}
	if err != nil && ctx.Err() == nil {
		r.log.Warn("store a minute of usage", "device", deviceID, "error", err)
	}
}

func (m *minuteSum) add(s Sample) {
	m.n++
	m.last = s
	if s.Total != nil {
		m.total = s.Total
		m.totalCPU += s.Total.CPU
		m.totals++
	}
	for _, g := range s.Groups {
		e := m.groups[g.Key]
		if e == nil {
			e = &sum[Group]{}
			m.groups[g.Key] = e
		}
		e.latest, e.seenAt = g, s.At
		e.cpu += g.CPU
	}
	for _, p := range s.Procs {
		id := procID{p.PID, p.Name}
		e := m.procs[id]
		if e == nil {
			e = &sum[Proc]{}
			m.procs[id] = e
		}
		e.latest, e.seenAt = p, s.At
		e.cpu += p.CPU
	}
}

// row sums up the minute: CPU is averaged over its samples, and memory and
// the process count are what the last sample said. Something the last
// sample didn't list used no memory, as far as it's known.
func (m *minuteSum) row(deviceID string) (store.UsageRow, error) {
	end := m.last.At
	groups := make([]Group, 0, len(m.groups))
	for _, e := range m.groups {
		g := e.latest
		g.CPU = round2(e.cpu / float64(m.n))
		if !e.seenAt.Equal(end) {
			g.Mem = 0
		}
		groups = append(groups, g)
	}
	procs := make([]Proc, 0, len(m.procs))
	for _, e := range m.procs {
		p := e.latest
		p.CPU = round2(e.cpu / float64(m.n))
		if !e.seenAt.Equal(end) {
			p.Mem = 0
		}
		p.Cmd = cut(p.Cmd, maxStoreCmd)
		procs = append(procs, p)
	}
	// Maps are in no order, so ties need one.
	slices.SortFunc(groups, func(a, b Group) int { return cmp.Compare(a.Key, b.Key) })
	slices.SortFunc(procs, func(a, b Proc) int { return cmp.Compare(a.PID, b.PID) })
	groups = top(groups, storeGroups, func(g Group) (float64, uint64) { return g.CPU, g.Mem })
	procs = top(procs, storeProcs, func(p Proc) (float64, uint64) { return p.CPU, p.Mem })

	totals := make([]groupTotal, len(groups))
	for i, g := range groups {
		totals[i] = groupTotal{Key: g.Key, CPU: g.CPU, Mem: g.Mem}
	}
	summary, err := json.Marshal(totals)
	if err != nil {
		return store.UsageRow{}, err
	}
	d := detail{Groups: make([]groupDetail, len(groups)), Procs: make([]procDetail, len(procs))}
	for i, g := range groups {
		d.Groups[i] = groupDetail{groupTotal: totals[i], Procs: g.Procs, Top: g.Top}
	}
	for i, p := range procs {
		d.Procs[i] = procDetail(p)
	}
	if m.total != nil {
		t := *m.total
		t.CPU = round2(m.totalCPU / float64(m.totals))
		d.Total = &totalsDetail{t}
	}
	compressed, err := deflate(d)
	if err != nil {
		return store.UsageRow{}, err
	}
	return store.UsageRow{
		DeviceID: deviceID,
		At:       end,
		Source:   string(m.last.Source),
		Cores:    m.last.Cores,
		Groups:   string(summary),
		Detail:   compressed,
	}, nil
}

// What a stored minute holds, in JSON arrays to keep it small. The groups
// column has each group's groupTotal, and the detail column a detail.
type (
	groupTotal struct {
		Key string
		CPU float64
		Mem uint64
	}
	groupDetail struct {
		groupTotal
		Procs int
		Top   []string
	}
	procDetail   Proc
	totalsDetail struct{ Totals }
	detail       struct {
		Groups []groupDetail `json:"g"`
		Procs  []procDetail  `json:"p"`
		Total  *totalsDetail `json:"t"`
	}
)

func (g groupTotal) MarshalJSON() ([]byte, error) { return json.Marshal([]any{g.Key, g.CPU, g.Mem}) }

func (g *groupTotal) UnmarshalJSON(b []byte) error { return decodeTuple(b, &g.Key, &g.CPU, &g.Mem) }

func (g groupDetail) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{g.Key, g.CPU, g.Mem, g.Procs, g.Top})
}

func (g *groupDetail) UnmarshalJSON(b []byte) error {
	return decodeTuple(b, &g.Key, &g.CPU, &g.Mem, &g.Procs, &g.Top)
}

func (p procDetail) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{p.PID, p.Name, p.User, p.CPU, p.Mem, p.Group, p.Cmd})
}

func (p *procDetail) UnmarshalJSON(b []byte) error {
	return decodeTuple(b, &p.PID, &p.Name, &p.User, &p.CPU, &p.Mem, &p.Group, &p.Cmd)
}

func (t totalsDetail) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{t.CPU, t.Mem, t.Procs})
}

func (t *totalsDetail) UnmarshalJSON(b []byte) error { return decodeTuple(b, &t.CPU, &t.Mem, &t.Procs) }

var errShortTuple = errors.New("an array in stored usage is too short")

// decodeTuple decodes the JSON array in b into dst, one element each.
func decodeTuple(b []byte, dst ...any) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if len(raw) < len(dst) {
		return errShortTuple
	}
	for i, d := range dst {
		if err := json.Unmarshal(raw[i], d); err != nil {
			return err
		}
	}
	return nil
}

// deflaters holds the writers that deflate has used. A writer at the best
// compression takes over a megabyte, and a device's minute is a few kilobytes,
// so a new one for each minute of 200 devices would be most of what Bield
// allocates.
var deflaters sync.Pool // of *flate.Writer

// deflate encodes d as JSON compressed with DEFLATE.
func deflate(d detail) ([]byte, error) {
	var buf bytes.Buffer
	zw, _ := deflaters.Get().(*flate.Writer)
	if zw == nil {
		var err error
		if zw, err = flate.NewWriter(&buf, flate.BestCompression); err != nil {
			return nil, err
		}
	} else {
		zw.Reset(&buf)
	}
	defer deflaters.Put(zw)
	if err := json.NewEncoder(zw).Encode(d); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// decodeDetail reads a stored minute, with its detail.
func decodeDetail(row store.UsageRow) (Sample, error) {
	b, err := io.ReadAll(io.LimitReader(flate.NewReader(bytes.NewReader(row.Detail)), 1<<20))
	if err != nil {
		return Sample{}, err
	}
	var d detail
	if err := json.Unmarshal(b, &d); err != nil {
		return Sample{}, err
	}
	s := Sample{
		At:     row.At,
		Source: Source(row.Source),
		Cores:  row.Cores,
		Groups: make([]Group, len(d.Groups)),
		Procs:  make([]Proc, len(d.Procs)),
	}
	for i, g := range d.Groups {
		kind, name := SplitKey(g.Key)
		s.Groups[i] = Group{Key: g.Key, Kind: kind, Name: name, CPU: g.CPU, Mem: g.Mem, Procs: g.Procs, Top: g.Top}
	}
	for i, p := range d.Procs {
		s.Procs[i] = Proc(p)
	}
	if d.Total != nil {
		s.Total = &d.Total.Totals
	}
	return s, nil
}

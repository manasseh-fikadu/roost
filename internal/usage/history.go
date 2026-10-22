package usage

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"time"
)

// How much a History lists.
const (
	// historyConsumers is how many of the biggest groups by CPU, and by
	// memory, a History lists.
	historyConsumers = 15
	// historySeries is how many of the biggest groups by CPU, and by
	// memory, a History charts.
	historySeries = 5
)

// A History sums up which groups used a device's CPU and memory over a span
// of time, from its stored minutes.
type History struct {
	// Minutes is how many stored minutes the span has.
	Minutes int
	// Consumers are the biggest groups by average CPU, and by average
	// memory, biggest CPU first.
	Consumers []Consumer
	// CPU and Mem chart the few biggest groups by each, averaged over each
	// step.
	CPU, Mem []Series
}

// A Consumer is a group's use over a span of time. A minute the group isn't
// in counts as zero, since it was too small to store.
type Consumer struct {
	Key  string
	Kind Kind
	Name string
	// CPUAvg and CPUPeak are the average and highest CPU of the minutes, in
	// percent.
	CPUAvg, CPUPeak float64
	// MemAvg and MemPeak are the average and most memory of the minutes,
	// in bytes.
	MemAvg, MemPeak uint64
}

// A Series is a group's CPU or memory over time.
type Series struct {
	Key  string
	Kind Kind
	Name string
	// Points has a point for each step with stored minutes, oldest first.
	Points []Point
}

// A Point is a value at the start of a step.
type Point struct {
	At time.Time
	V  float64
}

// History sums up a device's stored minutes since a time, in steps of the
// given size that start at multiples of it since the Unix epoch.
func (r *Recorder) History(ctx context.Context, deviceID string, since time.Time, step time.Duration) (History, error) {
	if step.Milliseconds() <= 0 {
		return History{}, fmt.Errorf("step %v is under a millisecond", step)
	}
	rows, err := r.st.UsageSince(ctx, deviceID, since)
	if err != nil {
		return History{}, err
	}
	type stats struct{ cpu, cpuPeak, mem, memPeak float64 }
	type bucket struct {
		start    time.Time
		n        int
		cpu, mem map[string]float64
	}
	all := map[string]*stats{}
	var buckets []*bucket
	minutes := 0
	for _, row := range rows {
		var groups []groupTotal
		if json.Unmarshal([]byte(row.Groups), &groups) != nil {
			continue
		}
		minutes++
		start := time.UnixMilli(row.At.UnixMilli() / step.Milliseconds() * step.Milliseconds())
		if len(buckets) == 0 || !buckets[len(buckets)-1].start.Equal(start) {
			buckets = append(buckets, &bucket{start: start, cpu: map[string]float64{}, mem: map[string]float64{}})
		}
		b := buckets[len(buckets)-1]
		b.n++
		for _, g := range groups {
			s := all[g.Key]
			if s == nil {
				s = &stats{}
				all[g.Key] = s
			}
			mem := float64(g.Mem)
			s.cpu += g.CPU
			s.mem += mem
			s.cpuPeak = max(s.cpuPeak, g.CPU)
			s.memPeak = max(s.memPeak, mem)
			b.cpu[g.Key] += g.CPU
			b.mem[g.Key] += mem
		}
	}

	consumers := make([]Consumer, 0, len(all))
	for key, s := range all {
		kind, name := SplitKey(key)
		n := float64(max(1, minutes))
		consumers = append(consumers, Consumer{
			Key: key, Kind: kind, Name: name,
			CPUAvg: round2(s.cpu / n), CPUPeak: s.cpuPeak,
			MemAvg: uint64(math.Round(s.mem / n)), MemPeak: uint64(s.memPeak),
		})
	}
	slices.SortFunc(consumers, func(a, b Consumer) int { return cmp.Compare(a.Key, b.Key) })
	byCPU := biggest(consumers, func(c Consumer) float64 { return c.CPUAvg })
	byMem := biggest(consumers, func(c Consumer) float64 { return float64(c.MemAvg) })

	series := func(cs []Consumer, value func(b *bucket, key string) float64) []Series {
		out := make([]Series, 0, historySeries)
		for _, c := range cs[:min(historySeries, len(cs))] {
			s := Series{Key: c.Key, Kind: c.Kind, Name: c.Name, Points: make([]Point, len(buckets))}
			for i, b := range buckets {
				s.Points[i] = Point{At: b.start, V: value(b, c.Key)}
			}
			out = append(out, s)
		}
		return out
	}
	h := History{
		Minutes: minutes,
		CPU: series(byCPU, func(b *bucket, key string) float64 {
			return round2(b.cpu[key] / float64(b.n))
		}),
		Mem: series(byMem, func(b *bucket, key string) float64 {
			return math.Round(b.mem[key] / float64(b.n))
		}),
	}
	// The biggest by each measure, once each, by CPU.
	listed := map[string]bool{}
	for _, c := range slices.Concat(byCPU[:min(historyConsumers, len(byCPU))], byMem[:min(historyConsumers, len(byMem))]) {
		if !listed[c.Key] {
			listed[c.Key] = true
			h.Consumers = append(h.Consumers, c)
		}
	}
	slices.SortStableFunc(h.Consumers, func(a, b Consumer) int {
		return cmp.Or(cmp.Compare(b.CPUAvg, a.CPUAvg), cmp.Compare(b.MemAvg, a.MemAvg))
	})
	return h, nil
}

// biggest returns the consumers with a value above zero, biggest first.
func biggest(cs []Consumer, value func(Consumer) float64) []Consumer {
	var out []Consumer
	for _, c := range cs {
		if value(c) > 0 {
			out = append(out, c)
		}
	}
	slices.SortStableFunc(out, func(a, b Consumer) int { return cmp.Compare(value(b), value(a)) })
	return out
}

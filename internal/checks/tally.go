package checks

import "time"

// tallyStep is the span each of a tally's counts covers.
const tallyStep = 5 * time.Minute

// A tally counts a check's results, or a device's pings, over the last
// UptimeSpan, in steps of tallyStep, so the uptime is worked out without
// reading the store. Its zero value has no counts.
type tally struct {
	// steps is a ring: a step's start says which span it counts now.
	steps [UptimeSpan / tallyStep]tallyCount
}

type tallyCount struct {
	// start is when the step starts, in Unix milliseconds, or 0.
	start     int64
	total, ok int
}

// add counts total results at a time, of which ok passed. A time more than
// UptimeSpan before the latest counted is left out.
func (t *tally) add(at time.Time, total, ok int) {
	step := tallyStep.Milliseconds()
	start := at.UnixMilli() / step * step
	c := &t.steps[start/step%int64(len(t.steps))]
	switch {
	case c.start > start:
		return
	case c.start < start:
		*c = tallyCount{start: start}
	}
	c.total += total
	c.ok += ok
}

// count counts one result.
func (t *tally) count(res Result) {
	ok := 0
	if res.OK {
		ok = 1
	}
	t.add(res.At, 1, ok)
}

// uptime returns the percentage of the results over the UptimeSpan before
// now that passed, by whole steps, or nil if there are none.
func (t *tally) uptime(now time.Time) *float64 {
	step := tallyStep.Milliseconds()
	since := now.Add(-UptimeSpan).UnixMilli() / step * step
	total, ok := 0, 0
	for _, c := range t.steps {
		if c.start != 0 && c.start >= since {
			total += c.total
			ok += c.ok
		}
	}
	if total == 0 {
		return nil
	}
	return new(float64(ok) / float64(total) * 100)
}

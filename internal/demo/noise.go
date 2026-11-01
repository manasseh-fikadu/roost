package demo

import (
	"math"
	"time"
)

// mix spreads the bits of x, so nearby inputs give unrelated outputs
// (splitmix64's finalizer).
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// unit returns a number from 0 up to 1 that depends only on seed and n.
func unit(seed uint64, n int64) float64 {
	return float64(mix(seed^mix(uint64(n)))>>11) / (1 << 53) //nolint:gosec // G115: n is only a number to mix.
}

// wave returns smooth noise from -1 to 1 that changes over about period, and
// depends only on seed and t. It's made of random values at every period,
// with a smooth curve between them, so a history and the live readings that
// follow it come from the same line.
func wave(seed uint64, t time.Time, period time.Duration) float64 {
	x := float64(t.UnixMilli()) / float64(period.Milliseconds())
	i := math.Floor(x)
	f := x - i
	f = f * f * (3 - 2*f)
	a, b := unit(seed, int64(i))*2-1, unit(seed, int64(i)+1)*2-1
	return a + (b-a)*f
}

// daily returns how busy a day is at t, from 0 in the small hours to 1 at
// peak hour (UTC), as a smooth curve.
func daily(t time.Time, peak float64) float64 {
	hour := float64(t.UTC().Hour()) + float64(t.UTC().Minute())/60
	return (1 + math.Cos((hour-peak)/24*2*math.Pi)) / 2
}

// clamp limits v to from lo to hi.
func clamp(v, lo, hi float64) float64 { return math.Min(hi, math.Max(lo, v)) }

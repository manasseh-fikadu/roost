package demo

import (
	"math"
	"time"
)

// A reading is what a machine was doing at one moment. All of its values
// come from the machine and the time, so the history Bield starts with and
// the readings that follow it are one line.
type reading struct {
	// cpu, mem and disk are percentages.
	cpu, mem, disk float64
	load           [3]float64
	// rx and tx are bytes per second.
	rx, tx float64
	// temp is in degrees, or 0 if the machine has no sensor.
	temp float64
	// battery is a percentage, and charging says whether it's filling.
	battery  float64
	charging bool
}

// seed returns a number that tells the machine's noise from the others'.
func (m *machine) seed(salt uint64) uint64 {
	return mix(uint64(m.index)*7919 + salt) //nolint:gosec // G115: a machine number is small.
}

// at works out what the machine is doing at t. start is when the demo
// started, which its incidents are measured from.
func (m *machine) at(t, start time.Time) reading {
	r := reading{}
	day := daily(t, m.peak)
	r.cpu = m.cpu + m.cpuSwing*day + m.cpuSwing*0.5*wave(m.seed(1), t, 7*time.Minute)
	if m.bursty {
		// Now and then it works hard for a few minutes.
		r.cpu += m.cpuSwing * 2.5 * math.Max(0, wave(m.seed(2), t, 40*time.Minute)-0.55)
	}
	r.mem = m.mem + m.memSwing*day + m.memSwing*0.6*wave(m.seed(3), t, 25*time.Minute)
	r.disk = m.disk + m.diskPerDay*t.Sub(start).Hours()/24 + 0.05*wave(m.seed(4), t, 3*time.Hour)

	for _, in := range m.incidents {
		from, to := start.Add(in.from), start.Add(in.to)
		if t.Before(from) || t.After(to) {
			continue
		}
		var strength float64
		if in.ramp {
			strength = t.Sub(from).Seconds() / to.Sub(from).Seconds()
		} else {
			// Even a spike has soft edges.
			edge := math.Min(t.Sub(from).Minutes(), to.Sub(t).Minutes())
			strength = clamp(edge/5, 0.3, 1)
		}
		r.cpu += in.cpu * strength * (0.85 + 0.15*wave(m.seed(5), t, 2*time.Minute))
		r.mem += in.mem * strength
		r.disk += in.disk * strength
	}
	r.cpu, r.mem, r.disk = clamp(r.cpu, 0.3, 100), clamp(r.mem, 1, 99), clamp(r.disk, 1, 99.5)

	// The load average follows the CPU, slowly.
	cores := float64(max(m.cores, 1))
	for i, span := range []time.Duration{0, 2 * time.Minute, 7 * time.Minute} {
		c := r.cpu
		if span > 0 {
			c = m.cpu + m.cpuSwing*daily(t.Add(-span), m.peak) + m.cpuSwing*0.5*wave(m.seed(1), t.Add(-span), 7*time.Minute)
		}
		r.load[i] = math.Max(0.01, clamp(c, 0, 100)/100*cores*(0.9+0.2*wave(m.seed(6), t, 9*time.Minute)))
	}

	busy := 0.25 + 1.05*day
	r.rx = m.rx * busy * math.Max(0.05, 1+0.6*wave(m.seed(7), t, 11*time.Minute))
	r.tx = m.tx * busy * math.Max(0.05, 1+0.6*wave(m.seed(8), t, 13*time.Minute))
	if m.temp > 0 {
		r.temp = m.temp + 0.25*r.cpu + 1.5*wave(m.seed(9), t, 17*time.Minute)
	}
	if m.battery {
		// It drains for eight hours and fills for three, over and over.
		const drain, fill = 8 * time.Hour, 3 * time.Hour
		phase := time.Duration(t.UnixNano()+int64(m.seed(10)%uint64(drain+fill))) % (drain + fill)
		if phase < drain {
			r.battery = 100 - 78*phase.Seconds()/drain.Seconds()
		} else {
			r.battery = 22 + 78*(phase-drain).Seconds()/fill.Seconds()
			r.charging = true
		}
	}
	return r
}

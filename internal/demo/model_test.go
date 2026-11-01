package demo

import (
	"math"
	"testing"
	"time"
)

func TestWave(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for i := range 2000 {
		at := t0.Add(time.Duration(i) * 37 * time.Second)
		v := wave(7, at, 5*time.Minute)
		if v < -1 || v > 1 || math.IsNaN(v) {
			t.Fatalf("wave(%v) = %v, want from -1 to 1", at, v)
		}
		if again := wave(7, at, 5*time.Minute); again != v {
			t.Fatalf("wave(%v) changed from %v to %v, want it to depend on the time alone", at, v, again)
		}
		// Smooth: a second later it hasn't gone far.
		if next := wave(7, at.Add(time.Second), 5*time.Minute); math.Abs(next-v) > 0.05 {
			t.Fatalf("wave jumped from %v to %v in a second", v, next)
		}
	}
}

func TestDaily(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if peak, night := daily(day.Add(15*time.Hour), 15), daily(day.Add(3*time.Hour), 15); peak < 0.99 || night > 0.1 {
		t.Errorf("daily() = %v at the peak and %v at night, want about 1 and 0", peak, night)
	}
}

func TestCast(t *testing.T) {
	t.Parallel()
	for _, n := range []int{2, 12, 13, 200, 1000} {
		ms := newCast(n, 1)
		if len(ms) != n {
			t.Fatalf("newCast(%d) made %d machines", n, len(ms))
		}
		ids, hosts, ips := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for i, m := range ms {
			if m.index != i || ids[m.id] || hosts[m.host] || ips[m.ip4.String()] {
				t.Fatalf("newCast(%d): machine %d (%s) repeats an index, ID, name or address", n, i, m.host)
			}
			ids[m.id], hosts[m.host], ips[m.ip4.String()] = true, true, true
			if m.cores < 1 || m.memGB <= 0 || m.diskGB <= 0 {
				t.Errorf("machine %s has %d cores, %v GB of memory and %v GB of disk", m.host, m.cores, m.memGB, m.diskGB)
			}
		}
		if ms[0].host != "roost" {
			t.Errorf("the first machine is %s, want Roost's own", ms[0].host)
		}
	}
}

func TestMachine_at(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	for _, m := range newCast(40, 1) {
		// Nothing measures a phone, so what it would do doesn't matter.
		if !m.agent && m.index != 0 {
			continue
		}
		for i := range 3 * 24 * 60 {
			at := start.Add(-time.Duration(i) * time.Minute)
			r := m.at(at, start)
			ok := r.cpu > 0 && r.cpu <= 100 && r.mem > 0 && r.mem < 100 && r.disk > 0 && r.disk < 100 &&
				r.load[0] > 0 && r.rx > 0 && r.tx > 0 && (r.battery == 0) == !m.battery && (r.temp > 0) == (m.temp > 0)
			if !ok {
				t.Fatalf("%s at %v: %+v, want values a machine could have", m.host, at, r)
			}
			if again := m.at(at, start); again != r {
				t.Fatalf("%s at %v changed between calls", m.host, at)
			}
		}
	}
}

// TestMachine_incidents checks that the strains of the base cast are there
// when they should be, and the disk of the NAS fills.
func TestMachine_incidents(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	ms := newCast(12, 1)
	byHost := map[string]*machine{}
	for _, m := range ms {
		byHost[m.host] = m
	}
	nyc := byHost["vps-nyc"]
	if during, after := nyc.at(start.Add(-26*time.Hour), start).cpu, nyc.at(start.Add(-20*time.Hour), start).cpu; during < 70 || after > 40 {
		t.Errorf("vps-nyc's CPU is %.0f%% during its incident and %.0f%% after, want a spike and then calm", during, after)
	}
	nas := byHost["nas"]
	if then, now := nas.at(start.Add(-7*24*time.Hour), start).disk, nas.at(start, start).disk; now-then < 1.5 || now < 89 {
		t.Errorf("the NAS's disk went from %.1f%% to %.1f%% over a week, want it to fill past 89%%", then, now)
	}
}

func TestProbe(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	blog := probes[7]
	if blog.name != "Blog" {
		t.Fatalf("probes[7] is %s, want Blog; fix this test", blog.name)
	}
	if !blog.downAt(start.Add(-30*time.Hour-50*time.Minute), start) || blog.downAt(start.Add(-time.Hour), start) {
		t.Error("the Blog should be down in its outage a day ago and up an hour ago")
	}
	garage := probes[2]
	if !garage.downAt(start.Add(time.Hour), start) {
		t.Error("the garage camera should be down for good: its outage has no end")
	}
}

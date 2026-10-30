package docker

import (
	"strings"
	"time"
)

// RawStats is one sample from the Engine API's stats for a container, with
// the fields Roost reads. Docker counts up, so every rate, CPU included, is
// the change between two samples.
type RawStats struct {
	Read     string `json:"read"`
	CPUStats struct {
		CPUUsage struct {
			TotalUsage *uint64  `json:"total_usage"`
			PerCPU     []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage *uint64 `json:"system_cpu_usage"`
		OnlineCPUs     int     `json:"online_cpus"`
	} `json:"cpu_stats"`
	// PreCPUStats is the sample before this one, which a sample from a
	// stream has, and a one-shot sample doesn't.
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage *uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage *uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage *uint64           `json:"usage"`
		Limit *uint64           `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	// Networks are the container's network interfaces, by name. A container
	// with no network, or on the host's, has none.
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytes []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
	PidsStats struct {
		Current *uint64 `json:"current"`
	} `json:"pids_stats"`
}

// A count is a number Docker counts up, or nothing if it didn't say.
type count struct {
	n  uint64
	ok bool
}

func countOf(p *uint64) count {
	if p == nil {
		return count{}
	}
	return count{n: *p, ok: true}
}

// Counters are the numbers a sample counts up, which the next sample
// needs to work out rates.
type Counters struct {
	at         time.Time
	cpu, sys   count
	cpus       int
	rx, tx     count
	blkR, blkW count
}

// Stats is what a container used, at a moment. A value is nil if the
// container's sample can't say it, such as a rate from the first sample.
type Stats struct {
	At time.Time
	// CPU is the share of one CPU the container used, in percent, as
	// `docker stats` says it.
	CPU *float64
	// MemUsed is the memory in use, without the page cache the kernel can
	// drop, in bytes. MemLimit is the container's limit, which is the
	// machine's memory if it has none.
	MemUsed, MemLimit *float64
	// NetRx and NetTx are the bytes per second received and sent, over all
	// its interfaces, and the totals since it started.
	NetRx, NetTx           *float64
	NetRxTotal, NetTxTotal *float64
	// BlkRead and BlkWrite are the bytes read from and written to disks since
	// it started, and BlkReadRate and BlkWriteRate the bytes per second.
	BlkRead, BlkWrite         *float64
	BlkReadRate, BlkWriteRate *float64
	// Pids is how many processes are running in it.
	Pids *float64
	// CPUs is how many CPUs it can see: 100% of CPU is one of them.
	CPUs int
}

// maxGap is the longest time between two samples that still make a rate.
const maxGap = 10 * time.Minute

// sampleTime returns when a sample was taken, or false for the zero time
// that Docker gives a container that isn't running.
func sampleTime(raw *RawStats) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, raw.Read)
	if err != nil || t.Year() < 2000 {
		return time.Time{}, false
	}
	return t, true
}

func countersOf(raw *RawStats, at time.Time) Counters {
	c := Counters{
		at:   at,
		cpu:  countOf(raw.CPUStats.CPUUsage.TotalUsage),
		sys:  countOf(raw.CPUStats.SystemCPUUsage),
		cpus: raw.CPUStats.OnlineCPUs,
	}
	if c.cpus == 0 {
		c.cpus = max(len(raw.CPUStats.CPUUsage.PerCPU), 1)
	}
	if raw.Networks != nil {
		var rx, tx uint64
		for _, n := range raw.Networks {
			rx += n.RxBytes
			tx += n.TxBytes
		}
		c.rx, c.tx = count{rx, true}, count{tx, true}
	}
	if io := raw.BlkioStats.IOServiceBytes; io != nil {
		// cgroup v1 says "Read" and "Write", and v2 "read" and "write".
		for _, e := range io {
			switch {
			case strings.EqualFold(e.Op, "read"):
				c.blkR.n += e.Value
			case strings.EqualFold(e.Op, "write"):
				c.blkW.n += e.Value
			}
		}
		c.blkR.ok, c.blkW.ok = true, true
	}
	return c
}

// Derive works out a container's Stats from a sample and the counters of the
// one before it, if there is one. It returns the counters to derive the next
// sample from. It reports false for a sample of a container that isn't
// running, which has no time or numbers.
func Derive(raw *RawStats, prev *Counters) (Stats, Counters, bool) {
	at, ok := sampleTime(raw)
	if !ok {
		return Stats{}, Counters{}, false
	}
	cur := countersOf(raw, at)
	usable := prev != nil && at.After(prev.at) && at.Sub(prev.at) < maxGap
	var secs float64
	if usable {
		secs = at.Sub(prev.at).Seconds()
	}
	s := Stats{At: at, CPUs: cur.cpus}
	if usable {
		s.CPU = cpuShare(cur.cpu, cur.sys, prev.cpu, prev.sys, cur.cpus)
	} else {
		// Without an earlier sample of ours, a sample from a stream carries
		// Docker's own.
		pre := raw.PreCPUStats
		if pre.SystemCPUUsage != nil {
			s.CPU = cpuShare(cur.cpu, cur.sys, countOf(pre.CPUUsage.TotalUsage), countOf(pre.SystemCPUUsage), cur.cpus)
		}
	}
	if mem := raw.MemoryStats; mem.Usage != nil {
		// Page cache the kernel can drop isn't really used, and `docker stats`
		// leaves it out too.
		inactive, ok := mem.Stats["inactive_file"]
		if !ok {
			inactive = mem.Stats["total_inactive_file"]
		}
		s.MemUsed = f64(*mem.Usage - min(inactive, *mem.Usage))
		if mem.Limit != nil {
			s.MemLimit = f64(*mem.Limit)
		}
	}
	s.NetRxTotal, s.NetTxTotal = cur.rx.float(), cur.tx.float()
	s.BlkRead, s.BlkWrite = cur.blkR.float(), cur.blkW.float()
	if usable {
		s.NetRx, s.NetTx = perSecond(cur.rx, prev.rx, secs), perSecond(cur.tx, prev.tx, secs)
		s.BlkReadRate, s.BlkWriteRate = perSecond(cur.blkR, prev.blkR, secs), perSecond(cur.blkW, prev.blkW, secs)
	}
	if raw.PidsStats.Current != nil {
		s.Pids = f64(*raw.PidsStats.Current)
	}
	return s, cur, true
}

// cpuShare returns the share of one CPU used between two samples, in
// percent, or nil if there isn't enough to say.
func cpuShare(cpu, sys, prevCPU, prevSys count, cpus int) *float64 {
	if !cpu.ok || !sys.ok || !prevCPU.ok || !prevSys.ok || cpu.n < prevCPU.n || sys.n <= prevSys.n {
		return nil
	}
	return f64(float64(cpu.n-prevCPU.n) / float64(sys.n-prevSys.n) * float64(cpus) * 100)
}

func perSecond(cur, prev count, secs float64) *float64 {
	if !cur.ok || !prev.ok || secs <= 0 || cur.n < prev.n {
		return nil
	}
	return f64(float64(cur.n-prev.n) / secs)
}

func (c count) float() *float64 {
	if !c.ok {
		return nil
	}
	return f64(float64(c.n))
}

func f64[T uint64 | float64](v T) *float64 {
	f := float64(v)
	return &f
}

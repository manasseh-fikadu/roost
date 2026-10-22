package hostmetrics_test

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"sync"
	"testing"
	"testing/fstest"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/hostmetrics"
)

const gib = 1 << 30

// machine is a fake Linux machine's files, as they are at one moment.
func machine() fstest.MapFS {
	return files(map[string]string{
		"proc/stat": "cpu  1000 0 500 8000 500 0 0 0 0 0\ncpu0 500 0 250 4000 250 0 0 0 0 0\ncpu1 500 0 250 4000 250 0 0 0 0 0\nintr 1\n",
		"proc/meminfo": "MemTotal:        8388608 kB\nMemFree:          100000 kB\nMemAvailable:    6291456 kB\n" +
			"SwapTotal:       2097152 kB\nSwapFree:        1048576 kB\n",
		"proc/loadavg":   "0.50 0.25 0.10 2/377 12345\n",
		"proc/uptime":    "3600.52 7000.00\n",
		"proc/net/route": "Iface\tDestination\tGateway\nlo\t0000007F\t00000000\neth0\t00000000\t012844BC\n",
		"proc/net/dev": "Inter-|   Receive\n face |bytes    packets\n" +
			"    lo: 999 1 0 0 0 0 0 0 999 1 0 0 0 0 0 0\n" +
			"  eth0: 10000 10 0 0 0 0 0 0 20000 20 0 0 0 0 0 0\n",
		"sys/class/thermal/thermal_zone0/temp":    "45000\n",
		"sys/class/thermal/thermal_zone1/temp":    "52500\n",
		"sys/class/thermal/cooling_device0/type":  "Processor\n",
		"proc/sys/kernel/hostname":                "hub\n",
		"proc/sys/kernel/osrelease":               "6.12.0-1-amd64\n",
		"etc/os-release":                          "NAME=\"Debian GNU/Linux\"\nPRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\n",
		"sys/devices/virtual/dmi/id/sys_vendor":   "QEMU\n",
		"sys/devices/virtual/dmi/id/product_name": "Standard PC\n",
	})
}

func files(m map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, data := range m {
		fsys[name] = &fstest.MapFile{Data: []byte(data)}
	}
	return fsys
}

// with returns a copy of fsys with the files in m changed, or removed if
// their content is "".
func with(fsys fstest.MapFS, m map[string]string) fstest.MapFS {
	out := maps.Clone(fsys)
	for name, data := range m {
		if data == "" {
			delete(out, name)
			continue
		}
		out[name] = &fstest.MapFile{Data: []byte(data)}
	}
	return out
}

// fakeSystem is a System whose files a test can change between samples.
type fakeSystem struct {
	mu   sync.Mutex
	fsys fstest.MapFS
	disk func() (uint64, uint64, error)
}

// change changes the files in m, or removes them if their content is "".
func (f *fakeSystem) change(m map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fsys = with(f.fsys, m)
}

func (f *fakeSystem) system() hostmetrics.System {
	return hostmetrics.System{
		FS: fsFunc(func() fs.FS {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.fsys
		}),
		Statfs: func(path string) (uint64, uint64, error) {
			if path != "/" {
				return 0, 0, errors.New("unexpected path " + path)
			}
			return f.disk()
		},
	}
}

// fsFunc is an fs.FS that opens files in whatever FS the function returns
// at the time.
type fsFunc func() fs.FS

func (f fsFunc) Open(name string) (fs.File, error) { return f().Open(name) }

func newFake() *fakeSystem {
	return &fakeSystem{fsys: machine(), disk: func() (uint64, uint64, error) { return 100 * gib, 40 * gib, nil }}
}

// collect runs a Collector of sys for long enough to take n samples, and
// returns its reading after each.
func collect(t *testing.T, sys *fakeSystem, n int, between func(i int)) []hostmetrics.Reading {
	t.Helper()
	var readings []hostmetrics.Reading
	synctest.Test(t, func(t *testing.T) {
		c := hostmetrics.NewCollector(sys.system(), nil)
		go c.Run(t.Context())
		for i := range n {
			<-c.Changes()
			readings = append(readings, c.Reading())
			if between != nil {
				between(i)
			}
			time.Sleep(hostmetrics.Interval)
		}
	})
	return readings
}

func TestCollector(t *testing.T) {
	t.Parallel()
	sys := newFake()
	readings := collect(t, sys, 2, func(int) {
		// Over the next 5 seconds, the CPUs are busy 300 of 1000 ticks, and
		// eth0 receives 5000 bytes and sends 10000.
		sys.change(map[string]string{
			"proc/stat":    "cpu  1200 0 600 8600 600 0 0 0 0 0\n",
			"proc/net/dev": "  eth0: 15000 10 0 0 0 0 0 0 30000 20 0 0 0 0 0 0\n",
		})
	})

	first, second := readings[0], readings[1]
	if first.Err != nil || first.Sample == nil {
		t.Fatalf("first reading = %+v, want a sample", first)
	}
	if s := first.Sample; s.CPU != nil || s.NetRx != nil || s.NetTx != nil {
		t.Errorf("first sample has CPU %v and network %v/%v, want none, since there's nothing to compare with", s.CPU, s.NetRx, s.NetTx)
	}
	wantInfo := hostmetrics.Info{
		Hostname: "hub", OSVersion: "Debian GNU/Linux 13 (trixie)", Kernel: "6.12.0-1-amd64",
		Model: "QEMU Standard PC", Arch: second.Info.Arch, Cores: 2,
	}
	if second.Info != wantInfo || second.Info.Arch == "" {
		t.Errorf("info = %+v, want %+v", second.Info, wantInfo)
	}

	s := second.Sample
	if second.Err != nil || s == nil {
		t.Fatalf("second reading = %+v, want a sample", second)
	}
	checks := []struct {
		name      string
		got, want any
	}{
		{"CPU", deref(s.CPU), 30.0},
		{"load", deref(s.Load), [3]float64{0.5, 0.25, 0.1}},
		{"procs", s.Procs, 377},
		{"memory", s.Mem, hostmetrics.Usage{Used: 2 * gib, Total: 8 * gib}},
		{"swap", deref(s.Swap), hostmetrics.Usage{Used: gib, Total: 2 * gib}},
		{"disk", deref(s.Disk), hostmetrics.Usage{Used: 60 * gib, Total: 100 * gib}},
		{"received", deref(s.NetRx), 1000.0},
		{"sent", deref(s.NetTx), 2000.0},
		{"temperature", deref(s.Temp), 52.5},
		{"uptime", s.Uptime, 3600 * time.Second},
		{"time between samples", s.At.Sub(first.Sample.At), hostmetrics.Interval},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if s.CPUSustained != nil {
		t.Errorf("sustained CPU = %v after 5 seconds, want none", *s.CPUSustained)
	}
}

// What the machine doesn't say is left out, and only missing CPU or memory
// fails a sample.
func TestCollector_missing(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		remove  []string
		wantErr bool
	}{
		{name: "no CPU", remove: []string{"proc/stat"}, wantErr: true},
		{name: "no memory", remove: []string{"proc/meminfo"}, wantErr: true},
		{
			name: "no extras",
			remove: []string{
				"proc/loadavg", "proc/uptime", "proc/net/route", "sys/class/thermal/thermal_zone0/temp",
				"sys/class/thermal/thermal_zone1/temp", "etc/os-release", "sys/devices/virtual/dmi/id/sys_vendor",
				"sys/devices/virtual/dmi/id/product_name",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sys := newFake()
			gone := map[string]string{}
			for _, name := range tt.remove {
				gone[name] = ""
			}
			sys.change(gone)
			sys.disk = func() (uint64, uint64, error) { return 0, 0, errors.ErrUnsupported }
			r := collect(t, sys, 1, nil)[0]
			if tt.wantErr {
				if r.Err == nil || r.Sample != nil {
					t.Errorf("reading = %+v, want an error and no sample", r)
				}
				return
			}
			if r.Err != nil || r.Sample == nil {
				t.Fatalf("reading = %+v, want a sample", r)
			}
			s := r.Sample
			if s.Load != nil || s.Procs != 0 || s.Uptime != 0 || s.Disk != nil || s.Temp != nil {
				t.Errorf("sample = %+v, want no load, processes, uptime, disk or temperature", s)
			}
			if r.Info.OSVersion != "Linux 6.12.0-1-amd64" || r.Info.Model != "" {
				t.Errorf("info = %+v, want the kernel for the OS and no model", r.Info)
			}
		})
	}
}

// A failed sample keeps the last one that worked, until one works again.
func TestCollector_recovers(t *testing.T) {
	t.Parallel()
	sys := newFake()
	meminfo := string(sys.fsys["proc/meminfo"].Data)
	readings := collect(t, sys, 3, func(i int) {
		if i == 0 {
			sys.change(map[string]string{"proc/meminfo": "nothing useful\n"})
		} else {
			sys.change(map[string]string{"proc/meminfo": meminfo})
		}
	})
	if r := readings[1]; r.Err == nil || r.Sample != readings[0].Sample {
		t.Errorf("reading after a failure = %+v, want the error and the first sample", r)
	}
	if r := readings[2]; r.Err != nil || r.Sample == nil || r.Sample == readings[0].Sample {
		t.Errorf("reading after recovering = %+v, want a new sample", r)
	}
}

func TestCollector_sustainedCPU(t *testing.T) {
	t.Parallel()
	sys := newFake()
	var busy, total uint64 = 1500, 10000
	n := int(hostmetrics.SustainedWindow/hostmetrics.Interval) + 2
	readings := collect(t, sys, n, func(i int) {
		// Busy 50% for the first half of the window, then 90%.
		pct := uint64(50)
		if i >= n/2 {
			pct = 90
		}
		busy += pct * 10
		total += 1000
		idle := total - busy
		sys.change(map[string]string{
			"proc/stat": fmt.Sprintf("cpu  %d 0 0 %d 0 0 0 0 0 0\n", busy, idle),
		})
	})

	// The window isn't mostly covered until 4 minutes in.
	for i, r := range readings {
		covered := time.Duration(i-1) * hostmetrics.Interval
		if got := r.Sample.CPUSustained; (got != nil) != (covered >= hostmetrics.SustainedWindow*8/10) {
			t.Fatalf("sample %d, %v in: sustained CPU = %v", i, covered, got)
		}
	}
	last := readings[len(readings)-1].Sample
	if got := *last.CPUSustained; got <= 50 || got >= 90 {
		t.Errorf("sustained CPU = %v, want between the two halves' 50 and 90", got)
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

package docker_test

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/docker"
)

// sample returns a sample of the Engine API's stats as Docker 29 writes
// them on a cgroup v2 machine, with the fields the test changes.
func sample(t *testing.T, edit func(m map[string]any)) *docker.RawStats {
	t.Helper()
	m := map[string]any{
		"read": "2027-03-01T10:00:00.5Z",
		"cpu_stats": map[string]any{
			"cpu_usage":        map[string]any{"total_usage": 2_000_000_000},
			"system_cpu_usage": 40_000_000_000,
			"online_cpus":      4,
		},
		"precpu_stats": map[string]any{"cpu_usage": map[string]any{"total_usage": 0}},
		"memory_stats": map[string]any{
			"usage": 100_000_000, "limit": 8_000_000_000,
			"stats": map[string]any{"inactive_file": 30_000_000, "anon": 60_000_000},
		},
		"networks": map[string]any{
			"eth0": map[string]any{"rx_bytes": 1000, "tx_bytes": 500},
			"eth1": map[string]any{"rx_bytes": 24, "tx_bytes": 12},
		},
		"blkio_stats": map[string]any{"io_service_bytes_recursive": []any{
			map[string]any{"op": "read", "value": 4096},
			map[string]any{"op": "write", "value": 1024},
			map[string]any{"op": "read", "value": 4096},
		}},
		"pids_stats": map[string]any{"current": 7},
	}
	if edit != nil {
		edit(m)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var raw docker.RawStats
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	return &raw
}

// show writes a possibly missing number for a test's message.
func show(p *float64) string {
	if p == nil {
		return "none"
	}
	return strconv.FormatFloat(*p, 'g', -1, 64)
}

func near(got *float64, want float64) bool {
	return got != nil && math.Abs(*got-want) < 1e-6
}

func TestDerive_firstSample(t *testing.T) {
	t.Parallel()
	got, _, ok := docker.Derive(sample(t, nil), nil)
	if !ok {
		t.Fatal("Derive() ok = false, want a sample")
	}
	// Page cache is left out of memory.
	if !near(got.MemUsed, 70_000_000) || !near(got.MemLimit, 8_000_000_000) {
		t.Errorf("memory = %s of %s, want 70 MB of 8 GB", show(got.MemUsed), show(got.MemLimit))
	}
	if !near(got.NetRxTotal, 1024) || !near(got.NetTxTotal, 512) || !near(got.BlkRead, 8192) || !near(got.BlkWrite, 1024) {
		t.Errorf("totals = rx %s tx %s read %s write %s, want 1024, 512, 8192 and 1024", show(got.NetRxTotal), show(got.NetTxTotal), show(got.BlkRead), show(got.BlkWrite))
	}
	if got.CPU != nil || got.NetRx != nil || got.BlkReadRate != nil {
		t.Errorf("a first one-shot sample has rates: cpu %s, net %s, disk %s", show(got.CPU), show(got.NetRx), show(got.BlkReadRate))
	}
	if !near(got.Pids, 7) || got.CPUs != 4 || !got.At.Equal(time.Date(2027, 3, 1, 10, 0, 0, 5e8, time.UTC)) {
		t.Errorf("pids = %s, cpus = %d, at = %v", show(got.Pids), got.CPUs, got.At)
	}
}

func TestDerive_streamedFirstSampleUsesDockersPrevious(t *testing.T) {
	t.Parallel()
	raw := sample(t, func(m map[string]any) {
		m["precpu_stats"] = map[string]any{
			"cpu_usage": map[string]any{"total_usage": 1_000_000_000}, "system_cpu_usage": 36_000_000_000,
		}
	})
	got, _, _ := docker.Derive(raw, nil)
	// 1 s of CPU in 4 s of the machine's 4 CPUs: a quarter of the four, one CPU.
	if !near(got.CPU, 100) {
		t.Errorf("CPU = %s, want 100", show(got.CPU))
	}
}

func TestDerive_rates(t *testing.T) {
	t.Parallel()
	_, first, _ := docker.Derive(sample(t, nil), nil)
	later := sample(t, func(m map[string]any) {
		m["read"] = "2027-03-01T10:00:02.5Z"
		m["cpu_stats"] = map[string]any{
			"cpu_usage": map[string]any{"total_usage": 2_500_000_000}, "system_cpu_usage": 48_000_000_000, "online_cpus": 4,
		}
		m["networks"] = map[string]any{"eth0": map[string]any{"rx_bytes": 3024, "tx_bytes": 1512}}
		m["blkio_stats"] = map[string]any{"io_service_bytes_recursive": []any{
			map[string]any{"op": "Read", "value": 20480}, map[string]any{"op": "Write", "value": 5120},
		}}
	})
	got, _, ok := docker.Derive(later, &first)
	if !ok {
		t.Fatal("Derive() ok = false")
	}
	// 0.5 s of CPU while the machine's four CPUs gave 8 s: 6.25% of the
	// four, which is 25% of one.
	if !near(got.CPU, 25) {
		t.Errorf("CPU = %s, want 25", show(got.CPU))
	}
	if !near(got.NetRx, 1000) || !near(got.NetTx, 500) {
		t.Errorf("net = %s and %s, want 1000 and 500 a second", show(got.NetRx), show(got.NetTx))
	}
	if !near(got.BlkReadRate, (20480-8192)/2.0) || !near(got.BlkWriteRate, (5120-1024)/2.0) {
		t.Errorf("disk = %s and %s", show(got.BlkReadRate), show(got.BlkWriteRate))
	}
}

func TestDerive_noRateFrom(t *testing.T) {
	t.Parallel()
	_, first, _ := docker.Derive(sample(t, nil), nil)
	tests := []struct {
		name string
		edit func(m map[string]any)
	}{
		{"a counter that went back, as when the container restarted", func(m map[string]any) {
			m["read"] = "2027-03-01T10:00:02.5Z"
			m["networks"] = map[string]any{"eth0": map[string]any{"rx_bytes": 10, "tx_bytes": 10}}
		}},
		{"a sample too long after", func(m map[string]any) {
			m["read"] = "2027-03-01T10:20:00Z"
			m["networks"] = map[string]any{"eth0": map[string]any{"rx_bytes": 9000, "tx_bytes": 9000}}
		}},
		{"a sample from the same moment", func(m map[string]any) {
			m["networks"] = map[string]any{"eth0": map[string]any{"rx_bytes": 9000, "tx_bytes": 9000}}
		}},
		{"a sample from before", func(m map[string]any) {
			m["read"] = "2027-03-01T09:59:00Z"
			m["networks"] = map[string]any{"eth0": map[string]any{"rx_bytes": 9000, "tx_bytes": 9000}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _, ok := docker.Derive(sample(t, tt.edit), &first)
			if !ok {
				t.Fatal("Derive() ok = false")
			}
			if got.NetRx != nil || got.CPU != nil {
				t.Errorf("rates = net %s, cpu %s, want none", show(got.NetRx), show(got.CPU))
			}
		})
	}
}

func TestDerive_stoppedContainer(t *testing.T) {
	t.Parallel()
	// Docker keeps sending samples with the zero time for a container that
	// stopped.
	raw := sample(t, func(m map[string]any) { m["read"] = "0001-01-01T00:00:00Z" })
	if _, _, ok := docker.Derive(raw, nil); ok {
		t.Error("Derive() ok = true for a sample at the zero time, want false")
	}
}

func TestDerive_missingParts(t *testing.T) {
	t.Parallel()
	raw := sample(t, func(m map[string]any) {
		delete(m, "networks")
		delete(m, "blkio_stats")
		delete(m, "pids_stats")
		m["memory_stats"] = map[string]any{"usage": 50, "stats": map[string]any{"total_inactive_file": 80}}
		m["cpu_stats"] = map[string]any{
			"cpu_usage": map[string]any{"total_usage": 1, "percpu_usage": []any{1, 2}}, "system_cpu_usage": 2,
		}
	})
	got, _, ok := docker.Derive(raw, nil)
	if !ok {
		t.Fatal("Derive() ok = false")
	}
	// A container on the host's network has none of its own, and v1 names the
	// cache total_inactive_file. It can't have more cache than memory.
	if got.NetRxTotal != nil || got.BlkRead != nil || got.Pids != nil || got.MemLimit != nil {
		t.Errorf("got net %s, disk %s, pids %s, limit %s, want none of them", show(got.NetRxTotal), show(got.BlkRead), show(got.Pids), show(got.MemLimit))
	}
	if !near(got.MemUsed, 0) || got.CPUs != 2 {
		t.Errorf("memory = %s, cpus = %d, want 0 and 2", show(got.MemUsed), got.CPUs)
	}
}

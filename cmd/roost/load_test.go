//go:build load

package main

// TestLoad runs Roost against a big demo and says what it costs: its CPU and
// memory while nothing looks at it, while browsers have it open, and while
// they ask for a lot. It's not a unit test: it takes minutes, so it only builds
// with the load tag. See docs/benchmark.md for how to run it, and read its
// profile with go tool pprof.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/manasseh-fikadu/roost/internal/config"
)

func loadEnv(name string, fallback float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(name), 64); err == nil {
		return v
	}
	return fallback
}

// cpuTime returns the CPU time this process has used.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// rss returns the process's resident memory in megabytes, from /proc, or 0.
func rss() float64 {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var kb float64
		if n, _ := fmt.Sscanf(sc.Text(), "VmRSS: %f kB", &kb); n == 1 {
			return kb / 1024
		}
	}
	return 0
}

func TestLoad(t *testing.T) {
	devices := int(loadEnv("ROOST_LOAD_DEVICES", 200))
	days := loadEnv("ROOST_LOAD_DAYS", 7)
	window := time.Duration(loadEnv("ROOST_LOAD_SECONDS", 30)) * time.Second
	cfg := config.Config{
		Demo: true, DemoListen: "127.0.0.1:0", DemoDevices: devices, DemoDays: days,
		DataDir: filepath.Join(t.TempDir(), "data"),
	}
	began := time.Now()
	log := startServe(t, cfg, nil)
	// Seeding takes longer than waitFor waits.
	var url string
	for url == "" {
		if m := regexp.MustCompile(`roost is running.* url=(\S+)`).FindStringSubmatch(log.String()); m != nil {
			url = m[1]
		}
		if time.Since(began) > 15*time.Minute {
			t.Fatalf("Roost didn't start in 15 minutes:\n%s", log)
		}
		time.Sleep(time.Second)
	}
	t.Logf("%d devices, %.0f days of history: up in %v", devices, days, time.Since(began).Round(time.Second))
	t.Logf("database: %.0f MB", dirSize(cfg.DataDir)/1e6)

	// What does it cost with nobody looking? Once the agents have settled.
	time.Sleep(10 * time.Second)
	// requests, if it's above zero, is how many requests run makes, to say what
	// each costs.
	measure := func(name string, requests int, run func()) {
		runtime.GC()
		before, start := cpuTime(), time.Now()
		run()
		used, took := cpuTime()-before, time.Since(start)
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		cost := ""
		if requests > 0 {
			cost = fmt.Sprintf(", %.1f ms of CPU a request", float64(used.Microseconds())/float64(requests)/1000)
		}
		t.Logf("%-28s %5.1f%% of a core, heap %3.0f MB, resident %3.0f MB%s", name,
			float64(used)/float64(took)*100, float64(ms.HeapAlloc)/1e6, rss(), cost)
	}
	measure("nobody looking", 0, func() { time.Sleep(window) })

	// Browsers that have the app open each hold the stream.
	client := &http.Client{Timeout: 0}
	var wg sync.WaitGroup
	ctx, stop := context.WithCancel(t.Context())
	var snapshotBytes int
	var mu sync.Mutex
	for range 10 {
		wg.Go(func() {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/stream", nil)
			if err != nil {
				return
			}
			res, err := client.Do(req)
			if err != nil {
				return
			}
			defer func() { _ = res.Body.Close() }()
			n, _ := io.Copy(io.Discard, res.Body)
			mu.Lock()
			snapshotBytes += int(n)
			mu.Unlock()
		})
	}
	measure("10 browsers on the stream", 0, func() { time.Sleep(window) })
	stop()
	wg.Wait()
	t.Logf("%.1f MB sent to each browser in that time", float64(snapshotBytes)/10/1e6)

	// One snapshot, to see how big the page's data is.
	snap := firstSnapshot(t, &http.Client{Timeout: 30 * time.Second}, url)
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("a snapshot is %.0f KB of JSON, %d devices, %d checks, %d alerts", float64(len(raw))/1e3, len(snap.Devices), len(snap.Checks), len(snap.Alerts))

	// Someone clicking through the 200 devices' pages with a month and a quarter of history.
	client2 := &http.Client{Timeout: 60 * time.Second}
	measure("clicking through devices", 400, func() {
		for i, d := range snap.Devices {
			if i >= 100 {
				break
			}
			for _, rg := range []string{"24h", "30d", "90d"} {
				get(t, client2, fmt.Sprintf("%s/api/devices/%s/history?range=%s", url, d.ID, rg))
			}
			get(t, client2, fmt.Sprintf("%s/api/devices/%s/usage/history?range=24h", url, d.ID))
		}
	})
	// How long each page's request takes, on average.
	for _, path := range []string{
		"/api/alerts", "/api/events", "/api/checks/recent", "/api/health/history?range=24h", "/api/health/history?range=7d",
		"/api/devices/" + snap.Devices[1].ID + "/history?range=24h",
		"/api/devices/" + snap.Devices[1].ID + "/history?range=90d",
		"/api/devices/" + snap.Devices[1].ID + "/usage/history?range=24h",
		"/api/devices/" + snap.Devices[1].ID + "/usage",
		"/api/checks/" + snap.Checks[0].ID + "/history?range=24h",
	} {
		start := time.Now()
		for range 20 {
			get(t, client2, url+path)
		}
		t.Logf("GET %-62s %6.1f ms", path, float64(time.Since(start).Microseconds())/20/1000)
	}
}

// dirSize returns how many bytes the files in a directory take.
func dirSize(dir string) float64 {
	var n int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n += info.Size()
		}
		return nil
	})
	return float64(n)
}

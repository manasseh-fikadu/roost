package docker_test

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/docker"
	"github.com/manasseh-fikadu/bield/internal/store"
)

func TestSampler(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		var (
			mu   sync.Mutex
			rows []store.ContainerMetricsRow
			down atomic.Bool
		)
		e := newEngine()
		e.answer = func(w http.ResponseWriter, _ *http.Request) bool {
			if down.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return true
			}
			return false
		}
		h := docker.NewHost(client(t, e))
		defer h.Close()
		s := docker.NewSampler(docker.SamplerOptions{
			Targets: func() []docker.Target { return []docker.Target{{ID: "hub", Host: h}} },
			Record: func(_ context.Context, r []store.ContainerMetricsRow) error {
				mu.Lock()
				defer mu.Unlock()
				rows = append(rows, r...)
				return nil
			},
		})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go s.Run(ctx)

		got := func() []store.ContainerMetricsRow {
			synctest.Wait()
			mu.Lock()
			defer mu.Unlock()
			return append([]store.ContainerMetricsRow(nil), rows...)
		}
		time.Sleep(20 * time.Second)
		first := got()
		// The stopped container has no row, and a first sample has no rates.
		if len(first) != 1 || first[0].Name != "web" || first[0].DeviceID != "hub" || !first[0].At.Equal(time.Now()) {
			t.Fatalf("after the first sample, rows = %+v, want one for web on the hub, now", first)
		}
		if first[0].CPU != nil || first[0].NetRx != nil || !near(first[0].Mem, 100) {
			t.Errorf("first row = %+v, want its memory, and no rates", first[0])
		}

		// A minute on, the rates are the change over the minute.
		time.Sleep(docker.SampleInterval)
		second := got()
		if len(second) != 2 || !near(second[1].CPU, 25) || second[1].NetRx == nil || *second[1].NetRx != 0 {
			t.Fatalf("after the second sample, rows = %+v, want rates in the second row", second)
		}

		// A minute when Docker doesn't answer is a gap, and the next
		// minute has no earlier counters to work rates from.
		down.Store(true)
		time.Sleep(docker.SampleInterval)
		if n := len(got()); n != 2 {
			t.Errorf("rows = %d after a failed minute, want a gap", n)
		}
		down.Store(false)
		time.Sleep(docker.SampleInterval)
		last := got()
		if len(last) != 3 || last[2].CPU != nil {
			t.Errorf("after the daemon came back, rows = %+v, want one without rates", last)
		}
	})
}

package docker_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/docker"
)

// engine is a fake Docker Engine that serves a few containers and counts
// what it is asked.
type engine struct {
	mu   sync.Mutex
	hits map[string]int
	// cpu and sys are the counters in the next sample of the container, and
	// grow by the given steps each time.
	cpu, sys uint64
	// running is false once the container is stopped.
	running bool
	// skew is added to the time of each sample, as when the machine's clock
	// is wrong.
	skew time.Duration
	// answer, if it's set, answers a request instead of the engine, and says
	// whether it did.
	answer func(w http.ResponseWriter, r *http.Request) bool
}

func newEngine() *engine {
	return &engine{hits: map[string]int{}, running: true}
}

func (e *engine) hit(key string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hits[key]
}

func (e *engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	e.hits[r.Method+" "+r.URL.Path]++
	answer := e.answer
	e.mu.Unlock()
	if answer != nil && answer(w, r) {
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/containers/nope/"):
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such container: nope"}`))
	case r.URL.Path == "/containers/json":
		e.mu.Lock()
		state := "running"
		if !e.running {
			state = "exited"
		}
		e.mu.Unlock()
		reply(w, []map[string]any{
			{"Id": "aaaa1111", "Names": []string{"/web"}, "Image": "nginx", "State": state, "Status": "Up 2 hours", "Created": 1_800_000_000},
			{"Id": "bbbb2222", "Names": []string{"/old"}, "Image": "busybox", "State": "exited", "Status": "Exited (0) 1 day ago", "Created": 1_800_000_000},
		})
	case strings.HasSuffix(r.URL.Path, "/json") && strings.HasPrefix(r.URL.Path, "/containers/"):
		reply(w, map[string]any{"Id": "x", "Name": "/web", "State": map[string]any{"Running": true}, "Config": map[string]any{"Tty": false}})
	case strings.HasSuffix(r.URL.Path, "/stats") && r.URL.Query().Get("stream") == "0":
		e.mu.Lock()
		e.cpu += 500_000_000
		e.sys += 8_000_000_000
		cpu, sys := e.cpu, e.sys
		e.mu.Unlock()
		reply(w, statsAt(time.Now().Add(e.skew), cpu, sys))
	case strings.HasSuffix(r.URL.Path, "/stats"):
		e.streamStats(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start"):
		w.WriteHeader(http.StatusNotModified)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop"):
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// streamStats writes a sample a second until the client goes away or the
// container stops.
func (e *engine) streamStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	rc := http.NewResponseController(w)
	var cpu, sys uint64
	for {
		e.mu.Lock()
		running := e.running
		e.mu.Unlock()
		cpu, sys = cpu+500_000_000, sys+8_000_000_000
		var sample map[string]any
		if running {
			sample = statsAt(time.Now(), cpu, sys)
		} else {
			sample = map[string]any{"read": "0001-01-01T00:00:00Z"}
		}
		if err := json.NewEncoder(w).Encode(sample); err != nil {
			return
		}
		if err := rc.Flush(); err != nil {
			return
		}
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
			return
		}
	}
}

// statsAt returns a sample taken at a time.
func statsAt(at time.Time, cpu, sys uint64) map[string]any {
	return map[string]any{
		"read":         at.UTC().Format(time.RFC3339Nano),
		"cpu_stats":    map[string]any{"cpu_usage": map[string]any{"total_usage": cpu}, "system_cpu_usage": sys, "online_cpus": 4},
		"memory_stats": map[string]any{"usage": 100, "limit": 1000},
		"networks":     map[string]any{"eth0": map[string]any{"rx_bytes": 10, "tx_bytes": 20}},
		"pids_stats":   map[string]any{"current": 3},
	}
}

func TestHost_Entries(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newEngine()
		h := docker.NewHost(client(t, e))
		defer h.Close()

		got, err := h.Entries(t.Context())
		if err != nil {
			t.Fatalf("Entries() error = %v", err)
		}
		if len(got) != 2 || got[0].Container.Name() != "web" || got[1].Container.Name() != "old" {
			t.Fatalf("Entries() = %d entries, want web and old", len(got))
		}
		web := got[0]
		if web.Inspect == nil || web.Stats == nil {
			t.Fatalf("web = %+v, want it inspected and sampled", web)
		}
		// The container was new, so it was sampled twice, a second apart:
		// 0.5 s of CPU on four CPUs that gave 8 s is 25% of one.
		if !near(web.Stats.CPU, 25) {
			t.Errorf("web's CPU = %s, want 25", show(web.Stats.CPU))
		}
		if got[1].Inspect == nil || got[1].Stats != nil {
			t.Errorf("old = %+v, want it inspected, and not sampled since it isn't running", got[1])
		}
		if n := e.hit("GET /containers/aaaa1111/stats"); n != 2 {
			t.Errorf("sampled web %d times, want 2 for a container seen the first time", n)
		}

		// Within a few seconds, it's the same answer, with no more requests.
		if _, err := h.Entries(t.Context()); err != nil {
			t.Fatal(err)
		}
		if n := e.hit("GET /containers/json"); n != 1 {
			t.Errorf("listed %d times, want 1", n)
		}

		// A refresh takes one sample, and works the rate from the one before.
		time.Sleep(5 * time.Second)
		again, err := h.Entries(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if n := e.hit("GET /containers/aaaa1111/stats"); n != 3 {
			t.Errorf("sampled web %d times, want 3", n)
		}
		if !again[0].Stats.At.After(web.Stats.At) {
			t.Errorf("web's stats are from %v, want them newer than %v", again[0].Stats.At, web.Stats.At)
		}
		// The container was inspected once: its state and status didn't change.
		if n := e.hit("GET /containers/aaaa1111/json"); n != 1 {
			t.Errorf("inspected web %d times, want 1", n)
		}
	})
}

func TestHost_EntriesShareOneRound(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newEngine()
		h := docker.NewHost(client(t, e))
		defer h.Close()
		var wg sync.WaitGroup
		for range 10 {
			wg.Go(func() {
				if _, err := h.Entries(t.Context()); err != nil {
					t.Errorf("Entries() error = %v", err)
				}
			})
		}
		wg.Wait()
		if n := e.hit("GET /containers/json"); n != 1 {
			t.Errorf("ten viewers made %d list requests, want 1", n)
		}
	})
}

func TestHost_EntriesFail(t *testing.T) {
	t.Parallel()
	e := newEngine()
	e.answer = func(w http.ResponseWriter, _ *http.Request) bool {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"the daemon is restarting"}`))
		return true
	}
	h := docker.NewHost(client(t, e))
	defer h.Close()
	_, err := h.Entries(t.Context())
	var de *docker.Error
	if !errors.As(err, &de) || de.Status != 500 || de.Message != "the daemon is restarting" || de.ClientStatus() != http.StatusBadGateway {
		t.Errorf("Entries() error = %v, want Docker's 500 as a bad gateway", err)
	}
}

func TestHost_Do(t *testing.T) {
	t.Parallel()
	e := newEngine()
	h := docker.NewHost(client(t, e))
	defer h.Close()
	ctx := t.Context()

	if changed, err := h.Do(ctx, "web", docker.Stop); err != nil || !changed {
		t.Errorf("Do(stop) = %v, %v, want a change", changed, err)
	}
	// Docker answers 304 for a container that already is as asked.
	if changed, err := h.Do(ctx, "web", docker.Start); err != nil || changed {
		t.Errorf("Do(start) = %v, %v, want no change", changed, err)
	}
	if got := e.hit("POST /containers/web/stop"); got != 1 {
		t.Errorf("stop requests = %d, want 1", got)
	}
	if _, err := h.Do(ctx, "nope", docker.Stop); err == nil {
		t.Error("Do() on a container that doesn't exist succeeded")
	} else if de, ok := errors.AsType[*docker.Error](err); !ok || de.ClientStatus() != http.StatusNotFound {
		t.Errorf("Do() error = %v, want Docker's 404", err)
	}
	if _, err := h.Do(ctx, "web", "kill"); err == nil {
		t.Error("Do(kill) succeeded, want an error")
	}
}

func TestHost_DoRefreshesTheList(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newEngine()
		h := docker.NewHost(client(t, e))
		defer h.Close()
		list, err := h.Containers(t.Context())
		if err != nil || list[0].State != "running" {
			t.Fatalf("Containers() = %v, %v, want web running", list, err)
		}
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
		if _, err := h.Do(t.Context(), "web", docker.Stop); err != nil {
			t.Fatal(err)
		}
		list, err = h.Containers(t.Context())
		if err != nil || list[0].State != "exited" {
			t.Errorf("Containers() after a stop = %v, %v, want web exited at once", list, err)
		}
	})
}

func TestHost_IDsNeverReachAPath(t *testing.T) {
	t.Parallel()
	e := newEngine()
	h := docker.NewHost(client(t, e))
	defer h.Close()
	for _, id := range []string{"", "..", "../images/json", "a/b", "a?x=1", "a#b", "-rf", "a b", "a\x00b", strings.Repeat("a", 129), "é"} {
		if docker.ValidID(id) {
			t.Errorf("ValidID(%q) = true", id)
		}
		if _, err := h.Do(t.Context(), id, docker.Stop); !errors.Is(err, docker.ErrBadID) {
			t.Errorf("Do(%q) error = %v, want ErrBadID", id, err)
		}
		if _, err := h.Client().Inspect(t.Context(), id); !errors.Is(err, docker.ErrBadID) {
			t.Errorf("Inspect(%q) error = %v, want ErrBadID", id, err)
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.hits) != 0 {
		t.Errorf("Docker was asked for %v, want nothing", e.hits)
	}
	for _, id := range []string{"web", "a", "web-app_1.2", "0123456789abcdef", strings.Repeat("a", 128)} {
		if !docker.ValidID(id) {
			t.Errorf("ValidID(%q) = false", id)
		}
	}
}

func TestHost_Watch(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newEngine()
		h := docker.NewHost(client(t, e))
		defer h.Close()

		a := h.Watch("aaaa1111")
		first := <-a.C
		if first.CPU != nil {
			// A streamed sample from this fake has no precpu, so the first has no rate.
			t.Errorf("first CPU = %s, want none", show(first.CPU))
		}
		second := <-a.C
		if !near(second.CPU, 25) || !second.At.After(first.At) {
			t.Errorf("second = %+v, want 25%% CPU a second later", second)
		}

		// A viewer that joins gets what happened, and shares the stream.
		b := h.Watch("aaaa1111")
		if len(b.Recent) != 2 {
			t.Errorf("a late viewer's Recent has %d samples, want 2", len(b.Recent))
		}
		third := <-b.C
		if got := <-a.C; !got.At.Equal(third.At) {
			t.Errorf("the viewers got different samples: %v and %v", got.At, third.At)
		}
		if n := e.hit("GET /containers/aaaa1111/stats"); n != 1 {
			t.Errorf("Docker was asked for the stream %d times, want once for both viewers", n)
		}

		// The stream stays open for a moment after the last viewer leaves.
		a.Close()
		b.Close()
		time.Sleep(10 * time.Second)
		c := h.Watch("aaaa1111")
		if len(c.Recent) < 12 {
			t.Errorf("a viewer who came back soon has %d recent samples, want the stream kept", len(c.Recent))
		}
		if n := e.hit("GET /containers/aaaa1111/stats"); n != 1 {
			t.Errorf("Docker was asked for the stream %d times, want it kept open", n)
		}
		c.Close()
		time.Sleep(20 * time.Second)
		synctest.Wait()
		d := h.Watch("aaaa1111")
		if len(d.Recent) != 0 {
			t.Errorf("after the stream closed, a new viewer has %d recent samples, want a new stream", len(d.Recent))
		}
		d.Close()
	})
}

func TestHost_WatchStoppedContainer(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newEngine()
		h := docker.NewHost(client(t, e))
		defer h.Close()
		w := h.Watch("aaaa1111")
		<-w.C
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
		select {
		case <-w.Done():
		case <-time.After(30 * time.Second):
			t.Fatal("the stats didn't end after the container stopped")
		}
		if got := w.Reason(); got != "The container stopped" {
			t.Errorf("Reason() = %q", got)
		}
		// Stats of a stopped container start again with the next viewer.
		e.mu.Lock()
		e.running = true
		e.mu.Unlock()
		again := h.Watch("aaaa1111")
		defer again.Close()
		if _, ok := <-again.C; !ok {
			t.Error("the new viewer's stats ended at once")
		}
	})
}

func TestHost_WatchAfterClose(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := docker.NewHost(client(t, newEngine()))
		w := h.Watch("aaaa1111")
		<-w.C
		h.Close()
		<-w.Done()
		if got := h.Watch("aaaa1111"); got != nil {
			t.Error("Watch() after Close() returned a watch")
		}
	})
}

// A machine whose clock is far off still has its counters used: they're as
// old as Roost's clock says, not the machine's.
func TestHost_EntriesWithAnotherClock(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// The bubble's clock starts in the year 2000, and a clock behind it
		// must not go before that, which Docker takes for the zero time.
		time.Sleep(48 * time.Hour)
		e := newEngine()
		e.skew = -3 * time.Hour
		h := docker.NewHost(client(t, e))
		defer h.Close()
		if _, err := h.Entries(t.Context()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Second)
		got, err := h.Entries(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		// Two samples for a container seen the first time, and one for the
		// next refresh, not two again.
		if n := e.hit("GET /containers/aaaa1111/stats"); n != 3 {
			t.Errorf("sampled web %d times, want 3", n)
		}
		if got[0].Stats == nil || !near(got[0].Stats.CPU, 25) {
			t.Errorf("web's stats = %+v, want 25%% CPU", got[0].Stats)
		}
	})
}

// An action goes on when the caller goes away: Docker acts anyway.
func TestHost_DoGoesOnWithoutTheCaller(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newEngine()
		release := make(chan struct{})
		e.answer = func(w http.ResponseWriter, r *http.Request) bool {
			if r.Method != http.MethodPost {
				return false
			}
			<-release
			w.WriteHeader(http.StatusNoContent)
			return true
		}
		h := docker.NewHost(client(t, e))
		defer h.Close()
		ctx, cancel := context.WithCancel(t.Context())
		type result struct {
			changed bool
			err     error
		}
		done := make(chan result)
		go func() {
			changed, err := h.Do(ctx, "web", docker.Stop)
			done <- result{changed, err}
		}()
		synctest.Wait()
		cancel()
		time.Sleep(time.Second)
		close(release)
		if got := <-done; got.err != nil || !got.changed {
			t.Errorf("Do() = %+v after the caller left, want the stop to finish", got)
		}
	})
}

func TestHost_NameAndStatsReasonsHideTheCause(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newEngine()
		e.answer = func(w http.ResponseWriter, r *http.Request) bool {
			if !strings.HasSuffix(r.URL.Path, "/stats") {
				return false
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"mount /home/someone/secret-dir failed"}`))
			return true
		}
		h := docker.NewHost(client(t, e))
		defer h.Close()
		w := h.Watch("aaaa1111")
		<-w.Done()
		// Viewers read the reason of a stream that ended.
		if got := w.Reason(); strings.Contains(got, "secret-dir") || got == "" {
			t.Errorf("Reason() = %q, want one that doesn't say what Docker said", got)
		}
	})
}

func TestClient_logsStreamThatNeverStarts(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newEngine()
		e.answer = func(w http.ResponseWriter, r *http.Request) bool {
			if !strings.HasSuffix(r.URL.Path, "/logs") {
				return false
			}
			<-r.Context().Done()
			return true
		}
		c := client(t, e)
		_, _, err := c.Logs(t.Context(), "web", false, docker.LogOptions{Stdout: true, Tail: -1})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Logs() error = %v, want it to have timed out", err)
		}
	})
}

func TestHost_Name(t *testing.T) {
	t.Parallel()
	h := docker.NewHost(client(t, newEngine()))
	defer h.Close()
	for _, id := range []string{"aaaa1111", "aaaa", "web"} {
		if got, err := h.Name(t.Context(), id); err != nil || got != "web" {
			t.Errorf("Name(%q) = %q, %v, want web", id, got, err)
		}
	}
	// A container that left the list is still found by inspecting it.
	if got, err := h.Name(t.Context(), "gone"); err != nil || got != "web" {
		t.Errorf("Name(gone) = %q, %v, want the inspected name", got, err)
	}
}

func TestCache_failureIsNotKept(t *testing.T) {
	t.Parallel()
	var fail atomic.Bool
	fail.Store(true)
	e := newEngine()
	e.answer = func(w http.ResponseWriter, _ *http.Request) bool {
		if fail.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return true
		}
		return false
	}
	h := docker.NewHost(client(t, e))
	defer h.Close()
	if _, err := h.Containers(context.Background()); err == nil {
		t.Fatal("Containers() succeeded while Docker failed")
	}
	fail.Store(false)
	if _, err := h.Containers(context.Background()); err != nil {
		t.Errorf("Containers() error = %v, want the failure forgotten", err)
	}
	if n := e.hit("GET /containers/json"); n != 2 {
		t.Errorf("listed %d times, want 2", n)
	}
}

package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"
)

const (
	// recentSamples is how many of a container's latest samples are kept, so a
	// page that opens can draw its charts at once: five minutes.
	recentSamples = 300
	// feedLinger is how long a container's stats stay open after the last
	// viewer leaves, so a reload finds them.
	feedLinger = 15 * time.Second
	// watchBuffer is how many samples a viewer can be behind by before it
	// misses some.
	watchBuffer = 16
	// stoppedSamples is how many empty samples in a row mean a container
	// stopped: Docker goes on sending them instead of ending the stream.
	stoppedSamples = 3
)

// A Watch is one viewer's view of a container's live stats. Many viewers of a
// container share one stream from Docker.
type Watch struct {
	// Recent are the latest samples from before the Watch began, oldest first.
	Recent []Stats
	// C receives each sample. A viewer that falls behind misses samples.
	C <-chan Stats
	c chan Stats
	f *feed
}

// Done is closed when the stats end: when the container stops, or Docker
// stops answering.
func (w *Watch) Done() <-chan struct{} { return w.f.done }

// Reason says why the stats ended. Call it after Done is closed.
func (w *Watch) Reason() string { return w.f.reason }

// Close stops watching. The stream stays open for a moment for the next
// viewer.
func (w *Watch) Close() { w.f.leave(w) }

// feed is the stats of one container, read once for all its viewers.
type feed struct {
	h      *Host
	id     string
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	subs   map[*Watch]struct{}
	recent []Stats
	linger *time.Timer
	ended  bool
	reason string
}

// Watch starts watching the live stats of a container, by its full ID. It
// returns nil once the Host is closed.
func (h *Host) Watch(id string) *Watch {
	for {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return nil
		}
		f := h.feeds[id]
		if f == nil {
			f = h.startFeed(id)
			h.feeds[id] = f
		}
		h.mu.Unlock()
		if w := f.join(); w != nil {
			return w
		}
		// The stats ended just now. The next try starts them again.
	}
}

// startFeed starts reading a container's stats. The Host holds its lock.
func (h *Host) startFeed(id string) *feed {
	ctx, cancel := context.WithCancel(context.Background())
	f := &feed{h: h, id: id, cancel: cancel, done: make(chan struct{}), subs: map[*Watch]struct{}{}}
	go f.run(ctx)
	return f
}

func (f *feed) join() *Watch {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ended {
		return nil
	}
	if f.linger != nil {
		f.linger.Stop()
		f.linger = nil
	}
	c := make(chan Stats, watchBuffer)
	w := &Watch{Recent: append([]Stats(nil), f.recent...), C: c, c: c, f: f}
	f.subs[w] = struct{}{}
	return w
}

func (f *feed) leave(w *Watch) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.subs, w)
	if len(f.subs) == 0 && !f.ended && f.linger == nil {
		f.linger = time.AfterFunc(feedLinger, func() {
			f.mu.Lock()
			idle := len(f.subs) == 0
			f.mu.Unlock()
			if idle {
				f.finish("nobody is watching")
			}
		})
	}
}

// stop ends the feed without a reason a viewer would read.
func (f *feed) stop() { f.finish("the stats were closed") }

func (f *feed) run(ctx context.Context) {
	body, err := f.h.client.StatsStream(ctx, f.id)
	if err != nil {
		f.finish(err.Error())
		return
	}
	defer func() { _ = body.Close() }()
	dec := json.NewDecoder(body)
	var (
		prev  *Counters
		empty int
	)
	for {
		var raw RawStats
		if err := dec.Decode(&raw); err != nil {
			switch {
			case ctx.Err() != nil:
				// finish has run, or is about to.
			case errors.Is(err, io.EOF):
				f.finish("The container stopped")
			default:
				f.finish(err.Error())
			}
			return
		}
		s, c, ok := Derive(&raw, prev)
		if !ok {
			if empty++; empty >= stoppedSamples {
				f.finish("The container stopped")
				return
			}
			continue
		}
		empty, prev = 0, &c
		f.publish(s)
	}
}

func (f *feed) publish(s Stats) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recent = append(f.recent, s)
	if len(f.recent) > recentSamples {
		f.recent = f.recent[len(f.recent)-recentSamples:]
	}
	for w := range f.subs {
		select {
		case w.c <- s:
		default:
			// This viewer is behind. The next sample is soon enough.
		}
	}
}

// finish ends the feed for its viewers, once.
func (f *feed) finish(reason string) {
	f.mu.Lock()
	if f.ended {
		f.mu.Unlock()
		return
	}
	f.ended, f.reason = true, reason
	if f.linger != nil {
		f.linger.Stop()
		f.linger = nil
	}
	f.mu.Unlock()
	f.cancel()
	f.h.mu.Lock()
	if f.h.feeds[f.id] == f {
		delete(f.h.feeds, f.id)
	}
	f.h.mu.Unlock()
	close(f.done)
}

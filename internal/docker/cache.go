package docker

import (
	"context"
	"sync"
	"time"
)

// A cache keeps the last answer to a question for a while, and lets callers
// that ask while it is being worked out share the answer, so a page that
// several people have open costs Docker one set of requests.
type cache[T any] struct {
	ttl time.Duration

	mu   sync.Mutex
	at   time.Time
	val  T
	has  bool
	call *call[T]
	// gen counts the times the cache was told to forget, so an answer that
	// was being worked out then isn't taken for one that's current.
	gen int
}

type call[T any] struct {
	done chan struct{}
	gen  int
	val  T
	err  error
}

func newCache[T any](ttl time.Duration) *cache[T] { return &cache[T]{ttl: ttl} }

// get returns the answer, which load works out unless it is younger than the
// cache's ttl. A load goes on if the caller that started it goes away,
// since another may be waiting for it.
func (c *cache[T]) get(ctx context.Context, load func(ctx context.Context) (T, error)) (T, error) {
	c.mu.Lock()
	if c.has && time.Since(c.at) < c.ttl {
		val := c.val
		c.mu.Unlock()
		return val, nil
	}
	cl := c.call
	if cl == nil || cl.gen != c.gen {
		cl = &call[T]{done: make(chan struct{}), gen: c.gen}
		c.call = cl
		go func() {
			cl.val, cl.err = load(context.WithoutCancel(ctx))
			c.mu.Lock()
			if cl.err == nil && cl.gen == c.gen {
				c.val, c.at, c.has = cl.val, time.Now(), true
			}
			if c.call == cl {
				c.call = nil
			}
			c.mu.Unlock()
			close(cl.done)
		}()
	}
	c.mu.Unlock()
	select {
	case <-cl.done:
		return cl.val, cl.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// forget drops the answer, and the one being worked out, so the next get
// works it out again.
func (c *cache[T]) forget() {
	c.mu.Lock()
	c.has = false
	c.gen++
	c.mu.Unlock()
}

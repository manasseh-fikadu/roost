package api

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// A limiter lets each key, such as a machine, make a few requests at once and
// a steady number of them after that: a token bucket per key.
type limiter struct {
	// rate is how many requests a second a key may keep up, and burst how many
	// it may make at once after a quiet time.
	rate  float64
	burst float64

	mu       sync.Mutex
	buckets  map[string]*bucket
	capacity int
}

type bucket struct {
	tokens float64
	at     time.Time
}

// maxBuckets is how many keys a limiter keeps before it forgets the ones that
// have been quiet long enough to be full again. A key is a machine, so it's
// far more than a tailnet has.
const maxBuckets = 4096

func newLimiter(rate float64, burst int) *limiter {
	return &limiter{rate: rate, burst: float64(burst), buckets: map[string]*bucket{}, capacity: maxBuckets}
}

// allow reports whether key may make a request at now. If it may not, it
// returns how long until it may.
func (l *limiter) allow(key string, now time.Time) (ok bool, wait time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= l.capacity {
			l.sweep(now)
		}
		b = &bucket{tokens: l.burst, at: now}
		l.buckets[key] = b
	}
	b.tokens = math.Min(l.burst, b.tokens+now.Sub(b.at).Seconds()*l.rate)
	b.at = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}

// sweep forgets the keys whose buckets have filled up again, which is the
// same as never having been seen. If none has, it forgets them all rather
// than grow without end.
func (l *limiter) sweep(now time.Time) {
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.at).Seconds()*l.rate >= l.burst {
			delete(l.buckets, k)
		}
	}
	if len(l.buckets) >= l.capacity {
		clear(l.buckets)
	}
}

// The limits on routes that machines call, which aren't behind a role. They
// are there to stop a loop, not to ration anything: a machine that enrolls
// does it once, and may run the command a few times more while it sorts out
// what went wrong. An agent reports every 15 seconds, or as often as its
// INTERVAL says.
const (
	enrollRate     = 0.3 // about one every 3 seconds
	enrollBurst    = 20
	heartbeatRate  = 2.0
	heartbeatBurst = 20
)

// limits holds the limiters of the routes that machines call.
type limits struct {
	enroll, agent *limiter
}

func newLimits() limits {
	return limits{
		enroll: newLimiter(enrollRate, enrollBurst),
		agent:  newLimiter(heartbeatRate, heartbeatBurst),
	}
}

// limitKey says who a request from a machine counts against: the machine
// Tailscale says it came from, or else the address it came from.
func limitKey(c caller, remoteAddr string) string {
	if c.node != "" {
		return c.node
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// tooMany answers a request that went over its limiter, and reports whether
// it did.
func (h *handler) tooMany(w http.ResponseWriter, r *http.Request, l *limiter) bool {
	ok, wait := l.allow(limitKey(callerOf(r), r.RemoteAddr), time.Now())
	if ok {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	h.deny(w, r, http.StatusTooManyRequests, "too many requests from this machine; wait a moment")
	return true
}

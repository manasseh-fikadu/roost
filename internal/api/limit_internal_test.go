package api

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestLimiter(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		l := newLimiter(1, 3)

		// A key can spend its burst at once, and then has to wait.
		for i := range 3 {
			if ok, _ := l.allow("a", time.Now()); !ok {
				t.Fatalf("request %d of the burst was refused", i+1)
			}
		}
		ok, wait := l.allow("a", time.Now())
		if ok || wait != time.Second {
			t.Fatalf("after the burst: allowed %v, wait %v, want refused for 1s", ok, wait)
		}

		// Another key has a burst of its own.
		if ok, _ := l.allow("b", time.Now()); !ok {
			t.Error("b was refused because a used its requests")
		}

		// A token a second comes back.
		time.Sleep(time.Second)
		if ok, _ := l.allow("a", time.Now()); !ok {
			t.Error("a was refused after a second")
		}
		if ok, _ := l.allow("a", time.Now()); ok {
			t.Error("a had two requests in a second")
		}

		// A long quiet time gives back the burst and no more.
		time.Sleep(time.Hour)
		for i := range 3 {
			if ok, _ := l.allow("a", time.Now()); !ok {
				t.Fatalf("request %d after an hour was refused", i+1)
			}
		}
		if ok, _ := l.allow("a", time.Now()); ok {
			t.Error("a had more than its burst after an hour")
		}
	})
}

func TestLimiter_forgets(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		l := newLimiter(1, 2)
		l.capacity = 4

		// Machines that are quiet again are forgotten when there's no room.
		for _, k := range []string{"a", "b", "c", "d"} {
			l.allow(k, time.Now())
		}
		time.Sleep(time.Minute)
		l.allow("e", time.Now())
		if len(l.buckets) != 1 {
			t.Errorf("%d keys kept, want only the new one", len(l.buckets))
		}

		// Machines that are all busy don't make it grow without end.
		for _, k := range []string{"f", "g", "h"} {
			l.allow(k, time.Now())
			l.allow(k, time.Now())
			l.allow(k, time.Now())
		}
		for _, k := range []string{"i", "j", "k"} {
			l.allow(k, time.Now())
			if len(l.buckets) > l.capacity {
				t.Fatalf("%d keys kept after %s, want at most %d", len(l.buckets), k, l.capacity)
			}
		}
	})
}

func TestLimitKey(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		caller caller
		remote string
		want   string
	}{
		{"a known machine", caller{node: "n123"}, "100.64.0.5:4000", "n123"},
		{"an unknown address", caller{}, "100.64.0.9:4000", "100.64.0.9"},
		{"an address without a port", caller{}, "100.64.0.9", "100.64.0.9"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := limitKey(tt.caller, tt.remote); got != tt.want {
				t.Errorf("limitKey = %q, want %q", got, tt.want)
			}
		})
	}
}

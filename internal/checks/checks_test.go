package checks_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/checks"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// newStore opens a new store without its default checks. Call it in a
// synctest bubble.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "roost.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// Let the prune that Open starts finish, so it doesn't hold the store.
	synctest.Wait()
	defaults, err := st.Checks(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range defaults {
		if err := st.DeleteCheck(t.Context(), c.ID); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

// newRunner returns a loaded Runner for st on the network n.
func newRunner(t *testing.T, st *store.Store, n *fakeNet) *checks.Runner {
	t.Helper()
	r := checks.NewRunner(st, checks.Options{Dial: n.dial, Lookup: n.lookup})
	if err := r.Load(t.Context()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return r
}

// start runs r until the test ends.
func start(t *testing.T, r *checks.Runner) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// tcpCheck returns a TCP check of addr, every interval, which times out
// after a second.
func tcpCheck(name, addr string, interval time.Duration) store.Check {
	return store.Check{Name: name, Kind: checks.KindTCP, Target: addr, Group: "Test", Interval: interval, Timeout: time.Second}
}

func add(t *testing.T, r *checks.Runner, c store.Check) store.Check {
	t.Helper()
	c, err := r.Add(t.Context(), c)
	if err != nil {
		t.Fatalf("Add(%s) error = %v", c.Name, err)
	}
	return c
}

// recent returns the details of a check's latest results, oldest first.
func recent(t *testing.T, r *checks.Runner, id string) []string {
	t.Helper()
	s, ok := r.Check(id)
	if !ok {
		t.Fatalf("Check(%s) found nothing", id)
	}
	var out []string
	for _, res := range s.Recent {
		out = append(out, res.Detail)
	}
	return out
}

// stored returns the details of a check's results in the store, oldest
// first.
func stored(t *testing.T, st *store.Store, id string) []string {
	t.Helper()
	results, err := st.RecentCheckResults(t.Context(), time.Time{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, res := range results[id] {
		out = append(out, res.Detail)
	}
	return out
}

func TestProbes(t *testing.T) {
	t.Parallel()
	status := func(code int) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
	}
	httpCheck := func(target string, expect int) store.Check {
		return store.Check{Kind: checks.KindHTTP, Target: target, ExpectStatus: expect, Timeout: time.Second}
	}
	dnsCheck := store.Check{Kind: checks.KindDNS, Target: "example.com", Timeout: time.Second}
	tests := []struct {
		name string
		// serve, if it's set, answers on api.example.com, on port 443 over
		// TLS with a certificate that no one trusts if secure is set, and
		// on port 80 if it isn't.
		serve  http.Handler
		secure bool
		// listen makes db.example.com:5432 accept connections, and hold
		// makes it never answer.
		listen, hold bool
		// hosts are the names that resolve.
		hosts map[string][]netip.Addr
		check store.Check
		want  checks.Result
	}{
		{
			name:  "an HTTP status below 400 passes",
			serve: status(http.StatusOK),
			check: httpCheck("http://api.example.com/health", 0),
			want:  checks.Result{OK: true, Latency: netDelay, Detail: "HTTP 200 OK"},
		},
		{
			name:  "an HTTP status of 400 or more fails",
			serve: status(http.StatusServiceUnavailable),
			check: httpCheck("http://api.example.com/", 0),
			want:  checks.Result{Latency: netDelay, Detail: "HTTP 503 Service Unavailable"},
		},
		{
			name:  "another status than the one expected fails",
			serve: status(http.StatusOK),
			check: httpCheck("http://api.example.com/", http.StatusNoContent),
			want:  checks.Result{Latency: netDelay, Detail: "HTTP 200 OK (expected 204)"},
		},
		{
			name:  "the status expected passes, even 400 or more",
			serve: status(http.StatusUnauthorized),
			check: httpCheck("http://api.example.com/", http.StatusUnauthorized),
			want:  checks.Result{OK: true, Latency: netDelay, Detail: "HTTP 401 Unauthorized"},
		},
		{
			name:  "a redirect is the answer",
			serve: http.RedirectHandler("http://elsewhere.example.com/", http.StatusFound),
			check: httpCheck("http://api.example.com/", 0),
			want:  checks.Result{OK: true, Latency: netDelay, Detail: "HTTP 302 Found"},
		},
		{
			name: "the time to answer counts",
			serve: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				time.Sleep(40 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
			}),
			check: httpCheck("http://api.example.com/", 0),
			want:  checks.Result{OK: true, Latency: netDelay + 40*time.Millisecond, Detail: "HTTP 200 OK"},
		},
		{
			name:   "a certificate no one trusts fails",
			serve:  status(http.StatusOK),
			secure: true,
			check:  httpCheck("https://api.example.com/", 0),
			want:   checks.Result{Detail: "tls: failed to verify certificate: x509: certificate signed by unknown authority"},
		},
		{
			name:   "insecure skips checking the certificate",
			serve:  status(http.StatusOK),
			secure: true,
			check:  store.Check{Kind: checks.KindHTTP, Target: "https://api.example.com/", Insecure: true, Timeout: time.Second},
			want:   checks.Result{OK: true, Latency: netDelay, Detail: "HTTP 200 OK"},
		},
		{
			// The rest of a URL can hold a secret, such as a token.
			name:  "an error leaves out the URL",
			check: httpCheck("https://api.example.com/private/path?item=42", 0),
			want:  checks.Result{Detail: "dial tcp: connect: connection refused"},
		},
		{
			name: "an answer that's too slow times out",
			serve: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
				case <-time.After(2 * time.Second):
					w.WriteHeader(http.StatusOK)
				}
			}),
			check: httpCheck("http://api.example.com/", 0),
			want:  checks.Result{Detail: "Timed out after 1s"},
		},
		{
			name:   "a TCP connection passes",
			listen: true,
			check:  tcpCheck("", "db.example.com:5432", time.Minute),
			want:   checks.Result{OK: true, Latency: netDelay, Detail: "Connected"},
		},
		{
			name:  "a refused TCP connection fails",
			check: tcpCheck("", "db.example.com:5432", time.Minute),
			want:  checks.Result{Detail: "dial tcp: connect: connection refused"},
		},
		{
			name:  "a TCP connection that doesn't open times out",
			hold:  true,
			check: store.Check{Kind: checks.KindTCP, Target: "db.example.com:5432", Timeout: 1500 * time.Millisecond},
			want:  checks.Result{Detail: "Timed out after 1.5s"},
		},
		{
			name:  "a name with an address passes",
			hosts: map[string][]netip.Addr{"example.com": {netip.MustParseAddr("192.0.2.1")}},
			check: dnsCheck,
			want:  checks.Result{OK: true, Latency: netDelay, Detail: "192.0.2.1"},
		},
		{
			name: "a name with several addresses passes",
			hosts: map[string][]netip.Addr{"example.com": {
				netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2"),
			}},
			check: dnsCheck,
			// IPv4 first.
			want: checks.Result{OK: true, Latency: netDelay, Detail: "192.0.2.1 and 2 more"},
		},
		{
			name:  "a name without addresses fails",
			hosts: map[string][]netip.Addr{"example.com": {}},
			check: dnsCheck,
			want:  checks.Result{Latency: netDelay, Detail: "No addresses"},
		},
		{
			name:  "a name that doesn't exist fails",
			check: dnsCheck,
			want:  checks.Result{Detail: "lookup example.com: no such host"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				n := newFakeNet()
				switch {
				case tt.serve != nil && tt.secure:
					n.serve(t, "api.example.com:443", true, tt.serve)
				case tt.serve != nil:
					n.serve(t, "api.example.com:80", false, tt.serve)
				case tt.listen:
					n.listen(t)
				case tt.hold:
					t.Cleanup(n.hold("db.example.com:5432"))
				}
				maps.Copy(n.hosts, tt.hosts)
				r := newRunner(t, newStore(t), n)
				start(t, r)
				c := tt.check
				c.Name, c.Group, c.Interval, c.Paused = "Probe", "Test", time.Hour, true
				c = add(t, r, c)
				began := time.Now()
				got, err := r.RunNow(t.Context(), c.ID)
				if err != nil {
					t.Fatalf("RunNow() error = %v", err)
				}
				if !got.At.Equal(began) {
					t.Errorf("RunNow() result at %v, want when it started, %v", got.At, began)
				}
				got.At = time.Time{}
				if got != tt.want {
					t.Errorf("RunNow() = %+v, want %+v", got, tt.want)
				}
				if n.dialsTo("elsewhere.example.com:80") != 0 {
					t.Error("an HTTP check followed a redirect")
				}
				// A net.Pipe has no buffer, so a TLS client and server that
				// close at once block each other's close alerts until their
				// 5-second write deadline. Let that pass before the bubble
				// ends.
				time.Sleep(10 * time.Second)
				synctest.Wait()
			})
		})
	}
}

func TestRunner_runsEachCheckWhenItsDue(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newFakeNet()
		n.listen(t)
		st := newStore(t)
		r := newRunner(t, st, n)
		start(t, r)
		fast := add(t, r, tcpCheck("Fast", "db.example.com:5432", 10*time.Second))
		slow := add(t, r, tcpCheck("Slow", "cache.example.com:6379", time.Minute))
		paused := tcpCheck("Paused", "db.example.com:5432", 10*time.Second)
		paused.Paused = true
		paused = add(t, r, paused)

		// New checks run straight away, and then every interval.
		synctest.Wait()
		time.Sleep(time.Minute - time.Millisecond)
		synctest.Wait()
		if got := recent(t, r, fast.ID); len(got) != 6 {
			t.Errorf("after a minute, the 10-second check ran %d times, want 6", len(got))
		}
		if got, want := recent(t, r, slow.ID), []string{"dial tcp: connect: connection refused"}; !slices.Equal(got, want) {
			t.Errorf("after a minute, the 1-minute check's results = %q, want %q", got, want)
		}
		if got := recent(t, r, paused.ID); len(got) != 0 {
			t.Errorf("the paused check ran %d times, want none", len(got))
		}
		if got := stored(t, st, fast.ID); len(got) != 6 {
			t.Errorf("the store has %d results of the 10-second check, want 6", len(got))
		}
		s, _ := r.Check(slow.ID)
		if s.Failures != 1 || s.Uptime == nil || *s.Uptime != 0 {
			t.Errorf("the failing check has %d failures in a row and uptime %v, want 1 and 0%%", s.Failures, s.Uptime)
		}
		if s, _ := r.Check(fast.ID); s.Uptime == nil || *s.Uptime != 100 {
			t.Errorf("the passing check has uptime %v, want 100%%", s.Uptime)
		}

		// Only the latest results are kept in memory.
		time.Sleep(time.Hour)
		synctest.Wait()
		if got := recent(t, r, fast.ID); len(got) != checks.RecentResults {
			t.Errorf("after an hour, the 10-second check has %d latest results, want %d", len(got), checks.RecentResults)
		}
	})
}

func TestRunner_runsSixAtOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newFakeNet()
		release := n.hold("slow.example.com:80")
		r := newRunner(t, newStore(t), n)
		start(t, r)
		var ids []string
		for i := range checks.MaxRunning + 2 {
			ids = append(ids, add(t, r, tcpCheck(fmt.Sprint("Slow ", i), "slow.example.com:80", time.Minute)).ID)
		}
		synctest.Wait()
		if got := n.waitingDials(); got != checks.MaxRunning {
			t.Fatalf("%d checks run at once, want %d", got, checks.MaxRunning)
		}

		// The others run as soon as the first ones end.
		release()
		synctest.Wait()
		if got := n.dialsTo("slow.example.com:80"); got != checks.MaxRunning+2 {
			t.Errorf("after the first ones ended, the checks ran %d times, want %d", got, checks.MaxRunning+2)
		}
		for _, id := range ids {
			if got := recent(t, r, id); len(got) != 1 {
				t.Errorf("check %s has %d results, want 1", id, len(got))
			}
		}
	})
}

func TestRunner_RunNow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newFakeNet()
		n.listen(t)
		st := newStore(t)
		r := newRunner(t, st, n)
		start(t, r)

		c := tcpCheck("DB", "db.example.com:5432", time.Hour)
		c.Paused = true
		c = add(t, r, c)
		res, err := r.RunNow(t.Context(), c.ID)
		if err != nil || !res.OK {
			t.Fatalf("RunNow() of a paused check = %+v, %v, want it to pass", res, err)
		}
		if got := stored(t, st, c.ID); !slices.Equal(got, []string{"Connected"}) {
			t.Errorf("the store has %q, want the run's result", got)
		}
		if _, err := r.RunNow(t.Context(), "missing"); !errors.Is(err, checks.ErrNotFound) {
			t.Errorf("RunNow() of a missing check: error = %v, want ErrNotFound", err)
		}

		// Asking while it runs waits for that run.
		release := n.hold("slow.example.com:80")
		slow := add(t, r, tcpCheck("Slow", "slow.example.com:80", time.Hour))
		synctest.Wait()
		got := make(chan error)
		go func() {
			_, err := r.RunNow(t.Context(), slow.ID)
			got <- err
		}()
		synctest.Wait()
		release()
		if err := <-got; err != nil {
			t.Errorf("RunNow() while it ran: error = %v", err)
		}
		if dials := n.dialsTo("slow.example.com:80"); dials != 1 {
			t.Errorf("the check ran %d times, want once", dials)
		}

		// A run that hasn't started when the check is deleted fails.
		release = n.hold("slow.example.com:80")
		defer release()
		for i := range checks.MaxRunning {
			add(t, r, tcpCheck(fmt.Sprint("Busy ", i), "slow.example.com:80", time.Hour))
		}
		synctest.Wait()
		go func() {
			_, err := r.RunNow(t.Context(), c.ID)
			got <- err
		}()
		synctest.Wait()
		if err := r.Delete(t.Context(), c.ID); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if err := <-got; !errors.Is(err, checks.ErrNotFound) {
			t.Errorf("RunNow() of a check deleted before it ran: error = %v, want ErrNotFound", err)
		}
	})
}

func TestRunner_changeAndDelete(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		n := newFakeNet()
		n.listen(t)
		release := n.hold("old.example.com:5432")
		defer release()
		st := newStore(t)
		r := newRunner(t, st, n)
		start(t, r)
		c := add(t, r, tcpCheck("DB", "old.example.com:5432", time.Hour))
		synctest.Wait()

		// The run in progress was of the old target, so it doesn't count,
		// and the check runs again straight away.
		c.Name, c.Target = "Database", "db.example.com:5432"
		changed, err := r.Change(t.Context(), c)
		if err != nil {
			t.Fatalf("Change() error = %v", err)
		}
		if changed.Created.IsZero() || changed.Name != "Database" {
			t.Errorf("Change() = %+v, want the new check, created as before", changed)
		}
		release()
		time.Sleep(time.Second)
		synctest.Wait()
		if got := recent(t, r, c.ID); !slices.Equal(got, []string{"Connected"}) {
			t.Errorf("after Change(), the results = %q, want only the new target's", got)
		}
		if got := stored(t, st, c.ID); !slices.Equal(got, []string{"Connected"}) {
			t.Errorf("after Change(), the store has %q, want only the new target's result", got)
		}
		if s, _ := r.Check(c.ID); s.Check.Name != "Database" {
			t.Errorf("Check() = %+v, want the new name", s.Check)
		}

		if err := r.Delete(t.Context(), c.ID); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if _, ok := r.Check(c.ID); ok {
			t.Error("Check() found a deleted check")
		}
		if len(r.Checks()) != 0 {
			t.Errorf("Checks() = %+v, want none", r.Checks())
		}
		for name, err := range map[string]error{
			"Delete": r.Delete(t.Context(), c.ID),
			"Change": func() error { _, err := r.Change(t.Context(), c); return err }(),
		} {
			if !errors.Is(err, checks.ErrNotFound) {
				t.Errorf("%s() of a deleted check: error = %v, want ErrNotFound", name, err)
			}
		}
		if got, err := st.Checks(t.Context()); err != nil || len(got) != 0 {
			t.Errorf("the store has checks %+v, %v, want none", got, err)
		}
	})
}

func TestRunner_Checks(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRunner(t, newStore(t), newFakeNet())
		for _, c := range []store.Check{
			{Name: "b", Group: "web"},
			{Name: "A", Group: "Web"},
			{Name: "z", Group: "Internet"},
		} {
			c.Kind, c.Target, c.Interval, c.Timeout = checks.KindTCP, "db:5432", time.Minute, time.Second
			add(t, r, c)
		}
		var got []string
		for _, s := range r.Checks() {
			got = append(got, s.Check.Group+"/"+s.Check.Name)
		}
		if want := []string{"Internet/z", "Web/A", "web/b"}; !slices.Equal(got, want) {
			t.Errorf("Checks() = %q, want %q, by group and name in any case", got, want)
		}
	})
}

func TestRunner_Load(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		st := newStore(t)
		now := time.Now()
		c, err := st.AddCheck(t.Context(), tcpCheck("DB", "db.example.com:5432", time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		for i, ok := range []bool{true, true, false, false} {
			res := store.CheckResult{CheckID: c.ID, At: now.Add(time.Duration(i-4) * time.Minute), OK: ok, Detail: strconv.Itoa(i)}
			if err := st.AddCheckResult(t.Context(), res); err != nil {
				t.Fatal(err)
			}
		}
		err = st.AddPings(t.Context(), []store.Ping{
			{DeviceID: "laptop", At: now.Add(-2 * time.Minute), Replied: true, RTT: 9 * time.Millisecond},
			{DeviceID: "laptop", At: now.Add(-time.Minute)},
		})
		if err != nil {
			t.Fatal(err)
		}

		r := newRunner(t, st, newFakeNet())
		s, ok := r.Check(c.ID)
		if !ok {
			t.Fatal("Check() after Load() found nothing")
		}
		if got := recent(t, r, c.ID); !slices.Equal(got, []string{"0", "1", "2", "3"}) || s.Failures != 2 {
			t.Errorf("after Load(), the results are %q with %d failures in a row, want 4 with 2", got, s.Failures)
		}
		if s.Uptime == nil || *s.Uptime != 50 {
			t.Errorf("after Load(), the uptime is %v, want 50%%", s.Uptime)
		}
		p := r.Pings()["laptop"]
		if len(p.Recent) != 2 || p.Recent[0].Detail != "Replied" || p.Recent[0].Latency != 9*time.Millisecond ||
			p.Recent[1].Detail != "No reply" || p.Misses != 1 || p.Uptime == nil || *p.Uptime != 50 {
			t.Errorf("after Load(), the laptop's pings = %+v, want its 2 pings, 1 missed", p)
		}
	})
}

func TestRunner_ObservePings(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRunner(t, newStore(t), newFakeNet())
		now := time.Now()
		status := &tailnet.Status{Peers: []tailnet.Device{{ID: "laptop"}, {ID: "phone"}, {ID: "server"}}}
		reply := func(at time.Duration, res tailnet.PingResult) tailnet.PingRecord {
			return tailnet.PingRecord{At: now.Add(at), Result: &res}
		}
		r.ObservePings(tailnet.Snapshot{Status: status, Pings: map[string]tailnet.PingRecord{
			"laptop": reply(0, tailnet.PingResult{Latency: 4 * time.Millisecond, Endpoint: "192.0.2.1:41641"}),
			"phone":  reply(0, tailnet.PingResult{Latency: 80 * time.Millisecond, DERPRegion: "fra"}),
			"server": reply(0, tailnet.PingResult{Latency: 20 * time.Millisecond, PeerRelay: "192.0.2.9:7777:1"}),
		}})
		r.ObservePings(tailnet.Snapshot{Status: status, Pings: map[string]tailnet.PingRecord{
			// The same ping again doesn't count twice.
			"laptop": reply(0, tailnet.PingResult{Latency: 4 * time.Millisecond}),
			"phone":  {At: now.Add(time.Minute), Err: tailnet.ErrNoReply, Misses: 3},
			// A ping that failed on Roost's side says nothing of the peer.
			"server": {At: now.Add(time.Minute), Err: errors.New("the node isn't running")},
		}})
		want := map[string]struct {
			details []string
			misses  int
		}{
			"laptop": {[]string{"Direct connection"}, 0},
			"phone":  {[]string{"Relayed through DERP fra", "No reply within 5s"}, 3},
			"server": {[]string{"Relayed through a peer relay"}, 0},
		}
		got := r.Pings()
		for id, w := range want {
			var details []string
			for _, res := range got[id].Recent {
				details = append(details, res.Detail)
			}
			if !slices.Equal(details, w.details) || got[id].Misses != w.misses {
				t.Errorf("%s's pings = %q with %d misses, want %q with %d", id, details, got[id].Misses, w.details, w.misses)
			}
		}
		if l := got["laptop"].Recent[0]; !l.OK || l.Latency != 4*time.Millisecond || !l.At.Equal(now) {
			t.Errorf("the laptop's ping = %+v, want a 4 ms reply", l)
		}

		// Right after Roost starts, the status can have no peers yet, which
		// doesn't mean they left. A device whose latest ping is over a day
		// old is forgotten.
		r.ObservePings(tailnet.Snapshot{Status: &tailnet.Status{}})
		if got := r.Pings(); len(got) != 3 {
			t.Errorf("with no peers in the status, Pings() = %+v, want all three peers' pings", got)
		}
		// The server's latest ping is now over a day old, and the phone's
		// isn't yet.
		later := checks.UptimeSpan + 30*time.Second
		time.Sleep(later)
		r.ObservePings(tailnet.Snapshot{Pings: map[string]tailnet.PingRecord{
			"laptop": reply(later, tailnet.PingResult{Latency: 5 * time.Millisecond}),
		}})
		if got := r.Pings(); len(got) != 2 || len(got["laptop"].Recent) != 2 || len(got["phone"].Recent) != 2 {
			t.Errorf("a day later, Pings() = %+v, want the laptop's and phone's", got)
		}
	})
}

// The runner counts each device's pings over the last day, a step of 5
// minutes at a time, for its uptime.
func TestRunner_pingUptime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRunner(t, newStore(t), newFakeNet())
		uptime := func() any {
			if u := r.Pings()["laptop"].Uptime; u != nil {
				return *u
			}
			return nil
		}
		ping := func(replied bool) {
			rec := tailnet.PingRecord{At: time.Now(), Err: tailnet.ErrNoReply}
			if replied {
				rec = tailnet.PingRecord{At: time.Now(), Result: &tailnet.PingResult{Latency: time.Millisecond}}
			}
			r.ObservePings(tailnet.Snapshot{Pings: map[string]tailnet.PingRecord{"laptop": rec}})
		}

		for _, replied := range []bool{true, true, true, false} {
			ping(replied)
			time.Sleep(time.Minute)
		}
		if got := uptime(); got != 75.0 {
			t.Errorf("after 4 pings, 3 answered, the uptime = %v, want 75", got)
		}
		// A day after the first ping, its step counts this one instead.
		time.Sleep(checks.UptimeSpan - 4*time.Minute)
		ping(false)
		if got := uptime(); got != 0.0 {
			t.Errorf("a day later, after a missed ping, the uptime = %v, want 0", got)
		}
		time.Sleep(checks.UptimeSpan + 10*time.Minute)
		if got := uptime(); got != nil {
			t.Errorf("after a day without pings, the uptime = %v, want none", got)
		}
	})
}

func TestClean(t *testing.T) {
	t.Parallel()
	valid := store.Check{Name: "API", Kind: checks.KindHTTP, Target: "https://api.example.com/health", Group: "Apps", Interval: time.Minute, Timeout: 5 * time.Second}
	with := func(f func(c *store.Check)) store.Check {
		c := valid
		f(&c)
		return c
	}
	tests := []struct {
		name    string
		in      store.Check
		want    store.Check
		wantErr string
	}{
		{name: "valid", in: valid, want: valid},
		{
			name: "defaults",
			in:   store.Check{Name: " DB ", Kind: checks.KindTCP, Target: " db:5432 "},
			want: store.Check{Name: "DB", Kind: checks.KindTCP, Target: "db:5432", Group: "Custom", Interval: time.Minute, Timeout: 5 * time.Second},
		},
		{
			name: "only HTTP checks expect a status or skip the certificate",
			in:   store.Check{Name: "DNS", Kind: checks.KindDNS, Target: "example.com.", ExpectStatus: 200, Insecure: true},
			want: store.Check{Name: "DNS", Kind: checks.KindDNS, Target: "example.com.", Group: "Custom", Interval: time.Minute, Timeout: 5 * time.Second},
		},
		{name: "an HTTP check's options", in: with(func(c *store.Check) { c.ExpectStatus, c.Insecure = 204, true }),
			want: with(func(c *store.Check) { c.ExpectStatus, c.Insecure = 204, true })},
		{name: "an IPv6 target", in: with(func(c *store.Check) { c.Kind, c.Target = checks.KindTCP, "[2001:db8::1]:443" }),
			want: with(func(c *store.Check) { c.Kind, c.Target = checks.KindTCP, "[2001:db8::1]:443" })},
		{name: "an IP in a URL", in: with(func(c *store.Check) { c.Target = "http://127.0.0.1:8080/healthz" }),
			want: with(func(c *store.Check) { c.Target = "http://127.0.0.1:8080/healthz" })},
		{name: "no name", in: with(func(c *store.Check) { c.Name = "  " }), wantErr: "the check needs a name"},
		{name: "a long name", in: with(func(c *store.Check) { c.Name = strings.Repeat("x", 81) }), wantErr: "the name is too long"},
		{name: "a name of two lines", in: with(func(c *store.Check) { c.Name = "a\nb" }), wantErr: "the name can only be one line"},
		{name: "a long group", in: with(func(c *store.Check) { c.Group = strings.Repeat("x", 41) }), wantErr: "the group is too long"},
		{name: "an unknown kind", in: with(func(c *store.Check) { c.Kind = "ping" }), wantErr: "the check must be an HTTP, TCP or DNS check"},
		{name: "no target", in: with(func(c *store.Check) { c.Target = "" }), wantErr: "the check needs a target"},
		{name: "a short interval", in: with(func(c *store.Check) { c.Interval = 5 * time.Second; c.Timeout = time.Second }),
			wantErr: "the interval must be from 10 seconds to 24 hours"},
		{name: "a long interval", in: with(func(c *store.Check) { c.Interval = 25 * time.Hour }),
			wantErr: "the interval must be from 10 seconds to 24 hours"},
		{name: "a short timeout", in: with(func(c *store.Check) { c.Timeout = 100 * time.Millisecond }),
			wantErr: "the timeout must be from 200 ms to 60 seconds"},
		{name: "a timeout as long as the interval", in: with(func(c *store.Check) { c.Interval, c.Timeout = 10*time.Second, 10*time.Second }),
			wantErr: "the timeout must be shorter than the interval"},
		{name: "a URL without a scheme", in: with(func(c *store.Check) { c.Target = "api.example.com/health" }),
			wantErr: "the target must be a full http or https URL, such as https://example.com/health"},
		{name: "an FTP URL", in: with(func(c *store.Check) { c.Target = "ftp://files.example.com/" }),
			wantErr: "the target must be a full http or https URL, such as https://example.com/health"},
		{name: "a URL with a space", in: with(func(c *store.Check) { c.Target = "https://api.example.com/a b" }),
			wantErr: "the target must be a full http or https URL, such as https://example.com/health"},
		{name: "a URL with a bad port", in: with(func(c *store.Check) { c.Target = "https://api.example.com:99999/" }),
			wantErr: "the URL's port must be a number from 1 to 65535"},
		{name: "a URL with a bad host", in: with(func(c *store.Check) { c.Target = "https://api_.example!.com/" }),
			wantErr: "the URL's host isn't a host name or an IP address"},
		{name: "an expected status out of range", in: with(func(c *store.Check) { c.ExpectStatus = 600 }),
			wantErr: "the expected status must be from 100 to 599"},
		{name: "a TCP target without a port", in: with(func(c *store.Check) { c.Kind, c.Target = checks.KindTCP, "db.example.com" }),
			wantErr: "the target must be a host and a port, such as db.example.com:5432"},
		{name: "a TCP target with a named port", in: with(func(c *store.Check) { c.Kind, c.Target = checks.KindTCP, "db.example.com:postgres" }),
			wantErr: "the port must be a number from 1 to 65535"},
		{name: "a TCP target with port 0", in: with(func(c *store.Check) { c.Kind, c.Target = checks.KindTCP, "db.example.com:0" }),
			wantErr: "the port must be a number from 1 to 65535"},
		{name: "a TCP target with a bad host", in: with(func(c *store.Check) { c.Kind, c.Target = checks.KindTCP, "-db-:5432" }),
			wantErr: "the target's host isn't a host name or an IP address"},
		{name: "a DNS target that's a URL", in: with(func(c *store.Check) { c.Kind, c.Target = checks.KindDNS, "https://example.com" }),
			wantErr: "the target must be a host name, such as example.com"},
		{name: "a DNS target that's an address", in: with(func(c *store.Check) { c.Kind, c.Target = checks.KindDNS, "192.0.2.1" }),
			wantErr: "the target must be a host name, such as example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := checks.Clean(tt.in)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Errorf("Clean() error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Clean() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Clean() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

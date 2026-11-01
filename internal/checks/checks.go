// Package checks runs the checks that admins set up, which probe services
// from the hub with an HTTP request, a TCP connection or a DNS lookup, and
// keeps their results. It also keeps the latest pings of each peer, which
// the tailnet package sends, so that each peer can be shown as a check too.
package checks

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

const (
	// MaxRunning is how many checks run at once. A check that's due waits
	// for one to finish.
	MaxRunning = 6
	// RecentResults is how many of the latest results of each check, and
	// pings of each peer, Runner keeps.
	RecentResults = 30
	// UptimeSpan is the time an uptime covers.
	UptimeSpan = 24 * time.Hour
	// startSpread is the time between the checks' first runs, so they don't
	// all run as Bield starts.
	startSpread = 400 * time.Millisecond
	// failingAfter is how many results in a row must fail before Bield logs
	// that a check is failing.
	failingAfter = 2
)

// ErrNotFound means no check has the ID.
var ErrNotFound = errors.New("no check has this ID")

// errStopped is the error of a run that Runner.Run stopped before it ended.
var errStopped = errors.New("bield is shutting down")

// Options configures NewRunner.
type Options struct {
	// Dial connects to the targets of HTTP and TCP checks. Bield passes its
	// node's, which reaches the tailnet as well as everything else.
	// Required.
	Dial DialFunc
	// Lookup resolves the host names of DNS checks. Nil asks the name
	// servers in /etc/resolv.conf, through Dial.
	Lookup LookupFunc
	// Logger receives errors and checks that start or stop failing. Nil
	// discards them.
	Logger *slog.Logger
}

// Runner runs the checks in the store, each every so often and at most
// MaxRunning at once, and keeps their latest results. It also keeps the
// latest pings of each peer. It's safe for concurrent use.
type Runner struct {
	st      *store.Store
	probe   prober
	log     *slog.Logger
	changes chan struct{}
	// wake tells Run to look again for checks that are due.
	wake chan struct{}
	// slots holds a value for each check that's running.
	slots chan struct{}
	// writeMu is held while a check is added, changed or deleted, so the
	// store and the runner change in the same order.
	writeMu sync.Mutex

	mu      sync.Mutex
	checks  map[string]*entry
	pings   map[string]*pinged
	stopped bool
	// recordErr is the latest error from storing a result, so it's logged
	// once until storing works again.
	recordErr error
}

// An entry is a check and what the runner knows of it.
type entry struct {
	check store.Check
	// rev counts the changes to the check, so the result of a run that
	// started before one doesn't count.
	rev int
	// next is when the check is due.
	next time.Time
	// run is the run in progress, if there is one, and pending the run an
	// admin asked for, if it hasn't started.
	run, pending *run
	recent       []Result
	failures     int
	// tally counts its results over the last UptimeSpan.
	tally tally
}

// A run is one run of a check, which callers can wait for.
type run struct {
	rev  int
	done chan struct{}
	// result and err are set before done is closed.
	result Result
	err    error
}

func newRun() *run { return &run{done: make(chan struct{})} }

// end sets the run's result or error, and lets those waiting for it go.
func (r *run) end(res Result, err error) {
	r.result, r.err = res, err
	close(r.done)
}

// pinged is what the runner knows of a peer's pings.
type pinged struct {
	// last is when the latest ping the runner kept was sent.
	last   time.Time
	recent []Result
	misses int
	// tally counts its pings over the last UptimeSpan.
	tally tally
}

// NewRunner returns a Runner for the checks in st. Call Load before Run.
// It panics if opts lacks a required field.
func NewRunner(st *store.Store, opts Options) *Runner {
	if st == nil || opts.Dial == nil {
		panic("checks.NewRunner: the store and Options.Dial are required")
	}
	r := &Runner{
		st:      st,
		probe:   prober{dial: opts.Dial, lookup: opts.Lookup},
		log:     opts.Logger,
		changes: make(chan struct{}, 1),
		wake:    make(chan struct{}, 1),
		slots:   make(chan struct{}, MaxRunning),
		checks:  map[string]*entry{},
		pings:   map[string]*pinged{},
	}
	if r.probe.lookup == nil {
		r.probe.lookup = resolverLookup(opts.Dial)
	}
	if r.log == nil {
		r.log = slog.New(slog.DiscardHandler)
	}
	return r
}

// Load reads the checks from the store, with their latest results and the
// latest pings of each peer, and counts their results over the last
// UptimeSpan. From then on, the runner counts them as they come. It fails
// only if it can't read the checks.
func (r *Runner) Load(ctx context.Context) error {
	defs, err := r.st.Checks(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	since := now.Add(-UptimeSpan)
	results, err := r.st.RecentCheckResults(ctx, since, RecentResults)
	if err != nil {
		r.log.Warn("read the checks' latest results", "error", err)
	}
	pings, err := r.st.RecentPings(ctx, since, RecentResults)
	if err != nil {
		r.log.Warn("read the latest pings", "error", err)
	}
	checkTallies, pingTallies, err := r.st.Tallies(ctx, since, tallyStep)
	if err != nil {
		r.log.Warn("count the checks' results", "error", err)
	}

	r.mu.Lock()
	for i, c := range defs {
		e := &entry{check: c, next: now.Add(time.Duration(i) * startSpread)}
		for _, res := range results[c.ID] {
			e.add(Result{At: res.At, OK: res.OK, Latency: res.Latency, Detail: res.Detail})
		}
		r.checks[c.ID] = e
	}
	for id, ps := range pings {
		p := &pinged{}
		for _, ping := range ps {
			res := Result{At: ping.At, OK: ping.Replied, Latency: ping.RTT, Detail: "No reply"}
			if ping.Replied {
				res.Detail = "Replied"
			}
			p.add(res)
		}
		r.pings[id] = p
	}
	for _, t := range checkTallies {
		if e := r.checks[t.ID]; e != nil {
			e.tally.add(t.Start, t.Total, t.OK)
		}
	}
	for _, t := range pingTallies {
		p := r.pings[t.ID]
		if p == nil {
			p = &pinged{}
			r.pings[t.ID] = p
		}
		p.tally.add(t.Start, t.Total, t.OK)
	}
	r.mu.Unlock()
	return nil
}

// add adds a result to e's latest, and counts its failures.
func (e *entry) add(res Result) {
	e.recent = appendResult(e.recent, res)
	if res.OK {
		e.failures = 0
	} else {
		e.failures++
	}
}

// add adds a result to p's latest, and counts its misses.
func (p *pinged) add(res Result) {
	p.recent, p.last = appendResult(p.recent, res), res.At
	if res.OK {
		p.misses = 0
	} else {
		p.misses++
	}
}

// appendResult returns recent with res after it, keeping the latest
// RecentResults. It never changes recent's array, so a caller can keep the
// old slice.
func appendResult(recent []Result, res Result) []Result {
	keep := recent[max(0, len(recent)-RecentResults+1):]
	return append(slices.Clip(keep), res)
}

// Changes receives a value when a check is added, changed or deleted, or
// gets a result. Only one goroutine should receive from it.
func (r *Runner) Changes() <-chan struct{} { return r.changes }

func (r *Runner) changed() {
	select {
	case r.changes <- struct{}{}:
	default:
	}
}

func (r *Runner) poke() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run runs each check when it's due, until ctx ends. When it returns, no
// check is running.
func (r *Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
		r.mu.Lock()
		defer r.mu.Unlock()
		r.stopped = true
		for _, e := range r.checks {
			if e.pending != nil {
				e.pending.end(Result{}, errStopped)
				e.pending = nil
			}
		}
	}()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		timer.Reset(r.startDue(ctx, &wg))
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		case <-timer.C:
		}
	}
}

// startDue starts the checks that are due, as many as there are free slots,
// and returns how long until the next check that's waiting is due. A run
// that ends frees its slot and wakes Run.
func (r *Runner) startDue(ctx context.Context, wg *sync.WaitGroup) time.Duration {
	now := time.Now()
	wait := time.Hour
	r.mu.Lock()
	defer r.mu.Unlock()
	var due []*entry
	for _, e := range r.checks {
		switch {
		case e.run != nil:
		case e.pending != nil, !e.check.Paused && !now.Before(e.next):
			due = append(due, e)
		case !e.check.Paused:
			wait = min(wait, e.next.Sub(now))
		}
	}
	// Runs an admin asked for go first, then the checks due longest.
	slices.SortFunc(due, func(a, b *entry) int {
		if (a.pending != nil) != (b.pending != nil) {
			if a.pending != nil {
				return -1
			}
			return 1
		}
		return cmp.Or(a.next.Compare(b.next), cmp.Compare(a.check.ID, b.check.ID))
	})
	for _, e := range due {
		select {
		case r.slots <- struct{}{}:
		default:
			return wait
		}
		run := e.pending
		if run == nil {
			run = newRun()
		}
		run.rev, e.run, e.pending = e.rev, run, nil
		e.next = now.Add(e.check.Interval)
		c := e.check
		wg.Go(func() { r.execute(ctx, c, run) })
	}
	return wait
}

// execute runs a check, keeps the result, frees its slot, and then stores
// the result.
func (r *Runner) execute(ctx context.Context, c store.Check, run *run) {
	res := r.probe.run(ctx, c)
	current, was, failures := r.finish(ctx, c.ID, run, res)
	// The check isn't running now, so Run can start it again.
	<-r.slots
	r.poke()
	if !current {
		return
	}
	r.changed()
	switch {
	case failures == failingAfter:
		r.log.Warn("a check is failing", "check", c.Name, "id", c.ID, "detail", res.Detail)
	case res.OK && was >= failingAfter:
		r.log.Info("a check passes again", "check", c.Name, "id", c.ID)
	}
	r.record(ctx, store.CheckResult{CheckID: c.ID, At: res.At, OK: res.OK, Latency: res.Latency, Detail: res.Detail})
}

// finish ends a run of the check with an ID, and keeps its result if it
// counts. It reports whether it does, and how many results in a row failed
// before and after it.
func (r *Runner) finish(ctx context.Context, id string, run *run, res Result) (current bool, was, failures int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.checks[id]
	if ok && e.run == run {
		e.run = nil
	}
	// A run that Bield stopped as it shut down says nothing of the target,
	// and one that started before an admin changed the check isn't of the
	// check as it is.
	if ctx.Err() != nil {
		run.end(Result{}, errStopped)
		return false, 0, 0
	}
	run.end(res, nil)
	if !ok || e.rev != run.rev {
		return false, 0, 0
	}
	was = e.failures
	e.add(res)
	e.tally.count(res)
	return true, was, e.failures
}

// record stores a check's result. The check may have been deleted since it
// ran, and then its result goes with it.
func (r *Runner) record(ctx context.Context, res store.CheckResult) {
	err := r.st.AddCheckResult(ctx, res)
	if errors.Is(err, store.ErrNoCheck) {
		err = nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case err != nil && r.recordErr == nil && ctx.Err() == nil:
		r.log.Warn("record a check's result", "error", err)
	case err == nil && r.recordErr != nil:
		r.log.Info("recorded a check's result again")
	}
	r.recordErr = err
}

// RunNow runs a check as soon as a slot is free, even if it's paused, and
// returns its result. If the check is running already, it waits for that
// run instead. It returns ErrNotFound if there's no such check, or if it's
// deleted before it runs.
func (r *Runner) RunNow(ctx context.Context, id string) (Result, error) {
	r.mu.Lock()
	e, ok := r.checks[id]
	if !ok {
		r.mu.Unlock()
		return Result{}, ErrNotFound
	}
	if r.stopped {
		r.mu.Unlock()
		return Result{}, errStopped
	}
	run := e.run
	if run == nil || run.rev != e.rev {
		if e.pending == nil {
			e.pending = newRun()
		}
		run = e.pending
	}
	r.mu.Unlock()
	r.poke()
	select {
	case <-run.done:
		return run.result, run.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// Add saves a new check, as Clean returns it, and runs it soon unless it's
// paused. It returns the check with its ID.
func (r *Runner) Add(ctx context.Context, c store.Check) (store.Check, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	c.Created = time.Now()
	c, err := r.st.AddCheck(ctx, c)
	if err != nil {
		return store.Check{}, err
	}
	r.mu.Lock()
	r.checks[c.ID] = &entry{check: c}
	r.mu.Unlock()
	r.changed()
	r.poke()
	return c, nil
}

// Change replaces the check with c's ID, as Clean returns it, and runs it
// again soon unless it's paused. A run that started before doesn't count.
// It returns the check as it is now, or ErrNotFound.
func (r *Runner) Change(ctx context.Context, c store.Check) (store.Check, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	r.mu.Lock()
	e, ok := r.checks[c.ID]
	if ok {
		c.Created = e.check.Created
	}
	r.mu.Unlock()
	if !ok {
		return store.Check{}, ErrNotFound
	}
	if err := r.st.UpdateCheck(ctx, c); err != nil {
		if errors.Is(err, store.ErrNoCheck) {
			err = ErrNotFound
		}
		return store.Check{}, err
	}
	r.mu.Lock()
	e.check, e.next = c, time.Time{}
	e.rev++
	r.mu.Unlock()
	r.changed()
	r.poke()
	return c, nil
}

// Delete deletes a check and its results. A run of it that's waiting to
// start fails with ErrNotFound. It returns ErrNotFound if there's no such
// check.
func (r *Runner) Delete(ctx context.Context, id string) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	r.mu.Lock()
	_, ok := r.checks[id]
	r.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	if err := r.st.DeleteCheck(ctx, id); err != nil && !errors.Is(err, store.ErrNoCheck) {
		return err
	}
	r.mu.Lock()
	if e := r.checks[id]; e.pending != nil {
		e.pending.end(Result{}, ErrNotFound)
	}
	delete(r.checks, id)
	r.mu.Unlock()
	r.changed()
	return nil
}

// A Status is what the runner knows of a check.
type Status struct {
	Check store.Check
	// Recent are its latest results, oldest first, up to RecentResults.
	Recent []Result
	// Failures is how many of its latest results failed in a row.
	Failures int
	// Uptime is the percentage of its results over the last UptimeSpan that
	// passed, or nil if it has none.
	Uptime *float64
}

// Checks returns what the runner knows of each check, by group and name.
func (r *Runner) Checks() []Status {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Status, 0, len(r.checks))
	for _, e := range r.checks {
		out = append(out, e.status(now))
	}
	slices.SortFunc(out, func(a, b Status) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Check.Group), strings.ToLower(b.Check.Group)),
			cmp.Compare(strings.ToLower(a.Check.Name), strings.ToLower(b.Check.Name)),
			cmp.Compare(a.Check.ID, b.Check.ID),
		)
	})
	return out
}

// Check returns what the runner knows of the check with an ID.
func (r *Runner) Check(id string) (Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.checks[id]
	if !ok {
		return Status{}, false
	}
	return e.status(time.Now()), true
}

func (e *entry) status(now time.Time) Status {
	return Status{Check: e.check, Recent: e.recent, Failures: e.failures, Uptime: e.tally.uptime(now)}
}

// PingStatus is what the runner knows of a peer's pings.
type PingStatus struct {
	// Recent are its latest pings, oldest first, up to RecentResults.
	Recent []Result
	// Misses is how many of its latest pings went unanswered in a row.
	Misses int
	// Uptime is the percentage of its pings over the last UptimeSpan that
	// got a reply, or nil if it has none.
	Uptime *float64
}

// Pings returns what the runner knows of each peer's pings, by device ID.
func (r *Runner) Pings() map[string]PingStatus {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]PingStatus, len(r.pings))
	for id, p := range r.pings {
		out[id] = PingStatus{Recent: p.recent, Misses: p.misses, Uptime: p.tally.uptime(now)}
	}
	return out
}

// ObservePings keeps the pings in snap that are newer than the ones it has,
// and forgets a device's pings once its latest is more than UptimeSpan old,
// such as after it left the tailnet. A ping that failed on Bield's side says
// nothing about the peer, so it's left out.
func (r *Runner) ObservePings(snap tailnet.Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, rec := range snap.Pings {
		if rec.Result == nil && !errors.Is(rec.Err, tailnet.ErrNoReply) {
			continue
		}
		p := r.pings[id]
		if p == nil {
			p = &pinged{}
			r.pings[id] = p
		}
		if !rec.At.After(p.last) {
			continue
		}
		res := pingResult(rec)
		p.add(res)
		p.tally.count(res)
		// The poller counts the misses, and starts again while the peer is
		// offline.
		p.misses = rec.Misses
	}
	// The peers can't say which devices left: right after Bield starts,
	// the tailnet's status can have none yet.
	old := time.Now().Add(-UptimeSpan)
	maps.DeleteFunc(r.pings, func(_ string, p *pinged) bool { return p.last.Before(old) })
}

// pingResult returns a ping as the result of a check.
func pingResult(rec tailnet.PingRecord) Result {
	res := rec.Result
	if res == nil {
		return Result{At: rec.At, Detail: "No reply within " + shortDuration(tailnet.PingTimeout)}
	}
	out := Result{At: rec.At, OK: true, Latency: res.Latency, Detail: "Direct connection"}
	switch {
	case res.DERPRegion != "":
		out.Detail = "Relayed through DERP " + res.DERPRegion
	case res.PeerRelay != "":
		out.Detail = "Relayed through a peer relay"
	}
	return out
}

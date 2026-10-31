// Package systemd watches systemd units on the machine Roost runs on: the
// ones an admin watches, and every unit that failed, both the system's and
// those of the user Roost runs as.
package systemd

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Interval is how often a Collector asks systemd.
const Interval = 30 * time.Second

// timeout is how long a run of systemctl can take.
const timeout = 15 * time.Second

// Scope says whose units they are.
type Scope string

// The scopes of units.
const (
	// System units run for the whole machine.
	System Scope = "system"
	// User units run for the user Roost runs as.
	User Scope = "user"
)

// ValidName reports whether name can be the name of a unit that Roost asks
// systemd about, such as "ssh.service" or "getty@tty1.service": systemd's own
// characters, and not a template such as "getty@.service". systemctl fails
// for the whole list if one name isn't valid.
func ValidName(name string) bool {
	if name == "" || len(name) > 255 || name[0] == '-' || strings.Contains(name, "@.") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune(`:_.\@-`, r):
		default:
			return false
		}
	}
	return true
}

// userPrefix marks a watched unit as a user unit, as in "user:app.service".
const userPrefix = "user:"

// A Unit is a systemd unit, as systemctl shows it.
type Unit struct {
	// Name is the unit's ID, such as "ssh.service".
	Name        string
	Scope       Scope
	Description string
	// Active is systemd's ActiveState, such as "active", "inactive" or
	// "failed".
	Active string
	// Sub is its SubState, such as "running" or "exited".
	Sub string
	// Since is when it entered its active state, if systemd says.
	Since time.Time
	// MainPID is its main process, or 0.
	MainPID int
	// Memory is how much memory it uses, in bytes, or 0 if systemd doesn't
	// count it.
	Memory uint64
	// Watched reports whether an admin watches the unit. A unit that isn't
	// watched is only listed while it's failed.
	Watched bool
}

// Failed reports whether the unit failed.
func (u Unit) Failed() bool { return u.Active == "failed" }

// Key names the unit with its scope, such as "user:app.service".
func (u Unit) Key() string {
	if u.Scope == User {
		return userPrefix + u.Name
	}
	return u.Name
}

// A Reading is what a Collector last learned.
type Reading struct {
	// Units are the watched units the machine has, and the failed units,
	// the system's first, by name. It's nil before the first run that
	// worked.
	Units []Unit
	// At is when a run last worked, or zero.
	At time.Time
	// Err is why the latest run failed, if it did. Units are then from
	// the last run that worked, for each scope.
	Err error
}

// A RunFunc runs systemctl with args and returns what it printed.
type RunFunc func(ctx context.Context, args ...string) ([]byte, error)

// Options configures NewCollector.
type Options struct {
	// Run runs systemctl. Nil runs the one on the PATH.
	Run RunFunc
	// Watched returns the units to watch, such as "ssh.service" and
	// "user:app.service". Nil watches none.
	Watched func() []string
	// UserScope says whether to ask about the user's units too. Use
	// HasUserScope.
	UserScope bool
	Logger    *slog.Logger
}

// Available reports whether this machine runs systemd, as far as Roost can
// tell.
func Available() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	_, err := os.Stat("/run/systemd/system")
	return err == nil
}

// HasUserScope reports whether the user Roost runs as has a systemd user
// session to ask about. Root's isn't asked about, and neither is a system
// account's without one.
func HasUserScope() bool {
	if os.Geteuid() == 0 {
		return false
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "bus")) //nolint:gosec // G703: it's Roost's own environment.
	return err == nil
}

// A Collector asks systemd about the watched and failed units every
// Interval, and keeps the answer. It's safe for concurrent use.
type Collector struct {
	run       RunFunc
	watched   func() []string
	userScope bool
	log       *slog.Logger
	changes   chan struct{}
	// refresh asks Run to ask systemd now.
	refresh chan struct{}

	mu      sync.Mutex
	reading Reading
	// byScope holds the units of each scope from its last run that worked.
	byScope map[Scope][]Unit
}

// NewCollector returns a Collector. Nothing is asked until Run.
func NewCollector(opts Options) *Collector {
	c := &Collector{
		run:       opts.Run,
		watched:   opts.Watched,
		userScope: opts.UserScope,
		log:       opts.Logger,
		changes:   make(chan struct{}, 1),
		refresh:   make(chan struct{}, 1),
		byScope:   map[Scope][]Unit{},
	}
	if c.run == nil {
		c.run = systemctl
	}
	if c.watched == nil {
		c.watched = func() []string { return nil }
	}
	if c.log == nil {
		c.log = slog.New(slog.DiscardHandler)
	}
	return c
}

// Run asks systemd now and then every Interval, until ctx ends.
func (c *Collector) Run(ctx context.Context) {
	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		c.collect(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-c.refresh:
		}
	}
}

// Refresh makes Run ask systemd soon, such as after the watched units
// change, instead of when its interval ends.
func (c *Collector) Refresh() {
	select {
	case c.refresh <- struct{}{}:
	default:
	}
}

// Reading returns what the collector knows now.
func (c *Collector) Reading() Reading {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.reading
	r.Units = slices.Clone(r.Units)
	return r
}

// Changes receives a value after each run, whether or not it worked. Only
// one goroutine should receive from it.
func (c *Collector) Changes() <-chan struct{} { return c.changes }

func (c *Collector) collect(ctx context.Context) {
	watched := map[Scope][]string{}
	for _, w := range c.watched() {
		// A name that isn't valid fails every name asked about with it, so one
		// that got into the settings, such as by editing the database, is left
		// out.
		if !ValidName(strings.TrimPrefix(w, userPrefix)) {
			continue
		}
		if name, ok := strings.CutPrefix(w, userPrefix); ok {
			watched[User] = append(watched[User], name)
		} else {
			watched[System] = append(watched[System], w)
		}
	}
	scopes := []Scope{System}
	if c.userScope {
		scopes = append(scopes, User)
	}
	var errs []error
	fresh := map[Scope][]Unit{}
	for _, scope := range scopes {
		units, err := c.scope(ctx, scope, watched[scope])
		if err != nil {
			errs = append(errs, err)
			continue
		}
		fresh[scope] = units
	}
	if !c.userScope && len(watched[User]) > 0 {
		errs = append(errs, errors.New("can't watch user units without a systemd user session"))
	}
	if ctx.Err() != nil {
		return
	}
	err := errors.Join(errs...)

	c.mu.Lock()
	maps.Copy(c.byScope, fresh)
	switch {
	case err != nil && c.reading.Err == nil:
		c.log.Warn("ask systemd about units", "error", err)
	case err == nil && c.reading.Err != nil:
		c.log.Info("asked systemd about units again")
	}
	c.reading.Err = err
	if len(fresh) > 0 {
		var all []Unit
		for _, scope := range []Scope{System, User} {
			all = append(all, c.byScope[scope]...)
		}
		c.reading.Units = all
		if c.reading.Units == nil {
			c.reading.Units = []Unit{}
		}
		if err == nil {
			c.reading.At = time.Now()
		}
	}
	c.mu.Unlock()
	select {
	case c.changes <- struct{}{}:
	default:
	}
}

// scope returns the watched units of a scope that exist, and its failed
// units, by name.
func (c *Collector) scope(ctx context.Context, scope Scope, watched []string) ([]Unit, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var flags []string
	if scope == User {
		flags = []string{"--user"}
	}
	out, err := c.run(ctx, append(flags, "list-units", "--failed", "--plain", "--no-legend", "--no-pager", "--full")...)
	if err != nil {
		return nil, fmt.Errorf("list the %s's failed units: %w", scopeName(scope), err)
	}
	failed := failedNames(out)
	names := slices.Compact(slices.Sorted(slices.Values(append(slices.Clone(watched), failed...))))
	if len(names) == 0 {
		return []Unit{}, nil
	}
	out, err = c.run(ctx, append(append(flags, "show", "--no-pager", "--timestamp=unix",
		"--property=Id,LoadState,Description,ActiveState,SubState,ActiveEnterTimestamp,MainPID,MemoryCurrent", "--"), names...)...)
	if err != nil {
		return nil, fmt.Errorf("show the %s's units: %w", scopeName(scope), err)
	}
	isWatched := map[string]bool{}
	for _, w := range watched {
		isWatched[w] = true
	}
	var units []Unit
	seen := map[string]bool{}
	for i, props := range showBlocks(out) {
		// systemctl shows the units in the order it was given them.
		asked := ""
		if i < len(names) {
			asked = names[i]
		}
		u, ok := parseUnit(props, scope)
		if !ok || seen[u.Name] {
			continue
		}
		seen[u.Name] = true
		// An alias, such as sshd.service for ssh.service, counts as the unit.
		u.Watched = isWatched[u.Name] || isWatched[asked]
		if u.Watched || u.Failed() {
			units = append(units, u)
		}
	}
	slices.SortFunc(units, func(a, b Unit) int { return cmp.Compare(a.Name, b.Name) })
	if units == nil {
		units = []Unit{}
	}
	return units, nil
}

func scopeName(s Scope) string {
	if s == User {
		return "user"
	}
	return "system"
}

// failedNames reads the unit names from `systemctl list-units --plain
// --no-legend`, one unit a line, its name first.
func failedNames(out []byte) []string {
	var names []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if fields := strings.Fields(sc.Text()); len(fields) > 0 {
			names = append(names, strings.TrimPrefix(fields[0], "●"))
		}
	}
	return slices.DeleteFunc(names, func(n string) bool { return n == "" })
}

// showBlocks splits the output of `systemctl show` into the properties of
// each unit, which it separates with blank lines.
func showBlocks(out []byte) []map[string]string {
	var (
		blocks []map[string]string
		cur    map[string]string
	)
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			if cur != nil {
				blocks = append(blocks, cur)
				cur = nil
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if cur == nil {
			cur = map[string]string{}
		}
		cur[k] = v
	}
	if cur != nil {
		blocks = append(blocks, cur)
	}
	return blocks
}

// parseUnit reads a unit from its properties. It returns false for a unit
// the machine doesn't have.
func parseUnit(p map[string]string, scope Scope) (Unit, bool) {
	if p["Id"] == "" || p["LoadState"] == "not-found" {
		return Unit{}, false
	}
	u := Unit{
		Name:        p["Id"],
		Scope:       scope,
		Description: p["Description"],
		Active:      cmp.Or(p["ActiveState"], "unknown"),
		Sub:         p["SubState"],
	}
	if secs, err := strconv.ParseInt(strings.TrimPrefix(p["ActiveEnterTimestamp"], "@"), 10, 64); err == nil && secs > 0 {
		u.Since = time.Unix(secs, 0)
	}
	if pid, err := strconv.Atoi(p["MainPID"]); err == nil && pid > 0 {
		u.MainPID = pid
	}
	// systemd says [not set], or the largest uint64, when it doesn't count.
	if mem, err := strconv.ParseUint(p["MemoryCurrent"], 10, 64); err == nil && mem < 1<<60 {
		u.Memory = mem
	}
	return u, true
}

// systemctl runs the systemctl on the PATH, in English and without colors,
// so its output can be read.
func systemctl(ctx context.Context, args ...string) ([]byte, error) {
	// The arguments are flags and unit names, which settings.Check allows,
	// and no shell reads them.
	cmd := exec.CommandContext(ctx, "systemctl", args...) //nolint:gosec // G204: see above.
	cmd.Env = append(os.Environ(), "LC_ALL=C", "SYSTEMD_COLORS=0", "SYSTEMD_PAGER=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return out, nil
}

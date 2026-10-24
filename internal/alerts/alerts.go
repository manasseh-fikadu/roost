// Package alerts opens and resolves alerts from the conditions that rules
// find, such as a disk that's nearly full. A condition that appears opens an
// alert, and one that's gone resolves it, but only where its rule had fresh
// data to look at, and only once it's been gone for ResolveAfter, so a
// condition that comes and goes stays one alert. Admins can acknowledge,
// snooze and write notes on alerts.
package alerts

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/manasseh-fikadu/roost/internal/store"
)

const (
	// ResolveAfter is how long a condition must be gone before its alert
	// resolves.
	ResolveAfter = time.Minute
	// saveEvery is how often an open alert's title, detail and last sighting
	// are saved, if they changed. Its severity is saved at once.
	saveEvery = 5 * time.Minute
	// MaxSnooze is the longest an alert can be snoozed for.
	MaxSnooze = 30 * 24 * time.Hour
	// MaxNote is the longest a note can be, in characters.
	MaxNote = 4000
)

// ErrNotOpen means no open alert has the ID.
var ErrNotOpen = errors.New("no open alert has this ID")

// Severity says how much an alert matters.
type Severity string

// The severities of an alert, most first.
const (
	Critical Severity = "critical"
	Warning  Severity = "warning"
	Info     Severity = "info"
)

func (s Severity) rank() int {
	switch s {
	case Critical:
		return 0
	case Warning:
		return 1
	case Info:
	}
	return 2
}

// A Condition is something a rule found that needs attention.
type Condition struct {
	// Key names it, so it's the same each time a rule finds it, such as
	// "disk:hub".
	Key string
	// Source says what it's about, such as "metrics" or "check". With
	// DeviceID, it says whose data a rule must have to say it's gone.
	Source   string
	DeviceID string
	CheckID  string
	Severity Severity
	// Title and Detail say what's wrong, such as "Disk 91% full on laptop".
	// They can change while it lasts.
	Title, Detail string
}

// FreshFunc reports whether the rules looked at fresh data about a source,
// on a device, or with deviceID "", about the source as a whole. An open
// alert only resolves when its data was fresh.
type FreshFunc func(source, deviceID string) bool

// An Engine keeps the open alerts, and opens and resolves them as Apply is
// told the conditions. It's safe for concurrent use.
type Engine struct {
	st  *store.Store
	log *slog.Logger

	mu   sync.Mutex
	open map[string]*openAlert
	// saveErr is the latest error saving an alert, so it's logged once.
	saveErr error
}

type openAlert struct {
	alert store.Alert
	// missingSince is when its condition was first gone with fresh data,
	// or zero while it's there.
	missingSince time.Time
	// saved is when the alert was last saved.
	saved time.Time
}

// NewEngine returns an Engine for the alerts in st. Call Load before Apply.
func NewEngine(st *store.Store, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Engine{st: st, log: log, open: map[string]*openAlert{}}
}

// Load reads the open alerts from the store.
func (e *Engine) Load(ctx context.Context) error {
	open, err := e.st.OpenAlerts(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, a := range open {
		e.open[a.Key] = &openAlert{alert: a, saved: now}
	}
	return nil
}

// Apply opens an alert for each condition that has none, updates the open
// alerts of the others, and resolves the open alerts whose condition has
// been gone, with fresh data, for ResolveAfter. It reports whether an alert
// opened, resolved or changed severity.
func (e *Engine) Apply(ctx context.Context, conds []Condition, fresh FreshFunc, now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	var (
		events  []store.Event
		changed bool
		seen    = make(map[string]bool, len(conds))
	)
	for _, c := range conds {
		if seen[c.Key] {
			continue
		}
		seen[c.Key] = true
		if o := e.open[c.Key]; o != nil {
			o.missingSince = time.Time{}
			a := &o.alert
			escalated := a.Severity != string(c.Severity)
			a.Severity, a.Title, a.Detail, a.LastSeen = string(c.Severity), c.Title, c.Detail, now
			if escalated || now.Sub(o.saved) >= saveEvery {
				e.save(ctx, o, now)
			}
			changed = changed || escalated
			continue
		}
		a, err := e.st.AddAlert(ctx, store.Alert{
			Key: c.Key, Source: c.Source, DeviceID: c.DeviceID, CheckID: c.CheckID, Severity: string(c.Severity),
			Title: c.Title, Detail: c.Detail, OpenedAt: now, LastSeen: now,
		})
		if err != nil {
			if ctx.Err() == nil {
				e.log.Warn("open an alert", "key", c.Key, "error", err)
			}
			continue
		}
		e.open[c.Key] = &openAlert{alert: a, saved: now}
		changed = true
		level := "warning"
		if c.Severity == Critical {
			level = "error"
		}
		events = append(events, store.Event{At: now, Level: level, Kind: "alert", Message: c.Title, DeviceID: c.DeviceID})
		e.log.Info("opened an alert", "key", c.Key, "title", c.Title)
	}
	for key, o := range e.open {
		if seen[key] {
			continue
		}
		a := &o.alert
		if !fresh(a.Source, a.DeviceID) {
			// Without fresh data, Roost can't say it's gone.
			o.missingSince = time.Time{}
			continue
		}
		if o.missingSince.IsZero() {
			o.missingSince = now
		}
		if now.Sub(o.missingSince) < ResolveAfter {
			continue
		}
		a.ResolvedAt = o.missingSince
		if err := e.st.SaveAlert(ctx, *a); err != nil {
			if ctx.Err() == nil {
				e.log.Warn("resolve an alert", "key", key, "error", err)
			}
			a.ResolvedAt = time.Time{}
			continue
		}
		delete(e.open, key)
		changed = true
		events = append(events, store.Event{At: now, Level: "success", Kind: "alert", Message: "Resolved: " + a.Title, DeviceID: a.DeviceID})
		e.log.Info("resolved an alert", "key", key, "title", a.Title)
	}
	if err := e.st.AddEvents(ctx, events...); err != nil && ctx.Err() == nil {
		e.log.Warn("log alerts in the activity log", "error", err)
	}
	return changed
}

// save saves an open alert, and logs the first error of a run of them.
func (e *Engine) save(ctx context.Context, o *openAlert, now time.Time) {
	err := e.st.SaveAlert(ctx, o.alert)
	switch {
	case err != nil && e.saveErr == nil && ctx.Err() == nil:
		e.log.Warn("save an alert", "key", o.alert.Key, "error", err)
	case err == nil && e.saveErr != nil:
		e.log.Info("saved an alert again")
	}
	e.saveErr = err
	if err == nil {
		o.saved = now
	}
}

// Open returns the open alerts, the most severe first, and then the latest
// opened.
func (e *Engine) Open() []store.Alert {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]store.Alert, 0, len(e.open))
	for _, o := range e.open {
		a := o.alert
		a.Notes = slices.Clone(a.Notes)
		out = append(out, a)
	}
	slices.SortFunc(out, func(a, b store.Alert) int {
		return cmp.Or(
			cmp.Compare(Severity(a.Severity).rank(), Severity(b.Severity).rank()),
			b.OpenedAt.Compare(a.OpenedAt),
			cmp.Compare(b.ID, a.ID),
		)
	})
	return out
}

// byID returns the open alert with an ID, or nil. Call it with mu held.
func (e *Engine) byID(id int64) *openAlert {
	for _, o := range e.open {
		if o.alert.ID == id {
			return o
		}
	}
	return nil
}

// Ack acknowledges an open alert for the admin with login by, or with ack
// false, takes that back, and returns the alert. It returns ErrNotOpen if no
// open alert has the ID.
func (e *Engine) Ack(ctx context.Context, id int64, by string, ack bool, now time.Time) (store.Alert, error) {
	return e.change(ctx, id, func(a *store.Alert) store.Event {
		if !ack {
			a.AckedAt, a.AckedBy = time.Time{}, ""
			return store.Event{Message: "Unacknowledged: " + a.Title}
		}
		a.AckedAt, a.AckedBy = now, by
		return store.Event{Message: "Acknowledged: " + a.Title}
	}, by, now)
}

// Snooze snoozes an open alert until a time, at most MaxSnooze from now, for
// the admin with login by, or with until zero, wakes it. It returns the
// alert, or ErrNotOpen if no open alert has the ID.
func (e *Engine) Snooze(ctx context.Context, id int64, by string, until, now time.Time) (store.Alert, error) {
	if !until.IsZero() && !until.After(now) {
		return store.Alert{}, errors.New("an alert can only be snoozed until a time to come")
	}
	if !until.IsZero() && until.After(now.Add(MaxSnooze)) {
		until = now.Add(MaxSnooze)
	}
	return e.change(ctx, id, func(a *store.Alert) store.Event {
		if until.IsZero() {
			a.SnoozedUntil, a.SnoozedBy = time.Time{}, ""
			return store.Event{Message: "Woke: " + a.Title}
		}
		a.SnoozedUntil, a.SnoozedBy = until, by
		return store.Event{Message: fmt.Sprintf("Snoozed for %s: %s", shortDuration(until.Sub(now)), a.Title)}
	}, by, now)
}

// change changes an open alert with f, saves it, and logs what f says in
// the activity log, as done by the admin with login by.
func (e *Engine) change(ctx context.Context, id int64, f func(a *store.Alert) store.Event, by string, now time.Time) (store.Alert, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	o := e.byID(id)
	if o == nil {
		return store.Alert{}, ErrNotOpen
	}
	next := o.alert
	ev := f(&next)
	if err := e.st.SaveAlert(ctx, next); err != nil {
		return store.Alert{}, err
	}
	o.alert, o.saved = next, now
	ev.At, ev.Level, ev.Kind, ev.DeviceID, ev.Actor = now, "info", "alert", next.DeviceID, by
	if err := e.st.AddEvents(ctx, ev); err != nil {
		e.log.Warn("log a change to an alert in the activity log", "error", err)
	}
	return next, nil
}

// AddNote adds a note by the admin with login by to an alert, open or
// resolved, and returns it. It returns store.ErrNoAlert if there's no such
// alert.
func (e *Engine) AddNote(ctx context.Context, id int64, by, text string, now time.Time) (store.AlertNote, error) {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return store.AlertNote{}, errors.New("the note is empty")
	case utf8.RuneCountInString(text) > MaxNote:
		return store.AlertNote{}, errors.New("the note is too long")
	}
	n, err := e.st.AddAlertNote(ctx, id, store.AlertNote{At: now, Author: by, Text: text})
	if err != nil {
		return store.AlertNote{}, err
	}
	e.mu.Lock()
	var a store.Alert
	if o := e.byID(id); o != nil {
		o.alert.Notes = append(o.alert.Notes, n)
		a = o.alert
	}
	e.mu.Unlock()
	if a.ID == 0 {
		if a, err = e.st.AlertByID(ctx, id); err != nil {
			e.log.Warn("read an alert", "id", id, "error", err)
		}
	}
	ev := store.Event{At: now, Level: "info", Kind: "alert", Message: "Wrote a note on: " + a.Title, DeviceID: a.DeviceID, Actor: by}
	if err := e.st.AddEvents(ctx, ev); err != nil {
		e.log.Warn("log a note in the activity log", "error", err)
	}
	return n, nil
}

// shortDuration writes d in its largest whole unit, such as 4h or 7d.
func shortDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	switch {
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return fmt.Sprintf("%dm", d/time.Minute)
}

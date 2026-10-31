package notify

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/manasseh-fikadu/roost/internal/settings"
	"github.com/manasseh-fikadu/roost/internal/store"
)

const (
	// passEvery is how often a Dispatcher looks at the open alerts.
	passEvery = 5 * time.Second
	// maxTries is how many times Roost tries to send one message before it
	// gives up on it.
	maxTries = 4
	// EventKind is the kind of the events a Dispatcher adds to the activity
	// log.
	EventKind = "notification"
)

// retryAfter is how long Roost waits after a message fails, by how many
// times it has failed.
var retryAfter = [maxTries - 1]time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}

// A Delivery is what happened the last time Roost sent to a channel.
type Delivery struct {
	At    time.Time
	Event Event
	OK    bool
	// Error says what went wrong, without any secret in it. It's for admins:
	// the activity log, which viewers read, has Reason instead.
	Error string
}

// A Store holds the channels, what they were told, and the alerts. A
// *store.Store is one.
type Store interface {
	Channels(ctx context.Context) ([]store.Channel, error)
	Notified(ctx context.Context) ([]store.Notified, error)
	SetNotified(ctx context.Context, n store.Notified) error
	ClearNotified(ctx context.Context, alertID, channelID int64) error
	AlertByID(ctx context.Context, id int64) (store.Alert, error)
	AddEvents(ctx context.Context, events ...store.Event) error
}

// Options configure a Dispatcher.
type Options struct {
	// Store holds the channels and what they were told. Required.
	Store Store
	// Open returns the open alerts. Required.
	Open func() []store.Alert
	// Settings says when to remind and what Roost is called. Required.
	Settings *settings.Cache
	// AppURL is where Roost is on the tailnet, which the links in messages
	// start with unless the settings have a public URL. If both are empty,
	// messages have no link.
	AppURL string
	// Env is what senders reach servers with. If it's nil, they dial from
	// this machine.
	Env *Env
	// Changed is called after the Dispatcher adds to the activity log.
	Changed func()
	// NewSender returns the sender of a channel. If it's nil, it's
	// NewSender with Env.
	NewSender func(Channel) (Sender, error)
	Logger    *slog.Logger
}

// A Dispatcher sends the notifications the channels are due. Run it in a
// goroutine. It's safe for concurrent use.
type Dispatcher struct {
	opts Options
	log  *slog.Logger

	mu sync.Mutex
	// busy holds the channels that are sending, which wait for their turn.
	busy map[int64]bool
	// retries holds the messages that failed and are due again.
	retries map[jobKey]*retry
	// last is what each channel did last.
	last map[int64]Delivery
	// unsaved holds what channels were told that the store couldn't record,
	// until it can, so that a message isn't sent again meanwhile.
	unsaved map[[2]int64]unsaved
	// testing holds the channels that a test is being sent to.
	testing map[int64]bool
	// warned holds the channels whose config was reported as bad, so it's
	// reported once.
	warned map[int64]bool
	wg     sync.WaitGroup
}

// unsaved is what a channel was told, or told resolved if clear is set, that
// the store hasn't recorded yet.
type unsaved struct {
	n     store.Notified
	clear bool
}

type jobKey struct {
	alert, channel int64
	event          Event
}

type retry struct {
	tries int
	next  time.Time
}

// A job is a message a channel is due.
type job struct {
	ch    Channel
	event Event
	alert store.Alert
	// forget means the channel needn't be told, but what it was told about
	// the alert can be forgotten.
	forget bool
}

func (j job) key() jobKey { return jobKey{j.alert.ID, j.ch.ID, j.event} }

// NewDispatcher returns a Dispatcher.
func NewDispatcher(opts Options) *Dispatcher {
	if opts.Store == nil || opts.Open == nil || opts.Settings == nil {
		panic("notify.NewDispatcher: Options.Store, Open and Settings are required")
	}
	if opts.Env == nil {
		opts.Env = NewEnv(nil)
	}
	if opts.NewSender == nil {
		env := opts.Env
		opts.NewSender = func(c Channel) (Sender, error) { return NewSender(c, env) }
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Dispatcher{
		opts: opts, log: log, busy: map[int64]bool{}, retries: map[jobKey]*retry{},
		last: map[int64]Delivery{}, warned: map[int64]bool{}, unsaved: map[[2]int64]unsaved{}, testing: map[int64]bool{},
	}
}

// Run sends notifications as they come due, until ctx ends.
func (d *Dispatcher) Run(ctx context.Context) {
	defer d.wg.Wait()
	ticker := time.NewTicker(passEvery)
	defer ticker.Stop()
	for {
		d.pass(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Last returns what the channel with an ID did last, if it has sent since
// Roost started.
func (d *Dispatcher) Last(channelID int64) (Delivery, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	dl, ok := d.last[channelID]
	return dl, ok
}

// Forget drops what the Dispatcher remembers of a channel that's gone.
func (d *Dispatcher) Forget(channelID int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.last, channelID)
	delete(d.warned, channelID)
	maps.DeleteFunc(d.unsaved, func(k [2]int64, _ unsaved) bool { return k[0] == channelID })
	for k := range d.retries {
		if k.channel == channelID {
			delete(d.retries, k)
		}
	}
}

// Test sends a test message to c, for the admin with login by, and says what
// happened. It doesn't change what the channel has been told.
func (d *Dispatcher) Test(ctx context.Context, c Channel, by string) Delivery {
	d.mu.Lock()
	if d.testing[c.ID] {
		d.mu.Unlock()
		return Delivery{At: time.Now(), Event: Test, Error: "a test is already being sent to this channel"}
	}
	d.testing[c.ID] = true
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.testing, c.ID)
		d.mu.Unlock()
	}()

	now := time.Now()
	s := d.opts.Settings.Get(ctx)
	m := Message{Event: Test, Severity: "info", Title: "Test", At: now, Instance: s.General.Name}
	if base := d.baseURL(s); base != "" {
		m.URL = base + "/settings?tab=notifications"
	}
	err := d.send(ctx, c, m)
	dl := d.record(c.ID, Test, err, now)
	ev := store.Event{At: now, Actor: by}
	if err == nil {
		ev.Level, ev.Kind, ev.Message = "success", EventKind, "Sent a test notification to "+c.Name
	} else {
		d.log.Warn("send a test notification", "channel", c.Name, "error", err)
		ev.Level, ev.Kind, ev.Message = "error", EventKind, fmt.Sprintf("The test notification to %s failed: %s", c.Name, Reason(err))
	}
	d.addEvent(ctx, ev)
	return dl
}

// pass sends what's due at now.
func (d *Dispatcher) pass(ctx context.Context, now time.Time) {
	rows, err := d.opts.Store.Channels(ctx)
	if err != nil {
		if ctx.Err() == nil {
			d.log.Warn("read the notification channels", "error", err)
		}
		return
	}
	if len(rows) == 0 {
		return
	}
	channels := make([]Channel, 0, len(rows))
	for _, row := range rows {
		c, err := FromStore(row)
		if err != nil {
			d.warnOnce(row.ID, "read a notification channel", err)
			continue
		}
		channels = append(channels, c)
	}
	d.flush(ctx)
	notified, err := d.opts.Store.Notified(ctx)
	if err != nil {
		if ctx.Err() == nil {
			d.log.Warn("read what the channels were told", "error", err)
		}
		return
	}
	notified = d.withUnsaved(notified)
	open := d.opts.Open()
	openIDs := make(map[int64]bool, len(open))
	for _, a := range open {
		openIDs[a.ID] = true
	}
	// What an alert that was open when a channel was told has become.
	gone := map[int64]store.Alert{}
	for _, n := range notified {
		if _, ok := gone[n.AlertID]; ok || openIDs[n.AlertID] {
			continue
		}
		a, err := d.opts.Store.AlertByID(ctx, n.AlertID)
		if err == nil {
			gone[n.AlertID] = a
		} else if !errors.Is(err, store.ErrNoAlert) && ctx.Err() == nil {
			d.log.Warn("read an alert", "id", n.AlertID, "error", err)
		}
	}

	s := d.opts.Settings.Get(ctx)
	jobs := due(now, s.Notifications, channels, open, notified, gone)
	d.dispatch(ctx, s, jobs)
}

// flush tries again to record what the store couldn't earlier.
func (d *Dispatcher) flush(ctx context.Context) {
	d.mu.Lock()
	pending := maps.Clone(d.unsaved)
	d.mu.Unlock()
	for k, u := range pending {
		var err error
		if u.clear {
			err = d.opts.Store.ClearNotified(ctx, u.n.AlertID, u.n.ChannelID)
		} else {
			err = d.opts.Store.SetNotified(ctx, u.n)
		}
		if err != nil {
			continue
		}
		d.mu.Lock()
		// A newer change to the same pair stays.
		if d.unsaved[k] == u {
			delete(d.unsaved, k)
		}
		d.mu.Unlock()
	}
}

// withUnsaved returns notified with what the store hasn't recorded yet.
func (d *Dispatcher) withUnsaved(notified []store.Notified) []store.Notified {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.unsaved) == 0 {
		return notified
	}
	out := make([]store.Notified, 0, len(notified)+len(d.unsaved))
	for _, n := range notified {
		if _, ok := d.unsaved[[2]int64{n.ChannelID, n.AlertID}]; !ok {
			out = append(out, n)
		}
	}
	for _, u := range d.unsaved {
		if !u.clear {
			out = append(out, u.n)
		}
	}
	return out
}

// warnOnce logs an error about a channel the first time.
func (d *Dispatcher) warnOnce(channelID int64, what string, err error) {
	d.mu.Lock()
	first := !d.warned[channelID]
	d.warned[channelID] = true
	d.mu.Unlock()
	if first {
		d.log.Warn(what, "error", err)
	}
}

// due returns the messages that channels are due at now, the most severe
// alerts first: when an alert opens, escalates, needs a reminder or
// resolves. open are the open alerts, notified is what the channels were
// told, and gone holds the alerts that were open then and aren't now.
func due(now time.Time, cfg settings.Notifications, channels []Channel, open []store.Alert, notified []store.Notified, gone map[int64]store.Alert) []job {
	told := make(map[[2]int64]store.Notified, len(notified))
	for _, n := range notified {
		told[[2]int64{n.ChannelID, n.AlertID}] = n
	}
	openIDs := make(map[int64]bool, len(open))
	for _, a := range open {
		openIDs[a.ID] = true
	}
	var jobs []job
	for _, ch := range channels {
		if ch.Enabled {
			for _, a := range open {
				n, has := told[[2]int64{ch.ID, a.ID}]
				if event, ok := dueFor(now, cfg, ch, a, n, has); ok {
					jobs = append(jobs, job{ch: ch, event: event, alert: a})
				}
			}
		}
		for _, n := range notified {
			if n.ChannelID != ch.ID || openIDs[n.AlertID] {
				continue
			}
			a, ok := gone[n.AlertID]
			if !ok || a.ResolvedAt.IsZero() {
				continue
			}
			switch {
			case !ch.Enabled || !ch.OnResolve:
				jobs = append(jobs, job{ch: ch, alert: a, forget: true})
			case !held(cfg, now, a.Severity):
				jobs = append(jobs, job{ch: ch, event: Resolved, alert: a})
			}
		}
	}
	slices.SortStableFunc(jobs, func(a, b job) int {
		return cmp.Or(
			cmp.Compare(a.ch.ID, b.ch.ID),
			cmp.Compare(rank(a.alert.Severity), rank(b.alert.Severity)),
			cmp.Compare(a.alert.ID, b.alert.ID),
		)
	})
	return jobs
}

// dueFor says what an enabled channel is due about an open alert a, if
// anything, given what the channel was told, if it was told anything.
func dueFor(now time.Time, cfg settings.Notifications, ch Channel, a store.Alert, told store.Notified, hasTold bool) (Event, bool) {
	switch {
	case rank(a.Severity) > rank(ch.MinSeverity):
		return "", false
	case !a.AckedAt.IsZero() || a.SnoozedUntil.After(now):
		return "", false
	case held(cfg, now, a.Severity):
		return "", false
	case !hasTold:
		return Opened, true
	case rank(a.Severity) < rank(told.Severity):
		return Escalated, true
	case cfg.Remind() > 0 && now.Sub(told.At) >= cfg.Remind():
		return Reminder, true
	}
	return "", false
}

// held reports whether the quiet hours hold back a notification about an
// alert of a severity at now.
func held(cfg settings.Notifications, now time.Time, severity string) bool {
	return cfg.Quiet.Active(now) && (!cfg.Quiet.AllowCritical || severity != "critical")
}

// rank orders severities, the most severe first.
func rank(severity string) int {
	switch severity {
	case "critical":
		return 0
	case "warning":
		return 1
	}
	return 2
}

// dispatch sends jobs, one channel at a time, each channel in its own
// goroutine, so one that's slow or down doesn't hold up the rest.
func (d *Dispatcher) dispatch(ctx context.Context, s settings.Settings, jobs []job) {
	wanted := make(map[jobKey]bool, len(jobs))
	byChannel := map[int64][]job{}
	for _, j := range jobs {
		wanted[j.key()] = true
		byChannel[j.ch.ID] = append(byChannel[j.ch.ID], j)
	}
	d.mu.Lock()
	// A message that's no longer due, such as one for an alert that someone
	// acknowledged, isn't tried again.
	for k := range d.retries {
		if !wanted[k] {
			delete(d.retries, k)
		}
	}
	for id, js := range byChannel {
		if d.busy[id] {
			continue
		}
		d.busy[id] = true
		d.wg.Go(func() {
			defer func() {
				d.mu.Lock()
				delete(d.busy, id)
				d.mu.Unlock()
			}()
			for _, j := range js {
				if ctx.Err() != nil {
					return
				}
				d.deliver(ctx, s, j)
			}
		})
	}
	d.mu.Unlock()
}

// deliver sends one message, and keeps track of what came of it.
func (d *Dispatcher) deliver(ctx context.Context, s settings.Settings, j job) {
	if j.forget {
		d.settle(ctx, j, time.Now())
		return
	}
	key := j.key()
	d.mu.Lock()
	r := d.retries[key]
	waiting := r != nil && time.Now().Before(r.next)
	d.mu.Unlock()
	if waiting {
		return
	}

	now := time.Now()
	m := d.message(s, j, now)
	err := d.send(ctx, j.ch, m)
	if ctx.Err() != nil {
		return
	}
	d.record(j.ch.ID, j.event, err, now)
	if err == nil {
		d.mu.Lock()
		delete(d.retries, key)
		d.mu.Unlock()
		d.settle(ctx, j, now)
		d.addEvent(ctx, store.Event{
			At: now, Level: "success", Kind: EventKind, DeviceID: j.alert.DeviceID,
			Message: successMessage(j),
		})
		return
	}

	d.mu.Lock()
	if r == nil {
		r = &retry{}
		d.retries[key] = r
	}
	r.tries++
	tries := r.tries
	if tries < maxTries {
		r.next = now.Add(retryAfter[tries-1])
	} else {
		delete(d.retries, key)
	}
	d.mu.Unlock()
	d.log.Warn("send a notification", "channel", j.ch.Name, "alert", j.alert.ID, "event", j.event, "try", tries, "error", err)
	if tries < maxTries {
		return
	}
	// It's given up on: the channel counts as told, so the next message is a
	// reminder, if there are reminders, rather than this one again.
	d.settle(ctx, j, now)
	d.addEvent(ctx, store.Event{
		At: now, Level: "error", Kind: EventKind, DeviceID: j.alert.DeviceID,
		Message: fmt.Sprintf("Couldn't notify %s about: %s (%s; gave up after %d tries)",
			j.ch.Name, shortTitle(j.alert), Reason(err), tries),
	})
}

// settle records that a channel has been dealt with for an alert: told about
// it, or told it resolved, or no longer needs to be.
func (d *Dispatcher) settle(ctx context.Context, j job, now time.Time) {
	u := unsaved{n: store.Notified{AlertID: j.alert.ID, ChannelID: j.ch.ID, Severity: j.alert.Severity, At: now}}
	var err error
	if j.forget || j.event == Resolved {
		u.clear = true
		err = d.opts.Store.ClearNotified(ctx, j.alert.ID, j.ch.ID)
	} else {
		err = d.opts.Store.SetNotified(ctx, u.n)
	}
	if err == nil || ctx.Err() != nil {
		return
	}
	d.log.Warn("record a notification", "channel", j.ch.Name, "alert", j.alert.ID, "error", err)
	// Without a record, the message would be sent again at the next pass.
	// The Dispatcher remembers it until the store takes it.
	d.mu.Lock()
	d.unsaved[[2]int64{j.ch.ID, j.alert.ID}] = u
	d.mu.Unlock()
}

// message returns the message that j sends, at now.
func (d *Dispatcher) message(s settings.Settings, j job, now time.Time) Message {
	a := j.alert
	m := Message{
		Event: j.event, Severity: a.Severity, Title: a.Title, Detail: a.Detail, AlertID: a.ID,
		OpenedAt: a.OpenedAt, ResolvedAt: a.ResolvedAt, At: now, Instance: s.General.Name,
	}
	if base := d.baseURL(s); base != "" {
		m.URL = base + "/alerts?alert=" + strconv.FormatInt(a.ID, 10)
	}
	return m
}

// baseURL returns where Roost is, without a slash at the end, or "".
func (d *Dispatcher) baseURL(s settings.Settings) string {
	return strings.TrimSuffix(cmp.Or(s.General.PublicURL, d.opts.AppURL), "/")
}

// send sends m to c.
func (d *Dispatcher) send(ctx context.Context, c Channel, m Message) error {
	sender, err := d.opts.NewSender(c)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, SendTimeout)
	defer cancel()
	return sender.Send(ctx, m)
}

// record notes what a channel just did, and returns it.
func (d *Dispatcher) record(channelID int64, event Event, err error, now time.Time) Delivery {
	dl := Delivery{At: now, Event: event, OK: err == nil}
	if err != nil {
		dl.Error = trunc(oneLine(err.Error()), maxAnswer)
	}
	d.mu.Lock()
	d.last[channelID] = dl
	d.mu.Unlock()
	return dl
}

// addEvent adds an event to the activity log.
func (d *Dispatcher) addEvent(ctx context.Context, e store.Event) {
	if err := d.opts.Store.AddEvents(ctx, e); err != nil {
		if ctx.Err() == nil {
			d.log.Warn("log a notification in the activity log", "error", err)
		}
		return
	}
	if d.opts.Changed != nil {
		d.opts.Changed()
	}
}

func successMessage(j job) string {
	title := shortTitle(j.alert)
	switch j.event {
	case Escalated:
		return fmt.Sprintf("Notified %s that it got worse: %s", j.ch.Name, title)
	case Reminder:
		return fmt.Sprintf("Reminded %s of: %s", j.ch.Name, title)
	case Resolved:
		return fmt.Sprintf("Notified %s that it resolved: %s", j.ch.Name, title)
	default:
		return fmt.Sprintf("Notified %s about: %s", j.ch.Name, title)
	}
}

func shortTitle(a store.Alert) string { return trunc(oneLine(a.Title), 100) }

package notify

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/settings"
	"github.com/manasseh-fikadu/roost/internal/store"
)

// A flakyStore can't record what channels were told while fail is set.
type flakyStore struct {
	*store.Store
	fail atomic.Bool
}

var errFlaky = errors.New("the database is locked")

func (f *flakyStore) SetNotified(ctx context.Context, n store.Notified) error {
	if f.fail.Load() {
		return errFlaky
	}
	return f.Store.SetNotified(ctx, n)
}

func (f *flakyStore) ClearNotified(ctx context.Context, alertID, channelID int64) error {
	if f.fail.Load() {
		return errFlaky
	}
	return f.Store.ClearNotified(ctx, alertID, channelID)
}

type sentMessage struct {
	channel string
	Message
}

// A rig is a Dispatcher with channels that record what they send, and alerts
// the test opens and resolves.
type rig struct {
	t     *testing.T
	st    *store.Store
	flaky *flakyStore
	cache *settings.Cache
	d     *Dispatcher

	mu   sync.Mutex
	open []store.Alert
	sent []sentMessage
	// fail makes the channel with a name fail with the error.
	fail map[string]error
	// block makes the channel with a name wait until its context ends.
	block map[string]bool
}

type senderFunc func(ctx context.Context, m Message) error

func (f senderFunc) Send(ctx context.Context, m Message) error { return f(ctx, m) }

func newRig(t *testing.T, appURL string) *rig {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "roost.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	r := &rig{t: t, st: st, flaky: &flakyStore{Store: st}, cache: settings.NewCache(st, nil), fail: map[string]error{}, block: map[string]bool{}}
	r.d = NewDispatcher(Options{
		Store: r.flaky, Settings: r.cache, AppURL: appURL,
		Open: func() []store.Alert {
			r.mu.Lock()
			defer r.mu.Unlock()
			return slices.Clone(r.open)
		},
		NewSender: func(c Channel) (Sender, error) {
			return senderFunc(func(ctx context.Context, m Message) error {
				r.mu.Lock()
				err, block := r.fail[c.Name], r.block[c.Name]
				r.mu.Unlock()
				if block {
					<-ctx.Done()
					return ctx.Err()
				}
				if err != nil {
					return err
				}
				r.mu.Lock()
				r.sent = append(r.sent, sentMessage{c.Name, m})
				r.mu.Unlock()
				return nil
			}), nil
		},
	})
	return r
}

func (r *rig) channel(name, minSeverity string, onResolve bool) Channel {
	r.t.Helper()
	c := Channel{Name: name, Kind: Webhook, Enabled: true, MinSeverity: minSeverity, OnResolve: onResolve, Config: Config{URL: "https://example.com/hook"}}
	sc, err := r.st.AddChannel(r.t.Context(), c.ToStore())
	if err != nil {
		r.t.Fatal(err)
	}
	c.ID = sc.ID
	return c
}

func (r *rig) alert(key, severity, title string) store.Alert {
	r.t.Helper()
	now := time.Now()
	a, err := r.st.AddAlert(r.t.Context(), store.Alert{
		Key: key, Source: "metrics", DeviceID: "laptop", Severity: severity, Title: title, Detail: "details", OpenedAt: now, LastSeen: now,
	})
	if err != nil {
		r.t.Fatal(err)
	}
	r.mu.Lock()
	r.open = append(r.open, a)
	r.mu.Unlock()
	return a
}

// change changes an open alert, in the store and in the open alerts.
func (r *rig) change(a store.Alert, f func(a *store.Alert)) store.Alert {
	r.t.Helper()
	f(&a)
	if err := r.st.SaveAlert(r.t.Context(), a); err != nil {
		r.t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.open {
		if r.open[i].ID == a.ID {
			r.open[i] = a
		}
	}
	return a
}

func (r *rig) resolve(a store.Alert) {
	r.t.Helper()
	a = r.change(a, func(a *store.Alert) { a.ResolvedAt = time.Now() })
	r.mu.Lock()
	r.open = slices.DeleteFunc(r.open, func(o store.Alert) bool { return o.ID == a.ID })
	r.mu.Unlock()
}

func (r *rig) settings(change func(s *settings.Settings)) {
	r.t.Helper()
	_, _, err := r.cache.Update(r.t.Context(), func(s *settings.Settings) error { change(s); return nil })
	if err != nil {
		r.t.Fatal(err)
	}
}

// pass looks at the alerts and waits for what that sends.
func (r *rig) pass() {
	r.d.pass(r.t.Context(), time.Now())
	r.d.wg.Wait()
}

// take returns what was sent since it was last called, as "channel event
// title" lines, sorted.
func (r *rig) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, m := range r.sent {
		out = append(out, m.channel+" "+string(m.Event)+" "+m.Title)
	}
	r.sent = nil
	// Channels send at the same time, in no order.
	slices.Sort(out)
	return out
}

// events returns the messages of the notification events, the oldest first.
func (r *rig) events() []string {
	r.t.Helper()
	es, err := r.st.Events(r.t.Context(), store.EventQuery{Kind: EventKind, Limit: 100})
	if err != nil {
		r.t.Fatal(err)
	}
	var out []string
	for _, e := range slices.Backward(es) {
		out = append(out, e.Level+": "+e.Message)
	}
	return out
}

func (r *rig) told() int {
	r.t.Helper()
	n, err := r.st.Notified(r.t.Context())
	if err != nil {
		r.t.Fatal(err)
	}
	return len(n)
}

func wantLines(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got %q\nwant %q", what, got, want)
	}
}

func TestDispatcher_opensEscalatesResolves(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "https://roost.example.ts.net/")
		r.channel("Phone", "warning", true)
		r.channel("Pager", "critical", false)

		disk := r.alert("disk:laptop", "warning", "Disk 90% full")
		info := r.alert("note", "info", "Heads up")
		r.pass()
		wantLines(t, "a warning opens", r.take(), "Phone opened Disk 90% full")
		r.pass()
		wantLines(t, "nothing is sent twice", r.take())

		disk = r.change(disk, func(a *store.Alert) { a.Severity, a.Title = "critical", "Disk 97% full" })
		r.pass()
		wantLines(t, "it gets worse", r.take(), "Pager opened Disk 97% full", "Phone escalated Disk 97% full")
		r.pass()
		wantLines(t, "worse is said once", r.take())

		time.Sleep(90 * time.Second)
		r.resolve(disk)
		r.resolve(info)
		r.pass()
		wantLines(t, "it resolves", r.take(), "Phone resolved Disk 97% full")
		if n := r.told(); n != 0 {
			t.Errorf("%d rows of what channels were told are left after the alert resolved, want 0", n)
		}
		r.pass()
		wantLines(t, "resolving is said once", r.take())

		events := r.events()
		slices.Sort(events)
		wantLines(t, "the activity log", events,
			"success: Notified Pager about: Disk 97% full",
			"success: Notified Phone about: Disk 90% full",
			"success: Notified Phone that it got worse: Disk 97% full",
			"success: Notified Phone that it resolved: Disk 97% full",
		)
	})
}

func TestDispatcher_message(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "https://roost.example.ts.net/")
		r.channel("Phone", "info", true)
		a := r.alert("disk:laptop", "warning", "Disk 90% full")
		r.pass()
		time.Sleep(time.Hour)
		r.resolve(a)
		r.pass()

		r.mu.Lock()
		sent := slices.Clone(r.sent)
		r.mu.Unlock()
		if len(sent) != 2 {
			t.Fatalf("sent %d messages, want 2", len(sent))
		}
		opened, resolved := sent[0], sent[1]
		if opened.URL != "https://roost.example.ts.net/alerts?alert=1" {
			t.Errorf("URL = %q, want a link to the alert on the app's address, without a double slash", opened.URL)
		}
		if opened.Instance != "Roost" || opened.Severity != "warning" || opened.Detail != "details" || opened.AlertID != a.ID {
			t.Errorf("opened = %+v", opened.Message)
		}
		if resolved.Event != Resolved || resolved.ResolvedAt.IsZero() || resolved.Facts() != "Resolved after 1h" {
			t.Errorf("resolved = %+v, facts %q", resolved.Message, resolved.Facts())
		}

		// A public URL, if there is one, is what links start with.
		r.settings(func(s *settings.Settings) { s.General.PublicURL, s.General.Name = "https://roost.example.com", "Lab" })
		r.alert("cpu:laptop", "warning", "CPU busy")
		r.pass()
		r.mu.Lock()
		last := r.sent[len(r.sent)-1]
		r.mu.Unlock()
		if last.URL != "https://roost.example.com/alerts?alert=2" || last.Instance != "Lab" {
			t.Errorf("URL = %q, instance %q, want the public URL and the name", last.URL, last.Instance)
		}

		// Without any address, there's no link rather than a broken one.
		bare := newRig(t, "")
		bare.channel("Phone", "info", true)
		bare.alert("disk:laptop", "warning", "Disk 90% full")
		bare.pass()
		if len(bare.sent) != 1 || bare.sent[0].URL != "" {
			t.Errorf("sent = %+v, want one message with no link", bare.sent)
		}
	})
}

func TestDispatcher_ackAndSnooze(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "")
		r.channel("Phone", "info", true)

		acked := r.alert("a", "warning", "Acked before it was told")
		r.change(acked, func(a *store.Alert) { a.AckedAt, a.AckedBy = time.Now(), "alice" })
		snoozed := r.alert("b", "warning", "Snoozed")
		r.change(snoozed, func(a *store.Alert) { a.SnoozedUntil = time.Now().Add(time.Hour) })
		r.pass()
		wantLines(t, "acknowledged and snoozed alerts", r.take())

		time.Sleep(30 * time.Minute)
		r.pass()
		wantLines(t, "still snoozed", r.take())

		time.Sleep(31 * time.Minute)
		r.pass()
		wantLines(t, "once the snooze is over", r.take(), "Phone opened Snoozed")
		r.pass()
		wantLines(t, "once only", r.take())

		// Taking back an acknowledgement makes the alert due.
		r.change(acked, func(a *store.Alert) { a.AckedAt, a.AckedBy = time.Time{}, "" })
		r.pass()
		wantLines(t, "not acknowledged any more", r.take(), "Phone opened Acked before it was told")
	})
}

func TestDispatcher_reminders(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "")
		r.channel("Phone", "warning", true)
		a := r.alert("disk", "warning", "Disk")
		r.pass()
		wantLines(t, "opening", r.take(), "Phone opened Disk")

		time.Sleep(25 * time.Hour)
		r.pass()
		wantLines(t, "no reminders unless asked for", r.take())

		r.settings(func(s *settings.Settings) { s.Notifications.RemindHours = 2 })
		r.pass()
		wantLines(t, "reminders are due once asked for, as it was last told long ago", r.take(), "Phone reminder Disk")
		time.Sleep(119 * time.Minute)
		r.pass()
		wantLines(t, "before the next one", r.take())
		time.Sleep(2 * time.Minute)
		r.pass()
		wantLines(t, "two hours after the last", r.take(), "Phone reminder Disk")

		// Nobody is reminded of what they know about.
		r.change(a, func(a *store.Alert) { a.AckedAt, a.AckedBy = time.Now(), "alice" })
		time.Sleep(5 * time.Hour)
		r.pass()
		wantLines(t, "an acknowledged alert", r.take())
		wantLines(t, "the activity log", r.events(),
			"success: Notified Phone about: Disk", "success: Reminded Phone of: Disk", "success: Reminded Phone of: Disk")
	})
}

func TestDispatcher_quietHours(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// A bubble's clock starts at midnight UTC.
		r := newRig(t, "")
		r.channel("Phone", "info", true)
		r.settings(func(s *settings.Settings) {
			s.Notifications.Quiet = settings.QuietHours{Enabled: true, Start: "22:00", End: "07:00", Timezone: "UTC", AllowCritical: true}
		})
		warning := r.alert("a", "warning", "Warning at night")
		r.alert("b", "critical", "Critical at night")
		flap := r.alert("c", "warning", "Gone by morning")
		r.pass()
		wantLines(t, "at night", r.take(), "Phone opened Critical at night")

		r.resolve(flap)
		time.Sleep(7 * time.Hour)
		r.pass()
		wantLines(t, "when the night ends", r.take(), "Phone opened Warning at night")
		if n := r.told(); n != 2 {
			t.Errorf("%d rows of what channels were told, want 2: nothing was said of the alert that flapped away", n)
		}

		// What resolves during the night is said in the morning.
		r.settings(func(s *settings.Settings) { s.Notifications.Quiet.Start, s.Notifications.Quiet.End = "08:00", "09:00" })
		time.Sleep(time.Hour)
		r.resolve(warning)
		r.pass()
		wantLines(t, "resolved during quiet hours", r.take())
		time.Sleep(time.Hour)
		r.pass()
		wantLines(t, "after them", r.take(), "Phone resolved Warning at night")

		// Critical alerts are held too, if the settings say so.
		r.settings(func(s *settings.Settings) {
			s.Notifications.Quiet = settings.QuietHours{Enabled: true, Start: "09:00", End: "23:00", Timezone: "UTC"}
		})
		r.alert("d", "critical", "Critical, held")
		r.pass()
		wantLines(t, "critical during quiet hours that don't allow it", r.take())
	})
}

func TestDispatcher_failures(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "")
		r.channel("Phone", "info", true)
		a := r.alert("disk", "warning", "Disk")
		r.fail["Phone"] = &StatusError{Code: 500, Body: "internal address 10.1.2.3 is down"}

		r.pass()
		r.pass() // within the wait, so it isn't tried again
		time.Sleep(29 * time.Second)
		r.pass()
		if got := r.events(); len(got) != 0 {
			t.Errorf("activity log = %q, want nothing yet: the first failures are retried quietly", got)
		}
		time.Sleep(time.Second) // 30s after the first try
		r.pass()
		time.Sleep(2*time.Minute + time.Second)
		r.pass()
		if got := r.events(); len(got) != 0 {
			t.Errorf("activity log = %q, want nothing yet", got)
		}
		time.Sleep(10*time.Minute + time.Second)
		r.pass()
		got := r.events()
		wantLines(t, "after the last try", got, "error: Couldn't notify Phone about: Disk (the server answered 500; gave up after 4 tries)")
		if strings.Contains(strings.Join(got, ""), "10.1.2.3") {
			t.Errorf("activity log = %q, which has what the server said, and viewers read it", got)
		}
		if dl, ok := r.d.Last(1); !ok || dl.OK || !strings.Contains(dl.Error, "10.1.2.3") {
			t.Errorf("Last() = %+v, %v, want the failure, with what the server said, for admins", dl, ok)
		}

		// It's given up on, not tried again every pass.
		time.Sleep(time.Hour)
		r.pass()
		wantLines(t, "later", r.events(), got...)
		r.mu.Lock()
		delete(r.fail, "Phone")
		r.mu.Unlock()
		r.resolve(a)
		r.pass()
		wantLines(t, "once it works again", r.take(), "Phone resolved Disk")
	})
}

func TestDispatcher_retrySucceeds(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "")
		r.channel("Phone", "info", true)
		a := r.alert("disk", "warning", "Disk")
		r.fail["Phone"] = errors.New("boom")
		r.pass()
		r.mu.Lock()
		delete(r.fail, "Phone")
		r.mu.Unlock()
		time.Sleep(31 * time.Second)
		r.pass()
		wantLines(t, "the retry", r.take(), "Phone opened Disk")
		wantLines(t, "the activity log", r.events(), "success: Notified Phone about: Disk")
		if dl, _ := r.d.Last(1); !dl.OK {
			t.Errorf("Last() = %+v, want a success", dl)
		}

		// A retry that's no longer due is dropped.
		r.fail["Phone"] = errors.New("boom")
		r.change(a, func(a *store.Alert) { a.Severity = "critical" })
		r.pass()
		r.change(a, func(a *store.Alert) { a.Severity, a.AckedAt = "critical", time.Now() })
		r.mu.Lock()
		delete(r.fail, "Phone")
		r.mu.Unlock()
		time.Sleep(time.Minute)
		r.pass()
		wantLines(t, "an alert that was acknowledged while it was retried", r.take())
		r.d.mu.Lock()
		n := len(r.d.retries)
		r.d.mu.Unlock()
		if n != 0 {
			t.Errorf("%d retries are left, want none", n)
		}
	})
}

func TestDispatcher_disabledChannel(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "")
		ch := r.channel("Phone", "info", true)
		a := r.alert("disk", "warning", "Disk")
		r.pass()
		wantLines(t, "enabled", r.take(), "Phone opened Disk")

		ch.Enabled = false
		if err := r.st.UpdateChannel(t.Context(), ch.ToStore()); err != nil {
			t.Fatal(err)
		}
		r.alert("cpu", "critical", "CPU")
		r.resolve(a)
		r.pass()
		wantLines(t, "disabled", r.take())
		if n := r.told(); n != 0 {
			t.Errorf("%d rows of what the channel was told are left, want 0: a disabled channel forgets resolved alerts", n)
		}
	})
}

func TestDispatcher_oneChannelDoesNotHoldUpAnother(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "")
		r.channel("Phone", "info", true)
		r.channel("Slow", "info", true)
		r.block["Slow"] = true
		r.alert("disk", "warning", "Disk")

		r.d.pass(t.Context(), time.Now())
		synctest.Wait()
		wantLines(t, "while Slow waits", r.take(), "Phone opened Disk")
		// A pass while it waits doesn't pile up sends to it.
		r.d.pass(t.Context(), time.Now())
		synctest.Wait()

		time.Sleep(SendTimeout + time.Second)
		r.d.wg.Wait()
		if dl, ok := r.d.Last(2); !ok || dl.OK || Reason(errors.New(dl.Error)) == "" {
			t.Errorf("Last() of the slow channel = %+v, %v, want a failure", dl, ok)
		}
	})
}

// A message that was sent but couldn't be recorded isn't sent again while the
// store fails, and is recorded once it can.
func TestDispatcher_storeFails(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "")
		r.channel("Phone", "info", true)
		r.channel("Quiet", "info", false)
		a := r.alert("disk", "warning", "Disk")
		r.flaky.fail.Store(true)
		r.pass()
		wantLines(t, "first", r.take(), "Phone opened Disk", "Quiet opened Disk")
		for range 3 {
			time.Sleep(time.Minute)
			r.pass()
		}
		wantLines(t, "while the store fails", r.take())
		if n := r.told(); n != 0 {
			t.Fatalf("%d rows are recorded while the store fails, want none", n)
		}

		// A resolved alert is said once too, and a channel that needn't hear
		// it forgets it.
		r.resolve(a)
		r.pass()
		wantLines(t, "resolved while the store fails", r.take(), "Phone resolved Disk")
		time.Sleep(time.Minute)
		r.pass()
		wantLines(t, "resolved again", r.take())

		r.flaky.fail.Store(false)
		r.pass()
		wantLines(t, "once the store works", r.take())
		if n := r.told(); n != 0 {
			t.Errorf("%d rows are recorded after the alert resolved, want none", n)
		}
		r.d.mu.Lock()
		n := len(r.d.unsaved)
		r.d.mu.Unlock()
		if n != 0 {
			t.Errorf("%d notices are still unsaved, want none", n)
		}
	})
}

func TestDispatcher_storeRecoversAfterOpening(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "")
		r.channel("Phone", "info", true)
		r.alert("disk", "warning", "Disk")
		r.flaky.fail.Store(true)
		r.pass()
		wantLines(t, "opened", r.take(), "Phone opened Disk")
		r.flaky.fail.Store(false)
		r.pass()
		wantLines(t, "once the store works", r.take())
		if n := r.told(); n != 1 {
			t.Errorf("%d rows are recorded once the store works, want 1", n)
		}
	})
}

func TestDispatcher_Test_oneAtATime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "")
		ch := r.channel("Slow", "info", true)
		r.block["Slow"] = true
		first := make(chan Delivery, 1)
		go func() { first <- r.d.Test(t.Context(), ch, "alice") }()
		synctest.Wait()
		second := r.d.Test(t.Context(), ch, "alice")
		if second.OK || !strings.Contains(second.Error, "already being sent") {
			t.Errorf("a second Test() = %+v, want a refusal while the first waits", second)
		}
		time.Sleep(SendTimeout + time.Second)
		if dl := <-first; dl.OK {
			t.Errorf("the first Test() = %+v, want a failure: the channel never answered", dl)
		}
		r.block["Slow"] = false
		if dl := r.d.Test(t.Context(), ch, "alice"); !dl.OK {
			t.Errorf("Test() after the first ended = %+v, want a success", dl)
		}
	})
}

func TestDispatcher_Test(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "https://roost.example.ts.net")
		ch := r.channel("Phone", "critical", true)
		dl := r.d.Test(t.Context(), ch, "alice@example.com")
		if !dl.OK || dl.Event != Test {
			t.Fatalf("Test() = %+v, want a success", dl)
		}
		r.mu.Lock()
		sent := slices.Clone(r.sent)
		r.mu.Unlock()
		if len(sent) != 1 || sent[0].Event != Test || sent[0].AlertID != 0 || sent[0].Instance != "Roost" ||
			sent[0].URL != "https://roost.example.ts.net/settings?tab=notifications" {
			t.Errorf("sent = %+v, want one test message", sent)
		}
		if last, ok := r.d.Last(ch.ID); !ok || !last.OK {
			t.Errorf("Last() = %+v, %v", last, ok)
		}
		if n := r.told(); n != 0 {
			t.Errorf("a test left %d rows of what channels were told", n)
		}

		r.fail["Phone"] = &StatusError{Code: 401, Body: "private details"}
		dl = r.d.Test(t.Context(), ch, "alice@example.com")
		if dl.OK || !strings.Contains(dl.Error, "private details") {
			t.Errorf("Test() = %+v, want a failure with what the server said", dl)
		}
		es, err := r.st.Events(t.Context(), store.EventQuery{Kind: EventKind, Limit: 10})
		if err != nil || len(es) != 2 {
			t.Fatalf("Events() = %+v, %v, want two", es, err)
		}
		if es[0].Level != "error" || es[0].Message != "The test notification to Phone failed: the server answered 401" || es[0].Actor != "alice@example.com" ||
			es[1].Level != "success" || es[1].Message != "Sent a test notification to Phone" {
			t.Errorf("events = %+v", es)
		}

		r.d.Forget(ch.ID)
		if _, ok := r.d.Last(ch.ID); ok {
			t.Error("Last() of a forgotten channel still has something")
		}
	})
}

func TestDispatcher_Run(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, "")
		r.channel("Phone", "info", true)
		changed := 0
		r.d.opts.Changed = func() { changed++ }
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { r.d.Run(ctx); close(done) }()

		synctest.Wait()
		r.alert("disk", "warning", "Disk")
		time.Sleep(passEvery + time.Second)
		synctest.Wait()
		wantLines(t, "the next pass", r.take(), "Phone opened Disk")
		if changed != 1 {
			t.Errorf("Changed was called %d times, want 1", changed)
		}
		cancel()
		<-done
	})
}

func TestDue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	ch := Channel{ID: 1, Name: "Phone", Enabled: true, MinSeverity: "warning", OnResolve: true}
	warn := store.Alert{ID: 1, Severity: "warning", Title: "A"}
	crit := store.Alert{ID: 2, Severity: "critical", Title: "B"}
	info := store.Alert{ID: 3, Severity: "info", Title: "C"}
	jobs := due(now, settings.Notifications{}, []Channel{ch}, []store.Alert{info, warn, crit}, nil, nil)
	var got []string
	for _, j := range jobs {
		got = append(got, string(j.event)+" "+j.alert.Title)
	}
	// The most severe go first, and the info alert is below the channel's.
	wantLines(t, "due", got, "opened B", "opened A")
}

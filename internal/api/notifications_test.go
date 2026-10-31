package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/manasseh-fikadu/roost/internal/alerts"
	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/notify"
	"github.com/manasseh-fikadu/roost/internal/settings"
	"github.com/manasseh-fikadu/roost/internal/store"
)

// A recorder is the sender of every channel in a test: it keeps what it was
// sent, and fails with err if there is one.
type recorder struct {
	mu   sync.Mutex
	sent []notify.Message
	err  error
}

func (r *recorder) newSender(notify.Channel) (notify.Sender, error) { return r, nil }

func (r *recorder) Send(_ context.Context, m notify.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.sent = append(r.sent, m)
	return nil
}

func notifyHandler(t *testing.T) (http.Handler, *store.Store, *recorder) {
	t.Helper()
	st := testStore(t)
	rec := &recorder{}
	opts := withStore(t, testTailnet(), st, api.Options{AppURL: "https://roost.example.ts.net"})
	opts.Notify = notify.NewDispatcher(notify.Options{
		Store: st, Open: opts.Alerts.Open, Settings: opts.Settings, AppURL: "https://roost.example.ts.net", NewSender: rec.newSender,
	})
	return api.New(opts), st, rec
}

const (
	webhookJSON = `{"name":"Hook","kind":"webhook","enabled":true,"minSeverity":"warning","onResolve":true,
		"config":{"url":"https://example.com/hook/very-secret-path","token":"s3cret-token"}}`
	emailJSON = `{"name":"Mail","kind":"email","enabled":true,"minSeverity":"critical","onResolve":false,
		"config":{"host":"smtp.example.com","security":"tls","username":"roost","password":"mail-password",
		"from":"roost@example.com","to":[" alice@example.com ","","bob@example.com"]}}`
)

func addChannel(t *testing.T, h http.Handler, body string) api.NotificationChannel {
	t.Helper()
	rec := send(t, h, http.MethodPost, "/api/notifications/channels", laptopIP, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/notifications/channels = %d %s, want 201", rec.Code, rec.Body)
	}
	var c api.NotificationChannel
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func listChannels(t *testing.T, h http.Handler) []api.NotificationChannel {
	t.Helper()
	var out api.NotificationChannels
	get(t, h, "/api/notifications/channels", laptopIP, &out)
	return out.Channels
}

func TestChannels(t *testing.T) {
	t.Parallel()
	h, st, _ := notifyHandler(t)

	// Only admins see or change channels.
	for from, want := range map[netip.Addr]int{laptopIP: http.StatusOK, phoneIP: http.StatusForbidden, tabletIP: http.StatusForbidden} {
		if rec := serve(h, newRequest(t, http.MethodGet, "/api/notifications/channels", from)); rec.Code != want {
			t.Errorf("GET /api/notifications/channels from %v = %d, want %d", from, rec.Code, want)
		}
	}
	if rec := send(t, h, http.MethodPost, "/api/notifications/channels", phoneIP, webhookJSON); rec.Code != http.StatusForbidden {
		t.Errorf("POST by a viewer = %d, want 403", rec.Code)
	}
	if got := listChannels(t, h); len(got) != 0 {
		t.Fatalf("channels of a new Roost = %+v, want none", got)
	}

	hook := addChannel(t, h, webhookJSON)
	mail := addChannel(t, h, emailJSON)
	if hook.ID == 0 || hook.Name != "Hook" || hook.Kind != api.ChannelKindWebhook || !hook.Enabled || hook.MinSeverity != api.AlertSeverityWarning || !hook.OnResolve {
		t.Errorf("added channel = %+v", hook)
	}
	if !slices.Equal(hook.Secrets, []string{"url", "token"}) || hook.Summary != "https://example.com" {
		t.Errorf("secrets %v, summary %q, want url and token saved and the host shown without the path", hook.Secrets, hook.Summary)
	}
	if !slices.Equal(mail.Secrets, []string{"password"}) || !slices.Equal(mail.Config.To, []string{"alice@example.com", "bob@example.com"}) ||
		mail.Summary != "alice@example.com, bob@example.com via smtp.example.com" || mail.Config.Username != "roost" {
		t.Errorf("added mail channel = %+v", mail)
	}

	// No answer has a secret in it, and neither has the activity log.
	secrets := []string{"very-secret-path", "s3cret-token", "mail-password"}
	check := func(what, body string) {
		t.Helper()
		for _, s := range secrets {
			if strings.Contains(body, s) {
				t.Errorf("%s has the secret %q in it:\n%s", what, s, body)
			}
		}
	}
	rec := serve(h, newRequest(t, http.MethodGet, "/api/notifications/channels", laptopIP))
	check("the list of channels", rec.Body.String())
	check("a new channel", string(mustJSON(t, hook)))
	rec = serve(h, newRequest(t, http.MethodGet, "/api/events?kind=notification", laptopIP))
	check("the activity log", rec.Body.String())
	if got := listChannels(t, h); len(got) != 2 || got[0].Name != "Hook" || got[1].Name != "Mail" {
		t.Errorf("channels = %+v, want both, by name", got)
	}
	// What is stored does have them: that is how Roost sends.
	rows, _ := st.Channels(t.Context())
	if len(rows) != 2 || !strings.Contains(rows[0].Config, "very-secret-path") {
		t.Errorf("stored channels = %+v, want the webhook's address kept", rows)
	}

	// A change that leaves the secrets out keeps them.
	put := func(id int64, body string) *api.NotificationChannel {
		t.Helper()
		rec := send(t, h, http.MethodPut, "/api/notifications/channels/"+strconv.FormatInt(id, 10), laptopIP, body)
		if rec.Code != http.StatusOK {
			t.Logf("PUT = %d %s", rec.Code, rec.Body)
			return nil
		}
		var c api.NotificationChannel
		if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		return &c
	}
	c := put(hook.ID, `{"name":"Hook 2","kind":"webhook","enabled":false,"minSeverity":"critical","onResolve":false,"config":{}}`)
	if c == nil || c.Name != "Hook 2" || c.Enabled || c.MinSeverity != api.AlertSeverityCritical || c.OnResolve || !slices.Equal(c.Secrets, []string{"url", "token"}) {
		t.Fatalf("changed channel = %+v, want new routing and the secrets kept", c)
	}
	sc, _ := notify.FromStore(mustChannel(t, st, hook.ID))
	if sc.Config.URL != "https://example.com/hook/very-secret-path" || sc.Config.Token != "s3cret-token" {
		t.Errorf("stored config = %+v, want the secrets kept", sc.Config)
	}
	// A secret that's sent replaces it, and an empty one takes it away.
	c = put(hook.ID, `{"name":"Hook 2","kind":"webhook","enabled":true,"minSeverity":"info","onResolve":true,
		"config":{"url":" https://example.com/new ","token":""}}`)
	if c == nil || !slices.Equal(c.Secrets, []string{"url"}) {
		t.Fatalf("changed channel = %+v, want only the address saved", c)
	}
	sc, _ = notify.FromStore(mustChannel(t, st, hook.ID))
	if sc.Config.URL != "https://example.com/new" || sc.Config.Token != "" {
		t.Errorf("stored config = %+v, want the new address and no token", sc.Config)
	}

	// Mistakes.
	for name, tt := range map[string]struct {
		method, target, body string
		want                 int
		msg                  string
	}{
		"a name that is taken": {http.MethodPost, "/api/notifications/channels",
			`{"name":"mail","kind":"webhook","minSeverity":"info","config":{"url":"https://example.com/h"}}`, http.StatusConflict, "name"},
		"a bad address": {http.MethodPost, "/api/notifications/channels",
			`{"name":"Bad","kind":"discord","minSeverity":"info","config":{"url":"https://example.com/oops-secret-value"}}`, http.StatusBadRequest, "Discord gave you"},
		"a kind that does not exist": {http.MethodPost, "/api/notifications/channels",
			`{"name":"Bad","kind":"pager","minSeverity":"info","config":{}}`, http.StatusBadRequest, "kind"},
		"no severity": {http.MethodPost, "/api/notifications/channels",
			`{"name":"Bad","kind":"webhook","config":{"url":"https://example.com/h"}}`, http.StatusBadRequest, "least severe"},
		"a new kind": {http.MethodPut, "/api/notifications/channels/" + strconv.FormatInt(hook.ID, 10),
			`{"name":"Hook","kind":"slack","minSeverity":"info","config":{}}`, http.StatusBadRequest, "kind can't change"},
		"a name that another has": {http.MethodPut, "/api/notifications/channels/" + strconv.FormatInt(hook.ID, 10),
			`{"name":"MAIL","kind":"webhook","minSeverity":"info","config":{}}`, http.StatusConflict, "name"},
		"a missing channel": {http.MethodPut, "/api/notifications/channels/999",
			`{"name":"x","kind":"webhook","minSeverity":"info","config":{}}`, http.StatusNotFound, "no notification channel"},
		"an ID that is not one":      {http.MethodDelete, "/api/notifications/channels/abc", ``, http.StatusNotFound, "no notification channel"},
		"deleting a missing channel": {http.MethodDelete, "/api/notifications/channels/999", ``, http.StatusNotFound, "no notification channel"},
		"testing a missing channel":  {http.MethodPost, "/api/notifications/channels/999/test", ``, http.StatusNotFound, "no notification channel"},
	} {
		rec := send(t, h, tt.method, tt.target, laptopIP, tt.body)
		var e api.ErrorBody
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		if rec.Code != tt.want || !strings.Contains(e.Error, tt.msg) || strings.Contains(rec.Body.String(), "oops-secret-value") {
			t.Errorf("%s: %s %s = %d %s, want %d and %q, without the secret", name, tt.method, tt.target, rec.Code, rec.Body, tt.want, tt.msg)
		}
	}

	// Deleting.
	if rec := send(t, h, http.MethodDelete, "/api/notifications/channels/"+strconv.FormatInt(mail.ID, 10), laptopIP, ""); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE = %d %s, want 204", rec.Code, rec.Body)
	}
	if got := listChannels(t, h); len(got) != 1 || got[0].ID != hook.ID {
		t.Errorf("channels after a delete = %+v", got)
	}

	// The activity log says who did what, to admins.
	var events api.EventList
	get(t, h, "/api/events?kind=notification", laptopIP, &events)
	var msgs []string
	for _, e := range slices.Backward(events.Events) {
		msgs = append(msgs, e.Message)
		if e.Actor != alice {
			t.Errorf("event %q has actor %q, want alice", e.Message, e.Actor)
		}
	}
	want := []string{
		"Added the notification channel Hook", "Added the notification channel Mail",
		"Changed the notification channel Hook 2", "Changed the notification channel Hook 2",
		"Deleted the notification channel Mail",
	}
	if !slices.Equal(msgs, want) {
		t.Errorf("activity = %q, want %q", msgs, want)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustChannel(t *testing.T, st *store.Store, id int64) store.Channel {
	t.Helper()
	rows, err := st.Channels(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no channel %d", id)
	return store.Channel{}
}

func TestChannels_limit(t *testing.T) {
	t.Parallel()
	h, _, _ := notifyHandler(t)
	for i := range 20 {
		addChannel(t, h, `{"name":"Hook `+strconv.Itoa(i)+`","kind":"webhook","minSeverity":"info","config":{"url":"https://example.com/h"}}`)
	}
	rec := send(t, h, http.MethodPost, "/api/notifications/channels", laptopIP,
		`{"name":"One too many","kind":"webhook","minSeverity":"info","config":{"url":"https://example.com/h"}}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "20") {
		t.Errorf("the 21st channel = %d %s, want a 400 that says the limit", rec.Code, rec.Body)
	}
}

func TestChannels_test(t *testing.T) {
	t.Parallel()
	h, st, rec := notifyHandler(t)
	hook := addChannel(t, h, webhookJSON)
	target := "/api/notifications/channels/" + strconv.FormatInt(hook.ID, 10) + "/test"

	if r := send(t, h, http.MethodPost, target, phoneIP, ""); r.Code != http.StatusForbidden {
		t.Errorf("a viewer's test = %d, want 403", r.Code)
	}
	var dl api.ChannelDelivery
	r := send(t, h, http.MethodPost, target, laptopIP, "")
	if err := json.Unmarshal(r.Body.Bytes(), &dl); r.Code != http.StatusOK || err != nil || !dl.OK || dl.Event != "test" || dl.Error != "" {
		t.Fatalf("POST test = %d %s, want a success", r.Code, r.Body)
	}
	if len(rec.sent) != 1 || rec.sent[0].Event != notify.Test || rec.sent[0].URL != "https://roost.example.ts.net/settings?tab=notifications" || rec.sent[0].Instance != "Roost" {
		t.Errorf("sent = %+v, want one test message", rec.sent)
	}
	if got := listChannels(t, h)[0]; got.Last == nil || !got.Last.OK || got.Last.Event != "test" {
		t.Errorf("channel after a test = %+v, want its last delivery", got)
	}

	// A channel that fails says why to the admin who asked, and less in the
	// activity log.
	rec.err = &notify.StatusError{Code: 404, Body: "Unknown Webhook"}
	r = send(t, h, http.MethodPost, target, laptopIP, "")
	if err := json.Unmarshal(r.Body.Bytes(), &dl); r.Code != http.StatusOK || err != nil || dl.OK || !strings.Contains(dl.Error, "Unknown Webhook") {
		t.Fatalf("POST test = %d %s, want a failure with what the server said", r.Code, r.Body)
	}
	es, err := st.Events(t.Context(), store.EventQuery{Kind: "notification", Limit: 2})
	if err != nil || len(es) != 2 || es[0].Level != "error" || es[0].Message != "The test notification to Hook failed: the server answered 404" ||
		es[0].Actor != alice || es[1].Message != "Sent a test notification to Hook" {
		t.Errorf("events = %+v, %v", es, err)
	}
	// Viewers read that, without who asked.
	var seen api.EventList
	if r := serve(h, newRequest(t, http.MethodGet, "/api/events?kind=notification", phoneIP)); r.Code != http.StatusOK {
		t.Fatalf("GET /api/events = %d", r.Code)
	} else if err := json.Unmarshal(r.Body.Bytes(), &seen); err != nil {
		t.Fatal(err)
	}
	if len(seen.Events) == 0 || slices.ContainsFunc(seen.Events, func(e api.Event) bool { return e.Actor != "" || strings.Contains(e.Message, "Unknown Webhook") }) {
		t.Errorf("events a viewer sees = %+v, want no actors and no server answers", seen.Events)
	}
}

func TestSettings_notifications(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	h := api.New(withStore(t, testTailnet(), st, api.Options{}))
	v := getSettings(t, h)
	if v.Settings.Notifications.RemindHours != 0 || v.Settings.Notifications.Quiet.Enabled || v.Settings.Notifications.Quiet.Start != "22:00" ||
		v.Settings.Notifications.Quiet.End != "07:00" || !v.Settings.Notifications.Quiet.AllowCritical {
		t.Errorf("notifications of a new Roost = %+v", v.Settings.Notifications)
	}
	patchSettings(t, h, `{"notifications": {"remindHours": 6}}`)
	v = patchSettings(t, h, `{"notifications": {"quiet": {"enabled": true, "start": "23:30", "timezone": "Europe/Berlin"}}}`)
	if got := v.Settings.Notifications; got.RemindHours != 6 || got.Quiet != (api.QuietHours{Enabled: true, Start: "23:30", End: "07:00", Timezone: "Europe/Berlin", AllowCritical: true}) {
		t.Errorf("notifications = %+v, want the two changes together", got)
	}
	saved, err := settings.Load(t.Context(), st)
	if err != nil || saved.Notifications.Quiet.Timezone != "Europe/Berlin" || saved.Notifications.Remind() != 6*time.Hour {
		t.Errorf("saved = %+v, %v", saved.Notifications, err)
	}
	for _, body := range []string{
		`{"notifications": {"remindHours": 9999}}`,
		`{"notifications": {"quiet": {"timezone": "Mars/Olympus"}}}`,
		`{"notifications": {"quiet": {"start": "7pm"}}}`,
		`{"notifications": {"quiet": {"end": "23:30"}}}`,
	} {
		if rec := send(t, h, http.MethodPatch, "/api/settings", laptopIP, body); rec.Code != http.StatusBadRequest {
			t.Errorf("PATCH %s = %d %s, want 400", body, rec.Code, rec.Body)
		}
	}
	var events api.EventList
	get(t, h, "/api/events?kind=settings", laptopIP, &events)
	var msgs []string
	for _, e := range slices.Backward(events.Events) {
		msgs = append(msgs, e.Message)
	}
	want := []string{
		"Set reminders about open alerts to every 6 hours",
		"Set quiet hours to 23:30–07:00 Europe/Berlin, with critical alerts getting through",
	}
	if !slices.Equal(msgs, want) {
		t.Errorf("activity = %q, want %q", msgs, want)
	}
}

func TestAlert_byID(t *testing.T) {
	t.Parallel()
	st := testStore(t)
	engine := alerts.NewEngine(st, nil)
	h := api.New(withStore(t, testTailnet(), st, api.Options{Alerts: engine}))
	now := time.Now()
	open, err := st.AddAlert(t.Context(), store.Alert{Key: "disk:a", Source: "metrics", Severity: "warning", Title: "Disk", OpenedAt: now, LastSeen: now})
	if err != nil {
		t.Fatal(err)
	}
	done, err := st.AddAlert(t.Context(), store.Alert{Key: "disk:b", Source: "metrics", Severity: "critical", Title: "Old", OpenedAt: now, LastSeen: now})
	if err != nil {
		t.Fatal(err)
	}
	done.ResolvedAt = now.Add(time.Minute)
	if err := st.SaveAlert(t.Context(), done); err != nil {
		t.Fatal(err)
	}
	if err := engine.Load(t.Context()); err != nil {
		t.Fatal(err)
	}

	for _, from := range []netip.Addr{laptopIP, phoneIP} {
		var a api.Alert
		get(t, h, "/api/alerts/"+strconv.FormatInt(open.ID, 10), from, &a)
		if a.ID != open.ID || a.State != api.AlertStateOpen || a.Title != "Disk" {
			t.Errorf("open alert for %v = %+v", from, a)
		}
		get(t, h, "/api/alerts/"+strconv.FormatInt(done.ID, 10), from, &a)
		if a.ID != done.ID || a.State != api.AlertStateResolved || a.ResolvedAt == nil {
			t.Errorf("resolved alert for %v = %+v", from, a)
		}
	}
	for _, target := range []string{"/api/alerts/999", "/api/alerts/abc"} {
		if rec := serve(h, newRequest(t, http.MethodGet, target, laptopIP)); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
	}
	if rec := serve(h, newRequest(t, http.MethodGet, "/api/alerts/1", tabletIP)); rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/alerts/1 by someone with no role = %d, want 403", rec.Code)
	}
}

func TestEventKindNotification(t *testing.T) {
	t.Parallel()
	if string(api.EventKindNotification) != notify.EventKind {
		t.Errorf("EventKindNotification = %q, but notify adds events of kind %q", api.EventKindNotification, notify.EventKind)
	}
}

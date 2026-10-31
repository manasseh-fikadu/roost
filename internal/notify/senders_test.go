package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var sentAt = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// opened is a message about a critical alert that opened two hours ago.
func opened() Message {
	return Message{
		Event: Opened, Severity: "critical", Title: "Disk 96% full on <laptop> & co", Detail: "12 GB left on /",
		AlertID: 12, OpenedAt: sentAt.Add(-2 * time.Hour), At: sentAt, URL: "https://roost.example.ts.net/alerts?alert=12",
		Instance: "Home lab",
	}
}

// A hook records the requests it gets, and answers each with a status.
type hook struct {
	srv *httptest.Server

	mu      sync.Mutex
	status  int
	answer  string
	headers []http.Header
	paths   []string
	bodies  [][]byte
}

func newHook(t *testing.T) *hook {
	t.Helper()
	h := &hook{status: http.StatusOK}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		defer h.mu.Unlock()
		h.headers, h.paths, h.bodies = append(h.headers, r.Header), append(h.paths, r.URL.Path), append(h.bodies, body)
		if h.status >= 300 && h.status < 400 {
			w.Header().Set("Location", "/elsewhere")
		}
		w.WriteHeader(h.status)
		_, _ = io.WriteString(w, h.answer)
	}))
	t.Cleanup(h.srv.Close)
	return h
}

// json returns the body of the only request as a map.
func (h *hook) json(t *testing.T) map[string]any {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.bodies) != 1 {
		t.Fatalf("the hook got %d requests, want 1", len(h.bodies))
	}
	var out map[string]any
	if err := json.Unmarshal(h.bodies[0], &out); err != nil {
		t.Fatalf("the request body isn't JSON: %v\n%s", err, h.bodies[0])
	}
	return out
}

func (h *hook) header(name string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.headers[0].Get(name)
}

func TestNtfy(t *testing.T) {
	t.Parallel()
	h := newHook(t)
	err := ntfy{Config{Server: h.srv.URL, Topic: "roost", Token: "tk_abc"}, NewEnv(nil)}.Send(t.Context(), opened())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	got := h.json(t)
	if got["topic"] != "roost" || got["title"] != "Critical: Disk 96% full on <laptop> & co" || got["priority"] != 5.0 ||
		got["click"] != "https://roost.example.ts.net/alerts?alert=12" {
		t.Errorf("body = %v", got)
	}
	if msg, _ := got["message"].(string); !strings.Contains(msg, "12 GB left on /") || !strings.Contains(msg, "Critical, open for 2h") ||
		!strings.Contains(msg, "alerts?alert=12") {
		t.Errorf("message = %q, want the detail, the age and the link", msg)
	}
	if tags, _ := got["tags"].([]any); len(tags) != 1 || tags[0] != "rotating_light" {
		t.Errorf("tags = %v", got["tags"])
	}
	if got := h.header("Authorization"); got != "Bearer tk_abc" {
		t.Errorf("Authorization = %q, want the token", got)
	}

	// A login is sent as basic auth, and resolving is quiet.
	h2 := newHook(t)
	m := opened()
	m.Event, m.ResolvedAt = Resolved, sentAt
	if err := (ntfy{Config{Server: h2.srv.URL + "/", Topic: "roost", Username: "alice", Password: "pw"}, NewEnv(nil)}).Send(t.Context(), m); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got := h2.header("Authorization"); got != "Basic YWxpY2U6cHc=" {
		t.Errorf("Authorization = %q, want alice:pw as basic auth", got)
	}
	if got := h2.json(t); got["priority"] != 3.0 || got["title"] != "Resolved: Disk 96% full on <laptop> & co" {
		t.Errorf("resolved body = %v", got)
	}
}

func TestNtfy_priorities(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		event    Event
		severity string
		want     int
	}{
		{Opened, "critical", 5}, {Opened, "warning", 4}, {Opened, "info", 3},
		{Reminder, "critical", 5}, {Resolved, "critical", 3}, {Test, "info", 3},
	} {
		if got := (ntfy{}).priority(Message{Event: tt.event, Severity: tt.severity}); got != tt.want {
			t.Errorf("priority(%s %s) = %d, want %d", tt.event, tt.severity, got, tt.want)
		}
	}
}

func TestDiscord(t *testing.T) {
	t.Parallel()
	h := newHook(t)
	h.status = http.StatusNoContent
	if err := (discord{Config{URL: h.srv.URL + "/api/webhooks/1/token"}, NewEnv(nil)}).Send(t.Context(), opened()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	got := h.json(t)
	embeds, _ := got["embeds"].([]any)
	if len(embeds) != 1 {
		t.Fatalf("body = %v, want one embed", got)
	}
	e, _ := embeds[0].(map[string]any)
	if e["title"] != "Critical: Disk 96% full on <laptop> & co" || e["color"] != float64(0xE5484D) || e["url"] != "https://roost.example.ts.net/alerts?alert=12" ||
		e["timestamp"] != "2026-11-01T12:00:00Z" {
		t.Errorf("embed = %v", e)
	}
	if d, _ := e["description"].(string); d != "12 GB left on /\n\nCritical, open for 2h" {
		t.Errorf("description = %q", d)
	}
	// Nothing in an alert's title can ping anyone.
	mentions, _ := got["allowed_mentions"].(map[string]any)
	if parse, ok := mentions["parse"].([]any); !ok || len(parse) != 0 {
		t.Errorf("allowed_mentions = %v, want no parse", got["allowed_mentions"])
	}
	if got["username"] != "Home lab" {
		t.Errorf("username = %v", got["username"])
	}
}

func TestSlack(t *testing.T) {
	t.Parallel()
	h := newHook(t)
	if err := (slack{Config{URL: h.srv.URL + "/services/T/B/X"}, NewEnv(nil)}).Send(t.Context(), opened()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	got := h.json(t)
	atts, _ := got["attachments"].([]any)
	if len(atts) != 1 {
		t.Fatalf("body = %v, want one attachment", got)
	}
	a, _ := atts[0].(map[string]any)
	// Slack reads <, > and & as markup.
	if a["title"] != "Critical: Disk 96% full on &lt;laptop&gt; &amp; co" || a["color"] != "#e5484d" {
		t.Errorf("attachment = %v", a)
	}
	if text, _ := a["text"].(string); !strings.Contains(text, "<https://roost.example.ts.net/alerts?alert=12|Open in Home lab>") {
		t.Errorf("text = %q, want a link", text)
	}
}

func TestTelegram(t *testing.T) {
	t.Parallel()
	h := newHook(t)
	token := "123456789:AAH-abcdefghijklmnopqrstuvwxyz0123"
	err := telegram{Config{Token: token, ChatID: "-100123"}, NewEnv(nil), h.srv.URL}.Send(t.Context(), opened())
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got := h.paths[0]; got != "/bot"+token+"/sendMessage" {
		t.Errorf("path = %q, want the bot's sendMessage", got)
	}
	got := h.json(t)
	text, _ := got["text"].(string)
	if got["chat_id"] != "-100123" || got["parse_mode"] != "HTML" ||
		!strings.Contains(text, "<b>Critical: Disk 96% full on &lt;laptop&gt; &amp; co</b>") ||
		!strings.Contains(text, `<a href="https://roost.example.ts.net/alerts?alert=12">Open in Home lab</a>`) {
		t.Errorf("body = %v", got)
	}
}

func TestWebhook(t *testing.T) {
	t.Parallel()
	h := newHook(t)
	m := opened()
	m.Event, m.ResolvedAt = Resolved, sentAt.Add(-time.Hour)
	if err := (webhook{Config{URL: h.srv.URL + "/hook", Token: "s3cret"}, NewEnv(nil)}).Send(t.Context(), m); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	var p Payload
	h.mu.Lock()
	err := json.Unmarshal(h.bodies[0], &p)
	h.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if p.Event != Resolved || p.Severity != "critical" || p.Title != m.Title || p.Detail != m.Detail || p.Instance != "Home lab" ||
		p.Alert == nil || p.Alert.ID != 12 || p.Alert.URL != m.URL || p.Alert.ResolvedAt == nil || *p.Alert.ResolvedAt != "2026-11-01T11:00:00Z" ||
		p.Alert.OpenedAt != "2026-11-01T10:00:00Z" || !strings.HasPrefix(p.Text, "Resolved: Disk 96%") {
		t.Errorf("payload = %+v, alert %+v", p, p.Alert)
	}
	if got := h.header("Authorization"); got != "Bearer s3cret" {
		t.Errorf("Authorization = %q", got)
	}
	if got := h.header("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}

	// A test has no alert in it.
	h2 := newHook(t)
	if err := (webhook{Config{URL: h2.srv.URL}, NewEnv(nil)}).Send(t.Context(), Message{Event: Test, Severity: "info", Instance: "Roost", At: sentAt}); err != nil {
		t.Fatal(err)
	}
	if got := h2.json(t); got["alert"] != nil || got["event"] != "test" {
		t.Errorf("test body = %v, want no alert", got)
	}
	if got := h2.header("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want none without a token", got)
	}
}

func TestSenders_failures(t *testing.T) {
	t.Parallel()
	t.Run("a status", func(t *testing.T) {
		t.Parallel()
		h := newHook(t)
		h.status, h.answer = http.StatusNotFound, "{\"message\":\"Unknown\nWebhook\"}"
		err := (webhook{Config{URL: h.srv.URL + "/hook/secret-token-1234"}, NewEnv(nil)}).Send(t.Context(), opened())
		var status *StatusError
		if !errors.As(err, &status) || status.Code != 404 || !strings.Contains(status.Body, "Unknown Webhook") {
			t.Fatalf("Send() error = %v, want a 404 with what the server said", err)
		}
		if got := Reason(err); got != "the server answered 404" {
			t.Errorf("Reason() = %q", got)
		}
	})
	t.Run("a redirect isn't followed", func(t *testing.T) {
		t.Parallel()
		h := newHook(t)
		h.status = http.StatusFound
		err := (webhook{Config{URL: h.srv.URL + "/hook", Token: "s3cret"}, NewEnv(nil)}).Send(t.Context(), opened())
		var status *StatusError
		if !errors.As(err, &status) || status.Code != 302 {
			t.Errorf("Send() error = %v, want the 302", err)
		}
		if len(h.paths) != 1 {
			t.Errorf("the hook got %d requests, want 1: %v", len(h.paths), h.paths)
		}
	})
	t.Run("an address that isn't there leaves out the address", func(t *testing.T) {
		t.Parallel()
		l, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		_ = l.Close()
		err = (discord{Config{URL: "http://" + addr + "/api/webhooks/1/very-secret-token"}, NewEnv(nil)}).Send(t.Context(), opened())
		if err == nil {
			t.Fatal("Send() to a closed port didn't fail")
		}
		if strings.Contains(err.Error(), "very-secret-token") || strings.Contains(err.Error(), "/api/webhooks") {
			t.Errorf("Send() error = %q, which has the webhook's address in it", err)
		}
		if got := Reason(err); got != "couldn't reach the server" {
			t.Errorf("Reason() = %q", got)
		}
	})
	t.Run("a server that is too slow", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			// The server only notices a client that left once it has read the request.
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
		}))
		t.Cleanup(srv.Close)
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		err := (webhook{Config{URL: srv.URL + "/secret-path"}, NewEnv(nil)}).Send(ctx, opened())
		if err == nil || strings.Contains(err.Error(), "secret-path") {
			t.Fatalf("Send() error = %v, want a timeout without the address", err)
		}
		if got := Reason(err); got != "it took too long" {
			t.Errorf("Reason() = %q", got)
		}
	})
}

func TestReason(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		err  error
		want string
	}{
		{&StatusError{Code: 429}, "the server answered 429"},
		{context.DeadlineExceeded, "it took too long"},
		{&net.DNSError{Name: "ntfy.internal.example", Err: "no such host"}, "couldn't find the server"},
		{&net.OpError{Op: "dial", Err: errors.New("refused")}, "couldn't reach the server"},
		{errBadConfig, "the channel's settings are wrong"},
		{errors.New("something with 10.0.0.5 in it"), "it failed"},
	} {
		if got := Reason(tt.err); got != tt.want {
			t.Errorf("Reason(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}

func TestMessage_text(t *testing.T) {
	t.Parallel()
	m := opened()
	if got, want := m.Subject(), "Critical: Disk 96% full on <laptop> & co"; got != want {
		t.Errorf("Subject() = %q, want %q", got, want)
	}
	if got, want := m.Text(), "12 GB left on /\n\nCritical, open for 2h\n\nhttps://roost.example.ts.net/alerts?alert=12"; got != want {
		t.Errorf("Text() = %q, want %q", got, want)
	}
	for _, tt := range []struct {
		event   Event
		subject string
	}{
		{Escalated, "Now critical: Disk 96% full on <laptop> & co"},
		{Reminder, "Still open, critical: Disk 96% full on <laptop> & co"},
		{Resolved, "Resolved: Disk 96% full on <laptop> & co"},
		{Test, "Test from Home lab"},
	} {
		m := opened()
		m.Event = tt.event
		if got := m.Subject(); got != tt.subject {
			t.Errorf("Subject() of %s = %q, want %q", tt.event, got, tt.subject)
		}
	}
	r := opened()
	r.Event, r.ResolvedAt, r.URL = Resolved, sentAt.Add(-30*time.Minute), ""
	if got, want := r.Text(), "12 GB left on /\n\nResolved after 1h 30m"; got != want {
		t.Errorf("Text() of a resolved alert = %q, want %q", got, want)
	}
	if got := (Message{Event: Test, Instance: "Roost"}).Text(); !strings.Contains(got, "This is a test") {
		t.Errorf("Text() of a test = %q", got)
	}
}

func TestAge(t *testing.T) {
	t.Parallel()
	for in, want := range map[time.Duration]string{
		20 * time.Second:                "under a minute",
		5 * time.Minute:                 "5m",
		time.Hour:                       "1h",
		3*time.Hour + 20*time.Minute:    "3h 20m",
		26 * time.Hour:                  "1d 2h",
		48 * time.Hour:                  "2d",
		48*time.Hour + 20*time.Minute:   "2d",
		-time.Minute:                    "under a minute",
		90*time.Minute + 29*time.Second: "1h 30m",
		3*time.Hour + 29*time.Second:    "3h",
	} {
		if got := Age(in); got != want {
			t.Errorf("Age(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestTrunc(t *testing.T) {
	t.Parallel()
	if got := trunc("héllo wörld", 6); got != "héllo…" {
		t.Errorf("trunc() = %q", got)
	}
	if got := trunc("short", 10); got != "short" {
		t.Errorf("trunc() of a short string = %q", got)
	}
	if got := oneLine("a\r\nb\tc  d"); got != "a b c  d" {
		t.Errorf("oneLine() = %q", got)
	}
}

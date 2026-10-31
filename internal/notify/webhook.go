package notify

import (
	"context"
	"time"
)

// webhook posts JSON to an address. The payload has a "text" too, which the
// webhooks of Mattermost, Rocket.Chat and many others post as the message.
type webhook struct {
	cfg Config
	env *Env
}

// Payload is what a webhook channel posts.
type Payload struct {
	// Event is why it's sent: "opened", "escalated", "reminder", "resolved"
	// or "test".
	Event Event `json:"event"`
	// Text is the message as text, to post as it is.
	Text string `json:"text"`
	// Severity is "critical", "warning" or "info".
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	// Alert is the alert the message is about. It's nil for a test.
	Alert *PayloadAlert `json:"alert,omitempty"`
	// Instance is what Roost is called.
	Instance string `json:"instance"`
	At       string `json:"at"`
}

// PayloadAlert is the alert in a Payload.
type PayloadAlert struct {
	ID         int64   `json:"id"`
	OpenedAt   string  `json:"openedAt"`
	ResolvedAt *string `json:"resolvedAt"`
	// URL is where the alert is in Roost, or "" if Roost doesn't know its
	// own address.
	URL string `json:"url"`
}

func (w webhook) Send(ctx context.Context, m Message) error {
	p := Payload{
		Event: m.Event, Text: m.Subject() + "\n\n" + m.Text(), Severity: m.Severity,
		Title: m.Title, Detail: m.Detail, Instance: m.Instance, At: m.At.UTC().Format(time.RFC3339),
	}
	if m.AlertID != 0 {
		a := &PayloadAlert{ID: m.AlertID, OpenedAt: m.OpenedAt.UTC().Format(time.RFC3339), URL: m.URL}
		if !m.ResolvedAt.IsZero() {
			s := m.ResolvedAt.UTC().Format(time.RFC3339)
			a.ResolvedAt = &s
		}
		p.Alert = a
	}
	return w.env.postJSON(ctx, w.cfg.URL, bearer(w.cfg.Token), p)
}

package notify

import (
	"context"
	"encoding/base64"
)

// ntfy publishes to a topic, with the JSON API, which carries any character
// in the title and the message.
type ntfy struct {
	cfg Config
	env *Env
}

func (n ntfy) Send(ctx context.Context, m Message) error {
	server := n.cfg.Server
	if server == "" {
		server = DefaultNtfyServer
	}
	body := map[string]any{
		"topic":    n.cfg.Topic,
		"title":    trunc(oneLine(m.Subject()), 200),
		"message":  trunc(m.Text(), 4000),
		"priority": n.priority(m),
		"tags":     n.tags(m),
	}
	if m.URL != "" {
		body["click"] = m.URL
	}
	headers := bearer(n.cfg.Token)
	if n.cfg.Username != "" {
		auth := base64.StdEncoding.EncodeToString([]byte(n.cfg.Username + ":" + n.cfg.Password))
		headers = map[string]string{"Authorization": "Basic " + auth}
	}
	return n.env.postJSON(ctx, server, headers, body)
}

// priority is ntfy's, from 1 (min) to 5 (max): a critical alert makes noise,
// and one that cleared doesn't.
func (ntfy) priority(m Message) int {
	switch {
	case m.Event == Resolved || m.Event == Test:
		return 3
	case m.Severity == "critical":
		return 5
	case m.Severity == "warning":
		return 4
	}
	return 3
}

// tags are the emoji ntfy shows in front of the title.
func (ntfy) tags(m Message) []string {
	switch {
	case m.Event == Resolved:
		return []string{"white_check_mark"}
	case m.Event == Test:
		return []string{"test_tube"}
	case m.Event == Reminder:
		return []string{"bell"}
	case m.Severity == "critical":
		return []string{"rotating_light"}
	case m.Severity == "warning":
		return []string{"warning"}
	}
	return []string{"information_source"}
}

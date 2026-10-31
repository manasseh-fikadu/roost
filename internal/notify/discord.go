package notify

import (
	"context"
	"time"
)

// discord posts an embed through a channel's webhook.
type discord struct {
	cfg Config
	env *Env
}

func (d discord) Send(ctx context.Context, m Message) error {
	embed := map[string]any{
		"title":       trunc(oneLine(m.Subject()), 256),
		"description": trunc(d.description(m), 4000),
		"color":       color(m),
		"timestamp":   m.At.UTC().Format(time.RFC3339),
		"footer":      map[string]string{"text": trunc(oneLine(m.Instance), 100)},
	}
	if m.URL != "" {
		embed["url"] = m.URL
	}
	return d.env.postJSON(ctx, d.cfg.URL, nil, map[string]any{
		"username": trunc(oneLine(m.Instance), 80),
		"embeds":   []any{embed},
		// An alert's title can name a container or a device that someone
		// called @everyone.
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
}

func (discord) description(m Message) string {
	s := m.Detail
	if f := m.Facts(); f != "" {
		if s != "" {
			s += "\n\n"
		}
		s += f
	}
	if m.Event == Test {
		s = "This is a test. If you can read it, " + m.Instance + " can reach you here."
	}
	return s
}

// color is the stripe of an embed, as a number.
func color(m Message) int {
	switch {
	case m.Event == Resolved:
		return 0x30A46C
	case m.Event == Test:
		return 0x8B8D98
	case m.Severity == "critical":
		return 0xE5484D
	case m.Severity == "warning":
		return 0xF5A524
	}
	return 0x3E63DD
}

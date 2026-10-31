package notify

import (
	"context"
	"fmt"
	"strings"
)

// slack posts to a channel through an incoming webhook.
type slack struct {
	cfg Config
	env *Env
}

var slackEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func (s slack) Send(ctx context.Context, m Message) error {
	subject := trunc(oneLine(m.Subject()), 300)
	var lines []string
	if m.Event == Test {
		lines = append(lines, slackEscape.Replace("This is a test. If you can read it, "+m.Instance+" can reach you here."))
	}
	if m.Detail != "" {
		lines = append(lines, slackEscape.Replace(trunc(m.Detail, 2500)))
	}
	if f := m.Facts(); f != "" {
		lines = append(lines, slackEscape.Replace(f))
	}
	if m.URL != "" {
		lines = append(lines, fmt.Sprintf("<%s|Open in %s>", m.URL, slackEscape.Replace(oneLine(m.Instance))))
	}
	return s.env.postJSON(ctx, s.cfg.URL, nil, map[string]any{
		// The text is what a notification shows, and the attachment is what
		// the channel shows, with a stripe of the severity's color.
		"text": subject,
		"attachments": []any{map[string]any{
			"color":     fmt.Sprintf("#%06x", color(m)),
			"title":     slackEscape.Replace(subject),
			"text":      strings.Join(lines, "\n"),
			"fallback":  subject,
			"mrkdwn_in": []string{"text"},
		}},
	})
}

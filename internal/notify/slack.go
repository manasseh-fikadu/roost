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

// slackEscape escapes what Slack reads as markup, such as <!channel>, which
// would ping everyone if a device or container were called that.
var slackEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// slackLink is a link to url, labeled, in Slack's syntax. A | in the address
// would end it early.
func slackLink(url, label string) string {
	url = strings.NewReplacer("|", "%7C", "<", "%3C", ">", "%3E").Replace(url)
	return fmt.Sprintf("<%s|%s>", strings.ReplaceAll(url, "&", "&amp;"), slackEscape.Replace(label))
}

func (s slack) Send(ctx context.Context, m Message) error {
	subject := slackEscape.Replace(trunc(oneLine(m.Subject()), 300))
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
		lines = append(lines, slackLink(m.URL, "Open in "+oneLine(m.Instance)))
	}
	return s.env.postJSON(ctx, s.cfg.URL, nil, map[string]any{
		// The text is what a notification shows, and the attachment is what
		// the channel shows, with a stripe of the severity's color.
		"text": subject,
		"attachments": []any{map[string]any{
			"color":     fmt.Sprintf("#%06x", color(m)),
			"title":     subject,
			"text":      strings.Join(lines, "\n"),
			"fallback":  subject,
			"mrkdwn_in": []string{"text"},
		}},
	})
}

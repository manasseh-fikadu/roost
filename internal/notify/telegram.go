package notify

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"strings"
)

// telegram sends a message from a bot.
type telegram struct {
	cfg Config
	env *Env
	// api is where the Bot API is.
	api string
}

func (t telegram) Send(ctx context.Context, m Message) error {
	lines := []string{"<b>" + html.EscapeString(trunc(oneLine(m.Subject()), 300)) + "</b>"}
	if m.Event == Test {
		lines = append(lines, html.EscapeString("This is a test. If you can read it, "+m.Instance+" can reach you here."))
	}
	if m.Detail != "" {
		lines = append(lines, html.EscapeString(trunc(m.Detail, 3000)))
	}
	if f := m.Facts(); f != "" {
		lines = append(lines, html.EscapeString(f))
	}
	if m.URL != "" {
		lines = append(lines, fmt.Sprintf(`<a href="%s">Open in %s</a>`, html.EscapeString(m.URL), html.EscapeString(oneLine(m.Instance))))
	}
	address := t.api + "/bot" + url.PathEscape(t.cfg.Token) + "/sendMessage"
	return t.env.postJSON(ctx, address, nil, map[string]any{
		"chat_id":                  t.cfg.ChatID,
		"text":                     strings.Join(lines, "\n\n"),
		"parse_mode":               "HTML",
		"disable_web_page_preview": true,
	})
}

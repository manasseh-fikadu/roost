package notify

import (
	"strings"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/store"
)

func TestChannel_Check(t *testing.T) {
	t.Parallel()
	ok := map[Kind]Config{
		Ntfy:     {Topic: "roost-alerts"},
		Discord:  {URL: "https://discord.com/api/webhooks/123/abc-DEF"},
		Slack:    {URL: "https://hooks.slack.com/services/T000/B000/XXXX"},
		Telegram: {Token: "123456789:AAH-abcdefghijklmnopqrstuvwxyz0123", ChatID: "-1001234567890"},
		Email:    {Host: "smtp.example.com", From: "Roost <roost@example.com>", To: []string{"alice@example.com"}},
		Webhook:  {URL: "https://example.com/hook"},
	}
	with := func(k Kind, change func(c *Config)) Config {
		c := ok[k]
		c.To = append([]string(nil), c.To...)
		change(&c)
		return c
	}
	for _, tt := range []struct {
		name    string
		kind    Kind
		cfg     Config
		wantErr string
	}{
		{"ntfy", Ntfy, ok[Ntfy], ""},
		{"ntfy on its own server", Ntfy, with(Ntfy, func(c *Config) { c.Server = "http://ntfy.tailnet.ts.net:8080" }), ""},
		{"ntfy with a token", Ntfy, with(Ntfy, func(c *Config) { c.Token = "tk_abc" }), ""},
		{"ntfy with a login", Ntfy, with(Ntfy, func(c *Config) { c.Username, c.Password = "alice", "pw" }), ""},
		{"ntfy with a token and a login", Ntfy, with(Ntfy, func(c *Config) { c.Token, c.Username, c.Password = "tk", "alice", "pw" }), "not both"},
		{"ntfy with a username only", Ntfy, with(Ntfy, func(c *Config) { c.Username = "alice" }), "needs a password"},
		{"ntfy without a topic", Ntfy, with(Ntfy, func(c *Config) { c.Topic = "" }), "topic"},
		{"ntfy with a topic that has a slash", Ntfy, with(Ntfy, func(c *Config) { c.Topic = "a/b" }), "topic"},
		{"ntfy with a server that isn't an address", Ntfy, with(Ntfy, func(c *Config) { c.Server = "ntfy.sh" }), "server"},
		{"ntfy with a server that has a login", Ntfy, with(Ntfy, func(c *Config) { c.Server = "https://a:b@ntfy.sh" }), "server"},
		{"ntfy with a token that has a line break", Ntfy, with(Ntfy, func(c *Config) { c.Token = "a\r\nX-Evil: 1" }), "printable"},
		{"discord", Discord, ok[Discord], ""},
		{"discord without an address", Discord, Config{}, "Discord gave you"},
		{"discord with a page's address", Discord, with(Discord, func(c *Config) { c.URL = "https://discord.com/channels/1/2" }), "Discord gave you"},
		{"discord over http", Discord, with(Discord, func(c *Config) { c.URL = "http://discord.com/api/webhooks/1/a" }), "Discord gave you"},
		{"discord on another host", Discord, with(Discord, func(c *Config) { c.URL = "https://discord.com.evil.example/api/webhooks/1/a" }), "Discord gave you"},
		{"discord with no token", Discord, with(Discord, func(c *Config) { c.URL = "https://discord.com/api/webhooks/" }), "Discord gave you"},
		{"slack", Slack, ok[Slack], ""},
		{"slack on another host", Slack, with(Slack, func(c *Config) { c.URL = "https://example.com/services/T/B/X" }), "Slack gave you"},
		{"telegram", Telegram, ok[Telegram], ""},
		{"telegram to a channel", Telegram, with(Telegram, func(c *Config) { c.ChatID = "@roost_alerts" }), ""},
		{"telegram with a short token", Telegram, with(Telegram, func(c *Config) { c.Token = "123:abc" }), "bot token"},
		{"telegram with a chat that isn't one", Telegram, with(Telegram, func(c *Config) { c.ChatID = "my chat" }), "chat ID"},
		{"email", Email, ok[Email], ""},
		{"email over TLS with a login", Email, with(Email, func(c *Config) { c.Security, c.Port, c.Username, c.Password = TLS, 465, "roost", "pw" }), ""},
		{"email with a login in the clear", Email, with(Email, func(c *Config) { c.Security, c.Username, c.Password = Plain, "roost", "pw" }), "TLS or STARTTLS"},
		{"email with a login in the clear on this machine", Email, with(Email, func(c *Config) { c.Host, c.Security, c.Username = "127.0.0.1", Plain, "roost" }), ""},
		{"email without a host", Email, with(Email, func(c *Config) { c.Host = "" }), "host"},
		{"email with a host that has a space", Email, with(Email, func(c *Config) { c.Host = "smtp example.com" }), "host"},
		{"email with a port that is too big", Email, with(Email, func(c *Config) { c.Port = 70000 }), "port"},
		{"email with a security it doesn't know", Email, with(Email, func(c *Config) { c.Security = "ssl" }), "STARTTLS, TLS or none"},
		{"email from nobody", Email, with(Email, func(c *Config) { c.From = "" }), "from address"},
		{"email from a line break", Email, with(Email, func(c *Config) { c.From = "roost@example.com\r\nBcc: x@example.com" }), "from address"},
		{"email to nobody", Email, with(Email, func(c *Config) { c.To = nil }), "1 to 10"},
		{"email to too many", Email, with(Email, func(c *Config) { c.To = make([]string, 11) }), "1 to 10"},
		{"email to a name", Email, with(Email, func(c *Config) { c.To = []string{"Alice <alice@example.com>"} }), "email address"},
		{"email to something that isn't an address", Email, with(Email, func(c *Config) { c.To = []string{"alice"} }), "email address"},
		{"webhook", Webhook, ok[Webhook], ""},
		{"webhook with a token", Webhook, with(Webhook, func(c *Config) { c.Token = "s3cret" }), ""},
		{"webhook to a tailnet name", Webhook, with(Webhook, func(c *Config) { c.URL = "http://homeassistant.tailnet.ts.net:8123/api/webhook/x" }), ""},
		{"webhook without an address", Webhook, Config{}, "web address"},
		{"webhook to something else", Webhook, with(Webhook, func(c *Config) { c.URL = "ftp://example.com/hook" }), "web address"},
		{"webhook with a login in the address", Webhook, with(Webhook, func(c *Config) { c.URL = "https://a:b@example.com/hook" }), "username and password"},
		{"webhook with a port that is too big", Webhook, with(Webhook, func(c *Config) { c.URL = "https://example.com:99999/hook" }), "port"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ch := Channel{Name: "Test", Kind: tt.kind, MinSeverity: "warning", Config: tt.cfg}
			err := ch.Check()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Check() error = %v, want none", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Check() error = %v, want one that says %q", err, tt.wantErr)
			}
			// What's wrong with a channel is said without repeating its
			// secrets.
			if err != nil {
				// A webhook's address is a secret from its last element on.
				_, key, _ := strings.Cut(strings.TrimPrefix(tt.cfg.URL, "https://"), "/")
				key = key[strings.LastIndex(key, "/")+1:]
				for _, secret := range []string{key, tt.cfg.Token, tt.cfg.Password} {
					if len(secret) > 6 && strings.Contains(err.Error(), secret) {
						t.Errorf("Check() error = %q, which has a secret in it", err)
					}
				}
			}
		})
	}
}

func TestChannel_Check_nameAndRouting(t *testing.T) {
	t.Parallel()
	good := Channel{Name: "Phone", Kind: Ntfy, MinSeverity: "info", Config: Config{Topic: "roost"}}
	for name, change := range map[string]func(c *Channel){
		"a name with spaces around it": func(c *Channel) { c.Name = " Phone" },
		"no name":                      func(c *Channel) { c.Name = "" },
		"a long name":                  func(c *Channel) { c.Name = strings.Repeat("é", MaxName+1) },
		"a two line name":              func(c *Channel) { c.Name = "a\nb" },
		"a kind that isn't one":        func(c *Channel) { c.Kind = "pager" },
		"a severity that isn't one":    func(c *Channel) { c.MinSeverity = "fatal" },
	} {
		c := good
		change(&c)
		if err := c.Check(); err == nil {
			t.Errorf("Check() of a channel with %s didn't fail", name)
		}
	}
	if err := good.Check(); err != nil {
		t.Errorf("Check() = %v", err)
	}
	long := good
	long.Name = strings.Repeat("é", MaxName)
	if err := long.Check(); err != nil {
		t.Errorf("Check() of a name of %d characters = %v", MaxName, err)
	}
}

func TestChannel_store(t *testing.T) {
	t.Parallel()
	c := Channel{
		ID: 3, Name: "Mail", Kind: Email, Enabled: true, MinSeverity: "critical", OnResolve: true,
		Config: Config{Host: "smtp.example.com", Port: 465, Security: TLS, Password: "pw", From: "a@example.com", To: []string{"b@example.com"}},
	}
	got, err := FromStore(c.ToStore())
	if err != nil {
		t.Fatalf("FromStore() error = %v", err)
	}
	if got.ID != 3 || got.Name != "Mail" || got.Kind != Email || !got.Enabled || got.MinSeverity != "critical" || !got.OnResolve ||
		got.Config.Host != "smtp.example.com" || got.Config.Port != 465 || got.Config.Password != "pw" || len(got.Config.To) != 1 {
		t.Errorf("FromStore(ToStore()) = %+v, want %+v", got, c)
	}
	if _, err := FromStore(store.Channel{Name: "x", Config: "{"}); err == nil {
		t.Error("FromStore() of a config that can't be read didn't fail")
	}
}

func TestSecrets(t *testing.T) {
	t.Parallel()
	c := Config{URL: "u", Password: "p"}
	for _, tt := range []struct {
		kind Kind
		want []string
	}{
		{Ntfy, []string{"token", "password"}},
		{Discord, []string{"url"}},
		{Slack, []string{"url"}},
		{Telegram, []string{"token"}},
		{Email, []string{"password"}},
		{Webhook, []string{"url", "token"}},
		{"pager", nil},
	} {
		got := Secrets(tt.kind)
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("Secrets(%q) = %v, want %v", tt.kind, got, tt.want)
		}
	}
	if !c.HasSecret("url") || !c.HasSecret("password") || c.HasSecret("token") || c.HasSecret("nope") {
		t.Error("HasSecret() doesn't say which secrets are set")
	}
}

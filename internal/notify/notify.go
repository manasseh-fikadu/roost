// Package notify sends a message to where an admin can see it when an alert
// opens, such as a phone or a chat room. A Channel is one such place, set up
// on the Settings page, and a Dispatcher decides when each channel hears
// about which alert and sends it.
package notify

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/manasseh-fikadu/roost/internal/netguard"
	"github.com/manasseh-fikadu/roost/internal/store"
)

// A Kind is a sort of place that Roost sends to.
type Kind string

// The kinds of channel.
const (
	// Ntfy publishes to a topic on ntfy.sh or a server of one's own.
	Ntfy Kind = "ntfy"
	// Discord posts to a channel through a webhook.
	Discord Kind = "discord"
	// Slack posts to a channel through an incoming webhook.
	Slack Kind = "slack"
	// Telegram sends a message from a bot.
	Telegram Kind = "telegram"
	// Email sends mail through an SMTP server.
	Email Kind = "email"
	// Webhook posts JSON to any address.
	Webhook Kind = "webhook"
)

// Kinds lists the kinds of channel.
var Kinds = []Kind{Ntfy, Discord, Slack, Telegram, Email, Webhook}

// Valid reports whether k is a kind of channel.
func (k Kind) Valid() bool { return slices.Contains(Kinds, k) }

// How a mail server is reached.
const (
	// StartTLS connects in the clear and switches to TLS, which port 587
	// does. It's the default.
	StartTLS = "starttls"
	// TLS connects over TLS from the start, which port 465 does.
	TLS = "tls"
	// Plain never uses TLS, which Roost only signs in over on the same
	// machine.
	Plain = "none"
)

// Limits of what a channel can be set up with.
const (
	// MaxName is the longest name a channel can have, in characters.
	MaxName = 40
	// MaxRecipients is how many addresses an email channel can send to.
	MaxRecipients = 10
	// DefaultNtfyServer is where an ntfy channel publishes unless it says.
	DefaultNtfyServer = "https://ntfy.sh"
)

// A Config says where a channel sends and how, with the secrets to do it.
// Each kind uses some of the fields, and Check says which.
type Config struct {
	// Server is the address of an ntfy server. It's DefaultNtfyServer if
	// empty.
	Server string `json:"server,omitempty"`
	// Topic is the ntfy topic to publish to.
	Topic string `json:"topic,omitempty"`
	// URL is the webhook's address: a secret for Discord, Slack and generic
	// webhooks, since whoever has it can post.
	URL string `json:"url,omitempty"`
	// Token is a secret: an ntfy access token, a Telegram bot's token, or
	// the bearer token that a generic webhook is sent.
	Token string `json:"token,omitempty"`
	// ChatID is the Telegram chat to send to: a number, or @name for a
	// public channel.
	ChatID string `json:"chatId,omitempty"`
	// Host, Port and Security say how to reach the mail server. A Port of 0
	// is the one that Security uses.
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Security string `json:"security,omitempty"`
	// Username and Password sign in to the mail server, or to an ntfy
	// server. Password is a secret.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// From is the address mail comes from, such as "Roost <roost@example.com>",
	// and To are the addresses it goes to.
	From string   `json:"from,omitempty"`
	To   []string `json:"to,omitempty"`
}

// The secret fields of a Config, by the name they have in the API.
const (
	SecretURL      = "url"
	SecretToken    = "token"
	SecretPassword = "password"
)

// Secrets returns the names of the fields of a Config of kind k that hold
// secrets.
func Secrets(k Kind) []string {
	switch k {
	case Ntfy:
		return []string{SecretToken, SecretPassword}
	case Discord, Slack:
		return []string{SecretURL}
	case Telegram:
		return []string{SecretToken}
	case Email:
		return []string{SecretPassword}
	case Webhook:
		return []string{SecretURL, SecretToken}
	}
	return nil
}

// HasSecret reports whether the secret field called name, such as "url", has
// a value.
func (c Config) HasSecret(name string) bool {
	switch name {
	case SecretURL:
		return c.URL != ""
	case SecretToken:
		return c.Token != ""
	case SecretPassword:
		return c.Password != ""
	}
	return false
}

// A Channel is a place that Roost sends notifications to.
type Channel struct {
	ID      int64
	Name    string
	Kind    Kind
	Enabled bool
	// MinSeverity is the least severe alert it hears about: "info",
	// "warning" or "critical".
	MinSeverity string
	// OnResolve is whether it hears when an alert resolves too.
	OnResolve bool
	Config    Config
}

// FromStore returns the channel that sc holds, or an error if its config
// can't be read, as if it was edited by hand.
func FromStore(sc store.Channel) (Channel, error) {
	c := Channel{
		ID: sc.ID, Name: sc.Name, Kind: Kind(sc.Kind), Enabled: sc.Enabled,
		MinSeverity: sc.MinSeverity, OnResolve: sc.OnResolve,
	}
	if err := json.Unmarshal([]byte(sc.Config), &c.Config); err != nil {
		return Channel{}, fmt.Errorf("read the config of the channel %q: %w", sc.Name, err)
	}
	return c, nil
}

// ToStore returns c as the store holds it.
func (c Channel) ToStore() store.Channel {
	// The database holds the channel's secrets, which only its owner can read.
	raw, _ := json.Marshal(c.Config) //nolint:errchkjson,gosec // a Config always encodes, and its secrets are meant to be stored
	return store.Channel{
		ID: c.ID, Name: c.Name, Kind: string(c.Kind), Enabled: c.Enabled,
		MinSeverity: c.MinSeverity, OnResolve: c.OnResolve, Config: string(raw),
	}
}

// Check returns an error that says, in words an admin can read, what's wrong
// with c, if anything is. It never repeats a secret.
func (c Channel) Check() error {
	switch {
	case c.Name == "" || strings.TrimSpace(c.Name) != c.Name:
		return errors.New("the name can't be empty, or start or end with a space")
	case utf8.RuneCountInString(c.Name) > MaxName:
		return fmt.Errorf("the name can be up to %d characters", MaxName)
	case strings.ContainsFunc(c.Name, unicode.IsControl):
		return errors.New("the name can only be one line")
	case !c.Kind.Valid():
		return errors.New("this isn't a kind of channel Roost knows")
	case c.MinSeverity != "info" && c.MinSeverity != "warning" && c.MinSeverity != "critical":
		return errors.New("the least severe alert must be info, warning or critical")
	}
	return c.Config.Check(c.Kind)
}

var (
	ntfyTopic = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	botToken  = regexp.MustCompile(`^[0-9]{1,20}:[A-Za-z0-9_-]{20,100}$`)
	chatID    = regexp.MustCompile(`^(-?[0-9]{1,20}|@[A-Za-z][A-Za-z0-9_]{4,31})$`)
	hostName  = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)
)

// Check returns an error that says what's wrong with c, for a channel of kind
// k, if anything is.
func (c Config) Check(k Kind) error {
	switch k {
	case Ntfy:
		return c.checkNtfy()
	case Discord:
		return checkWebhook(c.URL, "Discord", "/api/webhooks/", "discord.com", "discordapp.com", "ptb.discord.com", "canary.discord.com")
	case Slack:
		return checkWebhook(c.URL, "Slack", "/services/", "hooks.slack.com", "hooks.slack-gov.com")
	case Telegram:
		switch {
		case !botToken.MatchString(c.Token):
			return errors.New("the bot token should look like 123456789:ABC-def…, as BotFather gave it")
		case !chatID.MatchString(c.ChatID):
			return errors.New("the chat ID must be a number, or @name for a public channel")
		}
		return nil
	case Email:
		return c.checkEmail()
	case Webhook:
		if err := checkURL(c.URL); err != nil {
			return fmt.Errorf("the webhook's address: %w", err)
		}
		return checkHeaderValue(c.Token, "the token")
	}
	return errors.New("this isn't a kind of channel Roost knows")
}

func (c Config) checkNtfy() error {
	if c.Server != "" {
		if err := checkURL(c.Server); err != nil {
			return fmt.Errorf("the server's address: %w", err)
		}
		if u, _ := url.Parse(c.Server); u.RawQuery != "" || u.Fragment != "" {
			return errors.New("the server's address can't have a query or a fragment")
		}
	}
	switch {
	case !ntfyTopic.MatchString(c.Topic):
		return errors.New("the topic can have letters, digits, - and _, up to 64 of them")
	case c.Token != "" && c.Username != "":
		return errors.New("sign in with an access token or with a username, not both")
	case (c.Username == "") != (c.Password == ""):
		return errors.New("a username needs a password, and a password needs a username")
	}
	if err := checkHeaderValue(c.Token, "the access token"); err != nil {
		return err
	}
	return checkHeaderValue(c.Username, "the username")
}

// checkWebhook checks the address of a chat service's webhook: it must be
// the service's own, on one of hosts and under prefix, so a typo or a pasted
// page address is caught.
func checkWebhook(raw, service, prefix string, hosts ...string) error {
	u, err := url.Parse(raw)
	if raw == "" || err != nil || u.Scheme != "https" || !slices.Contains(hosts, strings.ToLower(u.Hostname())) ||
		u.Port() != "" || u.User != nil || !strings.HasPrefix(u.Path, prefix) || len(u.Path) == len(prefix) ||
		u.Fragment != "" {
		return fmt.Errorf("the webhook's address should be the one %s gave you: https://%s%s…", service, hosts[0], prefix)
	}
	return nil
}

// checkURL checks that s is the address of a web server: with a scheme of
// http or https, a host, and no login in it.
func checkURL(s string) error {
	u, err := url.Parse(s)
	switch {
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Opaque != "":
		return errors.New("must be a web address, such as https://example.com/hook")
	case u.User != nil:
		return errors.New("can't have a username and password in it")
	case !validHost(u.Hostname()):
		return errors.New("has a host that isn't a name or an IP address")
	case netguard.ForbiddenHost(u.Hostname()):
		return errors.New("is a link-local or cloud metadata address, which Roost doesn't connect to")
	}
	if p := u.Port(); p != "" {
		if n := atoi(p); n < 1 || n > 65535 {
			return errors.New("has a port that isn't from 1 to 65535")
		}
	}
	return nil
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' || n > 1<<20 {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// validHost reports whether host, without brackets, is a DNS name or an IP
// address without a zone.
func validHost(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Zone() == ""
	}
	return len(host) <= 253 && hostName.MatchString(host)
}

// checkHeaderValue checks that s can go in an HTTP header.
func checkHeaderValue(s, what string) error {
	for _, r := range s {
		if r < ' ' || r > '~' {
			return fmt.Errorf("%s can only have printable ASCII characters", what)
		}
	}
	return nil
}

func (c Config) checkEmail() error {
	switch c.Security {
	case "", StartTLS, TLS, Plain:
	default:
		return errors.New("the connection must be STARTTLS, TLS or none")
	}
	switch {
	case !validHost(c.Host):
		return errors.New("the mail server's host must be a name or an IP address")
	case c.Port < 0 || c.Port > 65535:
		return errors.New("the port must be from 1 to 65535")
	case c.Password != "" && c.Username == "":
		return errors.New("a password needs a username")
	case c.Username != "" && c.Security == Plain && !localHost(c.Host):
		return errors.New("signing in needs TLS or STARTTLS, unless the mail server is on the same machine as Roost")
	case strings.ContainsFunc(c.Username+c.Password, unicode.IsControl):
		return errors.New("the username and password can only be one line")
	}
	if _, err := mail.ParseAddress(c.From); err != nil || strings.ContainsAny(c.From, "\r\n") {
		return errors.New("the from address isn't an email address, such as roost@example.com")
	}
	if len(c.To) == 0 || len(c.To) > MaxRecipients {
		return fmt.Errorf("send mail to 1 to %d addresses", MaxRecipients)
	}
	for _, to := range c.To {
		if a, err := mail.ParseAddress(to); err != nil || a.Name != "" || strings.ContainsAny(to, "\r\n") {
			return errors.New("every address to send mail to must be an email address, such as alice@example.com")
		}
	}
	return nil
}

// localHost reports whether host is the machine Roost runs on.
func localHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// DialFunc connects to an address, like net.Dialer.DialContext.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Normalize returns c as a channel of kind k keeps it: without the fields
// that kind doesn't use, and without the spaces around the ones it does. A
// password is left as it is, since a space can be part of one.
func (c Config) Normalize(k Kind) Config {
	t := strings.TrimSpace
	var out Config
	switch k {
	case Ntfy:
		out = Config{Server: t(c.Server), Topic: t(c.Topic), Token: t(c.Token), Username: t(c.Username), Password: c.Password}
	case Discord, Slack:
		out = Config{URL: t(c.URL)}
	case Telegram:
		out = Config{Token: t(c.Token), ChatID: t(c.ChatID)}
	case Email:
		out = Config{
			Host: t(c.Host), Port: c.Port, Security: t(c.Security), Username: t(c.Username), Password: c.Password,
			From: t(c.From),
		}
		for _, to := range c.To {
			if to = t(to); to != "" {
				out.To = append(out.To, to)
			}
		}
	case Webhook:
		out = Config{URL: t(c.URL), Token: t(c.Token)}
	}
	return out
}

// Summary says where c sends, without any secret, such as "ntfy.sh/roost".
func (c Channel) Summary() string {
	cfg := c.Config
	switch c.Kind {
	case Ntfy:
		server := cmp.Or(cfg.Server, DefaultNtfyServer)
		if u, err := url.Parse(server); err == nil && u.Host != "" {
			server = u.Host + strings.TrimSuffix(u.Path, "/")
		}
		return server + "/" + cfg.Topic
	case Discord:
		return "Discord webhook"
	case Slack:
		return "Slack webhook"
	case Telegram:
		return "Telegram chat " + cfg.ChatID
	case Email:
		return strings.Join(cfg.To, ", ") + " via " + cfg.Host
	case Webhook:
		// The rest of the address can hold a token.
		if u, err := url.Parse(cfg.URL); err == nil {
			return u.Scheme + "://" + u.Host
		}
	}
	return ""
}

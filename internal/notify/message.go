package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// An Event says why a message is sent.
type Event string

// The reasons to send a message.
const (
	// Opened is an alert that opened.
	Opened Event = "opened"
	// Escalated is an alert that got more severe than the channel last heard.
	Escalated Event = "escalated"
	// Reminder is an alert that's still open, and that nobody has
	// acknowledged.
	Reminder Event = "reminder"
	// Resolved is an alert that cleared.
	Resolved Event = "resolved"
	// Test is a message an admin asked for, to see that the channel works.
	Test Event = "test"
)

// A Message is what a channel sends about an alert.
type Message struct {
	Event Event
	// Severity is the alert's: "critical", "warning" or "info".
	Severity string
	// Title and Detail say what's wrong, such as "Disk 91% full on laptop".
	Title, Detail string
	// AlertID is the alert's ID, or 0 for a test.
	AlertID    int64
	OpenedAt   time.Time
	ResolvedAt time.Time
	// At is when the message is sent.
	At time.Time
	// URL is where the alert is in Roost, or "" if Roost doesn't know its
	// own address.
	URL string
	// Instance is what Roost is called.
	Instance string
}

// Subject is a line that says what the message is about, such as "Critical:
// Disk 91% full on laptop".
func (m Message) Subject() string {
	switch m.Event {
	case Resolved:
		return "Resolved: " + m.Title
	case Test:
		return "Test from " + m.Instance
	case Reminder:
		return "Still open, " + m.Severity + ": " + m.Title
	case Escalated:
		return "Now " + m.Severity + ": " + m.Title
	default:
		return capitalize(m.Severity) + ": " + m.Title
	}
}

// Text is the message as plain text, without its subject.
func (m Message) Text() string {
	var lines []string
	if m.Event == Test {
		lines = append(lines, "This is a test. If you can read it, "+m.Instance+" can reach you here.")
	}
	if m.Detail != "" {
		lines = append(lines, m.Detail)
	}
	if s := m.Facts(); s != "" {
		lines = append(lines, s)
	}
	if m.URL != "" {
		lines = append(lines, m.URL)
	}
	return strings.Join(lines, "\n\n")
}

// Facts is a line about the alert's age, such as "Warning, open for 3h 20m".
func (m Message) Facts() string {
	switch m.Event {
	case Resolved:
		if m.OpenedAt.IsZero() || m.ResolvedAt.IsZero() {
			return "Resolved"
		}
		return "Resolved after " + Age(m.ResolvedAt.Sub(m.OpenedAt))
	case Test:
		return ""
	default:
		if m.OpenedAt.IsZero() || m.At.Before(m.OpenedAt) {
			return capitalize(m.Severity)
		}
		return capitalize(m.Severity) + ", open for " + Age(m.At.Sub(m.OpenedAt))
	}
}

// Age writes d in its two largest units, such as 3h 20m or 2d 4h.
func Age(d time.Duration) string {
	d = d.Round(time.Minute)
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("%dm", d/time.Minute)
	case d < 24*time.Hour:
		h, m := d/time.Hour, d%time.Hour/time.Minute
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	}
	days, h := d/(24*time.Hour), d%(24*time.Hour)/time.Hour
	if h == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd %dh", days, h)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// oneLine replaces the line breaks and other control characters in s with
// spaces, for a place that has room for one line, such as a mail's subject.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsControl), " ")
}

// trunc cuts s to n characters at most, ending it with … if it cut.
func trunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimRightFunc(string(r[:n-1]), unicode.IsSpace) + "…"
}

// A StatusError means a server answered with a status that isn't a success.
type StatusError struct {
	Code int
	// Body is the start of what the server said.
	Body string
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("the server answered %d", e.Code)
	}
	return fmt.Sprintf("the server answered %d: %s", e.Code, e.Body)
}

// Reason says, in a few words and without the addresses or secrets in
// err, why a message didn't go, for the activity log, which viewers read.
func Reason(err error) string {
	var (
		status *StatusError
		smtp   *textproto.Error
		dns    *net.DNSError
		net_   net.Error
		cert   *tls.CertificateVerificationError
	)
	switch {
	case errors.As(err, &status):
		return fmt.Sprintf("the server answered %d", status.Code)
	case errors.As(err, &smtp):
		return fmt.Sprintf("the mail server answered %d", smtp.Code)
	case errors.Is(err, context.DeadlineExceeded):
		return "it took too long"
	case errors.As(err, &cert):
		return "the server's certificate isn't trusted"
	case errors.As(err, &dns):
		return "couldn't find the server"
	case errors.As(err, &net_):
		return "couldn't reach the server"
	case errors.Is(err, errBadConfig):
		return "the channel's settings are wrong"
	}
	return "it failed"
}

// errBadConfig marks an error that comes from a channel that can't send
// because of how it's set up.
var errBadConfig = errors.New("the channel can't send")

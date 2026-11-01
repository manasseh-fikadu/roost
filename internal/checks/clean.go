package checks

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/manasseh-fikadu/bield/internal/netguard"
	"github.com/manasseh-fikadu/bield/internal/store"
)

// What a check can be.
const (
	MinInterval = 10 * time.Second
	MaxInterval = 24 * time.Hour
	MinTimeout  = 200 * time.Millisecond
	MaxTimeout  = time.Minute
	// The longest a check's name, group and target can be, in characters.
	maxName   = 80
	maxGroup  = 40
	maxTarget = 2000
)

// What a check gets if it leaves them out.
const (
	DefaultInterval = time.Minute
	DefaultTimeout  = 5 * time.Second
	DefaultGroup    = "Custom"
)

// Clean checks a check that an admin set up, fills in the defaults for what
// it leaves out, and returns it as the store keeps it. Its ID and when it
// was created are left as they are. The error says what's wrong, for the
// admin.
func Clean(c store.Check) (store.Check, error) {
	c.Name, c.Group, c.Target = strings.TrimSpace(c.Name), strings.TrimSpace(c.Group), strings.TrimSpace(c.Target)
	if c.Interval == 0 {
		c.Interval = DefaultInterval
	}
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeout
	}
	if c.Group == "" {
		c.Group = DefaultGroup
	}
	if err := oneLine("name", c.Name, maxName); err != nil {
		return store.Check{}, err
	}
	if err := oneLine("group", c.Group, maxGroup); err != nil {
		return store.Check{}, err
	}
	switch {
	case c.Name == "":
		return store.Check{}, errors.New("the check needs a name")
	case c.Target == "":
		return store.Check{}, errors.New("the check needs a target")
	case utf8.RuneCountInString(c.Target) > maxTarget:
		return store.Check{}, errors.New("the target is too long")
	case c.Interval < MinInterval || c.Interval > MaxInterval:
		return store.Check{}, errors.New("the interval must be from 10 seconds to 24 hours")
	case c.Timeout < MinTimeout || c.Timeout > MaxTimeout:
		return store.Check{}, errors.New("the timeout must be from 200 ms to 60 seconds")
	case c.Timeout >= c.Interval:
		return store.Check{}, errors.New("the timeout must be shorter than the interval")
	}

	var err error
	switch c.Kind {
	case KindHTTP:
		err = checkURL(c.Target)
		if c.ExpectStatus != 0 && (c.ExpectStatus < 100 || c.ExpectStatus > 599) {
			err = errors.New("the expected status must be from 100 to 599")
		}
	case KindTCP:
		err = checkHostPort(c.Target)
		c.ExpectStatus, c.Insecure = 0, false
	case KindDNS:
		if !hostName(c.Target) {
			err = errors.New("the target must be a host name, such as example.com")
		}
		c.ExpectStatus, c.Insecure = 0, false
	default:
		err = errors.New("the check must be an HTTP, TCP or DNS check")
	}
	if err != nil {
		return store.Check{}, err
	}
	return c, nil
}

// oneLine checks that a field is one line, and at most n characters long.
func oneLine(field, s string, n int) error {
	switch {
	case utf8.RuneCountInString(s) > n:
		return errors.New("the " + field + " is too long")
	case strings.ContainsFunc(s, unicode.IsControl):
		return errors.New("the " + field + " can only be one line")
	}
	return nil
}

func checkURL(target string) error {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		strings.ContainsFunc(target, unicode.IsSpace) {
		return errors.New("the target must be a full http or https URL, such as https://example.com/health")
	}
	if host := u.Hostname(); !hostName(host) && !isIP(host) {
		return errors.New("the URL's host isn't a host name or an IP address")
	} else if netguard.ForbiddenHost(host) {
		return errForbiddenTarget
	}
	if p := u.Port(); p != "" && !validPort(p) {
		return errors.New("the URL's port must be a number from 1 to 65535")
	}
	return nil
}

func checkHostPort(target string) error {
	host, port, err := net.SplitHostPort(target)
	switch {
	case err != nil:
		return errors.New("the target must be a host and a port, such as db.example.com:5432")
	case !hostName(host) && !isIP(host):
		return errors.New("the target's host isn't a host name or an IP address")
	case !validPort(port):
		return errors.New("the port must be a number from 1 to 65535")
	case netguard.ForbiddenHost(host):
		return errForbiddenTarget
	}
	return nil
}

// errForbiddenTarget is why a check can't aim at a link-local address. A check
// that reached a cloud's metadata service would show, by its status, that the
// credentials are there to take.
var errForbiddenTarget = errors.New("the target is a link-local or cloud metadata address, which Bield doesn't connect to")

func validPort(p string) bool {
	n, err := strconv.ParseUint(p, 10, 16)
	return err == nil && n > 0
}

func isIP(s string) bool {
	_, err := netip.ParseAddr(s)
	return err == nil
}

// hostName reports whether s is a host name, such as example.com or db,
// which may end in a dot. An IP address isn't one.
func hostName(s string) bool {
	s = strings.TrimSuffix(s, ".")
	if s == "" || len(s) > 253 || isIP(s) {
		return false
	}
	for label := range strings.SplitSeq(s, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		if strings.ContainsFunc(label, notHostChar) {
			return false
		}
	}
	return true
}

// notHostChar reports whether r can't be in a host name's label. Underscores
// can, as in _dmarc.example.com.
func notHostChar(r rune) bool {
	return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_'
}

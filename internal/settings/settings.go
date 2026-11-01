// Package settings holds what admins can change about how Roost watches the
// tailnet, such as when an alert opens, and what it's called. The store keeps
// them, and the Settings page (#10) changes them. Until an admin does, they're
// Defaults.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	// The notifications' quiet hours can name any time zone, and a minimal
	// container has no database of them.
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"

	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/systemd"
)

// kvKey is the setting in the store's kv table that holds them, as JSON.
const kvKey = "settings"

// MaxName is the longest name Roost can have, in characters.
const MaxName = 40

// Settings are what admins can change.
type Settings struct {
	General    General    `json:"general"`
	Thresholds Thresholds `json:"thresholds"`
	// Rules say which alerts Roost raises.
	Rules Rules `json:"rules"`
	// WatchedUnits are the systemd units on the machine Roost runs on that
	// it watches, such as "ssh.service", or "user:app.service" for a user
	// unit of the user Roost runs as. Roost always watches failed units.
	WatchedUnits  []string      `json:"watchedUnits"`
	Retention     Retention     `json:"retention"`
	Notifications Notifications `json:"notifications"`
}

// General is how Roost presents itself.
type General struct {
	// Name is shown in the header, and in the title of every page.
	Name string `json:"name"`
	// PublicURL is the address that people and agents reach Roost at, such
	// as https://roost.example.ts.net, if it isn't the one the app was
	// opened at. It's used in the links and commands Roost writes. It's ""
	// to use the address of the request.
	PublicURL string `json:"publicUrl"`
	// SetupDone is whether an admin finished or skipped the first-run setup,
	// which the app shows admins until then.
	SetupDone bool `json:"setupDone"`
}

// Thresholds are when a device needs attention, and when an alert opens.
type Thresholds struct {
	// DiskWarn and DiskCritical are how full a disk can get, in percent.
	DiskWarn     float64 `json:"diskWarn"`
	DiskCritical float64 `json:"diskCritical"`
	// MemoryWarn is how much memory can be used, in percent.
	MemoryWarn float64 `json:"memoryWarn"`
	// CPUWarn is how busy the CPUs can be, in percent, on average over
	// CPUWindowMinutes.
	CPUWarn float64 `json:"cpuWarn"`
	// CPUWindowMinutes is how long the CPUs must stay busy to count.
	CPUWindowMinutes int `json:"cpuWindowMinutes"`
	// OfflineMinutes is how long a device can be offline before an alert,
	// if it alerts when it's offline.
	OfflineMinutes int `json:"offlineMinutes"`
	// KeyExpiryDays is how soon before a device's Tailscale key expires to
	// warn about it.
	KeyExpiryDays int `json:"keyExpiryDays"`
	// CheckFailures is how many times in a row a check must fail before an
	// alert.
	CheckFailures int `json:"checkFailures"`
}

// OfflineAfter is OfflineMinutes as a duration.
func (t Thresholds) OfflineAfter() time.Duration {
	return time.Duration(t.OfflineMinutes) * time.Minute
}

// KeyExpiryWarn is KeyExpiryDays as a duration.
func (t Thresholds) KeyExpiryWarn() time.Duration {
	return time.Duration(t.KeyExpiryDays) * 24 * time.Hour
}

// CPUWindow is CPUWindowMinutes as a duration.
func (t Thresholds) CPUWindow() time.Duration {
	return time.Duration(t.CPUWindowMinutes) * time.Minute
}

// MaxCPUWindow is the longest CPU window, which is how much of the CPU's
// past Roost keeps.
const MaxCPUWindow = time.Hour

// Rules say which alerts Roost raises. A device that a rule is off for isn't
// flagged for it in the dashboard either.
type Rules struct {
	// Offline alerts when a device has been off the tailnet for the offline
	// threshold.
	Offline bool `json:"offline"`
	// KeyExpiry alerts when a device's Tailscale key is about to expire.
	KeyExpiry bool `json:"keyExpiry"`
	// AgentStopped alerts when a device's agent stops reporting.
	AgentStopped bool `json:"agentStopped"`
	// Disk, Memory and CPU alert when a device passes its threshold.
	Disk   bool `json:"disk"`
	Memory bool `json:"memory"`
	CPU    bool `json:"cpu"`
	// Checks alerts when a check keeps failing.
	Checks bool `json:"checks"`
	// Containers alerts when a Docker container on Roost's machine stops
	// unexpectedly.
	Containers bool `json:"containers"`
	// Units alerts when a systemd unit fails, or a watched one isn't
	// running.
	Units bool `json:"units"`
	// Collectors alerts when one of Roost's own jobs keeps failing.
	Collectors bool `json:"collectors"`
}

// Retention is how many days Roost keeps each kind of history. It lasts as
// long as the day count says, and Roost deletes what's older within the hour.
type Retention struct {
	// ActivityDays is how long the activity log and resolved alerts last.
	ActivityDays int `json:"activityDays"`
	// HistoryDays is how long what devices used, and the round-trip times to
	// them, last, in buckets of an hour. What used their CPU, and what their
	// containers used, last 7 days at most.
	HistoryDays int `json:"historyDays"`
	// ChecksDays is how long the results of checks, and the health score,
	// last.
	ChecksDays int `json:"checksDays"`
}

// Notifications are when Roost sends the notifications that its channels
// send. Which alerts a channel hears about is up to the channel.
type Notifications struct {
	// RemindHours is how many hours after the last notification about an
	// alert Roost sends another, while the alert is open and nobody has
	// acknowledged or snoozed it. 0 sends none.
	RemindHours int `json:"remindHours"`
	// Quiet holds notifications back at night.
	Quiet QuietHours `json:"quiet"`
}

// MaxRemindHours is the longest time between reminders: a week.
const MaxRemindHours = 7 * 24

// Remind is RemindHours as a duration, or 0 for no reminders.
func (n Notifications) Remind() time.Duration {
	return time.Duration(n.RemindHours) * time.Hour
}

// QuietHours are the hours of the day when Roost holds notifications back,
// and sends them when the hours end if the alert they're about is still
// there, and nobody has acknowledged it.
type QuietHours struct {
	Enabled bool `json:"enabled"`
	// Start and End are times of the day, such as "22:00" and "07:00". The
	// hours run from Start until End, through midnight if End is earlier.
	Start string `json:"start"`
	End   string `json:"end"`
	// Timezone is the name of the time zone the times are in, such as
	// "Europe/Berlin". It's "" for the one that Roost's machine is in.
	Timezone string `json:"timezone"`
	// AllowCritical sends notifications about critical alerts during the
	// quiet hours too.
	AllowCritical bool `json:"allowCritical"`
}

// Active reports whether t is in the quiet hours.
func (q QuietHours) Active(t time.Time) bool {
	start, ok1 := clock(q.Start)
	end, ok2 := clock(q.End)
	if !q.Enabled || !ok1 || !ok2 || start == end {
		return false
	}
	loc, err := q.location()
	if err != nil {
		loc = time.Local
	}
	t = t.In(loc)
	now := t.Hour()*60 + t.Minute()
	if start < end {
		return now >= start && now < end
	}
	return now >= start || now < end
}

func (q QuietHours) location() (*time.Location, error) {
	if q.Timezone == "" {
		return time.Local, nil
	}
	return time.LoadLocation(q.Timezone)
}

// clock returns the minutes since midnight of a time of day written as
// "15:04", and whether it is one.
func clock(s string) (int, bool) {
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != len("15:04") {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}

// Limits of a retention, in days.
const (
	MaxActivityDays = 365
	MaxHistoryDays  = 90
	MaxChecksDays   = 90
)

// Keep returns r for the store.
func (r Retention) Keep() store.Retention {
	const day = 24 * time.Hour
	return store.Retention{
		Activity: time.Duration(r.ActivityDays) * day,
		History:  time.Duration(r.HistoryDays) * day,
		Checks:   time.Duration(r.ChecksDays) * day,
	}
}

// Defaults returns the settings a new hub starts with.
func Defaults() Settings {
	return Settings{
		General: General{Name: "Roost"},
		Thresholds: Thresholds{
			DiskWarn:         85,
			DiskCritical:     95,
			MemoryWarn:       92,
			CPUWarn:          90,
			CPUWindowMinutes: 5,
			OfflineMinutes:   10,
			KeyExpiryDays:    14,
			CheckFailures:    2,
		},
		Rules: Rules{
			Offline: true, KeyExpiry: true, AgentStopped: true, Disk: true, Memory: true, CPU: true,
			Checks: true, Containers: true, Units: true, Collectors: true,
		},
		// ssh.service is sshd.service on some systems. Roost leaves out the
		// ones a machine doesn't have.
		WatchedUnits: []string{"ssh.service", "sshd.service", "docker.service", "tailscaled.service", "cron.service"},
		Retention:    Retention{ActivityDays: 30, HistoryDays: 7, ChecksDays: 7},
		Notifications: Notifications{
			Quiet: QuietHours{Start: "22:00", End: "07:00", AllowCritical: true},
		},
	}
}

// ErrCorrupt means the saved settings can't be read, as if they were edited
// by hand.
var ErrCorrupt = errors.New("can't read the saved settings")

// Load returns the settings in st: what an admin saved, and the defaults
// for anything they didn't.
func Load(ctx context.Context, st *store.Store) (Settings, error) {
	s := Defaults()
	raw, ok, err := st.KV(ctx, kvKey)
	if err != nil || !ok {
		return s, err
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Defaults(), fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	return s, nil
}

// StoreRetention returns how long the store keeps history, for
// store.Options. It fails if the settings can't be read, so the store keeps
// everything.
func StoreRetention(ctx context.Context, st *store.Store) (store.Retention, error) {
	s, err := Load(ctx, st)
	if err != nil {
		return store.Retention{}, err
	}
	return s.Retention.Keep(), nil
}

// Save checks s and saves it in st.
func Save(ctx context.Context, st *store.Store, s Settings) error {
	if err := s.Check(); err != nil {
		return err
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return st.SetKV(ctx, kvKey, string(raw))
}

// An InvalidError says why settings aren't allowed, in words an admin can
// read.
type InvalidError struct{ Err error }

func (e *InvalidError) Error() string { return e.Err.Error() }
func (e *InvalidError) Unwrap() error { return e.Err }

// Check returns an *InvalidError that says what's wrong with s, if anything
// is.
func (s Settings) Check() error {
	for _, err := range []error{s.General.Check(), s.Thresholds.Check(), checkUnits(s.WatchedUnits), s.Retention.Check(), s.Notifications.Check()} {
		if err != nil {
			return &InvalidError{err}
		}
	}
	return nil
}

// Check returns an error that says what's wrong with g, if anything is.
func (g General) Check() error {
	switch {
	case g.Name == "" || strings.TrimSpace(g.Name) != g.Name:
		return errors.New("the name can't be empty, or start or end with a space")
	case utf8.RuneCountInString(g.Name) > MaxName:
		return fmt.Errorf("the name can be up to %d characters", MaxName)
	case strings.ContainsFunc(g.Name, unicode.IsControl):
		return errors.New("the name can only be one line")
	}
	return checkPublicURL(g.PublicURL)
}

// CleanPublicURL returns the address of a public URL that an admin typed:
// without spaces around it or a slash at its end. It fails if it isn't a web
// address that Roost can write links with.
func CleanPublicURL(s string) (string, error) {
	s = strings.TrimSuffix(strings.TrimSpace(s), "/")
	return s, checkPublicURL(s)
}

func checkPublicURL(s string) error {
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	switch {
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Opaque != "":
		return errors.New("the public URL must be a web address, such as https://roost.example.ts.net")
	case u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.HasSuffix(s, "/"):
		return errors.New("the public URL can only be a scheme, a host and a port, such as https://roost.example.ts.net:8443")
	case !validHost(u.Hostname()) || !validPort(u):
		return errors.New("the public URL's host must be a name or an IP address, and its port a number from 1 to 65535")
	}
	return nil
}

// hostName is a DNS name: labels of letters, digits and hyphens.
var hostName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`)

// validHost reports whether host, without brackets, is a DNS name or an IP
// address without a zone.
func validHost(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.Zone() == ""
	}
	return len(host) <= 253 && hostName.MatchString(host)
}

// validPort reports whether u has no port, or a port from 1 to 65535.
func validPort(u *url.URL) bool {
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	port := u.Port()
	if port == "" {
		return true
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

// Check returns an error that says what's wrong with t, if anything is.
func (t Thresholds) Check() error {
	for _, p := range []struct {
		name string
		v    float64
	}{{"disk warning", t.DiskWarn}, {"critical disk", t.DiskCritical}, {"memory", t.MemoryWarn}, {"CPU", t.CPUWarn}} {
		if !(p.v > 0 && p.v <= 100) {
			return fmt.Errorf("the %s threshold must be a percentage above 0", p.name)
		}
	}
	switch {
	case t.DiskCritical < t.DiskWarn:
		return errors.New("a disk can't be critical before it needs a warning")
	case t.CPUWindowMinutes < 1 || t.CPUWindowMinutes > int(MaxCPUWindow/time.Minute):
		return errors.New("the time the CPU must stay busy must be from a minute to an hour")
	case t.OfflineMinutes < 1 || t.OfflineMinutes > 7*24*60:
		return errors.New("the time a device can be offline must be from a minute to a week")
	case t.KeyExpiryDays < 1 || t.KeyExpiryDays > 365:
		return errors.New("the warning before a key expires must be from a day to a year")
	case t.CheckFailures < 1 || t.CheckFailures > 100:
		return errors.New("the failures in a row before an alert must be from 1 to 100")
	}
	return nil
}

func checkUnits(units []string) error {
	for _, u := range units {
		if !systemd.ValidName(strings.TrimPrefix(u, "user:")) {
			return fmt.Errorf("%q isn't the name of a systemd unit", u)
		}
	}
	return nil
}

// Check returns an error that says what's wrong with r, if anything is.
func (r Retention) Check() error {
	for _, p := range []struct {
		name     string
		got, max int
	}{
		{"the activity log", r.ActivityDays, MaxActivityDays},
		{"the history of devices", r.HistoryDays, MaxHistoryDays},
		{"the results of checks", r.ChecksDays, MaxChecksDays},
	} {
		if p.got < 1 || p.got > p.max {
			return fmt.Errorf("the days to keep %s must be from 1 to %d", p.name, p.max)
		}
	}
	return nil
}

// Check returns an error that says what's wrong with n, if anything is.
func (n Notifications) Check() error {
	if n.RemindHours < 0 || n.RemindHours > MaxRemindHours {
		return fmt.Errorf("the hours between reminders must be from 1 to %d, or 0 for none", MaxRemindHours)
	}
	return n.Quiet.Check()
}

// Check returns an error that says what's wrong with q, if anything is.
func (q QuietHours) Check() error {
	start, ok1 := clock(q.Start)
	end, ok2 := clock(q.End)
	switch {
	case !ok1 || !ok2:
		return errors.New("the quiet hours must start and end at a time of the day, such as 22:00")
	case start == end:
		return errors.New("the quiet hours can't start when they end")
	}
	if _, err := q.location(); err != nil {
		return fmt.Errorf("%q isn't the name of a time zone, such as Europe/Berlin", q.Timezone)
	}
	return nil
}

// For returns t with the thresholds of one device in place of it. A critical
// disk threshold below the warning one, which can follow from changing
// either, becomes the warning one.
func (t Thresholds) For(o store.ThresholdOverrides) Thresholds {
	if o.DiskWarn != nil {
		t.DiskWarn = *o.DiskWarn
	}
	if o.DiskCritical != nil {
		t.DiskCritical = *o.DiskCritical
	}
	if o.MemoryWarn != nil {
		t.MemoryWarn = *o.MemoryWarn
	}
	if o.CPUWarn != nil {
		t.CPUWarn = *o.CPUWarn
	}
	if o.OfflineMinutes != nil {
		t.OfflineMinutes = *o.OfflineMinutes
	}
	t.DiskCritical = max(t.DiskCritical, t.DiskWarn)
	return t
}

// CheckOverrides returns an error that says what's wrong with the
// thresholds o of one device, if anything is, given the settings' t.
func (t Thresholds) CheckOverrides(o store.ThresholdOverrides) error {
	// The CPU window and the rest aren't the device's to set.
	if err := t.For(o).Check(); err != nil {
		return err
	}
	// A critical level the admin typed must not hide behind the warning one.
	if o.DiskWarn != nil && o.DiskCritical != nil && *o.DiskCritical < *o.DiskWarn {
		return errors.New("a disk can't be critical before it needs a warning")
	}
	return nil
}

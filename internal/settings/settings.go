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
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/manasseh-fikadu/roost/internal/store"
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
	WatchedUnits []string  `json:"watchedUnits"`
	Retention    Retention `json:"retention"`
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
	// HistoryDays is how long what devices and containers used, and the
	// round-trip times to devices, last.
	HistoryDays int `json:"historyDays"`
	// ChecksDays is how long the results of checks, and the health score,
	// last.
	ChecksDays int `json:"checksDays"`
}

// Limits of a retention, in days.
const (
	MaxActivityDays = 365
	MaxHistoryDays  = 30
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
	}
}

// Load returns the settings in st: what an admin saved, and the defaults
// for anything they didn't.
func Load(ctx context.Context, st *store.Store) (Settings, error) {
	s := Defaults()
	raw, ok, err := st.KV(ctx, kvKey)
	if err != nil || !ok {
		return s, err
	}
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Defaults(), fmt.Errorf("read the settings: %w", err)
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
	for _, err := range []error{s.General.Check(), s.Thresholds.Check(), checkUnits(s.WatchedUnits), s.Retention.Check()} {
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
	case strings.ContainsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }):
		return errors.New("the public URL can't have spaces in it")
	}
	return nil
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
	case t.CPUWindowMinutes < 1 || t.CPUWindow() > MaxCPUWindow:
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
		name := strings.TrimPrefix(u, "user:")
		if name == "" || strings.ContainsAny(name, " \t\n/") || strings.HasPrefix(name, "-") {
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

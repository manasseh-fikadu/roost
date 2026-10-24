// Package settings holds what admins can change about how Roost watches the
// tailnet, such as when an alert opens. The store keeps them, and the
// Settings page (#10) changes them. Until an admin does, they're Defaults.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

// kvKey is the setting in the store's kv table that holds them, as JSON.
const kvKey = "settings"

// Settings are what admins can change.
type Settings struct {
	Thresholds Thresholds `json:"thresholds"`
	// WatchedUnits are the systemd units on the machine Roost runs on that
	// it watches, such as "ssh.service", or "user:app.service" for a user
	// unit of the user Roost runs as. Roost always watches failed units.
	WatchedUnits []string `json:"watchedUnits"`
}

// Thresholds are when a device needs attention, and when an alert opens.
type Thresholds struct {
	// DiskWarn and DiskCritical are how full a disk can get, in percent.
	DiskWarn     float64 `json:"diskWarn"`
	DiskCritical float64 `json:"diskCritical"`
	// MemoryWarn is how much memory can be used, in percent.
	MemoryWarn float64 `json:"memoryWarn"`
	// CPUWarn is how busy the CPUs can be, in percent, on average over five
	// minutes.
	CPUWarn float64 `json:"cpuWarn"`
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

// Defaults returns the settings a new hub starts with.
func Defaults() Settings {
	return Settings{
		Thresholds: Thresholds{
			DiskWarn:       85,
			DiskCritical:   95,
			MemoryWarn:     92,
			CPUWarn:        90,
			OfflineMinutes: 10,
			KeyExpiryDays:  14,
			CheckFailures:  2,
		},
		// ssh.service is sshd.service on some systems. Roost leaves out the
		// ones a machine doesn't have.
		WatchedUnits: []string{"ssh.service", "sshd.service", "docker.service", "tailscaled.service", "cron.service"},
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

// Check returns an error that says what's wrong with s, if anything is.
func (s Settings) Check() error {
	t := s.Thresholds
	for _, p := range []struct {
		name string
		v    float64
	}{{"disk warning", t.DiskWarn}, {"critical disk", t.DiskCritical}, {"memory", t.MemoryWarn}, {"CPU", t.CPUWarn}} {
		if p.v <= 0 || p.v > 100 {
			return fmt.Errorf("the %s threshold must be a percentage above 0", p.name)
		}
	}
	switch {
	case t.DiskCritical < t.DiskWarn:
		return errors.New("a disk can't be critical before it needs a warning")
	case t.OfflineMinutes < 1 || t.OfflineMinutes > 7*24*60:
		return errors.New("the time a device can be offline must be from a minute to a week")
	case t.KeyExpiryDays < 1 || t.KeyExpiryDays > 365:
		return errors.New("the warning before a key expires must be from a day to a year")
	case t.CheckFailures < 1 || t.CheckFailures > 100:
		return errors.New("the failures in a row before an alert must be from 1 to 100")
	}
	for _, u := range s.WatchedUnits {
		name := strings.TrimPrefix(u, "user:")
		if name == "" || strings.ContainsAny(name, " \t\n/") || strings.HasPrefix(name, "-") {
			return fmt.Errorf("%q isn't the name of a systemd unit", u)
		}
	}
	return nil
}

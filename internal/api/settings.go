package api

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/manasseh-fikadu/roost/internal/settings"
	"github.com/manasseh-fikadu/roost/internal/systemd"
)

// settings answers GET /api/settings.
func (h *handler) settings(w http.ResponseWriter, r *http.Request) {
	h.writeSettings(w, r, h.opts.Settings.Get(r.Context()))
}

// changeSettings answers PATCH /api/settings, which replaces the parts of the
// settings it has, and applies them at once.
func (h *handler) changeSettings(w http.ResponseWriter, r *http.Request) {
	var body ChangeSettings
	if !h.readJSON(w, r, &body) {
		return
	}
	before, after, err := h.opts.Settings.Update(r.Context(), body.apply)
	var invalid *settings.InvalidError
	switch {
	case errors.As(err, &invalid):
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: invalid.Error()})
		return
	case err != nil:
		h.fail(w, r, "change the settings", err)
		return
	}

	by := callerOf(r).who.LoginName
	for _, msg := range settings.Changes(before, after) {
		h.log.Info("changed the settings", "by", by, "change", msg)
		h.logAction(r, EventKindSettings, "", msg)
	}
	if h.opts.Systemd != nil && !sameSet(before.WatchedUnits, after.WatchedUnits) {
		h.opts.Systemd.Refresh()
	}
	h.opts.Feed.Poke()
	h.writeSettings(w, r, after)
}

// apply changes the settings of s that b has.
func (b ChangeSettings) apply(s *settings.Settings) error {
	if g := b.General; g != nil {
		if g.Name != nil {
			s.General.Name = strings.TrimSpace(*g.Name)
		}
		if g.PublicURL != nil {
			url, err := settings.CleanPublicURL(*g.PublicURL)
			if err != nil {
				return &settings.InvalidError{Err: err}
			}
			s.General.PublicURL = url
		}
	}
	if t := b.Thresholds; t != nil {
		th := &s.Thresholds
		set(&th.DiskWarn, t.DiskWarn)
		set(&th.DiskCritical, t.DiskCritical)
		set(&th.MemoryWarn, t.MemoryWarn)
		set(&th.CPUWarn, t.CPUWarn)
		set(&th.CPUWindowMinutes, t.CPUWindowMinutes)
		set(&th.OfflineMinutes, t.OfflineMinutes)
		set(&th.KeyExpiryDays, t.KeyExpiryDays)
		set(&th.CheckFailures, t.CheckFailures)
	}
	if r := b.Rules; r != nil {
		ru := &s.Rules
		set(&ru.Offline, r.Offline)
		set(&ru.KeyExpiry, r.KeyExpiry)
		set(&ru.AgentStopped, r.AgentStopped)
		set(&ru.Disk, r.Disk)
		set(&ru.Memory, r.Memory)
		set(&ru.CPU, r.CPU)
		set(&ru.Checks, r.Checks)
		set(&ru.Containers, r.Containers)
		set(&ru.Units, r.Units)
		set(&ru.Collectors, r.Collectors)
	}
	if r := b.Retention; r != nil {
		set(&s.Retention.ActivityDays, r.ActivityDays)
		set(&s.Retention.HistoryDays, r.HistoryDays)
		set(&s.Retention.ChecksDays, r.ChecksDays)
	}
	if n := b.Notifications; n != nil {
		set(&s.Notifications.RemindHours, n.RemindHours)
		if q := n.Quiet; q != nil {
			qu := &s.Notifications.Quiet
			set(&qu.Enabled, q.Enabled)
			set(&qu.Start, q.Start)
			set(&qu.End, q.End)
			set(&qu.Timezone, q.Timezone)
			set(&qu.AllowCritical, q.AllowCritical)
		}
	}
	if b.WatchedUnits != nil {
		s.WatchedUnits = cleanUnits(b.WatchedUnits)
	}
	return nil
}

// set sets *dst to *src, unless src is nil.
func set[T any](dst, src *T) {
	if src != nil {
		*dst = *src
	}
}

// cleanUnits trims the names of watched units, and drops the empty ones and
// repeats, keeping the order.
func cleanUnits(units []string) []string {
	out := make([]string, 0, len(units))
	for _, u := range units {
		if u = strings.TrimSpace(u); u != "" && !slices.Contains(out, u) {
			out = append(out, u)
		}
	}
	return out
}

// sameSet reports whether a and b have the same items, in any order.
func sameSet(a, b []string) bool {
	return len(a) == len(b) && !slices.ContainsFunc(a, func(x string) bool { return !slices.Contains(b, x) })
}

// writeSettings answers with s and what goes with it.
func (h *handler) writeSettings(w http.ResponseWriter, r *http.Request, s settings.Settings) {
	mode := h.opts.Tailscale
	if mode == "" {
		mode = TailscaleModeEmbedded
	}
	h.writeJSON(w, r, http.StatusOK, SettingsView{
		Settings: newSettings(s), Defaults: newSettings(settings.Defaults()), Tailscale: mode,
	})
}

func newSettings(s settings.Settings) Settings {
	return Settings{
		General:      GeneralSettings(s.General),
		Thresholds:   Thresholds(s.Thresholds),
		Rules:        AlertRules(s.Rules),
		WatchedUnits: append([]string{}, s.WatchedUnits...),
		Retention:    RetentionSettings(s.Retention),
		Notifications: NotificationSettings{
			RemindHours: s.Notifications.RemindHours,
			Quiet:       QuietHours(s.Notifications.Quiet),
		},
	}
}

// installedServices answers GET /api/settings/units, which lists the services
// on Roost's machine to pick the ones to watch from.
func (h *handler) installedServices(w http.ResponseWriter, r *http.Request) {
	out := InstalledServices{Services: []InstalledService{}}
	if h.opts.Systemd == nil {
		h.writeJSON(w, r, http.StatusOK, out)
		return
	}
	out.Available = true
	services, err := h.opts.Systemd.Installed(r.Context())
	if err != nil {
		h.log.Warn("list the services on this machine", "error", err)
		h.writeJSON(w, r, http.StatusBadGateway, ErrorBody{Error: "Roost couldn't list the services systemd has"})
		return
	}
	for _, s := range services {
		scope := UnitScopeSystem
		if s.Scope == systemd.User {
			scope = UnitScopeUser
		}
		out.Services = append(out.Services, InstalledService{
			Key: s.Key(), Name: s.Name, Scope: scope, Description: s.Description, Active: s.Active,
		})
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

package api

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/manasseh-fikadu/roost/internal/alerts"
	"github.com/manasseh-fikadu/roost/internal/docker"
	"github.com/manasseh-fikadu/roost/internal/settings"
)

const (
	// keyExpiryCritical is how soon before a Tailscale key expires its
	// alert is critical.
	keyExpiryCritical = 48 * time.Hour
	// collectorGrace is how long a collector can fail before an alert.
	collectorGrace = 2 * time.Minute
)

// alertConditions returns the conditions that need an alert in snap, built
// at now from src, with the machines Roost measures, and says whose data was
// fresh. started is when Roost started.
func alertConditions(snap Snapshot, machines []machine, src sources, started, now time.Time) ([]alerts.Condition, alerts.FreshFunc) {
	rules := src.global.Rules
	var out []alerts.Condition
	add := func(c alerts.Condition) { out = append(out, c) }
	byID := make(map[string]Device, len(snap.Devices))
	for _, d := range snap.Devices {
		byID[d.ID] = d
	}

	for _, d := range snap.Devices {
		t := src.thresholdsFor(d.ID)
		if rules.Offline && !d.Self && !d.Online && d.Settings.AlertOffline && d.LastSeen != nil {
			if gone := now.Sub(*d.LastSeen); gone >= t.OfflineAfter() {
				add(alerts.Condition{
					Key: "offline:" + d.ID, Source: string(AlertSourceDevice), DeviceID: d.ID, Severity: alerts.Warning,
					Title:  d.Name + " is offline",
					Detail: "It left the tailnet " + ago(gone) + ".",
				})
			}
		}
		if d.KeyExpiry != nil && rules.KeyExpiry {
			if left := d.KeyExpiry.Sub(now); left < t.KeyExpiryWarn() {
				c := alerts.Condition{
					Key: "key-expiry:" + d.ID, Source: string(AlertSourceDevice), DeviceID: d.ID, Severity: alerts.Warning,
					Title:  fmt.Sprintf("Tailscale key of %s expires in %s", d.Name, days(left)),
					Detail: "Log it in to Tailscale again, or turn off key expiry for it in the Tailscale admin console.",
				}
				if left <= 0 {
					c.Title = "Tailscale key of " + d.Name + " expired"
				}
				if left <= keyExpiryCritical {
					c.Severity = alerts.Critical
				}
				add(c)
			}
		}
		if a := d.Agent; a != nil && a.Stale && a.LastSeen != nil && rules.AgentStopped {
			add(alerts.Condition{
				Key: "agent:" + d.ID, Source: string(AlertSourceAgent), DeviceID: d.ID, Severity: alerts.Warning,
				Title:  "Agent on " + d.Name + " stopped reporting",
				Detail: "It's on the tailnet, but its last heartbeat came " + ago(now.Sub(*a.LastSeen)) + ".",
			})
		}
	}

	current := map[string]bool{}
	for _, m := range machines {
		d, ok := byID[m.deviceID]
		if !ok || !m.current || d.Metrics == nil {
			continue
		}
		current[m.deviceID] = true
		t := src.thresholdsFor(d.ID)
		metrics := func(kind string, sev alerts.Severity, title, detail string) {
			add(alerts.Condition{
				Key: kind + ":" + d.ID, Source: string(AlertSourceMetrics), DeviceID: d.ID, Severity: sev,
				Title: title, Detail: detail,
			})
		}
		if u := d.Metrics.Disk; u != nil && rules.Disk {
			if pct, ok := percent(u); ok && pct >= t.DiskWarn {
				sev := alerts.Warning
				if pct >= t.DiskCritical {
					sev = alerts.Critical
				}
				metrics("disk", sev, fmt.Sprintf("Disk %.0f%% full on %s", pct, d.Name),
					fmt.Sprintf("%s of %s left on /.", sizeText(u.Total-u.Used), sizeText(u.Total)))
			}
		}
		if u := d.Metrics.Memory; u != nil && rules.Memory {
			if pct, ok := percent(u); ok && pct >= t.MemoryWarn {
				metrics("memory", alerts.Warning, fmt.Sprintf("Memory %.0f%% used on %s", pct, d.Name),
					fmt.Sprintf("%s of %s free.", sizeText(u.Total-u.Used), sizeText(u.Total)))
			}
		}
		if cpu := m.sustained; cpu != nil && rules.CPU && *cpu >= t.CPUWarn {
			metrics("cpu", alerts.Warning, fmt.Sprintf("CPU %.0f%% busy on %s", *cpu, d.Name),
				"On average over the last "+shortDuration(t.CPUWindow())+".")
		}
	}

	for _, c := range snap.Checks {
		if !rules.Checks || c.State != CheckStateFailing || c.Failures < src.global.Thresholds.CheckFailures {
			continue
		}
		cond := alerts.Condition{
			Key: "check:" + c.ID, Source: string(AlertSourceCheck), DeviceID: c.DeviceID, CheckID: c.ID,
			Severity: alerts.Critical, Title: c.Name + " is failing",
			Detail: fmt.Sprintf("%s · %d failures in a row · %s", c.StateReason, c.Failures, c.Endpoint),
		}
		if c.Kind == CheckKindPing {
			cond.Severity = alerts.Warning
			if d, ok := byID[c.DeviceID]; ok {
				cond.Title = d.Name + " doesn't answer pings"
			}
			cond.Detail = fmt.Sprintf("It's online, but missed the last %d pings from the hub.", c.Failures)
		}
		add(cond)
	}

	for _, c := range snap.Containers {
		if !rules.Containers {
			break
		}
		r := findContainer(src, c.Name)
		problem, sev := containerProblem(r)
		if problem == "" {
			continue
		}
		add(alerts.Condition{
			Key: "container:" + c.Name, Source: string(AlertSourceContainer), DeviceID: c.DeviceID, Severity: sev,
			Title: "Container " + c.Name + " " + problem, Detail: c.Status + " · " + c.Image,
		})
	}

	for _, u := range snap.Units {
		if !rules.Units {
			break
		}
		problem := unitProblem(u)
		if problem == "" {
			continue
		}
		cond := alerts.Condition{
			Key: "unit:" + unitKey(u), Source: string(AlertSourceUnit), DeviceID: u.DeviceID, Severity: alerts.Warning,
			Title: u.Name + " " + problem, Detail: u.Description,
		}
		if u.Scope == UnitScopeUser {
			cond.Title = "User unit " + cond.Title
		}
		add(cond)
	}

	for _, c := range snap.Collectors {
		if !rules.Collectors || c.State != CollectorStateFailing {
			continue
		}
		since := started
		if c.LastOK != nil {
			since = *c.LastOK
		}
		if now.Sub(since) < collectorGrace {
			continue
		}
		detail := "It hasn't worked since Roost started " + ago(now.Sub(started)) + "."
		if c.LastOK != nil {
			detail = "It last worked " + ago(now.Sub(*c.LastOK)) + "."
		}
		// The collector's error can name paths on the hub, so only admins
		// see it, on Overview.
		add(alerts.Condition{
			Key: "collector:" + c.Name, Source: string(AlertSourceCollector), Severity: alerts.Warning,
			Title: c.Name + " keeps failing", Detail: detail,
		})
	}

	tailnetFresh := fresh(src.tailnet.StatusAt, src.tailnet.StatusErr, now)
	var dockerFresh, unitsFresh bool
	if r := src.docker; r != nil {
		dockerFresh = fresh(r.At, r.Err, now)
	}
	if r := src.systemd; r != nil {
		unitsFresh = fresh(r.At, r.Err, now)
	}
	isFresh := func(key, source, deviceID string) bool {
		// An alert of a rule that's turned off has nothing to wait for.
		if !ruleOn(rules, key) {
			return true
		}
		// A device that left the tailnet takes its alerts with it.
		if _, ok := byID[deviceID]; deviceID != "" && !ok && tailnetFresh {
			return true
		}
		switch AlertSource(source) {
		case AlertSourceDevice, AlertSourceAgent:
			return tailnetFresh
		case AlertSourceMetrics:
			// A device nothing measures, such as one whose agent was revoked,
			// has no metrics left to wait for. One with an agent that stopped,
			// or hasn't reported since it enrolled, waits for its next report.
			d := byID[deviceID]
			return current[deviceID] || (d.MeasuredBy == "" && d.Agent == nil)
		case AlertSourceContainer:
			return dockerFresh
		case AlertSourceUnit:
			return unitsFresh
		case AlertSourceCheck, AlertSourceCollector:
			return true
		}
		return false
	}
	return out, isFresh
}

// ruleOn reports whether the rule that opens the alert with a key is on.
func ruleOn(r settings.Rules, key string) bool {
	kind, _, _ := strings.Cut(key, ":")
	switch kind {
	case "offline":
		return r.Offline
	case "key-expiry":
		return r.KeyExpiry
	case "agent":
		return r.AgentStopped
	case "disk":
		return r.Disk
	case "memory":
		return r.Memory
	case "cpu":
		return r.CPU
	case "check":
		return r.Checks
	case "container":
		return r.Containers
	case "unit":
		return r.Units
	case "collector":
		return r.Collectors
	}
	return true
}

// findContainer returns the container of src's Docker reading with a name.
func findContainer(src sources, name string) (c docker.Container) {
	if src.docker == nil {
		return c
	}
	for _, c := range src.docker.Containers {
		if c.Name == name {
			return c
		}
	}
	return c
}

// ago writes a span of time back, such as "5m ago" or "3d ago".
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
}

// days writes a span of time to come in whole days, rounded up, or hours
// under a day.
func days(d time.Duration) string {
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", max(1, int(math.Ceil(d.Hours()))))
	}
	n := int(math.Ceil(d.Hours() / 24))
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

// sizeText writes a number of bytes, such as 512 MB or 12.3 GB.
func sizeText(n uint64) string {
	const unit = 1024
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	v, i := float64(n), 0
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	if i == 0 || v >= 100 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

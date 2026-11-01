package settings

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Changes describes what differs between two settings, as sentences for the
// activity log, one for each part of the settings that changed. The public
// URL's value is left out of them.
func Changes(before, after Settings) []string {
	var out []string
	if before.General.Name != after.General.Name {
		out = append(out, fmt.Sprintf("Renamed Bield to %q", after.General.Name))
	}
	switch {
	case before.General.PublicURL == after.General.PublicURL:
	case after.General.PublicURL == "":
		out = append(out, "Cleared the public URL")
	default:
		out = append(out, "Changed the public URL")
	}
	switch {
	case before.General.SetupDone == after.General.SetupDone:
	case after.General.SetupDone:
		out = append(out, "Finished the first-run setup")
	default:
		out = append(out, "Turned the first-run setup back on")
	}

	b, a := before.Thresholds, after.Thresholds
	var th []string
	for _, f := range []struct {
		label       string
		before, now string
	}{
		{"disk warning", percent(b.DiskWarn), percent(a.DiskWarn)},
		{"critical disk", percent(b.DiskCritical), percent(a.DiskCritical)},
		{"memory", percent(b.MemoryWarn), percent(a.MemoryWarn)},
		{"CPU", percent(b.CPUWarn), percent(a.CPUWarn)},
		{"CPU window", minutes(b.CPUWindowMinutes), minutes(a.CPUWindowMinutes)},
		{"offline after", minutes(b.OfflineMinutes), minutes(a.OfflineMinutes)},
		{"key expiry warning", days(b.KeyExpiryDays), days(a.KeyExpiryDays)},
		{"failing checks", times(b.CheckFailures), times(a.CheckFailures)},
	} {
		if f.before != f.now {
			th = append(th, fmt.Sprintf("%s %s → %s", f.label, f.before, f.now))
		}
	}
	if len(th) > 0 {
		out = append(out, "Changed the alert thresholds: "+strings.Join(th, ", "))
	}

	var on, off []string
	br, ar := ruleFlags(before.Rules), ruleFlags(after.Rules)
	for i, r := range ruleLabels {
		switch {
		case br[i] && !ar[i]:
			off = append(off, r)
		case !br[i] && ar[i]:
			on = append(on, r)
		}
	}
	if len(off) > 0 {
		out = append(out, "Turned off alerts for "+strings.Join(off, ", "))
	}
	if len(on) > 0 {
		out = append(out, "Turned on alerts for "+strings.Join(on, ", "))
	}

	added, removed := difference(after.WatchedUnits, before.WatchedUnits), difference(before.WatchedUnits, after.WatchedUnits)
	var units []string
	if len(added) > 0 {
		units = append(units, "started watching "+list(added))
	}
	if len(removed) > 0 {
		units = append(units, "stopped watching "+list(removed))
	}
	if len(units) > 0 {
		out = append(out, "Changed the watched systemd units: "+strings.Join(units, ", "))
	}

	br2, ar2 := before.Retention, after.Retention
	var keep []string
	for _, f := range []struct {
		label       string
		before, now int
	}{
		{"activity log", br2.ActivityDays, ar2.ActivityDays},
		{"device history", br2.HistoryDays, ar2.HistoryDays},
		{"check results", br2.ChecksDays, ar2.ChecksDays},
	} {
		if f.before != f.now {
			keep = append(keep, fmt.Sprintf("%s %s → %s", f.label, days(f.before), days(f.now)))
		}
	}
	if len(keep) > 0 {
		out = append(out, "Changed how long Bield keeps history: "+strings.Join(keep, ", "))
	}
	return append(out, notificationChanges(before.Notifications, after.Notifications)...)
}

func notificationChanges(b, a Notifications) []string {
	var out []string
	switch {
	case b.RemindHours == a.RemindHours:
	case a.RemindHours == 0:
		out = append(out, "Turned off reminders about open alerts")
	default:
		out = append(out, "Set reminders about open alerts to every "+hours(a.RemindHours))
	}
	switch {
	case b.Quiet == a.Quiet:
	case !a.Quiet.Enabled:
		out = append(out, "Turned off quiet hours")
	default:
		msg := fmt.Sprintf("Set quiet hours to %s–%s", a.Quiet.Start, a.Quiet.End)
		if a.Quiet.Timezone != "" {
			msg += " " + a.Quiet.Timezone
		}
		if a.Quiet.AllowCritical {
			msg += ", with critical alerts getting through"
		}
		out = append(out, msg)
	}
	return out
}

func hours(n int) string {
	if n == 1 {
		return "hour"
	}
	return fmt.Sprintf("%d hours", n)
}

// ruleLabels name the rules in ruleFlags' order.
var ruleLabels = []string{
	"offline devices", "key expiry", "stopped agents", "disk", "memory", "CPU", "checks", "containers",
	"systemd units", "Bield's own jobs",
}

func ruleFlags(r Rules) []bool {
	return []bool{r.Offline, r.KeyExpiry, r.AgentStopped, r.Disk, r.Memory, r.CPU, r.Checks, r.Containers, r.Units, r.Collectors}
}

func percent(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "%" }

func minutes(n int) string {
	if n == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", n)
}

func days(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

func times(n int) string {
	if n == 1 {
		return "1 failure"
	}
	return fmt.Sprintf("%d failures", n)
}

// difference returns the items of a that b doesn't have.
func difference(a, b []string) []string {
	var out []string
	for _, s := range a {
		if !slices.Contains(b, s) && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// list joins the first few names of a list, and says how many more there are.
func list(names []string) string {
	const shown = 4
	if len(names) <= shown {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(names[:shown], ", "), len(names)-shown)
}

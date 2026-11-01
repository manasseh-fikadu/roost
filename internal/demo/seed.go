package demo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/manasseh-fikadu/roost/internal/notify"
	"github.com/manasseh-fikadu/roost/internal/settings"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/usage"
)

// metricsEvery and pingEvery are how often a made-up history has a row for a
// device, as the real one does.
const (
	metricsEvery = 30 * time.Second
	pingEvery    = time.Minute
	// usageEvery is how often a made-up history has what ran on a machine for
	// the last day, and usageOlderEvery before that. Roost stores a minute of
	// it each time, so this history is thinner than a real one, which would
	// take too long to make.
	usageEvery      = 5 * time.Minute
	usageOlderEvery = time.Hour
	// usageMachines is how many machines have a history of what runs on them.
	usageMachines = 12
)

// Seed starts a new database with the demo's settings, people, checks and
// history, which ends where the demo starts and reaches back span. It writes
// what Roost would have written if it had watched this tailnet for that long.
func (d *Demo) Seed(ctx context.Context, st *store.Store, span time.Duration) error {
	steps := []struct {
		name string
		run  func() error
	}{
		{"settings", func() error { return d.seedSettings(ctx, st, span) }},
		{"people", func() error { return d.seedPeople(ctx, st) }},
		{"agents and SSH access", func() error { return d.seedAccess(ctx, st) }},
		{"checks", func() error { return d.seedChecks(ctx, st, span) }},
		{"metrics", func() error { return d.seedMetrics(ctx, st, span) }},
		{"pings", func() error { return d.seedPings(ctx, st, span) }},
		{"container metrics", func() error { return d.seedContainers(ctx, st, span) }},
		{"usage", func() error { return d.seedUsage(ctx, st, span) }},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			return fmt.Errorf("seed the demo's %s: %w", step.name, err)
		}
	}
	return nil
}

func (d *Demo) seedSettings(ctx context.Context, st *store.Store, span time.Duration) error {
	s := settings.Defaults()
	s.General.Name = "Roost demo"
	s.General.SetupDone = true
	// The history is kept as long as it reaches back.
	days := int(math.Ceil(span.Hours() / 24))
	s.Retention.HistoryDays = min(max(days, s.Retention.HistoryDays), settings.MaxHistoryDays)
	s.Retention.ChecksDays = min(max(days, s.Retention.ChecksDays), settings.MaxChecksDays)
	return settings.Save(ctx, st, s)
}

func (d *Demo) seedPeople(ctx context.Context, st *store.Store) error {
	if _, err := st.SetRole(ctx, sam, store.Viewer); err != nil {
		return err
	}
	for _, m := range d.machines {
		ds := store.DeviceSettings{Notes: m.notes, Kind: m.kind}
		if m.name != m.host {
			ds.DisplayName = m.name
		}
		if m.ignoreOffline {
			ds.AlertOffline = new(false)
		}
		if ds == (store.DeviceSettings{}) {
			continue
		}
		if err := st.SetDeviceSettings(ctx, m.id, ds); err != nil {
			return err
		}
	}
	return nil
}

// seedAccess enrolls the agents and sets up SSH access, as an admin did a
// while ago.
func (d *Demo) seedAccess(ctx context.Context, st *store.Store) error {
	for i, m := range d.machines {
		if m.agent {
			at := d.start.Add(-time.Duration(3+i*7) * 24 * time.Hour)
			code, _, err := st.NewEnrollmentCode(ctx, alex, at)
			if err != nil {
				return err
			}
			if _, _, err := st.Enroll(ctx, code, m.id, at.Add(time.Minute)); err != nil {
				return err
			}
		}
		if m.ssh || i == 0 {
			id, login := m.id, "alex"
			if i == 0 {
				id = store.HubID
			}
			if strings.HasPrefix(m.host, "pi") {
				login = "pi"
			}
			err := st.SetSSHAccess(ctx, store.SSHAccess{
				DeviceID: id, User: login, Port: 22, CreatedAt: d.start.Add(-time.Duration(20+i*5) * 24 * time.Hour), CreatedBy: alex,
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// seedChecks adds the checks with the results they'd have had, and the health
// score that follows.
func (d *Demo) seedChecks(ctx context.Context, st *store.Store, span time.Duration) error {
	// A new database starts with checks of the internet, which a demo
	// can't reach.
	defaults, err := st.Checks(ctx)
	if err != nil {
		return err
	}
	for _, c := range defaults {
		if err := st.DeleteCheck(ctx, c.ID); err != nil {
			return err
		}
	}
	added := map[string]store.Check{}
	for _, c := range d.storeChecks() {
		c, err := st.AddCheck(ctx, c)
		if err != nil {
			return err
		}
		added[c.Name] = c
	}
	var results []store.CheckResult
	var health []store.HealthSample
	every := time.Minute
	for t := d.start.Add(-span).Truncate(every); t.Before(d.start); t = t.Add(every) {
		passing, failing := 0, 0
		for _, p := range probes {
			c := added[p.name]
			r := d.probeAt(p, t)
			results = append(results, store.CheckResult{CheckID: c.ID, At: t, OK: r.ok, Latency: r.latency, Detail: r.detail})
			if r.ok {
				passing++
			} else {
				failing++
			}
		}
		health = append(health, store.HealthSample{
			At: t, Score: float64(passing) / float64(passing+failing) * 100, Passing: passing, Failing: failing,
		})
		if len(results) >= 20000 {
			if err := st.AddCheckResults(ctx, results); err != nil {
				return err
			}
			results = results[:0]
		}
	}
	if err := st.AddCheckResults(ctx, results); err != nil {
		return err
	}
	if err := st.AddHealthSamples(ctx, health); err != nil {
		return err
	}
	return d.seedAlerts(ctx, st, added)
}

// probeResult is what a check found when it ran.
type probeResult struct {
	ok      bool
	latency time.Duration
	detail  string
}

// probeAt returns what the check of p found at t.
func (d *Demo) probeAt(p probe, t time.Time) probeResult {
	m := d.machineNamed(p.onHost, nil)
	offline := m != nil && m.offlineFor > 0 && !t.Before(d.start.Add(-m.offlineFor))
	switch {
	case offline:
		return probeResult{detail: "Timed out after " + shortSecs(cmp.Or(p.timeout, 10*time.Second))}
	case p.downAt(t, d.start) || p.flakyAt(t, d.seed):
		if p.kind == "http" {
			return probeResult{latency: p.latencyAt(t, d.seed), detail: fmt.Sprintf("HTTP 503 Service Unavailable (expected %d)", cmp.Or(p.expect, 200))}
		}
		return probeResult{detail: "dial tcp " + p.target + ": connect: connection refused"}
	}
	r := probeResult{ok: true, latency: p.latencyAt(t, d.seed)}
	switch p.kind {
	case "http":
		code := cmp.Or(p.expect, 200)
		r.detail = fmt.Sprintf("HTTP %d %s", code, map[int]string{200: "OK", 302: "Found"}[code])
	case "dns":
		r.detail = "203.0.113.10 and 1 more"
	default:
		r.detail = "Connected"
	}
	return r
}

func shortSecs(d time.Duration) string { return fmt.Sprintf("%gs", d.Seconds()) }

// reportsAt reports whether machine m sent metrics at t: a machine with
// nothing to report doesn't, and one that left the tailnet stopped.
func (d *Demo) reportsAt(m *machine, t time.Time) bool {
	if m.index != 0 && !m.agent {
		return false
	}
	return m.offlineFor == 0 || t.Before(d.start.Add(-m.offlineFor))
}

// historyID returns the ID a machine's history is kept under.
func historyID(m *machine) string {
	if m.index == 0 {
		return store.HubID
	}
	return m.id
}

// seedMetrics writes the metrics as Roost keeps them: every sample of the last
// store.RawKeep, and before that, buckets: of store.FineStep for as far back as
// they last, and of store.CoarseStep for the rest.
func (d *Demo) seedMetrics(ctx context.Context, st *store.Store, span time.Duration) error {
	var rows []store.MetricsRow
	flush := func() error {
		err := st.AddMetrics(ctx, rows)
		rows = rows[:0]
		return err
	}
	for t := d.start.Add(-min(span, store.RawKeep)).Truncate(metricsEvery); t.Before(d.start); t = t.Add(metricsEvery) {
		for _, m := range d.machines {
			if !d.reportsAt(m, t) {
				continue
			}
			rows = append(rows, d.metricsRow(m, t))
		}
		if len(rows) >= 50000 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}

	var buckets []store.MetricsRollup
	err := d.eachBucket(span, func(step time.Duration, at time.Time) error {
		for _, m := range d.machines {
			if d.reportsAt(m, at) && d.reportsAt(m, at.Add(step)) {
				buckets = append(buckets, d.metricsBucket(m, step, at))
			}
		}
		if len(buckets) < 50000 {
			return nil
		}
		err := st.AddMetricsRollups(ctx, buckets)
		buckets = buckets[:0]
		return err
	})
	if err != nil {
		return err
	}
	return st.AddMetricsRollups(ctx, buckets)
}

// eachBucket calls fn for each bucket that Roost would have made of a history
// that reaches back span, up to the last that is over: the buckets of
// store.FineStep for as far back as they last, and of store.CoarseStep for all
// of it. It goes oldest first, a step at a time. Roost makes buckets for the
// time of the raw samples too, and the buckets of the two steps overlap.
func (d *Demo) eachBucket(span time.Duration, fn func(step time.Duration, at time.Time) error) error {
	over := d.start.Add(-time.Minute)
	for _, step := range []time.Duration{store.CoarseStep, store.FineStep} {
		begin := d.start.Add(-min(span, store.FineKeep)).Truncate(step)
		if step == store.CoarseStep {
			begin = d.start.Add(-span).Truncate(step)
		}
		for at := begin; !at.Add(step).After(over); at = at.Add(step) {
			if err := fn(step, at); err != nil {
				return err
			}
		}
	}
	return nil
}

// metricsBucket returns the average of what machine m did over the bucket that
// starts at at, from a few samples of it.
func (d *Demo) metricsBucket(m *machine, step time.Duration, at time.Time) store.MetricsRollup {
	samples := 1
	if step >= store.CoarseStep {
		samples = 4
	}
	var sum reading
	for i := range samples {
		r := m.at(at.Add(time.Duration(float64(step)*(float64(i)+0.5)/float64(samples))), d.start)
		sum.cpu, sum.mem, sum.disk, sum.load[0] = sum.cpu+r.cpu, sum.mem+r.mem, sum.disk+r.disk, sum.load[0]+r.load[0]
		sum.rx, sum.tx, sum.temp, sum.battery = sum.rx+r.rx, sum.tx+r.tx, sum.temp+r.temp, sum.battery+r.battery
	}
	k := float64(samples)
	b := store.MetricsRollup{
		DeviceID: historyID(m), Step: step, At: at, N: int(step / metricsEvery),
		CPU: new(sum.cpu / k), Mem: new(sum.mem / k), Disk: new(sum.disk / k), Load1: new(sum.load[0] / k),
		NetRx: new(sum.rx / k), NetTx: new(sum.tx / k),
	}
	if m.temp > 0 {
		b.Temp = new(sum.temp / k)
	}
	if m.battery {
		b.Battery = new(sum.battery / k)
	}
	return b
}

// metricsRow returns what machine m did at t, as the store keeps it.
func (d *Demo) metricsRow(m *machine, t time.Time) store.MetricsRow {
	r := m.at(t, d.start)
	row := store.MetricsRow{
		DeviceID: historyID(m), At: t, CPU: &r.cpu, Mem: &r.mem, Disk: &r.disk, Load1: &r.load[0], NetRx: &r.rx, NetTx: &r.tx,
	}
	if r.temp > 0 {
		row.Temp = &r.temp
	}
	if m.battery {
		row.Battery = &r.battery
	}
	return row
}

// seedPings writes the pings as Roost keeps them, like the metrics.
func (d *Demo) seedPings(ctx context.Context, st *store.Store, span time.Duration) error {
	var pings []store.Ping
	for t := d.start.Add(-min(span, store.RawKeep)).Truncate(pingEvery); t.Before(d.start); t = t.Add(pingEvery) {
		for _, m := range d.machines[1:] {
			if m.offlineFor > 0 && !t.Before(d.start.Add(-m.offlineFor)) {
				continue
			}
			p := store.Ping{DeviceID: m.id, At: t, Replied: true, RTT: d.rttAt(m, t)}
			if lostAt(m, t) {
				p.Replied, p.RTT = false, 0
			}
			pings = append(pings, p)
		}
		if len(pings) >= 50000 {
			if err := st.AddPings(ctx, pings); err != nil {
				return err
			}
			pings = pings[:0]
		}
	}
	if err := st.AddPings(ctx, pings); err != nil {
		return err
	}

	var buckets []store.PingRollup
	err := d.eachBucket(span, func(step time.Duration, at time.Time) error {
		for _, m := range d.machines[1:] {
			if m.offlineFor > 0 && !at.Add(step).Before(d.start.Add(-m.offlineFor)) {
				continue
			}
			sent := int(step / pingEvery)
			// The pings that get lost are the same few each time, as they would
			// be for a machine on a poor link.
			lost := int(math.Round(lossRate(m) * float64(sent) * (0.5 + unit(m.seed(14), at.Unix()/60))))
			buckets = append(buckets, store.PingRollup{
				DeviceID: m.id, Step: step, At: at, Pings: sent, Replies: sent - lost, RTT: d.rttAt(m, at.Add(step/2)),
			})
		}
		if len(buckets) < 50000 {
			return nil
		}
		err := st.AddPingRollups(ctx, buckets)
		buckets = buckets[:0]
		return err
	})
	if err != nil {
		return err
	}
	return st.AddPingRollups(ctx, buckets)
}

// rttAt returns how long a ping of machine m took at t.
func (d *Demo) rttAt(m *machine, t time.Time) time.Duration {
	rtt := cmp.Or(m.rtt, 40*time.Millisecond)
	return time.Duration(float64(rtt) * (1 + 0.18*wave(m.seed(11), t, 45*time.Second)))
}

// lossRate is the share of the pings to machine m that get lost, mostly on
// relayed paths.
func lossRate(m *machine) float64 {
	if m.direct {
		return 0.004
	}
	return 0.014
}

// lostAt reports whether the ping to machine m at t got no reply.
func lostAt(m *machine, t time.Time) bool {
	return unit(m.seed(12), t.Unix()/60) < lossRate(m)
}

// seedContainers records what the containers of the machines with Docker
// used, as the sampler does every SampleInterval.
func (d *Demo) seedContainers(ctx context.Context, st *store.Store, span time.Duration) error {
	span = min(span, store.DetailKeep)
	var rows []store.ContainerMetricsRow
	every := 5 * time.Minute
	for t := d.start.Add(-span).Truncate(every); t.Before(d.start); t = t.Add(every) {
		for _, m := range d.machines {
			eng := d.engines[m.id]
			if eng == nil || !d.reportsAt(m, t) && m.index != 0 {
				continue
			}
			for _, c := range m.containers {
				if c.state != "running" || !t.After(d.start.Add(c.started)) {
					continue
				}
				cpu := eng.rate(c, t)
				mem := c.memMB * 1e6 * (1 + 0.04*wave(hashString(c.name)^7, t, 20*time.Minute))
				rx, tx, rd, wr := cpu*1500, cpu*900, cpu*400, cpu*250
				rows = append(rows, store.ContainerMetricsRow{
					DeviceID: historyID(m), Name: c.name, At: t, CPU: &cpu, Mem: &mem, NetRx: &rx, NetTx: &tx, BlkRead: &rd, BlkWrite: &wr,
				})
			}
		}
		if len(rows) >= 50000 {
			if err := st.AddContainerMetrics(ctx, rows); err != nil {
				return err
			}
			rows = rows[:0]
		}
	}
	return st.AddContainerMetrics(ctx, rows)
}

// seedUsage records what ran on the first machines, with a Recorder, the way
// Roost keeps it: a sample every usageEvery is each a minute of its own.
func (d *Demo) seedUsage(ctx context.Context, st *store.Store, span time.Duration) error {
	span = min(span, store.DetailKeep)
	rec := usage.NewRecorder(st, d.log)
	older := d.start.Add(-24 * time.Hour)
	step := func(t time.Time) time.Duration {
		if t.Before(older) {
			return usageOlderEvery
		}
		return usageEvery
	}
	for t := d.start.Add(-span).Truncate(usageOlderEvery); t.Before(d.start); t = t.Add(step(t)) {
		for _, m := range d.machines[:min(usageMachines, len(d.machines))] {
			if !d.reportsAt(m, t) || len(m.workloads) == 0 {
				continue
			}
			source := usage.SourceAgent
			if m.index == 0 {
				source = usage.SourceHub
			}
			rec.Record(ctx, historyID(m), d.usage(m, m.at(t, d.start), t, source))
		}
	}
	return nil
}

// seedAlerts writes the alerts that opened and cleared over the past days, and
// the ones that are open now, with what the admin did about them. The open ones
// are conditions that are really there, so Roost carries them on.
func (d *Demo) seedAlerts(ctx context.Context, st *store.Store, checks map[string]store.Check) error {
	byHost := func(host string) *machine { return d.machineNamed(host, d.machines[0]) }
	type alert struct {
		a      store.Alert
		notes  []store.AlertNote
		notify bool
	}
	nas, garage, work, nyc, fra, mac := byHost("nas"), byHost("pi-garage"), byHost("work-mac"), byHost("vps-nyc"), byHost("vps-fra"), byHost("macbook")
	ago := func(dur time.Duration) time.Time { return d.start.Add(-dur) }
	list := []alert{
		{a: store.Alert{
			Key: "disk:" + nas.id, Source: "metrics", DeviceID: nas.id, Severity: "warning",
			Title: "Disk 90% full on NAS", Detail: "764 GB of 7.3 TB left on /.", OpenedAt: ago(15 * 24 * time.Hour),
			AckedAt: ago(14*24*time.Hour + 22*time.Hour), AckedBy: alex,
		}, notes: []store.AlertNote{
			{At: ago(14*24*time.Hour + 22*time.Hour), Author: alex, Text: "Known. Photos are the bulk of it."},
			{At: ago(2 * 24 * time.Hour), Author: alex, Text: "Ordered two 12 TB disks, they arrive Friday."},
		}, notify: true},
		{a: store.Alert{
			Key: "offline:" + garage.id, Source: "device", DeviceID: garage.id, Severity: "warning",
			Title: "Garage Pi is offline", Detail: "It left the tailnet 3h ago.", OpenedAt: ago(garage.offlineFor - 10*time.Minute),
		}, notify: true},
		{a: store.Alert{
			Key: "check:" + checks["Garage camera"].ID, Source: "check", DeviceID: "", CheckID: checks["Garage camera"].ID, Severity: "critical",
			Title: "Garage camera is failing", Detail: "Timed out after 10s · 3 failures in a row · http://pi-garage.tail-demo.ts.net:8081/",
			OpenedAt: ago(garage.offlineFor - 13*time.Minute), SnoozedUntil: d.start.Add(5 * time.Hour), SnoozedBy: alex,
		}, notes: []store.AlertNote{{At: ago(2 * time.Hour), Author: alex, Text: "The Pi lost its wifi again. I'll reboot the extender tonight."}}, notify: true},
		{a: store.Alert{
			Key: "key-expiry:" + work.id, Source: "device", DeviceID: work.id, Severity: "warning",
			Title: "Tailscale key of Work laptop expires in 7 days", Detail: "Log it in to Tailscale again, or turn off key expiry for it in the Tailscale admin console.",
			OpenedAt: ago(7*24*time.Hour + 21*time.Hour),
		}, notify: true},
		// Cleared ones.
		{a: store.Alert{
			Key: "cpu:" + nyc.id, Source: "metrics", DeviceID: nyc.id, Severity: "warning", Title: "CPU 94% busy on New York VPS",
			Detail: "On average over the last 5m.", OpenedAt: ago(26*time.Hour + 40*time.Minute), ResolvedAt: ago(25*time.Hour + 18*time.Minute),
		}, notes: []store.AlertNote{{At: ago(25*time.Hour + 30*time.Minute), Author: alex, Text: "A runaway cron job. Fixed the schedule."}}},
		{a: store.Alert{
			Key: "check:" + checks["Status page"].ID, Source: "check", CheckID: checks["Status page"].ID, Severity: "critical",
			Title: "Status page is failing", Detail: "HTTP 503 Service Unavailable (expected 200) · 2 failures in a row · http://vps-nyc.tail-demo.ts.net:3001/",
			OpenedAt: ago(26*time.Hour + 58*time.Minute), ResolvedAt: ago(25 * time.Hour),
		}},
		{a: store.Alert{
			Key: "check:" + checks["Blog"].ID, Source: "check", CheckID: checks["Blog"].ID, Severity: "critical",
			Title: "Blog is failing", Detail: "HTTP 503 Service Unavailable (expected 200) · 2 failures in a row · http://vps-fra.tail-demo.ts.net:2368/",
			OpenedAt: ago(30*time.Hour + 58*time.Minute), ResolvedAt: ago(30*time.Hour + 38*time.Minute),
		}},
		{a: store.Alert{
			Key: "agent:" + mac.id, Source: "agent", DeviceID: mac.id, Severity: "warning", Title: "Agent on MacBook Pro stopped reporting",
			Detail: "It's on the tailnet, but its last heartbeat came 3m ago.", OpenedAt: ago(2*24*time.Hour + 6*time.Hour), ResolvedAt: ago(2*24*time.Hour + 5*time.Hour + 20*time.Minute),
		}},
		{a: store.Alert{
			Key: "memory:" + fra.id, Source: "metrics", DeviceID: fra.id, Severity: "warning", Title: "Memory 93% used on Frankfurt VPS",
			Detail: "133 MB of 1.9 GB free.", OpenedAt: ago(4*24*time.Hour + 9*time.Hour), ResolvedAt: ago(4*24*time.Hour + 8*time.Hour + 30*time.Minute),
		}},
		{a: store.Alert{
			Key: "unit:backup.service", Source: "unit", DeviceID: d.machines[0].id, Severity: "warning", Title: "backup.service failed",
			Detail: "Nightly restic backup to the NAS", OpenedAt: ago(3*24*time.Hour + 9*time.Hour), ResolvedAt: ago(3*24*time.Hour + 4*time.Hour),
		}, notes: []store.AlertNote{{At: ago(3*24*time.Hour + 5*time.Hour), Author: alex, Text: "The NAS was rebooting for updates. Ran it by hand."}}},
	}
	channels, err := d.seedChannels(ctx, st)
	if err != nil {
		return err
	}
	for _, l := range list {
		l.a.LastSeen = cmp.Or(l.a.ResolvedAt, d.start)
		a, err := st.AddAlert(ctx, l.a)
		if err != nil {
			return err
		}
		l.a.ID = a.ID
		if err := st.SaveAlert(ctx, l.a); err != nil {
			return err
		}
		for _, n := range l.notes {
			if _, err := st.AddAlertNote(ctx, a.ID, n); err != nil {
				return err
			}
		}
		for _, c := range channels {
			err := st.SetNotified(ctx, store.Notified{AlertID: a.ID, ChannelID: c.ID, Severity: l.a.Severity, At: l.a.OpenedAt.Add(time.Minute)})
			if err != nil {
				return err
			}
		}
	}
	return d.seedEvents(ctx, st)
}

// seedChannels adds the notification channels: an ntfy topic and a webhook, both
// on the tailnet, where the demo's servers take whatever Roost sends.
func (d *Demo) seedChannels(ctx context.Context, st *store.Store) ([]store.Channel, error) {
	host := "http://homelab.tail-demo.ts.net"
	configs := []struct {
		name, kind, min string
		resolve         bool
		cfg             notify.Config
	}{
		{"Phone (ntfy)", "ntfy", "warning", true, notify.Config{Server: host + ":8090", Topic: "roost-alerts"}},
		{"Node-RED", "webhook", "info", false, notify.Config{URL: host + ":1880/hook/roost"}},
	}
	var out []store.Channel
	for _, c := range configs {
		raw, err := json.Marshal(c.cfg) //nolint:gosec // G117: a channel keeps its secrets in its config.
		if err != nil {
			return nil, err
		}
		ch, err := st.AddChannel(ctx, store.Channel{
			Name: c.name, Kind: c.kind, Enabled: true, MinSeverity: c.min, OnResolve: c.resolve, Config: string(raw),
			Created: d.start.Add(-30 * 24 * time.Hour),
		})
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, nil
}

// seedEvents writes the activity log of the past days.
func (d *Demo) seedEvents(ctx context.Context, st *store.Store) error {
	id := func(host string) string { return d.machineNamed(host, d.machines[0]).id }
	ago := func(dur time.Duration) time.Time { return d.start.Add(-dur) }
	h := time.Hour
	events := []store.Event{
		{At: ago(8 * 24 * h), Level: "success", Kind: "device", Message: "Sam's iPad came online", DeviceID: id("ipad")},
		{At: ago(7*24*h + 3*h), Level: "info", Kind: "check", Message: "Added the check Photos", Actor: alex},
		{At: ago(6*24*h + 9*h), Level: "info", Kind: "settings", Message: "Changed the notification settings", Actor: alex},
		{At: ago(5*24*h + 2*h), Level: "success", Kind: "agent", Message: "The agent on Kitchen display started reporting", DeviceID: id("pi-kitchen")},
		{At: ago(4*24*h + 9*h), Level: "warning", Kind: "alert", Message: "Memory 93% used on Frankfurt VPS", DeviceID: id("vps-fra")},
		{At: ago(4*24*h + 8*h + 30*time.Minute), Level: "success", Kind: "alert", Message: "Resolved: Memory 93% used on Frankfurt VPS", DeviceID: id("vps-fra")},
		{At: ago(3*24*h + 9*h), Level: "warning", Kind: "alert", Message: "backup.service failed", DeviceID: d.machines[0].id},
		{At: ago(3*24*h + 5*h), Level: "info", Kind: "alert", Message: "Wrote a note on: backup.service failed", DeviceID: d.machines[0].id, Actor: alex},
		{At: ago(3*24*h + 4*h), Level: "success", Kind: "alert", Message: "Resolved: backup.service failed", DeviceID: d.machines[0].id},
		{At: ago(2*24*h + 6*h), Level: "warning", Kind: "agent", Message: "The agent on MacBook Pro stopped reporting", DeviceID: id("macbook")},
		{At: ago(2*24*h + 5*h + 20*time.Minute), Level: "success", Kind: "agent", Message: "The agent on MacBook Pro is reporting again", DeviceID: id("macbook")},
		{At: ago(2*24*h + 2*h), Level: "info", Kind: "alert", Message: "Wrote a note on: Disk 90% full on NAS", DeviceID: id("nas"), Actor: alex},
		{At: ago(31 * h), Level: "warning", Kind: "alert", Message: "Blog is failing"},
		{At: ago(30*h + 38*time.Minute), Level: "success", Kind: "alert", Message: "Resolved: Blog is failing"},
		{At: ago(27 * h), Level: "warning", Kind: "alert", Message: "Status page is failing"},
		{At: ago(26*h + 40*time.Minute), Level: "warning", Kind: "alert", Message: "CPU 94% busy on New York VPS", DeviceID: id("vps-nyc")},
		{At: ago(26 * h), Level: "warning", Kind: "device", Message: "Sam's iPad went offline", DeviceID: id("ipad")},
		{At: ago(25*h + 30*time.Minute), Level: "info", Kind: "alert", Message: "Wrote a note on: CPU 94% busy on New York VPS", DeviceID: id("vps-nyc"), Actor: alex},
		{At: ago(25*h + 18*time.Minute), Level: "success", Kind: "alert", Message: "Resolved: CPU 94% busy on New York VPS", DeviceID: id("vps-nyc")},
		{At: ago(25 * h), Level: "success", Kind: "alert", Message: "Resolved: Status page is failing"},
		{At: ago(9 * h), Level: "warning", Kind: "container", Message: "Container duplicati was killed: out of memory", DeviceID: id("nas")},
		{At: ago(3*h + 20*time.Minute), Level: "warning", Kind: "device", Message: "Garage Pi went offline", DeviceID: id("pi-garage")},
		{At: ago(3*h + 10*time.Minute), Level: "warning", Kind: "alert", Message: "Garage Pi is offline", DeviceID: id("pi-garage")},
		{At: ago(3*h + 7*time.Minute), Level: "error", Kind: "alert", Message: "Garage camera is failing"},
		{At: ago(2 * h), Level: "info", Kind: "alert", Message: "Wrote a note on: Garage camera is failing", Actor: alex},
		{At: ago(90 * time.Minute), Level: "info", Kind: "alert", Message: "Snoozed for 6h: Garage camera is failing", Actor: alex},
		{At: ago(26 * time.Minute), Level: "warning", Kind: "container", Message: "Container qbittorrent became unhealthy", DeviceID: id("nas")},
	}
	return st.AddEvents(ctx, events...)
}

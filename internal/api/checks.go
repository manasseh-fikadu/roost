package api

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/checks"
	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

const (
	// pingPrefix starts the ID of a ping check, before its device's ID.
	pingPrefix = "ping:"
	// pingGroup is the group the ping checks are listed in.
	pingGroup = "Tailnet"
)

// When the health score counts as healthy or degraded, and below that,
// critical.
const (
	healthyScore  = 90
	degradedScore = 70
)

// newChecks returns the checks admins set up, from src, and then a ping
// check for each peer in devices.
func newChecks(src sources, devices []Device) []Check {
	out := make([]Check, 0, len(src.checks)+len(devices))
	for _, s := range src.checks {
		out = append(out, newCheck(s))
	}
	for _, d := range devices {
		if !d.Self {
			out = append(out, newPingCheck(d, src.pings[d.ID]))
		}
	}
	return out
}

// newCheck returns a check that an admin set up, as the API shows it.
func newCheck(s checks.Status) Check {
	c := s.Check
	out := Check{
		ID:           c.ID,
		Name:         c.Name,
		Kind:         CheckKind(c.Kind),
		Target:       c.Target,
		Endpoint:     endpoint(c),
		Group:        c.Group,
		IntervalSec:  int(c.Interval / time.Second),
		TimeoutMs:    int(c.Timeout / time.Millisecond),
		ExpectStatus: c.ExpectStatus,
		Insecure:     c.Insecure,
		Paused:       c.Paused,
		Last:         lastResult(s.Recent),
		Failures:     s.Failures,
		Uptime:       roundUptime(s.Uptime),
	}
	switch {
	case c.Paused:
		out.State, out.StateReason = CheckStatePaused, "Paused"
	case len(s.Recent) == 0:
		out.State, out.StateReason = CheckStatePending, "Waiting for the first result"
	default:
		last := s.Recent[len(s.Recent)-1]
		out.State, out.StateReason = CheckStateFailing, last.Detail
		if last.OK {
			out.State = CheckStatePassing
		}
	}
	return out
}

// endpoint returns the part of c's target that can't hold a secret: for a
// URL, its scheme, host and port, since the rest can hold a token, as in
// https://hc-ping.com/<uuid>.
func endpoint(c store.Check) string {
	if c.Kind != checks.KindHTTP {
		return c.Target
	}
	u, err := url.Parse(c.Target)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// newPingCheck returns the check of Bield's pings of d, as the API shows
// it. It fails when d stops answering pings, as d's page says, which takes
// two missed pings in a row.
func newPingCheck(d Device, p checks.PingStatus) Check {
	ip := ""
	for _, a := range d.IPs {
		if !strings.Contains(a, ":") {
			ip = a
			break
		}
	}
	if ip == "" && len(d.IPs) > 0 {
		ip = d.IPs[0]
	}
	out := Check{
		ID:          pingPrefix + d.ID,
		Name:        "Ping " + d.Name,
		Kind:        CheckKindPing,
		Target:      ip,
		Endpoint:    ip,
		Group:       pingGroup,
		IntervalSec: int(tailnet.PingInterval / time.Second),
		TimeoutMs:   int(tailnet.PingTimeout / time.Millisecond),
		DeviceID:    d.ID,
		Last:        lastResult(p.Recent),
		Failures:    p.Misses,
		Uptime:      roundUptime(p.Uptime),
	}
	switch {
	case !d.Online:
		out.State, out.StateReason = CheckStateUnavailable, "Offline"
	case len(p.Recent) == 0:
		out.State, out.StateReason = CheckStatePending, "Waiting for the first ping"
	case d.Path.Unanswered:
		out.State, out.StateReason = CheckStateFailing, fmt.Sprintf("Missed %d pings in a row", p.Misses)
	case !p.Recent[len(p.Recent)-1].OK:
		out.State, out.StateReason = CheckStatePassing, "Missed the latest ping"
	default:
		out.State, out.StateReason = CheckStatePassing, p.Recent[len(p.Recent)-1].Detail
	}
	return out
}

// lastResult returns the last of rs as the API shows it, or nil if there
// are none.
func lastResult(rs []checks.Result) *CheckResult {
	if len(rs) == 0 {
		return nil
	}
	return &newResults(rs[len(rs)-1:])[0]
}

func newResults(rs []checks.Result) []CheckResult {
	out := make([]CheckResult, len(rs))
	for i, r := range rs {
		out[i] = CheckResult{At: r.At, OK: r.OK, Detail: r.Detail}
		if r.Latency > 0 {
			// A tenth of a millisecond is close enough, and shorter.
			v := math.Round(ms(r.Latency)*10) / 10
			out[i].LatencyMs = &v
		}
	}
	return out
}

func roundUptime(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return new(math.Round(*v*100) / 100)
}

// newHealthScore sums up cs.
func newHealthScore(cs []Check) HealthScore {
	h := HealthScore{Label: HealthLabelUnknown, Total: len(cs)}
	for _, c := range cs {
		switch c.State {
		case CheckStatePassing:
			h.Passing++
		case CheckStateFailing:
			h.Failing++
		case CheckStatePending, CheckStatePaused, CheckStateUnavailable:
			h.Unavailable++
		}
	}
	score, ok := scoreOf(h)
	if !ok {
		return h
	}
	// Rounding down, a score of 100 means every live check passes.
	rounded := int(math.Floor(score))
	h.Score = &rounded
	switch {
	case rounded >= healthyScore:
		h.Label = HealthLabelHealthy
	case rounded >= degradedScore:
		h.Label = HealthLabelDegraded
	default:
		h.Label = HealthLabelCritical
	}
	return h
}

// scoreOf returns the percentage of h's live checks that pass, if any are
// live.
func scoreOf(h HealthScore) (float64, bool) {
	live := h.Passing + h.Failing
	if live == 0 {
		return 0, false
	}
	return float64(h.Passing) / float64(live) * 100, true
}

// checkInput returns the check in, as Clean takes it.
func checkInput(in CheckInput) store.Check {
	return store.Check{
		Name:         in.Name,
		Kind:         string(in.Kind),
		Target:       in.Target,
		Group:        in.Group,
		Interval:     time.Duration(in.IntervalSec) * time.Second,
		Timeout:      time.Duration(in.TimeoutMs) * time.Millisecond,
		ExpectStatus: in.ExpectStatus,
		Insecure:     in.Insecure,
		Paused:       in.Paused,
	}
}

// checkAnswer returns the check c as the API shows it, with what the runner
// knows of it.
func (h *handler) checkAnswer(c store.Check) Check {
	s, ok := h.opts.Checks.Check(c.ID)
	if !ok {
		s = checks.Status{Check: c}
	}
	return newCheck(s)
}

// addCheck answers POST /api/checks, which adds a check.
func (h *handler) addCheck(w http.ResponseWriter, r *http.Request) {
	var in CheckInput
	if !h.readJSON(w, r, &in) {
		return
	}
	c, err := checks.Clean(checkInput(in))
	if err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	if c, err = h.opts.Checks.Add(r.Context(), c); err != nil {
		h.fail(w, r, "add a check", err)
		return
	}
	h.log.Info("added a check", "check", c.ID, "name", c.Name, "by", callerOf(r).who.LoginName)
	h.logAction(r, EventKindCheck, "", "Added the check "+c.Name)
	h.writeJSON(w, r, http.StatusCreated, h.checkAnswer(c))
}

// changeCheck answers PUT /api/checks/{id}, which replaces a check.
func (h *handler) changeCheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.pingCheck(w, r, id) {
		return
	}
	var in CheckInput
	if !h.readJSON(w, r, &in) {
		return
	}
	c := checkInput(in)
	c.ID = id
	c, err := checks.Clean(c)
	if err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	c, err = h.opts.Checks.Change(r.Context(), c)
	switch {
	case errors.Is(err, checks.ErrNotFound):
		h.noCheck(w, r)
		return
	case err != nil:
		h.fail(w, r, "change a check", err)
		return
	}
	h.log.Info("changed a check", "check", c.ID, "name", c.Name, "by", callerOf(r).who.LoginName)
	h.logAction(r, EventKindCheck, "", "Changed the check "+c.Name)
	h.writeJSON(w, r, http.StatusOK, h.checkAnswer(c))
}

// deleteCheck answers DELETE /api/checks/{id}, which deletes a check and
// its results.
func (h *handler) deleteCheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.pingCheck(w, r, id) {
		return
	}
	old, _ := h.opts.Checks.Check(id)
	err := h.opts.Checks.Delete(r.Context(), id)
	switch {
	case errors.Is(err, checks.ErrNotFound):
		h.noCheck(w, r)
		return
	case err != nil:
		h.fail(w, r, "delete a check", err)
		return
	}
	h.log.Info("deleted a check", "check", id, "by", callerOf(r).who.LoginName)
	h.logAction(r, EventKindCheck, "", "Deleted the check "+old.Check.Name)
	w.WriteHeader(http.StatusNoContent)
}

// runCheck answers POST /api/checks/{id}/run, which runs a check now, with
// its result.
func (h *handler) runCheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.pingCheck(w, r, id) {
		return
	}
	res, err := h.opts.Checks.RunNow(r.Context(), id)
	switch {
	case errors.Is(err, checks.ErrNotFound):
		h.noCheck(w, r)
		return
	case err != nil && r.Context().Err() != nil:
		// The caller went away.
		return
	case err != nil:
		h.writeJSON(w, r, http.StatusServiceUnavailable, ErrorBody{Error: "the check didn't run: " + err.Error()})
		return
	}
	h.writeJSON(w, r, http.StatusOK, newResults([]checks.Result{res})[0])
}

// pingCheck answers a request to change a ping check, and returns true, if
// id is a ping check's.
func (h *handler) pingCheck(w http.ResponseWriter, r *http.Request, id string) bool {
	if !strings.HasPrefix(id, pingPrefix) {
		return false
	}
	h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{
		Error: "Bield pings every peer on its own, so its ping checks can't be changed or run",
	})
	return true
}

func (h *handler) noCheck(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: "no check has this ID"})
}

// recentResults answers GET /api/checks/recent.
func (h *handler) recentResults(w http.ResponseWriter, r *http.Request) {
	out := RecentResults{Checks: map[string][]CheckResult{}}
	for _, s := range h.opts.Checks.Checks() {
		out.Checks[s.Check.ID] = newResults(s.Recent)
	}
	for id, p := range h.opts.Checks.Pings() {
		out.Checks[pingPrefix+id] = newResults(p.Recent)
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// checkHistory answers GET /api/checks/{id}/history.
func (h *handler) checkHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	from, to, step, ok := h.historyRange(w, r)
	if !ok {
		return
	}
	out := CheckHistory{From: from, To: to, Step: step.Milliseconds(), Latency: []Point{}, Uptime: []Point{}}
	if deviceID, ping := strings.CutPrefix(id, pingPrefix); ping {
		d, _, found := h.deviceByID(deviceID)
		if !found {
			h.noCheck(w, r)
			return
		}
		pings, err := h.opts.Store.RTTHistory(r.Context(), d.ID, from, step)
		if err != nil {
			h.fail(w, r, "read a check's history", err)
			return
		}
		for _, b := range pings {
			out.add(b.Start, b.Pings, b.Replies, b.RTT)
		}
		h.writeJSON(w, r, http.StatusOK, out)
		return
	}
	if _, found := h.opts.Checks.Check(id); !found {
		h.noCheck(w, r)
		return
	}
	results, err := h.opts.Store.CheckHistory(r.Context(), id, from, step)
	if err != nil {
		h.fail(w, r, "read a check's history", err)
		return
	}
	for _, b := range results {
		out.add(b.Start, b.Runs, b.Passed, b.Latency)
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// add adds the points of a step that starts at start, in which the check ran
// runs times and passed ok times, and the target took latency on average to
// answer.
func (h *CheckHistory) add(start time.Time, runs, ok int, latency time.Duration) {
	ts := start.UnixMilli()
	p := Point{TS: ts}
	if latency > 0 {
		p.V = new(math.Round(ms(latency)*10) / 10)
	}
	h.Latency = append(h.Latency, p)
	h.Uptime = append(h.Uptime, Point{TS: ts, V: new(float64(ok) / float64(runs) * 100)})
}

// healthHistory answers GET /api/health/history.
func (h *handler) healthHistory(w http.ResponseWriter, r *http.Request) {
	from, to, step, ok := h.historyRange(w, r)
	if !ok {
		return
	}
	buckets, err := h.opts.Store.HealthHistory(r.Context(), from, step)
	if err != nil {
		h.fail(w, r, "read the health history", err)
		return
	}
	out := HealthHistory{From: from, To: to, Step: step.Milliseconds(), Score: make([]Point, len(buckets))}
	for i, b := range buckets {
		out.Score[i] = Point{TS: b.Start.UnixMilli(), V: new(math.Round(b.Score*10) / 10)}
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

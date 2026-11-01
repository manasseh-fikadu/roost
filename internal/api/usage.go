package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/manasseh-fikadu/bield/internal/usage"
)

const (
	// usageNear is how far from the time asked for a stored minute can be.
	// Past a day only one minute in ten keeps its detail, so it's a few
	// minutes either way.
	usageNear = 15 * time.Minute
	// usageKept is how far back stored minutes go, as in store's retention.
	usageKept = 7 * 24 * time.Hour
)

// deviceUsage answers GET /api/devices/{id}/usage, with the device's latest
// sample, or with ?at=, the stored minute nearest that time.
func (h *handler) deviceUsage(w http.ResponseWriter, r *http.Request) {
	_, historyID, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	var (
		s        usage.Sample
		interval time.Duration
		err      error
	)
	if raw := r.URL.Query().Get("at"); raw != "" {
		at, perr := strconv.ParseInt(raw, 10, 64)
		if perr != nil || at <= 0 {
			h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: "at must be a time in Unix milliseconds"})
			return
		}
		_, interval, _ = h.opts.Usage.Latest(historyID)
		s, ok, err = h.opts.Usage.At(r.Context(), historyID, time.UnixMilli(at), usageNear)
	} else {
		s, interval, ok = h.opts.Usage.Latest(historyID)
		if !ok {
			// Until the device sends a sample after Bield starts, the latest
			// stored minute will do.
			s, ok, err = h.opts.Usage.At(r.Context(), historyID, time.Now(), usageKept)
		}
	}
	if err != nil {
		h.fail(w, r, "read a device's usage", err)
		return
	}
	out := DeviceUsage{Interval: int(interval / time.Second)}
	if ok {
		out.Sample = newUsageSample(s)
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// usageHistory answers GET /api/devices/{id}/usage/history.
func (h *handler) usageHistory(w http.ResponseWriter, r *http.Request) {
	_, historyID, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	from, to, step, ok := h.historyRange(w, r)
	if !ok {
		return
	}
	hist, err := h.opts.Usage.History(r.Context(), historyID, from, step)
	if err != nil {
		h.fail(w, r, "read a device's usage history", err)
		return
	}
	out := UsageHistory{
		From: from, To: to, Step: step.Milliseconds(), Minutes: hist.Minutes,
		Consumers: make([]UsageConsumer, len(hist.Consumers)),
		CPU:       newUsageSeries(hist.CPU),
		Mem:       newUsageSeries(hist.Mem),
	}
	for i, c := range hist.Consumers {
		out.Consumers[i] = UsageConsumer{
			Key: c.Key, Kind: UsageKind(c.Kind), Name: c.Name,
			CPUAvg: c.CPUAvg, CPUPeak: c.CPUPeak, MemAvg: c.MemAvg, MemPeak: c.MemPeak,
		}
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

func newUsageSample(s usage.Sample) *UsageSample {
	out := &UsageSample{
		At:     s.At,
		Cores:  s.Cores,
		Groups: make([]UsageGroup, len(s.Groups)),
		Procs:  make([]UsageProc, len(s.Procs)),
	}
	for i, g := range s.Groups {
		top := g.Top
		if top == nil {
			top = []string{}
		}
		out.Groups[i] = UsageGroup{Key: g.Key, Kind: UsageKind(g.Kind), Name: g.Name, CPU: g.CPU, Mem: g.Mem, Procs: g.Procs, Top: top}
	}
	for i, p := range s.Procs {
		out.Procs[i] = UsageProc{PID: p.PID, Name: p.Name, User: p.User, Cmd: p.Cmd, CPU: p.CPU, Mem: p.Mem, Group: p.Group}
	}
	if t := s.Total; t != nil {
		out.Total = &UsageTotal{CPU: t.CPU, Mem: t.Mem, Procs: t.Procs}
	}
	return out
}

func newUsageSeries(series []usage.Series) []UsageSeries {
	out := make([]UsageSeries, len(series))
	for i, s := range series {
		points := make([]Point, len(s.Points))
		for j, p := range s.Points {
			points[j] = Point{TS: p.At.UnixMilli(), V: new(p.V)}
		}
		out[i] = UsageSeries{Key: s.Key, Kind: UsageKind(s.Kind), Name: s.Name, Points: points}
	}
	return out
}

package api

import (
	"cmp"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/manasseh-fikadu/roost/internal/settings"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// The longest a device's display name and notes can be, in characters.
const (
	maxDisplayName = 64
	maxNotes       = 4000
)

// historyRanges are the spans GET /api/devices/{id}/history covers, and the
// time each point sums up.
var historyRanges = map[string]struct{ span, step time.Duration }{
	"1h":  {time.Hour, time.Minute},
	"6h":  {6 * time.Hour, 2 * time.Minute},
	"24h": {24 * time.Hour, 5 * time.Minute},
	"7d":  {7 * 24 * time.Hour, time.Hour},
	// Past two days, a device's metrics and pings come from the buckets
	// that package store folds them into, which keep them up to 90 days.
	"30d": {30 * 24 * time.Hour, 2 * time.Hour},
	"90d": {90 * 24 * time.Hour, 6 * time.Hour},
}

// device returns the device on the tailnet with the ID in r's path, and the
// ID its history is kept under.
func (h *handler) device(r *http.Request) (tailnet.Device, string, bool) {
	return h.deviceByID(r.PathValue("id"))
}

// deviceByID returns the device on the tailnet with an ID, and the ID its
// history is kept under.
func (h *handler) deviceByID(id string) (tailnet.Device, string, bool) {
	st := h.opts.Snapshot().Status
	if st == nil {
		return tailnet.Device{}, "", false
	}
	if st.Self.ID == id {
		return st.Self, historyID(st.Self, true), true
	}
	for _, d := range st.Peers {
		if d.ID == id {
			return d, historyID(d, false), true
		}
	}
	return tailnet.Device{}, "", false
}

func (h *handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: "no device with this ID is on the tailnet"})
}

// setDeviceSettings answers PUT /api/devices/{id}/settings, which replaces a
// device's settings, with the settings as they are now.
func (h *handler) setDeviceSettings(w http.ResponseWriter, r *http.Request) {
	d, _, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	var body SetDeviceSettings
	if !h.readJSON(w, r, &body) {
		return
	}
	ds, err := cleanSettings(body, d, h.opts.Settings.Get(r.Context()).Thresholds)
	if err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	if err := h.opts.Store.SetDeviceSettings(r.Context(), d.ID, ds); err != nil {
		h.fail(w, r, "save device settings", err)
		return
	}
	h.log.Info("changed a device's settings", "device", d.ID, "by", callerOf(r).who.LoginName)
	h.logAction(r, EventKindDevice, d.ID, "Changed the settings of "+h.deviceName(r, d.ID))
	h.opts.Feed.Poke()
	h.writeJSON(w, r, http.StatusOK, DeviceSettings{
		DisplayName:  ds.DisplayName,
		Kind:         DeviceKind(ds.Kind),
		AlertOffline: alertOffline(d, ds),
		Thresholds:   newDeviceThresholds(ds.Thresholds),
		Notes:        ds.Notes,
	})
}

// cleanSettings checks the settings an admin sent for d, and returns them as
// the store keeps them. Alerting when d goes offline is only stored if it
// differs from the default, so the default can change later. global are the
// settings' thresholds, which the device's own must fit with.
func cleanSettings(s SetDeviceSettings, d tailnet.Device, global settings.Thresholds) (store.DeviceSettings, error) {
	name := strings.TrimSpace(s.DisplayName)
	switch {
	case utf8.RuneCountInString(name) > maxDisplayName:
		return store.DeviceSettings{}, errors.New("the display name is too long")
	case strings.ContainsFunc(name, unicode.IsControl):
		return store.DeviceSettings{}, errors.New("the display name can only be one line")
	case s.Kind != "" && !validKind(s.Kind):
		return store.DeviceSettings{}, errors.New("unknown kind of device " + string(s.Kind))
	case utf8.RuneCountInString(s.Notes) > maxNotes:
		return store.DeviceSettings{}, errors.New("the notes are too long")
	}
	own := store.ThresholdOverrides(s.Thresholds)
	if err := global.CheckOverrides(own); err != nil {
		return store.DeviceSettings{}, err
	}
	out := store.DeviceSettings{
		DisplayName: name, Kind: string(s.Kind), Thresholds: own,
		Notes: strings.TrimRightFunc(s.Notes, unicode.IsSpace),
	}
	if s.AlertOffline != nil && *s.AlertOffline != defaultAlertOffline(d) {
		out.AlertOffline = s.AlertOffline
	}
	return out, nil
}

// historyRange reads the range of a request for history, such as
// ?range=6h, which is 24h if it's left out. If the range is unknown, it
// answers with an error and returns false. Points start at multiples of the
// step, so the first starts a little before the span does.
func (h *handler) historyRange(w http.ResponseWriter, r *http.Request) (from, to time.Time, step time.Duration, ok bool) {
	rg, ok := historyRanges[cmp.Or(r.URL.Query().Get("range"), "24h")]
	if !ok {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: "the range must be 1h, 6h, 24h, 7d, 30d or 90d"})
		return time.Time{}, time.Time{}, 0, false
	}
	to = time.Now()
	ms := rg.step.Milliseconds()
	return time.UnixMilli(to.Add(-rg.span).UnixMilli() / ms * ms), to, rg.step, true
}

// deviceHistory answers GET /api/devices/{id}/history.
func (h *handler) deviceHistory(w http.ResponseWriter, r *http.Request) {
	d, historyID, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	from, to, step, ok := h.historyRange(w, r)
	if !ok {
		return
	}
	pings, err := h.opts.Store.RTTHistory(r.Context(), d.ID, from, step)
	if err != nil {
		h.fail(w, r, "read a device's history", err)
		return
	}
	metrics, err := h.opts.Store.MetricsHistory(r.Context(), historyID, from, step)
	if err != nil {
		h.fail(w, r, "read a device's history", err)
		return
	}
	out := DeviceHistory{
		From: from, To: to, Step: step.Milliseconds(),
		CPU:    make([]Point, len(metrics)),
		Memory: make([]Point, len(metrics)),
		NetRx:  make([]Point, len(metrics)),
		NetTx:  make([]Point, len(metrics)),
		RTT:    make([]Point, 0, len(pings)),
	}
	for i, b := range metrics {
		ts := b.Start.UnixMilli()
		out.CPU[i], out.Memory[i] = Point{ts, b.CPU}, Point{ts, b.Mem}
		out.NetRx[i], out.NetTx[i] = Point{ts, b.NetRx}, Point{ts, b.NetTx}
	}
	for _, b := range pings {
		p := Point{TS: b.Start.UnixMilli()}
		if b.Replies > 0 {
			v := ms(b.RTT)
			p.V = &v
		}
		out.RTT = append(out.RTT, p)
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

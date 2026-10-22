package api

import (
	"cmp"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
}

// device returns the device on the tailnet with the ID in r's path.
func (h *handler) device(r *http.Request) (tailnet.Device, bool) {
	id := r.PathValue("id")
	st := h.opts.Snapshot().Status
	if st == nil {
		return tailnet.Device{}, false
	}
	if st.Self.ID == id {
		return st.Self, true
	}
	for _, d := range st.Peers {
		if d.ID == id {
			return d, true
		}
	}
	return tailnet.Device{}, false
}

func (h *handler) notFound(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: "no device with this ID is on the tailnet"})
}

// setDeviceSettings answers PUT /api/devices/{id}/settings, which replaces a
// device's settings, with the settings as they are now.
func (h *handler) setDeviceSettings(w http.ResponseWriter, r *http.Request) {
	d, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	var body SetDeviceSettings
	if !h.readJSON(w, r, &body) {
		return
	}
	ds, err := cleanSettings(body, d)
	if err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	if err := h.opts.Store.SetDeviceSettings(r.Context(), d.ID, ds); err != nil {
		h.fail(w, r, "save device settings", err)
		return
	}
	h.log.Info("changed a device's settings", "device", d.ID, "by", callerOf(r).who.LoginName)
	h.opts.Feed.Poke()
	h.writeJSON(w, r, http.StatusOK, DeviceSettings{
		DisplayName:  ds.DisplayName,
		Kind:         DeviceKind(ds.Kind),
		AlertOffline: alertOffline(d, ds),
		Notes:        ds.Notes,
	})
}

// cleanSettings checks the settings an admin sent for d, and returns them as
// the store keeps them. Alerting when d goes offline is only stored if it
// differs from the default, so the default can change later.
func cleanSettings(s SetDeviceSettings, d tailnet.Device) (store.DeviceSettings, error) {
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
	out := store.DeviceSettings{DisplayName: name, Kind: string(s.Kind), Notes: strings.TrimRightFunc(s.Notes, unicode.IsSpace)}
	if s.AlertOffline != nil && *s.AlertOffline != defaultAlertOffline(d) {
		out.AlertOffline = s.AlertOffline
	}
	return out, nil
}

// deviceHistory answers GET /api/devices/{id}/history.
func (h *handler) deviceHistory(w http.ResponseWriter, r *http.Request) {
	d, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return
	}
	rg, ok := historyRanges[cmp.Or(r.URL.Query().Get("range"), "24h")]
	if !ok {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: "the range must be 1h, 6h, 24h or 7d"})
		return
	}
	now := time.Now()
	step := rg.step.Milliseconds()
	// Points start at multiples of the step, so the first starts a little
	// before the span does.
	from := time.UnixMilli(now.Add(-rg.span).UnixMilli() / step * step)
	buckets, err := h.opts.Store.RTTHistory(r.Context(), d.ID, from, rg.step)
	if err != nil {
		h.fail(w, r, "read a device's history", err)
		return
	}
	out := DeviceHistory{From: from, To: now, Step: step, RTT: make([]Point, 0, len(buckets))}
	for _, b := range buckets {
		p := Point{TS: b.Start.UnixMilli()}
		if b.Replies > 0 {
			v := ms(b.RTT)
			p.V = &v
		}
		out.RTT = append(out.RTT, p)
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

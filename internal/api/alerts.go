package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/manasseh-fikadu/roost/internal/alerts"
	"github.com/manasseh-fikadu/roost/internal/store"
)

const (
	// listLimit is how many alerts or events a page has, unless the request
	// says fewer.
	listLimit = 100
	// maxSnoozeMinutes is the longest an alert can be snoozed for.
	maxSnoozeMinutes = int(alerts.MaxSnooze / time.Minute)
)

// newAlerts returns alerts as the API shows them, at now.
func newAlerts(as []store.Alert, now time.Time) []Alert {
	out := make([]Alert, len(as))
	for i, a := range as {
		out[i] = newAlert(a, now)
	}
	return out
}

func newAlert(a store.Alert, now time.Time) Alert {
	out := Alert{
		ID:           a.ID,
		Source:       AlertSource(a.Source),
		DeviceID:     a.DeviceID,
		CheckID:      a.CheckID,
		Severity:     AlertSeverity(a.Severity),
		Title:        a.Title,
		Detail:       a.Detail,
		OpenedAt:     a.OpenedAt,
		LastSeen:     a.LastSeen,
		ResolvedAt:   timePtr(a.ResolvedAt),
		AckedAt:      timePtr(a.AckedAt),
		AckedBy:      a.AckedBy,
		Notes:        make([]AlertNote, len(a.Notes)),
		State:        AlertStateOpen,
		SnoozedUntil: timePtr(a.SnoozedUntil),
		SnoozedBy:    a.SnoozedBy,
	}
	for i, n := range a.Notes {
		out.Notes[i] = AlertNote{At: n.At, By: n.Author, Text: n.Text}
	}
	switch {
	case !a.ResolvedAt.IsZero():
		out.State = AlertStateResolved
	case a.SnoozedUntil.After(now):
		out.State = AlertStateSnoozed
	case !a.AckedAt.IsZero():
		out.State = AlertStateAcknowledged
	}
	// A snooze that's over says nothing.
	if out.State != AlertStateSnoozed {
		out.SnoozedUntil, out.SnoozedBy = nil, ""
	}
	return out
}

// listAlerts answers GET /api/alerts with resolved alerts.
func (h *handler) listAlerts(w http.ResponseWriter, r *http.Request) {
	before, limit, ok := h.page(w, r, "before")
	if !ok {
		return
	}
	// One more than the page says whether there are more.
	as, err := h.opts.Store.ResolvedAlerts(r.Context(), before, limit+1)
	if err != nil {
		h.fail(w, r, "list resolved alerts", err)
		return
	}
	out := AlertList{More: len(as) > limit}
	out.Alerts = newAlerts(as[:min(len(as), limit)], time.Now())
	h.writeJSON(w, r, http.StatusOK, out)
}

// page reads a page's cursor, the ID in the query parameter named cursor,
// and its limit. If they're bad, it answers with an error and returns false.
func (h *handler) page(w http.ResponseWriter, r *http.Request, cursor string) (id int64, limit int, ok bool) {
	q := r.URL.Query()
	limit = listLimit
	if s := q.Get(cursor); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v < 0 {
			h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: cursor + " must be an ID"})
			return 0, 0, false
		}
		id = v
	}
	if s := q.Get("limit"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v < 1 || v > listLimit {
			h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: "the limit must be from 1 to " + strconv.Itoa(listLimit)})
			return 0, 0, false
		}
		limit = v
	}
	return id, limit, true
}

// alertID reads the alert ID in r's path. If it's bad, it answers 404 and
// returns false.
func (h *handler) alertID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.noAlert(w, r, "no alert has this ID")
		return 0, false
	}
	return id, true
}

func (h *handler) noAlert(w http.ResponseWriter, r *http.Request, msg string) {
	h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: msg})
}

// ackAlert answers PUT and DELETE /api/alerts/{id}/ack, which acknowledge
// an open alert and take that back.
func (h *handler) ackAlert(w http.ResponseWriter, r *http.Request) {
	id, ok := h.alertID(w, r)
	if !ok {
		return
	}
	now := time.Now()
	a, err := h.opts.Alerts.Ack(r.Context(), id, callerOf(r).who.LoginName, r.Method == http.MethodPut, now)
	h.answerAlert(w, r, a, err, now)
}

// snoozeAlert answers PUT /api/alerts/{id}/snooze, which snoozes an open
// alert, and DELETE, which wakes it.
func (h *handler) snoozeAlert(w http.ResponseWriter, r *http.Request) {
	id, ok := h.alertID(w, r)
	if !ok {
		return
	}
	now := time.Now()
	var until time.Time
	if r.Method == http.MethodPut {
		var body SnoozeAlert
		if !h.readJSON(w, r, &body) {
			return
		}
		if body.Minutes < 1 || body.Minutes > maxSnoozeMinutes {
			h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: "an alert can be snoozed for a minute to 30 days"})
			return
		}
		until = now.Add(time.Duration(body.Minutes) * time.Minute)
	}
	a, err := h.opts.Alerts.Snooze(r.Context(), id, callerOf(r).who.LoginName, until, now)
	h.answerAlert(w, r, a, err, now)
}

func (h *handler) answerAlert(w http.ResponseWriter, r *http.Request, a store.Alert, err error, now time.Time) {
	switch {
	case errors.Is(err, alerts.ErrNotOpen):
		h.noAlert(w, r, "no open alert has this ID")
		return
	case err != nil:
		h.fail(w, r, "change an alert", err)
		return
	}
	h.opts.Feed.Poke()
	h.writeJSON(w, r, http.StatusOK, newAlert(a, now))
}

// addAlertNote answers POST /api/alerts/{id}/notes, which adds a note to an
// alert, with the note.
func (h *handler) addAlertNote(w http.ResponseWriter, r *http.Request) {
	id, ok := h.alertID(w, r)
	if !ok {
		return
	}
	var body AddNote
	if !h.readJSON(w, r, &body) {
		return
	}
	text, err := alerts.CleanNote(body.Text)
	if err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	n, err := h.opts.Alerts.AddNote(r.Context(), id, callerOf(r).who.LoginName, text, time.Now())
	switch {
	case errors.Is(err, store.ErrNoAlert):
		h.noAlert(w, r, "no alert has this ID")
		return
	case err != nil && errors.Is(err, r.Context().Err()):
		return
	case err != nil:
		h.fail(w, r, "add a note to an alert", err)
		return
	}
	h.opts.Feed.Poke()
	h.writeJSON(w, r, http.StatusCreated, AlertNote{At: n.At, By: n.Author, Text: n.Text})
}

// listEvents answers GET /api/events with a page of the activity log.
func (h *handler) listEvents(w http.ResponseWriter, r *http.Request) {
	before, limit, ok := h.page(w, r, "before")
	if !ok {
		return
	}
	after, _, ok := h.page(w, r, "after")
	if !ok {
		return
	}
	q := r.URL.Query()
	query := store.EventQuery{Before: before, After: after, Kind: q.Get("kind"), DeviceID: q.Get("device"), Limit: limit + 1}
	// Who has a role is for admins, like GET /api/access, and so are SSH
	// access and terminals, which only admins can use.
	if callerOf(r).level < admins {
		query.HideKinds = []string{string(EventKindAccess), string(EventKindSSH), string(EventKindTerminal), string(EventKindFiles)}
	}
	events, err := h.opts.Store.Events(r.Context(), query)
	if err != nil {
		h.fail(w, r, "read the activity log", err)
		return
	}
	out := EventList{Events: make([]Event, 0, min(len(events), limit)), More: len(events) > limit}
	for _, e := range events[:min(len(events), limit)] {
		out.Events = append(out.Events, Event{
			ID: e.ID, At: e.At, Level: EventLevel(e.Level), Kind: EventKind(e.Kind),
			Message: e.Message, DeviceID: e.DeviceID, Actor: e.Actor,
		})
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// logAction adds what an admin did to the activity log.
func (h *handler) logAction(r *http.Request, kind EventKind, deviceID, message string) {
	h.logEvent(r, store.Event{Kind: string(kind), Message: message, DeviceID: deviceID, Actor: callerOf(r).who.LoginName})
}

// logEvent adds an event to the activity log, now and at the info level
// unless it says.
func (h *handler) logEvent(r *http.Request, e store.Event) {
	h.addEvent(r.Context(), e)
}

// eventTimeout is the longest Roost waits to write a line of the activity log.
const eventTimeout = 10 * time.Second

// addEvent is logEvent for code that has no request.
func (h *handler) addEvent(ctx context.Context, e store.Event) {
	e.At = time.Now()
	if e.Level == "" {
		e.Level = string(EventLevelInfo)
	}
	// What an admin did is logged even if their browser went away while it
	// was happening, or a download took longer than they stayed.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), eventTimeout)
	defer cancel()
	if err := h.opts.Store.AddEvents(ctx, e); err != nil {
		h.log.Warn("add to the activity log", "error", err)
	}
	h.opts.Feed.Poke()
}

// deviceName returns the name the app shows for the device with an ID, or
// the ID if it's not on the tailnet.
func (h *handler) deviceName(r *http.Request, id string) string {
	if p := h.opts.Feed.latest(r.Context()); p != nil {
		for _, d := range p.snap.Devices {
			if d.ID == id {
				return d.Name
			}
		}
	}
	return id
}

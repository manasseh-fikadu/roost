package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/manasseh-fikadu/roost/internal/notify"
	"github.com/manasseh-fikadu/roost/internal/store"
)

// maxChannels is how many notification channels Roost can have.
const maxChannels = 20

// listChannels answers GET /api/notifications/channels.
func (h *handler) listChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.channels(r)
	if err != nil {
		h.fail(w, r, "read the notification channels", err)
		return
	}
	out := NotificationChannels{Channels: make([]NotificationChannel, len(channels))}
	for i, c := range channels {
		out.Channels[i] = h.newChannel(c)
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// channels returns the notification channels. One that can't be read, as if
// its config was edited by hand, is left out.
func (h *handler) channels(r *http.Request) ([]notify.Channel, error) {
	rows, err := h.opts.Store.Channels(r.Context())
	if err != nil {
		return nil, err
	}
	out := make([]notify.Channel, 0, len(rows))
	for _, row := range rows {
		c, err := notify.FromStore(row)
		if err != nil {
			h.log.Warn("read a notification channel", "id", row.ID, "error", err)
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// newChannel returns c as the API shows it: without its secrets.
func (h *handler) newChannel(c notify.Channel) NotificationChannel {
	cfg := c.Config
	out := NotificationChannel{
		ID: c.ID, Name: c.Name, Kind: ChannelKind(c.Kind), Enabled: c.Enabled,
		MinSeverity: AlertSeverity(c.MinSeverity), OnResolve: c.OnResolve,
		Config: ChannelConfig{
			Server: cfg.Server, Topic: cfg.Topic, ChatID: cfg.ChatID, Host: cfg.Host, Port: cfg.Port,
			Security: cfg.Security, Username: cfg.Username, From: cfg.From, To: cfg.To,
		},
		Secrets: []string{},
		Summary: c.Summary(),
	}
	for _, name := range notify.Secrets(c.Kind) {
		if cfg.HasSecret(name) {
			out.Secrets = append(out.Secrets, name)
		}
	}
	if dl, ok := h.opts.Notify.Last(c.ID); ok {
		out.Last = &ChannelDelivery{At: dl.At, Event: string(dl.Event), OK: dl.OK, Error: dl.Error}
	}
	return out
}

// channelOf returns the channel that in describes, as it would be after the
// request. The secrets that in leaves out are those of old, if there is one.
func channelOf(in ChannelInput, old *notify.Channel) notify.Channel {
	kind := notify.Kind(in.Kind)
	cfg := notify.Config{
		Server: in.Config.Server, Topic: in.Config.Topic, ChatID: in.Config.ChatID, Host: in.Config.Host,
		Port: in.Config.Port, Security: in.Config.Security, Username: in.Config.Username, From: in.Config.From,
		To: in.Config.To,
	}
	if old != nil {
		cfg.URL, cfg.Token, cfg.Password = old.Config.URL, old.Config.Token, old.Config.Password
	}
	set(&cfg.URL, in.Config.URL)
	set(&cfg.Token, in.Config.Token)
	set(&cfg.Password, in.Config.Password)
	c := notify.Channel{
		Name: strings.TrimSpace(in.Name), Kind: kind, Enabled: in.Enabled, MinSeverity: string(in.MinSeverity),
		OnResolve: in.OnResolve, Config: cfg.Normalize(kind),
	}
	if old != nil {
		c.ID = old.ID
	}
	return c
}

// addChannel answers POST /api/notifications/channels.
func (h *handler) addChannel(w http.ResponseWriter, r *http.Request) {
	var in ChannelInput
	if !h.readJSON(w, r, &in) {
		return
	}
	c := channelOf(in, nil)
	if err := c.Check(); err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	existing, err := h.opts.Store.Channels(r.Context())
	if err != nil {
		h.fail(w, r, "read the notification channels", err)
		return
	}
	if len(existing) >= maxChannels {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: "Roost can send to up to " + strconv.Itoa(maxChannels) + " channels"})
		return
	}
	row := c.ToStore()
	row.Created = time.Now()
	row, err = h.opts.Store.AddChannel(r.Context(), row)
	if h.channelFailed(w, r, "add a notification channel", err) {
		return
	}
	c.ID = row.ID
	h.log.Info("added a notification channel", "channel", c.ID, "name", c.Name, "kind", c.Kind, "by", callerOf(r).who.LoginName)
	h.logAction(r, EventKindNotification, "", "Added the notification channel "+c.Name)
	h.writeJSON(w, r, http.StatusCreated, h.newChannel(c))
}

// channelFailed answers a request whose store call returned err, and reports
// whether it did.
func (h *handler) channelFailed(w http.ResponseWriter, r *http.Request, what string, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrChannelNameTaken):
		h.writeJSON(w, r, http.StatusConflict, ErrorBody{Error: "another channel already has this name"})
	case errors.Is(err, store.ErrNoChannel):
		h.noChannel(w, r)
	default:
		h.fail(w, r, what, err)
	}
	return true
}

func (h *handler) noChannel(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, r, http.StatusNotFound, ErrorBody{Error: "no notification channel has this ID"})
}

// channelByID returns the channel with the ID in r's path. If there's none,
// it answers and reports false.
func (h *handler) channelByID(w http.ResponseWriter, r *http.Request) (notify.Channel, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.noChannel(w, r)
		return notify.Channel{}, false
	}
	channels, err := h.channels(r)
	if err != nil {
		h.fail(w, r, "read the notification channels", err)
		return notify.Channel{}, false
	}
	for _, c := range channels {
		if c.ID == id {
			return c, true
		}
	}
	h.noChannel(w, r)
	return notify.Channel{}, false
}

// changeChannel answers PUT /api/notifications/channels/{id}.
func (h *handler) changeChannel(w http.ResponseWriter, r *http.Request) {
	old, ok := h.channelByID(w, r)
	if !ok {
		return
	}
	var in ChannelInput
	if !h.readJSON(w, r, &in) {
		return
	}
	if notify.Kind(in.Kind) != old.Kind {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: "a channel's kind can't change; add a new channel instead"})
		return
	}
	c := channelOf(in, &old)
	if err := c.Check(); err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	if h.channelFailed(w, r, "change a notification channel", h.opts.Store.UpdateChannel(r.Context(), c.ToStore())) {
		return
	}
	h.log.Info("changed a notification channel", "channel", c.ID, "name", c.Name, "by", callerOf(r).who.LoginName)
	h.logAction(r, EventKindNotification, "", "Changed the notification channel "+c.Name)
	h.writeJSON(w, r, http.StatusOK, h.newChannel(c))
}

// deleteChannel answers DELETE /api/notifications/channels/{id}.
func (h *handler) deleteChannel(w http.ResponseWriter, r *http.Request) {
	old, ok := h.channelByID(w, r)
	if !ok {
		return
	}
	if h.channelFailed(w, r, "delete a notification channel", h.opts.Store.DeleteChannel(r.Context(), old.ID)) {
		return
	}
	h.opts.Notify.Forget(old.ID)
	h.log.Info("deleted a notification channel", "channel", old.ID, "name", old.Name, "by", callerOf(r).who.LoginName)
	h.logAction(r, EventKindNotification, "", "Deleted the notification channel "+old.Name)
	w.WriteHeader(http.StatusNoContent)
}

// testChannel answers POST /api/notifications/channels/{id}/test, which
// sends a test message and says how it went.
func (h *handler) testChannel(w http.ResponseWriter, r *http.Request) {
	c, ok := h.channelByID(w, r)
	if !ok {
		return
	}
	dl := h.opts.Notify.Test(r.Context(), c, callerOf(r).who.LoginName)
	if r.Context().Err() != nil {
		return
	}
	h.writeJSON(w, r, http.StatusOK, ChannelDelivery{At: dl.At, Event: string(dl.Event), OK: dl.OK, Error: dl.Error})
}

// getAlert answers GET /api/alerts/{id} with an alert, open or resolved.
func (h *handler) getAlert(w http.ResponseWriter, r *http.Request) {
	id, ok := h.alertID(w, r)
	if !ok {
		return
	}
	now := time.Now()
	// An open alert is fresher in the engine than in the store.
	for _, a := range h.opts.Alerts.Open() {
		if a.ID == id {
			h.writeJSON(w, r, http.StatusOK, newAlert(a, now))
			return
		}
	}
	a, err := h.opts.Store.AlertByID(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNoAlert):
		h.noAlert(w, r, "no alert has this ID")
		return
	case err != nil:
		h.fail(w, r, "read an alert", err)
		return
	}
	h.writeJSON(w, r, http.StatusOK, newAlert(a, now))
}

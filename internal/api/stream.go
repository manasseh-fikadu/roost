package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

const (
	// streamInterval is how often GET /api/stream sends a snapshot: as often
	// as Roost polls the tailnet's status.
	streamInterval = tailnet.StatusInterval
	// streamWriteTimeout is how long a client gets to take one event before
	// Roost gives up on it. It's shorter than the time the server gives
	// responses to finish when it shuts down, so a stalled client can't hold
	// the shutdown up.
	streamWriteTimeout = 5 * time.Second
)

// stream answers GET /api/stream with server-sent events: a "snapshot" event
// with a Snapshot as soon as it opens, and another every streamInterval. It
// ends when the client goes away, when the server shuts down, or when the
// caller's role changes, so that the client reconnects and is checked again.
func (h *handler) stream(w http.ResponseWriter, r *http.Request) {
	c := callerOf(r)
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// The server still writes the end of the response after this returns, and
	// a client that stopped reading mustn't make that wait forever.
	defer func() { _ = setWriteDeadline(rc, time.Now().Add(streamWriteTimeout)) }()

	t := time.NewTicker(streamInterval)
	defer t.Stop()
	for {
		snap := newSnapshot(h.opts.Snapshot(), h.opts.Version, time.Now())
		if c.level < admins {
			snap = redact(snap)
		}
		if err := sendEvent(w, rc, "snapshot", snap); err != nil {
			h.log.Debug("send an event", "path", r.URL.Path, "error", err)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-h.opts.Stopping:
			return
		case <-t.C:
		}
		// If the store can't say, the stream carries on, and the next tick
		// asks again. Only a role that's known to differ ends it.
		if l, err := h.levelOf(r.Context(), c.who.LoginName); err == nil && l != c.level {
			return
		}
	}
}

// sendEvent writes one server-sent event and flushes it to the client.
func sendEvent(w io.Writer, rc *http.ResponseController, event string, v any) error {
	// JSON escapes every newline inside strings, so data fits on one line.
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	// The deadline covers only this write. Left in place, it would end an
	// idle HTTP/2 stream when it passed, even with nothing left to write.
	if err := setWriteDeadline(rc, time.Now().Add(streamWriteTimeout)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	if err := rc.Flush(); err != nil {
		return err
	}
	return setWriteDeadline(rc, time.Time{})
}

// setWriteDeadline sets the response's write deadline, if the connection
// supports one.
func setWriteDeadline(rc *http.ResponseController, t time.Time) error {
	if err := rc.SetWriteDeadline(t); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

// newSnapshot makes a Snapshot from what the tailnet poller knows at now.
func newSnapshot(s tailnet.Snapshot, version string, now time.Time) Snapshot {
	out := Snapshot{
		GeneratedAt: now,
		Hub:         Hub{Version: version},
		Collectors: []Collector{
			newCollector("Tailscale status", s.StatusAt, s.StatusErr),
			newCollector("Tailscale netcheck", s.ConnectivityAt, s.ConnectivityErr),
		},
		Devices: []Device{},
	}
	if st := s.Status; st != nil {
		out.Hub.DNSName, out.Hub.Tailnet = st.Self.DNSName, st.Tailnet
		self := newDevice(st.Self)
		self.Self = true
		out.Devices = append(out.Devices, self)
		for _, d := range st.Peers {
			out.Devices = append(out.Devices, newDevice(d))
		}
	}
	return out
}

// newCollector reports on a collector that last worked at lastOK, if that
// isn't zero, and failed with err, if it isn't nil.
func newCollector(name string, lastOK time.Time, err error) Collector {
	c := Collector{Name: name, State: CollectorStateOK, LastOK: timePtr(lastOK)}
	switch {
	case err != nil:
		c.State, c.Error = CollectorStateFailing, err.Error()
	case lastOK.IsZero():
		c.State = CollectorStateWaiting
	}
	return c
}

func newDevice(d tailnet.Device) Device {
	name := d.HostName
	if name == "" {
		name, _, _ = strings.Cut(d.DNSName, ".")
	}
	ips := make([]string, len(d.IPs))
	for i, ip := range d.IPs {
		ips[i] = ip.String()
	}
	return Device{ID: d.ID, Name: name, DNSName: d.DNSName, OS: d.OS, IPs: ips, Online: d.Online}
}

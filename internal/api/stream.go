package api

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// roleCheckInterval is how often GET /api/stream checks that the
	// caller's role hasn't changed.
	roleCheckInterval = 10 * time.Second
	// streamWriteTimeout is how long a client gets to take one event before
	// Roost gives up on it. It's shorter than the time the server gives
	// responses to finish when it shuts down, so a stalled client can't hold
	// the shutdown up.
	streamWriteTimeout = 5 * time.Second
)

// stream answers GET /api/stream with server-sent events: a "snapshot" event
// with the latest Snapshot as soon as it opens, and then each new one the
// Feed publishes. If the client accepts gzip, the stream is one gzip stream,
// and each event is flushed so the client can read it at once. It ends when
// the client goes away, when the server shuts down, or when the caller's role
// changes, so that the client reconnects and is checked again.
func (h *handler) stream(w http.ResponseWriter, r *http.Request) {
	c := callerOf(r)
	p := h.opts.Feed.latest(r.Context())
	if p == nil {
		h.fail(w, r, "build a snapshot", errors.New("the first snapshot couldn't be encoded"))
		return
	}
	ew := &eventWriter{w: w, rc: http.NewResponseController(w), gzip: acceptsGzip(r)}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Accept-Encoding")
	if ew.gzip {
		w.Header().Set("Content-Encoding", "gzip")
	}
	w.WriteHeader(http.StatusOK)
	// The server still writes the end of the response after this returns, and
	// a client that stopped reading mustn't make that wait forever.
	defer func() { _ = ew.close() }()

	roleCheck := time.NewTicker(roleCheckInterval)
	defer roleCheck.Stop()
	for {
		ev := p.viewer
		if c.level >= admins {
			ev = p.admin
		}
		if err := ew.send(ev); err != nil {
			h.log.Debug("send an event", "path", r.URL.Path, "error", err)
			return
		}
	wait:
		for {
			select {
			case <-r.Context().Done():
				return
			case <-h.opts.Stopping:
				return
			case <-p.next:
				p = h.opts.Feed.latest(r.Context())
				break wait
			case <-roleCheck.C:
				// If the store can't say, the stream carries on, and the next
				// check asks again. Only a role that's known to differ ends it.
				if l, err := h.levelOf(r.Context(), c.who.LoginName); err == nil && l != c.level {
					return
				}
			}
		}
	}
}

// acceptsGzip reports whether r's client takes gzip-encoded responses.
func acceptsGzip(r *http.Request) bool {
	for _, header := range r.Header.Values("Accept-Encoding") {
		for part := range strings.SplitSeq(header, ",") {
			name, params, _ := strings.Cut(part, ";")
			if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
				continue
			}
			// gzip;q=0 turns it down.
			q, ok := strings.CutPrefix(strings.ReplaceAll(params, " ", ""), "q=")
			if !ok {
				return true
			}
			weight, err := strconv.ParseFloat(q, 64)
			return err == nil && weight > 0
		}
	}
	return false
}

// gzipHeader starts a gzip stream with no file name or time, from an unknown
// OS.
var gzipHeader = []byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 0xff}

// eventWriter writes events to a response, as they are or as one gzip
// stream.
type eventWriter struct {
	w    io.Writer
	rc   *http.ResponseController
	gzip bool

	started bool
	// crc and size are the CRC-32 and length of everything sent, which end a
	// gzip stream.
	crc  uint32
	size uint64
}

// send writes ev and flushes it to the client.
func (e *eventWriter) send(ev *event) error {
	// The deadline covers only this write. Left in place, it would end an
	// idle HTTP/2 stream when it passed, even with nothing left to write.
	if err := setWriteDeadline(e.rc, time.Now().Add(streamWriteTimeout)); err != nil {
		return err
	}
	if err := e.write(ev); err != nil {
		return err
	}
	if err := e.rc.Flush(); err != nil {
		return err
	}
	return setWriteDeadline(e.rc, time.Time{})
}

func (e *eventWriter) write(ev *event) error {
	if !e.gzip {
		_, err := e.w.Write(ev.raw)
		return err
	}
	if !e.started {
		if _, err := e.w.Write(gzipHeader); err != nil {
			return err
		}
		e.started = true
	}
	// Each event's DEFLATE blocks stand alone, so any number of them in a
	// row make one valid stream.
	if _, err := e.w.Write(ev.deflated); err != nil {
		return err
	}
	e.crc = crc32.Update(e.crc, crc32.IEEETable, ev.raw)
	e.size += uint64(len(ev.raw))
	return nil
}

// close ends a gzip stream, so the client sees it end cleanly. It leaves a
// write deadline in place for the rest of the response.
func (e *eventWriter) close() error {
	if err := setWriteDeadline(e.rc, time.Now().Add(streamWriteTimeout)); err != nil || !e.gzip || !e.started {
		return err
	}
	// An empty final block, then the CRC-32 and the length modulo 2^32 of
	// all the data.
	end := []byte{3, 0}
	end = binary.LittleEndian.AppendUint32(end, e.crc)
	end = binary.LittleEndian.AppendUint32(end, uint32(e.size)) //nolint:gosec // G115: gzip keeps the length modulo 2^32.
	if _, err := e.w.Write(end); err != nil {
		return err
	}
	return e.rc.Flush()
}

// setWriteDeadline sets the response's write deadline, if the connection
// supports one.
func setWriteDeadline(rc *http.ResponseController, t time.Time) error {
	if err := rc.SetWriteDeadline(t); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"time"
)

const (
	// sseKeepAlive is how often a stream that has nothing to say writes a
	// comment, so a dead connection shows itself and a proxy doesn't close
	// an idle one.
	sseKeepAlive = 20 * time.Second
)

// An sse writes server-sent events to a response. It compresses them if the
// client takes gzip, and flushes each one, since a container's log text
// shrinks several times over and a slow link feels that. Every write has a
// deadline, so a client that stops reading ends the stream instead of
// holding it.
type sse struct {
	w  http.ResponseWriter
	rc *http.ResponseController
	gz *gzip.Writer
}

// openSSE starts a stream of events on w.
func openSSE(w http.ResponseWriter, r *http.Request) *sse {
	s := &sse{w: w, rc: http.NewResponseController(w)}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Accept-Encoding")
	if acceptsGzip(r) {
		w.Header().Set("Content-Encoding", "gzip")
		s.gz = gzip.NewWriter(w)
	}
	w.WriteHeader(http.StatusOK)
	return s
}

// send writes an event with a name and JSON data, and flushes it.
func (s *sse) send(event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	out := make([]byte, 0, len(event)+len(b)+16)
	out = append(out, "event: "...)
	out = append(out, event...)
	out = append(out, "\ndata: "...)
	out = append(out, b...)
	out = append(out, "\n\n"...)
	return s.write(out)
}

// keepAlive writes a comment, which clients ignore.
func (s *sse) keepAlive() error { return s.write([]byte(": keep-alive\n\n")) }

func (s *sse) write(b []byte) error {
	// The deadline covers only this write, as in the stream of snapshots.
	if err := setWriteDeadline(s.rc, time.Now().Add(streamWriteTimeout)); err != nil {
		return err
	}
	if s.gz != nil {
		if _, err := s.gz.Write(b); err != nil {
			return err
		}
		if err := s.gz.Flush(); err != nil {
			return err
		}
	} else if _, err := s.w.Write(b); err != nil {
		return err
	}
	if err := s.rc.Flush(); err != nil {
		return err
	}
	return setWriteDeadline(s.rc, time.Time{})
}

// close ends the stream, which a client sees as the end of a gzip stream.
func (s *sse) close() {
	if s.gz == nil {
		return
	}
	_ = setWriteDeadline(s.rc, time.Now().Add(streamWriteTimeout))
	_ = s.gz.Close()
	_ = s.rc.Flush()
}

// streamContext returns a context for a response that goes on until the
// client goes away or the server shuts down.
func (h *handler) streamContext(r *http.Request) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(r.Context())
	if h.opts.Stopping != nil {
		go func() {
			select {
			case <-h.opts.Stopping:
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	return ctx, cancel
}

// callerRoleChanged reports whether the caller's role is no longer what it was
// when they asked, so a stream can end and the client asks again. If the
// store can't say, it isn't.
func (h *handler) callerRoleChanged(ctx context.Context, c caller) bool {
	l, err := h.levelOf(ctx, c.who.LoginName)
	return err == nil && l != c.level
}

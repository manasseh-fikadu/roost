package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// deadlineRecorder is a ResponseWriter that records the write deadlines set
// on it.
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.deadlines = append(d.deadlines, t)
	return nil
}

// An event's write deadline must not outlive the write: over HTTP/2, a
// deadline that passes while the stream waits for the next event resets it.
func TestSendEvent_clearsWriteDeadline(t *testing.T) {
	t.Parallel()
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}

	if err := sendEvent(w, http.NewResponseController(w), "snapshot", map[string]int{"n": 1}); err != nil {
		t.Fatalf("sendEvent() error = %v", err)
	}

	if got, want := w.Body.String(), "event: snapshot\ndata: {\"n\":1}\n\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if !w.Flushed {
		t.Error("the event wasn't flushed")
	}
	if len(w.deadlines) != 2 || w.deadlines[0].IsZero() || !w.deadlines[1].IsZero() {
		t.Errorf("write deadlines = %v, want one for the write, then none", w.deadlines)
	}
}

package api

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// Clients that join at different times get the same compressed bytes for
// each event, and each one's stream decodes, event by event as it's
// flushed, to the events it was sent.
func TestEventWriter(t *testing.T) {
	t.Parallel()
	f := &Feed{}
	var events []*event
	for _, data := range []any{map[string]int{"n": 1}, strings.Repeat("roost ", 2000), map[string]int{"n": 3}} {
		ev, err := f.newEvent("snapshot", data)
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, ev)
	}
	if got, want := string(events[0].raw), "event: snapshot\ndata: {\"n\":1}\n\n"; got != want {
		t.Fatalf("first event = %q, want %q", got, want)
	}

	for _, tt := range []struct {
		name string
		gzip bool
		// from is the first event the client gets.
		from int
	}{
		{name: "plain", gzip: false},
		{name: "gzip", gzip: true},
		{name: "gzip, joining later", gzip: true, from: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
			ew := &eventWriter{w: w, rc: http.NewResponseController(w), gzip: tt.gzip}
			var want bytes.Buffer
			for i, ev := range events[tt.from:] {
				if err := ew.send(ev); err != nil {
					t.Fatalf("send() error = %v", err)
				}
				want.Write(ev.raw)
				if got, _ := decode(w.Body.Bytes(), tt.gzip); got != want.String() {
					t.Fatalf("after event %d, the client can read %q, want %q", i, got, want.String())
				}
			}
			if err := ew.close(); err != nil {
				t.Fatalf("close() error = %v", err)
			}
			got, err := decode(w.Body.Bytes(), tt.gzip)
			if err != nil || got != want.String() {
				t.Errorf("whole stream = %q, %v; want %q and no error", got, err, want.String())
			}

			// Each send sets a deadline and then clears it, and close leaves
			// one in place.
			n := len(events) - tt.from
			if len(w.deadlines) != 2*n+1 {
				t.Fatalf("write deadlines = %v, want %d", w.deadlines, 2*n+1)
			}
			for i, d := range w.deadlines {
				if want := i%2 == 1; d.IsZero() != want {
					t.Errorf("write deadline %d = %v, want cleared: %v", i, d, want)
				}
			}
		})
	}
}

// decode returns what a client can read of a response so far, and the error
// it hits at the end, if any.
func decode(body []byte, gzipped bool) (string, error) {
	if !gzipped {
		return string(body), nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	zr.Multistream(false)
	got, err := io.ReadAll(zr)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return string(got), err
	}
	return string(got), errors.Join(err, zr.Close())
}

func TestAcceptsGzip(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		header []string
		want   bool
	}{
		{nil, false},
		{[]string{"gzip"}, true},
		{[]string{"gzip, deflate, br, zstd"}, true},
		{[]string{"br", "GZIP"}, true},
		{[]string{"br;q=1.0, gzip;q=0.5"}, true},
		{[]string{"gzip;q=0"}, false},
		{[]string{"gzip; q=0.0"}, false},
		{[]string{"identity"}, false},
		{[]string{"x-gzip"}, false},
	} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/stream", nil)
		for _, v := range tt.header {
			r.Header.Add("Accept-Encoding", v)
		}
		if got := acceptsGzip(r); got != tt.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", tt.header, got, tt.want)
		}
	}
}

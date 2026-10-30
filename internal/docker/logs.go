package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A LogLine is one line of a container's output.
type LogLine struct {
	// Stream is 1 for standard output and 2 for standard error.
	Stream int
	// Time is the RFC 3339 time Docker stamped the line with, as it wrote it,
	// so asking for the logs since then resumes exactly. It's empty if the
	// line has none.
	Time string
	Text string
}

const (
	// maxLogLine is the longest a line gets, so output without a newline
	// can't grow without bound.
	maxLogLine = 64 << 10
	// maxFrame is the largest frame of multiplexed output Roost waits for.
	// Docker's are far smaller, so a larger size means the stream isn't
	// multiplexed after all.
	maxFrame = 1 << 20
)

// stamp matches the time Docker puts in front of a line when asked to.
var stamp = regexp.MustCompile(`^\d{4}-\d\d-\d\dT[\d:.]+(Z|[+-]\d\d:\d\d) `)

// A LogDecoder cuts a container's output into lines. Without a TTY, Docker
// multiplexes standard output and error into frames, each with an 8-byte
// header: the stream, three zeros, and the size as a big-endian uint32. With
// one, it sends the terminal's bytes as they are. The decoder carries
// partial frames and lines from one chunk to the next.
type LogDecoder struct {
	multiplexed, timestamps bool
	pending                 []byte
	partial                 [3][]byte
}

// NewLogDecoder returns a decoder for output that is multiplexed or not, and
// that has Docker's timestamps or not.
func NewLogDecoder(multiplexed, timestamps bool) *LogDecoder {
	return &LogDecoder{multiplexed: multiplexed, timestamps: timestamps}
}

// Push adds the lines in chunk to out, and returns it.
func (d *LogDecoder) Push(chunk []byte, out []LogLine) []LogLine {
	if !d.multiplexed {
		return d.feed(1, chunk, out)
	}
	data := chunk
	if len(d.pending) > 0 {
		data = append(d.pending, chunk...)
	}
	for len(data) >= 8 {
		kind, size := data[0], binary.BigEndian.Uint32(data[4:8])
		if kind > 2 || data[1] != 0 || data[2] != 0 || data[3] != 0 || size > maxFrame {
			// Not a header after all, as when Docker doesn't say a container
			// has a TTY: the rest is text.
			d.multiplexed, d.pending = false, nil
			return d.feed(1, data, out)
		}
		if len(data)-8 < int(size) {
			break
		}
		out = d.feed(int(kind), data[8:8+size], out)
		data = data[8+size:]
	}
	d.pending = append(d.pending[:0], data...)
	return out
}

// Flush adds the lines that were waiting for a newline to out, and returns
// it. Call it when the output ends.
func (d *LogDecoder) Flush(out []LogLine) []LogLine {
	for s := range d.partial {
		if len(d.partial[s]) > 0 {
			out = d.emit(s, d.take(s, nil), out)
		}
	}
	return out
}

// feed adds the lines in data, which is output of a stream, to out.
func (d *LogDecoder) feed(stream int, data []byte, out []LogLine) []LogLine {
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			break
		}
		out = d.emit(stream, d.take(stream, data[:i]), out)
		data = data[i+1:]
	}
	if len(data) > 0 {
		d.partial[stream] = append(d.partial[stream], data...)
		if len(d.partial[stream]) > maxLogLine {
			out = d.emit(stream, d.take(stream, nil), out)
		}
	}
	return out
}

// take returns what a stream has left of a line, followed by tail, and
// starts it again.
func (d *LogDecoder) take(stream int, tail []byte) []byte {
	if len(d.partial[stream]) == 0 {
		return tail
	}
	line := append(d.partial[stream], tail...)
	d.partial[stream] = nil
	return line
}

func (d *LogDecoder) emit(stream int, line []byte, out []LogLine) []LogLine {
	// A TTY ends lines with \r\n, and programs sometimes add another \r.
	text := string(bytes.TrimRight(line, "\r"))
	l := LogLine{Stream: 1}
	if stream == 2 {
		l.Stream = 2
	}
	if d.timestamps {
		if loc := stamp.FindStringIndex(text); loc != nil {
			l.Time, text = text[:loc[1]-1], text[loc[1]:]
		}
	}
	l.Text = text
	return append(out, l)
}

// ansi matches the escape sequences that colour and move a terminal's text.
var ansi = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|\x1b[@-Z\\\\-_]")

// StripANSI removes the terminal escape sequences from s.
func StripANSI(s string) string { return ansi.ReplaceAllString(s, "") }

// ErrBadSince is the error for a time that is none of the forms ParseSince
// reads.
var ErrBadSince = errors.New("the time must be Unix seconds, an RFC 3339 time, or an age such as 15m, 6h or 7d")

var (
	unixSince = regexp.MustCompile(`^\d+(\.\d+)?$`)
	ageSince  = regexp.MustCompile(`^(\d{1,9})([smhd])$`)
	ageUnits  = map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}
)

// ParseSince turns the time a caller asks for logs since into the form
// Docker reads: Unix seconds, with nanoseconds if there are any. It takes
// that form, an RFC 3339 time, which is what each line carries so a
// reconnect can resume exactly, or an age such as 15m, 6h or 7d. An empty
// string means no limit.
func ParseSince(s string, now time.Time) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || unixSince.MatchString(s) {
		return s, nil
	}
	if m := ageSince.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return strconv.FormatInt(now.Add(-time.Duration(n)*ageUnits[m[2]]).Unix(), 10), nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return "", ErrBadSince
	}
	return fmt.Sprintf("%d.%09d", t.Unix(), t.Nanosecond()), nil
}

// LogOptions say which of a container's logs to read.
type LogOptions struct {
	// Follow keeps the stream open for new lines.
	Follow bool
	// Stdout and Stderr choose the streams.
	Stdout, Stderr bool
	// Tail is how many lines from the end to start with, or -1 for all.
	Tail int
	// Since is when to start, as ParseSince returns it, or empty.
	Since string
}

// Logs starts reading a container's logs, with Docker's timestamps, and
// returns a decoder for them, and the reader of the output. It ends when ctx
// does, or when the reader is closed. A container with a TTY is told by tty,
// which Inspect says.
func (c *Client) Logs(ctx context.Context, id string, tty bool, o LogOptions) (io.ReadCloser, *LogDecoder, error) {
	q := url.Values{"timestamps": {"1"}}
	q.Set("follow", boolParam(o.Follow))
	q.Set("stdout", boolParam(o.Stdout))
	q.Set("stderr", boolParam(o.Stderr))
	if o.Tail < 0 {
		q.Set("tail", "all")
	} else {
		q.Set("tail", strconv.Itoa(o.Tail))
	}
	if o.Since != "" {
		q.Set("since", o.Since)
	}
	resp, err := c.stream(ctx, "/containers/"+url.PathEscape(id)+"/logs", q)
	if err != nil {
		return nil, nil, err
	}
	multiplexed := !tty || strings.Contains(resp.Header.Get("Content-Type"), "multiplexed")
	return resp.Body, NewLogDecoder(multiplexed, true), nil
}

func boolParam(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

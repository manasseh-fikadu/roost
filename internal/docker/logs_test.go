package docker_test

import (
	"encoding/binary"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/manasseh-fikadu/roost/internal/docker"
)

// frame returns text as a frame of a multiplexed stream.
func frame(stream byte, text string) []byte {
	b := []byte{stream, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(b[4:], uint32(len(text))) //nolint:gosec // G115: a test's frame is small.
	return append(b, text...)
}

// decode pushes the chunks through a decoder, and flushes it.
func decode(multiplexed, timestamps bool, chunks ...[]byte) []docker.LogLine {
	d := docker.NewLogDecoder(multiplexed, timestamps)
	var out []docker.LogLine
	for _, c := range chunks {
		out = d.Push(c, out)
	}
	return d.Flush(out)
}

func TestLogDecoder_multiplexed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		chunks [][]byte
		want   []docker.LogLine
	}{
		{
			name: "both streams, with their timestamps",
			chunks: [][]byte{
				frame(1, "2027-03-01T10:00:00.123456789Z hello\n"),
				frame(2, "2027-03-01T10:00:01Z oops\n"),
			},
			want: []docker.LogLine{
				{Stream: 1, Time: "2027-03-01T10:00:00.123456789Z", Text: "hello"},
				{Stream: 2, Time: "2027-03-01T10:00:01Z", Text: "oops"},
			},
		},
		{
			name: "a frame cut in the middle of its header and of its text",
			chunks: func() [][]byte {
				all := slices.Concat(frame(1, "one\ntwo\n"), frame(1, "three\n"))
				return [][]byte{all[:3], all[3:11], all[11:14], all[14:]}
			}(),
			want: []docker.LogLine{{Stream: 1, Text: "one"}, {Stream: 1, Text: "two"}, {Stream: 1, Text: "three"}},
		},
		{
			name:   "a line in two frames, with another stream's line between them",
			chunks: [][]byte{slices.Concat(frame(1, "par"), frame(2, "err\n"), frame(1, "tial\n"))},
			want:   []docker.LogLine{{Stream: 2, Text: "err"}, {Stream: 1, Text: "partial"}},
		},
		{
			name:   "output that ends without a newline",
			chunks: [][]byte{frame(1, "done\nand then")},
			want:   []docker.LogLine{{Stream: 1, Text: "done"}, {Stream: 1, Text: "and then"}},
		},
		{
			name:   "a blank line and a line with a carriage return",
			chunks: [][]byte{frame(1, "\nwindows\r\n")},
			want:   []docker.LogLine{{Stream: 1}, {Stream: 1, Text: "windows"}},
		},
		{
			name:   "a line without a timestamp stays whole",
			chunks: [][]byte{frame(1, "2027 was a year\n")},
			want:   []docker.LogLine{{Stream: 1, Text: "2027 was a year"}},
		},
		{
			name:   "stdin counts as stdout",
			chunks: [][]byte{frame(0, "typed\n")},
			want:   []docker.LogLine{{Stream: 1, Text: "typed"}},
		},
		{
			name:   "text where a header should be reads as text",
			chunks: [][]byte{[]byte("plain output\nwithout frames\n")},
			want:   []docker.LogLine{{Stream: 1, Text: "plain output"}, {Stream: 1, Text: "without frames"}},
		},
		{
			name:   "a header that claims a huge frame reads as text",
			chunks: [][]byte{append([]byte{1, 0, 0, 0, 0xff, 0xff, 0xff, 0xff}, "tail\n"...)},
			want:   []docker.LogLine{{Stream: 1, Text: "\x01\x00\x00\x00\xff\xff\xff\xfftail"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := decode(true, true, tt.chunks...)
			if !slices.Equal(got, tt.want) {
				t.Errorf("lines = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLogDecoder_longLineWithStampedParts(t *testing.T) {
	t.Parallel()
	// Docker cuts a line over 16 KiB into messages, each with its own
	// timestamp. The line has the first stamp, and none inside it.
	got := decode(true, true,
		frame(1, "2027-03-01T10:00:00.000000001Z aaaa"),
		frame(1, "2027-03-01T10:00:00.000000002Z bbbb"),
		frame(1, "2027-03-01T10:00:00.000000003Z cccc\n"),
		frame(1, "2027-03-01T10:00:01Z next\n"),
	)
	want := []docker.LogLine{
		{Stream: 1, Time: "2027-03-01T10:00:00.000000001Z", Text: "aaaabbbbcccc"},
		{Stream: 1, Time: "2027-03-01T10:00:01Z", Text: "next"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %+v, want %+v", got, want)
	}
	// The other stream's parts don't take the stamp off this one's.
	got = decode(true, true,
		frame(1, "2027-03-01T10:00:00Z out-one "),
		frame(2, "2027-03-01T10:00:01Z err\n"),
		frame(1, "2027-03-01T10:00:02Z out-two\n"),
	)
	want = []docker.LogLine{
		{Stream: 2, Time: "2027-03-01T10:00:01Z", Text: "err"},
		{Stream: 1, Time: "2027-03-01T10:00:00Z", Text: "out-one out-two"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %+v, want %+v", got, want)
	}
}

func TestLogDecoder_tty(t *testing.T) {
	t.Parallel()
	got := decode(false, true, []byte("2027-03-01T10:00:00Z $ ls\r\n2027-03-01T10:00:01Z fi"), []byte("le\r\r\n"))
	want := []docker.LogLine{
		{Stream: 1, Time: "2027-03-01T10:00:00Z", Text: "$ ls"},
		{Stream: 1, Time: "2027-03-01T10:00:01Z", Text: "file"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("lines = %+v, want %+v", got, want)
	}
}

func TestLogDecoder_withoutTimestamps(t *testing.T) {
	t.Parallel()
	got := decode(true, false, frame(1, "2027-03-01T10:00:00Z hello\n"))
	if want := []docker.LogLine{{Stream: 1, Text: "2027-03-01T10:00:00Z hello"}}; !slices.Equal(got, want) {
		t.Errorf("lines = %+v, want %+v", got, want)
	}
}

func TestLogDecoder_longLineIsCut(t *testing.T) {
	t.Parallel()
	got := decode(true, false, frame(1, strings.Repeat("x", 100_000)), frame(1, "y\n"))
	if len(got) != 2 || len(got[0].Text) != 100_000 || got[1].Text != "y" {
		var sizes []int
		for _, l := range got {
			sizes = append(sizes, len(l.Text))
		}
		t.Fatalf("line sizes = %v, want one of 100000 cut where the frame ended, then y", sizes)
	}
}

func TestStripANSI(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"\x1b[31mred\x1b[0m text":                   "red text",
		"\x1b[1;38;5;208mbold\x1b[m":                "bold",
		"\x1b]0;title\x07after":                     "after",
		"\x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\": "link",
		"no escapes":                                "no escapes",
	} {
		if got := docker.StripANSI(in); got != want {
			t.Errorf("StripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSince(t *testing.T) {
	t.Parallel()
	now := time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC)
	unix := func(d time.Duration) string { return strconv.FormatInt(now.Add(d).Unix(), 10) }
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"  ", ""},
		{"1800000000", "1800000000"},
		{"1800000000.5", "1800000000.5"},
		{"30s", unix(-30 * time.Second)},
		{"15m", unix(-15 * time.Minute)},
		{"6h", unix(-6 * time.Hour)},
		{"7d", unix(-7 * 24 * time.Hour)},
		{"2027-03-01T11:00:00Z", unix(-time.Hour) + ".000000000"},
		{"2027-03-01T11:59:59.123456789Z", unix(-time.Second) + ".123456789"},
		{"2027-03-01T13:00:00+01:00", unix(0) + ".000000000"},
		{"2027-03-01T11:59:59.5Z", unix(-time.Second) + ".500000000"},
	}
	for _, tt := range tests {
		got, err := docker.ParseSince(tt.in, now)
		if err != nil || got != tt.want {
			t.Errorf("ParseSince(%q) = %q, %v, want %q", tt.in, got, err, tt.want)
		}
	}
	// An age from before 1970 is all of the log, and doesn't wrap around.
	for _, in := range []string{"106752d", "999999999d", "999999999h"} {
		if got, err := docker.ParseSince(in, now); err != nil || got != "0" {
			t.Errorf("ParseSince(%q) = %q, %v, want 0", in, got, err)
		}
	}
	for _, in := range []string{"yesterday", "-5", "5x", "1e9", "15m; rm -rf", "1.5.2", "99999999999d"} {
		if got, err := docker.ParseSince(in, now); !errors.Is(err, docker.ErrBadSince) {
			t.Errorf("ParseSince(%q) = %q, %v, want ErrBadSince", in, got, err)
		}
	}
}

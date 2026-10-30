package files

import (
	"net/http"
	"testing"
	"time"
)

func TestAbs(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in, want string
		ok       bool
	}{
		{in: "/a/b", want: "/a/b", ok: true},
		{in: "/a/b/", want: "/a/b", ok: true},
		{in: "/a/b/../c", want: "/a/c", ok: true},
		{in: "/../..", want: "/", ok: true},
		{in: "/", want: "/", ok: true},
		{in: "//a///b", want: "/a/b", ok: true},
		{in: "~", want: "/home/me", ok: true},
		{in: "~/x", want: "/home/me/x", ok: true},
		{in: "~/../root", want: "/home/root", ok: true},
		{in: "/with space/and\nnewline /", want: "/with space/and\nnewline ", ok: true},
		{in: "~other"},
		{in: "relative"},
		{in: ""},
		{in: "/a\x00b"},
		{in: "/" + repeat("x", 4096)},
	} {
		got, err := absPath(tt.in, "/home/me")
		if tt.ok != (err == nil) || got != tt.want {
			t.Errorf("absPath(%q) = %q, %v; want %q, ok %v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

func TestSafeName(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"a", "a b", "-rf", ".hidden", "a\nb", `a\b`, "it's", "..a", "a..", repeat("é", 127)} {
		if _, err := SafeName(ok); err != nil {
			t.Errorf("SafeName(%q) = %v, want ok", ok, err)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", "/", "a\x00b", repeat("x", 256)} {
		if _, err := SafeName(bad); err == nil {
			t.Errorf("SafeName(%q) is ok, want an error", bad)
		}
	}
}

func TestSafeRelative(t *testing.T) {
	t.Parallel()
	got, err := safeRelative("photos//2024/a.jpg/")
	if err != nil || len(got) != 3 || got[0] != "photos" || got[2] != "a.jpg" {
		t.Errorf("safeRelative = %q, %v", got, err)
	}
	for _, bad := range []string{"", "/", "a/../b", "../a", "a/./b/..", repeat("a/", 65)} {
		if _, err := safeRelative(bad); err == nil {
			t.Errorf("safeRelative(%q) is ok, want an error", bad)
		}
	}
}

func TestSplitExt(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ in, stem, ext string }{
		{"report.xlsx", "report", ".xlsx"},
		{"report.final.xlsx", "report.final", ".xlsx"},
		{"backup.tar.gz", "backup", ".tar.gz"},
		{"BACKUP.TAR.GZ", "BACKUP", ".TAR.GZ"},
		{"Makefile", "Makefile", ""},
		{".bashrc", ".bashrc", ""},
		{".config.json", ".config.json", ""},
		{"trailing.", "trailing.", ""},
		{"a.verylongextension", "a.verylongextension", ""},
		{"a b.txt", "a b", ".txt"},
	} {
		if stem, ext := splitExt(tt.in); stem != tt.stem || ext != tt.ext {
			t.Errorf("splitExt(%q) = %q, %q; want %q, %q", tt.in, stem, ext, tt.stem, tt.ext)
		}
	}
}

func TestModeString(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		mode uint32
		want string
	}{
		{0o100644, "-rw-r--r--"},
		{0o040755, "drwxr-xr-x"},
		{0o120777, "lrwxrwxrwx"},
		{0o104755, "-rwsr-xr-x"},
		{0o104644, "-rwSr--r--"},
		{0o102755, "-rwxr-sr-x"},
		{0o041777, "drwxrwxrwt"},
		{0o041776, "drwxrwxrwT"},
		{0o020660, "crw-rw----"},
		{0o060660, "brw-rw----"},
		{0o010644, "prw-r--r--"},
		{0o140755, "srwxr-xr-x"},
		{0o100000, "----------"},
	} {
		if got := modeString(tt.mode); got != tt.want {
			t.Errorf("modeString(%#o) = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestParseRange(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in         string
		start, end int64
		ok, bad    bool
	}{
		{in: ""},
		{in: "bytes=0-9", start: 0, end: 9, ok: true},
		{in: "bytes=10-", start: 10, end: 99, ok: true},
		{in: "bytes=-10", start: 90, end: 99, ok: true},
		{in: "bytes=-500", start: 0, end: 99, ok: true},
		{in: "bytes=50-5000", start: 50, end: 99, ok: true},
		{in: "bytes=99-99", start: 99, end: 99, ok: true},
		{in: "bytes=100-", bad: true},
		{in: "bytes=20-10", bad: true},
		{in: "bytes=-0", bad: true},
		{in: "bytes=-", bad: true},
		{in: "bytes=99999999999999999999-", bad: true},
		// Several ranges, and other units, get the whole file.
		{in: "bytes=0-1,5-6"},
		{in: "items=0-1"},
		{in: "garbage"},
	} {
		start, end, ok, bad := parseRange(tt.in, 100)
		if ok != tt.ok || bad != tt.bad || ok && (start != tt.start || end != tt.end) {
			t.Errorf("parseRange(%q) = %d, %d, %v, %v; want %d, %d, %v, %v", tt.in, start, end, ok, bad, tt.start, tt.end, tt.ok, tt.bad)
		}
	}
}

func TestDisposition(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ kind, name, want string }{
		{"inline", "a.txt", `inline; filename="a.txt"; filename*=UTF-8''a.txt`},
		{"attachment", "my file.txt", `attachment; filename="my file.txt"; filename*=UTF-8''my%20file.txt`},
		{"attachment", `a"b;c\d%e`, `attachment; filename="a_b_c_d_e"; filename*=UTF-8''a%22b%3Bc%5Cd%25e`},
		{"attachment", "it's (1)*.txt", `attachment; filename="it's (1)*.txt"; filename*=UTF-8''it%27s%20%281%29%2A.txt`},
		{"attachment", "työ.txt", `attachment; filename="ty_.txt"; filename*=UTF-8''ty%C3%B6.txt`},
		{"attachment", "a\r\nSet-Cookie: x", `attachment; filename="a__Set-Cookie: x"; filename*=UTF-8''a%0D%0ASet-Cookie%3A%20x`},
	} {
		if got := disposition(tt.kind, tt.name); got != tt.want {
			t.Errorf("disposition(%q, %q) =\n%s\nwant\n%s", tt.kind, tt.name, got, tt.want)
		}
	}
}

func TestKindOf(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]Kind{
		"photo.JPG": KindImage, "clip.mp4": KindVideo, "song.flac": KindAudio, "doc.pdf": KindPDF,
		"main.go": KindText, "Dockerfile": KindText, ".bashrc": KindText, "README": KindText, "app.log.1": KindText,
		"nginx.conf.2": KindText, "notes.docx": KindDocx, "data.csv": KindSheet, "a.xlsx": KindSheet,
		"a.tar.gz": KindArchive, "program": KindOther, "": KindOther, ".": KindOther, "a.unknownext": KindOther,
	} {
		if got := KindOf(name); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// HTML and everything that runs script must never be shown inline.
func TestInlineType(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"a.png": "image/png", "a.svg": "image/svg+xml", "a.mp4": "video/mp4", "a.pdf": "application/pdf",
		"a.txt": "text/plain; charset=utf-8", "a.csv": "text/plain; charset=utf-8", "a.json": "text/plain; charset=utf-8",
		"a.html": "text/plain; charset=utf-8", "a.js": "text/plain; charset=utf-8",
		"a.docx": "", "a.zip": "", "a.exe": "", "noext": "",
	} {
		if got := InlineType(name); got != want {
			t.Errorf("InlineType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestArchives(t *testing.T) {
	t.Parallel()
	var a Archives
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	token := a.Park("laptop", "/home/me", []string{"a", "b"}, now)
	if dir, names, ok := a.Take(token, "laptop", now.Add(archiveTTL-time.Second)); !ok || dir != "/home/me" || len(names) != 2 {
		t.Errorf("Take = %q, %q, %v", dir, names, ok)
	}
	// It stays, so a download that failed can try again.
	if _, _, ok := a.Take(token, "laptop", now.Add(time.Minute)); !ok {
		t.Error("a taken selection is gone")
	}
	if _, _, ok := a.Take(token, "phone", now); ok {
		t.Error("another device took the selection")
	}
	if _, _, ok := a.Take("nope", "laptop", now); ok {
		t.Error("an unknown token worked")
	}
	if _, _, ok := a.Take(token, "laptop", now.Add(archiveTTL+time.Second)); ok {
		t.Error("an expired selection worked")
	}
	// Parking sweeps the ones that expired.
	a.Park("laptop", "/", []string{"x"}, now.Add(time.Hour))
	if len(a.parked) != 1 {
		t.Errorf("%d selections parked, want only the new one", len(a.parked))
	}
	if t1, t2 := a.Park("d", "/", nil, now), a.Park("d", "/", nil, now); t1 == t2 || len(t1) < 20 {
		t.Errorf("tokens %q and %q should be long and different", t1, t2)
	}
}

func TestErrors(t *testing.T) {
	t.Parallel()
	err := errorf(http.StatusNotFound, "%s doesn't exist", "/x")
	if err.Error() != "/x doesn't exist" || err.Status != 404 {
		t.Errorf("errorf = %+v", err)
	}
}

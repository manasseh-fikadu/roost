package files_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/files"
)

func TestRemote_Upload(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "")
	r := m.open(t)
	big := bytes.Repeat([]byte("0123456789abcdef"), 1<<16+7) // over 1 MiB, to cross packets
	for _, name := range append(slices.Clone(hostile), "big.bin") {
		body := []byte("body of " + name)
		if name == "big.bin" {
			body = big
		}
		up, err := r.Upload(bytes.NewReader(body), m.home, name, files.ConflictFail)
		if err != nil || up.Name != name || up.Size != int64(len(body)) || up.Path != filepath.Join(m.home, name) {
			t.Errorf("Upload(%q) = %+v, %v", name, up, err)
			continue
		}
		if got, err := os.ReadFile(up.Path); err != nil || !bytes.Equal(got, body) {
			t.Errorf("%q has %d bytes, %v; want %d", name, len(got), err, len(body))
		}
	}
	// No half-written file stays.
	entries, _ := os.ReadDir(m.home)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("%q was left behind", e.Name())
		}
	}
}

func TestRemote_Upload_folder(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "")
	r := m.open(t)
	up, err := r.Upload(strings.NewReader("x"), m.home, "photos/2024/trip/a.jpg", files.ConflictFail)
	if err != nil || up.Name != "a.jpg" || up.Path != filepath.Join(m.home, "photos/2024/trip/a.jpg") {
		t.Fatalf("Upload = %+v, %v", up, err)
	}
	// Another file into the same new folders.
	if _, err := r.Upload(strings.NewReader("y"), m.home, "photos/2024/trip/b.jpg", files.ConflictFail); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../escape", "a/../../escape", "/abs/../..", "a//..", ""} {
		if _, err := r.Upload(strings.NewReader("x"), m.home, bad, files.ConflictFail); status(err) != 400 {
			t.Errorf("Upload(%q) = %v, want 400", bad, err)
		}
	}
	// A file where a folder should be.
	m.write(t, "blocker", "")
	if _, err := r.Upload(strings.NewReader("x"), m.home, "blocker/x", files.ConflictFail); status(err) != 409 {
		t.Errorf("a file in the way = %v, want 409", err)
	}
	if _, err := r.Upload(strings.NewReader("x"), filepath.Join(m.home, "nope"), "x", files.ConflictFail); status(err) != 404 {
		t.Errorf("a missing folder = %v, want 404", err)
	}
	if _, err := r.Upload(strings.NewReader("x"), filepath.Join(m.home, "blocker"), "x", files.ConflictFail); status(err) != 400 {
		t.Errorf("into a file = %v, want 400", err)
	}
}

func TestRemote_Upload_conflicts(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "")
	r := m.open(t)
	p := m.write(t, "a.tar.gz", "old")
	//nolint:gosec // G302: the test checks that the mode stays.
	if err := os.Chmod(p, 0o640); err != nil {
		t.Fatal(err)
	}
	read := func(rel string) string {
		b, _ := os.ReadFile(filepath.Clean(filepath.Join(m.home, rel)))
		return string(b)
	}
	if _, err := r.Upload(strings.NewReader("new"), m.home, "a.tar.gz", files.ConflictFail); status(err) != 409 || read("a.tar.gz") != "old" {
		t.Errorf("fail on an existing file: %v, content %q; want 409 and the old file", err, read("a.tar.gz"))
	}
	up, err := r.Upload(strings.NewReader("renamed"), m.home, "a.tar.gz", files.ConflictRename)
	if err != nil || up.Name != "a (1).tar.gz" || read("a (1).tar.gz") != "renamed" || read("a.tar.gz") != "old" {
		t.Errorf("rename = %+v, %v", up, err)
	}
	up, err = r.Upload(strings.NewReader("newer"), m.home, "a.tar.gz", files.ConflictOverwrite)
	if err != nil || up.Name != "a.tar.gz" || read("a.tar.gz") != "newer" {
		t.Errorf("overwrite = %+v, %v", up, err)
	}
	// An overwrite keeps the file's permissions.
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("mode after overwrite = %v, %v, want 0640", fi.Mode().Perm(), err)
	}
	// A folder is never replaced.
	if err := os.Mkdir(filepath.Join(m.home, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Upload(strings.NewReader("x"), m.home, "folder", files.ConflictOverwrite); status(err) != 409 {
		t.Errorf("overwriting a folder = %v, want 409", err)
	}
	// Overwriting through a link replaces the file it points to.
	m.write(t, "real", "real old")
	if err := os.Symlink("real", filepath.Join(m.home, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Upload(strings.NewReader("through"), m.home, "link", files.ConflictOverwrite); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(filepath.Join(m.home, "link")); err != nil || fi.Mode()&os.ModeSymlink == 0 || read("real") != "through" {
		t.Errorf("link = %v (%v), real = %q; want the link kept and its file changed", fi.Mode(), err, read("real"))
	}
}

// An upload that stops halfway leaves nothing, and the file it would have
// replaced is untouched.
func TestRemote_Upload_canceled(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "")
	r := m.open(t)
	m.write(t, "keep", "the good file")
	body := io.MultiReader(bytes.NewReader(bytes.Repeat([]byte("x"), 300_000)), iotest.ErrReader(errors.New("connection reset by peer")))
	_, err := r.Upload(body, m.home, "keep", files.ConflictOverwrite)
	if status(err) != files.StatusClientClosed {
		t.Errorf("a body that fails = %v, want status %d", err, files.StatusClientClosed)
	}
	if b, _ := os.ReadFile(filepath.Join(m.home, "keep")); string(b) != "the good file" {
		t.Errorf("keep = %q, want the old content", b)
	}
	entries, _ := os.ReadDir(m.home)
	if len(entries) != 1 {
		t.Errorf("%d files in the folder, want only keep", len(entries))
	}
	// Too big is not the same as canceled.
	_, err = r.Upload(http.MaxBytesReader(httptest.NewRecorder(), io.NopCloser(bytes.NewReader(make([]byte, 100))), 10), m.home, "big", files.ConflictFail)
	if status(err) != http.StatusRequestEntityTooLarge {
		t.Errorf("a body over the limit = %v, want 413", err)
	}
}

func TestRemote_WriteText(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "")
	r := m.open(t)
	p := m.write(t, "conf.yml", "a: 1\n")
	fi, _ := os.Stat(p)

	// SFTP knows whole seconds.
	e, err := r.WriteText(strings.NewReader("a: 2\n"), 5, p, false, fi.ModTime().Truncate(time.Second))
	if err != nil || e.Size != 5 {
		t.Fatalf("WriteText = %+v, %v", e, err)
	}
	if b, _ := os.ReadFile(filepath.Clean(p)); string(b) != "a: 2\n" {
		t.Errorf("file = %q", b)
	}
	// The file changed since the editor loaded it.
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if _, err := r.WriteText(strings.NewReader("clobber"), 7, p, false, old); status(err) != 409 {
		t.Errorf("a stale save = %v, want 409", err)
	}
	if b, _ := os.ReadFile(filepath.Clean(p)); string(b) != "a: 2\n" {
		t.Errorf("a stale save changed the file to %q", b)
	}
	// No time to check: save anyway.
	if _, err := r.WriteText(strings.NewReader("a: 3\n"), 5, p, false, time.Time{}); err != nil {
		t.Errorf("a save with no time to check: %v", err)
	}
	// Creating.
	newFile := filepath.Join(m.home, "new file.txt")
	if _, err := r.WriteText(strings.NewReader("hi"), 2, newFile, true, time.Time{}); err != nil {
		t.Errorf("create: %v", err)
	}
	if _, err := r.WriteText(strings.NewReader("hi"), 2, newFile, true, time.Time{}); status(err) != 409 {
		t.Errorf("creating what's there = %v, want 409", err)
	}
	if _, err := r.WriteText(strings.NewReader("hi"), 2, filepath.Join(m.home, "gone"), false, time.Time{}); status(err) != 404 {
		t.Errorf("saving what's gone = %v, want 404", err)
	}
	if _, err := r.WriteText(strings.NewReader("hi"), 2, m.home, false, time.Time{}); status(err) != 400 {
		t.Errorf("saving a folder = %v, want 400", err)
	}
	if _, err := r.WriteText(strings.NewReader("hi"), files.MaxText+1, p, false, time.Time{}); status(err) != 413 {
		t.Errorf("a big file = %v, want 413", err)
	}
	// With no length, it still stops.
	unknown := io.MultiReader(bytes.NewReader(make([]byte, files.MaxText)), bytes.NewReader([]byte("x")))
	if _, err := r.WriteText(unknown, -1, p, false, time.Time{}); status(err) != 413 {
		t.Errorf("a body over the limit with no length = %v, want 413", err)
	}
}

// A file in a folder the user can't write to is saved in place.
func TestRemote_WriteText_readOnlyFolder(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skip("root writes anywhere")
	}
	m := newMachine(t, "")
	r := m.open(t)
	p := m.write(t, "etc/app.conf", "old")
	//nolint:gosec // G302: a folder has to be searchable.
	if err := os.Chmod(filepath.Dir(p), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(p), 0o700) }) //nolint:gosec // G302: see above.
	if _, err := r.WriteText(strings.NewReader("new"), 3, p, false, time.Time{}); err != nil {
		t.Fatalf("WriteText in a read-only folder: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Clean(p)); string(b) != "new" {
		t.Errorf("file = %q, want new", b)
	}
}

func serve(t *testing.T, r *files.Remote, method, p string, header map[string]string, download bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, "/raw", nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	if err := r.ServeRaw(w, req, p, download); err != nil {
		t.Fatalf("ServeRaw(%s %s): %v", method, p, err)
	}
	return w
}

func TestRemote_ServeRaw(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "")
	r := m.open(t)
	content := bytes.Repeat([]byte("abcdefghij"), 100_000) // 1 MB, over many packets
	p := m.write(t, "data.bin", string(content))
	txt := m.write(t, "notes.txt", strings.Repeat("hello text\n", 500))
	html := m.write(t, "page.html", "<script>alert(1)</script>")
	pdf := m.write(t, "doc.pdf", "%PDF-1.4")

	w := serve(t, r, "GET", p, nil, false)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), content) {
		t.Fatalf("whole file: %d, %d bytes, want 200 and %d", w.Code, w.Body.Len(), len(content))
	}
	h := w.Header()
	if h.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(h.Get("Content-Disposition"), "attachment;") {
		t.Errorf("an unknown type is a download: %q, %q", h.Get("Content-Type"), h.Get("Content-Disposition"))
	}
	if h.Get("Content-Length") != strconv.Itoa(len(content)) || h.Get("Accept-Ranges") != "bytes" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers = %v", h)
	}
	if !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") || h.Get("ETag") == "" || h.Get("X-File-Mtime") == "" {
		t.Errorf("security and cache headers = %v", h)
	}

	// Ranges.
	for _, tt := range []struct {
		rng        string
		start, end int
	}{
		{"bytes=0-9", 0, 10},
		{"bytes=100000-100099", 100000, 100100},
		{"bytes=999990-", 999990, 1_000_000},
		{"bytes=-5", 999995, 1_000_000},
		{"bytes=70000-200000", 70000, 200001},
	} {
		w := serve(t, r, "GET", p, map[string]string{"Range": tt.rng}, false)
		if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), content[tt.start:tt.end]) ||
			w.Header().Get("Content-Range") != "bytes "+strconv.Itoa(tt.start)+"-"+strconv.Itoa(tt.end-1)+"/1000000" {
			t.Errorf("%s: %d, %d bytes, %q", tt.rng, w.Code, w.Body.Len(), w.Header().Get("Content-Range"))
		}
	}
	if w := serve(t, r, "GET", p, map[string]string{"Range": "bytes=2000000-"}, false); w.Code != 416 || w.Header().Get("Content-Range") != "bytes */1000000" {
		t.Errorf("a range past the end: %d, %q, want 416", w.Code, w.Header().Get("Content-Range"))
	}
	// A client that holds another copy gets the whole file.
	if w := serve(t, r, "GET", p, map[string]string{"Range": "bytes=0-9", "If-Range": `"stale"`}, false); w.Code != 200 || w.Body.Len() != len(content) {
		t.Errorf("If-Range with an old copy: %d, %d bytes, want the whole file", w.Code, w.Body.Len())
	}
	if w := serve(t, r, "GET", p, map[string]string{"Range": "bytes=0-9", "If-Range": h.Get("ETag")}, false); w.Code != 206 {
		t.Errorf("If-Range with the same copy: %d, want 206", w.Code)
	}
	// Not modified, and HEAD.
	if w := serve(t, r, "GET", p, map[string]string{"If-None-Match": h.Get("ETag")}, false); w.Code != 304 || w.Body.Len() != 0 {
		t.Errorf("If-None-Match: %d with %d bytes, want 304", w.Code, w.Body.Len())
	}
	if w := serve(t, r, "HEAD", p, nil, false); w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "1000000" {
		t.Errorf("HEAD: %d, %d bytes, length %q", w.Code, w.Body.Len(), w.Header().Get("Content-Length"))
	}

	// Text is shown as text, and compressed whole when the browser takes it.
	w = serve(t, r, "GET", txt, map[string]string{"Accept-Encoding": "gzip, br"}, false)
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" || w.Header().Get("Content-Encoding") != "gzip" ||
		!strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline;") || w.Header().Get("Content-Length") != "" {
		t.Errorf("gzipped text headers = %v", w.Header())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(zr); string(b) != strings.Repeat("hello text\n", 500) {
		t.Errorf("gzipped text = %d bytes", len(b))
	}
	if w = serve(t, r, "GET", txt, nil, false); w.Header().Get("Content-Encoding") != "" || w.Body.Len() != 5500 {
		t.Errorf("text with no gzip: %q, %d bytes", w.Header().Get("Content-Encoding"), w.Body.Len())
	}
	if w = serve(t, r, "GET", txt, map[string]string{"Accept-Encoding": "gzip", "Range": "bytes=0-4"}, false); w.Header().Get("Content-Encoding") != "" || w.Body.String() != "hello" {
		t.Errorf("a range of text: %q, %q", w.Header().Get("Content-Encoding"), w.Body)
	}
	// ?download=1 saves it instead.
	if w = serve(t, r, "GET", txt, nil, true); !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") || w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Errorf("a forced download: %v", w.Header())
	}

	// HTML is text, never a page; a PDF is shown without the sandbox.
	w = serve(t, r, "GET", html, nil, false)
	if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("HTML headers = %v", w.Header())
	}
	w = serve(t, r, "GET", pdf, nil, false)
	if w.Header().Get("Content-Type") != "application/pdf" || w.Header().Get("Content-Security-Policy") != "" {
		t.Errorf("PDF headers = %v", w.Header())
	}
	if w = serve(t, r, "GET", pdf, nil, true); w.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("a PDF download has no sandbox")
	}

	// An empty file.
	empty := m.write(t, "empty.txt", "")
	if w = serve(t, r, "GET", empty, nil, false); w.Code != 200 || w.Body.Len() != 0 {
		t.Errorf("empty file: %d, %d bytes", w.Code, w.Body.Len())
	}
}

func TestRemote_ServeRaw_names(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "")
	r := m.open(t)
	for _, name := range hostile {
		p := m.write(t, name, "x")
		w := serve(t, r, "GET", p, nil, true)
		cd := w.Header().Get("Content-Disposition")
		// A name can't end a header or add one.
		if strings.ContainsAny(cd, "\r\n") || strings.Count(cd, `"`) != 2 {
			t.Errorf("%q: Content-Disposition = %q", name, cd)
		}
		if len(w.Header()) > 14 {
			t.Errorf("%q: %d headers, a name added one: %v", name, len(w.Header()), w.Header())
		}
	}
}

func TestRemote_ServeRaw_errors(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "")
	r := m.open(t)
	for _, tt := range []struct {
		name, path string
		status     int
	}{
		{"a folder", m.home, 400},
		{"nothing", filepath.Join(m.home, "nope"), 404},
	} {
		w := httptest.NewRecorder()
		err := r.ServeRaw(w, httptest.NewRequestWithContext(t.Context(), "GET", "/raw", nil), tt.path, false)
		if status(err) != tt.status {
			t.Errorf("%s: ServeRaw = %v, want status %d", tt.name, err, tt.status)
		}
		if w.Body.Len() != 0 {
			t.Errorf("%s: wrote %d bytes before failing", tt.name, w.Body.Len())
		}
	}
}

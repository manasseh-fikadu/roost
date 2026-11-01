package api_test

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"unicode"

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/ssh/sshtest"
)

// filesEnv is an sshEnv whose laptop serves a folder of the test machine over
// SFTP and runs commands in sh, with Roost's access to it set up.
type filesEnv struct {
	*sshEnv
	home string
}

func newFilesEnv(t *testing.T) *filesEnv {
	t.Helper()
	e := newSSHEnv(t, testStore(t), nil)
	return setUpFiles(t, e)
}

func setUpFiles(t *testing.T, e *sshEnv) *filesEnv {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.sshd.ServeSFTP(home)
	e.sshd.Handle(sshtest.Shell("sh"))
	e.setUp(t)
	return &filesEnv{sshEnv: e, home: home}
}

func (e *filesEnv) write(t *testing.T, rel, content string) string {
	t.Helper()
	p := filepath.Join(e.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// post sends a JSON body as alice, and decodes the answer into v if it is
// 200.
func (e *filesEnv) post(t *testing.T, target, body string, v any) int {
	t.Helper()
	rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/files/"+target, laptopIP, body)
	if rec.Code == http.StatusOK && v != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code
}

func (e *filesEnv) messages(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, ev := range e.events(t, api.EventKindFiles) {
		if ev.Actor != alice || ev.DeviceID != "laptop" {
			t.Errorf("event %+v, want one by alice about the laptop", ev)
		}
		out = append(out, ev.Message)
	}
	return out
}

func TestFiles_listAndStat(t *testing.T) {
	t.Parallel()
	e := newFilesEnv(t)
	e.write(t, "notes.txt", "hello")
	e.write(t, "docs/a.md", "# a")

	var l api.FileListing
	get(t, e.h, "/api/devices/laptop/files/list", laptopIP, &l)
	if l.Path != e.home || l.Home != e.home || l.Parent != filepath.Dir(e.home) || len(l.Entries) != 2 {
		t.Errorf("list = %+v, want the home folder with 2 entries", l)
	}
	var sub api.FileListing
	get(t, e.h, "/api/devices/laptop/files/list?path="+url.QueryEscape(filepath.Join(e.home, "docs")), laptopIP, &sub)
	if len(sub.Entries) != 1 || sub.Entries[0].Name != "a.md" || sub.Entries[0].Type != api.FileTypeFile || sub.Entries[0].Size != 3 {
		t.Errorf("list of docs = %+v", sub.Entries)
	}
	var st api.FileStat
	get(t, e.h, "/api/devices/laptop/files/stat?path="+url.QueryEscape("~/notes.txt"), laptopIP, &st)
	if st.Entry.Name != "notes.txt" || st.Entry.Size != 5 || st.Home != e.home {
		t.Errorf("stat = %+v", st)
	}
	for _, tt := range []struct {
		target string
		want   int
	}{
		{"list?path=" + url.QueryEscape(filepath.Join(e.home, "nope")), http.StatusNotFound},
		{"list?path=" + url.QueryEscape(filepath.Join(e.home, "notes.txt")), http.StatusBadRequest},
		{"list?path=relative", http.StatusBadRequest},
		{"stat", http.StatusBadRequest},
		{"raw?path=" + url.QueryEscape(e.home), http.StatusBadRequest},
	} {
		if rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/files/"+tt.target, laptopIP)); rec.Code != tt.want {
			t.Errorf("GET %s = %d %s, want %d", tt.target, rec.Code, rec.Body, tt.want)
		}
	}
}

// Files are for admins: a viewer can't see them, or what happens to them.
func TestFiles_admins(t *testing.T) {
	t.Parallel()
	e := newFilesEnv(t)
	e.write(t, "a.txt", "x")
	for _, target := range []string{"list", "stat?path=/", "raw?path=" + url.QueryEscape(filepath.Join(e.home, "a.txt")), "archive?dir=/&name=x", "search?q=a"} {
		if rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/files/"+target, phoneIP)); rec.Code != http.StatusForbidden {
			t.Errorf("a viewer's GET %s = %d, want 403", target, rec.Code)
		}
	}
	for _, target := range []string{"mkdir", "rename", "move", "copy", "delete", "exists", "archive"} {
		if rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/files/"+target, phoneIP, `{}`); rec.Code != http.StatusForbidden {
			t.Errorf("a viewer's POST %s = %d, want 403", target, rec.Code)
		}
	}
	for _, target := range []string{"upload?dir=/&name=x", "write?path=/x"} {
		if rec := send(t, e.h, http.MethodPut, "/api/devices/laptop/files/"+target, phoneIP, `x`); rec.Code != http.StatusForbidden {
			t.Errorf("a viewer's PUT %s = %d, want 403", target, rec.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(e.home, "a.txt")); err != nil {
		t.Error(err)
	}
	// A request that changes things needs the CSRF header.
	req := newRequestBody(t, http.MethodPost, "/api/devices/laptop/files/delete", laptopIP, strings.NewReader(`{"paths":["`+filepath.Join(e.home, "a.txt")+`"]}`))
	req.Header.Del(api.CSRFHeader)
	if rec := serve(e.h, req); rec.Code != http.StatusForbidden {
		t.Errorf("a delete with no CSRF header = %d, want 403", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(e.home, "a.txt")); err != nil {
		t.Errorf("a delete with no CSRF header deleted the file: %v", err)
	}
	// Viewers don't see what admins did to files in the activity log.
	e.post(t, "mkdir", `{"dir":"`+e.home+`","name":"new"}`, nil)
	var events api.EventList
	get(t, e.h, "/api/events", phoneIP, &events)
	for _, ev := range events.Events {
		if ev.Kind == api.EventKindFiles {
			t.Errorf("a viewer sees the event %+v", ev)
		}
	}
}

func TestFiles_noAccess(t *testing.T) {
	t.Parallel()
	e := newSSHEnv(t, testStore(t), nil)
	// Not set up.
	if rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/files/list", laptopIP)); rec.Code != http.StatusConflict {
		t.Errorf("list before SSH is set up = %d %s, want 409", rec.Code, rec.Body)
	}
	if rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/nope/files/list", laptopIP)); rec.Code != http.StatusNotFound {
		t.Errorf("list of an unknown device = %d, want 404", rec.Code)
	}
	// Set up, but sshd has no SFTP.
	e.setUp(t)
	rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/files/list", laptopIP))
	var body api.ErrorBody
	if rec.Code != http.StatusConflict || json.Unmarshal(rec.Body.Bytes(), &body) != nil || !strings.Contains(body.Error, "SFTP") {
		t.Errorf("list without SFTP = %d %s, want 409 that says SFTP", rec.Code, rec.Body)
	}
}

func TestFiles_changes(t *testing.T) {
	t.Parallel()
	e := newFilesEnv(t)
	dest := filepath.Join(e.home, "dest")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	a := e.write(t, "a.txt", "a")
	b := e.write(t, "b.txt", "b")

	var entry api.FileEntry
	if code := e.post(t, "mkdir", `{"dir":"`+e.home+`","name":"new folder"}`, &entry); code != 200 || entry.Type != api.FileTypeDir || entry.Path != filepath.Join(e.home, "new folder") {
		t.Fatalf("mkdir = %d %+v", code, entry)
	}
	if code := e.post(t, "mkdir", `{"dir":"`+e.home+`","name":"new folder"}`, nil); code != http.StatusConflict {
		t.Errorf("mkdir again = %d, want 409", code)
	}
	if code := e.post(t, "rename", `{"path":"`+a+`","name":"c.txt"}`, &entry); code != 200 || entry.Name != "c.txt" {
		t.Fatalf("rename = %d %+v", code, entry)
	}
	c := filepath.Join(e.home, "c.txt")
	var bulk api.FileBulk
	if code := e.post(t, "copy", `{"paths":["`+b+`","`+c+`"],"dest":"`+dest+`"}`, &bulk); code != 200 || bulk.Done != 2 || len(bulk.Failed) != 0 {
		t.Fatalf("copy = %d %+v", code, bulk)
	}
	if code := e.post(t, "move", `{"paths":["`+b+`"],"dest":"`+dest+`","conflict":"rename"}`, &bulk); code != 200 || bulk.Done != 1 || bulk.Paths[0] != filepath.Join(dest, "b (1).txt") {
		t.Fatalf("move = %d %+v", code, bulk)
	}
	if code := e.post(t, "move", `{"paths":["`+c+`"],"dest":"`+dest+`"}`, nil); code != http.StatusConflict {
		t.Errorf("move onto a file = %d, want 409", code)
	}
	if code := e.post(t, "delete", `{"paths":["`+filepath.Join(dest, "b.txt")+`","`+filepath.Join(dest, "gone")+`"]}`, &bulk); code != 200 || bulk.Done != 1 || len(bulk.Failed) != 1 {
		t.Fatalf("delete = %d %+v", code, bulk)
	}
	if code := e.post(t, "delete", `{"paths":["/etc"]}`, nil); code != http.StatusBadRequest {
		t.Errorf("delete /etc = %d, want 400", code)
	}
	if code := e.post(t, "delete", `{"paths":[]}`, nil); code != http.StatusBadRequest {
		t.Errorf("delete nothing = %d, want 400", code)
	}
	if code := e.post(t, "delete", `{"paths":["x"],"surprise":1}`, nil); code != http.StatusBadRequest {
		t.Errorf("delete with an unknown field = %d, want 400", code)
	}
	var ex api.ExistingNames
	if code := e.post(t, "exists", `{"dir":"`+dest+`","names":["b (1).txt","nope","c.txt"]}`, &ex); code != 200 || strings.Join(ex.Existing, "|") != "b (1).txt|c.txt" {
		t.Errorf("exists = %d %+v", code, ex)
	}

	want := []string{
		"Deleted " + filepath.Join(dest, "b.txt") + " on laptop", // not asked to be about 2 of 2: the other one failed
		"Moved " + b + " to " + dest + " on laptop",
		"Copied 2 items in " + e.home + " to " + dest + " on laptop",
		"Renamed " + a + " to c.txt on laptop",
		"Created folder " + filepath.Join(e.home, "new folder") + " on laptop",
	}
	got := e.messages(t)
	// The delete had two paths and one failed.
	want[0] = "Deleted 1 of 2 items in " + dest + " on laptop"
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("activity =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestFiles_upload(t *testing.T) {
	t.Parallel()
	e := newFilesEnv(t)
	target := "/api/devices/laptop/files/upload?dir=" + url.QueryEscape(e.home) + "&name="
	rec := send(t, e.h, http.MethodPut, target+url.QueryEscape("photos/trip/a b.txt"), laptopIP, "hello")
	var up api.FileUpload
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &up) != nil || up.Size != 5 || up.Name != "a b.txt" {
		t.Fatalf("upload = %d %s", rec.Code, rec.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(e.home, "photos/trip/a b.txt")); string(b) != "hello" {
		t.Errorf("uploaded file = %q", b)
	}
	if rec := send(t, e.h, http.MethodPut, target+url.QueryEscape("photos/trip/a b.txt"), laptopIP, "again"); rec.Code != http.StatusConflict {
		t.Errorf("upload onto a file = %d, want 409", rec.Code)
	}
	rec = send(t, e.h, http.MethodPut, target+url.QueryEscape("photos/trip/a b.txt")+"&conflict=rename", laptopIP, "again")
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &up) != nil || up.Name != "a b (1).txt" {
		t.Errorf("upload with rename = %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, e.h, http.MethodPut, target+url.QueryEscape("../escape"), laptopIP, "x"); rec.Code != http.StatusBadRequest {
		t.Errorf("upload that escapes = %d, want 400", rec.Code)
	}
}

// An upload over the limit is refused, from its Content-Length before any of it
// is read, or else as it streams in, and leaves nothing behind.
func TestFiles_uploadLimit(t *testing.T) {
	t.Parallel()
	e := newFilesEnv(t)
	opts := e.opts
	opts.MaxUpload = 10
	h := api.New(opts)
	target := "/api/devices/laptop/files/upload?dir=" + url.QueryEscape(e.home) + "&name="

	if rec := send(t, h, http.MethodPut, target+"ok.bin", laptopIP, "0123456789"); rec.Code != http.StatusOK {
		t.Fatalf("a file of exactly the limit = %d %s, want 200", rec.Code, rec.Body)
	}

	t.Run("by its length", func(t *testing.T) {
		req := newRequestBody(t, http.MethodPut, target+"claimed.bin", laptopIP, strings.NewReader("x"))
		req.ContentLength = 11
		rec := serve(h, req)
		if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "up to 10 B") {
			t.Errorf("a file that says it's 11 bytes = %d %s, want 413", rec.Code, rec.Body)
		}
	})

	t.Run("as it comes", func(t *testing.T) {
		req := newRequestBody(t, http.MethodPut, target+"streamed.bin", laptopIP, io.NopCloser(strings.NewReader("0123456789ABCDEF")))
		req.ContentLength = -1
		rec := serve(h, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("a body of 16 bytes of unknown length = %d %s, want 413", rec.Code, rec.Body)
		}
	})

	for _, name := range []string{"claimed.bin", "streamed.bin"} {
		if _, err := os.Stat(filepath.Join(e.home, name)); err == nil {
			t.Errorf("%s was left behind", name)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(e.home, ".*.part")); len(left) != 0 {
		t.Errorf("partial files left: %v", left)
	}
}

// Uploading a folder is a request for every file in it, and one line in the
// activity log.
func TestFiles_uploadAudit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := testStore(t)
		synctest.Wait()
		e := setUpFiles(t, newSSHEnv(t, st, nil))
		target := "/api/devices/laptop/files/upload?dir=" + url.QueryEscape(e.home) + "&name="
		for _, name := range []string{"a.txt", "sub/b.txt", "sub/c.txt"} {
			if rec := send(t, e.h, http.MethodPut, target+url.QueryEscape(name), laptopIP, "12345"); rec.Code != 200 {
				t.Fatalf("upload %s = %d %s", name, rec.Code, rec.Body)
			}
			time.Sleep(2 * time.Second)
		}
		if got := e.messages(t); len(got) != 0 {
			t.Errorf("logged %q while the upload was going", got)
		}
		time.Sleep(10 * time.Second)
		synctest.Wait()
		want := "Uploaded 3 files (15 B) to " + e.home + " on laptop"
		if got := e.messages(t); len(got) != 1 || got[0] != want {
			t.Errorf("activity = %q, want %q", got, want)
		}
		// One file is named; another folder is its own line.
		other := "/api/devices/laptop/files/upload?dir=" + url.QueryEscape(filepath.Join(e.home, "sub")) + "&name=d.txt"
		if rec := send(t, e.h, http.MethodPut, other, laptopIP, strings.Repeat("x", 2048)); rec.Code != 200 {
			t.Fatal(rec.Body)
		}
		time.Sleep(10 * time.Second)
		synctest.Wait()
		if got := e.messages(t); len(got) != 2 || got[0] != "Uploaded d.txt (2.0 KB) to "+filepath.Join(e.home, "sub")+" on laptop" {
			t.Errorf("activity = %q", got)
		}
	})
}

// Roost shutting down writes the line of an upload that is still waiting.
func TestFiles_uploadAuditOnShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := testStore(t)
		synctest.Wait()
		stopping := make(chan struct{})
		e := setUpFiles(t, newSSHEnv(t, st, stopping))
		up := "/api/devices/laptop/files/upload?dir=" + url.QueryEscape(e.home) + "&name=a.txt"
		if rec := send(t, e.h, http.MethodPut, up, laptopIP, "x"); rec.Code != 200 {
			t.Fatal(rec.Body)
		}
		close(stopping)
		synctest.Wait()
		if got := e.messages(t); len(got) != 1 {
			t.Errorf("activity = %q, want the upload's line", got)
		}
	})
}

func TestFiles_write(t *testing.T) {
	t.Parallel()
	e := newFilesEnv(t)
	p := e.write(t, "conf.yml", "a: 1\n")
	var entry api.FileEntry
	target := "/api/devices/laptop/files/write?path=" + url.QueryEscape(p)
	for range 3 {
		rec := send(t, e.h, http.MethodPut, target, laptopIP, "a: 2\n")
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &entry) != nil || entry.Size != 5 {
			t.Fatalf("write = %d %s", rec.Code, rec.Body)
		}
	}
	// Saving the same file again and again is one edit in the log.
	if got := e.messages(t); len(got) != 1 || got[0] != "Edited "+p+" on laptop" {
		t.Errorf("activity = %q, want one edit", got)
	}
	stale := target + "&expectedMtime=" + "1000"
	if rec := send(t, e.h, http.MethodPut, stale, laptopIP, "clobber"); rec.Code != http.StatusConflict {
		t.Errorf("write over a changed file = %d, want 409", rec.Code)
	}
	fresh := target + "&expectedMtime=" + strconv.FormatInt(entry.ModifiedAt.UnixMilli(), 10)
	if rec := send(t, e.h, http.MethodPut, fresh, laptopIP, "a: 3\n"); rec.Code != 200 {
		t.Errorf("write over the file as it was = %d %s, want 200", rec.Code, rec.Body)
	}
	created := "/api/devices/laptop/files/write?create=1&path=" + url.QueryEscape(filepath.Join(e.home, "new.txt"))
	if rec := send(t, e.h, http.MethodPut, created, laptopIP, "hi"); rec.Code != 200 {
		t.Errorf("create = %d %s", rec.Code, rec.Body)
	}
	if rec := send(t, e.h, http.MethodPut, created, laptopIP, "hi"); rec.Code != http.StatusConflict {
		t.Errorf("create again = %d, want 409", rec.Code)
	}
	if rec := send(t, e.h, http.MethodPut, "/api/devices/laptop/files/write?path="+url.QueryEscape(filepath.Join(e.home, "gone")), laptopIP, "x"); rec.Code != http.StatusNotFound {
		t.Errorf("write to a missing file = %d, want 404", rec.Code)
	}
	big := strings.Repeat("x", 10<<20+1)
	if rec := send(t, e.h, http.MethodPut, target, laptopIP, big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("write of 10 MB and a byte = %d, want 413", rec.Code)
	}
}

// A file's content leaves the machine through the raw route, so the log says
// who read it: once for a file that is read in pieces, and separately for a
// download. A HEAD request and a refusal read nothing.
func TestFiles_rawIsLogged(t *testing.T) {
	t.Parallel()
	e := newFilesEnv(t)
	secret := e.write(t, "secrets.env", "TOKEN=abc\n")
	target := "/api/devices/laptop/files/raw?path=" + url.QueryEscape(secret)

	// A viewer can't read it, and it isn't logged.
	if rec := serve(e.h, newRequest(t, http.MethodGet, target, phoneIP)); rec.Code != http.StatusForbidden {
		t.Fatalf("a viewer's GET = %d, want 403", rec.Code)
	}
	if rec := serve(e.h, newRequest(t, http.MethodHead, target, laptopIP)); rec.Code != http.StatusOK {
		t.Fatalf("HEAD = %d", rec.Code)
	}
	if got := e.messages(t); len(got) != 0 {
		t.Fatalf("activity = %q, want nothing for a refusal and a HEAD", got)
	}
	if rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/files/raw?path="+url.QueryEscape(filepath.Join(e.home, "gone")), laptopIP)); rec.Code != http.StatusNotFound {
		t.Fatalf("GET of a missing file = %d, want 404", rec.Code)
	}

	for _, rng := range []string{"", "bytes=0-3", "bytes=4-", ""} {
		req := newRequest(t, http.MethodGet, target, laptopIP)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		if rec := serve(e.h, req); rec.Code != http.StatusOK && rec.Code != http.StatusPartialContent {
			t.Fatalf("GET %q = %d", rng, rec.Code)
		}
	}
	if rec := serve(e.h, newRequest(t, http.MethodGet, target+"&download=1", laptopIP)); rec.Code != http.StatusOK {
		t.Fatalf("download = %d", rec.Code)
	}
	got := e.messages(t)
	want := []string{"Downloaded " + secret + " on laptop", "Opened " + secret + " on laptop"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("activity =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A file can be called anything, and what the log says about it is what an
// admin reads in the Activity page: a name with a line break in it must not
// look like a line of the log, or turn the text around.
func TestFiles_hostileNamesInTheLog(t *testing.T) {
	t.Parallel()
	e := newFilesEnv(t)
	deleted := []string{"x\nDeleted /etc on prod", "y\r\nMoved /home to /tmp"}
	var paths []string
	for _, n := range deleted {
		paths = append(paths, e.write(t, n, "x"))
	}
	body, err := json.Marshal(api.DeleteFiles{Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	if code := e.post(t, "delete", string(body), nil); code != 200 {
		t.Fatalf("delete = %d", code)
	}
	for _, n := range []string{"\x1b[2Jscreen", "fake\u202etxt.exe", "tab\there"} {
		b, err := json.Marshal(api.MakeFolder{Dir: e.home, Name: n})
		if err != nil {
			t.Fatal(err)
		}
		if code := e.post(t, "mkdir", string(b), nil); code != 200 {
			t.Fatalf("mkdir %q = %d", n, code)
		}
	}
	msgs := e.messages(t)
	if len(msgs) != 4 {
		t.Fatalf("activity = %q, want a line for each of the four requests", msgs)
	}
	for _, m := range msgs {
		for _, r := range m {
			if unicode.IsControl(r) || r == 0x202e {
				t.Errorf("the line %q has the character %U in it", m, r)
			}
		}
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{
		`Deleted 2 items in several folders on laptop`, `\u001b[2Jscreen on laptop`, `fake\u202etxt.exe on laptop`, `tab\there on laptop`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("activity =\n%s\nwant it to show %q", joined, want)
		}
	}
}

// A single file's path is shown with its line break written out.
func TestFiles_hostileNameOfOneFileInTheLog(t *testing.T) {
	t.Parallel()
	e := newFilesEnv(t)
	p := e.write(t, "x\nDeleted /etc on prod", "x")
	body, err := json.Marshal(api.DeleteFiles{Paths: []string{p}})
	if err != nil {
		t.Fatal(err)
	}
	if code := e.post(t, "delete", string(body), nil); code != 200 {
		t.Fatalf("delete = %d", code)
	}
	want := "Deleted " + e.home + `/x\nDeleted /etc on prod on laptop`
	if got := e.messages(t); len(got) != 1 || got[0] != want {
		t.Errorf("activity = %q, want %q", got, want)
	}
}

func TestFiles_raw(t *testing.T) {
	t.Parallel()
	e := newFilesEnv(t)
	p := e.write(t, "page.html", "<script>alert(1)</script>")
	target := "/api/devices/laptop/files/raw?path=" + url.QueryEscape(p)
	rec := serve(e.h, newRequest(t, http.MethodGet, target, laptopIP))
	h := rec.Header()
	if rec.Code != 200 || rec.Body.String() != "<script>alert(1)</script>" || h.Get("Content-Type") != "text/plain; charset=utf-8" ||
		!strings.Contains(h.Get("Content-Security-Policy"), "sandbox") || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("raw = %d %q %v", rec.Code, rec.Body, h)
	}
	req := newRequest(t, http.MethodGet, target, laptopIP)
	req.Header.Set("Range", "bytes=1-6")
	if rec := serve(e.h, req); rec.Code != http.StatusPartialContent || rec.Body.String() != "script" {
		t.Errorf("ranged raw = %d %q", rec.Code, rec.Body)
	}
	if rec := serve(e.h, newRequest(t, http.MethodHead, target, laptopIP)); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Errorf("HEAD raw = %d with %d bytes", rec.Code, rec.Body.Len())
	}
	if rec := serve(e.h, newRequest(t, http.MethodGet, target+"&download=1", laptopIP)); !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment;") {
		t.Errorf("download=1 is %q", rec.Header().Get("Content-Disposition"))
	}
}

func TestFiles_archive(t *testing.T) {
	t.Parallel()
	e := newFilesEnv(t)
	e.write(t, "src/a.txt", "a")
	e.write(t, "src/sub/b.txt", "b")
	e.write(t, "src/c d.txt", "c")

	var tok api.ArchiveToken
	if code := e.post(t, "archive", `{"dir":"`+filepath.Join(e.home, "src")+`","names":["a.txt","sub"]}`, &tok); code != 200 || tok.Token == "" {
		t.Fatalf("archive = %d %+v", code, tok)
	}
	rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/files/archive?token="+tok.Token, laptopIP))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/gzip" ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), `src-2-items.tar.gz`) {
		t.Fatalf("download = %d %v", rec.Code, rec.Header())
	}
	got := tarFiles(t, rec.Body)
	if got["a.txt"] != "a" || got["sub/b.txt"] != "b" || len(got) != 2 {
		t.Errorf("archive has %v", got)
	}
	// A short selection can go in the URL.
	rec = serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/files/archive?dir="+url.QueryEscape(filepath.Join(e.home, "src"))+"&name="+url.QueryEscape("c d.txt"), laptopIP))
	if got := tarFiles(t, rec.Body); rec.Code != 200 || got["c d.txt"] != "c" {
		t.Errorf("download by name = %d %v", rec.Code, got)
	}
	if rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/files/archive?token=nope", laptopIP)); rec.Code != http.StatusGone {
		t.Errorf("download of an unknown token = %d, want 410", rec.Code)
	}
	if rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/files/archive?dir="+url.QueryEscape(e.home), laptopIP)); rec.Code != http.StatusBadRequest {
		t.Errorf("download of nothing = %d, want 400", rec.Code)
	}
	msgs := e.messages(t)
	if len(msgs) != 2 || msgs[1] != "Downloaded 2 items from "+filepath.Join(e.home, "src")+" as an archive on laptop" {
		t.Errorf("activity = %q", msgs)
	}
}

func TestFiles_search(t *testing.T) {
	t.Parallel()
	e := newFilesEnv(t)
	e.write(t, "a/needle.txt", "")
	e.write(t, "b/NEEDLE.md", "")
	e.write(t, "c/other", "")
	var found api.FileSearch
	get(t, e.h, "/api/devices/laptop/files/search?q=needle", laptopIP, &found)
	if found.Root != e.home || len(found.Entries) != 2 || found.Truncated {
		t.Errorf("search = %+v, want 2 under home", found)
	}
	for _, q := range []string{"", "a/b"} {
		if rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/laptop/files/search?q="+url.QueryEscape(q), laptopIP)); rec.Code != http.StatusBadRequest {
			t.Errorf("search for %q = %d, want 400", q, rec.Code)
		}
	}
}

func tarFiles(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(tr)
			out[h.Name] = string(b)
		}
	}
}

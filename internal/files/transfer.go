package files

import (
	"cmp"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/manasseh-fikadu/bield/internal/ssh"
)

// ErrAborted is returned when a download failed after its headers were
// sent, so the only way to tell the browser is to cut the connection.
var ErrAborted = errors.New("the transfer was cut short")

// sandbox is the Content-Security-Policy of everything served from a
// machine's files. Bield's origin can open shells on every machine, so
// nothing served from it may run script: this runs none, even in an SVG
// opened by itself. Only Bield's own pages may frame it.
const sandbox = "sandbox; default-src 'none'; img-src 'self' data: blob:; media-src 'self'; style-src 'unsafe-inline'; frame-ancestors 'self'"

// disposition returns a Content-Disposition with an ASCII name for old
// browsers and the real one as RFC 5987 says.
func disposition(kind, name string) string {
	fallback := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || strings.ContainsRune(`"\%;`, r) {
			return '_'
		}
		return r
	}, name)
	escaped := strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, kind, fallback, escaped)
}

var byteRange = regexp.MustCompile(`^bytes=(\d*)-(\d*)$`)

// parseRange reads a Range header for a file of size bytes. ok is false for
// no range, or one it doesn't handle, such as several: those get the whole
// file. invalid is true for a range outside the file.
func parseRange(h string, size int64) (start, end int64, ok, invalid bool) {
	m := byteRange.FindStringSubmatch(strings.TrimSpace(h))
	if m == nil {
		return 0, 0, false, false
	}
	if m[1] == "" {
		suffix, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil || suffix == 0 {
			return 0, 0, false, true
		}
		start, end = max(0, size-suffix), size-1
	} else {
		var err error
		if start, err = strconv.ParseInt(m[1], 10, 64); err != nil {
			return 0, 0, false, true
		}
		end = size - 1
		if m[2] != "" {
			e, err := strconv.ParseInt(m[2], 10, 64)
			if err != nil {
				return 0, 0, false, true
			}
			end = min(e, size-1)
		}
	}
	if start >= size || start > end {
		return 0, 0, false, true
	}
	return start, end, true, false
}

// quotaWriter writes the first n bytes it's given to w, and then fails with
// errQuota, which stops a copy that would otherwise go on to the end of a
// file.
type quotaWriter struct {
	w io.Writer
	n int64
}

var errQuota = errors.New("quota reached")

func (q *quotaWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > q.n {
		n, err := q.w.Write(p[:q.n])
		q.n -= int64(n)
		if err == nil {
			err = errQuota
		}
		return n, err
	}
	n, err := q.w.Write(p)
	q.n -= int64(n)
	if err == nil && q.n == 0 {
		err = errQuota
	}
	return n, err
}

// ServeRaw answers req with the file at p, or part of it, as the browser
// asks. Media, PDFs and text are shown inline; anything else, or anything
// when download is true, is a download. It copes with HEAD, Range and
// If-None-Match, and it compresses text it sends whole. It returns an error
// if the file can't be sent, and ErrAborted if that's after it began.
func (r *Remote) ServeRaw(w http.ResponseWriter, req *http.Request, p string, download bool) error {
	fi, err := r.sftp.Stat(p)
	if err != nil {
		return fsError(err, p)
	}
	switch {
	case fi.IsDir():
		return errorf(http.StatusBadRequest, "%s is a folder; download it as an archive", p)
	case !fi.Mode().IsRegular():
		return errorf(http.StatusBadRequest, "%s isn't a regular file", p)
	}
	name, size, mtime := path.Base(p), fi.Size(), fi.ModTime()
	inline := ""
	if !download {
		inline = InlineType(name)
	}
	etag := fmt.Sprintf(`"%x-%x"`, size, mtime.Unix())
	h := w.Header()
	h.Set("Content-Type", cmp.Or(inline, "application/octet-stream"))
	if inline != "" {
		h.Set("Content-Disposition", disposition("inline", name))
	} else {
		h.Set("Content-Disposition", disposition("attachment", name))
	}
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-cache")
	h.Set("Last-Modified", mtime.UTC().Format(http.TimeFormat))
	h.Set("X-File-Mtime", strconv.FormatInt(mtime.UnixMilli(), 10))
	h.Set("X-File-Size", strconv.FormatInt(size, 10))
	h.Set("Accept-Ranges", "bytes")
	h.Set("ETag", etag)
	// Chrome's PDF viewer won't load inside a sandboxed document, and a PDF
	// can't run script here.
	if KindOf(name) != KindPDF || inline == "" {
		h.Set("Content-Security-Policy", sandbox)
	}

	if req.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	start, end := int64(0), size
	var ranged bool
	// A client that holds an older copy gets the whole new one.
	if ifRange := req.Header.Get("If-Range"); ifRange == "" || ifRange == etag {
		first, last, ok, invalid := parseRange(req.Header.Get("Range"), size)
		if invalid {
			h.Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return nil
		}
		if ok {
			ranged, start, end = true, first, last+1
			h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, size))
		}
	}
	// Text is the one thing worth compressing on the fly, and only whole.
	zip := !ranged && strings.HasPrefix(inline, "text/") && size > 1024 && strings.Contains(req.Header.Get("Accept-Encoding"), "gzip")
	if zip {
		h.Set("Content-Encoding", "gzip")
		h.Set("Vary", "Accept-Encoding")
	} else {
		h.Set("Content-Length", strconv.FormatInt(end-start, 10))
	}
	status := http.StatusOK
	if ranged {
		status = http.StatusPartialContent
	}
	f, err := r.sftp.Open(p)
	if err != nil {
		return fsError(err, p)
	}
	defer func() { _ = f.Close() }()
	w.WriteHeader(status)
	if req.Method == http.MethodHead || end == start {
		return nil
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ErrAborted
	}
	var dst io.Writer = &quotaWriter{w: w, n: end - start}
	var gz *gzip.Writer
	if zip {
		gz = gzip.NewWriter(w)
		dst = &quotaWriter{w: gz, n: end - start}
	}
	// File.WriteTo keeps many reads in flight, which one read at a time
	// can't over a slow link.
	if _, err := f.WriteTo(dst); err != nil && !errors.Is(err, errQuota) {
		return fmt.Errorf("%w: reading %s: %w", ErrAborted, p, err)
	}
	if gz != nil {
		return gz.Close()
	}
	return nil
}

// ---- writing files ----

// readTracker remembers why a body stopped, so a browser that went away
// isn't taken for a problem with the machine.
type readTracker struct {
	r   io.Reader
	err error
}

func (t *readTracker) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err != nil && err != io.EOF {
		t.err = err
	}
	return n, err
}

// bodyError says why copying a body failed: the browser canceled, the body
// was too big, or the machine refused.
func bodyError(t *readTracker, err error, target string) error {
	var tooBig *http.MaxBytesError
	switch {
	case t.err == nil:
		return fsError(err, target)
	case errors.As(t.err, &tooBig):
		return errorf(http.StatusRequestEntityTooLarge, "That's more than %s can take", path.Base(target))
	default:
		return errorf(StatusClientClosed, "Upload canceled")
	}
}

// cappedReader fails with the error http.MaxBytesReader gives, as soon as
// its reader has more than left bytes.
type cappedReader struct {
	r    io.Reader
	left int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p[:min(int64(len(p)), c.left+1)])
	if c.left -= int64(n); c.left < 0 {
		return max(0, n+int(c.left)), &http.MaxBytesError{Limit: MaxText}
	}
	return n, err
}

// writeWith is how many writes are in flight for a file.
const writeWith = 16

// writeAtomically streams body into a hidden file beside target and renames
// it into place, so a transfer that stops halfway never replaces a good
// file. It keeps the permissions of the file it replaces, if replace says
// there was one.
func (r *Remote) writeAtomically(target string, body io.Reader, replace os.FileInfo, conflict Conflict) (string, int64, error) {
	dir := path.Dir(target)
	var salt [4]byte
	_, _ = rand.Read(salt[:])
	base := path.Base(target)
	tmp := path.Join(dir, "."+base[:min(len(base), 200)]+"."+hex.EncodeToString(salt[:])+".part")
	// A failure to make the hidden file is the folder's, and its name means
	// nothing.
	f, err := r.sftp.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		err = fsError(err, dir)
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusForbidden {
			return "", 0, errorf(http.StatusForbidden, "Permission denied: can't write to %s", dir)
		}
		return "", 0, err
	}
	track := &readTracker{r: body}
	n, err := f.ReadFromWithConcurrency(track, writeWith)
	if err == nil {
		err = f.Close()
	} else {
		_ = f.Close()
	}
	if err != nil {
		_ = r.sftp.Remove(tmp)
		return "", 0, bodyError(track, err, target)
	}
	final := target
	switch {
	case replace != nil:
		_ = r.sftp.Chmod(tmp, os.FileMode(rawMode(replace)&0o7777))
		err = r.replace(tmp, target)
	case conflict == ConflictRename:
		var free string
		if free, err = r.freeName(dir, base); err == nil {
			final = path.Join(dir, free)
			err = r.sftp.Rename(tmp, final)
		}
	default:
		err = r.sftp.Rename(tmp, final)
		if err != nil {
			if fi, _ := r.lstatIf(final); fi != nil {
				err = errorf(http.StatusConflict, "%s already exists", path.Base(final))
			}
		}
	}
	if err != nil {
		_ = r.sftp.Remove(tmp)
		return "", 0, fsError(err, target)
	}
	return final, n, nil
}

// An Uploaded is a file that was uploaded.
type Uploaded struct {
	Path, Name string
	Size       int64
}

// Upload writes body to the file called name in dir. The name may be a
// relative path from a folder upload, such as "photos/2024/a.jpg", whose
// folders are made.
func (r *Remote) Upload(body io.Reader, dir, name string, conflict Conflict) (Uploaded, error) {
	parts, err := safeRelative(name)
	if err != nil {
		return Uploaded{}, err
	}
	target := path.Join(append([]string{dir}, parts...)...)
	parent := path.Dir(target)
	if len(parts) > 1 {
		if err := r.sftp.MkdirAll(parent); err != nil {
			return Uploaded{}, errorf(http.StatusConflict, "%s exists and isn't a folder", parent)
		}
	} else if fi, err := r.sftp.Stat(parent); err != nil {
		return Uploaded{}, fsError(err, parent)
	} else if !fi.IsDir() {
		return Uploaded{}, errorf(http.StatusBadRequest, "%s isn't a folder", parent)
	}
	last := parts[len(parts)-1]
	current, err := r.lstatIf(target)
	if err != nil {
		return Uploaded{}, err
	}
	if current != nil && current.IsDir() {
		return Uploaded{}, errorf(http.StatusConflict, "A folder named %s already exists", last)
	}
	if current != nil && conflict != ConflictOverwrite && conflict != ConflictRename {
		return Uploaded{}, errorf(http.StatusConflict, "%s already exists", last)
	}
	var replace os.FileInfo
	if current != nil && conflict == ConflictOverwrite {
		if replace, err = r.sftp.Stat(target); err != nil {
			replace = current
		}
		// Overwriting through a link replaces the file it points to.
		if target, err = r.resolveLinks(target); err != nil {
			return Uploaded{}, err
		}
	}
	final, size, err := r.writeAtomically(target, body, replace, conflict)
	if err != nil {
		return Uploaded{}, err
	}
	return Uploaded{Path: final, Name: path.Base(final), Size: size}, nil
}

// resolveLinks follows p if it's a link, and the links it points to, to the
// file at the end. A server's own RealPath can't be trusted to: OpenSSH's
// does, and others leave the path as it is.
func (r *Remote) resolveLinks(p string) (string, error) {
	for range 40 {
		fi, err := r.sftp.Lstat(p)
		if err != nil {
			return "", fsError(err, p)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			return p, nil
		}
		target, err := r.sftp.ReadLink(p)
		if err != nil {
			return "", fsError(err, p)
		}
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(p), target)
		}
		p = target
	}
	return "", errorf(http.StatusBadRequest, "Too many links in a row to reach the file at %s", p)
}

// MaxText is the biggest file the editor saves.
const MaxText = 10 << 20

// WriteText saves the editor's buffer as the file at p. Unless create is
// true, the file must be there already, and its modification time must be
// expectedMtime if that isn't zero, so a file that changed since the editor
// loaded it isn't overwritten. length is the body's size, or -1 if unknown.
func (r *Remote) WriteText(body io.Reader, length int64, p string, create bool, expectedMtime time.Time) (Entry, error) {
	if length > MaxText {
		return Entry{}, errorf(http.StatusRequestEntityTooLarge, "Files over 10 MB can only be uploaded, not edited here")
	}
	current, err := r.lstatIf(p)
	if err != nil {
		return Entry{}, err
	}
	name := path.Base(p)
	switch {
	case create && current != nil:
		return Entry{}, errorf(http.StatusConflict, "%s already exists", name)
	case !create && current == nil:
		return Entry{}, errorf(http.StatusNotFound, "%s doesn't exist anymore", p)
	}
	target := p
	var st os.FileInfo
	if current != nil {
		if target, err = r.resolveLinks(p); err != nil {
			return Entry{}, err
		}
		if st, err = r.sftp.Stat(target); err != nil {
			return Entry{}, fsError(err, target)
		}
		if !st.Mode().IsRegular() {
			return Entry{}, errorf(http.StatusBadRequest, "%s isn't a regular file", p)
		}
		if !expectedMtime.IsZero() && !st.ModTime().Equal(expectedMtime) {
			return Entry{}, errorf(http.StatusConflict, "%s changed on disk since you opened it", name)
		}
	}
	body = &cappedReader{r: body, left: MaxText}
	_, _, err = r.writeAtomically(target, body, st, ConflictFail)
	var e *Error
	// No write access to the folder, say for a config file in a root-owned
	// one: write in place instead.
	if errors.As(err, &e) && e.Status == http.StatusForbidden && st != nil {
		f, openErr := r.sftp.OpenFile(target, os.O_WRONLY|os.O_TRUNC)
		if openErr != nil {
			return Entry{}, fsError(openErr, target)
		}
		track := &readTracker{r: body}
		_, err = f.ReadFromWithConcurrency(track, writeWith)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return Entry{}, bodyError(track, err, target)
		}
	} else if err != nil {
		return Entry{}, err
	}
	return r.Stat(p)
}

// ---- archives ----

// archiveTTL is how long a selection stays parked.
const archiveTTL = 5 * time.Minute

// Archives parks selections of files to download as an archive. A big
// selection doesn't fit in a URL, so the browser sends it first and downloads
// with the token it gets.
type Archives struct {
	mu     sync.Mutex
	parked map[string]parked
}

type parked struct {
	deviceID, dir string
	names         []string
	expires       time.Time
}

// Park keeps a selection, and returns the token to download it with.
func (a *Archives) Park(deviceID, dir string, names []string, now time.Time) string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	a.mu.Lock()
	defer a.mu.Unlock()
	for k, v := range a.parked {
		if v.expires.Before(now) {
			delete(a.parked, k)
		}
	}
	if a.parked == nil {
		a.parked = map[string]parked{}
	}
	a.parked[token] = parked{deviceID: deviceID, dir: dir, names: names, expires: now.Add(archiveTTL)}
	return token
}

// Take returns the selection parked under token for the device, or false if
// there's none, such as after it expired. It stays parked, so a download that
// fails can try again.
func (a *Archives) Take(token, deviceID string, now time.Time) (dir string, names []string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.parked[token]
	if !ok || p.deviceID != deviceID || p.expires.Before(now) {
		return "", nil, false
	}
	return p.dir, p.names, true
}

// maxArchiveCommand is the longest command an archive takes: the shell can
// read about 128 KiB for one.
const maxArchiveCommand = 100 << 10

// archiveScript writes a gzipped tar of the names, which are in the folder
// that is its first argument, to its output. GNU tar reads a backslash in a
// name as an escape, so a file called \n is a newline to it, unless it's
// told not to; other tars don't have the option, and don't need it.
// COPYFILE_DISABLE keeps a Mac's tar from adding ._ files for what it keeps
// beside a file.
const archiveScript = `d=$1; shift; u=; tar --no-unquote --version >/dev/null 2>&1 && u=--no-unquote; ` +
	`COPYFILE_DISABLE=1 exec tar $u -czf - -C "$d" -- "$@"`

// Archive starts a gzipped tar of the named files and folders in dir. It
// returns its output as a Stream, and the name to download it as. It fails
// before any output if a file isn't there. tar goes on past files it can't
// read, and what it said about them is in the Stream's stderr.
func (r *Remote) Archive(ctx context.Context, dir string, names []string) (*ssh.Stream, string, error) {
	if len(names) == 0 {
		return nil, "", errorf(http.StatusBadRequest, "Nothing to archive")
	}
	total := len(ssh.Quote(dir))
	for _, n := range names {
		if _, err := SafeName(n); err != nil {
			return nil, "", err
		}
		total += len(ssh.Quote(n)) + 1
	}
	if total > maxArchiveCommand {
		return nil, "", errorf(http.StatusBadRequest, "Too many items for one archive; download their folder instead")
	}
	var bad error
	var mu sync.Mutex
	parallel(names, 16, func(_ int, n string) {
		if _, err := r.sftp.Lstat(path.Join(dir, n)); err != nil {
			mu.Lock()
			bad = fsError(err, path.Join(dir, n))
			mu.Unlock()
		}
	})
	if bad != nil {
		return nil, "", bad
	}
	base := names[0]
	if len(names) > 1 {
		folder := "root"
		if dir != "/" {
			folder = path.Base(dir)
		}
		base = fmt.Sprintf("%s-%d-items", folder, len(names))
	}
	args := []string{dir}
	for _, n := range names {
		// bsdtar, which a Mac has, reads a name that starts with @ as an
		// archive to copy the members of, so that name goes in as a path.
		if strings.HasPrefix(n, "@") {
			n = "./" + n
		}
		args = append(args, n)
	}
	st, err := r.mgr.Stream(ctx, r.ID, archiveScript, args...)
	return st, base + ".tar.gz", err
}

// ArchiveDisposition returns the Content-Disposition for downloading an
// archive called name.
func ArchiveDisposition(name string) string { return disposition("attachment", name) }

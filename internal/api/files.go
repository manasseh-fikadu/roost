package api

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/manasseh-fikadu/bield/internal/files"
	"github.com/manasseh-fikadu/bield/internal/store"
)

// maxFilesBody is the largest JSON body the file routes read: ten thousand
// long paths.
const maxFilesBody = 4 << 20

// defaultMaxUpload is the most one uploaded file may have, unless Options says.
const defaultMaxUpload = 10 << 30

const (
	// uploadQuiet is how long after an upload's last file the activity log
	// waits to write its line, so a folder of a thousand files is one line.
	uploadQuiet = 5 * time.Second
	// editQuiet is how long saving the same file again counts as the same
	// edit.
	editQuiet = 10 * time.Minute
)

// fileAudit writes the activity log's lines about files that would be too
// many if every request had one.
type fileAudit struct {
	mu      sync.Mutex
	uploads map[string]*uploadBatch
	edits   map[string]time.Time
}

type uploadBatch struct {
	files int
	bytes int64
	first string
	// event is the line without its message, and device the name of the
	// device in it.
	event  store.Event
	device string
	dir    string
	timer  *time.Timer
}

// upload counts a file in the batch of its folder, and sets the batch to be
// logged once the uploads to it go quiet.
func (a *fileAudit) upload(h *handler, e store.Event, device, dir, name string, size int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := e.Actor + "\x00" + e.DeviceID + "\x00" + dir
	if a.uploads == nil {
		a.uploads = map[string]*uploadBatch{}
	}
	b := a.uploads[key]
	if b == nil {
		b = &uploadBatch{first: name, event: e, device: device, dir: dir}
		a.uploads[key] = b
	} else {
		b.timer.Stop()
	}
	b.files++
	b.bytes += size
	b.timer = time.AfterFunc(uploadQuiet, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.flushLocked(h, key)
	})
}

// flushLocked logs the batch with the key, if there is one.
func (a *fileAudit) flushLocked(h *handler, key string) {
	b := a.uploads[key]
	if b == nil {
		return
	}
	delete(a.uploads, key)
	what := b.first
	if b.files > 1 {
		what = strconv.Itoa(b.files) + " files"
	}
	b.event.Message = printable(fmt.Sprintf("Uploaded %s (%s) to %s on %s", what, byteLabel(b.bytes), b.dir, b.device))
	h.addEvent(context.WithoutCancel(context.Background()), b.event)
}

// flushAll logs every batch that is waiting, when Bield shuts down.
func (a *fileAudit) flushAll(h *handler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, b := range a.uploads {
		b.timer.Stop()
		a.flushLocked(h, key)
	}
}

// first reports whether what is the first time in editQuiet that actor did
// it on a device, as kind says: one line for saving the same file over and
// over, or for reading the same file's bytes in pieces, however many requests
// that takes. The window slides, so it stays one while it goes on.
func (a *fileAudit) first(kind, actor, deviceID, what string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.edits == nil {
		a.edits = map[string]time.Time{}
	}
	for k, at := range a.edits {
		if now.Sub(at) > editQuiet {
			delete(a.edits, k)
		}
	}
	key := kind + "\x00" + actor + "\x00" + deviceID + "\x00" + what
	_, again := a.edits[key]
	a.edits[key] = now
	return !again
}

// byteLabel writes a size as "1.5 MB".
func byteLabel(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f, i := float64(n), 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

// fileError answers with the status and message for an error from the file
// operations, and reports whether there was one.
func (h *handler) fileError(w http.ResponseWriter, r *http.Request, err error) bool {
	var fe *files.Error
	switch {
	case err == nil:
		return false
	case errors.As(err, &fe):
		if fe.Status == files.StatusClientClosed {
			// The browser went away.
			return true
		}
		h.writeJSON(w, r, fe.Status, ErrorBody{Error: fe.Msg})
		return true
	default:
		return h.sshError(w, r, err)
	}
}

// openFiles connects to the files of the device in the path. If it can't, it
// answers with an error and returns nil.
func (h *handler) openFiles(w http.ResponseWriter, r *http.Request) *files.Remote {
	rem, err := files.Open(r.Context(), h.opts.SSH, r.PathValue("id"))
	if h.fileError(w, r, err) {
		return nil
	}
	return rem
}

// abs checks a path from the request, or answers with why not.
func (h *handler) abs(w http.ResponseWriter, r *http.Request, rem *files.Remote, p string) (string, bool) {
	out, err := rem.Abs(p)
	return out, !h.fileError(w, r, err)
}

// fileEntry converts an entry for the API.
func fileEntry(e files.Entry) FileEntry {
	return FileEntry{
		Name: e.Name, Path: e.Path, Type: FileType(e.Type), TargetType: FileType(e.TargetType), Target: e.Target,
		Size: e.Size, ModifiedAt: e.ModTime, Mode: e.Mode, Owner: e.Owner, Group: e.Group,
	}
}

func fileEntries(in []files.Entry) []FileEntry {
	out := make([]FileEntry, len(in))
	for i, e := range in {
		out[i] = fileEntry(e)
	}
	return out
}

func fileBulk(b files.Bulk) FileBulk {
	out := FileBulk{Done: b.Done, Failed: make([]FileFailure, len(b.Failed)), Paths: b.Paths}
	if out.Paths == nil {
		out.Paths = []string{}
	}
	for i, f := range b.Failed {
		out.Failed[i] = FileFailure{Path: f.Path, Error: f.Error}
	}
	return out
}

// itemsLabel names what a request changed: the path, or how many items and
// where.
func itemsLabel(paths []string) string {
	if len(paths) == 1 {
		return paths[0]
	}
	return strconv.Itoa(len(paths)) + " items in " + commonDir(paths)
}

func commonDir(paths []string) string {
	dir := path.Dir(paths[0])
	for _, p := range paths[1:] {
		if path.Dir(p) != dir {
			return "several folders"
		}
	}
	return dir
}

// logFiles adds a line about files to the activity log. Names of files are
// whatever their owners called them, so the line shows them with their control
// characters spelled out (see printable).
func (h *handler) logFiles(r *http.Request, format string, args ...any) {
	id := r.PathValue("id")
	h.logAction(r, EventKindFiles, id, printable(fmt.Sprintf(format, args...))+" on "+h.deviceName(r, id))
}

// printable returns s with what can't be shown as itself written out: a file
// can be called "x", a newline and "Deleted /etc on prod", and an activity
// log that showed that as it is would show a line no one wrote. Control
// characters and the ones that turn text around, such as U+202E, become
// escapes like \n and \u202e, and bytes that aren't UTF-8 become U+FFFD.
func printable(s string) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	if !strings.ContainsFunc(s, unprintable) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case unprintable(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// unprintable reports whether r is a control character, or one of the marks
// that change which way text runs.
func unprintable(r rune) bool {
	return unicode.IsControl(r) || r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}

// logRead adds a line about a file being read to the activity log, the first
// time in a while that this admin reads it (see fileAudit.first). It's for
// what was sent, even if only part of it was.
func (h *handler) logRead(r *http.Request, verb, file string) {
	if h.audit.first("read "+verb, callerOf(r).who.LoginName, r.PathValue("id"), file, time.Now()) {
		h.logFiles(r, "%s %s", verb, file)
	}
}

// summary says what a request that changed several paths did.
func summary(verb string, res files.Bulk, paths []string, extra string) string {
	if len(res.Failed) == 0 {
		return verb + " " + itemsLabel(paths) + extra
	}
	return fmt.Sprintf("%s %d of %d items in %s%s", verb, res.Done, len(paths), commonDir(paths), extra)
}

// listFiles answers GET /api/devices/{id}/files/list?path=.
func (h *handler) listFiles(w http.ResponseWriter, r *http.Request) {
	rem := h.openFiles(w, r)
	if rem == nil {
		return
	}
	defer rem.Close()
	p := r.URL.Query().Get("path")
	if p != "" {
		var ok bool
		if p, ok = h.abs(w, r, rem, p); !ok {
			return
		}
	}
	l, err := rem.List(p)
	if h.fileError(w, r, err) {
		return
	}
	out := FileListing{Path: l.Path, Parent: l.Parent, Home: l.Home, Entries: fileEntries(l.Entries)}
	if l.Space != nil {
		out.Space = &FileSpace{Free: l.Space.Free, Total: l.Space.Total}
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// statFile answers GET /api/devices/{id}/files/stat?path=.
func (h *handler) statFile(w http.ResponseWriter, r *http.Request) {
	rem := h.openFiles(w, r)
	if rem == nil {
		return
	}
	defer rem.Close()
	p, ok := h.abs(w, r, rem, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	e, err := rem.Stat(p)
	if h.fileError(w, r, err) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, FileStat{Entry: fileEntry(e), Home: rem.Home()})
}

// rawFile answers GET and HEAD /api/devices/{id}/files/raw?path=&download=1
// with the file.
func (h *handler) rawFile(w http.ResponseWriter, r *http.Request) {
	rem := h.openFiles(w, r)
	if rem == nil {
		return
	}
	defer rem.Close()
	p, ok := h.abs(w, r, rem, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	download := r.URL.Query().Get("download") == "1"
	err := rem.ServeRaw(w, r, p, download)
	// A file's content leaves the machine here, so this is logged like the
	// archive is, though once for a file that is read in pieces, or again and
	// again by a preview. Even a download that broke off sent some of it.
	if (err == nil || errors.Is(err, files.ErrAborted)) && r.Method == http.MethodGet {
		verb := "Opened"
		if download {
			verb = "Downloaded"
		}
		h.logRead(r, verb, p)
	}
	if errors.Is(err, files.ErrAborted) {
		// The headers are out. The browser can only know by the connection
		// closing before the end.
		if r.Context().Err() == nil {
			h.log.Warn("a download failed", "device", r.PathValue("id"), "error", err)
		}
		panic(http.ErrAbortHandler)
	}
	h.fileError(w, r, err)
}

// parkArchive answers POST /api/devices/{id}/files/archive.
func (h *handler) parkArchive(w http.ResponseWriter, r *http.Request) {
	var body ArchiveFiles
	if !h.readJSONUpTo(w, r, &body, maxFilesBody) {
		return
	}
	rem := h.openFiles(w, r)
	if rem == nil {
		return
	}
	defer rem.Close()
	dir, ok := h.abs(w, r, rem, body.Dir)
	if !ok {
		return
	}
	if len(body.Names) == 0 {
		h.fileError(w, r, &files.Error{Status: http.StatusBadRequest, Msg: "Nothing to archive"})
		return
	}
	for _, n := range body.Names {
		if _, err := files.SafeName(n); h.fileError(w, r, err) {
			return
		}
	}
	token := h.archives.Park(r.PathValue("id"), dir, body.Names, time.Now())
	h.writeJSON(w, r, http.StatusOK, ArchiveToken{Token: token})
}

// archive answers GET /api/devices/{id}/files/archive with a .tar.gz of the
// files in a parked selection, or in ?dir=&name=&name=.
func (h *handler) archive(w http.ResponseWriter, r *http.Request) {
	id, q := r.PathValue("id"), r.URL.Query()
	rem := h.openFiles(w, r)
	if rem == nil {
		return
	}
	defer rem.Close()
	dir, names := q.Get("dir"), q["name"]
	if token := q.Get("token"); token != "" {
		var ok bool
		if dir, names, ok = h.archives.Take(token, id, time.Now()); !ok {
			h.writeJSON(w, r, http.StatusGone, ErrorBody{Error: "That download link expired; start it again"})
			return
		}
	}
	dir, ok := h.abs(w, r, rem, dir)
	if !ok {
		return
	}
	st, name, err := rem.Archive(r.Context(), dir, names)
	if h.fileError(w, r, err) {
		return
	}
	defer func() { _ = st.Close() }()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", files.ArchiveDisposition(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, copyErr := io.Copy(w, st)
	// tar goes on past files it can't read, and still makes an archive of
	// the rest.
	if copyErr == nil {
		if code, stderr := st.Wait(); stderr != "" {
			first := strings.Join(strings.SplitN(strings.TrimSpace(stderr), "\n", 3), " | ")
			h.log.Info("tar skipped files", "device", id, "dir", dir, "exit", code, "said", first)
		}
	}
	what := dir + "/" + names[0]
	if dir == "/" {
		what = "/" + names[0]
	}
	if len(names) > 1 {
		what = strconv.Itoa(len(names)) + " items from " + dir
	}
	h.logFiles(r, "Downloaded %s as an archive", what)
}

// namesExist answers POST /api/devices/{id}/files/exists.
func (h *handler) namesExist(w http.ResponseWriter, r *http.Request) {
	var body CheckNames
	if !h.readJSONUpTo(w, r, &body, maxFilesBody) {
		return
	}
	rem := h.openFiles(w, r)
	if rem == nil {
		return
	}
	defer rem.Close()
	dir, ok := h.abs(w, r, rem, body.Dir)
	if !ok {
		return
	}
	existing, err := rem.Exists(dir, body.Names)
	if h.fileError(w, r, err) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, ExistingNames{Existing: existing})
}

// conflictOf reads a conflict mode, which is fail unless it says.
func conflictOf(s string) files.Conflict {
	switch files.Conflict(s) {
	case files.ConflictOverwrite, files.ConflictRename:
		return files.Conflict(s)
	default:
		return files.ConflictFail
	}
}

// uploadFile answers PUT /api/devices/{id}/files/upload?dir=&name=&conflict=
// with the body written to the file.
func (h *handler) uploadFile(w http.ResponseWriter, r *http.Request) {
	limit := cmp.Or(h.opts.MaxUpload, defaultMaxUpload)
	if r.ContentLength > limit {
		// Before connecting to the machine, or reading any of it.
		h.writeJSON(w, r, http.StatusRequestEntityTooLarge, ErrorBody{Error: "Bield uploads files up to " + byteLabel(limit)})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	rem := h.openFiles(w, r)
	if rem == nil {
		return
	}
	defer rem.Close()
	q := r.URL.Query()
	dir, ok := h.abs(w, r, rem, q.Get("dir"))
	if !ok {
		return
	}
	up, err := rem.Upload(r.Body, dir, q.Get("name"), conflictOf(q.Get("conflict")))
	if h.fileError(w, r, err) {
		return
	}
	id, who := r.PathValue("id"), callerOf(r).who.LoginName
	// Batched by the folder the upload goes to, so a folder of files is one line
	// however deep it goes.
	h.audit.upload(h, store.Event{Kind: string(EventKindFiles), DeviceID: id, Actor: who}, h.deviceName(r, id), dir, up.Name, up.Size)
	h.writeJSON(w, r, http.StatusOK, FileUpload{Path: up.Path, Name: up.Name, Size: up.Size})
}

// writeFile answers PUT /api/devices/{id}/files/write?path=&create=1&expectedMtime=
// with the body saved as the file's content, and the file's entry.
func (h *handler) writeFile(w http.ResponseWriter, r *http.Request) {
	rem := h.openFiles(w, r)
	if rem == nil {
		return
	}
	defer rem.Close()
	q := r.URL.Query()
	p, ok := h.abs(w, r, rem, q.Get("path"))
	if !ok {
		return
	}
	var expected time.Time
	if ms, err := strconv.ParseInt(q.Get("expectedMtime"), 10, 64); err == nil {
		expected = time.UnixMilli(ms)
	}
	create := q.Get("create") == "1"
	body := http.MaxBytesReader(w, r.Body, files.MaxText)
	e, err := rem.WriteText(body, r.ContentLength, p, create, expected)
	if h.fileError(w, r, err) {
		return
	}
	// Saving the same file over and over while editing is one edit as far as
	// the log is concerned.
	if id := r.PathValue("id"); create || h.audit.first("edit", callerOf(r).who.LoginName, id, p, time.Now()) {
		verb := "Edited"
		if create {
			verb = "Created"
		}
		h.logFiles(r, "%s %s", verb, p)
	}
	h.writeJSON(w, r, http.StatusOK, fileEntry(e))
}

// makeFolder answers POST /api/devices/{id}/files/mkdir.
func (h *handler) makeFolder(w http.ResponseWriter, r *http.Request) {
	var body MakeFolder
	if !h.readJSON(w, r, &body) {
		return
	}
	rem := h.openFiles(w, r)
	if rem == nil {
		return
	}
	defer rem.Close()
	dir, ok := h.abs(w, r, rem, body.Dir)
	if !ok {
		return
	}
	e, err := rem.Mkdir(dir, body.Name)
	if h.fileError(w, r, err) {
		return
	}
	h.logFiles(r, "Created folder %s", e.Path)
	h.writeJSON(w, r, http.StatusOK, fileEntry(e))
}

// renameFile answers POST /api/devices/{id}/files/rename.
func (h *handler) renameFile(w http.ResponseWriter, r *http.Request) {
	var body RenameFile
	if !h.readJSON(w, r, &body) {
		return
	}
	rem := h.openFiles(w, r)
	if rem == nil {
		return
	}
	defer rem.Close()
	p, ok := h.abs(w, r, rem, body.Path)
	if !ok {
		return
	}
	e, err := rem.Rename(p, body.Name)
	if h.fileError(w, r, err) {
		return
	}
	if e.Path != p {
		h.logFiles(r, "Renamed %s to %s", p, e.Name)
	}
	h.writeJSON(w, r, http.StatusOK, fileEntry(e))
}

// transferFiles answers POST /api/devices/{id}/files/move and .../copy.
func (h *handler) transferFiles(verb string, do func(*files.Remote, context.Context, []string, string, files.Conflict) (files.Bulk, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body MoveFiles
		if !h.readJSONUpTo(w, r, &body, maxFilesBody) {
			return
		}
		rem := h.openFiles(w, r)
		if rem == nil {
			return
		}
		defer rem.Close()
		paths, err := rem.Paths(body.Paths)
		if h.fileError(w, r, err) {
			return
		}
		dest, ok := h.abs(w, r, rem, body.Dest)
		if !ok {
			return
		}
		res, err := do(rem, r.Context(), paths, dest, conflictOf(string(body.Conflict)))
		if h.fileError(w, r, err) {
			return
		}
		if res.Done > 0 {
			h.logFiles(r, "%s", summary(verb, res, paths, " to "+dest))
		}
		h.writeJSON(w, r, http.StatusOK, fileBulk(res))
	}
}

// deleteFiles answers POST /api/devices/{id}/files/delete.
func (h *handler) deleteFiles(w http.ResponseWriter, r *http.Request) {
	var body DeleteFiles
	if !h.readJSONUpTo(w, r, &body, maxFilesBody) {
		return
	}
	rem := h.openFiles(w, r)
	if rem == nil {
		return
	}
	defer rem.Close()
	paths, err := rem.Paths(body.Paths)
	if h.fileError(w, r, err) {
		return
	}
	res, err := rem.Delete(r.Context(), paths)
	if h.fileError(w, r, err) {
		return
	}
	if res.Done > 0 {
		h.logFiles(r, "%s", summary("Deleted", res, paths, ""))
	}
	h.writeJSON(w, r, http.StatusOK, fileBulk(res))
}

// searchFiles answers GET /api/devices/{id}/files/search?path=&q=&limit=&depth=.
func (h *handler) searchFiles(w http.ResponseWriter, r *http.Request) {
	rem := h.openFiles(w, r)
	if rem == nil {
		return
	}
	defer rem.Close()
	q := r.URL.Query()
	root := ""
	if p := q.Get("path"); p != "" {
		var ok bool
		if root, ok = h.abs(w, r, rem, p); !ok {
			return
		}
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	depth, _ := strconv.Atoi(q.Get("depth"))
	found, err := rem.Search(r.Context(), files.Search{Root: root, Query: q.Get("q"), Limit: limit, Depth: depth})
	if h.fileError(w, r, err) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, FileSearch{Root: found.Root, Query: found.Query, Entries: fileEntries(found.Entries), Truncated: found.Truncated})
}

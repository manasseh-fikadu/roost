// Package files works on the files of the tailnet's machines over SFTP, with
// a few shell commands for what SFTP can't do: copying, deleting folders,
// searching and archiving. It is the file manager's half of the work, and
// the API serves it.
package files

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"

	"github.com/manasseh-fikadu/roost/internal/ssh"
)

// An Error says why a file operation failed, in words for the person who
// asked, and the HTTP status the API answers with.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func errorf(status int, format string, args ...any) *Error {
	return &Error{Status: status, Msg: fmt.Sprintf(format, args...)}
}

// StatusClientClosed is the status of an upload the browser canceled. It's
// nginx's, since HTTP has none; nothing reads it, as the client is gone.
const StatusClientClosed = 499

// fsError turns an error from SFTP into an Error that names the path.
func fsError(err error, p string) error {
	var (
		e      *Error
		status *sftp.StatusError
	)
	switch {
	case err == nil:
		return nil
	case errors.As(err, &e), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, fs.ErrNotExist):
		return errorf(http.StatusNotFound, "%s doesn't exist", p)
	case errors.Is(err, fs.ErrPermission):
		return errorf(http.StatusForbidden, "Permission denied: %s", p)
	case errors.Is(err, sftp.ErrSSHFxOpUnsupported):
		return errorf(http.StatusNotImplemented, "The machine's SFTP server doesn't support that")
	case errors.As(err, &status) && status.Code == 4:
		return errorf(http.StatusBadRequest, "Couldn't complete that on %s", p)
	case errors.Is(err, os.ErrClosed), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return errorf(http.StatusBadGateway, "The SFTP session closed; try again")
	default:
		return errorf(http.StatusBadGateway, "%s: %v", p, err)
	}
}

// failed reports whether err is SFTP's generic failure, which is what a rename
// across file systems gives.
func failed(err error) bool {
	var status *sftp.StatusError
	return errors.As(err, &status) && status.Code == 4
}

// A Type is what a file is.
type Type string

// The types of file.
const (
	TypeFile    Type = "file"
	TypeDir     Type = "dir"
	TypeSymlink Type = "symlink"
	TypeOther   Type = "other"
)

// An Entry is a file, folder or link, as listing a folder shows it.
type Entry struct {
	Name string
	// Path is absolute on the machine.
	Path string
	// Type is what the entry itself is, without following a link.
	Type Type
	// TargetType is what a link points to, or "" if it's broken or the
	// entry isn't a link.
	TargetType Type
	// Target is where a link points, as written.
	Target string
	// Size is in bytes. For a link to a file, it's the file's.
	Size int64
	// ModTime is when it last changed. SFTP gives whole seconds.
	ModTime time.Time
	// Mode is its permissions as ls shows them, such as "drwxr-xr-x".
	Mode string
	// Owner and Group are names, or numbers when the machine has none.
	Owner, Group string
}

// A Listing is a folder and what's in it.
type Listing struct {
	// Path is the folder, with links resolved, and Parent the folder above
	// it, or "" for the root.
	Path, Parent string
	// Home is the home folder of the user Roost logs in as.
	Home    string
	Entries []Entry
	// Space is the room on the file system that holds the folder, or nil if
	// the machine doesn't say.
	Space *Space
}

// Space is the free and total bytes of a file system.
type Space struct {
	Free, Total uint64
}

// A Remote is the files of one machine, over a connection that it holds
// until Close.
type Remote struct {
	// ID is the device's.
	ID      string
	mgr     *ssh.Manager
	sftp    *ssh.SFTP
	release func()
}

// Open connects to the files of a device. Close the Remote when done.
func Open(ctx context.Context, mgr *ssh.Manager, deviceID string) (*Remote, error) {
	c, release, err := mgr.SFTP(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	return &Remote{ID: deviceID, mgr: mgr, sftp: c, release: release}, nil
}

// Close lets go of the connection.
func (r *Remote) Close() { r.release() }

// Home returns the home folder of the user Roost logs in as.
func (r *Remote) Home() string { return r.sftp.Home }

// ---- paths ----

const (
	maxPath = 4096
	maxName = 255
	// maxDepth is the most folders deep an uploaded file can be.
	maxDepth = 64
)

// Abs checks a path from outside and cleans it: "~" stands for the home
// folder, and "/a/b/../c/" is "/a/c".
func (r *Remote) Abs(p string) (string, error) { return absPath(p, r.sftp.Home) }

func absPath(p, home string) (string, error) {
	if home != "" && (p == "~" || strings.HasPrefix(p, "~/")) {
		p = home + p[1:]
	}
	if !strings.HasPrefix(p, "/") {
		return "", errorf(http.StatusBadRequest, "Paths must be absolute")
	}
	if len(p) > maxPath || strings.ContainsRune(p, 0) {
		return "", errorf(http.StatusBadRequest, "Invalid path")
	}
	return path.Clean(p), nil
}

// SafeName checks that a name is one segment of a path: not empty, "." or
// "..", and without a slash or NUL.
func SafeName(name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00") || len(name) > maxName {
		return "", errorf(http.StatusBadRequest, "Invalid name: %s", cmp.Or(name, "(empty)"))
	}
	return name, nil
}

// safeRelative splits a relative path from a folder upload, such as
// "photos/2024/a.jpg", into checked names.
func safeRelative(rel string) ([]string, error) {
	var parts []string
	for part := range strings.SplitSeq(rel, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 || len(parts) > maxDepth {
		return nil, errorf(http.StatusBadRequest, "Invalid file name")
	}
	for _, p := range parts {
		if _, err := SafeName(p); err != nil {
			return nil, err
		}
	}
	return parts, nil
}

// extension matches the end of a name that stays together when a number is
// put before it: ".xlsx", and ".tar.gz" as a whole.
var extension = regexp.MustCompile(`(?i)^(.+?)((?:\.tar)?\.[^.\s]{1,10})$`)

// splitExt turns "report.final.xlsx" into "report.final" and ".xlsx".
func splitExt(name string) (stem, ext string) {
	if m := extension.FindStringSubmatch(name); m != nil && !strings.HasPrefix(name, ".") {
		return m[1], m[2]
	}
	return name, ""
}

// freeName returns the first of name, "name (1).ext", "name (2).ext" and so
// on that isn't in dir.
func (r *Remote) freeName(dir, name string) (string, error) {
	if _, err := r.sftp.Lstat(path.Join(dir, name)); errors.Is(err, fs.ErrNotExist) {
		return name, nil
	} else if err != nil {
		return "", fsError(err, path.Join(dir, name))
	}
	stem, ext := splitExt(name)
	for i := 1; i < 1000; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if _, err := r.sftp.Lstat(path.Join(dir, candidate)); errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", fsError(err, path.Join(dir, candidate))
		}
	}
	return "", errorf(http.StatusConflict, "Too many copies of %s", name)
}

// lstatIf returns what's at p without following a link, or nil if nothing is.
func (r *Remote) lstatIf(p string) (os.FileInfo, error) {
	fi, err := r.sftp.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil //nolint:nilnil // Nothing there is not an error.
	}
	return fi, fsError(err, p)
}

// ---- entries ----

// The bits of a POSIX mode.
const (
	modeType = 0o170000
	modeDir  = 0o040000
	modeReg  = 0o100000
	modeLink = 0o120000
)

func typeOf(mode uint32) Type {
	switch mode & modeType {
	case modeReg:
		return TypeFile
	case modeDir:
		return TypeDir
	case modeLink:
		return TypeSymlink
	default:
		return TypeOther
	}
}

// modeString writes a POSIX mode as ls does: "drwxr-xr-x", "-rwsr-xr-x".
func modeString(mode uint32) string {
	kinds := map[uint32]byte{modeDir: 'd', modeLink: 'l', 0o020000: 'c', 0o060000: 'b', 0o010000: 'p', 0o140000: 's'}
	kind, ok := kinds[mode&modeType]
	if !ok {
		kind = '-'
	}
	triple := func(bits uint32, special bool, ch byte) string {
		b := []byte("---")
		for i, c := range []byte("rwx") {
			if bits&(4>>i) != 0 {
				b[i] = c
			}
		}
		if special {
			b[2] = ch
			if bits&1 == 0 {
				b[2] = ch - 'a' + 'A'
			}
		}
		return string(b)
	}
	return string(kind) + triple(mode>>6&7, mode&0o4000 != 0, 's') + triple(mode>>3&7, mode&0o2000 != 0, 's') + triple(mode&7, mode&0o1000 != 0, 't')
}

// rawMode returns the POSIX mode that SFTP reported for fi.
func rawMode(fi os.FileInfo) uint32 {
	if st, ok := fi.Sys().(*sftp.FileStat); ok {
		return st.Mode
	}
	return 0
}

// makeEntry describes fi, which is what's at p.
func (r *Remote) makeEntry(p string, fi os.FileInfo, names *idNames) Entry {
	mode := rawMode(fi)
	e := Entry{
		Name: path.Base(p), Path: p, Type: typeOf(mode), Size: fi.Size(), ModTime: fi.ModTime(), Mode: modeString(mode),
	}
	if st, ok := fi.Sys().(*sftp.FileStat); ok {
		e.Owner, e.Group = names.user(st.UID), names.group(st.GID)
	}
	return e
}

// resolveLink fills in where a link points and what's there, so linked
// folders can still be opened.
func (r *Remote) resolveLink(e *Entry) {
	if e.Type != TypeSymlink {
		return
	}
	if target, err := r.sftp.ReadLink(e.Path); err == nil {
		e.Target = target
	}
	if fi, err := r.sftp.Stat(e.Path); err == nil {
		e.TargetType = typeOf(rawMode(fi))
		if e.TargetType == TypeFile {
			e.Size = fi.Size()
		}
	}
}

// Stat describes what's at p, without following a link.
func (r *Remote) Stat(p string) (Entry, error) {
	fi, err := r.sftp.Lstat(p)
	if err != nil {
		return Entry{}, fsError(err, p)
	}
	e := r.makeEntry(p, fi, r.names())
	r.resolveLink(&e)
	return e, nil
}

// parallel calls fn for each of items, at most limit at a time, and waits.
func parallel[T any](items []T, limit int, fn func(i int, item T)) {
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, max(1, limit))
	)
	for i, item := range items {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			fn(i, item)
		})
	}
	wg.Wait()
}

// ---- listing ----

// List describes the folder at p, or the home folder if p is "".
func (r *Remote) List(p string) (Listing, error) {
	if p == "" {
		p = r.sftp.Home
	}
	resolved, err := r.sftp.RealPath(p)
	if err != nil {
		return Listing{}, fsError(err, p)
	}
	fi, err := r.sftp.Stat(resolved)
	if err != nil {
		return Listing{}, fsError(err, resolved)
	}
	if !fi.IsDir() {
		return Listing{}, errorf(http.StatusBadRequest, "%s isn't a folder", resolved)
	}
	items, err := r.sftp.ReadDir(resolved)
	if err != nil {
		return Listing{}, fsError(err, resolved)
	}
	names := r.names()
	out := Listing{Path: resolved, Home: r.sftp.Home, Entries: make([]Entry, len(items))}
	if resolved != "/" {
		out.Parent = path.Dir(resolved)
	}
	for i, fi := range items {
		out.Entries[i] = r.makeEntry(path.Join(resolved, fi.Name()), fi, names)
	}
	var links []int
	for i, e := range out.Entries {
		if e.Type == TypeSymlink {
			links = append(links, i)
		}
	}
	parallel(links, 16, func(_ int, i int) { r.resolveLink(&out.Entries[i]) })
	if vfs, err := r.sftp.StatVFS(resolved); err == nil {
		out.Space = &Space{Free: vfs.Bavail * vfs.Frsize, Total: vfs.Blocks * vfs.Frsize}
	}
	return out, nil
}

// Exists returns which of names are in dir. A name can be a relative path,
// such as "photos/a.jpg" from a folder upload.
func (r *Remote) Exists(dir string, names []string) ([]string, error) {
	hits := make([]bool, len(names))
	var bad error
	var mu sync.Mutex
	parallel(names, 16, func(i int, n string) {
		parts, err := safeRelative(n)
		if err == nil {
			var fi os.FileInfo
			fi, err = r.lstatIf(path.Join(append([]string{dir}, parts...)...))
			hits[i] = fi != nil
		}
		if err != nil {
			mu.Lock()
			bad = err
			mu.Unlock()
		}
	})
	if bad != nil {
		return nil, bad
	}
	existing := []string{}
	for i, n := range names {
		if hits[i] {
			existing = append(existing, n)
		}
	}
	return existing, nil
}

// ---- owners ----

// idNames are the names of the users and groups of one connection, from the
// machine's /etc/passwd and /etc/group, which SFTP lists only as numbers.
type idNames struct {
	users, groups map[uint32]string
}

func (n *idNames) user(id uint32) string  { return nameOf(n.users, id) }
func (n *idNames) group(id uint32) string { return nameOf(n.groups, id) }

func nameOf(m map[uint32]string, id uint32) string {
	if name, ok := m[id]; ok {
		return name
	}
	return strconv.FormatUint(uint64(id), 10)
}

// names returns the user and group names of the connection, read once. A
// machine whose users aren't in those files, such as a Mac's, shows numbers.
func (r *Remote) names() *idNames {
	v, _ := r.sftp.Cache.LoadOrStore("names", &nameCache{})
	c, _ := v.(*nameCache)
	c.once.Do(func() {
		c.names = &idNames{users: r.readIDs("/etc/passwd"), groups: r.readIDs("/etc/group")}
	})
	return c.names
}

// nameCache holds the names of a connection once they are read.
type nameCache struct {
	once  sync.Once
	names *idNames
}

// readIDs reads a passwd or group file's names by their IDs, or returns no
// names if it can't.
func (r *Remote) readIDs(file string) map[uint32]string {
	out := map[uint32]string{}
	f, err := r.sftp.Open(file)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, 4<<20))
	if err != nil {
		return out
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 3 || strings.HasPrefix(line, "#") {
			continue
		}
		if id, err := strconv.ParseUint(f[2], 10, 32); err == nil {
			// The first of two names for an ID is the one the system shows.
			if _, dup := out[uint32(id)]; !dup {
				out[uint32(id)] = f[0]
			}
		}
	}
	return out
}

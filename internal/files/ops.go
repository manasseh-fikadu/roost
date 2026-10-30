package files

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// protected are the folders that deleting or moving would take the machine
// down with.
var protected = map[string]bool{}

func init() {
	for p := range strings.FieldsSeq(`
		/ /bin /boot /dev /etc /home /lib /lib32 /lib64 /libx32 /lost+found /media /mnt /opt /proc /root
		/run /sbin /snap /srv /sys /tmp /usr /var /usr/bin /usr/lib /usr/local /usr/sbin /usr/share /var/lib /var/log
		/Applications /Library /System /Users /Volumes /cores /private /private/etc /private/tmp /private/var`) {
		protected[p] = true
	}
}

// guard refuses to touch p if the system depends on it: a folder in
// protected, the user's home, or a folder above it.
func (r *Remote) guard(p, verb string) error {
	resolved, err := r.sftp.RealPath(p)
	if err != nil {
		resolved = p
	}
	home := r.sftp.Home
	if protected[resolved] || protected[p] || resolved == home || strings.HasPrefix(home, resolved+"/") {
		return errorf(http.StatusBadRequest, "Refusing to %s %s: the system depends on it", verb, resolved)
	}
	return nil
}

// The most a shell command may run for.
const (
	copyTimeout   = 30 * time.Minute
	deleteTimeout = 10 * time.Minute
)

// leadingTool matches the name and colon that a tool starts an error with.
var leadingTool = regexp.MustCompile(`^\w+: `)

// run runs a script on the machine, and turns a failure into an Error with
// the first line of what the script said.
func (r *Remote) run(ctx context.Context, timeout time.Duration, what, script string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := r.mgr.Exec(ctx, r.ID, script, args...)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errorf(http.StatusGatewayTimeout, "%s took too long", what)
		}
		return err
	}
	if res.Code != 0 {
		first, _, _ := strings.Cut(strings.TrimSpace(string(res.Stderr)), "\n")
		first = leadingTool.ReplaceAllString(first, "")
		if first == "" {
			first = fmt.Sprintf("%s failed (exit %d)", what, res.Code)
		}
		return errorf(http.StatusBadRequest, "%s", first)
	}
	return nil
}

// ---- one file ----

// Mkdir makes a folder called name in dir, and describes it.
func (r *Remote) Mkdir(dir, name string) (Entry, error) {
	name, err := SafeName(name)
	if err != nil {
		return Entry{}, err
	}
	target := path.Join(dir, name)
	if fi, err := r.lstatIf(target); err != nil {
		return Entry{}, err
	} else if fi != nil {
		return Entry{}, errorf(http.StatusConflict, "%s already exists", name)
	}
	if err := r.sftp.Mkdir(target); err != nil {
		return Entry{}, fsError(err, target)
	}
	return r.Stat(target)
}

// Rename gives what's at p a new name in the same folder, and describes it.
func (r *Remote) Rename(p, name string) (Entry, error) {
	name, err := SafeName(name)
	if err != nil {
		return Entry{}, err
	}
	to := path.Join(path.Dir(p), name)
	if to == p {
		return r.Stat(p)
	}
	if err := r.guard(p, "rename"); err != nil {
		return Entry{}, err
	}
	// A rename that changes only the case, on a disk that ignores it, finds
	// the file itself at the new name.
	if !strings.EqualFold(to, p) {
		if fi, err := r.lstatIf(to); err != nil {
			return Entry{}, err
		} else if fi != nil {
			return Entry{}, errorf(http.StatusConflict, "%s already exists", name)
		}
	}
	if err := r.sftp.Rename(p, to); err != nil {
		return Entry{}, fsError(err, p)
	}
	return r.Stat(to)
}

// ---- several files ----

// A Conflict is what to do when a file is moved, copied or uploaded to
// where there's one with its name already.
type Conflict string

// The ways to settle a conflict.
const (
	// ConflictFail refuses.
	ConflictFail Conflict = "fail"
	// ConflictOverwrite replaces the file. It never replaces a folder.
	ConflictOverwrite Conflict = "overwrite"
	// ConflictRename gives the new file a name that's free, such as "a (1).txt".
	ConflictRename Conflict = "rename"
)

// A Failure is a path that an operation on several couldn't handle, and why.
type Failure struct {
	Path, Error string
}

// A Bulk is what an operation on several paths did. One failing doesn't stop
// the rest.
type Bulk struct {
	Done   int
	Failed []Failure
	// Paths are where the ones that worked ended up, in order.
	Paths []string
}

// maxPaths is the most paths one request can name.
const maxPaths = 10_000

// Paths checks and cleans the paths of a request.
func (r *Remote) Paths(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, errorf(http.StatusBadRequest, "No paths given")
	}
	if len(in) > maxPaths {
		return nil, errorf(http.StatusBadRequest, "Too many items at once")
	}
	out := make([]string, len(in))
	for i, p := range in {
		var err error
		if out[i], err = r.Abs(p); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// bulk runs op for each of paths, up to limit at a time. A lone failure is
// returned as it is, so the caller can show the real reason.
func bulk(paths []string, limit int, op func(p string) (string, error)) (Bulk, error) {
	if len(paths) == 1 {
		to, err := op(paths[0])
		if err != nil {
			return Bulk{}, err
		}
		return Bulk{Done: 1, Paths: []string{to}}, nil
	}
	type outcome struct {
		to  string
		err error
	}
	outcomes := make([]outcome, len(paths))
	parallel(paths, limit, func(i int, p string) {
		outcomes[i].to, outcomes[i].err = op(p)
	})
	out := Bulk{Failed: []Failure{}, Paths: []string{}}
	for i, o := range outcomes {
		if o.err != nil {
			out.Failed = append(out.Failed, Failure{Path: paths[i], Error: o.err.Error()})
			continue
		}
		out.Done++
		out.Paths = append(out.Paths, o.to)
	}
	return out, nil
}

// destination returns where src goes in dest, and whether it replaces a file
// that's there.
func (r *Remote) destination(src, dest string, conflict Conflict) (to string, replace bool, err error) {
	name := path.Base(src)
	to = path.Join(dest, name)
	if to == src && conflict != ConflictRename {
		return "", false, errorf(http.StatusConflict, "%s is already in %s", name, dest)
	}
	current, err := r.lstatIf(to)
	switch {
	case err != nil:
		return "", false, err
	case current == nil:
		return to, false, nil
	case conflict == ConflictRename:
		free, err := r.freeName(dest, name)
		return path.Join(dest, free), false, err
	case conflict == ConflictOverwrite:
		if current.IsDir() {
			return "", false, errorf(http.StatusConflict, "A folder named %s already exists in %s", name, dest)
		}
		return to, true, nil
	default:
		return "", false, errorf(http.StatusConflict, "%s already exists in %s", name, dest)
	}
}

// transfer checks a request to move or copy: dest must be a folder.
func (r *Remote) transfer(dest string) error {
	fi, err := r.sftp.Stat(dest)
	if err != nil {
		return fsError(err, dest)
	}
	if !fi.IsDir() {
		return errorf(http.StatusBadRequest, "%s isn't a folder", dest)
	}
	return nil
}

// intoItself refuses to put a folder inside itself.
func intoItself(src, dest, verb string) error {
	if dest == src || strings.HasPrefix(dest, src+"/") {
		return errorf(http.StatusBadRequest, "Can't %s %s into itself", verb, path.Base(src))
	}
	return nil
}

// Move moves paths into the folder dest. SFTP's rename can't cross file
// systems, so mv does that.
func (r *Remote) Move(ctx context.Context, paths []string, dest string, conflict Conflict) (Bulk, error) {
	if err := r.transfer(dest); err != nil {
		return Bulk{}, err
	}
	return bulk(paths, 1, func(src string) (string, error) {
		if err := r.guard(src, "move"); err != nil {
			return "", err
		}
		if err := intoItself(src, dest, "move"); err != nil {
			return "", err
		}
		to, replace, err := r.destination(src, dest, conflict)
		if err != nil {
			return "", err
		}
		if replace {
			err = r.replace(src, to)
		} else {
			err = r.sftp.Rename(src, to)
		}
		if err == nil {
			return to, nil
		}
		if !failed(err) {
			return "", fsError(err, src)
		}
		args := []string{src, to}
		script := `exec mv -- "$1" "$2"`
		if replace {
			script = `exec mv -f -- "$1" "$2"`
		}
		return to, r.run(ctx, copyTimeout, "Move", script, args...)
	})
}

// replace renames src over the file at to, which SFTP's own rename won't do.
// It's atomic if the machine has OpenSSH's extension for it, and otherwise
// it removes the old file first.
func (r *Remote) replace(src, to string) error {
	if _, ok := r.sftp.HasExtension("posix-rename@openssh.com"); ok {
		return r.sftp.PosixRename(src, to)
	}
	if err := r.sftp.Remove(to); err != nil {
		return err
	}
	return r.sftp.Rename(src, to)
}

// Copy copies paths into the folder dest, with their permissions and times.
func (r *Remote) Copy(ctx context.Context, paths []string, dest string, conflict Conflict) (Bulk, error) {
	if err := r.transfer(dest); err != nil {
		return Bulk{}, err
	}
	return bulk(paths, 1, func(src string) (string, error) {
		if _, err := r.sftp.Lstat(src); err != nil {
			return "", fsError(err, src)
		}
		if err := intoItself(src, dest, "copy"); err != nil {
			return "", err
		}
		to, _, err := r.destination(src, dest, conflict)
		if err != nil {
			return "", err
		}
		return to, r.run(ctx, copyTimeout, "Copy", `exec cp -a -- "$1" "$2"`, src, to)
	})
}

// Delete deletes paths, and what's in the folders among them. A link goes
// without what it points to.
func (r *Remote) Delete(ctx context.Context, paths []string) (Bulk, error) {
	return bulk(paths, 8, func(p string) (string, error) {
		fi, err := r.sftp.Lstat(p)
		if err != nil {
			return "", fsError(err, p)
		}
		if !fi.IsDir() {
			// Unlinking never follows a link.
			return p, fsError(r.sftp.Remove(p), p)
		}
		if err := r.guard(p, "delete"); err != nil {
			return "", err
		}
		if err := r.run(ctx, deleteTimeout, "Delete", `exec rm -rf -- "$1"`, p); err != nil {
			return "", err
		}
		if left, err := r.lstatIf(p); err != nil {
			return "", err
		} else if left != nil {
			return "", errorf(http.StatusBadRequest, "Couldn't delete everything in %s", p)
		}
		return p, nil
	})
}

// ---- search ----

// A Found is what a search found.
type Found struct {
	Root, Query string
	Entries     []Entry
	// Truncated is whether there were more, or the search ran out of time.
	Truncated bool
}

// A Search is what to look for.
type Search struct {
	// Root is where to start, or "" for the home folder.
	Root string
	// Query matches file names without regard to case. It has no slash. A
	// plain word matches names with it anywhere in them, and one with *, ?
	// or [ is a pattern.
	Query string
	// Limit is the most it returns, and Depth the most folders down it looks.
	// They default to 300 and 12.
	Limit, Depth int
}

// The defaults of a search.
const (
	defaultLimit = 300
	defaultDepth = 12
)

// searchTimeout is how long a search may run before it returns what it has.
const searchTimeout = 20 * time.Second

// searchScript prints the paths it finds, each followed by a NUL, so a name
// with a newline stays whole. It skips what holds no files worth finding.
const searchScript = `exec find "$1" -mindepth 1 -maxdepth "$2" ` +
	`\( -path /proc -o -path /sys -o -path /dev -o -path /run -o -name .git -o -name node_modules \) -prune ` +
	`-o -iname "$3" -print0 2>/dev/null`

// Search looks for files by name under a folder.
func (r *Remote) Search(ctx context.Context, s Search) (Found, error) {
	root := s.Root
	if root == "" {
		root = r.sftp.Home
	}
	query := strings.TrimSpace(s.Query)
	switch {
	case query == "":
		return Found{}, errorf(http.StatusBadRequest, "Type something to search for")
	case len(query) > 200 || strings.Contains(query, "/"):
		return Found{}, errorf(http.StatusBadRequest, "Search matches file names, without slashes")
	}
	limit, depth := min(1000, max(1, cmp.Or(s.Limit, defaultLimit))), min(30, max(1, cmp.Or(s.Depth, defaultDepth)))
	pattern := query
	if !strings.ContainsAny(query, "*?[") {
		pattern = "*" + query + "*"
	}
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	st, err := r.mgr.Stream(ctx, r.ID, searchScript, root, strconv.Itoa(depth), pattern)
	if err != nil {
		return Found{}, err
	}
	defer func() { _ = st.Close() }()
	found, more := readPaths(st, limit)
	out := Found{Root: root, Query: query, Entries: make([]Entry, 0, len(found)), Truncated: more || ctx.Err() != nil}
	entries := make([]*Entry, len(found))
	var mu sync.Mutex
	parallel(found, 16, func(i int, p string) {
		if e, err := r.Stat(p); err == nil {
			mu.Lock()
			entries[i] = &e
			mu.Unlock()
		}
	})
	for _, e := range entries {
		if e != nil {
			out.Entries = append(out.Entries, *e)
		}
	}
	return out, nil
}

// readPaths reads NUL-ended paths from rd until it has limit, and reports
// whether there were more.
func readPaths(rd io.Reader, limit int) (paths []string, more bool) {
	var (
		buf  = make([]byte, 64<<10)
		rest []byte
	)
	for {
		n, err := rd.Read(buf)
		rest = append(rest, buf[:n]...)
		for {
			i := bytes.IndexByte(rest, 0)
			if i < 0 {
				break
			}
			if len(paths) == limit {
				return paths, true
			}
			paths = append(paths, string(rest[:i]))
			rest = rest[i+1:]
		}
		if err != nil {
			return paths, false
		}
	}
}

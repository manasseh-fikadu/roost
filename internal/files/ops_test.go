package files_test

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/files"
)

// hostile are file names that shells read as more than one word, as syntax
// or as an option, and that a line-based tool splits in two, unless they're
// handled right.
var hostile = []string{
	"plain.txt",
	"two  spaces and 'quotes'.txt",
	`"double".txt`,
	"it's",
	`back\slash`,
	`trailing\`,
	`\\double`,
	"new\nline",
	"\nleading newline",
	"trailing newline\n",
	"tab\there",
	"esc\x1b[31m.txt",
	"$(touch pwned)",
	"`touch pwned`",
	"$HOME ${HOME}",
	"; touch pwned #",
	"a|b&c>d<e",
	"*",
	"? [a-z] {a,b}",
	"~",
	"-n",
	"-rf",
	"--",
	"!!",
	".hidden",
	"työ ☃ 🐓",
	"@at.txt",
	"bad \xff utf-8",
}

// noPwned fails the test if a command ran a file name as a command.
func noPwned(t *testing.T, m *machine) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(m.home, "pwned")); err == nil {
		t.Error("a command ran a file name as a command")
	}
}

func TestRemote_Move(t *testing.T) {
	t.Parallel()
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t, shell)
			r := m.open(t)
			dest := filepath.Join(m.home, "dest")
			if err := os.Mkdir(dest, 0o700); err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, name := range hostile {
				paths = append(paths, m.write(t, name, "content of "+name))
			}
			res, err := r.Move(t.Context(), paths, dest, files.ConflictFail)
			if err != nil || res.Done != len(hostile) || len(res.Failed) != 0 {
				t.Fatalf("Move = %+v, %v, want all %d moved", res, err, len(hostile))
			}
			for i, name := range hostile {
				got, err := os.ReadFile(filepath.Clean(filepath.Join(dest, name)))
				if err != nil || string(got) != "content of "+name {
					t.Errorf("%q in dest: %q, %v", name, got, err)
				}
				if res.Paths[i] != filepath.Join(dest, name) {
					t.Errorf("Move reported %q for %q", res.Paths[i], name)
				}
				if _, err := os.Lstat(paths[i]); err == nil {
					t.Errorf("%q is still where it was", name)
				}
			}
			noPwned(t, m)
		})
	}
}

func TestRemote_Move_conflicts(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "sh")
	r := m.open(t)
	dest := filepath.Join(m.home, "dest")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	m.write(t, "dest/a.txt", "old")
	m.write(t, "dest/folder/x", "")
	read := func(rel string) string {
		b, _ := os.ReadFile(filepath.Clean(filepath.Join(m.home, rel)))
		return string(b)
	}

	src := m.write(t, "a.txt", "new")
	_, err := r.Move(t.Context(), []string{src}, dest, files.ConflictFail)
	if status(err) != 409 || read("dest/a.txt") != "old" || read("a.txt") != "new" {
		t.Errorf("move onto a file with fail = %v, want a 409 that changes nothing", err)
	}
	res, err := r.Move(t.Context(), []string{src}, dest, files.ConflictRename)
	if err != nil || res.Paths[0] != filepath.Join(dest, "a (1).txt") || read("dest/a (1).txt") != "new" || read("dest/a.txt") != "old" {
		t.Errorf("move with rename = %+v, %v, want a (1).txt", res, err)
	}
	src = m.write(t, "a.txt", "newer")
	res, err = r.Move(t.Context(), []string{src}, dest, files.ConflictOverwrite)
	if err != nil || res.Paths[0] != filepath.Join(dest, "a.txt") || read("dest/a.txt") != "newer" {
		t.Errorf("move with overwrite = %+v, %v, want a.txt replaced", res, err)
	}
	// A folder is never replaced.
	src = m.write(t, "folder", "a file")
	if _, err := r.Move(t.Context(), []string{src}, dest, files.ConflictOverwrite); status(err) != 409 {
		t.Errorf("overwriting a folder = %v, want 409", err)
	}
	// Not into itself, or onto itself.
	if _, err := r.Move(t.Context(), []string{dest}, filepath.Join(dest, "folder"), files.ConflictFail); status(err) != 400 {
		t.Errorf("a folder into itself = %v, want 400", err)
	}
	// Not into a file.
	file := m.write(t, "a file", "")
	if _, err := r.Move(t.Context(), []string{src}, file, files.ConflictFail); status(err) != 400 {
		t.Errorf("into a file = %v, want 400", err)
	}
	// One failing doesn't stop the rest.
	ok := m.write(t, "ok.txt", "")
	res, err = r.Move(t.Context(), []string{filepath.Join(m.home, "missing"), ok}, dest, files.ConflictFail)
	if err != nil || res.Done != 1 || len(res.Failed) != 1 || res.Failed[0].Path != filepath.Join(m.home, "missing") {
		t.Errorf("a missing one among two = %+v, %v, want one done and one failed", res, err)
	}
}

func TestRemote_Copy(t *testing.T) {
	t.Parallel()
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t, shell)
			r := m.open(t)
			dest := filepath.Join(m.home, "dest")
			if err := os.Mkdir(dest, 0o700); err != nil {
				t.Fatal(err)
			}
			var paths []string
			for _, name := range hostile {
				paths = append(paths, m.write(t, "src/"+name, "content of "+name))
				// A folder with the name, holding a file with it.
				m.write(t, "src/dir "+name+"/"+name, "inside "+name)
				paths = append(paths, filepath.Join(m.home, "src", "dir "+name))
			}
			res, err := r.Copy(t.Context(), paths, dest, files.ConflictFail)
			if err != nil || res.Done != len(paths) || len(res.Failed) != 0 {
				t.Fatalf("Copy = %+v, %v, want all %d copied", res, err, len(paths))
			}
			for _, name := range hostile {
				if got, err := os.ReadFile(filepath.Clean(filepath.Join(dest, name))); err != nil || string(got) != "content of "+name {
					t.Errorf("%q in dest: %q, %v", name, got, err)
				}
				if got, err := os.ReadFile(filepath.Clean(filepath.Join(dest, "dir "+name, name))); err != nil || string(got) != "inside "+name {
					t.Errorf("%q in the folder copy: %q, %v", name, got, err)
				}
				// The originals stay.
				if _, err := os.Lstat(filepath.Join(m.home, "src", name)); err != nil {
					t.Errorf("%q is gone from where it was: %v", name, err)
				}
			}
			noPwned(t, m)
			// Copying into the folder it's in makes a free name.
			one := filepath.Join(m.home, "src", "plain.txt")
			res, err = r.Copy(t.Context(), []string{one}, filepath.Join(m.home, "src"), files.ConflictRename)
			if err != nil || res.Paths[0] != filepath.Join(m.home, "src", "plain (1).txt") {
				t.Errorf("copy beside itself = %+v, %v, want plain (1).txt", res, err)
			}
			if _, err := r.Copy(t.Context(), []string{one}, filepath.Join(m.home, "src"), files.ConflictFail); status(err) != 409 {
				t.Errorf("copy onto itself with fail = %v, want 409", err)
			}
		})
	}
}

func TestRemote_Copy_error(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "sh")
	r := m.open(t)
	dest := filepath.Join(m.home, "dest")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	unreadable := m.write(t, "unreadable", "x")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	_, err := r.Copy(t.Context(), []string{unreadable}, dest, files.ConflictFail)
	var e *files.Error
	// The message is cp's, without the tool's name in front.
	if !errors.As(err, &e) || e.Status != 400 || strings.HasPrefix(e.Msg, "cp:") || !strings.Contains(e.Msg, "Permission denied") {
		t.Errorf("copying an unreadable file = %v, want cp's message about permission", err)
	}
}

func TestRemote_Delete(t *testing.T) {
	t.Parallel()
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t, shell)
			r := m.open(t)
			var paths []string
			for _, name := range hostile {
				paths = append(paths, m.write(t, "keep/"+name, ""))
				m.write(t, "tree "+name+"/"+name+"/deep/"+name, "")
				paths = append(paths, filepath.Join(m.home, "tree "+name))
			}
			// A link goes without what it points to.
			target := m.write(t, "target/file", "")
			if err := os.Symlink(filepath.Join(m.home, "target"), filepath.Join(m.home, "link")); err != nil {
				t.Fatal(err)
			}
			paths = append(paths, filepath.Join(m.home, "link"))
			res, err := r.Delete(t.Context(), paths)
			if err != nil || res.Done != len(paths) || len(res.Failed) != 0 {
				t.Fatalf("Delete = %+v, %v, want all %d deleted", res, err, len(paths))
			}
			for _, p := range paths {
				if _, err := os.Lstat(p); err == nil {
					t.Errorf("%q is still there", p)
				}
			}
			if _, err := os.Stat(target); err != nil {
				t.Errorf("deleting the link deleted what it points to: %v", err)
			}
			// What wasn't asked for stays.
			left, _ := os.ReadDir(filepath.Join(m.home, "keep"))
			if len(left) != 0 {
				t.Errorf("%d files left in keep", len(left))
			}
			noPwned(t, m)
		})
	}
}

func TestRemote_Delete_protected(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "sh")
	r := m.open(t)
	for _, p := range []string{"/", "/etc", "/usr/bin", "/tmp", m.home, filepath.Dir(m.home)} {
		_, err := r.Delete(t.Context(), []string{p})
		if status(err) != 400 || !strings.Contains(err.Error(), "the system depends on it") {
			t.Errorf("Delete(%q) = %v, want a refusal", p, err)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s is gone: %v", p, err)
		}
	}
	if _, err := r.Move(t.Context(), []string{"/etc"}, m.home, files.ConflictFail); status(err) != 400 {
		t.Errorf("Move(/etc) = %v, want a refusal", err)
	}
	if _, err := r.Rename("/etc", "etc2"); status(err) != 400 {
		t.Errorf("Rename(/etc) = %v, want a refusal", err)
	}
	// Several at once report each one that was refused.
	ok := m.write(t, "ok", "")
	res, err := r.Delete(t.Context(), []string{"/etc", ok})
	if err != nil || res.Done != 1 || len(res.Failed) != 1 || res.Failed[0].Path != "/etc" {
		t.Errorf("Delete(/etc and a file) = %+v, %v", res, err)
	}
}

func TestRemote_Mkdir_Rename(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "sh")
	r := m.open(t)
	for _, name := range hostile {
		e, err := r.Mkdir(m.home, name)
		if name == "bad \xff utf-8" || err != nil {
			// SFTP carries names as bytes, so even this works.
			if err != nil {
				t.Errorf("Mkdir(%q): %v", name, err)
			}
			continue
		}
		if e.Type != files.TypeDir || e.Name != name {
			t.Errorf("Mkdir(%q) = %+v", name, e)
		}
		renamed, err := r.Rename(e.Path, "renamed "+name)
		if err != nil || renamed.Name != "renamed "+name {
			t.Errorf("Rename(%q) = %+v, %v", name, renamed, err)
		}
		if _, err := os.Stat(filepath.Join(m.home, "renamed "+name)); err != nil {
			t.Error(err)
		}
	}
	if _, err := r.Mkdir(m.home, "renamed plain.txt"); status(err) != 409 {
		t.Errorf("Mkdir of an existing name = %v, want 409", err)
	}
	for _, bad := range []string{"", ".", "..", "a/b", "a\x00b", strings.Repeat("x", 256)} {
		if _, err := r.Mkdir(m.home, bad); status(err) != 400 {
			t.Errorf("Mkdir(%q) = %v, want 400", bad, err)
		}
	}
	a := m.write(t, "a", "")
	m.write(t, "b", "")
	if _, err := r.Rename(a, "b"); status(err) != 409 {
		t.Errorf("Rename onto an existing name = %v, want 409", err)
	}
	if e, err := r.Rename(a, "a"); err != nil || e.Name != "a" {
		t.Errorf("Rename to the same name = %+v, %v, want it left alone", e, err)
	}
}

func TestRemote_Search(t *testing.T) {
	t.Parallel()
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t, shell)
			r := m.open(t)
			var want []string
			for _, name := range hostile {
				if name == "bad \xff utf-8" {
					continue
				}
				want = append(want, m.write(t, "tree/"+name+"/"+name+".needle", ""))
			}
			m.write(t, "tree/.git/needle", "")
			m.write(t, "tree/node_modules/needle", "")
			m.write(t, "tree/deep/a/b/c/d/needle", "")

			found, err := r.Search(t.Context(), files.Search{Root: filepath.Join(m.home, "tree"), Query: "NEEDLE", Limit: 1000, Depth: 5})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, e := range found.Entries {
				got = append(got, e.Path)
			}
			for _, w := range want {
				if !slices.Contains(got, w) {
					t.Errorf("search didn't find %q", w)
				}
			}
			for _, g := range got {
				if strings.Contains(g, "/.git/") || strings.Contains(g, "/node_modules/") {
					t.Errorf("search went into %q", g)
				}
				if strings.Contains(g, "/c/d/") {
					t.Errorf("search went deeper than 5 folders: %q", g)
				}
			}
			noPwned(t, m)

			// A hostile query is a pattern, and nothing else.
			for _, q := range []string{"$(touch pwned)", "`touch pwned`", "; touch pwned", "it's", `back\slash`, "new\nline"} {
				if _, err := r.Search(t.Context(), files.Search{Root: m.home, Query: q}); err != nil {
					t.Errorf("Search(%q): %v", q, err)
				}
			}
			noPwned(t, m)

			// The limit says there were more.
			limited, err := r.Search(t.Context(), files.Search{Root: filepath.Join(m.home, "tree"), Query: "needle", Limit: 3, Depth: 5})
			if err != nil || len(limited.Entries) != 3 || !limited.Truncated {
				t.Errorf("limited search = %d entries, truncated %v, %v; want 3 and truncated", len(limited.Entries), limited.Truncated, err)
			}
		})
	}
}

func TestRemote_Search_patterns(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "sh")
	r := m.open(t)
	m.write(t, "report-final.pdf", "")
	m.write(t, "Report-draft.PDF", "")
	m.write(t, "notes.txt", "")
	names := func(q string) []string {
		found, err := r.Search(t.Context(), files.Search{Root: m.home, Query: q})
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		var out []string
		for _, e := range found.Entries {
			out = append(out, e.Name)
		}
		slices.Sort(out)
		return out
	}
	if got := names("report"); !slices.Equal(got, []string{"Report-draft.PDF", "report-final.pdf"}) {
		t.Errorf("a word = %q", got)
	}
	if got := names("*.pdf"); !slices.Equal(got, []string{"Report-draft.PDF", "report-final.pdf"}) {
		t.Errorf("a pattern = %q", got)
	}
	if got := names("n?tes.txt"); !slices.Equal(got, []string{"notes.txt"}) {
		t.Errorf("a pattern with ? = %q", got)
	}
	for _, bad := range []string{"", "  ", "a/b", strings.Repeat("x", 201)} {
		if _, err := r.Search(t.Context(), files.Search{Root: m.home, Query: bad}); status(err) != 400 {
			t.Errorf("Search(%q) = %v, want 400", bad, err)
		}
	}
}

func TestRemote_Archive(t *testing.T) {
	t.Parallel()
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t, shell)
			r := m.open(t)
			var names []string
			for _, name := range hostile {
				if name == "bad \xff utf-8" {
					// tar can carry it, but a header's name is text.
					continue
				}
				m.write(t, "src/"+name, "content of "+name)
				m.write(t, "src/dir "+name+"/inner", "inside "+name)
				names = append(names, name, "dir "+name)
			}
			dir := filepath.Join(m.home, "src")
			st, name, err := r.Archive(t.Context(), dir, names)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			if want := "src-" + itoa(len(names)) + "-items.tar.gz"; name != want {
				t.Errorf("archive called %q, want %q", name, want)
			}
			got := readTar(t, st)
			if code, stderr := st.Wait(); code != 0 {
				t.Errorf("tar exited %d: %s", code, stderr)
			}
			for _, n := range hostile {
				if n == "bad \xff utf-8" {
					continue
				}
				if got[n] != "content of "+n {
					t.Errorf("%q in the archive: %q", n, got[n])
				}
				if got["dir "+n+"/inner"] != "inside "+n {
					t.Errorf("dir %q in the archive: %q", n, got["dir "+n+"/inner"])
				}
			}
			noPwned(t, m)

			// One name is the archive's, and it keeps its own folder's name.
			st2, name, err := r.Archive(t.Context(), dir, []string{"plain.txt"})
			if err != nil || name != "plain.txt.tar.gz" {
				t.Fatalf("one-file archive = %q, %v", name, err)
			}
			defer func() { _ = st2.Close() }()
			if got := readTar(t, st2); len(got) != 1 || got["plain.txt"] != "content of plain.txt" {
				t.Errorf("one-file archive has %v", got)
			}
		})
	}
}

func TestRemote_Archive_errors(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "sh")
	r := m.open(t)
	m.write(t, "a", "")
	dir := m.home
	for _, tt := range []struct {
		name   string
		names  []string
		status int
	}{
		{"nothing", nil, 400},
		{"a missing file", []string{"a", "missing"}, 404},
		{"a path", []string{"a/../b"}, 400},
		{"dots", []string{".."}, 400},
	} {
		if _, _, err := r.Archive(t.Context(), dir, tt.names); status(err) != tt.status {
			t.Errorf("%s: Archive = %v, want status %d", tt.name, err, tt.status)
		}
	}
	// So many names that the command wouldn't fit.
	var many []string
	for i := range 3000 {
		many = append(many, strings.Repeat("n", 100)+itoa(i))
	}
	if _, _, err := r.Archive(t.Context(), dir, many); status(err) != 400 {
		t.Errorf("a huge archive = %v, want 400", err)
	}
}

func readTar(t *testing.T, r io.Reader) map[string]string {
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
		if h.Typeflag != tar.TypeReg {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		// A name that tar might read as a command comes in as ./name.
		out[strings.TrimPrefix(h.Name, "./")] = string(b)
	}
}

// status returns the HTTP status of a files.Error, or 0.
func status(err error) int {
	if e, ok := errors.AsType[*files.Error](err); ok {
		return e.Status
	}
	return 0
}

func itoa(n int) string { return strconv.Itoa(n) }

// A folder can be called anything too: the one a file goes to, is searched,
// archived or deleted, and whatever the shell does with its name, the files
// in it are the ones asked for.
func TestRemote_hostileFolders(t *testing.T) {
	t.Parallel()
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			m := newMachine(t, shell)
			r := m.open(t)
			for _, name := range hostile {
				if name == "bad \xff utf-8" {
					continue
				}
				dir := filepath.Join(m.home, "box "+name)
				m.write(t, "box "+name+"/inside.needle", "inside")
				src := m.write(t, "src "+name+"/file.txt", "from "+name)

				// Copy and move into it, and out of it.
				if res, err := r.Copy(t.Context(), []string{src}, dir, files.ConflictFail); err != nil || res.Done != 1 {
					t.Fatalf("Copy into %q = %+v, %v", name, res, err)
				}
				if got, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "file.txt"))); err != nil || string(got) != "from "+name {
					t.Errorf("the copy in %q: %q, %v", name, got, err)
				}
				moved := filepath.Join(dir, "file.txt")
				back := filepath.Join(m.home, "back "+name)
				if err := os.Mkdir(back, 0o700); err != nil {
					t.Fatal(err)
				}
				if res, err := r.Move(t.Context(), []string{moved}, back, files.ConflictFail); err != nil || res.Done != 1 {
					t.Fatalf("Move out of %q = %+v, %v", name, res, err)
				}

				// Search it, archive from it.
				found, err := r.Search(t.Context(), files.Search{Root: dir, Query: "needle"})
				if err != nil || len(found.Entries) != 1 || found.Entries[0].Path != filepath.Join(dir, "inside.needle") {
					t.Errorf("Search in %q = %+v, %v", name, found.Entries, err)
				}
				st, _, err := r.Archive(t.Context(), dir, []string{"inside.needle"})
				if err != nil {
					t.Fatalf("Archive from %q: %v", name, err)
				}
				if got := readTar(t, st); got["inside.needle"] != "inside" {
					t.Errorf("archive from %q has %v", name, got)
				}
				_ = st.Close()

				// Delete it.
				if res, err := r.Delete(t.Context(), []string{dir, back}); err != nil || res.Done != 2 {
					t.Errorf("Delete of %q = %+v, %v", name, res, err)
				}
				if _, err := os.Lstat(dir); err == nil {
					t.Errorf("%q is still there", dir)
				}
			}
			noPwned(t, m)
		})
	}
}

// A link to a folder is the link, to everything but reading and writing the
// file that a path ends at: deleting, moving, copying, renaming, archiving and
// searching never reach through it, so a link that someone else put in a
// folder can't send the admin's delete, or a download, somewhere else.
func TestRemote_symlinks(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "sh")
	r := m.open(t)
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	precious := filepath.Join(outside, "precious.txt")
	if err := os.WriteFile(precious, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(m.home, "link")
	dest := filepath.Join(m.home, "dest")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	relink := func() {
		_ = os.Remove(link)
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
	}
	isLinkTo := func(p string) bool {
		got, err := os.Readlink(p)
		return err == nil && got == outside
	}
	intact := func(what string) {
		t.Helper()
		if b, err := os.ReadFile(filepath.Clean(precious)); err != nil || string(b) != "keep" {
			t.Errorf("after %s the file behind the link is %q, %v", what, b, err)
		}
	}

	relink()
	if res, err := r.Delete(t.Context(), []string{link}); err != nil || res.Done != 1 {
		t.Fatalf("Delete = %+v, %v", res, err)
	}
	intact("deleting the link")
	if _, err := os.Lstat(link); err == nil {
		t.Error("the link is still there")
	}

	relink()
	if res, err := r.Copy(t.Context(), []string{link}, dest, files.ConflictFail); err != nil || res.Done != 1 {
		t.Fatalf("Copy = %+v, %v", res, err)
	}
	if !isLinkTo(filepath.Join(dest, "link")) {
		t.Error("the copy of the link isn't a link to the same place")
	}
	if res, err := r.Move(t.Context(), []string{filepath.Join(dest, "link")}, m.home, files.ConflictRename); err != nil || res.Done != 1 {
		t.Fatalf("Move = %+v, %v", res, err)
	}
	if !isLinkTo(filepath.Join(m.home, "link (1)")) {
		t.Error("the moved link isn't a link to the same place")
	}
	if _, err := r.Rename(link, "renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if !isLinkTo(filepath.Join(m.home, "renamed")) {
		t.Error("the renamed link isn't a link to the same place")
	}
	intact("moving, copying and renaming the link")

	relink()
	st, _, err := r.Archive(t.Context(), m.home, []string{"link"})
	if err != nil {
		t.Fatal(err)
	}
	if got := readTar(t, st); len(got) != 0 {
		t.Errorf("the archive of a link has files in it: %v", got)
	}
	_ = st.Close()

	found, err := r.Search(t.Context(), files.Search{Root: m.home, Query: "precious"})
	if err != nil || len(found.Entries) != 0 {
		t.Errorf("Search went through the link: %+v, %v", found.Entries, err)
	}

	// A link in a folder that is deleted goes without what it points to.
	relink()
	holder := filepath.Join(m.home, "holder")
	if err := os.Mkdir(holder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(holder, "inner")); err != nil {
		t.Fatal(err)
	}
	if res, err := r.Delete(t.Context(), []string{holder}); err != nil || res.Done != 1 {
		t.Fatalf("Delete of a folder with a link = %+v, %v", res, err)
	}
	intact("deleting a folder that has a link in it")
}

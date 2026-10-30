package files_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/files"
)

func TestRemote_List(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "")
	m.write(t, "notes.txt", "hello")
	m.write(t, "docs/a.md", "# a")
	if err := os.Symlink("docs", filepath.Join(m.home, "link-to-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("notes.txt", filepath.Join(m.home, "link-to-file")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nowhere", filepath.Join(m.home, "broken")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(m.home, "notes.txt"), os.ModeSetuid|0o755); err != nil {
		t.Fatal(err)
	}
	r := m.open(t)

	got, err := r.List("")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != m.home || got.Home != m.home || got.Parent != filepath.Dir(m.home) {
		t.Errorf("List(\"\") = %q in %q under %q, want the home folder %q", got.Path, got.Home, got.Parent, m.home)
	}
	if got.Space == nil || got.Space.Total == 0 {
		t.Errorf("space = %+v, want the file system's", got.Space)
	}
	by := map[string]files.Entry{}
	for _, e := range got.Entries {
		by[e.Name] = e
	}
	if len(by) != 5 {
		t.Errorf("entries = %v, want 5", slices.Collect(func(yield func(string) bool) {
			for n := range by {
				if !yield(n) {
					return
				}
			}
		}))
	}
	if e := by["notes.txt"]; e.Type != files.TypeFile || e.Size != 5 || e.Mode != "-rwsr-xr-x" || e.Path != filepath.Join(m.home, "notes.txt") {
		t.Errorf("notes.txt = %+v", e)
	}
	if e := by["docs"]; e.Type != files.TypeDir || e.Mode != "drwx------" && e.Mode != "drwxr-xr-x" {
		t.Errorf("docs = %+v", e)
	}
	if e := by["link-to-dir"]; e.Type != files.TypeSymlink || e.TargetType != files.TypeDir || e.Target != "docs" {
		t.Errorf("link-to-dir = %+v, want a link to a folder", e)
	}
	if e := by["link-to-file"]; e.Type != files.TypeSymlink || e.TargetType != files.TypeFile || e.Size != 5 {
		t.Errorf("link-to-file = %+v, want a link to a 5 byte file", e)
	}
	if e := by["broken"]; e.Type != files.TypeSymlink || e.TargetType != "" || e.Target != "nowhere" {
		t.Errorf("broken = %+v, want a link to nothing", e)
	}
	// The owner is the test's own user, by name if the machine has one.
	if e := by["notes.txt"]; e.Owner == "" || e.Group == "" {
		t.Errorf("notes.txt owner %q, group %q, want names or numbers", e.Owner, e.Group)
	}

	// Through a link, to the folder it points at. (OpenSSH resolves the link
	// in the path; the test server leaves it.)
	sub, err := r.List(filepath.Join(m.home, "link-to-dir"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Entries) != 1 || sub.Entries[0].Name != "a.md" {
		t.Errorf("list through a link has %d entries, want a.md", len(sub.Entries))
	}
	if p, err := r.Abs("~/docs/../notes.txt"); err != nil || p != filepath.Join(m.home, "notes.txt") {
		t.Errorf("Abs = %q, %v, want the file in the home folder", p, err)
	}
}

func TestRemote_List_errors(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "")
	m.write(t, "file.txt", "x")
	r := m.open(t)
	for _, tt := range []struct {
		name, path string
		status     int
	}{
		{"missing", filepath.Join(m.home, "nope"), 404},
		{"a file", filepath.Join(m.home, "file.txt"), 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.List(tt.path)
			var e *files.Error
			if !errors.As(err, &e) || e.Status != tt.status {
				t.Errorf("List(%q) = %v, want an error with status %d", tt.path, err, tt.status)
			}
		})
	}
}

func TestRemote_Stat(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "")
	p := m.write(t, "a.txt", "abc")
	r := m.open(t)
	e, err := r.Stat(p)
	if err != nil || e.Name != "a.txt" || e.Size != 3 || e.Type != files.TypeFile {
		t.Errorf("Stat = %+v, %v", e, err)
	}
	if _, err := r.Stat(filepath.Join(m.home, "missing")); err == nil {
		t.Error("Stat of a missing file succeeded")
	}
}

func TestRemote_Exists(t *testing.T) {
	t.Parallel()
	m := newMachine(t, "")
	m.write(t, "a.txt", "")
	m.write(t, "photos/2024/b.jpg", "")
	r := m.open(t)
	got, err := r.Exists(m.home, []string{"a.txt", "c.txt", "photos/2024/b.jpg", "photos/2024/x.jpg"})
	if err != nil || !slices.Equal(got, []string{"a.txt", "photos/2024/b.jpg"}) {
		t.Errorf("Exists = %q, %v, want a.txt and photos/2024/b.jpg", got, err)
	}
	if _, err := r.Exists(m.home, []string{"../x"}); err == nil {
		t.Error("Exists accepted a name that leaves the folder")
	}
}

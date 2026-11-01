package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/store"
)

// entry is a file in an archive.
type entry struct {
	body string
	mode fs.FileMode
}

// readArchiveFile returns what is in the .tar.gz at path, by name.
func readArchiveFile(t *testing.T, path string) map[string]entry {
	t.Helper()
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	return readArchive(t, f)
}

// readArchive returns what is in a .tar.gz, by name.
func readArchive(t *testing.T, r io.Reader) map[string]entry {
	t.Helper()
	gz, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]entry{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Uid != 0 || h.Gid != 0 || h.Uname != "" {
			t.Errorf("%s is owned by %d:%d (%q), want no owner", h.Name, h.Uid, h.Gid, h.Uname)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		got[h.Name] = entry{string(body), fs.FileMode(h.Mode).Perm()} //nolint:gosec // G115: tar modes are small.
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBackup(t *testing.T) {
	t.Parallel()
	data := filepath.Join(t.TempDir(), "data")
	// Bield is running: the store stays open, and its -wal file exists.
	st, err := store.Open(t.Context(), filepath.Join(data, dbName), store.Options{NoPrune: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.SetRole(t.Context(), "alice@example.com", store.Admin); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(data, "ssh", "id_ed25519"), "private key")
	writeFile(t, filepath.Join(data, "tailscale", "tailscaled.state"), "node key")
	writeFile(t, filepath.Join(data, "tailscale", "certs", "cert.pem"), "certificate")
	writeFile(t, filepath.Join(data, dbName+".bak"), "older database")
	writeFile(t, filepath.Join(data, "backups", "before-upgrade.tar.gz"), "older backup")
	out := filepath.Join(t.TempDir(), "saved.tar.gz")
	var stdout, stderr bytes.Buffer

	err = run(t.Context(), []string{"backup", "--data-dir", data, out}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("backup error = %v, want none", err)
	}
	if !strings.HasPrefix(stdout.String(), "Saved "+out+" (") || !strings.Contains(stderr.String(), "keep it private") {
		t.Errorf("backup said %q and %q, want where it saved the file and a warning", stdout.String(), stderr.String())
	}
	if info, err := os.Stat(out); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("backup file: Stat = %v, %v, want mode 0600", info, err)
	}
	got := readArchiveFile(t, out)
	names := make([]string, 0, len(got))
	for name := range got {
		names = append(names, name)
	}
	slices.Sort(names)
	want := []string{"bield.db", "ssh/", "ssh/id_ed25519", "tailscale/", "tailscale/certs/", "tailscale/certs/cert.pem", "tailscale/tailscaled.state"}
	if !slices.Equal(names, want) {
		t.Errorf("archive has %q, want %q", names, want)
	}
	if e := got["ssh/id_ed25519"]; e.body != "private key" || e.mode != 0o600 {
		t.Errorf("ssh/id_ed25519 = %+v, want its content and mode 0600", e)
	}

	// The database in the archive opens, and is what the live one held.
	restored := filepath.Join(t.TempDir(), dbName)
	writeFile(t, restored, got[dbName].body)
	copied, err := store.Open(t.Context(), restored, store.Options{NoPrune: true})
	if err != nil {
		t.Fatalf("open the database from the archive: %v", err)
	}
	t.Cleanup(func() { _ = copied.Close() })
	users, err := copied.Users(t.Context())
	if err != nil || len(users) != 1 || users[0].Login != "alice@example.com" {
		t.Errorf("users in the archived database = %+v, %v, want alice", users, err)
	}
}

func TestBackup_standardOutput(t *testing.T) {
	t.Parallel()
	data := filepath.Join(t.TempDir(), "data")
	st, err := store.Open(t.Context(), filepath.Join(data, dbName), store.Options{NoPrune: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	writeFile(t, filepath.Join(data, "ssh", "id_ed25519"), "private key")
	var stdout, stderr bytes.Buffer

	err = run(t.Context(), []string{"backup", "--data-dir", data, "-"}, &stdout, &stderr)

	if err != nil {
		t.Fatalf("backup - error = %v, want none", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("backup - wrote %q to stderr, which a pipe would show next to the archive", stderr.String())
	}
	got := readArchive(t, &stdout)
	if _, ok := got[dbName]; !ok || got["ssh/id_ed25519"].body != "private key" {
		t.Errorf("archive on stdout has %v, want the database and the key", got)
	}
	// The copy of the database was made in the data directory, and is gone.
	entries, err := os.ReadDir(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".bield-backup") {
			t.Errorf("backup left %s behind", e.Name())
		}
	}
}

func TestBackup_makesItsDirectory(t *testing.T) {
	t.Parallel()
	data := filepath.Join(t.TempDir(), "data")
	st, err := store.Open(t.Context(), filepath.Join(data, dbName), store.Options{NoPrune: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	out := filepath.Join(data, "backups", "bield.tar.gz")

	if err := run(t.Context(), []string{"backup", "--data-dir", data, out}, io.Discard, io.Discard); err != nil {
		t.Fatalf("backup error = %v, want none", err)
	}

	if info, err := os.Stat(filepath.Dir(out)); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("backups directory: Stat = %v, %v, want mode 0700", info, err)
	}
	if _, ok := readArchiveFile(t, out)[dbName]; !ok {
		t.Error("the backup in the new directory has no database")
	}
}

func TestBackup_failures(t *testing.T) {
	t.Parallel()
	t.Run("does not replace a file", func(t *testing.T) {
		t.Parallel()
		data := filepath.Join(t.TempDir(), "data")
		st, err := store.Open(t.Context(), filepath.Join(data, dbName), store.Options{NoPrune: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = st.Close() }()
		// Naming the live database by mistake must leave it alone.
		live := filepath.Join(data, dbName)
		before, err := os.ReadFile(filepath.Clean(live))
		if err != nil {
			t.Fatal(err)
		}

		err = run(t.Context(), []string{"backup", "--data-dir", data, live}, io.Discard, io.Discard)

		if !errors.Is(err, fs.ErrExist) {
			t.Errorf("backup over %s error = %v, want one for an existing file", live, err)
		}
		if after, _ := os.ReadFile(filepath.Clean(live)); !bytes.Equal(before, after) {
			t.Error("backup changed the live database")
		}
	})
	t.Run("no database", func(t *testing.T) {
		t.Parallel()
		data := t.TempDir()
		out := filepath.Join(t.TempDir(), "saved.tar.gz")

		err := run(t.Context(), []string{"backup", "--data-dir", data, out}, io.Discard, io.Discard)

		if err == nil || !strings.Contains(err.Error(), "no database to copy") {
			t.Errorf("backup of an empty directory error = %v, want no database", err)
		}
		if _, err := os.Stat(out); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("a failed backup left %s: Stat error = %v", out, err)
		}
		if _, err := os.Stat(filepath.Join(data, dbName)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("backup made a database: Stat error = %v", err)
		}
	})
	t.Run("too many arguments", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer

		err := run(t.Context(), []string{"backup", "a", "b"}, io.Discard, &stderr)

		if !errors.Is(err, errUsage) || !strings.Contains(stderr.String(), "Usage: bield backup") {
			t.Errorf("backup a b: error = %v, stderr = %q, want usage", err, stderr.String())
		}
	})
}

func TestBackup_insideTheDataDirectory(t *testing.T) {
	t.Parallel()
	data := filepath.Join(t.TempDir(), "data")
	st, err := store.Open(t.Context(), filepath.Join(data, dbName), store.Options{NoPrune: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	writeFile(t, filepath.Join(data, "ssh", "id_ed25519"), "private key")
	out := filepath.Join(data, "saved.tar.gz")

	if err := run(t.Context(), []string{"backup", "--data-dir", data, out}, io.Discard, io.Discard); err != nil {
		t.Fatalf("backup error = %v, want none", err)
	}

	for name := range readArchiveFile(t, out) {
		if strings.Contains(name, "saved") || strings.Contains(name, ".bield-backup") {
			t.Errorf("archive contains %s, which is the backup itself", name)
		}
	}
	// Nothing is left beside it.
	entries, err := os.ReadDir(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".bield-backup") {
			t.Errorf("backup left %s behind", e.Name())
		}
	}
}

func TestByteCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0 B"}, {999, "999 B"}, {1000, "1.0 kB"}, {1500, "1.5 kB"}, {3_400_000, "3.4 MB"}, {2_000_000_000, "2.0 GB"},
	}
	for _, tt := range tests {
		if got := byteCount(tt.n); got != tt.want {
			t.Errorf("byteCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

package hostmetrics_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/hostmetrics"
)

func TestLocal_readsUnderTheRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "proc", "loadavg"), []byte("0.50 0.25 0.10 2/377 12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	sys := hostmetrics.Local(root)

	got, err := fs.ReadFile(sys.FS, "proc/loadavg")
	if err != nil {
		t.Fatalf("read proc/loadavg under %s: %v", root, err)
	}
	if want := "0.50 0.25 0.10 2/377 12345\n"; string(got) != want {
		t.Errorf("proc/loadavg = %q, want %q", got, want)
	}
	if _, err := fs.ReadFile(sys.FS, "proc/stat"); err == nil {
		t.Error("read proc/stat, which the root doesn't have, so Local isn't confined to the root")
	}
}

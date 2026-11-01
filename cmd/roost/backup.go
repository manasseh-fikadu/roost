package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/manasseh-fikadu/roost/internal/config"
	"github.com/manasseh-fikadu/roost/internal/store"
)

const backupUsage = `Usage: roost backup [--data-dir directory] [file]

Saves Roost's data to a .tar.gz file, named roost-backup-<date>.tar.gz in the
current directory unless you give another. With "-" instead of a file name, it
writes the archive to standard output. It works while Roost is running. The
archive holds the database, Roost's Tailscale node and its SSH key, so keep it
private. A file is never replaced if it exists.

To restore it, stop Roost, extract the file into the data directory, and start
Roost. See docs/upgrading.md.
`

// dbName is the database's file name in the data directory, and in a backup.
const dbName = "roost.db"

// backup runs "roost backup".
func backup(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("roost backup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, backupUsage) }
	dataDir := envDataDir()
	fs.StringVar(&dataDir, "data-dir", dataDir, "`directory` that holds Roost's data (env ROOST_DATA_DIR)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		fs.Usage()
		return errUsage
	}
	if dataDir == "" {
		return errors.New("no data directory: set --data-dir or ROOST_DATA_DIR")
	}
	dst := fs.Arg(0)
	if dst == "-" {
		return writeBackup(ctx, dataDir, "", stdout)
	}
	if dst == "" {
		dst = "roost-backup-" + time.Now().Format("20060102-150405") + ".tar.gz"
	}
	size, err := writeBackupFile(ctx, dataDir, dst)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Saved %s (%s)\n", dst, byteCount(size))
	fmt.Fprintln(stderr, "It holds Roost's keys, so keep it private.")
	return nil
}

// writeBackupFile writes the backup to a new file at dst, and returns its size.
// If it fails, it leaves no file at dst.
func writeBackupFile(ctx context.Context, dataDir, dst string) (size int64, err error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return 0, err
	}
	// O_EXCL keeps it from replacing a file, such as the live database by a typo.
	out, err := os.OpenFile(filepath.Clean(dst), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.Remove(dst))
		}
	}()
	defer func() { err = errors.Join(err, out.Close()) }()
	if err := writeBackup(ctx, dataDir, dst, out); err != nil {
		return 0, err
	}
	if err := out.Sync(); err != nil {
		return 0, err
	}
	info, err := out.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// writeBackup writes the data directory as a .tar.gz to out. The database goes
// in as a copy made beside dst, or in the data directory if dst is "" because
// out isn't a file, so a big database doesn't fill a small /tmp. dst, if it's
// inside the data directory, is left out of the archive.
func writeBackup(ctx context.Context, dataDir, dst string, out io.Writer) (err error) {
	// A symlinked data directory is the directory it points to.
	root, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		return fmt.Errorf("open the data directory: %w", err)
	}
	snapDir := root
	if dst != "" {
		snapDir = filepath.Dir(dst)
	}
	snap, err := os.CreateTemp(snapDir, ".roost-backup-*.db")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.Remove(snap.Name())) }()
	if err := snap.Close(); err != nil {
		return err
	}
	dbPath := filepath.Join(root, dbName)
	if err := store.Snapshot(ctx, dbPath, snap.Name()); err != nil {
		return fmt.Errorf("copy %s: %w", dbPath, err)
	}

	// The archive is not part of itself, nor is the copy of the database.
	var skip []os.FileInfo
	for _, name := range []string{dst, snap.Name()} {
		if info, err := os.Stat(name); err == nil {
			skip = append(skip, info)
		}
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	if err := addFile(tw, snap.Name(), dbName); err != nil {
		return err
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// The database goes in from its copy. Its other files are the previous
		// backup and the log SQLite keeps beside it. Backups that the installer
		// made would otherwise go into the next one.
		if top, _, _ := strings.Cut(filepath.ToSlash(rel), "/"); strings.HasPrefix(top, dbName) || top == "backups" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil && slices.ContainsFunc(skip, func(s os.FileInfo) bool { return os.SameFile(info, s) }) {
			return nil
		}
		return addFile(tw, path, filepath.ToSlash(rel))
	})
	if err != nil {
		return fmt.Errorf("read %s: %w", root, err)
	}
	return errors.Join(tw.Close(), gz.Close())
}

// addFile adds the file, directory or symlink at path to the archive as name.
// It skips anything else, such as a socket. Files are owned by no one in the
// archive, so extracting it gives them to whoever does.
func addFile(tw *tar.Writer, path, name string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	var link string
	if info.Mode()&fs.ModeSymlink != 0 {
		if link, err = os.Readlink(path); err != nil {
			return err
		}
	} else if !info.Mode().IsRegular() && !info.IsDir() {
		return nil
	}
	h, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	h.Name, h.Uid, h.Gid, h.Uname, h.Gname = name, 0, 0, "", ""
	if info.IsDir() {
		h.Name += "/"
	}
	if err := tw.WriteHeader(h); err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(tw, f)
	return err
}

// byteCount formats a size, such as "3.4 MB".
func byteCount(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}

// envDataDir returns the data directory ROOST_DATA_DIR names, or the default.
func envDataDir() string {
	if dir := os.Getenv("ROOST_DATA_DIR"); dir != "" {
		return dir
	}
	return config.DefaultDataDir()
}

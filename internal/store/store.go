// Package store keeps Roost's data in a SQLite database.
//
// The schema is built by the numbered SQL files in migrations/, which are
// embedded in the binary. To change it, add the next file, such as
// 0002_device_settings.sql, and never edit one that has been merged. Open
// applies the files a database hasn't had yet, all in one transaction, and
// records the number of the last one in PRAGMA user_version.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// ErrNewerSchema means the database was migrated by a newer version of Roost,
// so this one doesn't know its schema.
var ErrNewerSchema = errors.New("the database was written by a newer version of Roost")

// busyTimeout is how long a statement waits for another connection to finish
// writing before it fails with SQLITE_BUSY.
const busyTimeout = 5 * time.Second

//go:embed migrations/*.sql
var embedded embed.FS

// Store is an open Roost database. It's safe for concurrent use.
type Store struct {
	db  *sql.DB
	log *slog.Logger

	stopPruning context.CancelFunc
	pruned      chan struct{} // closed when the pruner has stopped
	// retention says how long to keep history, or is nil for the defaults.
	// retentionErr is the latest error it returned, so it's logged once. Only
	// the pruner touches it.
	retention    func(ctx context.Context, s *Store) (Retention, error)
	retentionErr error
}

// Options configures Open.
type Options struct {
	// Logger receives migration progress and pruning errors. Nil discards them.
	Logger *slog.Logger
	// Retention says how long to keep history, each time the store prunes. It
	// is given the store to read the setting from. If it returns an error,
	// the store keeps everything until it works. Nil keeps the defaults of
	// DefaultRetention.
	Retention func(ctx context.Context, s *Store) (Retention, error)
	// NoPrune stops the store summing up and deleting history, for a command that opens it
	// for something else, since it doesn't know how long an admin chose to
	// keep it.
	NoPrune bool
}

// Open opens the SQLite database at path, creating it and its directory if they
// don't exist, and migrates it to the latest schema. Before migrating a database
// that already has a schema, it copies it to path + ".bak".
//
// Until Close is called, the store sums up metrics and round-trip times
// into buckets every few minutes, and deletes expired history once an hour,
// unless opts.NoPrune says not to.
func Open(ctx context.Context, path string, opts Options) (*Store, error) {
	return open(ctx, path, opts, migrationFiles())
}

func open(ctx context.Context, path string, opts Options, files fs.FS) (*Store, error) {
	migrations, err := loadMigrations(files)
	if err != nil {
		return nil, err
	}
	if err := create(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, log: opts.Logger, pruned: make(chan struct{}), retention: opts.Retention}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if err := s.migrate(ctx, path, migrations); err != nil {
		return nil, errors.Join(err, db.Close())
	}

	// Pruning runs until Close, even if ctx was only for opening.
	var pruneCtx context.Context
	pruneCtx, s.stopPruning = context.WithCancel(context.WithoutCancel(ctx))
	if opts.NoPrune {
		close(s.pruned)
		return s, nil
	}
	go func() {
		defer close(s.pruned)
		s.maintain(pruneCtx)
	}()
	return s, nil
}

// Close stops pruning and closes the database.
func (s *Store) Close() error {
	s.stopPruning()
	<-s.pruned
	return s.db.Close()
}

// create makes the database's directory and file if they don't exist. Only this
// user can read them, since the database holds the hub's secrets, and SQLite
// gives the -wal and -shm files the same mode as the database.
func create(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

// dsn returns the driver's data source name for the database at path. The
// settings in it apply to every connection in the pool.
func dsn(path string) string {
	q := url.Values{}
	q.Set("_busy_timeout", strconv.FormatInt(busyTimeout.Milliseconds(), 10))
	q.Set("_journal_mode", "WAL")
	// In WAL mode, NORMAL can lose the last commits in a power cut, but it can't
	// corrupt the database.
	q.Set("_synchronous", "NORMAL")
	q.Set("_foreign_keys", "1")
	// Take the write lock when a transaction begins, not at its first write. A
	// transaction that reads and then writes otherwise fails at once with
	// SQLITE_BUSY, without waiting out the busy timeout, if another connection
	// wrote in between.
	q.Set("_txlock", "immediate")
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()
}

// A migration is one numbered file in migrations/.
type migration struct {
	version int
	name    string
	sql     string
}

var migrationName = regexp.MustCompile(`^(\d{4})_[a-z0-9_]+\.sql$`)

func migrationFiles() fs.FS {
	files, err := fs.Sub(embedded, "migrations")
	if err != nil {
		panic(err) // fs.Sub only fails for an invalid directory name
	}
	return files
}

// loadMigrations reads the migrations in the root of files. Their names must be
// numbered from 0001 with no gaps, such as 0001_kv_and_events.sql.
func loadMigrations(files fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	migrations := make([]migration, 0, len(entries))
	for i, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migration %s: name isn't like 0001_create_things.sql", e.Name())
		}
		// ReadDir sorts by name, so a gap or a repeated number shows up here.
		version, _ := strconv.Atoi(m[1])
		if version != i+1 {
			return nil, fmt.Errorf("migration %s: expected number %04d next", e.Name(), i+1)
		}
		body, err := fs.ReadFile(files, e.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", e.Name(), err)
		}
		migrations = append(migrations, migration{version: version, name: e.Name(), sql: string(body)})
	}
	return migrations, nil
}

// migrate applies the migrations the database hasn't had yet, after backing it
// up if it already has a schema.
func (s *Store) migrate(ctx context.Context, path string, migrations []migration) error {
	var from int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&from); err != nil {
		return err
	}
	to := len(migrations)
	switch {
	case from > to:
		return fmt.Errorf("%w (schema version %d, but this one only knows up to %d)", ErrNewerSchema, from, to)
	case from == to:
		return nil
	case from > 0:
		backup := path + ".bak"
		kept, err := s.backup(ctx, backup, from)
		if err != nil {
			return fmt.Errorf("back up the database before migrating: %w", err)
		}
		if kept {
			s.log.Info("backed up the database before migrating", "backup", backup, "version", from)
		}
	}

	applied, err := s.upgrade(ctx, migrations[from:])
	if err != nil {
		return err
	}
	if applied > 0 {
		s.log.Info("migrated the database", "from", from, "to", to)
	}
	return nil
}

// upgrade applies migrations in one transaction and returns how many it
// applied, so either all of them apply or none do. The transaction takes the
// write lock as it begins and reads the version again, so it skips migrations
// that another process, such as roost admin, applied since migrate read it, and
// no other process sees the database halfway through.
func (s *Store) upgrade(ctx context.Context, migrations []migration) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	// After a successful Commit, Rollback does nothing.
	defer func() { _ = tx.Rollback() }()

	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, err
	}
	n := 0
	for _, m := range migrations {
		if m.version <= version {
			continue
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			return 0, fmt.Errorf("migration %s: %w", m.name, err)
		}
		// PRAGMA can't take a bound parameter, and the version is a number.
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(m.version)); err != nil {
			return 0, fmt.Errorf("migration %s: %w", m.name, err)
		}
		n++
	}
	if n == 0 {
		return 0, nil
	}
	return n, tx.Commit()
}

// backup writes a consistent copy of the database, as it is at version from,
// to dst, and reports whether it did. It writes the copy next to dst first, so
// a failed backup leaves the previous one in place. The temporary file's name
// is unique, so two processes can back up at once. If another process migrated
// the database before the copy was taken, the copy is dropped, since that
// process already backed up the database as it was.
func (s *Store) backup(ctx context.Context, dst string, from int) (bool, error) {
	// VACUUM INTO needs a missing or empty file, and keeps the mode of one that
	// exists. CreateTemp makes an empty one that only this user can read.
	f, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".*.tmp")
	if err != nil {
		return false, err
	}
	tmp := f.Name()
	if err := f.Close(); err != nil {
		return false, errors.Join(err, os.Remove(tmp))
	}
	if err := s.vacuumInto(ctx, tmp); err != nil {
		return false, errors.Join(err, os.Remove(tmp))
	}
	version, err := fileUserVersion(tmp)
	switch {
	case err != nil:
		return false, errors.Join(err, os.Remove(tmp))
	case version != from:
		return false, os.Remove(tmp)
	}
	return true, os.Rename(tmp, dst)
}

// fileUserVersion reads PRAGMA user_version from the header of the SQLite
// database at path, where it's a big-endian integer at offset 60.
func fileUserVersion(path string) (int, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	var header [64]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return 0, fmt.Errorf("read the header of %s: %w", path, err)
	}
	return int(int32(binary.BigEndian.Uint32(header[60:]))), nil //nolint:gosec // G115: SQLite stores a signed 32-bit integer.
}

// vacuumInto copies the database to the empty file at path and syncs it, which
// VACUUM INTO doesn't do on its own.
func (s *Store) vacuumInto(ctx context.Context, path string) error {
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_RDWR, 0)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

// Package store keeps Roost's data in a SQLite database.
//
// The schema is built by the numbered SQL files in migrations/, which are
// embedded in the binary. To change it, add the next file, such as
// 0002_device_settings.sql, and never edit one that has been merged. Open
// applies the files a database hasn't had yet, each in its own transaction, and
// records the number of the last one in PRAGMA user_version.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
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
}

// Options configures Open.
type Options struct {
	// Logger receives migration progress and pruning errors. Nil discards them.
	Logger *slog.Logger
}

// Open opens the SQLite database at path, creating it and its directory if they
// don't exist, and migrates it to the latest schema. Before migrating a database
// that already has a schema, it copies it to path + ".bak".
//
// Until Close is called, the store deletes expired history once an hour.
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
	s := &Store{db: db, log: opts.Logger, pruned: make(chan struct{})}
	if s.log == nil {
		s.log = slog.New(slog.DiscardHandler)
	}
	if err := s.migrate(ctx, path, migrations); err != nil {
		return nil, errors.Join(err, db.Close())
	}

	// Pruning runs until Close, even if ctx was only for opening.
	var pruneCtx context.Context
	pruneCtx, s.stopPruning = context.WithCancel(context.WithoutCancel(ctx))
	go func() {
		defer close(s.pruned)
		s.pruneHourly(pruneCtx)
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
		if err := s.backup(ctx, backup); err != nil {
			return fmt.Errorf("back up the database before migrating: %w", err)
		}
		s.log.Info("backed up the database before migrating", "backup", backup, "version", from)
	}

	for _, m := range migrations[from:] {
		if err := s.apply(ctx, m); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	s.log.Info("migrated the database", "from", from, "to", to)
	return nil
}

// apply runs one migration and records its version, in a single transaction.
func (s *Store) apply(ctx context.Context, m migration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// After a successful Commit, Rollback does nothing.
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	// PRAGMA can't take a bound parameter, and the version is a number.
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(m.version)); err != nil {
		return err
	}
	return tx.Commit()
}

// backup writes a consistent copy of the database to dst. It writes the copy
// next to dst first, so a failed backup leaves the previous one in place.
func (s *Store) backup(ctx context.Context, dst string) error {
	tmp := filepath.Clean(dst + ".tmp")
	// VACUUM INTO needs a missing or empty file, and keeps the mode of one that
	// exists, so creating it here keeps the copy private.
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := s.vacuumInto(ctx, tmp); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return os.Rename(tmp, dst)
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

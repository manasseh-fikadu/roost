package store

import (
	"context"
	"database/sql"
	"io/fs"
	"time"
)

// Hooks for the store_test package.

const PruneInterval = pruneInterval

// OpenWith is Open with its migrations read from files instead of the embedded ones.
func OpenWith(ctx context.Context, path string, opts Options, files fs.FS) (*Store, error) {
	return open(ctx, path, opts, files)
}

// Migrations returns the embedded migration files.
func Migrations() fs.FS { return migrationFiles() }

// DB returns the store's connection pool.
func (s *Store) DB() *sql.DB { return s.db }

// ApplyFrom applies the migrations in files after version from, as Open does
// once it has read the database's version.
func (s *Store) ApplyFrom(ctx context.Context, files fs.FS, from int) error {
	migrations, err := loadMigrations(files)
	if err != nil {
		return err
	}
	_, err = s.upgrade(ctx, migrations[from:])
	return err
}

// Prune deletes what has expired at now, as the store does once an hour.
func (s *Store) Prune(ctx context.Context, now time.Time) { s.prune(ctx, now) }

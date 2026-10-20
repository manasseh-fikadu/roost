package store

import (
	"context"
	"database/sql"
	"io/fs"
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

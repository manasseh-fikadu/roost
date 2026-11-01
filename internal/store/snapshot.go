package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

// Snapshot writes a consistent copy of the database at path to dst, which must
// be missing or empty. It works while Bield is running, and it leaves the
// database as it is: unlike Open, it doesn't migrate it, and it doesn't make
// one that doesn't exist. The copy has whatever schema version the database
// has, so only a Bield that knows that version can open it.
func Snapshot(ctx context.Context, path, dst string) (err error) {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("no database to copy: %w", err)
	}
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	return (&Store{db: db}).vacuumInto(ctx, dst)
}

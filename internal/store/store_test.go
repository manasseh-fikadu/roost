package store_test

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
)

func TestOpen_newDatabase(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "data")
	path := filepath.Join(dir, "roost.db")

	s := openStore(t, path, nil)

	if got, want := userVersion(t, s.DB()), latestVersion(t); got != want {
		t.Errorf("user_version = %d, want %d", got, want)
	}
	if got, want := tables(t, s.DB()), []string{
		"agents", "alert_notes", "alerts", "check_results", "checks", "container_metrics", "device_settings",
		"enrollment_codes", "enrollments", "events", "health", "kv", "metrics", "metrics_rollup",
		"notification_channels", "notification_state", "pings", "pings_rollup", "roles", "ssh_access", "usage_samples",
	}; !slices.Equal(got, want) {
		t.Errorf("tables = %q, want %q", got, want)
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a new database was backed up: Stat(%s.bak) error = %v", path, err)
	}
	// The database holds secrets, so only its owner can read it.
	wantMode := map[string]fs.FileMode{dir: 0o700, path: 0o600, path + "-wal": 0o600, path + "-shm": 0o600}
	for name, want := range wantMode {
		info, err := os.Stat(name)
		if err != nil {
			t.Errorf("Stat(%s) error = %v", name, err)
			continue
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("mode of %s = %v, want %v", name, got, want)
		}
	}
}

func TestOpen_connectionSettings(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), nil)
	pragmas := []struct {
		name string
		want string
	}{
		{name: "journal_mode", want: "wal"},
		{name: "synchronous", want: "1"}, // NORMAL
		{name: "busy_timeout", want: "5000"},
		{name: "foreign_keys", want: "1"},
	}

	// Two connections held at once are two different ones from the pool, and
	// every connection needs the settings.
	for i := range 2 {
		conn, err := s.DB().Conn(t.Context())
		if err != nil {
			t.Fatalf("Conn() error = %v", err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		for _, p := range pragmas {
			var got string
			if err := conn.QueryRowContext(t.Context(), "PRAGMA "+p.name).Scan(&got); err != nil {
				t.Fatalf("PRAGMA %s error = %v", p.name, err)
			}
			if got != p.want {
				t.Errorf("connection %d: PRAGMA %s = %q, want %q", i, p.name, got, p.want)
			}
		}
	}
}

func TestOpen_migratesFromEveryVersion(t *testing.T) {
	t.Parallel()
	all := store.Migrations()
	entries, err := fs.ReadDir(all, ".")
	if err != nil {
		t.Fatal(err)
	}

	for from := range len(entries) {
		t.Run(fmt.Sprintf("from version %d", from), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "roost.db")
			if from == 0 {
				// An empty file, like one SQLite leaves before its first write.
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				older := fstest.MapFS{}
				for _, e := range entries[:from] {
					data, err := fs.ReadFile(all, e.Name())
					if err != nil {
						t.Fatal(err)
					}
					older[e.Name()] = &fstest.MapFile{Data: data}
				}
				closeStore(t, openStore(t, path, older))
			}

			s := openStore(t, path, nil)

			if got := userVersion(t, s.DB()); got != len(entries) {
				t.Errorf("user_version = %d, want %d", got, len(entries))
			}
			_, err := os.Stat(path + ".bak")
			if backedUp, want := err == nil, from > 0; backedUp != want {
				t.Errorf("backed up = %t, want %t (Stat error = %v)", backedUp, want, err)
			}
		})
	}
}

func TestOpen_olderVersion(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "roost.db")
	createNotes := "CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL) STRICT;"
	addPinned := "ALTER TABLE notes ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0;"
	indexPinned := "CREATE INDEX notes_pinned ON notes (pinned);"

	old := openStore(t, path, migrations(createNotes))
	if _, err := old.DB().ExecContext(t.Context(), "INSERT INTO notes (body) VALUES ('hello')"); err != nil {
		t.Fatal(err)
	}
	closeStore(t, old)

	s := openStore(t, path, migrations(createNotes, addPinned, indexPinned))

	if got := userVersion(t, s.DB()); got != 3 {
		t.Errorf("user_version = %d, want 3", got)
	}
	var body string
	var pinned int
	if err := s.DB().QueryRowContext(t.Context(), "SELECT body, pinned FROM notes").Scan(&body, &pinned); err != nil {
		t.Fatalf("read the note after migrating: %v", err)
	}
	if body != "hello" || pinned != 0 {
		t.Errorf("note = (%q, %d), want (%q, 0)", body, pinned, "hello")
	}

	// The backup is the database as it was before the migrations.
	bak := rawDB(t, path+".bak")
	if got := userVersion(t, bak); got != 1 {
		t.Errorf("backup user_version = %d, want 1", got)
	}
	if err := bak.QueryRowContext(t.Context(), "SELECT body FROM notes").Scan(&body); err != nil || body != "hello" {
		t.Errorf("backup note = %q (error %v), want %q", body, err, "hello")
	}
	if info, err := os.Stat(path + ".bak"); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("Stat(%s.bak) = %v, %v, want mode 0600", path, info, err)
	}
	if leftovers, _ := filepath.Glob(path + ".bak.*"); len(leftovers) > 0 {
		t.Errorf("the backup's temporary file was left behind: %q", leftovers)
	}

	// The next upgrade replaces the backup.
	closeStore(t, s)
	openStore(t, path, migrations(createNotes, addPinned, indexPinned, "CREATE TABLE tags (name TEXT) STRICT;"))
	if got := userVersion(t, rawDB(t, path+".bak")); got != 3 {
		t.Errorf("backup user_version after the second upgrade = %d, want 3", got)
	}
}

// roost serve and roost admin can open the same database at once, so two
// processes can find the same migrations to apply.
func TestOpen_concurrentUpgrades(t *testing.T) {
	t.Parallel()
	createNotes := "CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT NOT NULL) STRICT;"
	newer := migrations(createNotes, "CREATE TABLE tags (name TEXT) STRICT;", "CREATE TABLE pins (id INTEGER) STRICT;")
	for round := range 20 {
		path := filepath.Join(t.TempDir(), "roost.db")
		closeStore(t, openStore(t, path, migrations(createNotes)))

		const openers = 4
		var wg sync.WaitGroup
		errs := make(chan error, openers)
		for range openers {
			wg.Go(func() {
				s, err := store.OpenWith(t.Context(), path, store.Options{}, newer)
				if err == nil {
					err = s.Close()
				}
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("round %d: Open() error = %v, want every opener to succeed", round, err)
			}
		}
		if got := userVersion(t, rawDB(t, path)); got != 3 {
			t.Fatalf("round %d: user_version = %d, want 3", round, got)
		}
		if leftovers, _ := filepath.Glob(path + ".bak.*"); len(leftovers) > 0 {
			t.Fatalf("round %d: the backup left temporary files: %q", round, leftovers)
		}
		// The backup is the database before the upgrade, even if an opener
		// copied it after another one had upgraded it.
		if got := userVersion(t, rawDB(t, path+".bak")); got != 1 {
			t.Fatalf("round %d: backup user_version = %d, want 1", round, got)
		}
	}
}

// A migration that another process applied after this one read the version
// isn't applied again.
func TestStore_applyFrom(t *testing.T) {
	t.Parallel()
	files := migrations("CREATE TABLE notes (id INTEGER) STRICT;", "CREATE TABLE tags (name TEXT) STRICT;")
	s := openStore(t, filepath.Join(t.TempDir(), "roost.db"), files)

	if err := s.ApplyFrom(t.Context(), files, 0); err != nil {
		t.Errorf("applying migrations the database already has: %v", err)
	}
	if got := userVersion(t, s.DB()); got != 2 {
		t.Errorf("user_version = %d, want 2", got)
	}
}

func TestOpen_failedMigration(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "roost.db")
	createNotes := "CREATE TABLE notes (body TEXT) STRICT;"
	closeStore(t, openStore(t, path, migrations(createNotes)))

	_, err := store.OpenWith(t.Context(), path, store.Options{},
		migrations(createNotes, "CREATE TABLE half_done (a INTEGER); INSERT INTO missing VALUES (1);"))

	if err == nil || !strings.Contains(err.Error(), "0002_step.sql") {
		t.Fatalf("Open() error = %v, want an error naming 0002_step.sql", err)
	}
	db := rawDB(t, path)
	if got := userVersion(t, db); got != 1 {
		t.Errorf("user_version = %d, want 1", got)
	}
	// The failed migration's first statement was rolled back with the rest.
	if got, want := tables(t, db), []string{"notes"}; !slices.Equal(got, want) {
		t.Errorf("tables = %q, want %q", got, want)
	}

	// So are the migrations before it in the same upgrade, which leaves the
	// database as its backup has it.
	_, err = store.OpenWith(t.Context(), path, store.Options{},
		migrations(createNotes, "CREATE TABLE tags (name TEXT) STRICT;", "INSERT INTO missing VALUES (1);"))
	if err == nil || !strings.Contains(err.Error(), "0003_step.sql") {
		t.Fatalf("Open() error = %v, want an error naming 0003_step.sql", err)
	}
	if got := userVersion(t, db); got != 1 {
		t.Errorf("user_version after a failed upgrade = %d, want 1", got)
	}
	if got, want := tables(t, db), []string{"notes"}; !slices.Equal(got, want) {
		t.Errorf("tables after a failed upgrade = %q, want %q", got, want)
	}
}

func TestOpen_newerDatabase(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "roost.db")
	db := rawDB(t, path)
	newer := latestVersion(t) + 1
	if _, err := db.ExecContext(t.Context(), fmt.Sprintf("PRAGMA user_version = %d", newer)); err != nil {
		t.Fatal(err)
	}

	_, err := store.Open(t.Context(), path, store.Options{})

	if !errors.Is(err, store.ErrNewerSchema) {
		t.Fatalf("Open() error = %v, want %v", err, store.ErrNewerSchema)
	}
	if got := userVersion(t, db); got != newer {
		t.Errorf("user_version = %d, want it left at %d", got, newer)
	}
	if _, err := os.Stat(path + ".bak"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat(%s.bak) error = %v, want no backup", path, err)
	}
}

func TestOpen_badMigrations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		files []string
		bad   string // the file the error must name
	}{
		{name: "gap", files: []string{"0001_a.sql", "0003_c.sql"}, bad: "0003_c.sql"},
		{name: "repeated number", files: []string{"0001_a.sql", "0001_b.sql"}, bad: "0001_b.sql"},
		{name: "starts at zero", files: []string{"0000_a.sql", "0001_b.sql"}, bad: "0000_a.sql"},
		{name: "starts at two", files: []string{"0002_b.sql"}, bad: "0002_b.sql"},
		{name: "no description", files: []string{"0001.sql"}, bad: "0001.sql"},
		{name: "three digits", files: []string{"001_a.sql"}, bad: "001_a.sql"},
		{name: "upper case", files: []string{"0001_Add.sql"}, bad: "0001_Add.sql"},
		{name: "not SQL", files: []string{"0001_a.txt"}, bad: "0001_a.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := fstest.MapFS{}
			for _, name := range tt.files {
				files[name] = &fstest.MapFile{Data: []byte("CREATE TABLE t (a INTEGER);")}
			}
			path := filepath.Join(t.TempDir(), "roost.db")

			_, err := store.OpenWith(t.Context(), path, store.Options{}, files)

			if err == nil || !strings.Contains(err.Error(), tt.bad) {
				t.Fatalf("Open() error = %v, want an error naming %s", err, tt.bad)
			}
			if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("Open() created the database before checking the migrations: Stat error = %v", err)
			}
		})
	}
}

func TestOpen_pathNeedsEscaping(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Characters that mean something in a file: URI.
	dir := "my data?#%20"
	path := filepath.Join(root, dir, "roost.db")

	openStore(t, path, nil)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != dir {
		t.Errorf("%s holds %v, want only %q", root, entries, dir)
	}
}

func TestStore_prunesExpiredEvents(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const month = 30 * 24 * time.Hour
		path := filepath.Join(t.TempDir(), "roost.db")
		s := openStore(t, path, nil)
		// Let the prune that Open starts with finish before adding rows.
		synctest.Wait()
		now := time.Now()
		addEvent(t, s.DB(), "expired", now.Add(-month-time.Minute))
		addEvent(t, s.DB(), "expires in 90 minutes", now.Add(-month+90*time.Minute))
		addEvent(t, s.DB(), "new", now)

		time.Sleep(store.PruneInterval)
		synctest.Wait()
		if got, want := events(t, s.DB()), []string{"expires in 90 minutes", "new"}; !slices.Equal(got, want) {
			t.Errorf("events after an hour = %q, want %q", got, want)
		}

		time.Sleep(store.PruneInterval)
		synctest.Wait()
		if got, want := events(t, s.DB()), []string{"new"}; !slices.Equal(got, want) {
			t.Errorf("events after two hours = %q, want %q", got, want)
		}

		// Opening the database prunes it straight away.
		addEvent(t, s.DB(), "expired while stopped", time.Now().Add(-month-time.Minute))
		closeStore(t, s)
		s = openStore(t, path, nil)
		synctest.Wait()
		if got, want := events(t, s.DB()), []string{"new"}; !slices.Equal(got, want) {
			t.Errorf("events after reopening = %q, want %q", got, want)
		}
	})
}

// openStore opens the database at path with the given migrations, or the
// embedded ones if files is nil, and closes it when the test ends.
func openStore(t *testing.T, path string, files fs.FS) *store.Store {
	t.Helper()
	if files == nil {
		files = store.Migrations()
	}
	s, err := store.OpenWith(t.Context(), path, store.Options{}, files)
	if err != nil {
		t.Fatalf("Open(%q) error = %v", path, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func closeStore(t *testing.T, s *store.Store) {
	t.Helper()
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// migrations returns migration files with the given SQL, numbered from 0001.
func migrations(bodies ...string) fstest.MapFS {
	files := fstest.MapFS{}
	for i, body := range bodies {
		files[fmt.Sprintf("%04d_step.sql", i+1)] = &fstest.MapFile{Data: []byte(body)}
	}
	return files
}

// latestVersion returns the number of the last embedded migration.
func latestVersion(t *testing.T) int {
	t.Helper()
	entries, err := fs.ReadDir(store.Migrations(), ".")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// rawDB opens the database at path without the store, and so without migrating it.
func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("PRAGMA user_version error = %v", err)
	}
	return v
}

// tables returns the names of the database's tables, leaving out SQLite's own.
func tables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return firstColumn(t, db, "SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
}

func addEvent(t *testing.T, db *sql.DB, message string, at time.Time) {
	t.Helper()
	_, err := db.ExecContext(t.Context(),
		"INSERT INTO events (ts, level, kind, message) VALUES (?, 'info', 'test', ?)", at.UnixMilli(), message)
	if err != nil {
		t.Fatalf("add event %q: %v", message, err)
	}
}

// events returns the messages in the activity log, oldest first.
func events(t *testing.T, db *sql.DB) []string {
	t.Helper()
	return firstColumn(t, db, "SELECT message FROM events ORDER BY ts")
}

// firstColumn returns the first column of every row the query returns.
func firstColumn(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		got = append(got, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSnapshot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "roost.db")
	// The store stays open, as it does while Roost runs.
	s := openStore(t, path, nil)
	if _, err := s.SetRole(t.Context(), "alice@example.com", store.Admin); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "copy.db")
	if err := os.WriteFile(dst, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.Snapshot(t.Context(), path, dst); err != nil {
		t.Fatalf("Snapshot() error = %v, want none", err)
	}

	// A change made after the copy isn't in it.
	if _, err := s.SetRole(t.Context(), "bob@example.com", store.Viewer); err != nil {
		t.Fatal(err)
	}
	copied := openStore(t, dst, nil)
	users, err := copied.Users(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Login != "alice@example.com" || users[0].Role != store.Admin {
		t.Errorf("users in the copy = %+v, want only alice as an admin", users)
	}
}

func TestSnapshot_leavesTheDatabaseAlone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "roost.db")
	dst := filepath.Join(dir, "copy.db")

	t.Run("missing database", func(t *testing.T) {
		err := store.Snapshot(t.Context(), path, dst)

		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Snapshot() error = %v, want one for a missing file", err)
		}
		for _, name := range []string{path, dst} {
			if _, err := os.Stat(name); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("Snapshot() created %s: Stat error = %v", name, err)
			}
		}
	})

	t.Run("old schema", func(t *testing.T) {
		// A database one migration behind, which Open would migrate.
		closeStore(t, openStore(t, path, migrations("CREATE TABLE a (x)")))
		if err := os.WriteFile(dst, nil, 0o600); err != nil {
			t.Fatal(err)
		}

		if err := store.Snapshot(t.Context(), path, dst); err != nil {
			t.Fatalf("Snapshot() error = %v, want none", err)
		}

		if got := userVersion(t, rawDB(t, dst)); got != 1 {
			t.Errorf("schema version of the copy = %d, want 1: Snapshot migrated it", got)
		}
	})
}

// testingTempDir returns a new directory that outlives a benchmark, since the
// benchmarks share a database.
func testingTempDir() (string, error) { return os.MkdirTemp("", "roost-bench-") }

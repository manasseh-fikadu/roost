package store_test

import (
	"errors"
	"io/fs"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"testing/fstest"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
)

func TestStore_Enroll(t *testing.T) {
	t.Parallel()
	s := openStore(t, filepath.Join(t.TempDir(), "bield.db"), nil)
	now := time.UnixMilli(1_800_000_000_000)
	code, expires, err := s.NewEnrollmentCode(t.Context(), "alice@example.com", now)
	if err != nil {
		t.Fatalf("NewEnrollmentCode() error = %v", err)
	}
	if len(code) < 20 || !expires.Equal(now.Add(store.EnrollmentCodeLife)) {
		t.Fatalf("NewEnrollmentCode() = %q, %v; want a long random code that expires in an hour", code, expires)
	}
	if got := firstColumn(t, s.DB(), "SELECT hex(code_hash) FROM enrollment_codes"); len(got) != 1 || got[0] == code {
		t.Errorf("stored codes = %q, want only a hash of the code", got)
	}

	enroll := func(nodeID string, at time.Time) string {
		t.Helper()
		if err := s.CheckEnrollmentCode(t.Context(), code, nodeID, at); err != nil {
			t.Fatalf("CheckEnrollmentCode(%s) error = %v", nodeID, err)
		}
		secret, e, err := s.Enroll(t.Context(), code, nodeID, at)
		if err != nil {
			t.Fatalf("Enroll(%s) error = %v", nodeID, err)
		}
		if want := (store.Enrollment{At: at, By: "alice@example.com"}); !e.At.Equal(want.At) || e.By != want.By {
			t.Errorf("Enroll(%s) = %+v, want %+v", nodeID, e, want)
		}
		if got, err := s.EnrolledNode(t.Context(), secret); err != nil || got != nodeID {
			t.Errorf("EnrolledNode(the secret) = %q, %v, want %q", got, err, nodeID)
		}
		return secret
	}
	first := enroll("laptop", now)

	// The first machine to use the code keeps it.
	for _, tt := range []struct {
		code, node string
		at         time.Time
		want       error
	}{
		{code, "server", now, store.ErrCodeUsed},
		{code, "", now, store.ErrCodeUsed},
		{"nope", "laptop", now, store.ErrUnknownCode},
		{code, "laptop", expires, store.ErrCodeExpired},
	} {
		if err := s.CheckEnrollmentCode(t.Context(), tt.code, tt.node, tt.at); !errors.Is(err, tt.want) {
			t.Errorf("CheckEnrollmentCode(%q, %q) error = %v, want %v", tt.code, tt.node, err, tt.want)
		}
		if tt.node == "" {
			continue
		}
		if _, _, err := s.Enroll(t.Context(), tt.code, tt.node, tt.at); !errors.Is(err, tt.want) {
			t.Errorf("Enroll(%q, %q) error = %v, want %v", tt.code, tt.node, err, tt.want)
		}
	}

	// It can enroll again, such as after a failed install, and its new
	// secret replaces the old one.
	second := enroll("laptop", now.Add(time.Minute))
	if second == first {
		t.Error("enrolling again gave the same secret")
	}
	if _, err := s.EnrolledNode(t.Context(), first); !errors.Is(err, store.ErrUnknownSecret) {
		t.Errorf("EnrolledNode(the first secret) error = %v, want ErrUnknownSecret", err)
	}
	got, err := s.Enrollments(t.Context())
	if err != nil {
		t.Fatalf("Enrollments() error = %v", err)
	}
	want := map[string]store.Enrollment{"laptop": {At: time.UnixMilli(now.Add(time.Minute).UnixMilli()), By: "alice@example.com"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Enrollments() = %+v, want %+v", got, want)
	}

	if ok, err := s.Unenroll(t.Context(), "server", "laptop"); !ok || err != nil {
		t.Errorf("Unenroll(server, laptop) = %v, %v, want true", ok, err)
	}
	if ok, err := s.Unenroll(t.Context(), "laptop"); ok || err != nil {
		t.Errorf("Unenroll(laptop) again = %v, %v, want false", ok, err)
	}
	if _, err := s.EnrolledNode(t.Context(), second); !errors.Is(err, store.ErrUnknownSecret) {
		t.Errorf("EnrolledNode() after Unenroll: error = %v, want ErrUnknownSecret", err)
	}
	if _, err := s.EnrolledNode(t.Context(), ""); !errors.Is(err, store.ErrUnknownSecret) {
		t.Errorf("EnrolledNode(\"\") error = %v, want ErrUnknownSecret", err)
	}

	// After unenrolling, such as when the agent is uninstalled, the machine
	// can still use its code. Revoking deletes the code too.
	third := enroll("laptop", now.Add(2*time.Minute))
	if ok, err := s.Revoke(t.Context(), "laptop"); !ok || err != nil {
		t.Errorf("Revoke(laptop) = %v, %v, want true", ok, err)
	}
	if _, err := s.EnrolledNode(t.Context(), third); !errors.Is(err, store.ErrUnknownSecret) {
		t.Errorf("EnrolledNode() after Revoke: error = %v, want ErrUnknownSecret", err)
	}
	if err := s.CheckEnrollmentCode(t.Context(), code, "laptop", now); !errors.Is(err, store.ErrUnknownCode) {
		t.Errorf("CheckEnrollmentCode() after Revoke: error = %v, want ErrUnknownCode", err)
	}
	if ok, err := s.Revoke(t.Context(), "laptop"); ok || err != nil {
		t.Errorf("Revoke(laptop) again = %v, %v, want false", ok, err)
	}
}

func TestStore_prunesExpiredCodes(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := openStore(t, filepath.Join(t.TempDir(), "bield.db"), nil)
		synctest.Wait()
		old, _, err := s.NewEnrollmentCode(t.Context(), "alice@example.com", time.Now().Add(-store.EnrollmentCodeLife-time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		fresh, _, err := s.NewEnrollmentCode(t.Context(), "alice@example.com", time.Now())
		if err != nil {
			t.Fatal(err)
		}

		time.Sleep(store.PruneInterval)
		synctest.Wait()
		if err := s.CheckEnrollmentCode(t.Context(), old, "laptop", time.Now()); !errors.Is(err, store.ErrUnknownCode) {
			t.Errorf("an expired code after an hour: error = %v, want ErrUnknownCode", err)
		}
		// The fresh code expired too, but only just.
		if err := s.CheckEnrollmentCode(t.Context(), fresh, "laptop", time.Now()); !errors.Is(err, store.ErrCodeExpired) {
			t.Errorf("a code that just expired: error = %v, want ErrCodeExpired", err)
		}
	})
}

// The token that agents shared before enrollment is gone.
func TestOpen_dropsAgentToken(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "bield.db")
	v5 := fstest.MapFS{}
	entries, err := fs.ReadDir(store.Migrations(), ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries[:5] {
		data, err := fs.ReadFile(store.Migrations(), e.Name())
		if err != nil {
			t.Fatal(err)
		}
		v5[e.Name()] = &fstest.MapFile{Data: data}
	}
	old := openStore(t, path, v5)
	if _, err := old.DB().ExecContext(t.Context(),
		"INSERT INTO kv (key, value) VALUES ('agent_token', 'shared'), ('who_can_view', 'listed')"); err != nil {
		t.Fatal(err)
	}
	closeStore(t, old)

	s := openStore(t, path, nil)
	if got := firstColumn(t, s.DB(), "SELECT key FROM kv ORDER BY key"); !slices.Equal(got, []string{"who_can_view"}) {
		t.Errorf("kv keys after migrating = %q, want only who_can_view", got)
	}
}

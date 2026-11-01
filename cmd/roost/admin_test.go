package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdmin(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "data")
	admin := func(args ...string) (string, string, error) {
		var stdout, stderr bytes.Buffer
		err := run(t.Context(), append([]string{"admin", args[0], "--data-dir", dir}, args[1:]...), &stdout, &stderr)
		return stdout.String(), stderr.String(), err
	}
	steps := []struct {
		args    []string
		out     string // stdout
		errOut  string // part of stderr
		wantErr string // part of the error
	}{
		{args: []string{"list"}, errOut: "no admins or viewers yet"},
		{args: []string{"add", "alice@example.com"}, out: "alice@example.com is now an admin\n"},
		{args: []string{"add", "Alice@Example.com"}, out: "Alice@Example.com was already an admin\n"},
		{args: []string{"add-viewer", "bob@github"}, out: "bob@github is now a viewer\n"},
		{args: []string{"add-viewer", "carol@example.com"}, out: "carol@example.com is now a viewer\n"},
		{args: []string{"add", "carol@example.com"}, out: "carol@example.com is now an admin\n"},
		{
			args:   []string{"list"},
			out:    "alice@example.com  admin\nbob@github         viewer\ncarol@example.com  admin\n",
			errOut: "No one else can view Roost",
		},
		{args: []string{"remove", "carol@example.com"}, out: "carol@example.com no longer has a role\n"},
		{args: []string{"remove", "carol@example.com"}, out: "carol@example.com had no role\n"},
		{args: []string{"add-viewer", "alice@example.com"}, wantErr: "the last admin has to stay an admin"},
		{args: []string{"remove", "alice@example.com"}, wantErr: "the last admin has to stay an admin"},
		{args: []string{"add", "alice smith"}, wantErr: "isn't a login"},
		{args: []string{"who-can-view"}, out: "listed\n"},
		{args: []string{"who-can-view", "tailnet"}, out: "Anyone who signs in from the tailnet can view Roost\n"},
		{args: []string{"who-can-view"}, out: "tailnet\n"},
		{
			args:   []string{"list"},
			out:    "alice@example.com  admin\nbob@github         viewer\n",
			errOut: "Anyone who signs in from the tailnet can view Roost too",
		},
		{args: []string{"who-can-view", "everyone"}, wantErr: `not "everyone"`},
		{args: []string{"who-can-view", "listed"}, out: "Only Roost's admins and viewers can view it\n"},
		// A Headscale user without an email is just a name.
		{args: []string{"add-viewer", "dave"}, out: "dave is now a viewer\n"},
		{args: []string{"add", "dave"}, out: "dave is now an admin\n"},
		{args: []string{"remove", "dave"}, out: "dave no longer has a role\n"},
	}
	for _, st := range steps {
		out, errOut, err := admin(st.args...)
		if st.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), st.wantErr) {
				t.Errorf("admin %q: error = %v, want one about %q", st.args, err, st.wantErr)
			}
			continue
		}
		if err != nil || out != st.out || !strings.Contains(errOut, st.errOut) {
			t.Errorf("admin %q = %q, %q, %v; want %q and %q on stderr", st.args, out, errOut, err, st.out, st.errOut)
		}
	}
}

func TestAdmin_usage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"admin"},
		{"admin", "delete", "alice@example.com"},
		{"admin", "add", "--data-dir", dir},
		{"admin", "add", "--data-dir", dir, "alice@example.com", "bob@example.com"},
		{"admin", "add-viewer", "--data-dir", dir},
		{"admin", "remove", "--data-dir", dir},
		{"admin", "list", "--data-dir", dir, "extra"},
		{"admin", "who-can-view", "--data-dir", dir, "tailnet", "listed"},
	} {
		var stderr bytes.Buffer
		if err := run(t.Context(), args, &bytes.Buffer{}, &stderr); !errors.Is(err, errUsage) || !strings.Contains(stderr.String(), "roost admin add") {
			t.Errorf("run(%q) error = %v, stderr %q; want the admin usage", args, err, stderr.String())
		}
	}
}

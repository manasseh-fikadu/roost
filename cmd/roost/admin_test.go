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
		err := run(t.Context(), append([]string{"admin"}, args...), &stdout, &stderr)
		return stdout.String(), stderr.String(), err
	}

	if out, errOut, err := admin("list", "--data-dir", dir); err != nil || out != "" || !strings.Contains(errOut, "no admins") {
		t.Errorf("admin list on a new database = %q, %q, %v; want no admins", out, errOut, err)
	}
	if out, _, err := admin("add", "--data-dir", dir, "alice@example.com"); err != nil || out != "alice@example.com is now an admin\n" {
		t.Errorf("admin add alice = %q, %v", out, err)
	}
	if out, _, err := admin("add", "--data-dir", dir, "Alice@Example.com"); err != nil || !strings.Contains(out, "already an admin") {
		t.Errorf("admin add Alice again = %q, %v; want already an admin", out, err)
	}
	if _, _, err := admin("add", "--data-dir", dir, "bob@github"); err != nil {
		t.Fatal(err)
	}
	if out, _, err := admin("list", "--data-dir", dir); err != nil || out != "alice@example.com\nbob@github\n" {
		t.Errorf("admin list = %q, %v; want alice and bob", out, err)
	}
	if _, _, err := admin("add", "--data-dir", dir, "alice"); err == nil || !strings.Contains(err.Error(), "isn't a Tailscale login") {
		t.Errorf("admin add with a bad login: error = %v", err)
	}
}

func TestAdmin_usage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"admin"},
		{"admin", "remove", "alice@example.com"},
		{"admin", "add", "--data-dir", dir},
		{"admin", "add", "--data-dir", dir, "alice@example.com", "bob@example.com"},
		{"admin", "list", "--data-dir", dir, "extra"},
	} {
		var stderr bytes.Buffer
		if err := run(t.Context(), args, &bytes.Buffer{}, &stderr); !errors.Is(err, errUsage) || !strings.Contains(stderr.String(), "roost admin add") {
			t.Errorf("run(%q) error = %v, stderr %q; want the admin usage", args, err, stderr.String())
		}
	}
}

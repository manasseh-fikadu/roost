package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		args       []string
		wantErr    error
		wantStdout string // substring
		wantStderr string // substring
	}{
		{name: "no command", wantErr: errUsage, wantStderr: "Usage: roost"},
		{name: "help", args: []string{"help"}, wantStdout: "Usage: roost"},
		{name: "unknown command", args: []string{"bogus"}, wantErr: errUsage, wantStderr: `unknown command "bogus"`},
		{name: "version", args: []string{"version"}, wantStdout: buildVersion() + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			err := run(t.Context(), tt.args, &stdout, &stderr)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("run(%q) error = %v, want %v", tt.args, err, tt.wantErr)
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestServe(t *testing.T) {
	t.Parallel()

	t.Run("stops cleanly when the context ends", func(t *testing.T) {
		t.Parallel()
		dataDir := filepath.Join(t.TempDir(), "data")
		ctx, cancel := context.WithCancel(t.Context())
		log := newLogWriter("roost is running")
		done := make(chan error, 1)

		go func() { done <- serve(ctx, []string{"--listen", "127.0.0.1:0", "--data-dir", dataDir}, log) }()
		select {
		case <-log.seen:
		case err := <-done:
			t.Fatalf("serve() returned before it was running: %v; log:\n%s", err, log)
		}
		cancel()

		if err := <-done; err != nil {
			t.Fatalf("serve() error = %v, want none; log:\n%s", err, log)
		}
		if _, err := os.Stat(filepath.Join(dataDir, "roost.db")); err != nil {
			t.Errorf("serve() didn't create the database: %v", err)
		}
	})

	tests := []struct {
		name    string
		args    func(t *testing.T) []string
		wantErr string // substring
	}{
		{
			name: "bad listen address",
			args: func(t *testing.T) []string {
				t.Helper()
				return []string{"--listen", "not-an-address", "--data-dir", t.TempDir()}
			},
			wantErr: "listen on not-an-address",
		},
		{
			name: "data directory is a file",
			args: func(t *testing.T) []string {
				t.Helper()
				file := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(file, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{"--listen", "127.0.0.1:0", "--data-dir", file}
			},
			wantErr: "roost.db",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer

			err := serve(t.Context(), tt.args(t), &stderr)

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("serve() error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// logWriter collects a log and closes seen when a line contains a marker.
type logWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	marker []byte
	seen   chan struct{}
}

func newLogWriter(marker string) *logWriter {
	return &logWriter{marker: []byte(marker), seen: make(chan struct{})}
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.marker != nil && bytes.Contains(p, w.marker) {
		close(w.seen)
		w.marker = nil
	}
	return w.buf.Write(p)
}

func (w *logWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

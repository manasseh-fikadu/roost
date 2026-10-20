package main

import (
	"bytes"
	"errors"
	"strings"
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

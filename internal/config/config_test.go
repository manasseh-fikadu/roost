package config_test

import (
	"errors"
	"flag"
	"io"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/config"
)

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		args      []string
		env       map[string]string
		want      config.Config
		wantErr   bool
		wantErrIs error // if set, the error must match it with errors.Is
	}{
		{
			name: "defaults",
			want: config.Config{Listen: config.DefaultListen, DataDir: config.DefaultDataDir()},
		},
		{
			name: "environment",
			env:  map[string]string{"ROOST_LISTEN": ":9000", "ROOST_DATA_DIR": "/srv/roost"},
			want: config.Config{Listen: ":9000", DataDir: "/srv/roost"},
		},
		{
			name: "empty environment variable is unset",
			env:  map[string]string{"ROOST_LISTEN": "", "ROOST_DATA_DIR": ""},
			want: config.Config{Listen: config.DefaultListen, DataDir: config.DefaultDataDir()},
		},
		{
			name: "flag beats environment",
			args: []string{"--listen", "127.0.0.1:1", "--data-dir", "data"},
			env:  map[string]string{"ROOST_LISTEN": ":9000", "ROOST_DATA_DIR": "/srv/roost"},
			want: config.Config{Listen: "127.0.0.1:1", DataDir: "data"},
		},
		{
			name:    "empty data directory",
			args:    []string{"--data-dir", ""},
			wantErr: true,
		},
		{
			name:      "help",
			args:      []string{"-h"},
			wantErr:   true,
			wantErrIs: flag.ErrHelp,
		},
		{
			name:    "unknown flag",
			args:    []string{"--nope"},
			wantErr: true,
		},
		{
			name:    "stray argument",
			args:    []string{"extra"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(key string) string { return tt.env[key] }

			got, err := config.Parse(tt.args, getenv, io.Discard)

			switch {
			case !tt.wantErr && err != nil:
				t.Fatalf("Parse(%q) error = %v, want none", tt.args, err)
			case tt.wantErr && err == nil:
				t.Fatalf("Parse(%q) = %+v, want an error", tt.args, got)
			case tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs):
				t.Fatalf("Parse(%q) error = %v, want %v", tt.args, err, tt.wantErrIs)
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

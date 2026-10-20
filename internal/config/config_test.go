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
		name    string
		args    []string
		env     map[string]string
		want    config.Config
		wantErr error // matched with errors.Is; errAny matches any error
	}{
		{
			name: "defaults",
			want: config.Config{Listen: config.DefaultListen},
		},
		{
			name: "environment",
			env:  map[string]string{"ROOST_LISTEN": ":9000"},
			want: config.Config{Listen: ":9000"},
		},
		{
			name: "empty environment variable is unset",
			env:  map[string]string{"ROOST_LISTEN": ""},
			want: config.Config{Listen: config.DefaultListen},
		},
		{
			name: "flag beats environment",
			args: []string{"--listen", "127.0.0.1:1"},
			env:  map[string]string{"ROOST_LISTEN": ":9000"},
			want: config.Config{Listen: "127.0.0.1:1"},
		},
		{
			name:    "help",
			args:    []string{"-h"},
			wantErr: flag.ErrHelp,
		},
		{
			name:    "unknown flag",
			args:    []string{"--nope"},
			wantErr: errAny,
		},
		{
			name:    "stray argument",
			args:    []string{"extra"},
			wantErr: errAny,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			getenv := func(key string) string { return tt.env[key] }

			got, err := config.Parse(tt.args, getenv, io.Discard)

			switch {
			case tt.wantErr == nil && err != nil:
				t.Fatalf("Parse(%q) error = %v, want none", tt.args, err)
			case tt.wantErr != nil && err == nil:
				t.Fatalf("Parse(%q) = %+v, want an error", tt.args, got)
			case tt.wantErr != nil && tt.wantErr != errAny && !errors.Is(err, tt.wantErr):
				t.Fatalf("Parse(%q) error = %v, want %v", tt.args, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

var errAny = errors.New("any error")

package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		listen  string
		want    string
		wantErr bool
	}{
		{name: "loopback", listen: "127.0.0.1:4700", want: "http://127.0.0.1:4700/"},
		{name: "every address", listen: ":4700", want: "http://127.0.0.1:4700/"},
		{name: "every IPv4 address", listen: "0.0.0.0:4700", want: "http://127.0.0.1:4700/"},
		{name: "every IPv6 address", listen: "[::]:4700", want: "http://127.0.0.1:4700/"},
		{name: "IPv6 loopback", listen: "[::1]:4700", want: "http://[::1]:4700/"},
		{name: "a name", listen: "localhost:9000", want: "http://localhost:9000/"},
		{name: "no port", listen: "127.0.0.1", wantErr: true},
		{name: "port zero", listen: "127.0.0.1:0", wantErr: true},
		{name: "port isn't a number", listen: "127.0.0.1:http", wantErr: true},
		{name: "empty", listen: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := healthURL(tt.listen)

			if (err != nil) != tt.wantErr {
				t.Fatalf("healthURL(%q) error = %v, want error: %v", tt.listen, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("healthURL(%q) = %q, want %q", tt.listen, got, tt.want)
			}
		})
	}
}

func TestHealthcheck(t *testing.T) {
	t.Parallel()
	answer := func(status int, location string) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			if location != "" {
				w.Header().Set("Location", location)
			}
			w.WriteHeader(status)
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	tests := []struct {
		name       string
		addr       string
		wantErr    string // part of the error, or "" for none
		wantStdout string
	}{
		{name: "running", addr: answer(http.StatusOK, "").Listener.Addr().String(), wantStdout: "Roost is running\n"},
		{name: "failing", addr: answer(http.StatusServiceUnavailable, "").Listener.Addr().String(), wantErr: "503"},
		{name: "redirecting", addr: answer(http.StatusFound, "http://elsewhere.example/").Listener.Addr().String(), wantErr: "302"},
		{name: "nothing listening", addr: closed.Listener.Addr().String(), wantErr: "no answer from Roost"},
		{name: "bad address", addr: "nonsense", wantErr: "isn't like"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			err := run(t.Context(), []string{"healthcheck", "--setup-listen", tt.addr}, &stdout, &stderr)

			if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Errorf("healthcheck error = %v, want one with %q", err, tt.wantErr)
			}
			if stdout.String() != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
			}
		})
	}
}

func TestHealthcheck_arguments(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer

	err := run(t.Context(), []string{"healthcheck", "extra"}, &bytes.Buffer{}, &stderr)

	if !errors.Is(err, errUsage) || !strings.Contains(stderr.String(), "Usage: roost healthcheck") {
		t.Errorf("healthcheck extra: error = %v, stderr = %q, want usage", err, stderr.String())
	}
}

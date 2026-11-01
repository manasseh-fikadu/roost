package api_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/tailnet/tailnettest"
)

const loginURL = "https://login.tailscale.com/a/0123456789"

func TestSetup(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		status      tailnet.Status
		statusErr   error
		appURL      string
		want        []string // substrings of the body
		wantNot     []string
		wantRefresh bool
	}{
		{
			name:        "needs login",
			status:      tailnet.Status{State: tailnet.StateNeedsLogin, LoginURL: loginURL},
			want:        []string{"Add Roost to your tailnet", `href="` + loginURL + `"`, "<svg", "TS_AUTHKEY"},
			wantRefresh: true,
		},
		{
			// Headscale's page asks someone to run a command, not to log in
			// to Tailscale.
			name: "needs login on a Headscale server",
			status: tailnet.Status{
				State: tailnet.StateNeedsLogin, ControlURL: "https://hs.example.com:8443",
				LoginURL: "https://hs.example.com:8443/register/abc",
			},
			want: []string{
				"Add Roost to your tailnet", "<code>hs.example.com:8443</code>", `href="https://hs.example.com:8443/register/abc"`,
				"headscale preauthkeys create",
			},
			wantNot:     []string{"Log in to Tailscale"},
			wantRefresh: true,
		},
		{
			name:        "starting",
			status:      tailnet.Status{State: "Starting"},
			want:        []string{"Connecting to Tailscale", "Tailscale is Starting."},
			wantNot:     []string{"<svg"},
			wantRefresh: true,
		},
		{
			name:        "running but not serving yet",
			status:      tailnet.Status{State: tailnet.StateRunning},
			want:        []string{"Connecting to Tailscale"},
			wantRefresh: true,
		},
		{
			name:    "running",
			status:  tailnet.Status{State: tailnet.StateRunning},
			appURL:  "https://roost.example.ts.net",
			want:    []string{"Roost is on your tailnet", `href="https://roost.example.ts.net"`},
			wantNot: []string{"login.tailscale.com"},
		},
		{
			name:        "Tailscale fails",
			statusErr:   errors.New("tsnet: <closed>"),
			want:        []string{"Roost can't reach Tailscale", "tsnet: &lt;closed&gt;"},
			wantRefresh: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tn := tailnettest.New(tt.status)
			tn.FailStatus(tt.statusErr)
			h := api.Setup(api.SetupOptions{Tailnet: tn, AppURL: func() string { return tt.appURL }})

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:4700/", nil))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body:\n%s", rec.Code, http.StatusOK, rec.Body)
			}
			body := rec.Body.String()
			for _, s := range tt.want {
				if !strings.Contains(body, s) {
					t.Errorf("body doesn't contain %q:\n%s", s, body)
				}
			}
			for _, s := range tt.wantNot {
				if strings.Contains(body, s) {
					t.Errorf("body contains %q:\n%s", s, body)
				}
			}
			if refresh := strings.Contains(body, `http-equiv="refresh"`); refresh != tt.wantRefresh {
				t.Errorf("page refreshes itself = %v, want %v", refresh, tt.wantRefresh)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
			if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "frame-ancestors 'none'") {
				t.Errorf("Content-Security-Policy = %q, want it to forbid framing", got)
			}
		})
	}
}

func TestSetup_requests(t *testing.T) {
	t.Parallel()
	tn := tailnettest.New(tailnet.Status{State: tailnet.StateNeedsLogin, LoginURL: loginURL})
	h := api.Setup(api.SetupOptions{Tailnet: tn, AppURL: func() string { return "" }})
	tests := []struct {
		name     string
		method   string
		host     string
		path     string
		wantCode int
	}{
		{"IPv4", http.MethodGet, "127.0.0.1:4700", "/", http.StatusOK},
		{"IPv6", http.MethodGet, "[::1]:4700", "/", http.StatusOK},
		{"localhost", http.MethodGet, "localhost:4700", "/", http.StatusOK},
		{"name under localhost", http.MethodGet, "roost.localhost:4700", "/", http.StatusOK},
		{"LAN address", http.MethodGet, "192.168.1.20:4700", "/", http.StatusOK},
		{"HEAD", http.MethodHead, "localhost:4700", "/", http.StatusOK},
		// A page whose domain resolves to 127.0.0.1 sends its own name.
		{"DNS rebinding", http.MethodGet, "attacker.example:4700", "/", http.StatusForbidden},
		{"lookalike of localhost", http.MethodGet, "localhost.attacker.example", "/", http.StatusForbidden},
		{"the API isn't here", http.MethodGet, "localhost:4700", "/api/health", http.StatusNotFound},
		{"nor the app", http.MethodGet, "localhost:4700", "/devices", http.StatusNotFound},
		{"POST", http.MethodPost, "localhost:4700", "/", http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			req.Host = tt.host
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantCode != http.StatusOK && strings.Contains(rec.Body.String(), loginURL) {
				t.Error("the login URL leaked into an error response")
			}
		})
	}
}

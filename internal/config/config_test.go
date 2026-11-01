package config_test

import (
	"errors"
	"flag"
	"io"
	"reflect"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/config"
)

func TestParse(t *testing.T) {
	t.Parallel()
	defaults := config.Config{
		Tailscale:   config.DefaultTailscale,
		Hostname:    config.DefaultHostname,
		SetupListen: config.DefaultSetupListen,
		Port:        config.DefaultPort,
		DataDir:     config.DefaultDataDir(),
		HostRoot:    config.DefaultHostRoot,
		DemoListen:  config.DefaultDemoListen,
		DemoDevices: config.DefaultDemoDevices,
		DemoDays:    config.DefaultDemoDays,
	}
	with := func(change func(*config.Config)) config.Config {
		cfg := defaults
		change(&cfg)
		return cfg
	}
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
			want: defaults,
		},
		{
			name: "environment",
			env: map[string]string{
				"ROOST_TAILSCALE":    "host",
				"ROOST_HOSTNAME":     "hub",
				"ROOST_SETUP_LISTEN": ":9000",
				"ROOST_PORT":         "8443",
				"ROOST_DATA_DIR":     "/srv/roost",
				"ROOST_HOST_ROOT":    "/host",
				"ROOST_DEV_UI":       "http://127.0.0.1:5173",
				"ROOST_ADMINS":       "alice@example.com, bob@github,",
				"ROOST_DEMO":         "true",
				"ROOST_DEMO_LISTEN":  "0.0.0.0:8080",
				"ROOST_DEMO_DEVICES": "200",
				"ROOST_DEMO_DAYS":    "30",
			},
			want: config.Config{
				Tailscale:   config.Host,
				Hostname:    "hub",
				SetupListen: ":9000",
				Port:        8443,
				DataDir:     "/srv/roost",
				HostRoot:    "/host",
				DevUI:       "http://127.0.0.1:5173",
				Admins:      []string{"alice@example.com", "bob@github"},
				Demo:        true,
				DemoListen:  "0.0.0.0:8080",
				DemoDevices: 200,
				DemoDays:    30,
			},
		},
		{
			name: "empty environment variable is unset",
			env:  map[string]string{"ROOST_SETUP_LISTEN": "", "ROOST_DATA_DIR": "", "ROOST_PORT": ""},
			want: defaults,
		},
		{
			name: "flag beats environment",
			args: []string{"--setup-listen", "127.0.0.1:1", "--data-dir", "data", "--port", "443"},
			env:  map[string]string{"ROOST_SETUP_LISTEN": ":9000", "ROOST_DATA_DIR": "/srv/roost", "ROOST_PORT": "1"},
			want: with(func(c *config.Config) { c.SetupListen, c.DataDir, c.Port = "127.0.0.1:1", "data", 443 }),
		},
		{
			name: "host mode",
			args: []string{"--tailscale", "host"},
			want: with(func(c *config.Config) { c.Tailscale = config.Host }),
		},
		{
			name:    "unknown mode",
			args:    []string{"--tailscale", "funnel"},
			wantErr: true,
		},
		{
			name:    "empty hostname",
			args:    []string{"--hostname", ""},
			wantErr: true,
		},
		{
			name:    "port out of range",
			args:    []string{"--port", "70000"},
			wantErr: true,
		},
		{
			name:    "port in the environment isn't a number",
			env:     map[string]string{"ROOST_PORT": "https"},
			wantErr: true,
		},
		{
			name: "port flag replaces a bad one in the environment",
			args: []string{"--port", "4800"},
			env:  map[string]string{"ROOST_PORT": "https"},
			want: with(func(c *config.Config) { c.Port = 4800 }),
		},
		{
			name: "admins flag beats environment",
			args: []string{"--admins", "carol@example.com"},
			env:  map[string]string{"ROOST_ADMINS": "alice@example.com"},
			want: with(func(c *config.Config) { c.Admins = []string{"carol@example.com"} }),
		},
		{
			name: "admins can be plain names, as on Headscale",
			args: []string{"--admins", "alice,bob@example.com"},
			want: with(func(c *config.Config) { c.Admins = []string{"alice", "bob@example.com"} }),
		},
		{
			name: "control URL flag beats environment",
			args: []string{"--control-url", "https://hs.example.com:8443"},
			env:  map[string]string{"ROOST_CONTROL_URL": "https://other.example.com"},
			want: with(func(c *config.Config) { c.ControlURL = "https://hs.example.com:8443" }),
		},
		{
			name: "control URL from the environment",
			env:  map[string]string{"ROOST_CONTROL_URL": "http://127.0.0.1:8080/"},
			want: with(func(c *config.Config) { c.ControlURL = "http://127.0.0.1:8080/" }),
		},
		{
			name:    "control URL in host mode",
			args:    []string{"--tailscale", "host", "--control-url", "https://hs.example.com"},
			wantErr: true,
		},
		{
			name:    "control URL isn't a web address",
			args:    []string{"--control-url", "hs.example.com"},
			wantErr: true,
		},
		{
			name:    "control URL with a path",
			args:    []string{"--control-url", "https://example.com/headscale"},
			wantErr: true,
		},
		{
			name:    "control URL with a password",
			args:    []string{"--control-url", "https://user:pass@hs.example.com"},
			wantErr: true,
		},
		{
			name:    "admin isn't a login",
			args:    []string{"--admins", "alice@example.com,bob smith"},
			wantErr: true,
		},
		{
			name:    "dev UI with a password",
			args:    []string{"--dev-ui", "http://user:secret@127.0.0.1:5173"},
			wantErr: true,
		},
		{
			name:    "dev UI with a query",
			args:    []string{"--dev-ui", "http://127.0.0.1:5173/?token=secret"},
			wantErr: true,
		},
		{
			name:    "dev UI isn't an http URL",
			args:    []string{"--dev-ui", "127.0.0.1:5173"},
			wantErr: true,
		},
		{
			name: "host root flag beats environment",
			args: []string{"--host-root", "/mnt/host"},
			env:  map[string]string{"ROOST_HOST_ROOT": "/host"},
			want: with(func(c *config.Config) { c.HostRoot = "/mnt/host" }),
		},
		{
			name:    "host root isn't absolute",
			args:    []string{"--host-root", "host"},
			wantErr: true,
		},
		{
			name:    "empty data directory",
			args:    []string{"--data-dir", ""},
			wantErr: true,
		},
		{
			name: "demo",
			args: []string{"--demo", "--demo-devices", "200"},
			want: with(func(c *config.Config) { c.Demo, c.DemoDevices = true, 200 }),
		},
		{
			name: "demo flag beats a bad environment variable",
			args: []string{"--demo=false"},
			env:  map[string]string{"ROOST_DEMO": "maybe"},
			want: defaults,
		},
		{
			name:    "demo environment variable isn't true or false",
			env:     map[string]string{"ROOST_DEMO": "maybe"},
			wantErr: true,
		},
		{
			name:    "demo devices in the environment isn't a number",
			env:     map[string]string{"ROOST_DEMO_DEVICES": "many"},
			wantErr: true,
		},
		{
			name:    "demo with no history",
			args:    []string{"--demo", "--demo-days", "0"},
			wantErr: true,
		},
		{
			name: "demo with a fraction of a day",
			args: []string{"--demo", "--demo-days", "0.05"},
			want: with(func(c *config.Config) { c.Demo, c.DemoDays = true, 0.05 }),
		},
		{
			name:    "demo with too much history",
			args:    []string{"--demo", "--demo-days", "400"},
			wantErr: true,
		},
		{
			name:    "demo with one device",
			args:    []string{"--demo", "--demo-devices", "1"},
			wantErr: true,
		},
		{
			name:    "demo with too many devices",
			args:    []string{"--demo", "--demo-devices", "5000"},
			wantErr: true,
		},
		{
			name:    "demo without an address",
			args:    []string{"--demo", "--demo-listen", ""},
			wantErr: true,
		},
		{
			name: "demo doesn't check the Tailscale settings",
			args: []string{"--demo", "--tailscale", "funnel", "--hostname", ""},
			want: with(func(c *config.Config) { c.Demo, c.Tailscale, c.Hostname = true, "funnel", "" }),
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
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse(%q) = %+v, want %+v", tt.args, got, tt.want)
			}
		})
	}
}

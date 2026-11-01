// Package config reads Roost's settings from flags and environment variables.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// The ways Roost can join the tailnet, set with --tailscale.
const (
	// Embedded runs a Tailscale node inside Roost, so Roost is its own
	// machine on the tailnet.
	Embedded = "embedded"
	// Host uses the Tailscale already running on this machine.
	Host = "host"
)

// Defaults for the settings that have one.
const (
	DefaultTailscale   = Embedded
	DefaultHostname    = "roost"
	DefaultSetupListen = "127.0.0.1:4700"
	DefaultPort        = 4700
	DefaultHostRoot    = "/"
	DefaultDemoListen  = "127.0.0.1:4700"
	DefaultDemoDevices = 12
	DefaultDemoDays    = 7.0
)

// The most a demo can have: devices, and days of history.
const (
	MaxDemoDevices = 1000
	MaxDemoDays    = 90
)

// Config holds the hub's settings.
type Config struct {
	// Tailscale is how Roost joins the tailnet: Embedded or Host.
	Tailscale string
	// Hostname is the embedded node's name on the tailnet.
	Hostname string
	// ControlURL is the URL of the control server the embedded node logs in
	// to, such as a Headscale server's. It's empty for Tailscale's own. Host
	// mode uses whatever control server the machine's Tailscale is logged in
	// to, so it has none.
	ControlURL string
	// SetupListen is the local TCP address of the first-run page, which
	// embedded mode serves until Roost is logged in to the tailnet.
	SetupListen string
	// Port is the port the app is served on in host mode, on each of the
	// machine's tailnet IPs. Embedded mode always uses 443, or 80 without
	// HTTPS.
	Port int
	// DataDir is the directory that holds the database and the embedded
	// node's state.
	DataDir string
	// HostRoot is the directory whose proc/ and sys/ Roost measures the machine
	// from. It is "/" unless Roost runs in a container that has the host's
	// /proc and /sys mounted somewhere else, such as /host.
	HostRoot string
	// DevUI, if set, is the URL of a Vite dev server to proxy the web UI to,
	// instead of serving the embedded build.
	DevUI string
	// Admins are the Tailscale logins to make Roost's first admins, along with
	// the owner of Roost's node. They're only used while Roost has no admins.
	Admins []string
	// Demo runs Roost against a made-up tailnet, with no Tailscale and none
	// of the machine's own data, so it needs nothing to show off. It keeps
	// its data in a temporary directory, and the Tailscale flags above don't
	// apply.
	Demo bool
	// DemoListen is the local TCP address the demo is served on.
	DemoListen string
	// DemoDevices is how many devices the demo's tailnet has, Roost's own
	// node included.
	DemoDevices int
	// DemoDays is how many days of history the demo starts with. It can be a
	// fraction, such as in a test that has no time to seed a day.
	DemoDays float64
}

// Parse reads the serve command's flags from args. A flag that isn't given falls
// back to its ROOST_* environment variable, looked up with getenv, and then to
// its default. Usage and errors are written to output.
func Parse(args []string, getenv func(string) string, output io.Writer) (Config, error) {
	fs := flag.NewFlagSet("roost serve", flag.ContinueOnError)
	fs.SetOutput(output)

	// A bad ROOST_PORT only matters if --port doesn't replace it.
	port, portErr := envInt(getenv, "ROOST_PORT", DefaultPort)
	var cfg Config
	fs.StringVar(&cfg.Tailscale, "tailscale", envOr(getenv, "ROOST_TAILSCALE", DefaultTailscale),
		"`mode`: \"embedded\" joins the tailnet as its own machine, \"host\" uses this machine's Tailscale (env ROOST_TAILSCALE)")
	fs.StringVar(&cfg.Hostname, "hostname", envOr(getenv, "ROOST_HOSTNAME", DefaultHostname),
		"`name` of Roost's machine on the tailnet in embedded mode (env ROOST_HOSTNAME)")
	fs.StringVar(&cfg.ControlURL, "control-url", envOr(getenv, "ROOST_CONTROL_URL", ""),
		"`URL` of the control server for the embedded node to log in to, such as a Headscale server's; Tailscale's by default (env ROOST_CONTROL_URL)")
	fs.StringVar(&cfg.SetupListen, "setup-listen", envOr(getenv, "ROOST_SETUP_LISTEN", DefaultSetupListen),
		"local `address` of the page for logging in to Tailscale in embedded mode (env ROOST_SETUP_LISTEN)")
	fs.IntVar(&cfg.Port, "port", port,
		"`port` to serve on in host mode, on this machine's tailnet IPs (env ROOST_PORT)")
	fs.StringVar(&cfg.DataDir, "data-dir", envOr(getenv, "ROOST_DATA_DIR", DefaultDataDir()),
		"`directory` to keep the database and Tailscale state in (env ROOST_DATA_DIR)")
	fs.StringVar(&cfg.HostRoot, "host-root", envOr(getenv, "ROOST_HOST_ROOT", DefaultHostRoot),
		"`directory` whose proc/ and sys/ are the host's, for Roost in a container that mounts them there (env ROOST_HOST_ROOT)")
	fs.StringVar(&cfg.DevUI, "dev-ui", envOr(getenv, "ROOST_DEV_UI", ""),
		"`URL` of a Vite dev server to proxy the web UI to, for development (env ROOST_DEV_UI)")
	admins := fs.String("admins", getenv("ROOST_ADMINS"),
		"comma-separated Tailscale `logins` to make admins while Roost has none (env ROOST_ADMINS)")
	demo, demoErr := envBool(getenv, "ROOST_DEMO")
	fs.BoolVar(&cfg.Demo, "demo", demo,
		"run against a made-up tailnet with seeded data, for screenshots and trying Roost out; needs no Tailscale (env ROOST_DEMO)")
	fs.StringVar(&cfg.DemoListen, "demo-listen", envOr(getenv, "ROOST_DEMO_LISTEN", DefaultDemoListen),
		"local `address` to serve the demo on (env ROOST_DEMO_LISTEN)")
	devices, devicesErr := envInt(getenv, "ROOST_DEMO_DEVICES", DefaultDemoDevices)
	fs.IntVar(&cfg.DemoDevices, "demo-devices", devices,
		"`number` of devices in the demo's tailnet, Roost's own included (env ROOST_DEMO_DEVICES)")
	days, daysErr := envFloat(getenv, "ROOST_DEMO_DAYS", DefaultDemoDays)
	fs.Float64Var(&cfg.DemoDays, "demo-days", days,
		"`number` of days of history the demo starts with (env ROOST_DEMO_DAYS)")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if portErr != nil && !isSet(fs, "port") {
		return Config{}, portErr
	}
	if demoErr != nil && !isSet(fs, "demo") {
		return Config{}, demoErr
	}
	if devicesErr != nil && !isSet(fs, "demo-devices") {
		return Config{}, devicesErr
	}
	if daysErr != nil && !isSet(fs, "demo-days") {
		return Config{}, daysErr
	}
	cfg.Admins = splitList(*admins)
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg Config) validate() error {
	if err := cfg.validateMode(); err != nil {
		return err
	}
	if cfg.DevUI != "" {
		u, err := url.Parse(cfg.DevUI)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("dev UI %q isn't an http:// URL", cfg.DevUI)
		}
		// The URL shows up in error pages that anyone on the tailnet can see.
		if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("the dev UI URL can't have a user, a query or a fragment")
		}
	}
	// The same rule as store.CleanLogin, which config can't import.
	for _, login := range cfg.Admins {
		if strings.ContainsFunc(login, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return fmt.Errorf("admin %q isn't a login, such as alice@example.com (or alice on a Headscale tailnet)", login)
		}
	}
	return nil
}

// validateMode checks the settings for the way Roost runs: a demo, whose
// tailnet is made up, or else a real one.
func (cfg Config) validateMode() error {
	if cfg.Demo {
		switch {
		case cfg.DemoListen == "":
			return errors.New("no demo address: set --demo-listen or ROOST_DEMO_LISTEN")
		case cfg.DemoDevices < 2 || cfg.DemoDevices > MaxDemoDevices:
			return fmt.Errorf("a demo needs from 2 to %d devices, not %d", MaxDemoDevices, cfg.DemoDevices)
		case !(cfg.DemoDays > 0 && cfg.DemoDays <= MaxDemoDays):
			return fmt.Errorf("a demo starts with up to %d days of history, not %v", MaxDemoDays, cfg.DemoDays)
		}
		return nil
	}
	switch {
	case cfg.Tailscale != Embedded && cfg.Tailscale != Host:
		return fmt.Errorf("unknown Tailscale mode %q: use %q or %q", cfg.Tailscale, Embedded, Host)
	case cfg.Hostname == "":
		return errors.New("no hostname: set --hostname or ROOST_HOSTNAME")
	case cfg.ControlURL != "" && cfg.Tailscale == Host:
		return errors.New("--control-url is for embedded mode: in host mode Roost uses the control server " +
			"this machine's Tailscale is logged in to, which `tailscale up --login-server` sets")
	case cfg.Port < 1 || cfg.Port > 65535:
		return fmt.Errorf("port %d is out of range", cfg.Port)
	case cfg.DataDir == "":
		return errors.New("no data directory: set --data-dir or ROOST_DATA_DIR")
	case !validControlURL(cfg.ControlURL):
		return fmt.Errorf("control URL %q isn't an http:// or https:// URL, such as https://headscale.example.com", cfg.ControlURL)
	case !filepath.IsAbs(cfg.HostRoot):
		return fmt.Errorf("host root %q isn't an absolute path", cfg.HostRoot)
	}
	return nil
}

// validControlURL reports whether raw is empty, which means Tailscale's own
// control server, or the address of another: a web address with nothing but
// the host and maybe a port.
func validControlURL(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	return u.User == nil && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/")
}

// splitList splits a comma-separated list, dropping empty items.
func splitList(s string) []string {
	var items []string
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

// isSet reports whether the flag called name was given.
func isSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) { set = set || f.Name == name })
	return set
}

// DefaultDataDir returns the data directory used when none is set: roost in the
// user's config directory, such as ~/.config/roost on Linux. It returns "" if
// there's no such directory.
func DefaultDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "roost")
}

func envOr(getenv func(string) string, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

// envBool reads a true or false from an environment variable, which counts as
// false if it's unset or empty.
func envBool(getenv func(string) string, key string) (bool, error) {
	v := getenv(key)
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s=%q isn't true or false", key, v)
	}
	return b, nil
}

func envFloat(getenv func(string) string, key string, fallback float64) (float64, error) {
	v := getenv(key)
	if v == "" {
		return fallback, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s=%q isn't a number", key, v)
	}
	return f, nil
}

func envInt(getenv func(string) string, key string, fallback int) (int, error) {
	v := getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s=%q isn't a number", key, v)
	}
	return n, nil
}

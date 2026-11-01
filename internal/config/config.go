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
)

// Config holds the hub's settings.
type Config struct {
	// Tailscale is how Roost joins the tailnet: Embedded or Host.
	Tailscale string
	// Hostname is the embedded node's name on the tailnet.
	Hostname string
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

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if portErr != nil && !isSet(fs, "port") {
		return Config{}, portErr
	}
	cfg.Admins = splitList(*admins)
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg Config) validate() error {
	switch {
	case cfg.Tailscale != Embedded && cfg.Tailscale != Host:
		return fmt.Errorf("unknown Tailscale mode %q: use %q or %q", cfg.Tailscale, Embedded, Host)
	case cfg.Hostname == "":
		return errors.New("no hostname: set --hostname or ROOST_HOSTNAME")
	case cfg.Port < 1 || cfg.Port > 65535:
		return fmt.Errorf("port %d is out of range", cfg.Port)
	case cfg.DataDir == "":
		return errors.New("no data directory: set --data-dir or ROOST_DATA_DIR")
	case !filepath.IsAbs(cfg.HostRoot):
		return fmt.Errorf("host root %q isn't an absolute path", cfg.HostRoot)
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
	for _, login := range cfg.Admins {
		if at := strings.IndexByte(login, '@'); at <= 0 || at == len(login)-1 || strings.ContainsAny(login, " \t") {
			return fmt.Errorf("admin %q isn't a Tailscale login, such as alice@example.com", login)
		}
	}
	return nil
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

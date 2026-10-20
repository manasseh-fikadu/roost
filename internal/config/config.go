// Package config reads Roost's settings from flags and environment variables.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DefaultListen is the address the hub listens on when nothing else is set.
const DefaultListen = "127.0.0.1:4700"

// Config holds the hub's settings.
type Config struct {
	// Listen is the TCP address of the HTTP server.
	Listen string
	// DataDir is the directory that holds the database.
	DataDir string
}

// Parse reads the serve command's flags from args. A flag that isn't given falls
// back to its ROOST_* environment variable, looked up with getenv, and then to
// its default. Usage and errors are written to output.
func Parse(args []string, getenv func(string) string, output io.Writer) (Config, error) {
	fs := flag.NewFlagSet("roost serve", flag.ContinueOnError)
	fs.SetOutput(output)

	var cfg Config
	fs.StringVar(&cfg.Listen, "listen", envOr(getenv, "ROOST_LISTEN", DefaultListen),
		"`address` to serve HTTP on (env ROOST_LISTEN)")
	fs.StringVar(&cfg.DataDir, "data-dir", envOr(getenv, "ROOST_DATA_DIR", DefaultDataDir()),
		"`directory` to keep the database in (env ROOST_DATA_DIR)")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if cfg.DataDir == "" {
		return Config{}, errors.New("no data directory: set --data-dir or ROOST_DATA_DIR")
	}
	return cfg, nil
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

// Package config reads Roost's settings from flags and environment variables.
package config

import (
	"flag"
	"fmt"
	"io"
)

// DefaultListen is the address the hub listens on when nothing else is set.
const DefaultListen = "127.0.0.1:4700"

// Config holds the hub's settings.
type Config struct {
	// Listen is the TCP address of the HTTP server.
	Listen string
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

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return cfg, nil
}

func envOr(getenv func(string) string, key, fallback string) string {
	if v := getenv(key); v != "" {
		return v
	}
	return fallback
}

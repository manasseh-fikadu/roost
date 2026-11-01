package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/manasseh-fikadu/bield/internal/config"
)

// healthTimeout is how long healthcheck waits for the setup page.
const healthTimeout = 5 * time.Second

const healthcheckUsage = `Usage: bield healthcheck [--setup-listen address]

Checks that Bield is running, for Docker's HEALTHCHECK. It asks the setup page
that "bield serve" listens on in embedded mode, and exits 0 if the page answers.
Bield counts as healthy while it waits to be logged in to Tailscale, since a
restart wouldn't help. Host mode serves no setup page, so there's nothing to
check.
`

// healthcheck runs "bield healthcheck".
func healthcheck(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("bield healthcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, healthcheckUsage) }
	listen := config.DefaultSetupListen
	if v := os.Getenv("BIELD_SETUP_LISTEN"); v != "" {
		listen = v
	}
	fs.StringVar(&listen, "setup-listen", listen, "`address` of the setup page, as bield serve was given it (env BIELD_SETUP_LISTEN)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return errUsage
	}
	target, err := healthURL(listen)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	//nolint:gosec // G704: the address names the setup page on this machine, from its own flag or environment.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	// No proxy: the page is on this machine.
	client := &http.Client{
		Transport:     &http.Transport{},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	res, err := client.Do(req) //nolint:gosec // G704: see above.
	if err != nil {
		return fmt.Errorf("no answer from Bield on %s: %w", listen, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("the setup page on %s answered %s", listen, res.Status)
	}
	fmt.Fprintln(stdout, "Bield is running")
	return nil
}

// healthURL returns the URL of the setup page that listens on listen. A
// listener on every address is reached on the loopback one.
func healthURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("setup address %q isn't like 127.0.0.1:4700: %w", listen, err)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("setup address %q has no port to check", listen)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/", nil
}

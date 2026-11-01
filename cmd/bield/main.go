// Command bield runs Bield, a self-hosted control plane for the machines on a tailnet.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
)

// version is set at build time with -ldflags "-X main.version=...".
var version string

const usageText = `Usage: bield <command> [flags]

Commands:
  serve        Run the Bield hub
  admin        Add or list Bield's admins
  backup       Save a copy of Bield's data
  healthcheck  Check that a running Bield answers (for Docker)
  version      Print the version

Run "bield <command> -h" to see a command's flags.
`

// errUsage means the usage text has already been printed.
var errUsage = errors.New("usage")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
	case errors.Is(err, errUsage):
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "bield:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return errUsage
	}
	switch args[0] {
	case "serve":
		return serve(ctx, args[1:], stderr)
	case "admin":
		return admin(ctx, args[1:], stdout, stderr)
	case "backup":
		return backup(ctx, args[1:], stdout, stderr)
	case "healthcheck":
		return healthcheck(ctx, args[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, buildVersion())
		return nil
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usageText)
		return nil
	default:
		fmt.Fprintf(stderr, "bield: unknown command %q\n\n%s", args[0], usageText)
		return errUsage
	}
}

// buildVersion returns the version stamped in by the Makefile, or else the module
// version that go install records, or "dev".
func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

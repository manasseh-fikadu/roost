// Command roost runs Roost, a self-hosted control plane for the machines on a tailnet.
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

const usage = `Usage: roost <command> [flags]

Commands:
  serve     Run the Roost hub
  version   Print the version

Run "roost <command> -h" to see a command's flags.
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
		fmt.Fprintln(os.Stderr, "roost:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return errUsage
	}
	switch args[0] {
	case "serve":
		return serve(ctx, args[1:], stderr)
	case "version":
		fmt.Fprintln(stdout, buildVersion())
		return nil
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usage)
		return nil
	default:
		fmt.Fprintf(stderr, "roost: unknown command %q\n\n%s", args[0], usage)
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

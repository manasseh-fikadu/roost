package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/manasseh-fikadu/roost/internal/config"
	"github.com/manasseh-fikadu/roost/internal/store"
)

const adminUsage = `Usage:
  roost admin add [--data-dir directory] <login>   Make a Tailscale login an admin
  roost admin list [--data-dir directory]          List the admins

Roost makes its first admins itself. Use "roost admin add" when no one can
sign in as an admin, such as when Roost's node is tagged. It works while
Roost is running.
`

// admin runs "roost admin", which changes the database directly, so it's the
// way back in when no admin can sign in.
func admin(ctx context.Context, args []string, stdout, stderr io.Writer) (err error) {
	if len(args) == 0 || (args[0] != "add" && args[0] != "list") {
		fmt.Fprint(stderr, adminUsage)
		return errUsage
	}
	cmd := args[0]
	fs := flag.NewFlagSet("roost admin "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dataDir := os.Getenv("ROOST_DATA_DIR")
	if dataDir == "" {
		dataDir = config.DefaultDataDir()
	}
	fs.StringVar(&dataDir, "data-dir", dataDir, "`directory` that holds Roost's database (env ROOST_DATA_DIR)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	wantArgs := 0
	if cmd == "add" {
		wantArgs = 1
	}
	if fs.NArg() != wantArgs {
		fmt.Fprint(stderr, adminUsage)
		return errUsage
	}
	if dataDir == "" {
		return errors.New("no data directory: set --data-dir or ROOST_DATA_DIR")
	}

	dbPath := filepath.Join(dataDir, "roost.db")
	st, err := store.Open(ctx, dbPath, store.Options{})
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer func() { err = errors.Join(err, st.Close()) }()

	if cmd == "list" {
		logins, err := st.Admins(ctx)
		if err != nil {
			return err
		}
		if len(logins) == 0 {
			fmt.Fprintln(stderr, "Roost has no admins yet")
		}
		for _, login := range logins {
			fmt.Fprintln(stdout, login)
		}
		return nil
	}

	login := strings.TrimSpace(fs.Arg(0))
	added, err := st.AddAdmin(ctx, login)
	if err != nil {
		return err
	}
	if added {
		fmt.Fprintf(stdout, "%s is now an admin\n", login)
	} else {
		fmt.Fprintf(stdout, "%s was already an admin\n", login)
	}
	return nil
}

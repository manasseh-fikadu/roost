package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/manasseh-fikadu/roost/internal/config"
	"github.com/manasseh-fikadu/roost/internal/store"
)

const adminUsage = `Usage:
  roost admin list [--data-dir directory]                List the admins and viewers
  roost admin add [--data-dir directory] <login>         Make a Tailscale login an admin
  roost admin add-viewer [--data-dir directory] <login>  Make a Tailscale login a viewer
  roost admin remove [--data-dir directory] <login>      Take a login's role away
  roost admin who-can-view [--data-dir directory] [tailnet|listed]
                                                         Show or set who can view Roost

Admins can do everything. Viewers see the dashboard, but can't change
anything or see logs, files or secrets. With who-can-view set to tailnet,
the default, anyone who signs in from the tailnet is a viewer. With listed,
only the admins and viewers can view Roost.

Roost makes its first admins itself. Use "roost admin add" when no one can
sign in as an admin, such as when Roost's node is tagged. Changes apply at
once, even while Roost is running.
`

// adminArgs is how many arguments each admin command takes, at least and at most.
var adminArgs = map[string][2]int{
	"list":         {0, 0},
	"add":          {1, 1},
	"add-viewer":   {1, 1},
	"remove":       {1, 1},
	"who-can-view": {0, 1},
}

// admin runs "roost admin", which changes the database directly, so it's the
// way back in when no admin can sign in.
func admin(ctx context.Context, args []string, stdout, stderr io.Writer) (err error) {
	if len(args) == 0 {
		fmt.Fprint(stderr, adminUsage)
		return errUsage
	}
	cmd := args[0]
	nargs, ok := adminArgs[cmd]
	if !ok {
		fmt.Fprint(stderr, adminUsage)
		return errUsage
	}
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
	if fs.NArg() < nargs[0] || fs.NArg() > nargs[1] {
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

	switch cmd {
	case "list":
		return listUsers(ctx, st, stdout, stderr)
	case "add":
		return setRole(ctx, st, fs.Arg(0), store.Admin, stdout)
	case "add-viewer":
		return setRole(ctx, st, fs.Arg(0), store.Viewer, stdout)
	case "remove":
		login, err := store.CleanLogin(fs.Arg(0))
		if err != nil {
			return err
		}
		removed, err := st.RemoveUser(ctx, login)
		if err != nil {
			return err
		}
		if removed {
			fmt.Fprintf(stdout, "%s no longer has a role\n", login)
		} else {
			fmt.Fprintf(stdout, "%s had no role\n", login)
		}
		return nil
	default: // who-can-view
		return whoCanView(ctx, st, fs.Arg(0), stdout)
	}
}

func listUsers(ctx context.Context, st *store.Store, stdout, stderr io.Writer) error {
	users, err := st.Users(ctx)
	if err != nil {
		return err
	}
	who, err := st.WhoCanView(ctx)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, u := range users {
		fmt.Fprintf(tw, "%s\t%s\n", u.Login, u.Role)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(users) == 0 {
		fmt.Fprintln(stderr, "Roost has no admins or viewers yet")
	}
	if who == store.Tailnet {
		fmt.Fprintln(stderr, "Anyone who signs in from the tailnet can view Roost too")
	} else {
		fmt.Fprintln(stderr, "No one else can view Roost")
	}
	return nil
}

func setRole(ctx context.Context, st *store.Store, login string, role store.Role, stdout io.Writer) error {
	login, err := store.CleanLogin(login)
	if err != nil {
		return err
	}
	changed, err := st.SetRole(ctx, login, role)
	if err != nil {
		return err
	}
	name := "a viewer"
	if role == store.Admin {
		name = "an admin"
	}
	if changed {
		fmt.Fprintf(stdout, "%s is now %s\n", login, name)
	} else {
		fmt.Fprintf(stdout, "%s was already %s\n", login, name)
	}
	return nil
}

// whoCanView prints who can view Roost, or sets it to who.
func whoCanView(ctx context.Context, st *store.Store, who string, stdout io.Writer) error {
	if who == "" {
		who, err := st.WhoCanView(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, who)
		return nil
	}
	if _, err := st.SetWhoCanView(ctx, who); err != nil {
		return err
	}
	if who == store.Tailnet {
		fmt.Fprintln(stdout, "Anyone who signs in from the tailnet can view Roost")
	} else {
		fmt.Fprintln(stdout, "Only Roost's admins and viewers can view it")
	}
	return nil
}

package systemd

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// An Installed is a service that the machine has, for picking which ones to
// watch.
type Installed struct {
	// Name is the unit's ID, such as "ssh.service".
	Name  string
	Scope Scope
	// Description is what systemd says the service is, if it's loaded.
	Description string
	// Active is systemd's ActiveState, such as "active" or "failed", or ""
	// for a service that is installed and not loaded.
	Active string
}

// Key names the service with its scope, as Unit.Key does.
func (i Installed) Key() string { return Unit{Name: i.Name, Scope: i.Scope}.Key() }

// Installed lists the services on the machine, the system's and the user's,
// by name: the ones systemd has loaded, such as the running and the failed,
// and the ones installed to be enabled or disabled. It fails only if systemd
// can't say about any scope.
func (c *Collector) Installed(ctx context.Context) ([]Installed, error) {
	scopes := []Scope{System}
	if c.userScope {
		scopes = append(scopes, User)
	}
	var (
		out  []Installed
		errs []error
	)
	for _, scope := range scopes {
		services, err := c.installed(ctx, scope)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, services...)
	}
	if len(errs) == len(scopes) {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

func (c *Collector) installed(ctx context.Context, scope Scope) ([]Installed, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var flags []string
	if scope == User {
		flags = []string{"--user"}
	}
	list := func(verb string) ([]byte, error) {
		return c.run(ctx, append(slices.Clone(flags), verb, "--type=service", "--plain", "--no-legend", "--no-pager", "--full")...)
	}
	loaded, err := c.run(ctx, append(slices.Clone(flags), "list-units", "--type=service", "--all",
		"--plain", "--no-legend", "--no-pager", "--full")...)
	if err != nil {
		return nil, fmt.Errorf("list the %s's services: %w", scopeName(scope), err)
	}
	files, err := list("list-unit-files")
	if err != nil {
		return nil, fmt.Errorf("list the %s's installed services: %w", scopeName(scope), err)
	}

	byName := map[string]Installed{}
	for _, f := range parseTable(loaded) {
		// An unloaded unit that is only named by another is "not-found".
		if len(f) < 4 || f[1] != "loaded" || !installable(f[0]) {
			continue
		}
		byName[f[0]] = Installed{Name: f[0], Scope: scope, Active: f[2], Description: strings.Join(f[4:], " ")}
	}
	for _, f := range parseTable(files) {
		if len(f) < 2 || !installable(f[0]) || (f[1] != "enabled" && f[1] != "disabled") {
			continue
		}
		if _, ok := byName[f[0]]; !ok {
			byName[f[0]] = Installed{Name: f[0], Scope: scope}
		}
	}
	out := make([]Installed, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Installed) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

// installable reports whether a unit name is a service that can be watched:
// not a template, such as getty@.service.
func installable(name string) bool {
	return strings.HasSuffix(name, ".service") && !strings.HasSuffix(name, "@.service")
}

// parseTable reads the fields of each line of `systemctl list-units
// --plain --no-legend`, or of list-unit-files.
func parseTable(out []byte) [][]string {
	var rows [][]string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) > 0 {
			f[0] = strings.TrimPrefix(f[0], "●")
			if f[0] == "" && len(f) > 1 {
				f = f[1:]
			}
			rows = append(rows, f)
		}
	}
	return rows
}

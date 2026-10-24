package systemd_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/systemd"
)

// fakeSystemctl answers like systemctl, from the units of each scope it
// holds. A unit that isn't there doesn't exist.
type fakeSystemctl struct {
	mu    sync.Mutex
	units map[systemd.Scope]map[string]fakeUnit
	// aliases point another name at a unit, as sshd.service is ssh.service.
	aliases map[string]string
	// fail makes a scope's runs fail.
	fail map[systemd.Scope]error
	runs []string
}

type fakeUnit struct {
	active, sub, desc string
}

func (f *fakeSystemctl) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, strings.Join(args, " "))
	scope := systemd.System
	if args[0] == "--user" {
		scope, args = systemd.User, args[1:]
	}
	if err := f.fail[scope]; err != nil {
		return nil, err
	}
	var out strings.Builder
	switch args[0] {
	case "list-units":
		for _, name := range sortedKeys(f.units[scope]) {
			if u := f.units[scope][name]; u.active == "failed" {
				fmt.Fprintf(&out, "%s loaded failed %s %s\n", name, u.sub, u.desc)
			}
		}
	case "show":
		names := args[slices.Index(args, "--")+1:]
		for i, name := range names {
			if i > 0 {
				out.WriteString("\n")
			}
			id := name
			if alias, ok := f.aliases[name]; ok {
				id = alias
			}
			u, ok := f.units[scope][id]
			if !ok {
				fmt.Fprintf(&out, "Id=%s\nLoadState=not-found\nActiveState=inactive\nSubState=dead\nMemoryCurrent=[not set]\n", id)
				continue
			}
			fmt.Fprintf(&out, "Id=%s\nLoadState=loaded\nDescription=%s\nActiveState=%s\nSubState=%s\n"+
				"ActiveEnterTimestamp=@1800000000\nMainPID=42\nMemoryCurrent=1048576\n", id, u.desc, u.active, u.sub)
		}
	default:
		return nil, fmt.Errorf("fake systemctl can't %q", args[0])
	}
	return []byte(out.String()), nil
}

func (f *fakeSystemctl) set(scope systemd.Scope, name string, u fakeUnit) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.units[scope][name] = u
}

func (f *fakeSystemctl) failScope(scope systemd.Scope, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[scope] = err
}

func sortedKeys(m map[string]fakeUnit) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// summary writes units as "key active/sub", with a * for watched ones.
func summary(units []systemd.Unit) []string {
	var out []string
	for _, u := range units {
		s := fmt.Sprintf("%s %s/%s", u.Key(), u.Active, u.Sub)
		if u.Watched {
			s += " *"
		}
		out = append(out, s)
	}
	return out
}

func newFake() *fakeSystemctl {
	return &fakeSystemctl{
		units: map[systemd.Scope]map[string]fakeUnit{
			systemd.System: {
				"ssh.service":    {"active", "running", "OpenBSD Secure Shell server"},
				"docker.service": {"active", "running", "Docker Application Container Engine"},
				"nginx.service":  {"failed", "failed", "A high performance web server"},
				"cron.service":   {"inactive", "dead", "Regular background program processing daemon"},
			},
			systemd.User: {
				"app.service":    {"active", "running", "My app"},
				"backup.service": {"failed", "failed", "Nightly backup"},
			},
		},
		aliases: map[string]string{"sshd.service": "ssh.service"},
		fail:    map[systemd.Scope]error{},
	}
}

func TestCollector(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		f := newFake()
		c := systemd.NewCollector(systemd.Options{
			Run:       f.run,
			UserScope: true,
			Watched: func() []string {
				return []string{"ssh.service", "sshd.service", "cron.service", "missing.service", "user:app.service"}
			},
		})
		if r := c.Reading(); r.Units != nil || !r.At.IsZero() {
			t.Errorf("Reading() before Run = %+v, want nothing yet", r)
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go c.Run(ctx)
		synctest.Wait()
		<-c.Changes()

		r := c.Reading()
		want := []string{
			"cron.service inactive/dead *",
			"nginx.service failed/failed",
			"ssh.service active/running *",
			"user:app.service active/running *",
			"user:backup.service failed/failed",
		}
		if got := summary(r.Units); !slices.Equal(got, want) || r.Err != nil || !r.At.Equal(time.Now()) {
			t.Errorf("Reading() = %q, %v at %v, want %q now", got, r.Err, r.At, want)
		}
		ssh := r.Units[2]
		if ssh.Description != "OpenBSD Secure Shell server" || ssh.MainPID != 42 || ssh.Memory != 1<<20 || !ssh.Since.Equal(time.Unix(1_800_000_000, 0)) {
			t.Errorf("ssh.service = %+v, want its details", ssh)
		}

		// A scope that fails keeps its units from before.
		f.set(systemd.System, "nginx.service", fakeUnit{"active", "running", "A high performance web server"})
		f.failScope(systemd.User, errors.New("Failed to connect to bus"))
		time.Sleep(systemd.Interval)
		synctest.Wait()
		r = c.Reading()
		want = []string{"cron.service inactive/dead *", "ssh.service active/running *", "user:app.service active/running *", "user:backup.service failed/failed"}
		if got := summary(r.Units); !slices.Equal(got, want) || r.Err == nil || !strings.Contains(r.Err.Error(), "user") {
			t.Errorf("after the user scope failed, Reading() = %q, %v, want %q and an error about user units", got, r.Err, want)
		}
		if !r.At.Equal(time.Now().Add(-systemd.Interval)) {
			t.Errorf("Reading().At = %v, want the last run that fully worked", r.At)
		}
	})
}

func TestCollector_withoutUserScope(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		f := newFake()
		c := systemd.NewCollector(systemd.Options{Run: f.run, Watched: func() []string { return []string{"user:app.service"} }})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go c.Run(ctx)
		synctest.Wait()
		r := c.Reading()
		if got := summary(r.Units); !slices.Equal(got, []string{"nginx.service failed/failed"}) || r.Err == nil {
			t.Errorf("Reading() = %q, %v, want the system's failed unit and an error about the user unit", got, r.Err)
		}
		for _, run := range f.runs {
			if strings.HasPrefix(run, "--user") {
				t.Errorf("ran systemctl %s without a user scope", run)
			}
		}
	})
}

func TestCollector_nothingToShow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		f := newFake()
		f.set(systemd.System, "nginx.service", fakeUnit{"active", "running", ""})
		c := systemd.NewCollector(systemd.Options{Run: f.run})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go c.Run(ctx)
		synctest.Wait()
		if r := c.Reading(); r.Units == nil || len(r.Units) != 0 || r.Err != nil {
			t.Errorf("Reading() = %+v, want no units, and no error", r)
		}
		if len(f.runs) != 1 {
			t.Errorf("systemctl ran %q, want only list-units with nothing to show", f.runs)
		}
	})
}

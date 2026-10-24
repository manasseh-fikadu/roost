package ssh

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestQuote(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, in, want string
	}{
		{name: "plain", in: "web-1", want: "'web-1'"},
		{name: "empty", in: "", want: "''"},
		{name: "single quote", in: "it's", want: `'it'\''s'`},
		{name: "only quotes", in: "''", want: `''\'''\'''`},
		{name: "shell syntax", in: "$(reboot) `id` ; | & > *", want: "'$(reboot) `id` ; | & > *'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Quote(tt.in); got != tt.want {
				t.Errorf("Quote(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestQuote_panics(t *testing.T) {
	t.Parallel()
	for _, in := range []string{`a\b`, "a\nb", "a\tb", "a\x00b"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Quote(%q) didn't panic", in)
				}
			}()
			Quote(in)
		}()
	}
}

func TestCheckSession(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		in   string
		ok   bool
	}{
		{name: "web name", in: "web-1", ok: true},
		{name: "spaces and quotes", in: `my "it's" session`, ok: true},
		{name: "unicode", in: "työ", ok: true},
		{name: "starts with a dash", in: "-t", ok: true},
		{name: "64 characters", in: strings.Repeat("é", 64), ok: true},
		{name: "empty", in: ""},
		{name: "65 characters", in: strings.Repeat("a", 65)},
		// tmux would make these an underscore, so the name wouldn't match.
		{name: "colon", in: "a:b"},
		// tmux reads a # in the name it's given as the start of a format.
		{name: "hash", in: "a#b"},
		{name: "format", in: "a#{host}b"},
		{name: "command format", in: "#(id)"},
		{name: "escaped hash", in: "x##S"},
		{name: "dot", in: "a.b"},
		{name: "backslash", in: `a\b`},
		{name: "newline", in: "a\nb"},
		{name: "invalid UTF-8", in: "a\xffb"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := CheckSession(tt.in)
			if tt.ok && err != nil || !tt.ok && !errors.Is(err, ErrBadSession) {
				t.Errorf("CheckSession(%q) = %v, want ok: %v", tt.in, err, tt.ok)
			}
		})
	}
}

func TestCheckDir(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		in   string
		ok   bool
	}{
		{name: "absolute", in: "/srv/app", ok: true},
		{name: "root", in: "/", ok: true},
		{name: "spaces and quotes", in: "/home/a/it's here", ok: true},
		{name: "relative", in: "srv/app"},
		{name: "home", in: "~/app"},
		{name: "empty", in: ""},
		{name: "backslash", in: `/a\b`},
		{name: "newline", in: "/a\nb"},
		{name: "too long", in: "/" + strings.Repeat("a", 1024)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := CheckDir(tt.in)
			if tt.ok && err != nil || !tt.ok && !errors.Is(err, ErrBadDir) {
				t.Errorf("CheckDir(%q) = %v, want ok: %v", tt.in, err, tt.ok)
			}
		})
	}
}

// shells are the login shells a user may have, which sshd runs Roost's
// commands with. The tests use those on the machine.
var shells = []string{"sh", "dash", "bash", "zsh", "fish"}

// fakeTools makes a directory with a tmux that prints its working directory
// as "cwd=…", and then its arguments, one per line, and a login shell that
// prints its working directory, and returns it.
func fakeTools(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	tools := map[string]string{
		"tmux":  "#!/bin/sh\nprintf 'cwd=%s\\n' \"$(pwd -P)\"\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done\n",
		"login": "#!/bin/sh\npwd\n",
	}
	for name, script := range tools {
		//nolint:gosec // G306: a fake tool has to be executable.
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return bin
}

// runIn runs cmd with shell -c in the directory start, as sshd does, with
// bin first on the PATH, and returns its output's lines. It fails if cmd ran
// anything else.
func runIn(t *testing.T, shell, bin, start, cmd string) []string {
	t.Helper()
	path, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s isn't installed", shell)
	}
	c := exec.CommandContext(t.Context(), path, "-c", cmd) //nolint:gosec // G204: the test runs Roost's commands in each shell.
	c.Dir = start
	c.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "SHELL="+filepath.Join(bin, "login"))
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		t.Fatalf("%s -c %s: %v\n%s", shell, cmd, err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "pwned")); err == nil {
		t.Errorf("%s -c %s ran a command it was given as a value", shell, cmd)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}

// awkward are names and directories that shells would read as more than
// one word, or as syntax, unless they're quoted right.
var awkward = []string{
	"web-1",
	"it's",
	"''",
	"two  words",
	`"double"`,
	"$(touch pwned)",
	"`touch pwned`",
	"$HOME",
	"; touch pwned",
	"; touch pwned #",
	"a#{host}b",
	"a##b",
	"#(touch pwned)",
	"a|b&c>d<e",
	"*",
	"~",
	"-t",
	"{a,b}",
	"!!",
	"työ ☃",
}

// startDir returns a new directory to start a command in, with its symbolic
// links resolved, as pwd -P prints it.
func startDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// tmuxCall splits the fake tmux's output into the directory it ran in and
// its arguments.
func tmuxCall(t *testing.T, lines []string) (cwd string, args []string) {
	t.Helper()
	cwd, ok := strings.CutPrefix(lines[0], "cwd=")
	if !ok {
		t.Fatalf("tmux printed %q, want its directory first", lines)
	}
	return cwd, lines[1:]
}

func TestTerminalCommand_tmux(t *testing.T) {
	t.Parallel()
	bin := fakeTools(t)
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			start := startDir(t)
			// The name goes to tmux as it is, in the directory the command
			// started in. tmux reads # as a format, so names with one are
			// refused.
			for _, name := range awkward {
				cmd, err := terminalCommand(name, "")
				if CheckSession(name) != nil {
					if !errors.Is(err, ErrBadSession) {
						t.Errorf("terminalCommand(%q) = %v, want ErrBadSession", name, err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("terminalCommand(%q): %v", name, err)
				}
				cwd, args := tmuxCall(t, runIn(t, shell, bin, start, cmd))
				want := []string{"new-session", "-A", "-s", name, ";", "set-option", "-q", "mouse", "on"}
				if !slices.Equal(args, want) || cwd != start {
					t.Errorf("session %q: tmux ran in %q with %q, want %q with %q", name, cwd, args, start, want)
				}
			}
		})
	}
}

// A new session starts in the directory because the command goes there
// first: tmux reads -c as a format, so `a#{host}b` would start in another.
func TestTerminalCommand_tmuxDirectory(t *testing.T) {
	t.Parallel()
	bin := fakeTools(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			start := startDir(t)
			for _, name := range awkward {
				dir := filepath.Join(root, name)
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				cmd, err := terminalCommand("web-1", dir)
				if err != nil {
					t.Fatalf("terminalCommand(web-1, %q): %v", dir, err)
				}
				cwd, args := tmuxCall(t, runIn(t, shell, bin, start, cmd))
				want := []string{"new-session", "-A", "-s", "web-1", ";", "set-option", "-q", "mouse", "on"}
				if cwd != dir || !slices.Equal(args, want) {
					t.Errorf("dir %q: tmux ran in %q with %q, want %q with %q (and no -c)", dir, cwd, args, dir, want)
				}
			}
			// A directory that's gone leaves the session where sshd put it.
			cmd, err := terminalCommand("web-1", filepath.Join(root, "gone"))
			if err != nil {
				t.Fatal(err)
			}
			if cwd, _ := tmuxCall(t, runIn(t, shell, bin, start, cmd)); cwd != start {
				t.Errorf("a missing directory: tmux ran in %q, want %q", cwd, start)
			}
		})
	}
}

func TestTerminalCommand_shell(t *testing.T) {
	t.Parallel()
	bin := fakeTools(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			start := startDir(t)
			for _, name := range awkward {
				dir := filepath.Join(root, name)
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				cmd, err := terminalCommand("", dir)
				if err != nil {
					t.Fatalf("terminalCommand(%q): %v", dir, err)
				}
				if got := runIn(t, shell, bin, start, cmd); !slices.Equal(got, []string{dir}) {
					t.Errorf("the shell started in %q, want %q", got, dir)
				}
			}
		})
	}
}

func TestTerminalCommand_loginShell(t *testing.T) {
	t.Parallel()
	cmd, err := terminalCommand("", "")
	if err != nil || cmd != "" {
		t.Errorf(`terminalCommand("", "") = %q, %v, want "" for sshd's own login shell`, cmd, err)
	}
	if _, err := terminalCommand("a:b", ""); !errors.Is(err, ErrBadSession) {
		t.Errorf("a bad session name: %v, want ErrBadSession", err)
	}
	if _, err := terminalCommand("", "relative"); !errors.Is(err, ErrBadDir) {
		t.Errorf("a bad directory: %v, want ErrBadDir", err)
	}
}

func TestKillScript(t *testing.T) {
	t.Parallel()
	bin := fakeTools(t)
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			start := startDir(t)
			for _, name := range awkward {
				_, got := tmuxCall(t, runIn(t, shell, bin, start, command(extraPath+killScript, name)))
				if want := []string{"kill-session", "-t", "=" + name}; !slices.Equal(got, want) {
					t.Errorf("tmux got %q, want %q", got, want)
				}
			}
		})
	}
}

func TestScripts_noBackslashes(t *testing.T) {
	t.Parallel()
	// Fish reads a backslash in single quotes as an escape.
	for name, script := range map[string]string{
		"tmux": tmuxScript, "shell": shellScript, "list": listScript, "kill": killScript, "check": checkScript,
	} {
		if strings.ContainsFunc(script, func(r rune) bool { return r == '\\' || r < ' ' }) {
			t.Errorf("the %s script has a backslash or control character: %s", name, script)
		}
	}
}

func TestSetupCommand(t *testing.T) {
	t.Parallel()
	line := `from="100.64.0.1",no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample roost@roost`
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			path, err := exec.LookPath(shell)
			if err != nil {
				t.Skipf("%s isn't installed", shell)
			}
			home := t.TempDir()
			keys := filepath.Join(home, ".ssh", "authorized_keys")
			// The command makes them private.
			if err := os.MkdirAll(filepath.Dir(keys), 0o755); err != nil { //nolint:gosec // G301: see above.
				t.Fatal(err)
			}
			if err := os.WriteFile(keys, []byte("ssh-ed25519 AAAAOther other@laptop\n"), 0o644); err != nil { //nolint:gosec // G306: see above.
				t.Fatal(err)
			}
			// Running it twice adds the line once.
			for range 2 {
				c := exec.CommandContext(t.Context(), path, "-c", SetupCommand(line)) //nolint:gosec // G204: as in runIn.
				c.Env = append(os.Environ(), "HOME="+home)
				if out, err := c.CombinedOutput(); err != nil {
					t.Fatalf("%s: %v\n%s", shell, err, out)
				}
			}
			got, err := os.ReadFile(filepath.Clean(keys))
			if err != nil {
				t.Fatal(err)
			}
			if want := "ssh-ed25519 AAAAOther other@laptop\n" + line + "\n"; string(got) != want {
				t.Errorf("authorized_keys =\n%s\nwant\n%s", got, want)
			}
			for path, want := range map[string]os.FileMode{filepath.Dir(keys): 0o700, keys: 0o600} {
				if info, err := os.Stat(path); err != nil || info.Mode().Perm() != want {
					t.Errorf("%s: mode %v, %v, want %v", path, info.Mode().Perm(), err, want)
				}
			}
		})
	}
}

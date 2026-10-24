package ssh

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// sshd runs each command Roost sends with the login shell of the user Roost
// logs in as, which may be sh, bash, zsh or fish. Fish reads a backslash in
// single quotes differently from the others, so Roost never sends one there:
// every command is a fixed sh script with no backslashes, and the values it
// works on, such as a session's name, go to sh as its arguments, each in
// single quotes. A value can't have a backslash or a control character, such
// as a newline. A single quote in a value becomes '\'', whose backslash is
// outside the quotes, where every one of those shells reads it the same.

// Quote returns s in single quotes for a POSIX shell, or fish, as one word.
// It panics if s has a backslash or a control character, which the shells
// don't agree on: check values with Clean first.
func Quote(s string) string {
	if strings.ContainsFunc(s, func(r rune) bool { return r == '\\' || unicode.IsControl(r) }) {
		panic("ssh.Quote: a backslash or control character in " + s)
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// command returns a command that runs script in sh with args as $1, $2 and
// so on, whatever the user's login shell is. The script must have no
// backslashes or control characters, which Quote checks.
func command(script string, args ...string) string {
	var b strings.Builder
	b.WriteString("exec sh -c " + Quote(script) + " sh")
	for _, a := range args {
		b.WriteString(" " + Quote(a))
	}
	return b.String()
}

// extraPath is added to the PATH of every script, after the system's
// directories, since sshd's PATH misses where Homebrew, MacPorts and Nix put
// tmux.
const extraPath = `PATH="$PATH:/usr/local/bin:/opt/homebrew/bin:/opt/local/bin:/home/linuxbrew/.linuxbrew/bin:$HOME/.nix-profile/bin"; `

// The longest a tmux session's name and a working directory can be.
const (
	maxSessionName = 64
	maxDir         = 1024
)

// Why a session name or working directory is refused.
var (
	ErrBadSession = errors.New("a session's name must be 1 to 64 characters, without a colon, dot, backslash or control character")
	ErrBadDir     = errors.New("the working directory must be an absolute path, without a backslash or control character")
)

// CheckSession returns ErrBadSession unless name can be a tmux session's
// name as it is: tmux turns a colon or dot into an underscore, and shows a
// backslash or control character escaped.
func CheckSession(name string) error {
	if name == "" || utf8.RuneCountInString(name) > maxSessionName || !utf8.ValidString(name) ||
		strings.ContainsAny(name, ":.\\") || strings.ContainsFunc(name, unicode.IsControl) {
		return ErrBadSession
	}
	return nil
}

// CheckDir returns ErrBadDir unless dir is an absolute path that Roost can
// send to a shell.
func CheckDir(dir string) error {
	if !strings.HasPrefix(dir, "/") || len(dir) > maxDir || !utf8.ValidString(dir) ||
		strings.ContainsRune(dir, '\\') || strings.ContainsFunc(dir, unicode.IsControl) {
		return ErrBadDir
	}
	return nil
}

// The scripts that terminals run. Each takes the session's name, if it has
// one, and then the working directory, which may be "". tmux only uses the
// directory when it makes the session: attaching to one keeps its own.
// Mouse mode lets the wheel scroll tmux's history, and Shift-drag still
// selects text in the browser. A ";" on its own is how tmux runs one command
// after another.
const (
	tmuxScript = extraPath +
		`if [ -n "$2" ]; then exec tmux new-session -A -s "$1" -c "$2" ";" set-option -q mouse on; fi; ` +
		`exec tmux new-session -A -s "$1" ";" set-option -q mouse on`
	shellScript = `if [ -n "$1" ]; then cd "$1"; fi; exec "${SHELL:-/bin/sh}" -l`
)

// terminalCommand returns the command that starts a terminal: attached to
// the tmux session called session, which it makes if there's none, or a
// login shell if session is "". It starts in dir, unless dir is "". It
// returns "" for a plain login shell, which sshd starts by itself.
func terminalCommand(session, dir string) (string, error) {
	if session != "" {
		if err := CheckSession(session); err != nil {
			return "", err
		}
	}
	if dir != "" {
		if err := CheckDir(dir); err != nil {
			return "", err
		}
	}
	switch {
	case session != "":
		return command(tmuxScript, session, dir), nil
	case dir != "":
		return command(shellScript, dir), nil
	default:
		return "", nil
	}
}

// sessionFields are what Sessions reads of each tmux session, separated by
// colons, which a session's name can't have. The pane's command comes last,
// so a colon in it can't move the other fields.
const sessionFields = "#{session_name}:#{session_created}:#{session_activity}:#{session_attached}:#{session_windows}:#{pane_current_command}"

// listScript lists the tmux sessions, or exits with 127 if there's no tmux.
// With no tmux server running, there are none, and list-sessions fails.
const listScript = extraPath + `command -v tmux >/dev/null 2>&1 || exit 127; tmux list-sessions -F "` + sessionFields + `" 2>/dev/null; exit 0`

// killScript ends the tmux session whose name is $1, and no other: "=" asks
// tmux for that name exactly, not one it starts.
const killScript = extraPath + `exec tmux kill-session -t "=$1"`

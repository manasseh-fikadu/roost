package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// The largest terminal Roost asks for, in columns and rows.
const (
	MaxCols = 500
	MaxRows = 200
)

// TerminalOptions says what terminal to open.
type TerminalOptions struct {
	// Session is the tmux session to attach to, which is made if there's
	// none. If it's "", the terminal is a login shell that ends when it's
	// closed.
	Session string
	// Dir is the directory to start in, or "" for the user's home. tmux
	// only uses it when it makes the session.
	Dir string
	// Cols and Rows are the terminal's size.
	Cols, Rows int
}

// Terminal is a shell on a device, on a connection of its own. Read its
// output, which has the shell's errors in it too, and write its input.
type Terminal struct {
	io.Reader
	io.Writer
	output  *io.PipeReader
	client  *gossh.Client
	session *gossh.Session
	// exited is closed once the shell is done, with its status in code if
	// it said one: hasCode.
	exited  chan struct{}
	code    int
	hasCode bool
	done    func()
	once    sync.Once
}

// OpenTerminal opens a terminal on a device. The terminal closes with the
// connection, or when the device's access changes or is removed.
func (m *Manager) OpenTerminal(ctx context.Context, deviceID string, opts TerminalOptions) (*Terminal, error) {
	cmd, err := terminalCommand(opts.Session, opts.Dir)
	if err != nil {
		return nil, err
	}
	t, err := m.target(deviceID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	a, ok := m.access[t.id]
	gen := m.gen[t.id]
	m.mu.Unlock()
	if !ok {
		return nil, ErrNotSetUp
	}
	client, err := m.connect(ctx, t, a)
	m.record(t.id, gen, err)
	if err != nil {
		return nil, err
	}
	if !m.addTerminal(t.id, gen, client) {
		_ = client.Close()
		return nil, ErrNotSetUp
	}
	term, err := startTerminal(client, cmd, opts)
	if err != nil {
		m.removeTerminal(t.id, client)
		_ = client.Close()
		return nil, err
	}
	term.done = func() { m.removeTerminal(t.id, client) }
	return term, nil
}

// addTerminal keeps client to close if the machine's access changes, unless
// it changed since gen.
func (m *Manager) addTerminal(id string, gen uint64, client *gossh.Client) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gen[id] != gen {
		return false
	}
	if m.terminals[id] == nil {
		m.terminals[id] = map[*gossh.Client]struct{}{}
	}
	m.terminals[id][client] = struct{}{}
	return true
}

func (m *Manager) removeTerminal(id string, client *gossh.Client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.terminals[id], client)
	if len(m.terminals[id]) == 0 {
		delete(m.terminals, id)
	}
}

func startTerminal(client *gossh.Client, cmd string, opts TerminalOptions) (*Terminal, error) {
	s, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	modes := gossh.TerminalModes{gossh.ECHO: 1, gossh.TTY_OP_ISPEED: 38400, gossh.TTY_OP_OSPEED: 38400}
	cols, rows := clampSize(opts.Cols, opts.Rows)
	if err := s.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		return nil, errors.Join(err, s.Close())
	}
	stdin, err := s.StdinPipe()
	if err != nil {
		return nil, errors.Join(err, s.Close())
	}
	// With a pty, the shell's errors go to its output, but sshd may still
	// send some separately.
	r, w := io.Pipe()
	s.Stdout, s.Stderr = w, w
	if cmd == "" {
		err = s.Shell()
	} else {
		err = s.Start(cmd)
	}
	if err != nil {
		return nil, errors.Join(err, s.Close())
	}
	term := &Terminal{Reader: r, Writer: stdin, output: r, client: client, session: s, exited: make(chan struct{})}
	go func() {
		// Wait returns once the output has all been copied.
		term.code, term.hasCode = exitCode(s.Wait())
		_ = w.Close()
		close(term.exited)
	}()
	return term, nil
}

// clampSize returns cols and rows in the range a terminal can have.
func clampSize(cols, rows int) (int, int) {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	return min(cols, MaxCols), min(rows, MaxRows)
}

// Resize tells the shell the terminal's new size.
func (t *Terminal) Resize(cols, rows int) error {
	cols, rows = clampSize(cols, rows)
	return t.session.WindowChange(rows, cols)
}

// Wait waits for the shell to be done, after all its output, and returns its
// exit status. ok is false if it didn't say one, since the connection
// dropped, as when keepalives got no answer, the machine rebooted, or an
// admin changed or removed the access: the shell may still be running, and
// so may the tmux session it was attached to.
func (t *Terminal) Wait() (code int, ok bool) {
	<-t.exited
	return t.code, t.hasCode
}

// exitCode returns the exit status that err, from Session.Wait, says, and
// whether it says one.
func exitCode(err error) (int, bool) {
	var exit *gossh.ExitError
	switch {
	case err == nil:
		return 0, true
	case errors.As(err, &exit):
		return exit.ExitStatus(), true
	default:
		return 0, false
	}
}

// Close closes the terminal and its connection. A tmux session keeps
// running.
func (t *Terminal) Close() error {
	var err error
	t.once.Do(func() {
		// Output that no one will read can't hold up the shell's end.
		_ = t.output.CloseWithError(io.ErrClosedPipe)
		err = t.client.Close()
		if t.done != nil {
			t.done()
		}
	})
	return err
}

// Session is a tmux session on a device.
type Session struct {
	Name string
	// Created is when it was made, and Activity when something last
	// happened in it.
	Created, Activity time.Time
	// Attached is how many clients are attached, such as terminals in
	// Roost and SSH logins.
	Attached int
	Windows  int
	// Command is what runs in its active pane, such as bash or htop.
	Command string
}

// Sessions lists the tmux sessions on a device, by name. It returns
// ErrNoTmux if the device has no tmux.
func (m *Manager) Sessions(ctx context.Context, deviceID string) ([]Session, error) {
	res, err := m.Exec(ctx, deviceID, listScript)
	switch {
	case err != nil:
		return nil, err
	case res.Code == 127:
		return nil, ErrNoTmux
	case res.Code != 0:
		return nil, fmt.Errorf("%w: %s", ErrTmux, strings.TrimSpace(string(res.Stderr)))
	}
	out := []Session{}
	for line := range strings.Lines(string(res.Stdout)) {
		f := strings.SplitN(strings.TrimRight(line, "\r\n"), ":", 6)
		if len(f) < 6 {
			continue
		}
		s := Session{Name: f[0], Command: f[5]}
		s.Created, s.Activity = unixTime(f[1]), unixTime(f[2])
		s.Attached, _ = strconv.Atoi(f[3])
		s.Windows, _ = strconv.Atoi(f[4])
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Session) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

func unixTime(s string) time.Time {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// KillSession ends the tmux session called name on a device, and whatever
// runs in it. It returns ErrNoSession if there's none.
func (m *Manager) KillSession(ctx context.Context, deviceID, name string) error {
	if err := CheckSession(name); err != nil {
		return err
	}
	res, err := m.Exec(ctx, deviceID, killScript, name)
	switch {
	case err != nil:
		return err
	case res.Code == 127:
		return ErrNoTmux
	case res.Code != 0:
		msg := strings.TrimSpace(string(res.Stderr))
		if strings.Contains(msg, "can't find session") || strings.Contains(msg, "no server running") {
			return ErrNoSession
		}
		return fmt.Errorf("%w: %s", ErrTmux, msg)
	}
	return nil
}

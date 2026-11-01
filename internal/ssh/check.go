package ssh

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/manasseh-fikadu/bield/internal/store"
)

// checkScript says whether the machine has tmux, and which Docker socket it
// has, if any: the system's, a rootless one, or Docker Desktop's.
const checkScript = `if command -v tmux >/dev/null 2>&1; then echo tmux; fi; ` +
	`for s in /var/run/docker.sock "$XDG_RUNTIME_DIR/docker.sock" "/run/user/$(id -u)/docker.sock" "$HOME/.docker/run/docker.sock"; do ` +
	`if [ -S "$s" ]; then echo "docker=$s"; break; fi; done; exit 0`

// dockerTimeout is how long a check waits for Docker to answer.
const dockerTimeout = 5 * time.Second

// Check logs in to a device afresh, unless something is using its shared
// connection, and finds out what it offers. It returns what it found: a
// failure to log in is in the Access, not the error.
func (m *Manager) Check(ctx context.Context, deviceID string) (Access, error) {
	t, err := m.target(deviceID)
	if err != nil {
		return Access{}, err
	}
	return m.check(ctx, t)
}

func (m *Manager) check(ctx context.Context, t target) (Access, error) {
	m.mu.Lock()
	gen := m.gen[t.id]
	if p := m.pool[t.id]; p != nil && p.active == 0 {
		// A changed key or user is only noticed by logging in again.
		delete(m.pool, t.id)
		p.close()
	}
	m.mu.Unlock()

	res, err := m.exec(ctx, t, checkScript)
	switch {
	case errors.Is(err, ErrNotSetUp):
		return Access{}, err
	case ctx.Err() != nil:
		return Access{}, ctx.Err()
	case err == nil && res.Code != 0:
		err = errors.New("checking the machine failed: " + strings.TrimSpace(string(res.Stderr)))
	}
	found := Access{State: StateOK}
	if err == nil {
		tmux, docker := false, false
		for line := range strings.Lines(string(res.Stdout)) {
			line = strings.TrimSpace(line)
			switch {
			case line == "tmux":
				tmux = true
			case strings.HasPrefix(line, "docker="):
				found.DockerSocket = strings.TrimPrefix(line, "docker=")
				docker = m.pingDocker(ctx, t, found.DockerSocket)
			}
		}
		found.Tmux, found.Docker = &tmux, &docker
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gen[t.id] == gen {
		m.setCheckLocked(t.id, found, err)
	}
	a, ok := m.access[t.id]
	if !ok {
		return Access{}, ErrNotSetUp
	}
	return m.viewLocked(t.id, a), nil
}

// pingDocker reports whether Docker answers on the socket at path on t.
func (m *Manager) pingDocker(ctx context.Context, t target, path string) bool {
	ctx, cancel := context.WithTimeout(ctx, dockerTimeout)
	defer cancel()
	conn, err := m.dialUnix(ctx, t, path)
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/_ping", nil)
	if err != nil {
		return false
	}
	if err := req.Write(conn); err != nil {
		return false
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return false
	}
	_ = res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// record notes whether connecting to a machine worked, and how, unless its
// access changed since gen. client is the connection, if it worked.
func (m *Manager) record(id string, gen uint64, client *gossh.Client, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.checks[id]
	if m.gen[id] != gen {
		return
	}
	if err == nil {
		method := methodOf(client)
		if v.State == StateOK && v.Method == method {
			return
		}
		v.Method = method
	}
	// A connection only says whether Bield can log in, so what the last
	// check found stays.
	v.State, v.Error, v.Refusal = StateOK, "", ""
	m.setCheckLocked(id, v, err)
	if err == nil {
		// A machine that wasn't OK, such as one whose key an admin just
		// added, may offer more than Bield last found, so check it now.
		select {
		case m.wake <- struct{}{}:
		default:
		}
	}
}

// setCheckLocked records what Bield found of a machine, or err if it
// couldn't log in. checkHostKey records a host key that changed.
func (m *Manager) setCheckLocked(id string, found Access, err error) {
	if _, ok := m.access[id]; !ok || errors.Is(err, ErrHostKeyChange) {
		return
	}
	prev := m.checks[id]
	found.CheckedAt, found.OfferedKey = time.Now(), ""
	if err != nil {
		found.State, found.Error = StateFailing, err.Error()
		found.Tmux, found.Docker, found.DockerSocket = prev.Tmux, prev.Docker, prev.DockerSocket
		found.Method = ""
		if ce, ok := errors.AsType[*ConnectError](err); ok {
			found.Refusal = ce.Refusal
		}
	} else {
		// A check runs commands over a connection that record has noted.
		found.Method = cmp.Or(found.Method, prev.Method)
	}
	m.checks[id] = found
	m.changed()
}

// Run checks every machine with SSH access now and then, and closes shared
// connections that have been idle for IdleTimeout, until ctx ends. Then it
// closes every connection and terminal. A machine is checked soon after its
// access is set up or changes, again recheckFailing after a failed check,
// and recheckOK after one that worked. One whose host key changed waits for
// an admin.
func (m *Manager) Run(ctx context.Context) {
	defer m.closeAll()
	var wg sync.WaitGroup
	defer wg.Wait()
	slots := make(chan struct{}, maxChecks)
	ticker := time.NewTicker(sweepEvery)
	defer ticker.Stop()
	for {
		now := time.Now()
		m.closeIdle(now)
		for _, t := range m.due(now) {
			wg.Go(func() {
				defer m.doneChecking(t.id)
				select {
				case slots <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-slots }()
				if _, err := m.check(ctx, t); err != nil && ctx.Err() == nil && !errors.Is(err, ErrNotSetUp) {
					m.log.Warn("check a machine over SSH", "device", t.id, "error", err)
				}
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.wake:
		}
	}
}

// due returns the machines to check now, and marks them as being checked.
// Peers that are offline or not on the tailnet wait.
func (m *Manager) due(now time.Time) []target {
	st := m.opts.Tailnet().Status
	if st == nil {
		return nil
	}
	m.mu.Lock()
	ids := make([]string, 0, len(m.access))
	for id := range m.access {
		v := m.checks[id]
		var wait time.Duration
		switch v.State {
		case StateHostKeyChanged:
			continue
		case StateFailing:
			wait = recheckFailing
		case StateOK:
			wait = recheckOK
			// Logging in says nothing of what the machine offers. If no
			// check has found out yet, it's due now, whenever it last
			// logged in.
			if v.Tmux == nil {
				wait = 0
			}
		case StateUnchecked, "":
		}
		if !m.checking[id] && now.Sub(v.CheckedAt) >= wait {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()

	var out []target
	for _, id := range ids {
		deviceID := id
		if id == store.HubID {
			deviceID = st.Self.ID
		}
		t, err := m.target(deviceID)
		if err != nil || t.id != id {
			continue
		}
		out = append(out, t)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range out {
		m.checking[t.id] = true
	}
	return out
}

// doneChecking marks a check of a machine as over, and wakes Run: an admin
// who changed the machine's access while it ran has no check yet, since due
// skips a machine that's being checked, and would wait for the next sweep.
func (m *Manager) doneChecking(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.checking, id)
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

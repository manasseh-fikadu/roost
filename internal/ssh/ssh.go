// Package ssh is how Roost logs in to the tailnet's machines over SSH, for
// terminals, files and Docker. Roost has its own key (see LoadKey), and an
// admin adds it to a machine's authorized_keys, for the account Roost should
// log in as. Roost trusts the host key a machine offers the first time, and
// refuses one that changes until an admin resets it.
//
// A Manager keeps one connection to each machine for commands, files and the
// Docker socket, and closes it after it's been idle for IdleTimeout. Each
// terminal gets a connection of its own, so a busy shell can't hold up the
// rest. Package sshtest fakes an SSH server for tests.
package ssh

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os/user"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

const (
	// IdleTimeout is how long a machine's shared connection stays open
	// with nothing using it.
	IdleTimeout = 10 * time.Minute
	// DefaultPort is where sshd listens unless an admin says otherwise.
	DefaultPort = 22

	// connectTimeout is how long connecting and logging in can take.
	connectTimeout = 15 * time.Second
	// keepaliveEvery is how often Roost checks that a connection is still
	// there, and keepaliveMisses how many checks in a row can go
	// unanswered before it gives up on it.
	keepaliveEvery  = 15 * time.Second
	keepaliveMisses = 3
	// sweepEvery is how often Run closes idle connections and checks
	// machines again.
	sweepEvery = time.Minute
	// recheckFailing and recheckOK are how long after a check Run checks a
	// machine again, if it failed or if it worked.
	recheckFailing = 5 * time.Minute
	recheckOK      = time.Hour
	// maxChecks is how many machines Run checks at once.
	maxChecks = 4
)

// Errors that say why Roost can't log in to a device.
var (
	ErrStarting      = errors.New("the hub is starting, so it doesn't know the tailnet yet")
	ErrNoDevice      = errors.New("no device with this ID is on the tailnet")
	ErrUnsupported   = errors.New("SSH access works on Linux, macOS and FreeBSD machines")
	ErrOffline       = errors.New("the device is offline")
	ErrNotSetUp      = errors.New("SSH access isn't set up for this device")
	ErrNotHub        = errors.New("only the machine Roost runs on can authorize Roost itself")
	ErrBadUser       = errors.New("the user must be a login name, such as alice")
	ErrBadPort       = errors.New("the port must be from 1 to 65535")
	ErrHostKeyChange = errors.New("the machine's host key changed")
	ErrNoTmux        = errors.New("tmux isn't installed on the device")
	ErrTmux          = errors.New("tmux failed")
	ErrNoSession     = errors.New("no tmux session with this name is running")
)

// State says whether Roost can log in to a machine.
type State string

// The states of an Access.
const (
	// StateUnchecked means Roost hasn't tried since it started or since the
	// access changed.
	StateUnchecked State = "unchecked"
	StateOK        State = "ok"
	StateFailing   State = "failing"
	// StateHostKeyChanged means the machine offered a different host key
	// from the one Roost pinned, so Roost won't log in until an admin
	// resets it.
	StateHostKeyChanged State = "host-key-changed"
)

// Access is how Roost logs in to a machine, and what it found the last time
// it did.
type Access struct {
	User string
	Port int
	// HostKey is the SHA256 fingerprint of the pinned host key, or "" until
	// the first connection pins it.
	HostKey string
	State   State
	// Error says why the last try failed, while the State is StateFailing
	// or StateHostKeyChanged.
	Error string
	// CheckedAt is when Roost last checked the machine, or zero.
	CheckedAt time.Time
	// Tmux and Docker say whether the machine has tmux, and a Docker
	// socket Roost's user can open, or are nil until a check finds out.
	Tmux, Docker *bool
	// DockerSocket is the Docker socket the check found, if any.
	DockerSocket string
	// OfferedKey is the fingerprint of the host key the machine offered
	// instead of the pinned one, while the State is StateHostKeyChanged.
	OfferedKey string
}

// Options configures NewManager.
type Options struct {
	// KeyDir is where Roost keeps its key, which it makes there the first
	// time it needs it. Required.
	KeyDir string
	// Store keeps how Roost logs in to each machine. Required.
	Store *store.Store
	// Tailnet returns what Roost last learned about the tailnet. Required.
	Tailnet func() tailnet.Snapshot
	// Dial connects to an address on the tailnet, through Roost's node.
	// Required.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// DialLocal connects to an address on the machine Roost runs on, such
	// as 127.0.0.1:22. If it's nil, it's a net.Dialer's.
	DialLocal func(ctx context.Context, network, addr string) (net.Conn, error)
	// HostMode reports whether Roost uses the machine's own Tailscale, so
	// its node is the machine. Otherwise its node is its own, and the
	// machine may be a peer marked SameMachine.
	HostMode bool
	// Logger receives what goes wrong. Nil discards it.
	Logger *slog.Logger
}

// Manager logs in to the tailnet's machines. It's safe for concurrent use.
type Manager struct {
	opts    Options
	log     *slog.Logger
	changes chan struct{}

	keyMu sync.Mutex
	key   *Key

	mu        sync.Mutex
	access    map[string]store.SSHAccess
	checks    map[string]Access // what the latest check found, by access ID
	checking  map[string]bool
	pool      map[string]*pooled
	terminals map[string]map[*gossh.Client]struct{}
	// gen counts the changes to each machine's access, so a check or
	// connection that started before one can't record what it found.
	gen map[string]uint64
	// wake makes Run check the machines soon, such as after an admin sets
	// one up.
	wake chan struct{}
}

// NewManager returns a Manager. Call Load before using it. It panics if
// opts lacks a required field.
func NewManager(opts Options) *Manager {
	if opts.KeyDir == "" || opts.Store == nil || opts.Tailnet == nil || opts.Dial == nil {
		panic("ssh.NewManager: Options.KeyDir, Store, Tailnet and Dial are required")
	}
	if opts.DialLocal == nil {
		var d net.Dialer
		opts.DialLocal = d.DialContext
	}
	m := &Manager{
		opts: opts, log: opts.Logger, changes: make(chan struct{}, 1), wake: make(chan struct{}, 1),
		access: map[string]store.SSHAccess{}, checks: map[string]Access{}, checking: map[string]bool{},
		pool: map[string]*pooled{}, terminals: map[string]map[*gossh.Client]struct{}{}, gen: map[string]uint64{},
	}
	if m.log == nil {
		m.log = slog.New(slog.DiscardHandler)
	}
	return m
}

// Load reads how Roost logs in to each machine.
func (m *Manager) Load(ctx context.Context) error {
	access, err := m.opts.Store.SSHAccess(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.access = access
	return nil
}

// Changes receives a value when what Access returns changes. Only one
// goroutine should receive from it.
func (m *Manager) Changes() <-chan struct{} { return m.changes }

func (m *Manager) changed() {
	select {
	case m.changes <- struct{}{}:
	default:
	}
}

// AccessID returns the ID that d's SSH access is kept under: store.HubID
// for the machine Roost runs on, which can be two nodes, Roost's own (self)
// and the machine's, and otherwise d's ID.
func AccessID(d tailnet.Device, self bool) string {
	if self || d.SameMachine {
		return store.HubID
	}
	return d.ID
}

// Supported reports whether Roost can log in to d over SSH. self marks
// Roost's own node.
func Supported(d tailnet.Device, self bool) bool {
	if self || d.SameMachine {
		return slices.Contains([]string{"linux", "darwin", "freebsd"}, runtime.GOOS)
	}
	return slices.Contains([]string{"linux", "macOS", "freebsd"}, d.OS)
}

// Access returns how Roost logs in to each machine that has SSH access set
// up, by access ID (see AccessID), and what it last found.
func (m *Manager) Access() map[string]Access {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Access, len(m.access))
	for id, a := range m.access {
		out[id] = m.viewLocked(id, a)
	}
	return out
}

// viewLocked returns a's Access, with what the latest check found.
func (m *Manager) viewLocked(id string, a store.SSHAccess) Access {
	v := m.checks[id]
	v.User, v.Port, v.HostKey = a.User, a.Port, fingerprint(a.HostKey)
	if v.State == "" {
		v.State = StateUnchecked
	}
	return v
}

// fingerprint returns the SHA256 fingerprint of a key as in known_hosts, or
// "" if there's none.
func fingerprint(key string) string {
	if key == "" {
		return ""
	}
	pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(key))
	if err != nil {
		return ""
	}
	return gossh.FingerprintSHA256(pub)
}

// A target is a device that Roost can log in to, and where.
type target struct {
	// id is its access ID, deviceID the ID of the device it was asked for
	// by, and name what to call it in messages.
	id, deviceID, name string
	// hub marks the machine Roost runs on.
	hub bool
	// addrs are where its sshd may listen, in order. If one refuses the
	// connection, Roost tries the next.
	addrs []address
}

type address struct {
	ip   netip.Addr
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// target finds the device with an ID on the tailnet.
func (m *Manager) target(deviceID string) (target, error) {
	st := m.opts.Tailnet().Status
	if st == nil {
		return target{}, ErrStarting
	}
	d, self := st.Self, true
	if d.ID != deviceID {
		i := slices.IndexFunc(st.Peers, func(p tailnet.Device) bool { return p.ID == deviceID })
		if i < 0 {
			return target{}, ErrNoDevice
		}
		d, self = st.Peers[i], false
	}
	if !Supported(d, self) {
		return target{}, ErrUnsupported
	}
	t := target{id: AccessID(d, self), deviceID: d.ID, name: deviceName(d)}
	if t.id != store.HubID {
		if !d.Online {
			return target{}, fmt.Errorf("%w: %s isn't connected to the tailnet", ErrOffline, t.name)
		}
		ip, ok := tailnetIP(d)
		if !ok {
			return target{}, fmt.Errorf("%w: %s has no tailnet address", ErrOffline, t.name)
		}
		t.addrs = []address{{ip, m.opts.Dial}}
		return t, nil
	}
	// On its own machine, Roost logs in on the loopback address. If sshd
	// only listens on the machine's tailnet address, it tries that next.
	t.hub, t.name = true, "the machine Roost runs on"
	t.addrs = []address{{netip.AddrFrom4([4]byte{127, 0, 0, 1}), m.opts.DialLocal}}
	machine, ok := st.Self, m.opts.HostMode
	if !ok {
		i := slices.IndexFunc(st.Peers, func(p tailnet.Device) bool { return p.SameMachine })
		if ok = i >= 0; ok {
			machine = st.Peers[i]
		}
	}
	if ip, has := tailnetIP(machine); ok && has {
		t.addrs = append(t.addrs, address{ip, m.opts.Dial})
	}
	return t, nil
}

func deviceName(d tailnet.Device) string {
	if d.HostName != "" {
		return d.HostName
	}
	name, _, _ := strings.Cut(d.DNSName, ".")
	return cmp.Or(name, d.ID)
}

// tailnetIP returns d's IPv4 address on the tailnet, or else its first.
func tailnetIP(d tailnet.Device) (netip.Addr, bool) {
	for _, ip := range d.IPs {
		if ip.Is4() {
			return ip, true
		}
	}
	if len(d.IPs) > 0 {
		return d.IPs[0], true
	}
	return netip.Addr{}, false
}

// Key returns Roost's key, making it the first time.
func (m *Manager) Key() (*Key, error) {
	m.keyMu.Lock()
	defer m.keyMu.Unlock()
	if m.key != nil {
		return m.key, nil
	}
	comment := "roost"
	if st := m.opts.Tailnet().Status; st != nil {
		if name, _, _ := strings.Cut(st.Self.DNSName, "."); name != "" {
			comment = "roost@" + name
		}
	}
	key, err := LoadKey(m.opts.KeyDir, comment)
	if err != nil {
		return nil, err
	}
	m.key = key
	return key, nil
}

// Setup is how to let Roost log in to a machine.
type Setup struct {
	// AuthorizedKey is the line for the machine's authorized_keys.
	AuthorizedKey string
	// Command adds AuthorizedKey to the authorized_keys of the user who
	// runs it.
	Command string
	// Hub marks the machine Roost runs on, where AuthorizeHere can add the
	// key for Roost's own user, called HubUser.
	Hub     bool
	HubUser string
}

// Setup returns how to let Roost log in to a device. The key only works
// from Roost's tailnet addresses, and on the machine Roost runs on, from
// the loopback addresses too.
func (m *Manager) Setup(deviceID string) (Setup, error) {
	t, err := m.target(deviceID)
	if errors.Is(err, ErrOffline) {
		// A machine that's off can still be set up.
		t, err = target{id: deviceID}, nil
	}
	if err != nil {
		return Setup{}, err
	}
	key, err := m.Key()
	if err != nil {
		return Setup{}, err
	}
	line := key.AuthorizedKey(m.from(t.hub))
	s := Setup{AuthorizedKey: line, Command: SetupCommand(line), Hub: t.hub}
	if t.hub {
		if u, err := user.Current(); err == nil {
			s.HubUser = u.Username
		}
	}
	return s, nil
}

// from returns the addresses Roost logs in from: its node's on the tailnet,
// and on its own machine, the loopback addresses.
func (m *Manager) from(hub bool) []netip.Addr {
	var from []netip.Addr
	if hub {
		from = append(from, netip.AddrFrom4([4]byte{127, 0, 0, 1}), netip.IPv6Loopback())
	}
	if st := m.opts.Tailnet().Status; st != nil {
		from = append(from, st.Self.IPs...)
	}
	return from
}

// userName is what a login name on a Unix machine can be.
var userName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)

// SetUp saves how Roost logs in to a device: as user, on port. Changing the
// port forgets the pinned host key, since another sshd may answer there.
// Roost checks the machine soon after. by is the admin's login.
func (m *Manager) SetUp(ctx context.Context, deviceID, username string, port int, by string) (Access, error) {
	switch {
	case !userName.MatchString(username):
		return Access{}, ErrBadUser
	case port < 1 || port > 65535:
		return Access{}, ErrBadPort
	}
	t, err := m.target(deviceID)
	if errors.Is(err, ErrOffline) {
		t, err = target{id: deviceID}, nil
	}
	if err != nil {
		return Access{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a := store.SSHAccess{DeviceID: t.id, User: username, Port: port, CreatedAt: time.Now(), CreatedBy: by}
	if prev, ok := m.access[t.id]; ok && prev.Port == port {
		a.HostKey, a.PinnedAt = prev.HostKey, prev.PinnedAt
	}
	if err := m.opts.Store.SetSSHAccess(ctx, a); err != nil {
		return Access{}, err
	}
	m.access[t.id] = a
	m.resetLocked(t.id)
	return m.viewLocked(t.id, a), nil
}

// AuthorizeHere lets Roost log in to the machine it runs on as the user it
// runs as, on port 22: it adds Roost's key to that user's authorized_keys,
// and sets up access with it. It reports whether it added the key, which
// may be there already. by is the admin's login.
func (m *Manager) AuthorizeHere(ctx context.Context, deviceID, by string) (Access, bool, error) {
	t, err := m.target(deviceID)
	if err != nil {
		return Access{}, false, err
	}
	if !t.hub {
		return Access{}, false, ErrNotHub
	}
	u, err := user.Current()
	if err != nil {
		return Access{}, false, fmt.Errorf("find out which user Roost runs as: %w", err)
	}
	key, err := m.Key()
	if err != nil {
		return Access{}, false, err
	}
	added, err := key.AuthorizeHere(key.AuthorizedKey(m.from(true)))
	if err != nil {
		return Access{}, false, fmt.Errorf("add Roost's key to %s's authorized_keys: %w", u.Username, err)
	}
	a, err := m.SetUp(ctx, deviceID, u.Username, DefaultPort, by)
	return a, added, err
}

// Remove forgets how Roost logs in to a device, and closes its connections
// and terminals. Roost's key stays in the machine's authorized_keys until
// someone takes it out.
func (m *Manager) Remove(ctx context.Context, deviceID string) error {
	id, err := m.accessID(deviceID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.opts.Store.DeleteSSHAccess(ctx, id); err != nil {
		if errors.Is(err, store.ErrNoSSHAccess) {
			return ErrNotSetUp
		}
		return err
	}
	delete(m.access, id)
	m.resetLocked(id)
	delete(m.checks, id)
	return nil
}

// ResetHostKey forgets a device's pinned host key, such as after the
// machine was reinstalled, so the next connection pins the key it offers.
func (m *Manager) ResetHostKey(ctx context.Context, deviceID string) (Access, error) {
	id, err := m.accessID(deviceID)
	if err != nil {
		return Access{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.access[id]
	if !ok {
		return Access{}, ErrNotSetUp
	}
	if err := m.opts.Store.ResetHostKey(ctx, id); err != nil {
		return Access{}, err
	}
	a.HostKey, a.PinnedAt = "", time.Time{}
	m.access[id] = a
	m.resetLocked(id)
	return m.viewLocked(id, a), nil
}

// accessID returns the access ID of the device with an ID, even if it's
// offline.
func (m *Manager) accessID(deviceID string) (string, error) {
	t, err := m.target(deviceID)
	if errors.Is(err, ErrOffline) {
		return deviceID, nil
	}
	return t.id, err
}

// resetLocked closes a machine's connections and terminals, since how Roost
// logs in to it changed, forgets what it found, and checks it again soon.
func (m *Manager) resetLocked(id string) {
	if p := m.pool[id]; p != nil {
		delete(m.pool, id)
		p.close()
	}
	for c := range m.terminals[id] {
		_ = c.Close()
	}
	delete(m.terminals, id)
	m.gen[id]++
	m.checks[id] = Access{State: StateUnchecked}
	m.changed()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// connect logs in to t with a, on a connection of its own.
func (m *Manager) connect(ctx context.Context, t target, a store.SSHAccess) (*gossh.Client, error) {
	key, err := m.Key()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	var hostKeyErr error
	config := &gossh.ClientConfig{
		User: a.User,
		Auth: []gossh.AuthMethod{gossh.PublicKeys(key.signer)},
		HostKeyCallback: func(_ string, _ net.Addr, offered gossh.PublicKey) error {
			hostKeyErr = m.checkHostKey(ctx, t, offered)
			return hostKeyErr
		},
		HostKeyAlgorithms: hostKeyAlgorithms(a.HostKey),
	}
	var conn net.Conn
	for i, addr := range t.addrs {
		hostport := net.JoinHostPort(addr.ip.String(), strconv.Itoa(a.Port))
		conn, err = addr.dial(ctx, "tcp", hostport)
		if err == nil || i == len(t.addrs)-1 || !refused(err) {
			break
		}
	}
	if err != nil {
		return nil, m.connectError(t, a, err)
	}
	// The deadline bounds the handshake, and stopping clears it.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	c, chans, reqs, err := gossh.NewClientConn(conn, conn.RemoteAddr().String(), config)
	if !stop() || err != nil {
		_ = conn.Close()
		if hostKeyErr != nil {
			return nil, hostKeyErr
		}
		return nil, m.connectError(t, a, cmp.Or(err, ctx.Err()))
	}
	_ = conn.SetDeadline(time.Time{})
	client := gossh.NewClient(c, chans, reqs)
	go keepalive(client)
	return client, nil
}

// checkHostKey checks the host key that t offered against the one pinned
// for it, and pins it if there's none.
func (m *Manager) checkHostKey(ctx context.Context, t target, offered gossh.PublicKey) error {
	key := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(offered)))
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.access[t.id]
	switch {
	case !ok:
		return ErrNotSetUp
	case a.HostKey == key:
		return nil
	case a.HostKey == "":
		now := time.Now()
		pinned, err := m.opts.Store.PinHostKey(ctx, t.id, key, now)
		if err != nil {
			return fmt.Errorf("pin the host key: %w", err)
		}
		if !pinned {
			return errors.New("another connection pinned a host key first; try again")
		}
		a.HostKey, a.PinnedAt = key, now
		m.access[t.id] = a
		m.log.Info("pinned an SSH host key", "device", t.id, "fingerprint", gossh.FingerprintSHA256(offered))
		m.changed()
		return nil
	}
	want, got := fingerprint(a.HostKey), gossh.FingerprintSHA256(offered)
	err := fmt.Errorf("%w: Roost pinned %s, and %s offered %s; if the machine was reinstalled, reset the host key",
		ErrHostKeyChange, want, t.name, got)
	v := m.checks[t.id]
	if v.State != StateHostKeyChanged || v.OfferedKey != got {
		m.log.Warn("refused an SSH host key that changed", "device", t.id, "pinned", want, "offered", got)
		event := store.Event{
			At: time.Now(), Level: "error", Kind: "ssh", DeviceID: t.deviceID,
			Message: "The SSH host key of " + t.name + " changed, so Roost won't log in to it until an admin resets it",
		}
		if err := m.opts.Store.AddEvents(context.WithoutCancel(ctx), event); err != nil {
			m.log.Warn("add to the activity log", "error", err)
		}
	}
	m.checks[t.id] = Access{
		State: StateHostKeyChanged, Error: err.Error(), CheckedAt: time.Now(), OfferedKey: got,
		Tmux: v.Tmux, Docker: v.Docker, DockerSocket: v.DockerSocket,
	}
	m.changed()
	return err
}

// firstHostKeys are the host key algorithms Roost asks for before it pins
// one, in OpenSSH's order, so it pins the key that ssh would show.
var firstHostKeys = []string{
	gossh.KeyAlgoED25519, gossh.KeyAlgoECDSA256, gossh.KeyAlgoECDSA384, gossh.KeyAlgoECDSA521,
	gossh.KeyAlgoRSASHA512, gossh.KeyAlgoRSASHA256, gossh.KeyAlgoRSA,
}

// hostKeyAlgorithms returns the algorithms to ask for, so a machine offers
// the same kind of key as the one pinned, or firstHostKeys if there's none.
func hostKeyAlgorithms(pinned string) []string {
	pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(pinned))
	if err != nil {
		return firstHostKeys
	}
	if pub.Type() == gossh.KeyAlgoRSA {
		return []string{gossh.KeyAlgoRSASHA512, gossh.KeyAlgoRSASHA256, gossh.KeyAlgoRSA}
	}
	return []string{pub.Type()}
}

// refused reports whether err means nothing listens at the address.
func refused(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(err.Error(), "connection refused")
}

// connectError says why logging in to t failed with err.
func (m *Manager) connectError(t target, a store.SSHAccess, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unable to authenticate"):
		msg = "Roost's key isn't in the authorized_keys of " + a.User + " on " + t.name
	case refused(err):
		msg = "nothing is listening for SSH on port " + strconv.Itoa(a.Port) + " of " + t.name
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, syscall.ETIMEDOUT) || isTimeout(err):
		msg = "timed out connecting to port " + strconv.Itoa(a.Port) + " of " + t.name
	case errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH):
		msg = "Roost can't reach " + t.name + " on the tailnet"
	}
	return &ConnectError{Msg: msg, Err: err}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// ConnectError means Roost couldn't log in to a machine.
type ConnectError struct {
	// Msg says why, for people.
	Msg string
	Err error
}

func (e *ConnectError) Error() string { return e.Msg }
func (e *ConnectError) Unwrap() error { return e.Err }

// keepalive checks every keepaliveEvery that c is still there, and closes it
// if keepaliveMisses checks in a row get no answer.
func keepalive(c *gossh.Client) {
	closed := make(chan struct{})
	go func() {
		_ = c.Wait()
		close(closed)
	}()
	t := time.NewTicker(keepaliveEvery)
	defer t.Stop()
	misses := 0
	for {
		select {
		case <-closed:
			return
		case <-t.C:
		}
		answered := make(chan error, 1)
		go func() {
			// Any answer will do: sshd refuses requests it doesn't know.
			_, _, err := c.SendRequest("keepalive@openssh.com", true, nil)
			answered <- err
		}()
		select {
		case <-closed:
			return
		case err := <-answered:
			if err == nil {
				misses = 0
				continue
			}
			misses = keepaliveMisses
		case <-t.C:
			misses++
		}
		if misses >= keepaliveMisses {
			_ = c.Close()
			return
		}
	}
}

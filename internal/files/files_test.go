package files_test

import (
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/manasseh-fikadu/bield/internal/files"
	"github.com/manasseh-fikadu/bield/internal/ssh"
	"github.com/manasseh-fikadu/bield/internal/ssh/sshtest"
	"github.com/manasseh-fikadu/bield/internal/store"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

// machine is a Linux laptop whose sshd serves a folder of the test machine
// over SFTP and runs commands in the shell, as sshd does.
type machine struct {
	// home is the folder SFTP starts in, with links resolved.
	home string
	mgr  *ssh.Manager
}

// newMachine returns a machine whose login shell is shell, or skips the
// test if the machine has none.
func newMachine(t *testing.T, shell string) *machine {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "bield.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	laptop := sshtest.New()
	laptop.ServeSFTP(home)
	t.Cleanup(laptop.Close)
	status := tailnet.Status{
		State: tailnet.StateRunning,
		Self:  tailnet.Device{ID: "bield", DNSName: "bield.example.ts.net", OS: "linux", Online: true},
		Peers: []tailnet.Device{{ID: "laptop", HostName: "laptop", OS: "linux", Online: true, IPs: addrs("100.64.0.2")}},
	}
	mgr := ssh.NewManager(ssh.Options{
		KeyDir:  t.TempDir(),
		Store:   st,
		Tailnet: func() tailnet.Snapshot { s := status; return tailnet.Snapshot{Status: &s} },
		Dial:    sshtest.Route(map[string]*sshtest.Server{"100.64.0.2": laptop}),
	})
	if err := mgr.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	key, err := mgr.Key()
	if err != nil {
		t.Fatal(err)
	}
	laptop.Authorize(key.PublicKey())
	if shell != "" {
		laptop.Handle(sshtest.Shell(shellPath(t, shell)))
	}
	if _, err := mgr.SetUp(t.Context(), "laptop", "alice", ssh.DefaultPort, "admin@example.com"); err != nil {
		t.Fatal(err)
	}
	return &machine{home: home, mgr: mgr}
}

// open returns the files of the machine, until the test ends.
func (m *machine) open(t *testing.T) *files.Remote {
	t.Helper()
	r, err := files.Open(t.Context(), m.mgr, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

// write makes a file in the machine's home, and folders above it.
func (m *machine) write(t *testing.T, rel, content string) string {
	t.Helper()
	p := filepath.Join(m.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func addrs(ips ...string) []netip.Addr {
	out := make([]netip.Addr, len(ips))
	for i, ip := range ips {
		out[i] = netip.MustParseAddr(ip)
	}
	return out
}

// shellPath finds a login shell, or skips the test.
func shellPath(t *testing.T, shell string) string {
	t.Helper()
	p, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s isn't installed", shell)
	}
	return p
}

// shells are the login shells a user may have, which sshd runs Bield's
// commands with. The tests use those on the machine.
var shells = []string{"sh", "dash", "bash", "zsh", "fish"}

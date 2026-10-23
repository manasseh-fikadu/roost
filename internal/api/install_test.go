package api_test

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/tailnet/tailnettest"
)

// TestInstallScripts runs the install and uninstall commands the way an admin
// would, on this machine, against a hub that serves on loopback and takes
// 127.0.0.1 for alice's laptop. The scripts write to a temporary HOME, and
// systemctl does nothing.
func TestInstallScripts(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the scripts install a systemd service on Linux, and a LaunchAgent on macOS")
	}
	for _, tool := range []string{"bash", "curl"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s to run the scripts with", tool)
		}
	}
	t.Parallel()

	tn := tailnettest.New(tailnet.Status{
		State: tailnet.StateRunning,
		Self:  tailnet.Device{ID: "roost", DNSName: "localhost", IPs: []netip.Addr{roostIP}, Owner: alice, Online: true},
		Peers: []tailnet.Device{
			{ID: "laptop", DNSName: "laptop.example.ts.net", IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, Owner: alice, Online: true},
		},
	})
	st := testStore(t)
	srv := httptest.NewUnstartedServer(nil)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	opts := withStore(tn, st, api.Options{AppURL: "http://localhost:" + port})
	srv.Config.Handler = api.New(opts)
	srv.Start()
	t.Cleanup(srv.Close)

	home, bin := t.TempDir(), t.TempDir()
	stubLog := filepath.Join(bin, "calls")
	for _, name := range []string{"systemctl", "loginctl"} {
		stub := "#!/bin/sh\necho \"" + name + " $*\" >> " + stubLog + "\n"
		//nolint:gosec // G306: a stub has to be executable.
		if err := os.WriteFile(filepath.Join(bin, name), []byte(stub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	run := func(command string) (string, error) {
		t.Helper()
		//nolint:gosec // G204: the commands are the ones Roost gives admins.
		cmd := exec.CommandContext(t.Context(), "bash", "-c", command)
		cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+os.Getenv("PATH"))
		cmd.WaitDelay = 10 * time.Second
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	confDir := filepath.Join(home, ".config", "roost-agent")
	envFile := filepath.Join(confDir, "env")
	// An env from an older agent that anyone could read, which the install
	// has to lock down.
	if err := os.MkdirAll(confDir, 0o755); err != nil { //nolint:gosec // G301: see above.
		t.Fatal(err)
	}
	if err := os.WriteFile(envFile, []byte("TOKEN=shared\n"), 0o644); err != nil { //nolint:gosec // G306: see above.
		t.Fatal(err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost:"+port+"/api/agent/codes", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(api.CSRFHeader, "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var code api.AgentCode
	err = json.NewDecoder(res.Body).Decode(&code)
	_ = res.Body.Close()
	if err != nil || res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/agent/codes = %d, %v", res.StatusCode, err)
	}

	secret := func() string {
		t.Helper()
		for name, want := range map[string]os.FileMode{confDir: 0o700, envFile: 0o600} {
			info, err := os.Stat(name)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != want {
				t.Errorf("%s has mode %v, want %v", name, got, want)
			}
		}
		env, err := os.ReadFile(filepath.Clean(envFile))
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.Lines(string(env)) {
			if s, ok := strings.CutPrefix(strings.TrimSpace(line), "SECRET="); ok {
				return s
			}
		}
		t.Fatalf("env has no secret:\n%s", env)
		return ""
	}

	// The machine can run the command again, such as after a failure, and
	// gets a new secret.
	var secrets []string
	for range 2 {
		if out, err := run(code.InstallCommand); err != nil || !strings.Contains(out, "Done.") {
			t.Fatalf("the install command failed: %v\n%s", err, out)
		}
		secrets = append(secrets, secret())
	}
	if secrets[0] == secrets[1] {
		t.Error("installing again kept the same secret")
	}
	if node, err := st.EnrolledNode(t.Context(), secrets[1]); err != nil || node != "laptop" {
		t.Errorf("the secret in env belongs to %q, %v; want the laptop", node, err)
	}
	if _, ok := opts.Agents.Reports()["laptop"]; !ok {
		t.Error("the install didn't send a first heartbeat")
	}
	if calls, err := os.ReadFile(filepath.Clean(stubLog)); err != nil || !strings.Contains(string(calls), "systemctl --user restart roost-agent.service") {
		t.Errorf("systemctl calls = %q, %v; want the service restarted", calls, err)
	}

	out, err := run("curl -fsSL http://localhost:" + port + "/api/agent/uninstall.sh | bash")
	if err != nil || !strings.Contains(out, "Roost forgot this machine's agent.") {
		t.Fatalf("the uninstall command failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(confDir); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) after uninstalling: %v, want it gone", confDir, err)
	}
	if enrolled, err := st.Enrollments(t.Context()); err != nil || len(enrolled) != 0 {
		t.Errorf("enrollments after uninstalling = %v, %v, want none", enrolled, err)
	}
	if _, ok := opts.Agents.Reports()["laptop"]; ok {
		t.Error("Roost still has the laptop's report after uninstalling")
	}

	// A code Roost doesn't know installs nothing, and says why.
	out, err = run("curl -fsSL http://localhost:" + port + "/api/agent/install/NOPE | bash")
	if err == nil || !strings.Contains(out, "Roost won't install the agent: Roost doesn't know the code") {
		t.Errorf("installing with an unknown code: %v\n%s", err, out)
	}
	if _, err := os.Stat(confDir); !os.IsNotExist(err) {
		t.Errorf("installing with an unknown code made %s: %v", confDir, err)
	}
}

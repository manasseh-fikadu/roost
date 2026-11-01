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

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
	"github.com/manasseh-fikadu/bield/internal/tailnet/tailnettest"
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
		Self:  tailnet.Device{ID: "bield", DNSName: "localhost", IPs: []netip.Addr{bieldIP}, Owner: alice, Online: true},
		Peers: []tailnet.Device{
			{ID: "laptop", DNSName: "laptop.example.ts.net", IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, Owner: alice, Online: true},
		},
	})
	st := testStore(t)
	srv := httptest.NewUnstartedServer(nil)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	opts := withStore(t, tn, st, api.Options{AppURL: "http://localhost:" + port})
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
		//nolint:gosec // G204: the commands are the ones Bield gives admins.
		cmd := exec.CommandContext(t.Context(), "bash", "-c", command)
		cmd.Env = append(os.Environ(), "HOME="+home, "PATH="+bin+":"+os.Getenv("PATH"))
		cmd.WaitDelay = 10 * time.Second
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	confDir := filepath.Join(home, ".config", "bield-agent")
	envFile := filepath.Join(confDir, "env")
	// An env from an older agent that anyone could read, which the install
	// has to lock down.
	if err := os.MkdirAll(confDir, 0o755); err != nil { //nolint:gosec // G301: see above.
		t.Fatal(err)
	}
	if err := os.WriteFile(envFile, []byte("TOKEN=shared\n"), 0o644); err != nil { //nolint:gosec // G306: see above.
		t.Fatal(err)
	}

	// call sends a request to the API as alice, from her laptop, and decodes
	// the response into v, unless it's nil.
	call := func(method, path string, v any) int {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, "http://localhost:"+port+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(api.CSRFHeader, "1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		if v != nil {
			if err := json.NewDecoder(res.Body).Decode(v); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
		}
		return res.StatusCode
	}
	var code api.AgentCode
	if status := call(http.MethodPost, "/api/agent/codes", &code); status != http.StatusCreated {
		t.Fatalf("POST /api/agent/codes = %d", status)
	}
	uninstall := "curl -fsSL http://localhost:" + port + "/api/agent/uninstall.sh | bash"

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
	if calls, err := os.ReadFile(filepath.Clean(stubLog)); err != nil || !strings.Contains(string(calls), "systemctl --user restart bield-agent.service") {
		t.Errorf("systemctl calls = %q, %v; want the service restarted", calls, err)
	}

	out, err := run(uninstall)
	if err != nil || !strings.Contains(out, "Bield forgot this machine's agent.") {
		t.Fatalf("the uninstall command failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(confDir); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) after uninstalling: %v, want it gone", confDir, err)
	}
	if enrolled, err := st.Enrollments(t.Context()); err != nil || len(enrolled) != 0 {
		t.Errorf("enrollments after uninstalling = %v, %v, want none", enrolled, err)
	}
	if _, ok := opts.Agents.Reports()["laptop"]; ok {
		t.Error("Bield still has the laptop's report after uninstalling")
	}

	// Uninstalling left the code working on this machine, so the agent can
	// go back on. Once an admin revokes it, the same command can't bring it
	// back, and uninstalling says Bield doesn't know it anymore.
	if out, err := run(code.InstallCommand); err != nil || !strings.Contains(out, "Done.") {
		t.Fatalf("installing again after uninstalling failed: %v\n%s", err, out)
	}
	if status := call(http.MethodDelete, "/api/devices/laptop/agent", nil); status != http.StatusNoContent {
		t.Fatalf("DELETE the laptop's agent = %d, want 204", status)
	}
	if out, err := run(code.InstallCommand); err == nil || !strings.Contains(out, "an admin revoked the agent it enrolled") {
		t.Errorf("installing again after revoking: %v\n%s", err, out)
	}
	out, err = run(uninstall)
	if err != nil || !strings.Contains(out, "Bield doesn't know this agent's secret: it was revoked") {
		t.Errorf("uninstalling after revoking: %v\n%s", err, out)
	}
	if _, err := os.Stat(confDir); !os.IsNotExist(err) {
		t.Errorf("Stat(%s) after uninstalling a revoked agent: %v, want it gone", confDir, err)
	}

	// A code Bield doesn't know installs nothing, and says why.
	out, err = run("curl -fsSL http://localhost:" + port + "/api/agent/install/NOPE | bash")
	if err == nil || !strings.Contains(out, "Bield won't install the agent: Bield doesn't know the code") {
		t.Errorf("installing with an unknown code: %v\n%s", err, out)
	}
	if _, err := os.Stat(confDir); !os.IsNotExist(err) {
		t.Errorf("installing with an unknown code made %s: %v", confDir, err)
	}
}

// TestRunRemote runs the script that Bield sends over SSH to install or remove
// the agent, with sh as sshd would, against a server that serves a script.
func TestRunRemote(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl to download with")
	}
	t.Parallel()
	if strings.ContainsAny(api.RunRemote, "'\\") {
		t.Error("the script has a single quote or a backslash, which fish would read in its own way")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#!/bin/bash\necho out; echo err >&2; echo \"user=$USER\"\n"))
	})
	mux.HandleFunc("/fails", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("#!/bin/bash\necho nope >&2\nexit 3\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	for _, tt := range []struct {
		name, path string
		code       int
		want       []string
	}{
		{"works", "/ok", 0, []string{"out", "err", "user="}},
		{"the script fails", "/fails", 3, []string{"nope"}},
		{"the download fails", "/missing", 1, []string{"could not download the script from " + srv.URL}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			//nolint:gosec // G204: the script is Bield's own, and the URL is the test server's.
			cmd := exec.CommandContext(t.Context(), "sh", "-c", api.RunRemote, "sh", srv.URL+tt.path, srv.URL)
			// Unset, as in a session without a login.
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
			var stdout, stderr strings.Builder
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			code := 0
			if ee, ok := err.(*exec.ExitError); ok { //nolint:errorlint // exec returns it as is.
				code = ee.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if code != tt.code {
				t.Errorf("exit status %d, want %d; output %q", code, tt.code, stdout.String())
			}
			// Errors come out with the rest, in order, so the admin sees them.
			if stderr.Len() > 0 {
				t.Errorf("stderr = %q, want everything on stdout", stderr.String())
			}
			for _, w := range tt.want {
				if !strings.Contains(stdout.String(), w) {
					t.Errorf("output %q lacks %q", stdout.String(), w)
				}
			}
		})
	}
}

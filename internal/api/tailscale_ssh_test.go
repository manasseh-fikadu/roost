package api_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/ssh/sshtest"
)

// tailscaleSSHEnv is an sshEnv whose laptop runs Tailscale SSH, as the
// tailnet's status and the laptop's sshd both say, with no key for Bield.
func tailscaleSSHEnv(t *testing.T, policy sshtest.Policy) *sshEnv {
	t.Helper()
	e := newSSHEnv(t, testStore(t), nil)
	status, err := e.tn.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i := range status.Peers {
		if status.Peers[i].ID == "laptop" {
			status.Peers[i].TailscaleSSH = true
		}
	}
	e.tn.SetStatus(*status)
	e.sshd.TailscaleSSH(policy)
	e.sshd.Handle(func(*sshtest.Exec) int { return 0 })
	e.setUp(t)
	return e
}

func TestSSH_tailscale(t *testing.T) {
	t.Parallel()
	e := tailscaleSSHEnv(t, sshtest.PolicyAccept)

	rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/ssh/check", laptopIP, "")
	var got api.DeviceSSH
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("POST check = %d %s, want 200", rec.Code, rec.Body)
	}
	if got.State != api.SSHStateOK || got.Method != api.SSHMethodTailscale || got.Policy != nil {
		t.Errorf("POST check = %+v, want ok through Tailscale SSH", got)
	}
	snap := api.NewFeed(api.FeedOptions{Tailnet: e.opts.Snapshot, Store: e.st, SSH: e.opts.SSH.Access}).Latest()
	laptop := findDevice(t, snap, "laptop")
	if !laptop.TailscaleSSH || laptop.SSH == nil || laptop.SSH.Method != api.SSHMethodTailscale {
		t.Errorf("laptop = %+v, want Tailscale SSH on, and a login through it", laptop)
	}
	if findDevice(t, snap, "server").TailscaleSSH {
		t.Error("the server runs Tailscale SSH, want it off")
	}
}

func TestSSH_tailscaleRefused(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		policy  sshtest.Policy
		refusal api.SSHRefusal
	}{
		{"no rule for the machine", sshtest.PolicyDenyNode, api.SSHRefusalNode},
		{"no rule for the user", sshtest.PolicyDenyUser, api.SSHRefusalUser},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := tailscaleSSHEnv(t, tt.policy)
			rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/ssh/check", laptopIP, "")
			var got api.DeviceSSH
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
				t.Fatalf("POST check = %d %s, want 200", rec.Code, rec.Body)
			}
			if got.State != api.SSHStateFailing || got.Method != "" || got.Policy == nil || got.Policy.Refusal != tt.refusal {
				t.Fatalf("POST check = %+v, want failing, with the refusal %s", got, tt.refusal)
			}
			// Bield and the laptop are both alice's, so the rule is for her own machines.
			want := "{\n  \"action\": \"accept\",\n  \"src\": [\"alice@example.com\"],\n" +
				"  \"dst\": [\"autogroup:self\"],\n  \"users\": [\"alice\"]\n}"
			if got.Policy.Snippet != want {
				t.Errorf("snippet =\n%s\nwant\n%s", got.Policy.Snippet, want)
			}
		})
	}
}

var codeInURL = regexp.MustCompile(`http://bield\.example\.ts\.net/api/agent/install/([A-Z0-9]+)`)

func TestAgent_installOverSSH(t *testing.T) {
	t.Parallel()
	e := newSSHEnv(t, testStore(t), nil)

	// Without SSH access, Bield makes no code.
	if rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/agent/install", laptopIP, ""); rec.Code != http.StatusConflict {
		t.Fatalf("POST install without access = %d %s, want 409", rec.Code, rec.Body)
	}

	e.setUp(t)
	var command string
	e.sshd.Handle(func(x *sshtest.Exec) int {
		command = x.Command
		_, _ = x.Stdout.Write([]byte("Installed.\n"))
		return 0
	})
	rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/agent/install", laptopIP, "")
	var got api.AgentRun
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("POST install = %d %s, want 200", rec.Code, rec.Body)
	}
	if !got.OK || got.Output != "Installed." {
		t.Errorf("POST install = %+v, want ok, with what the script printed", got)
	}
	m := codeInURL.FindStringSubmatch(command)
	if m == nil {
		t.Fatalf("the command was %q, want it to download from Bield with a code", command)
	}
	// The code is good for the laptop, and for no other machine.
	if err := e.st.CheckEnrollmentCode(t.Context(), m[1], "laptop", time.Now()); err != nil {
		t.Errorf("the code from the command: %v, want it good for the laptop", err)
	}
	if !strings.Contains(command, "'http://bield.example.ts.net'") {
		t.Errorf("the command was %q, want Bield's address too, for the error", command)
	}

	// A script that fails shows what it printed.
	e.sshd.Handle(func(x *sshtest.Exec) int {
		_, _ = x.Stdout.Write([]byte("The first heartbeat failed.\n"))
		return 1
	})
	rec = send(t, e.h, http.MethodPost, "/api/devices/laptop/agent/install", laptopIP, "")
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("POST install = %d %s, want 200", rec.Code, rec.Body)
	}
	if got.OK || got.Output != "The first heartbeat failed." {
		t.Errorf("POST install = %+v, want a failure, with what the script printed", got)
	}

	var msgs []string
	for _, ev := range e.events(t, api.EventKindAgent) {
		msgs = append(msgs, ev.Level+": "+ev.Message)
	}
	want := []string{
		"error: The agent install on laptop over SSH failed",
		"info: Ran the agent install on laptop over SSH",
	}
	if strings.Join(msgs, "|") != strings.Join(want, "|") {
		t.Errorf("events = %q, want %q", msgs, want)
	}

	if rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/agent/install", phoneIP, ""); rec.Code != http.StatusForbidden {
		t.Errorf("POST install as a viewer = %d, want 403", rec.Code)
	}
	if rec := send(t, e.h, http.MethodPost, "/api/devices/nope/agent/install", laptopIP, ""); rec.Code != http.StatusNotFound {
		t.Errorf("POST install on no device = %d, want 404", rec.Code)
	}
}

func TestAgent_uninstallOverSSH(t *testing.T) {
	t.Parallel()
	e := newSSHEnv(t, testStore(t), nil)
	e.setUp(t)
	var command string
	e.sshd.Handle(func(x *sshtest.Exec) int {
		command = x.Command
		return 0
	})
	rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/agent/uninstall", laptopIP, "")
	var got api.AgentRun
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || !got.OK {
		t.Fatalf("POST uninstall = %d %s, want 200 and ok", rec.Code, rec.Body)
	}
	if !strings.Contains(command, "'http://bield.example.ts.net/api/agent/uninstall.sh'") {
		t.Errorf("the command was %q, want it to download the uninstall script", command)
	}
	if strings.Contains(command, "/api/agent/install/") {
		t.Errorf("the command was %q, want no enrollment code", command)
	}
}

func TestAgent_remoteOutputIsTrimmed(t *testing.T) {
	t.Parallel()
	e := newSSHEnv(t, testStore(t), nil)
	e.setUp(t)
	long := strings.Repeat("é", 20_000)
	e.sshd.Handle(func(x *sshtest.Exec) int {
		_, _ = x.Stdout.Write([]byte(long))
		return 1
	})
	rec := send(t, e.h, http.MethodPost, "/api/devices/laptop/agent/install", laptopIP, "")
	var got api.AgentRun
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("POST install = %d, want 200", rec.Code)
	}
	if n := len(got.Output); n > 16<<10+len("…") || !strings.HasPrefix(got.Output, "…é") || strings.ContainsRune(got.Output, '�') {
		t.Errorf("output is %d bytes, starts %q, want the end of it, at most 16 KiB, in whole characters", n, got.Output[:8])
	}
}

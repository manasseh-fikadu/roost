package ssh_test

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/ssh"
	"github.com/manasseh-fikadu/bield/internal/ssh/sshtest"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

func TestManager_tailscaleSSH(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Bield's key isn't on the laptop, which needs none.
		e := newEnv(t, envOptions{noRun: true, unauthorized: true})
		e.laptop.TailscaleSSH(sshtest.PolicyAccept)
		ok(e.laptop)
		e.setUp(t, "laptop", "alice")

		a, err := e.m.Check(t.Context(), "laptop")
		if err != nil {
			t.Fatal(err)
		}
		if a.State != ssh.StateOK || a.Method != ssh.MethodTailscale || a.Refusal != "" {
			t.Errorf("Check = %+v, want ok through Tailscale SSH", a)
		}
		if got := e.laptop.Logins(); len(got) != 1 || got[0] != "alice" {
			t.Errorf("logins = %q, want alice once", got)
		}
		if got := e.m.Access()["laptop"]; got.Method != ssh.MethodTailscale {
			t.Errorf("Access = %+v, want the method to stay", got)
		}
	})
}

func TestManager_keyMethod(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{noRun: true})
		ok(e.laptop)
		e.setUp(t, "laptop", "alice")

		a, err := e.m.Check(t.Context(), "laptop")
		if err != nil {
			t.Fatal(err)
		}
		if a.State != ssh.StateOK || a.Method != ssh.MethodKey {
			t.Errorf("Check = %+v, want ok with Bield's key", a)
		}
	})
}

func TestManager_tailscaleSSHRefusals(t *testing.T) {
	for _, tt := range []struct {
		name    string
		policy  sshtest.Policy
		refusal ssh.Refusal
		want    string
	}{
		{
			name:    "check",
			policy:  sshtest.PolicyCheck,
			refusal: ssh.RefusedCheck,
			want:    "Tailscale SSH on laptop wants a browser check before it lets Bield in, and Bield can't make one",
		},
		{
			name:    "no rule for the machine",
			policy:  sshtest.PolicyDenyNode,
			refusal: ssh.RefusedNode,
			want:    "the tailnet policy doesn't let Bield use Tailscale SSH to log in to laptop",
		},
		{
			name:    "no rule for the user",
			policy:  sshtest.PolicyDenyUser,
			refusal: ssh.RefusedUser,
			want:    "Tailscale SSH on laptop won't let Bield log in as alice: the tailnet policy doesn't allow that user, or the machine has no account for it",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// Bield's key is on the laptop, but Tailscale SSH answers first.
				e := newEnv(t, envOptions{noRun: true})
				e.laptop.TailscaleSSH(tt.policy)
				e.setUp(t, "laptop", "alice")

				_, err := e.m.Exec(t.Context(), "laptop", "true")
				ce, isConnect := errors.AsType[*ssh.ConnectError](err)
				if !isConnect || ce.Msg != tt.want || ce.Refusal != tt.refusal {
					t.Errorf("Exec = %v, want %q (%s)", err, tt.want, tt.refusal)
				}
				a := e.m.Access()["laptop"]
				if a.State != ssh.StateFailing || a.Error != tt.want || a.Refusal != tt.refusal || a.Method != "" {
					t.Errorf("access = %+v, want failing with %q and the refusal %s", a, tt.want, tt.refusal)
				}
			})
		})
	}
}

// A policy change that lets Bield in clears the refusal.
func TestManager_tailscaleSSHRefusalClears(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := newEnv(t, envOptions{noRun: true})
		ok(e.laptop)
		e.laptop.TailscaleSSH(sshtest.PolicyDenyNode)
		e.setUp(t, "laptop", "alice")
		if _, err := e.m.Exec(t.Context(), "laptop", "true"); err == nil {
			t.Fatal("Exec worked, want a refusal")
		}

		e.laptop.TailscaleSSH(sshtest.PolicyAccept)
		time.Sleep(time.Second)
		a, err := e.m.Check(t.Context(), "laptop")
		if err != nil {
			t.Fatal(err)
		}
		if a.State != ssh.StateOK || a.Refusal != "" || a.Error != "" || a.Method != ssh.MethodTailscale {
			t.Errorf("Check = %+v, want ok through Tailscale SSH, with no refusal", a)
		}
	})
}

func TestPolicySnippet(t *testing.T) {
	bield := tailnet.Device{ID: "bield", Owner: "alice@example.com"}
	tagged := tailnet.Device{ID: "bield", Tags: []string{"tag:bield", "tag:other"}}
	for _, tt := range []struct {
		name     string
		src, dst tailnet.Device
		user     string
		want     string
	}{
		{
			name: "Bield and the machine belong to one user",
			src:  bield,
			dst:  tailnet.Device{HostName: "laptop", Owner: "alice@example.com"},
			user: "alice",
			want: `{
  "action": "accept",
  "src": ["alice@example.com"],
  "dst": ["autogroup:self"],
  "users": ["alice"]
}`,
		},
		{
			// Headscale's user without an email is a name, and a policy
			// refers to it as name@.
			name: "a user with no @ in their login",
			src:  tailnet.Device{ID: "bield", Owner: "alice"},
			dst:  tailnet.Device{HostName: "laptop", Owner: "alice"},
			user: "alice",
			want: `{
  "action": "accept",
  "src": ["alice@"],
  "dst": ["autogroup:self"],
  "users": ["alice"]
}`,
		},
		{
			name: "a tagged machine",
			src:  bield,
			dst:  tailnet.Device{HostName: "web", Tags: []string{"tag:server"}},
			user: "deploy",
			want: `{
  "action": "accept",
  "src": ["alice@example.com"],
  "dst": ["tag:server"],
  "users": ["deploy"]
}`,
		},
		{
			name: "a tagged Bield",
			src:  tagged,
			dst:  tailnet.Device{HostName: "web", Tags: []string{"tag:server"}},
			user: "deploy",
			want: `{
  "action": "accept",
  "src": ["tag:bield"],
  "dst": ["tag:server"],
  "users": ["deploy"]
}`,
		},
		{
			name: "a machine that has to be tagged first",
			src:  tagged,
			dst:  tailnet.Device{HostName: "laptop", Owner: "alice@example.com"},
			user: "alice",
			want: `{
  "action": "accept",
  "src": ["tag:bield"],
  // Tailscale SSH rules can only name a machine by its tag here.
  // Tag laptop and use its tag.
  "dst": ["tag:servers"],
  "users": ["alice"]
}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ssh.PolicySnippet(tt.src, tt.dst, tt.user); got != tt.want {
				t.Errorf("PolicySnippet() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

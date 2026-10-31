package ssh

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	gossh "golang.org/x/crypto/ssh"

	"github.com/manasseh-fikadu/roost/internal/tailnet"
)

// Tailscale SSH is an SSH server inside tailscaled. It answers connections
// to its machine's tailnet address on port 22, and lets in whoever the tailnet
// policy says, with no key: a client that sends "none" as its authentication
// is as good as one with a key, and x/crypto's client tries "none" first. The
// policy has the last word: a rule can accept Roost, ask Roost to visit a URL
// first (action "check"), or not cover it at all. Tailscale SSH says which of
// them it did in a banner before it hangs up or holds the login.

// Method says how Roost logged in to a machine.
type Method string

// The ways Roost logs in.
const (
	// MethodKey is a login with Roost's key, which an admin put in the
	// machine's authorized_keys.
	MethodKey Method = "key"
	// MethodTailscale is a login to the machine's Tailscale SSH, which let
	// Roost in without a key.
	MethodTailscale Method = "tailscale"
)

// Refusal says why a machine's Tailscale SSH didn't let Roost in, when the
// tailnet policy needs a change. Roost shows the rule to add.
type Refusal string

// The refusals a policy can cause.
const (
	// RefusedCheck means the rule that covers Roost has action "check", which
	// asks for a browser visit that Roost can't make.
	RefusedCheck Refusal = "check"
	// RefusedNode means no rule lets Roost SSH to the machine.
	RefusedNode Refusal = "node"
	// RefusedUser means a rule lets Roost in, but not as the user it asked
	// for.
	RefusedUser Refusal = "user"
)

// methodOf says how c, a client that is logged in, got in.
func methodOf(c *gossh.Client) Method {
	if strings.Contains(string(c.ServerVersion()), "Tailscale") {
		return MethodTailscale
	}
	return MethodKey
}

// tailscaleRefusal works out why Tailscale SSH on a machine turned Roost
// away from the banners it sent. msg says why for people, and is "" if none
// of the banners came from Tailscale SSH. refusal is "" unless the policy
// needs a change.
func tailscaleRefusal(name, user string, banners []string) (refusal Refusal, msg string) {
	text := strings.Join(banners, "")
	switch {
	case strings.Contains(text, "does not permit you to SSH as user"):
		// Tailscale says the same when the machine has no such account.
		return RefusedUser, fmt.Sprintf("Tailscale SSH on %s won't let Roost log in as %s: the tailnet policy doesn't allow that user, or the machine has no account for it", name, user)
	case strings.Contains(text, "does not permit you to SSH to this node"):
		return RefusedNode, "the tailnet policy doesn't let Roost use Tailscale SSH to log in to " + name
	case strings.Contains(text, "Tailscale SSH requires an additional check"):
		return RefusedCheck, fmt.Sprintf("Tailscale SSH on %s wants a browser check before it lets Roost in, and Roost can't make one", name)
	}
	// Anything else it says, such as that the user doesn't exist on the
	// machine, is for the admin to read.
	for _, b := range banners {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(b), "tailscale:"); ok && strings.TrimSpace(rest) != "" {
			return "", fmt.Sprintf("Tailscale SSH on %s said: %s", name, strings.TrimSpace(rest))
		}
	}
	return "", ""
}

// PolicySnippet returns a rule for the "ssh" section of the tailnet policy
// that lets src log in to dst as user, with no browser check. src is the
// node Roost connects from, and dst the machine it logs in to.
func PolicySnippet(src, dst tailnet.Device, user string) string {
	var b strings.Builder
	b.WriteString("{\n  \"action\": \"accept\",\n")
	b.WriteString("  \"src\": [" + jsonString(policySrc(src)) + "],\n")
	if d, ok := policyDst(src, dst); ok {
		b.WriteString("  \"dst\": [" + jsonString(d) + "],\n")
	} else {
		b.WriteString("  // Tailscale SSH rules can only name a machine by its tag here.\n")
		b.WriteString("  // Tag " + commentSafe(deviceName(dst)) + " and use its tag.\n")
		b.WriteString("  \"dst\": [\"tag:servers\"],\n")
	}
	b.WriteString("  \"users\": [" + jsonString(user) + "]\n}")
	return b.String()
}

// policySrc is how the policy names the node d.
func policySrc(d tailnet.Device) string {
	switch {
	case len(d.Tags) > 0:
		return d.Tags[0]
	case d.Owner != "":
		return d.Owner
	}
	return "autogroup:member"
}

// policyDst is how an ssh rule from src names the machine dst. It can be a
// tag, or "autogroup:self" for a user's own machines. It reports false if
// there's no way to name dst without tagging it.
func policyDst(src, dst tailnet.Device) (string, bool) {
	switch {
	case len(dst.Tags) > 0:
		return dst.Tags[0], true
	case len(src.Tags) == 0 && src.Owner != "" && src.Owner == dst.Owner:
		return "autogroup:self", true
	}
	return "", false
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		// A string always marshals; this keeps the rule well formed anyway.
		return strconv.Quote(s)
	}
	return string(b)
}

// commentSafe returns s without the characters that would end a line comment.
func commentSafe(s string) string {
	return strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
}

// SPDX-License-Identifier: MIT

package agent_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/manasseh-fikadu/roost/agent"
)

func TestInstall(t *testing.T) {
	t.Parallel()
	script := agent.Install("https://roost.example.ts.net", "CODE234")
	for _, want := range []string{
		"\nSERVER_URL='https://roost.example.ts.net'\n",
		"\nCODE='CODE234'\n",
		`AGENT_VERSION="` + agent.Version + `"`,
		"\nROOST_AGENT_EOF\n",
		"\nNAME=\"roost-agent\"\n",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("Install() doesn't contain %q", want)
		}
	}
	if strings.Contains(script, "__") {
		t.Error("Install() left a placeholder in the script")
	}

	// A URL can't break out of its quotes.
	script = agent.Install("https://x/'; rm -rf ~; '", "CODE234")
	if want := `SERVER_URL='https://x/'\''; rm -rf ~; '\'''`; !strings.Contains(script, want) {
		t.Errorf("Install() with a quote in the URL doesn't contain %s", want)
	}
	checkSyntax(t, script)
}

func TestUninstall(t *testing.T) {
	t.Parallel()
	script := agent.Uninstall()
	for _, want := range []string{"\nNAME=\"roost-agent\"\n", "/api/agent/enrollment"} {
		if !strings.Contains(script, want) {
			t.Errorf("Uninstall() doesn't contain %q", want)
		}
	}
	if strings.Contains(script, "__") {
		t.Error("Uninstall() left a placeholder in the script")
	}
	checkSyntax(t, script)
}

// checkSyntax fails the test if bash can't read script.
func checkSyntax(t *testing.T, script string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash to check the script's syntax")
	}
	cmd := exec.CommandContext(t.Context(), "bash", "-n")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("bash -n = %v: %s", err, out)
	}
}

func TestOlder(t *testing.T) {
	t.Parallel()
	if agent.Version == "" {
		t.Fatal("Version is empty")
	}
	for _, tt := range []struct {
		version string
		want    bool
	}{
		{agent.Version, false},
		{"0.9.9", true},
		{"1.2.0", true},
		{"1.10.0", false},
		{"99.0.0", false},
		{"", true},
		{"1.2", true},
		{"one.two.three", true},
		{"1.2.-1", true},
	} {
		if got := agent.Older(tt.version); got != tt.want {
			t.Errorf("Older(%q) = %v, want %v (the hub serves %s)", tt.version, got, tt.want, agent.Version)
		}
	}
}

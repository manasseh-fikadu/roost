// SPDX-License-Identifier: MIT

// Package agent holds the agent script and the script that installs it,
// which the hub serves to the machines that run the agent.
package agent

import (
	_ "embed"
	"regexp"
	"strconv"
	"strings"
)

var (
	//go:embed agent.sh
	script string
	//go:embed install.sh
	installer string
)

// Version is the version of the agent that the hub serves, such as 1.2.0.
var Version = regexp.MustCompile(`(?m)^AGENT_VERSION="([^"]+)"$`).FindStringSubmatch(script)[1]

// Install returns the script that installs the agent and has it report to
// the hub at serverURL. It reads the agents' token from ROOST_AGENT_TOKEN,
// so the script itself holds no secret.
func Install(serverURL string) string {
	return strings.NewReplacer(
		"__SERVER_URL__", shellQuote(serverURL),
		"__AGENT_SCRIPT__", strings.TrimRight(script, "\n"),
	).Replace(installer)
}

// Older reports whether version is older than the agent the hub serves. A
// version it can't read counts as older.
func Older(version string) bool {
	have, ok := parse(version)
	if !ok {
		return true
	}
	want, _ := parse(Version)
	for i := range want {
		if have[i] != want[i] {
			return have[i] < want[i]
		}
	}
	return false
}

// parse reads a version such as 1.2.0.
func parse(version string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(version, ".")
	if len(parts) != len(v) {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// shellQuote quotes s as one word for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

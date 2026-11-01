package demo

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// unitInfo is a systemd unit of the machine Roost pretends to run on.
type unitInfo struct {
	name, desc  string
	active, sub string
	pid         int
	memory      uint64
	since       time.Duration
	enabledFile bool
}

var hubUnits = []unitInfo{
	{name: "ssh.service", desc: "OpenBSD Secure Shell server", active: "active", sub: "running", pid: 611, memory: 7 << 20, since: -61 * 24 * time.Hour, enabledFile: true},
	{name: "docker.service", desc: "Docker Application Container Engine", active: "active", sub: "running", pid: 702, memory: 96 << 20, since: -61 * 24 * time.Hour, enabledFile: true},
	{name: "tailscaled.service", desc: "Tailscale node agent", active: "active", sub: "running", pid: 655, memory: 58 << 20, since: -61 * 24 * time.Hour, enabledFile: true},
	{name: "cron.service", desc: "Regular background program processing daemon", active: "active", sub: "running", pid: 601, memory: 2 << 20, since: -61 * 24 * time.Hour, enabledFile: true},
	{name: "unattended-upgrades.service", desc: "Unattended Upgrades Shutdown", active: "active", sub: "running", pid: 690, memory: 9 << 20, since: -61 * 24 * time.Hour, enabledFile: true},
	{name: "fail2ban.service", desc: "Fail2Ban Service", active: "active", sub: "running", pid: 714, memory: 31 << 20, since: -61 * 24 * time.Hour, enabledFile: true},
	{name: "backup.service", desc: "Nightly restic backup to the NAS", active: "failed", sub: "failed", since: -9 * time.Hour, enabledFile: true},
}

// Systemctl stands in for systemctl, for systemd.Options.Run: it answers
// the questions the collector asks about the units of the machine Roost
// pretends to run on.
func (d *Demo) Systemctl(_ context.Context, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "--user" {
		// The demo's Roost has no user session worth showing.
		return nil, nil
	}
	var out strings.Builder
	switch {
	case slices.Contains(args, "list-unit-files"):
		for _, u := range hubUnits {
			fmt.Fprintf(&out, "%s enabled\n", u.name)
		}
	case slices.Contains(args, "list-units") && slices.Contains(args, "--failed"):
		for _, u := range hubUnits {
			if u.active == "failed" {
				fmt.Fprintf(&out, "%s loaded %s %s %s\n", u.name, u.active, u.sub, u.desc)
			}
		}
	case slices.Contains(args, "list-units"):
		for _, u := range hubUnits {
			fmt.Fprintf(&out, "%s loaded %s %s %s\n", u.name, u.active, u.sub, u.desc)
		}
	case slices.Contains(args, "show"):
		i := slices.Index(args, "--")
		if i < 0 {
			return nil, nil
		}
		for _, name := range args[i+1:] {
			idx := slices.IndexFunc(hubUnits, func(u unitInfo) bool { return u.name == name })
			if idx < 0 {
				fmt.Fprintf(&out, "Id=%s\nLoadState=not-found\n\n", name)
				continue
			}
			u := hubUnits[idx]
			fmt.Fprintf(&out, "Id=%s\nLoadState=loaded\nDescription=%s\nActiveState=%s\nSubState=%s\n", u.name, u.desc, u.active, u.sub)
			fmt.Fprintf(&out, "ActiveEnterTimestamp=@%d\n", d.start.Add(u.since).Unix())
			if u.pid > 0 {
				fmt.Fprintf(&out, "MainPID=%d\nMemoryCurrent=%d\n", u.pid, u.memory)
			} else {
				fmt.Fprint(&out, "MainPID=0\nMemoryCurrent=[not set]\n")
			}
			out.WriteString("\n")
		}
	}
	return []byte(out.String()), nil
}

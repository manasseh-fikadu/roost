package api

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/manasseh-fikadu/bield/internal/alerts"
	"github.com/manasseh-fikadu/bield/internal/docker"
	"github.com/manasseh-fikadu/bield/internal/systemd"
	"github.com/manasseh-fikadu/bield/internal/tailnet"
)

// cleanExits are the exit codes of a container that was stopped rather than
// crashed: a clean exit, and the codes of SIGINT, SIGKILL and SIGTERM, which
// `docker stop` sends.
var cleanExits = []int{0, 130, 137, 143}

// newContainers returns the containers on the machine Bield runs on, as the
// API shows them, on the device deviceID, with the checks of their ports.
// hub holds the names and addresses of that machine.
func newContainers(r docker.Reading, deviceID string, cs []Check, hub []string) []Container {
	out := make([]Container, 0, len(r.Containers))
	for _, c := range r.Containers {
		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}
		ct := Container{
			ID:            id,
			Name:          c.Name,
			Image:         c.Image,
			Project:       c.Project,
			Service:       c.Service,
			State:         ContainerState(c.State),
			Status:        c.Status,
			Health:        c.Health,
			ExitCode:      c.ExitCode,
			OOMKilled:     c.OOMKilled,
			RestartCount:  c.RestartCount,
			RestartPolicy: c.RestartPolicy,
			StartedAt:     timePtr(c.StartedAt),
			FinishedAt:    timePtr(c.FinishedAt),
			Ports:         make([]ContainerPort, len(c.Ports)),
			DeviceID:      deviceID,
			CheckIDs:      []string{},
		}
		var public []int
		for i, p := range c.Ports {
			ct.Ports[i] = ContainerPort{IP: p.IP, Private: int(p.Private), Public: int(p.Public), Type: p.Type}
			if p.Public != 0 {
				public = append(public, int(p.Public))
			}
		}
		ct.Problem, _ = containerProblem(c)
		for _, check := range cs {
			if host, port, ok := checkHostPort(check); ok && slices.Contains(public, port) && slices.Contains(hub, host) {
				ct.CheckIDs = append(ct.CheckIDs, check.ID)
			}
		}
		out = append(out, ct)
	}
	return out
}

// containerProblem says what's wrong with a container, if anything, and how
// much it matters. A container that stopped cleanly only has a problem if
// its restart policy says it should run.
func containerProblem(c docker.Container) (string, alerts.Severity) {
	switch c.State {
	case "running":
		if c.Health == "unhealthy" {
			return "is unhealthy", alerts.Warning
		}
	case "restarting":
		return fmt.Sprintf("keeps restarting (%d restarts)", c.RestartCount), alerts.Warning
	case "dead":
		return "is dead", alerts.Critical
	case "exited":
		switch {
		case c.OOMKilled:
			return "was killed for using too much memory", alerts.Critical
		case !slices.Contains(cleanExits, c.ExitCode):
			return fmt.Sprintf("crashed (exit %d)", c.ExitCode), alerts.Critical
		case c.RestartPolicy == "always" || c.RestartPolicy == "unless-stopped":
			return "stopped", alerts.Warning
		}
	}
	return "", ""
}

// checkHostPort returns the host and port an HTTP or TCP check connects to.
func checkHostPort(c Check) (string, int, bool) {
	switch c.Kind {
	case CheckKindHTTP:
		u, err := url.Parse(c.Target)
		if err != nil {
			return "", 0, false
		}
		port := u.Port()
		if port == "" {
			port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
		}
		n, err := strconv.Atoi(port)
		return strings.ToLower(u.Hostname()), n, err == nil
	case CheckKindTCP:
		host, port, err := net.SplitHostPort(c.Target)
		if err != nil {
			return "", 0, false
		}
		n, err := strconv.Atoi(port)
		return strings.ToLower(host), n, err == nil
	case CheckKindDNS, CheckKindPing:
	}
	return "", 0, false
}

// hubNames returns the names and addresses that reach the machine Bield
// runs on: loopback, and its nodes' IPs and names.
func hubNames(st *tailnet.Status) []string {
	out := []string{"localhost", "127.0.0.1", "::1"}
	if st == nil {
		return out
	}
	for _, d := range append([]tailnet.Device{st.Self}, st.Peers...) {
		if d.ID != st.Self.ID && !d.SameMachine {
			continue
		}
		for _, ip := range d.IPs {
			out = append(out, ip.String())
		}
		if d.DNSName != "" {
			out = append(out, strings.ToLower(d.DNSName), strings.ToLower(firstLabel(d.DNSName)))
		}
		if d.HostName != "" {
			out = append(out, strings.ToLower(d.HostName))
		}
	}
	return out
}

// newUnits returns the systemd units of a reading, as the API shows them, on
// the device deviceID.
func newUnits(r systemd.Reading, deviceID string) []Unit {
	out := make([]Unit, len(r.Units))
	for i, u := range r.Units {
		out[i] = Unit{
			Name:        u.Name,
			Scope:       UnitScope(u.Scope),
			Description: u.Description,
			Active:      u.Active,
			Sub:         u.Sub,
			Since:       timePtr(u.Since),
			MainPID:     u.MainPID,
			Memory:      u.Memory,
			Watched:     u.Watched,
			DeviceID:    deviceID,
		}
	}
	return out
}

// unitProblem says what's wrong with a unit, if anything.
func unitProblem(u Unit) string {
	switch {
	case u.Active == "failed":
		return "failed"
	case !u.Watched:
	case u.Active == "inactive" || u.Active == "deactivating":
		return "isn't running"
	}
	return ""
}

// unitKey names a unit with its scope, as settings.Settings.WatchedUnits does.
func unitKey(u Unit) string {
	if u.Scope == UnitScopeUser {
		return "user:" + u.Name
	}
	return u.Name
}

// fresh reports whether a reading taken at worked, at now, is new enough to
// say what's true now.
func fresh(at time.Time, err error, now time.Time) bool {
	return err == nil && !at.IsZero() && now.Sub(at) < freshFor
}

// freshFor is how old a collector's reading can be and still say what's true.
const freshFor = 2 * time.Minute

package api

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/manasseh-fikadu/bield/internal/store"
)

// warmUp is how long after the tailnet's first status Bield waits before it
// logs devices coming and going. Right after it starts, it can see peers
// offline, or not at all.
const warmUp = time.Minute

// activity logs what changes from one snapshot to the next in the activity
// log: devices joining, leaving, going offline and coming online, agents
// starting and stopping reporting, and containers starting and stopping.
// Alerts log themselves. It's not safe for concurrent use.
type activity struct {
	// since is when the tailnet's first status came, or zero.
	since time.Time
	// devices and containers are as the last snapshot had them, or nil
	// before Bield knows them.
	devices    map[string]seenDevice
	containers map[string]seenContainer
}

type seenDevice struct {
	name   string
	online bool
	// agentSeen is when its agent's latest heartbeat came, and stale
	// reports whether it stopped reporting.
	agentSeen time.Time
	stale     bool
}

type seenContainer struct {
	state    ContainerState
	exitCode int
	oom      bool
}

// observe returns the events between the last snapshot and snap, built at
// now from src.
func (a *activity) observe(snap Snapshot, src sources, now time.Time) []store.Event {
	var events []store.Event
	if fresh(src.tailnet.StatusAt, src.tailnet.StatusErr, now) && src.tailnet.Status != nil {
		if a.since.IsZero() {
			a.since = now
		}
		devices := make(map[string]seenDevice, len(snap.Devices))
		for _, d := range snap.Devices {
			s := seenDevice{name: d.Name, online: d.Online}
			if ag := d.Agent; ag != nil {
				s.stale = ag.Stale
				if ag.LastSeen != nil {
					s.agentSeen = *ag.LastSeen
				}
			}
			devices[d.ID] = s
		}
		if a.devices != nil && now.Sub(a.since) >= warmUp {
			events = append(events, deviceEvents(a.devices, devices, snap, now)...)
		}
		a.devices = devices
	}

	if r := src.docker; r != nil && fresh(r.At, r.Err, now) {
		containers := make(map[string]seenContainer, len(snap.Containers))
		for _, c := range snap.Containers {
			containers[c.Name] = seenContainer{state: c.State, exitCode: c.ExitCode, oom: c.OOMKilled}
		}
		if a.containers != nil {
			deviceID := ""
			if st := src.tailnet.Status; st != nil {
				deviceID = st.Self.ID
			}
			events = append(events, containerEvents(a.containers, containers, deviceID, now)...)
		}
		a.containers = containers
	}
	return events
}

func deviceEvents(before, after map[string]seenDevice, snap Snapshot, now time.Time) []store.Event {
	var events []store.Event
	add := func(level EventLevel, kind EventKind, deviceID, message string) {
		events = append(events, store.Event{At: now, Level: string(level), Kind: string(kind), Message: message, DeviceID: deviceID})
	}
	for _, d := range snap.Devices {
		is, was := after[d.ID], before[d.ID]
		if _, ok := before[d.ID]; !ok {
			add(EventLevelInfo, EventKindDevice, d.ID, d.Name+" joined the tailnet")
			continue
		}
		switch {
		case d.Self:
		case is.online && !was.online:
			add(EventLevelSuccess, EventKindDevice, d.ID, d.Name+" came online")
		case !is.online && was.online:
			add(EventLevelWarning, EventKindDevice, d.ID, d.Name+" went offline")
		}
		switch {
		case was.agentSeen.IsZero() && !is.agentSeen.IsZero():
			add(EventLevelSuccess, EventKindAgent, d.ID, "The agent on "+d.Name+" started reporting")
		case is.stale && !was.stale:
			add(EventLevelWarning, EventKindAgent, d.ID, "The agent on "+d.Name+" stopped reporting")
		case was.stale && is.agentSeen.After(was.agentSeen):
			add(EventLevelSuccess, EventKindAgent, d.ID, "The agent on "+d.Name+" is reporting again")
		}
	}
	for _, id := range slices.Sorted(maps.Keys(before)) {
		if _, ok := after[id]; !ok {
			add(EventLevelWarning, EventKindDevice, id, before[id].name+" left the tailnet")
		}
	}
	return events
}

func containerEvents(before, after map[string]seenContainer, deviceID string, now time.Time) []store.Event {
	var events []store.Event
	add := func(level EventLevel, message string) {
		events = append(events, store.Event{At: now, Level: string(level), Kind: string(EventKindContainer), Message: message, DeviceID: deviceID})
	}
	for _, name := range slices.Sorted(maps.Keys(after)) {
		c, was := after[name], before[name]
		_, existed := before[name]
		switch {
		case c.state == ContainerStateRunning && (!existed || was.state != ContainerStateRunning):
			add(EventLevelSuccess, "Container "+name+" started")
		case !existed || c.state == was.state:
		case was.state == ContainerStateRunning && c.state == ContainerStateExited:
			switch {
			case c.oom:
				add(EventLevelError, "Container "+name+" was killed for using too much memory")
			case !slices.Contains(cleanExits, c.exitCode):
				add(EventLevelError, fmt.Sprintf("Container %s crashed (exit %d)", name, c.exitCode))
			default:
				add(EventLevelInfo, "Container "+name+" stopped")
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(before)) {
		if _, ok := after[name]; !ok {
			add(EventLevelInfo, "Container "+name+" was removed")
		}
	}
	return events
}

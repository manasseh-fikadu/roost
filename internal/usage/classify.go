package usage

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// cgroupPath returns the cgroup v2 path in the text of /proc/[pid]/cgroup,
// or on cgroup v1, the path in systemd's hierarchy.
func cgroupPath(text string) string {
	var (
		v2, systemd string
		hasSystemd  bool
	)
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\n")
		// hierarchy-ID:controllers:path
		_, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		controllers, p, ok := strings.Cut(rest, ":")
		switch {
		case !ok:
		case strings.HasPrefix(line, "0::"):
			v2 = p
		case controllers == "name=systemd":
			systemd, hasSystemd = p, true
		}
	}
	if v2 == "" || (v2 == "/" && hasSystemd) {
		return systemd
	}
	return v2
}

var (
	containerScope = regexp.MustCompile(`^(?:docker|libpod|cri-containerd|crio)-([0-9a-f]{64})\.scope$`)
	containerID    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	userService    = regexp.MustCompile(`^user@\d+\.service$`)
	sessionScope   = regexp.MustCompile(`^session-[^.]+\.scope$`)
	userSlice      = regexp.MustCompile(`^user-(\d+)\.slice$`)
	escaped        = regexp.MustCompile(`\\x([0-9a-fA-F]{2})`)
	launchSuffix   = regexp.MustCompile(`-(?:[0-9a-f]{8,}|\d+)$`)
)

// classify works out which group a process belongs to, from its cgroup
// path, whether it's a kernel thread, and its name. userName names a user
// ID. The agent's awk does the same, so a group has one key on every
// machine.
func classify(cgroup string, kthread bool, name string, userName func(uid int) string) (Kind, string) {
	if kthread {
		return KindKernel, "Kernel"
	}
	var segs []string
	for s := range strings.SplitSeq(cgroup, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	// No systemd groups, as in the root cgroup or inside a container: group
	// by program.
	if len(segs) == 0 {
		return KindOther, name
	}
	for i, s := range segs {
		id := ""
		if m := containerScope.FindStringSubmatch(s); m != nil {
			id = m[1]
		} else if s == "docker" && i+1 < len(segs) && containerID.MatchString(segs[i+1]) {
			id = segs[i+1]
		}
		if id != "" {
			// A process's cgroup has the container's ID, but not its name,
			// so the short ID names it.
			return KindContainer, id[:12]
		}
	}
	if segs[0] == "init.scope" {
		return KindService, "systemd"
	}
	if segs[0] == "user.slice" {
		if at := slices.IndexFunc(segs, userService.MatchString); at >= 0 {
			rest := segs[at+1:]
			if len(rest) == 0 || rest[0] == "init.scope" {
				return KindUserService, "systemd --user"
			}
			if i := slices.IndexFunc(rest, func(s string) bool { return strings.HasSuffix(s, ".service") }); i >= 0 {
				return KindUserService, unitName(rest[i])
			}
			// An app with a slice of its own is one group, however many
			// scopes it starts.
			if rest[0] == "app.slice" && len(rest) > 1 && strings.HasPrefix(rest[1], "app-") && strings.HasSuffix(rest[1], ".slice") {
				return KindUserService, appName(rest[1])
			}
			return KindUserService, appName(rest[len(rest)-1])
		}
		if slices.IndexFunc(segs, sessionScope.MatchString) >= 0 {
			who := "unknown"
			if len(segs) > 1 {
				if m := userSlice.FindStringSubmatch(segs[1]); m != nil {
					uid, _ := strconv.Atoi(m[1])
					who = userName(uid)
				}
			}
			return KindSession, "Login sessions (" + who + ")"
		}
	}
	if i := slices.IndexFunc(segs, func(s string) bool { return strings.HasSuffix(s, ".service") }); i >= 0 {
		return KindService, unitName(segs[i])
	}
	return KindOther, appName(segs[len(segs)-1])
}

// unitName returns a systemd unit's name without its type, unescaped, such
// as "my-app" for "my\x2dapp.service".
func unitName(unit string) string {
	for _, suffix := range []string{".service", ".scope", ".slice"} {
		if s, ok := strings.CutSuffix(unit, suffix); ok {
			unit = s
			break
		}
	}
	return escaped.ReplaceAllStringFunc(unit, func(m string) string {
		// Each escape is one byte, and a run of them can spell out a
		// character in UTF-8.
		b, _ := strconv.ParseUint(m[2:], 16, 8)
		return string([]byte{byte(b)})
	})
}

// appName returns the name of an app from its unit, without the number
// each launch adds, so its history lines up: "gnome-firefox" for
// "app-gnome-firefox-4213.scope".
func appName(unit string) string {
	name := launchSuffix.ReplaceAllString(strings.TrimPrefix(unitName(unit), "app-"), "")
	if name == "" {
		return unit
	}
	return name
}

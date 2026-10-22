package usage

import (
	"strconv"
	"testing"
)

func TestClassify(t *testing.T) {
	t.Parallel()
	const id = "4f1c2a3b5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f7a8"
	users := func(uid int) string {
		if uid == 1000 {
			return "alice"
		}
		return strconv.Itoa(uid)
	}
	for _, tt := range []struct {
		name, cgroup string
		kthread      bool
		wantKind     Kind
		wantName     string
	}{
		{"kernel thread", "", true, KindKernel, "Kernel"},
		{"root cgroup", "/", false, KindOther, "proc"},
		{"no cgroup", "", false, KindOther, "proc"},
		{"systemd", "/init.scope", false, KindService, "systemd"},
		{"system service", "/system.slice/ssh.service", false, KindService, "ssh"},
		{"service in a slice", "/system.slice/system-getty.slice/getty@tty1.service", false, KindService, "getty@tty1"},
		{"escaped dash", `/system.slice/my\x2dapp.service`, false, KindService, "my-app"},
		{"docker scope", "/system.slice/docker-" + id + ".scope", false, KindContainer, id[:12]},
		{"podman scope", "/machine.slice/libpod-" + id + ".scope/container", false, KindContainer, id[:12]},
		{"cgroup v1 docker", "/docker/" + id, false, KindContainer, id[:12]},
		{"user manager", "/user.slice/user-1000.slice/user@1000.service/init.scope", false, KindUserService, "systemd --user"},
		{"user manager itself", "/user.slice/user-1000.slice/user@1000.service", false, KindUserService, "systemd --user"},
		{"user service", "/user.slice/user-1000.slice/user@1000.service/app.slice/syncthing.service", false, KindUserService, "syncthing"},
		{
			"app with a slice",
			"/user.slice/user-1000.slice/user@1000.service/app.slice/app-editor.slice/app-editor-tabs.slice/app-editor-tabs-082379d79b4e.slice/run-406b8999.scope",
			false, KindUserService, "editor",
		},
		{"desktop app scope", "/user.slice/user-1000.slice/user@1000.service/app.slice/app-gnome-firefox-4213.scope", false, KindUserService, "gnome-firefox"},
		{"ssh session", "/user.slice/user-1000.slice/session-12.scope", false, KindSession, "Login sessions (alice)"},
		{"session of an unknown user", "/user.slice/user-1234.slice/session-3.scope", false, KindSession, "Login sessions (1234)"},
		{"some other slice", "/machine.slice/vm-runner.scope", false, KindOther, "vm-runner"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kind, name := classify(tt.cgroup, tt.kthread, "proc", users)
			if kind != tt.wantKind || name != tt.wantName {
				t.Errorf("classify(%q) = %s %q, want %s %q", tt.cgroup, kind, name, tt.wantKind, tt.wantName)
			}
		})
	}
}

func TestCgroupPath(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, text, want string }{
		{"cgroup v2", "0::/system.slice/ssh.service\n", "/system.slice/ssh.service"},
		{"v1 with systemd", "12:cpu,cpuacct:/\n1:name=systemd:/system.slice/cron.service\n0::/\n", "/system.slice/cron.service"},
		{"v2 at the root", "0::/\n", "/"},
		{"nothing", "", ""},
	} {
		if got := cgroupPath(tt.text); got != tt.want {
			t.Errorf("%s: cgroupPath() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

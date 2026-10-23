package usage_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/usage"
)

// fakeProc is a process in a fake /proc.
type fakeProc struct {
	pid          int
	comm         string
	kthread      bool
	ticks, start int
	rssPages     int
	uid          int
	cgroup, cmd  string
}

// fakeMachine returns a root directory with a /proc that has procs, whose CPU
// cores have spent total clock ticks in all, and an /etc/passwd.
func fakeMachine(total int, procs ...fakeProc) fstest.MapFS {
	fsys := fstest.MapFS{
		"proc/stat": {Data: fmt.Appendf(nil,
			"cpu  %d 0 0 0 0 0 0 0 0 0\ncpu0 1 0 0 0 0 0 0 0 0 0\ncpu1 1 0 0 0 0 0 0 0 0 0\nintr 1\n", total)},
		"proc/uptime": {Data: []byte("100.00 150.00\n")},
		"etc/passwd":  {Data: []byte("root:x:0:0:root:/root:/bin/bash\nalice:x:1000:1000::/home/alice:/bin/bash\n")},
	}
	for _, p := range procs {
		dir := fmt.Sprintf("proc/%d/", p.pid)
		flags := 0x400100
		if p.kthread {
			flags |= 0x00200000
		}
		// Fields 3 to 24 of proc(5), with utime as all the ticks.
		fsys[dir+"stat"] = &fstest.MapFile{Data: fmt.Appendf(nil, "%d (%s) S 0 0 0 0 -1 %d 0 0 0 0 %d 0 0 0 20 0 1 0 %d 1000 %d 0\n",
			p.pid, p.comm, flags, p.ticks, p.start, p.rssPages)}
		if !p.kthread {
			fsys[dir+"cmdline"] = &fstest.MapFile{Data: []byte(strings.ReplaceAll(p.cmd, " ", "\x00") + "\x00")}
			fsys[dir+"cgroup"] = &fstest.MapFile{Data: []byte("0::" + p.cgroup + "\n")}
			fsys[dir+"status"] = &fstest.MapFile{Data: fmt.Appendf(nil, "Name:\t%s\nUid:\t%d\t%d\t%d\t%d\n", p.comm, p.uid, p.uid, p.uid, p.uid)}
		}
	}
	return fsys
}

func TestSampler(t *testing.T) {
	t.Parallel()
	page := uint64(os.Getpagesize()) //nolint:gosec // G115: the page size is positive.
	ssh := fakeProc{pid: 100, comm: "sshd", ticks: 1000, start: 50, rssPages: 100, uid: 0, cgroup: "/system.slice/ssh.service", cmd: "/usr/sbin/sshd -D"}
	kworker := fakeProc{pid: 2, comm: "kworker/0:1", kthread: true, ticks: 10, start: 1}
	// Its PID is another process's by the second sample.
	gone := fakeProc{pid: 300, comm: "make", ticks: 500, start: 60, uid: 1000, cgroup: "/user.slice/user-1000.slice/session-2.scope", cmd: "make"}

	fsys := fakeMachine(10_000, ssh, kworker, gone)
	s := usage.NewSampler(fsys)
	if _, ok, err := s.Sample(time.Unix(0, 0)); ok || err != nil {
		t.Fatalf("first Sample() = %v, %v, want only readings for the next", ok, err)
	}

	// 1,000 ticks pass on all cores together.
	ssh.ticks += 100   // 10%
	kworker.ticks += 5 // 0.5%
	// These started since, so all their time counts.
	reused := fakeProc{pid: 300, comm: "cc", ticks: 20, start: 900, rssPages: 10, uid: 1000,
		cgroup: "/user.slice/user-1000.slice/session-2.scope", cmd: "cc -c main.c"}
	// The kernel cut its name to 15 bytes.
	long := fakeProc{pid: 400, comm: "language-server", ticks: 50, start: 950, rssPages: 200, uid: 1000,
		cgroup: "/user.slice/user-1000.slice/user@1000.service/app.slice/editor.service",
		cmd:    "/opt/bin/language-server-protocol --stdio"}
	node := fakeProc{pid: 500, comm: "MainThread", start: 960, rssPages: 1, uid: 1000,
		cgroup: "/user.slice/user-1000.slice/user@1000.service/app.slice/editor.service", cmd: "node server.js"}
	clear(fsys)
	maps.Copy(fsys, fakeMachine(11_000, ssh, kworker, reused, long, node))

	at := time.Unix(5, 0)
	got, ok, err := s.Sample(at)
	if !ok || err != nil {
		t.Fatalf("second Sample() = %v, %v", ok, err)
	}
	editor := "user-service:editor"
	sessions := "session:Login sessions (alice)"
	want := usage.Sample{
		At: at, Source: usage.SourceHub, Cores: 2,
		Groups: []usage.Group{
			{Key: "service:ssh", Kind: usage.KindService, Name: "ssh", CPU: 10, Mem: 100 * page, Procs: 1, Top: []string{"sshd"}},
			{Key: editor, Kind: usage.KindUserService, Name: "editor", CPU: 5, Mem: 201 * page, Procs: 2, Top: []string{"language-server-protocol", "node"}},
			{Key: sessions, Kind: usage.KindSession, Name: "Login sessions (alice)", CPU: 2, Mem: 10 * page, Procs: 1, Top: []string{"cc"}},
			{Key: "kernel:Kernel", Kind: usage.KindKernel, Name: "Kernel", CPU: 0.5, Procs: 1, Top: []string{"kworker/0:1"}},
		},
		Procs: []usage.Proc{
			{PID: 100, Name: "sshd", User: "root", Cmd: "/usr/sbin/sshd -D", CPU: 10, Mem: 100 * page, Group: "service:ssh"},
			{PID: 400, Name: "language-server-protocol", User: "alice", Cmd: "/opt/bin/language-server-protocol --stdio", CPU: 5, Mem: 200 * page, Group: editor},
			{PID: 300, Name: "cc", User: "alice", Cmd: "cc -c main.c", CPU: 2, Mem: 10 * page, Group: sessions},
			{PID: 2, Name: "kworker/0:1", Cmd: "[kworker/0:1]", CPU: 0.5, Group: "kernel:Kernel"},
			{PID: 500, Name: "node", User: "alice", Cmd: "node server.js", Mem: page, Group: editor},
		},
		Total: &usage.Totals{CPU: 17.5, Mem: 311 * page, Procs: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Sample() =\n%+v\nwant\n%+v", got, want)
	}

	delete(fsys, "proc/stat")
	if _, ok, err := s.Sample(at.Add(5 * time.Second)); ok || err == nil {
		t.Errorf("Sample() without /proc/stat = %v, %v, want an error", ok, err)
	}
}

// A process can exec, change its command line or user, or move to another
// cgroup, all with the same PID. Each sample shows it as it is then.
func TestSampler_changes(t *testing.T) {
	t.Parallel()
	// An sshd child, before pam_systemd moves it into alice's session.
	child := fakeProc{pid: 100, comm: "sshd", start: 50, rssPages: 1, uid: 0,
		cgroup: "/system.slice/ssh.service", cmd: "sshd: alice [priv]"}
	// A container's entrypoint, before it runs exec gosu postgres postgres.
	entry := fakeProc{pid: 200, comm: "docker-entrypoi", start: 60, rssPages: 1, uid: 0,
		cgroup: "/system.slice/docker-" + strings.Repeat("0123456789abcdef", 4) + ".scope", cmd: "/bin/sh /usr/local/bin/docker-entrypoint.sh postgres"}
	fsys := fakeMachine(1000, child, entry)
	s := usage.NewSampler(fsys)
	sample := func(at time.Time) []usage.Proc {
		t.Helper()
		got, ok, err := s.Sample(at)
		if !ok || err != nil {
			t.Fatalf("Sample() = %v, %v", ok, err)
		}
		return got.Procs
	}
	if _, ok, err := s.Sample(time.Unix(0, 0)); ok || err != nil {
		t.Fatalf("first Sample() = %v, %v, want only readings for the next", ok, err)
	}
	page := uint64(os.Getpagesize()) //nolint:gosec // G115: the page size is positive.
	before := sample(time.Unix(5, 0))

	child.uid, child.cgroup, child.cmd = 1000, "/user.slice/user-1000.slice/session-3.scope", "sshd: alice@pts/0"
	entry.comm, entry.uid, entry.cmd = "postgres", 999, "postgres"
	clear(fsys)
	maps.Copy(fsys, fakeMachine(2000, child, entry))
	after := sample(time.Unix(10, 0))

	container := "container:0123456789ab"
	want := [][]usage.Proc{
		{
			{PID: 100, Name: "sshd", User: "root", Cmd: "sshd: alice [priv]", Mem: page, Group: "service:ssh"},
			{PID: 200, Name: "docker-entrypoi", User: "root", Cmd: "/bin/sh /usr/local/bin/docker-entrypoint.sh postgres", Mem: page, Group: container},
		},
		{
			{PID: 100, Name: "sshd", User: "alice", Cmd: "sshd: alice@pts/0", Mem: page, Group: "session:Login sessions (alice)"},
			// 999 isn't in /etc/passwd.
			{PID: 200, Name: "postgres", User: "999", Cmd: "postgres", Mem: page, Group: container},
		},
	}
	for i, got := range [][]usage.Proc{before, after} {
		if !reflect.DeepEqual(got, want[i]) {
			t.Errorf("sample %d: Procs =\n%+v\nwant\n%+v", i+1, got, want[i])
		}
	}
}

func TestParseAgent(t *testing.T) {
	t.Parallel()
	at := time.Unix(1_800_000_000, 0)
	for _, tt := range []struct {
		name, body string
		want       usage.Sample
		wantOK     bool
	}{
		{
			name: "what the agent sends",
			body: `{"groups":[["service:ssh",1.5,2048,2,["sshd"]],["app:Google Chrome",30.25,1000,12,["Google Chrome Helper","Google Chrome"]]],
				"procs":[[42,"sshd","root",1.5,2048,"service:ssh","/usr/sbin/sshd -D"]],"total":[31.75,3048,14]}`,
			want: usage.Sample{
				At: at, Source: usage.SourceAgent, Cores: 8,
				Groups: []usage.Group{
					{Key: "app:Google Chrome", Kind: usage.KindApp, Name: "Google Chrome", CPU: 30.25, Mem: 1000, Procs: 12, Top: []string{"Google Chrome Helper", "Google Chrome"}},
					{Key: "service:ssh", Kind: usage.KindService, Name: "ssh", CPU: 1.5, Mem: 2048, Procs: 2, Top: []string{"sshd"}},
				},
				Procs: []usage.Proc{{PID: 42, Name: "sshd", User: "root", CPU: 1.5, Mem: 2048, Group: "service:ssh", Cmd: "/usr/sbin/sshd -D"}},
				Total: &usage.Totals{CPU: 31.75, Mem: 3048, Procs: 14},
			},
			wantOK: true,
		},
		{
			name: "values out of range",
			body: `{"groups":[["service:a",250,-5,-1,[]],["service:a",1,1,1,[]],["",1,1,1,[]],[7,1,1,1,[]],["toaster:b\u0007c",-3,1e300,"x","top"]],
				"procs":[[-1,"p\nq","r",1e9,"lots","nonsense:z","cmd\u0000line"],[1,"",null,1,1,"service:a",""]]}`,
			want: usage.Sample{
				At: at, Source: usage.SourceAgent, Cores: 8,
				Groups: []usage.Group{
					{Key: "service:a", Kind: usage.KindService, Name: "a", CPU: 100},
					// An unknown kind is part of the name.
					{Key: "other:toaster:b c", Kind: usage.KindOther, Name: "toaster:b c", Mem: 1 << 50},
				},
				Procs: []usage.Proc{{Name: "p q", User: "r", CPU: 100, Group: "other:nonsense:z", Cmd: "cmd line"}},
			},
			wantOK: true,
		},
		{name: "empty", body: `{"groups":[],"procs":[]}`},
		{name: "not an object", body: `[1,2]`},
		{name: "groups that aren't arrays", body: `{"groups":{"a":1}}`},
		{name: "nothing", body: ``},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := usage.ParseAgent(json.RawMessage(tt.body), at, 8)
			if ok != tt.wantOK {
				t.Fatalf("ParseAgent() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseAgent() =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestSplitKey(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		key  string
		kind usage.Kind
		name string
	}{
		{"container:web", usage.KindContainer, "web"},
		{"user-service:a:b", usage.KindUserService, "a:b"},
		{"toaster:b", usage.KindOther, "toaster:b"},
		{"plain", usage.KindOther, "plain"},
	} {
		if kind, name := usage.SplitKey(tt.key); kind != tt.kind || name != tt.name {
			t.Errorf("SplitKey(%q) = %s %q, want %s %q", tt.key, kind, name, tt.kind, tt.name)
		}
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "roost.db"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

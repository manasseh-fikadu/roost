package usage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// HubInterval is how often Run samples the hub's processes, as the old
// dashboard did.
const HubInterval = 5 * time.Second

// How long a command line can be, in bytes, in a live sample.
const maxCmd = 1000

// pfKthread is the flag in /proc/[pid]/stat that marks a kernel thread.
const pfKthread = 0x00200000

// usersMaxAge is how long a Sampler trusts its copy of /etc/passwd when it
// meets a user that isn't in it.
const usersMaxAge = 5 * time.Minute

// mainThread matches the name that Node and others give their main thread,
// which hides the program's own name.
var mainThread = regexp.MustCompile(`^(?:node-)?MainThread$`)

// A Sampler samples the processes of a Linux machine from /proc, the way the
// agent does. It's not safe for concurrent use.
type Sampler struct {
	fsys     fs.FS
	pageSize uint64

	// prev holds each process's CPU time at the previous sample, by PID.
	prev map[int]procTicks
	// prevTotal is the CPU time of all cores at the previous sample.
	prevTotal uint64
	sampled   bool

	users   map[int]string
	usersAt time.Time
}

// procInfo is what a sample reads of a process besides its stat file.
type procInfo struct {
	kthread bool
	cgroup  string
	cmd     string
	// uid is -1 if it's not known.
	uid int
}

type procTicks struct {
	// start is when the process started, in clock ticks since boot. A PID
	// with another start time is another process.
	start string
	ticks uint64
}

// NewSampler returns a Sampler of the machine whose root directory is fsys,
// such as os.DirFS("/").
func NewSampler(fsys fs.FS) *Sampler {
	return &Sampler{fsys: fsys, pageSize: uint64(os.Getpagesize())} //nolint:gosec // G115: the page size is positive.
}

// Run samples the processes every HubInterval and passes each sample to
// record, until ctx ends.
func (s *Sampler) Run(ctx context.Context, log *slog.Logger, record func(Sample)) {
	t := time.NewTicker(HubInterval)
	defer t.Stop()
	var failed bool
	for {
		sample, ok, err := s.Sample(time.Now())
		switch {
		case err != nil:
			if !failed {
				log.Warn("sample this machine's processes", "error", err)
			}
			failed = true
		case ok:
			if failed {
				log.Info("sampled this machine's processes again")
			}
			failed = false
			record(sample)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Sample reads every process once, and reports what each used since the
// previous call. The first call only takes the readings that the next one
// compares with, so it returns false.
func (s *Sampler) Sample(now time.Time) (Sample, bool, error) {
	total, cores, err := s.cpuTotal()
	if err != nil {
		return Sample{}, false, err
	}
	entries, err := fs.ReadDir(s.fsys, "proc")
	if err != nil {
		return Sample{}, false, err
	}
	first := !s.sampled
	elapsed := total - min(s.prevTotal, total)
	ticks := make(map[int]procTicks, len(s.prev))
	var procs []rawProc
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		st, ok := s.stat(pid)
		if !ok {
			// It exited while the sampler read /proc.
			continue
		}
		ticks[pid] = procTicks{start: st.start, ticks: st.ticks}
		if first {
			continue
		}
		// A process that wasn't there before started since, so all of its
		// time falls in this sample.
		used := st.ticks
		if before, ok := s.prev[pid]; ok && before.start == st.start {
			used -= min(before.ticks, st.ticks)
		}
		info := s.info(pid, st, now)
		name := st.comm
		// comm is cut at 15 bytes, and some programs rename their main
		// thread. The program's file name is the better name then.
		if !info.kthread && (len(st.comm) == 15 || mainThread.MatchString(st.comm)) {
			arg0, _, _ := strings.Cut(info.cmd, " ")
			if arg0 = path.Base(arg0); arg0 != "." && arg0 != "/" && (strings.HasPrefix(arg0, st.comm) || mainThread.MatchString(st.comm)) {
				name = arg0
			}
		}
		kind, group := classify(info.cgroup, info.kthread, name, s.userName)
		p := rawProc{pid: pid, name: name, user: s.userName(info.uid), cmd: info.cmd, mem: st.rss * s.pageSize, group: Key(kind, group)}
		if elapsed > 0 {
			p.cpu = min(100, float64(used)/float64(elapsed)*100)
		}
		procs = append(procs, p)
	}
	s.prev, s.prevTotal, s.sampled = ticks, total, true
	if first {
		return Sample{}, false, nil
	}
	return newSample(procs, now, SourceHub, cores), true, nil
}

// cpuTotal reads the CPU time of all cores together from /proc/stat, in
// clock ticks, and counts the cores.
func (s *Sampler) cpuTotal() (total uint64, cores int, err error) {
	text, err := fs.ReadFile(s.fsys, "proc/stat")
	if err != nil {
		return 0, 0, err
	}
	found := false
	for line := range strings.Lines(string(text)) {
		switch {
		case strings.HasPrefix(line, "cpu "):
			// user nice system idle iowait irq softirq steal. Guest time is
			// counted in user and nice already.
			for i, f := range strings.Fields(line)[1:] {
				if i == 8 {
					break
				}
				v, err := strconv.ParseUint(f, 10, 64)
				if err != nil {
					return 0, 0, fmt.Errorf("read /proc/stat: %w", err)
				}
				total += v
			}
			found = true
		case strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9':
			cores++
		}
	}
	if !found {
		return 0, 0, errors.New("no cpu line in /proc/stat")
	}
	return total, cores, nil
}

// procStat is what /proc/[pid]/stat says.
type procStat struct {
	comm  string
	flags uint64
	// ticks is the CPU time the process has used, in clock ticks.
	ticks uint64
	start string
	// rss is in pages.
	rss uint64
}

func (s *Sampler) stat(pid int) (procStat, bool) {
	b, err := fs.ReadFile(s.fsys, "proc/"+strconv.Itoa(pid)+"/stat")
	if err != nil {
		return procStat{}, false
	}
	text := string(b)
	// The name can hold spaces and parentheses, so it ends at the last ")".
	open, end := strings.IndexByte(text, '('), strings.LastIndexByte(text, ')')
	if open < 0 || end < open || end+2 > len(text) {
		return procStat{}, false
	}
	// f[0] is field 3 of proc(5), the state.
	f := strings.Fields(text[end+2:])
	if len(f) < 22 {
		return procStat{}, false
	}
	num := func(i int) uint64 {
		v, _ := strconv.ParseUint(f[i], 10, 64)
		return v
	}
	return procStat{
		comm:  text[open+1 : end],
		flags: num(6),
		ticks: num(11) + num(12), // utime and stime
		start: f[19],
		rss:   num(21),
	}, true
}

// info reads what a sample needs of process pid besides its stat file. It
// reads it every time, as the agent does, since exec, setproctitle, setuid
// and moving to another cgroup all change it while the process runs.
func (s *Sampler) info(pid int, st procStat, now time.Time) procInfo {
	dir := "proc/" + strconv.Itoa(pid) + "/"
	info := procInfo{kthread: st.flags&pfKthread != 0, uid: -1}
	if !info.kthread {
		cmdline, _ := fs.ReadFile(s.fsys, dir+"cmdline")
		info.cmd = cut(strings.TrimRight(strings.ReplaceAll(string(cmdline), "\x00", " "), " "), maxCmd)
		cgroup, _ := fs.ReadFile(s.fsys, dir+"cgroup")
		info.cgroup = cgroupPath(string(cgroup))
		status, _ := fs.ReadFile(s.fsys, dir+"status")
		info.uid = statusUID(string(status))
		if info.uid >= 0 {
			s.loadUsers(info.uid, now)
		}
	}
	if info.cmd == "" {
		info.cmd = "[" + st.comm + "]"
	}
	return info
}

// statusUID returns the real user ID in the text of /proc/[pid]/status, or
// -1.
func statusUID(text string) int {
	for line := range strings.Lines(text) {
		if rest, ok := strings.CutPrefix(line, "Uid:"); ok {
			if f := strings.Fields(rest); len(f) > 0 {
				if uid, err := strconv.Atoi(f[0]); err == nil {
					return uid
				}
			}
		}
	}
	return -1
}

// loadUsers reads /etc/passwd if it hasn't, or if it doesn't list uid and
// the copy is old.
func (s *Sampler) loadUsers(uid int, now time.Time) {
	if _, ok := s.users[uid]; ok || (s.users != nil && now.Sub(s.usersAt) < usersMaxAge) {
		return
	}
	s.users, s.usersAt = map[int]string{}, now
	text, _ := fs.ReadFile(s.fsys, "etc/passwd")
	for line := range strings.Lines(string(text)) {
		f := strings.Split(line, ":")
		if len(f) < 3 {
			continue
		}
		if id, err := strconv.Atoi(f[2]); err == nil {
			s.users[id] = f[0]
		}
	}
}

// userName returns the name of the user with uid, or the number if there's
// no such user, or "" if uid is -1.
func (s *Sampler) userName(uid int) string {
	if uid < 0 {
		return ""
	}
	if name, ok := s.users[uid]; ok {
		return name
	}
	return strconv.Itoa(uid)
}

// cut returns s cut to at most n bytes, at the start of a character.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

package demo

import (
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// An engine is a made-up Docker Engine: it serves the API of one machine's
// containers on a connection, as Docker does on its socket. Bield's Docker
// client, collector and sampler ask it what they'd ask a real one.
type engine struct {
	d *Demo
	m *machine

	mu    sync.Mutex
	state map[string]*running
}

// running is what a container has counted since the engine started.
type running struct {
	at              time.Time
	cpu             uint64 // CPU time it used, in nanoseconds
	sys             uint64 // CPU time the machine had
	rx, tx          uint64
	blkRead, blkWrt uint64
}

func newEngine(d *Demo, m *machine) *engine {
	return &engine{d: d, m: m, state: map[string]*running{}}
}

// containerID returns the 64-hex-digit ID of container c.
func (e *engine) containerID(c container) string {
	var b strings.Builder
	for i := range 8 {
		x := mix(e.d.seed ^ uint64(e.m.index)<<20 ^ uint64(len(c.name))<<8 ^ uint64(i) ^ hashString(c.name)) //nolint:gosec // G115: small positive numbers.
		fmt.Fprintf(&b, "%016x", x)
	}
	return b.String()[:64]
}

func hashString(s string) uint64 {
	h := uint64(14695981039346656037)
	for i := range len(s) {
		h = (h ^ uint64(s[i])) * 1099511628211
	}
	return h
}

func (e *engine) imageID(c container) string {
	return "sha256:" + e.containerID(container{name: c.image})[:64]
}

// find returns the container with a name or an ID, or a prefix of one.
func (e *engine) find(id string) (container, bool) {
	for _, c := range e.m.containers {
		if c.name == id || (len(id) >= 4 && strings.HasPrefix(e.containerID(c), id)) {
			return c, true
		}
	}
	return container{}, false
}

// status says how long a container has been up, or down, as Docker does.
func (e *engine) status(c container) string {
	age := humanAge(-c.started)
	switch c.state {
	case "exited":
		return fmt.Sprintf("Exited (%d) %s ago", c.exitCode, humanAge(time.Hour))
	case "restarting":
		return "Restarting (1) 8 seconds ago"
	}
	switch c.health {
	case "healthy":
		return "Up " + age + " (healthy)"
	case "unhealthy":
		return "Up " + age + " (unhealthy)"
	}
	return "Up " + age
}

func humanAge(d time.Duration) string {
	switch {
	case d < 2*time.Minute:
		return "About a minute"
	case d < 2*time.Hour:
		return fmt.Sprintf("%d minutes", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(d/time.Hour))
	}
	return fmt.Sprintf("%d days", int(d/(24*time.Hour)))
}

func (e *engine) labels(c container) map[string]string {
	if c.project == "" {
		return map[string]string{}
	}
	return map[string]string{
		"com.docker.compose.project": c.project, "com.docker.compose.service": strings.TrimPrefix(c.name, c.project+"-"),
	}
}

func (e *engine) network(c container) string {
	if c.project == "" {
		return "bridge"
	}
	return c.project + "_default"
}

func (e *engine) created(c container) time.Time {
	return e.d.start.Add(c.started - 24*time.Hour)
}

// ServeHTTP answers a request to the Engine API.
func (e *engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	// A version may come first, such as /v1.43/containers/json.
	if strings.HasPrefix(path, "v1.") {
		_, path, _ = strings.Cut(path, "/")
	}
	switch {
	case path == "_ping":
		_, _ = w.Write([]byte("OK"))
	case path == "version":
		reply(w, map[string]any{"ApiVersion": "1.51", "Version": "28.3.3"})
	case path == "info":
		e.info(w)
	case path == "containers/json":
		e.list(w)
	case strings.HasPrefix(path, "containers/"):
		e.container(w, r, strings.TrimPrefix(path, "containers/"))
	case path == "system/df":
		e.df(w)
	case path == "volumes":
		e.volumes(w)
	case path == "images/json":
		e.images(w)
	case path == "networks":
		e.networks(w)
	case strings.HasPrefix(path, "images/") && strings.HasSuffix(path, "/json"):
		e.image(w, strings.TrimSuffix(strings.TrimPrefix(path, "images/"), "/json"))
	default:
		http.NotFound(w, r)
	}
}

func reply(w http.ResponseWriter, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(append(b, '\n'))
}

func (e *engine) list(w http.ResponseWriter) {
	out := make([]map[string]any, 0, len(e.m.containers))
	for _, c := range e.m.containers {
		var ports []map[string]any
		for _, p := range c.ports {
			ports = append(ports, map[string]any{"IP": p.ip, "PrivatePort": p.private, "PublicPort": p.public, "Type": "tcp"})
		}
		var mounts []map[string]any
		for _, v := range c.volumes {
			mounts = append(mounts, map[string]any{"Type": "volume", "Name": v})
		}
		out = append(out, map[string]any{
			"Id": e.containerID(c), "Names": []string{"/" + c.name}, "Image": c.image, "ImageID": e.imageID(c),
			"Command": "/entrypoint.sh", "Created": e.created(c).Unix(), "State": c.state, "Status": e.status(c),
			"Labels": e.labels(c), "Ports": ports, "Mounts": mounts,
			"NetworkSettings": map[string]any{"Networks": map[string]any{
				e.network(c): map[string]any{"NetworkID": e.networkID(c), "IPAddress": e.ipOf(c), "MacAddress": "02:42:ac:12:00:02"},
			}},
		})
	}
	reply(w, out)
}

func (e *engine) networkID(c container) string {
	return e.containerID(container{name: "net:" + e.network(c)})
}

// ipOf returns the address of c on its network.
func (e *engine) ipOf(c container) string {
	i := slices.IndexFunc(e.m.containers, func(o container) bool { return o.name == c.name })
	return fmt.Sprintf("172.20.%d.%d", 1+int(hashString(e.network(c))%20), 2+i)
}

func (e *engine) container(w http.ResponseWriter, r *http.Request, rest string) {
	id, sub, _ := strings.Cut(rest, "/")
	c, ok := e.find(id)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		if err := json.NewEncoder(w).Encode(map[string]string{"message": "No such container: " + id}); err != nil {
			return
		}
		return
	}
	switch {
	case sub == "json":
		e.inspect(w, c)
	case sub == "top":
		e.top(w, c)
	case sub == "logs":
		e.logs(w, r, c)
	case sub == "stats" && r.URL.Query().Get("stream") == "0":
		reply(w, e.stats(c, time.Now()))
	case sub == "stats":
		e.streamStats(w, r, c)
	case r.Method == http.MethodPost && (sub == "start" || sub == "stop" || sub == "restart"):
		// A demo changes nothing, and Bield doesn't ask.
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (e *engine) inspect(w http.ResponseWriter, c container) {
	running := c.state == "running"
	started := e.d.start.Add(c.started)
	state := map[string]any{
		"Status": c.state, "Running": running, "Pid": 0, "ExitCode": c.exitCode, "OOMKilled": c.oom,
		"StartedAt": started.UTC().Format(time.RFC3339Nano), "FinishedAt": "0001-01-01T00:00:00Z",
	}
	if running {
		state["Pid"] = 2000 + int(hashString(c.name)%30000)
	} else {
		state["FinishedAt"] = e.d.start.Add(-27 * time.Minute).UTC().Format(time.RFC3339Nano)
	}
	config := map[string]any{
		"Hostname": e.containerID(c)[:12], "User": "", "Tty": false, "Image": c.image, "WorkingDir": "/",
		"Env": append([]string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "TZ=UTC"}, c.env...),
		"Cmd": []string{"/entrypoint.sh"}, "Entrypoint": []string{"/sbin/tini", "--"},
		"Labels": e.labels(c),
	}
	if c.health != "" {
		failing := 0
		if c.health == "unhealthy" {
			failing = 4
		}
		state["Health"] = map[string]any{"Status": c.health, "FailingStreak": failing, "Log": []map[string]any{{
			"Start": started.UTC().Format(time.RFC3339Nano), "End": started.UTC().Format(time.RFC3339Nano), "ExitCode": min(failing, 1),
			"Output": map[bool]string{true: "curl: (7) Failed to connect to localhost port 8080: Connection refused", false: "ok"}[failing > 0],
		}}}
		config["Healthcheck"] = map[string]any{
			"Test": []string{"CMD-SHELL", "curl -fsS http://localhost/ || exit 1"}, "Interval": 30_000_000_000,
			"Timeout": 5_000_000_000, "Retries": 3,
		}
	}
	exposed := map[string]any{}
	bindings := map[string]any{}
	for _, p := range c.ports {
		key := fmt.Sprintf("%d/tcp", p.private)
		exposed[key] = map[string]any{}
		bindings[key] = []map[string]any{{"HostIp": p.ip, "HostPort": strconv.Itoa(int(p.public))}}
	}
	config["ExposedPorts"] = exposed
	var mounts []map[string]any
	for _, v := range c.volumes {
		mounts = append(mounts, map[string]any{
			"Type": "volume", "Name": v, "Source": "/var/lib/docker/volumes/" + v + "/_data", "Destination": "/data",
			"Driver": "local", "Mode": "z", "RW": true,
		})
	}
	reply(w, map[string]any{
		"Id": e.containerID(c), "Name": "/" + c.name, "Created": e.created(c).UTC().Format(time.RFC3339Nano),
		"Path": "/entrypoint.sh", "Args": []string{}, "Image": e.imageID(c), "Platform": "linux", "Driver": "overlayfs",
		"RestartCount": c.restarts, "State": state, "Config": config,
		"HostConfig": map[string]any{
			"NetworkMode": e.network(c), "RestartPolicy": map[string]any{"Name": c.restart}, "Memory": int64(c.memMB*1.5) << 20,
			"LogConfig": map[string]any{"Type": "json-file"},
		},
		"NetworkSettings": map[string]any{
			"Ports": bindings,
			"Networks": map[string]any{e.network(c): map[string]any{
				"NetworkID": e.networkID(c), "IPAddress": e.ipOf(c), "IPPrefixLen": 16, "Gateway": "172.20.0.1",
				"MacAddress": "02:42:ac:14:00:02", "Aliases": []string{c.name},
			}},
		},
		"Mounts": mounts,
	})
}

func (e *engine) top(w http.ResponseWriter, c container) {
	pid := 2000 + int(hashString(c.name)%30000)
	reply(w, map[string]any{
		"Titles": []string{"UID", "PID", "PPID", "C", "STIME", "TTY", "TIME", "CMD"},
		"Processes": [][]string{
			{"root", strconv.Itoa(pid), "1", "0", "08:00", "?", "00:00:01", "/sbin/tini -- /entrypoint.sh"},
			{"root", strconv.Itoa(pid + 17), strconv.Itoa(pid), "1", "08:00", "?", "00:01:12", "/usr/bin/" + strings.Fields(c.image + " x")[0]},
		},
	})
}

// frame returns a line of output as a frame of Docker's multiplexed stream.
func frame(stream byte, text string) []byte {
	b := []byte{stream, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(b[4:], uint32(len(text))) //nolint:gosec // G115: a line is far under 4 GiB.
	return append(b, text...)
}

// logLine returns line n of a container's log, which was written at t.
func (e *engine) logLine(c container, n int64, t time.Time) (stream byte, text string) {
	lines := []string{
		"GET /health 200 3ms", "GET / 200 11ms", "processed 12 items in 41ms", "connection from 100.101.0.12 accepted",
		"cache hit ratio 0.94", "GET /api/items?limit=50 200 18ms", "checkpoint complete: wrote 31 buffers", "worker 3 is idle",
	}
	i := mix(hashString(c.name)^uint64(n)) % uint64(len(lines)) //nolint:gosec // G115: n only seeds the pick.
	pick := lines[i]
	stream = 1
	if c.health == "unhealthy" && n%5 == 0 {
		stream, pick = 2, "WARN health check failed: connection refused on :8080"
	}
	if c.state == "exited" && c.oom {
		stream, pick = 2, "fatal: out of memory: Killed process 4121 (duplicati)"
	}
	return stream, t.UTC().Format(time.RFC3339Nano) + " " + pick + "\n"
}

func (e *engine) logs(w http.ResponseWriter, r *http.Request, c container) {
	w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
	tail := 200
	if v := r.URL.Query().Get("tail"); v != "" && v != "all" {
		_, _ = fmt.Sscan(v, &tail)
	}
	now := time.Now()
	stdout, stderr := r.URL.Query().Get("stdout") != "0", r.URL.Query().Get("stderr") != "0"
	write := func(n int64, t time.Time) {
		stream, text := e.logLine(c, n, t)
		if (stream == 1 && stdout) || (stream == 2 && stderr) {
			_, _ = w.Write(frame(stream, text))
		}
	}
	// A line every ten seconds back from now.
	for i := tail; i >= 1; i-- {
		at := now.Add(-time.Duration(i) * 10 * time.Second)
		write(at.Unix()/10, at)
	}
	if r.URL.Query().Get("follow") != "1" || c.state != "running" {
		return
	}
	rc := http.NewResponseController(w)
	_ = rc.Flush()
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case at := <-t.C:
			write(at.Unix()/3, at)
			if rc.Flush() != nil {
				return
			}
		}
	}
}

// advance counts what container c used up to now, and returns the totals.
func (e *engine) advance(c container, now time.Time) running {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.state[c.name]
	if s == nil {
		// It has counted since it started.
		up := max(0, now.Sub(e.d.start.Add(c.started)).Seconds())
		avg := c.cpu / 100 * 1e9
		s = &running{at: now, cpu: uint64(avg * up), sys: uint64(float64(e.m.cores) * 1e9 * up)}
		s.rx, s.tx = uint64(c.cpu*1500*up), uint64(c.cpu*900*up)
		e.state[c.name] = s
	}
	if dt := now.Sub(s.at).Seconds(); dt > 0 {
		r := e.rate(c, now)
		s.cpu += uint64(r * 1e7 * dt)
		s.sys += uint64(float64(e.m.cores) * 1e9 * dt)
		s.rx += uint64(r * 1500 * dt)
		s.tx += uint64(r * 900 * dt)
		s.blkRead += uint64(r * 400 * dt)
		s.blkWrt += uint64(r * 250 * dt)
		s.at = now
	}
	return *s
}

// rate returns the share of one CPU that c uses at t, in percent.
func (e *engine) rate(c container, t time.Time) float64 {
	seed := hashString(c.name) ^ uint64(e.m.index) //nolint:gosec // G115: a machine number is small.
	return c.cpu * (0.65 + 0.7*daily(t, e.m.peak)) * (1 + 0.3*wave(seed, t, 4*time.Minute))
}

func (e *engine) stats(c container, now time.Time) map[string]any {
	if c.state != "running" {
		return map[string]any{"read": "0001-01-01T00:00:00Z"}
	}
	s := e.advance(c, now)
	mem := uint64(c.memMB * 1e6 * (1 + 0.04*wave(hashString(c.name)^7, now, 20*time.Minute)))
	return map[string]any{
		"read":      now.UTC().Format(time.RFC3339Nano),
		"cpu_stats": map[string]any{"cpu_usage": map[string]any{"total_usage": s.cpu}, "system_cpu_usage": s.sys, "online_cpus": e.m.cores},
		"memory_stats": map[string]any{
			"usage": mem + mem/8, "limit": uint64(e.m.memGB * (1 << 30)), "stats": map[string]any{"inactive_file": mem / 8},
		},
		"networks": map[string]any{"eth0": map[string]any{"rx_bytes": s.rx, "tx_bytes": s.tx}},
		"blkio_stats": map[string]any{"io_service_bytes_recursive": []map[string]any{
			{"op": "read", "value": s.blkRead}, {"op": "write", "value": s.blkWrt},
		}},
		"pids_stats": map[string]any{"current": 3 + int(hashString(c.name)%14)},
	}
}

// streamStats sends a sample each second until the client goes away.
func (e *engine) streamStats(w http.ResponseWriter, r *http.Request, c container) {
	rc := http.NewResponseController(w)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		line, err := json.Marshal(e.stats(c, time.Now()))
		if err != nil {
			return
		}
		if _, err := w.Write(append(line, '\n')); err != nil || rc.Flush() != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
		}
	}
}

func (e *engine) info(w http.ResponseWriter) {
	running := 0
	for _, c := range e.m.containers {
		if c.state == "running" {
			running++
		}
	}
	reply(w, map[string]any{
		"Name": e.m.host, "ServerVersion": "28.3.3", "OperatingSystem": e.m.osVersion, "KernelVersion": e.m.kernel,
		"Architecture": e.m.arch, "NCPU": e.m.cores, "MemTotal": int64(e.m.memGB * (1 << 30)),
		"Driver": "overlayfs", "LoggingDriver": "json-file", "CgroupVersion": "2", "CgroupDriver": "systemd",
		"DockerRootDir": "/var/lib/docker", "Containers": len(e.m.containers), "ContainersRunning": running,
		"ContainersStopped": len(e.m.containers) - running, "Images": len(e.imageNames()) + 1,
	})
}

// imageNames returns the distinct images of the machine's containers.
func (e *engine) imageNames() []string {
	var out []string
	for _, c := range e.m.containers {
		if !slices.Contains(out, c.image) {
			out = append(out, c.image)
		}
	}
	slices.Sort(out)
	return out
}

func imageSize(name string) int64 { return 40e6 + int64(hashString(name)%400e6) }

func (e *engine) df(w http.ResponseWriter) {
	var images, containers, volumes []map[string]any
	var layers int64
	for _, name := range e.imageNames() {
		used := 0
		for _, c := range e.m.containers {
			if c.image == name {
				used++
			}
		}
		layers += imageSize(name)
		images = append(images, map[string]any{
			"Id": e.imageID(container{image: name}), "Size": imageSize(name), "SharedSize": 0, "Containers": used,
		})
	}
	for _, c := range e.m.containers {
		containers = append(containers, map[string]any{"Id": e.containerID(c), "SizeRw": int64(c.memMB * 1e4), "State": c.state})
	}
	for _, v := range e.volumeNames() {
		refs := 0
		for _, c := range e.m.containers {
			if slices.Contains(c.volumes, v) {
				refs++
			}
		}
		volumes = append(volumes, map[string]any{"Name": v, "UsageData": map[string]any{"Size": 10e6 + int64(hashString(v)%900e6), "RefCount": refs}})
	}
	reply(w, map[string]any{
		"LayersSize": layers, "Images": images, "Containers": containers, "Volumes": volumes,
		"BuildCache": []map[string]any{{"Size": 120e6}},
	})
}

// volumeNames returns the machine's volumes: the ones its containers use,
// and one that none does any more.
func (e *engine) volumeNames() []string {
	var out []string
	for _, c := range e.m.containers {
		for _, v := range c.volumes {
			if !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
	}
	if len(out) > 0 {
		out = append(out, "old_"+e.m.host+"_data")
	}
	slices.Sort(out)
	return out
}

func (e *engine) volumes(w http.ResponseWriter) {
	out := []map[string]any{}
	for _, v := range e.volumeNames() {
		out = append(out, map[string]any{
			"Name": v, "Driver": "local", "Mountpoint": "/var/lib/docker/volumes/" + v + "/_data",
			"CreatedAt": e.d.start.Add(-90 * 24 * time.Hour).UTC().Format(time.RFC3339), "Scope": "local", "Labels": nil,
		})
	}
	reply(w, map[string]any{"Volumes": out})
}

func (e *engine) images(w http.ResponseWriter) {
	out := []map[string]any{}
	for _, name := range e.imageNames() {
		out = append(out, map[string]any{
			"Id": e.imageID(container{image: name}), "RepoTags": []string{name}, "RepoDigests": []string{name + "@sha256:" + e.containerID(container{name: name})[:64]},
			"Created": e.d.start.Add(-40 * 24 * time.Hour).Unix(), "Size": imageSize(name),
		})
	}
	reply(w, out)
}

func (e *engine) image(w http.ResponseWriter, name string) {
	reply(w, map[string]any{
		"Id": e.imageID(container{image: name}), "RepoDigests": []string{name + "@sha256:" + e.containerID(container{name: name})[:64]},
		"Created": e.d.start.Add(-40 * 24 * time.Hour).UTC().Format(time.RFC3339), "Size": imageSize(name),
	})
}

func (e *engine) networks(w http.ResponseWriter) {
	out := []map[string]any{{
		"Id": e.containerID(container{name: "net:bridge"}), "Name": "bridge", "Driver": "bridge", "Scope": "local",
		"Created": e.d.start.Add(-200 * 24 * time.Hour).UTC().Format(time.RFC3339),
		"IPAM":    map[string]any{"Config": []map[string]any{{"Subnet": "172.17.0.0/16", "Gateway": "172.17.0.1"}}},
	}}
	var seen []string
	for _, c := range e.m.containers {
		name := e.network(c)
		if name == "bridge" || slices.Contains(seen, name) {
			continue
		}
		seen = append(seen, name)
		subnet := fmt.Sprintf("172.20.%d.0/24", 1+int(hashString(name)%20))
		out = append(out, map[string]any{
			"Id": e.networkID(c), "Name": name, "Driver": "bridge", "Scope": "local",
			"Created": e.d.start.Add(-120 * 24 * time.Hour).UTC().Format(time.RFC3339),
			"Labels":  map[string]string{"com.docker.compose.network": "default", "com.docker.compose.project": c.project},
			"IPAM":    map[string]any{"Config": []map[string]any{{"Subnet": subnet, "Gateway": strings.TrimSuffix(subnet, "0/24") + "1"}}},
		})
	}
	slices.SortFunc(out, func(a, b map[string]any) int { return cmp.Compare(a["Name"].(string), b["Name"].(string)) }) //nolint:errcheck // the names above are strings.
	reply(w, out)
}

// serveConn serves the Engine API on one connection, as Docker does on a
// socket, until the connection closes.
func (e *engine) serveConn(c net.Conn) {
	done := make(chan struct{})
	srv := &http.Server{Handler: e, ReadHeaderTimeout: 10 * time.Second}
	_ = srv.Serve(&oneConn{conn: &notifyConn{Conn: c, done: done}, done: done})
}

// oneConn is a listener of a single connection.
type oneConn struct {
	conn *notifyConn
	done chan struct{}
	once sync.Once
}

func (l *oneConn) Accept() (net.Conn, error) {
	var c net.Conn
	l.once.Do(func() { c = l.conn })
	if c != nil {
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *oneConn) Close() error   { return nil }
func (l *oneConn) Addr() net.Addr { return &net.UnixAddr{Net: "unix"} }

// notifyConn closes done when it closes.
type notifyConn struct {
	net.Conn
	once sync.Once
	done chan struct{}
}

func (c *notifyConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return c.Conn.Close()
}

// dialer returns a function that connects to the engine, whatever it's asked
// for, as Docker's socket dialer does.
func (e *engine) dialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		client, server := net.Pipe()
		go e.serveConn(server)
		return client, nil
	}
}

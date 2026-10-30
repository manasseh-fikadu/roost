package api_test

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	webID    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	backupID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	// webImage is web's image.
	webImage = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
)

// engine is a fake Docker Engine with two containers: web, which runs in a
// Compose project with a volume, and backup, which crashed. It serves any
// number of connections, and counts what it is asked.
type engine struct {
	mu   sync.Mutex
	hits map[string]int
	// queries holds the query of the latest request to each path.
	queries map[string]string
	// webRunning is false once web is stopped.
	webRunning bool
	// cpu and sys are the counters in the next one-shot sample: web uses half
	// a second of the eight that the machine's four CPUs give, each time.
	cpu, sys uint64
}

func newEngine() *engine {
	return &engine{hits: map[string]int{}, queries: map[string]string{}, webRunning: true}
}

func (e *engine) hit(key string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hits[key]
}

func (e *engine) query(path string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.queries[path]
}

func (e *engine) total() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for k, v := range e.hits {
		if !strings.HasSuffix(k, "/_ping") {
			n += v
		}
	}
	return n
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

func notFound(w http.ResponseWriter, what string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	if err := json.NewEncoder(w).Encode(struct {
		Message string `json:"message"`
	}{"No such container: " + what}); err != nil {
		return
	}
}

// frame returns text as a frame of Docker's multiplexed output.
func frame(stream byte, text string) []byte {
	b := []byte{stream, 0, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(b[4:], uint32(len(text))) //nolint:gosec // G115: a test's frame is small.
	return append(b, text...)
}

func (e *engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	e.hits[r.Method+" "+r.URL.Path]++
	e.queries[r.URL.Path] = r.URL.RawQuery
	running := e.webRunning
	e.mu.Unlock()

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "containers" && parts[1] != "json" {
		e.container(w, r, parts[1], strings.Join(parts[2:], "/"), running)
		return
	}
	switch r.URL.Path {
	case "/_ping":
		_, _ = w.Write([]byte("OK"))
	case "/containers/json":
		state, status := "running", "Up 2 hours (healthy)"
		if !running {
			state, status = "exited", "Exited (0) 1 second ago"
		}
		reply(w, []map[string]any{
			{
				"Id": webID, "Names": []string{"/web"}, "Image": "nginx:1.27", "ImageID": webImage, "Command": "nginx",
				"Created": 1_800_000_000, "State": state, "Status": status,
				"Labels": map[string]string{"com.docker.compose.project": "shop", "com.docker.compose.service": "web"},
				"Ports": []map[string]any{
					{"IP": "0.0.0.0", "PrivatePort": 80, "PublicPort": 8080, "Type": "tcp"},
					{"IP": "::", "PrivatePort": 80, "PublicPort": 8080, "Type": "tcp"},
					{"IP": "127.0.0.1", "PrivatePort": 9000, "PublicPort": 9000, "Type": "tcp"},
					{"PrivatePort": 443, "Type": "tcp"},
				},
				"NetworkSettings": map[string]any{"Networks": map[string]any{"shop_default": map[string]any{"NetworkID": "net1", "IPAddress": "172.20.0.2", "MacAddress": "02:42:ac:14:00:02"}}},
				"Mounts":          []map[string]any{{"Type": "volume", "Name": "shop_data"}},
			},
			{
				"Id": backupID, "Names": []string{"/backup"}, "Image": "restic", "ImageID": "sha256:2222", "Created": 1_800_000_100,
				"State": "exited", "Status": "Exited (1) 5 minutes ago",
				"NetworkSettings": map[string]any{"Networks": map[string]any{}},
			},
		})
	case "/info":
		reply(w, map[string]any{
			"Name": "docker-host", "ServerVersion": "29.7.1", "OperatingSystem": "Debian GNU/Linux 13", "KernelVersion": "6.12.0-test",
			"Architecture": "x86_64", "NCPU": 4, "MemTotal": 8_000_000_000, "Driver": "overlayfs", "LoggingDriver": "json-file",
			"CgroupVersion": "2", "CgroupDriver": "systemd", "DockerRootDir": "/var/lib/docker",
			"Containers": 2, "ContainersRunning": 1, "ContainersStopped": 1, "Images": 2,
			"Warnings": []string{"WARNING: bridge-nf-call-iptables is disabled"},
		})
	case "/version":
		reply(w, map[string]any{"ApiVersion": "1.55"})
	case "/system/df":
		reply(w, map[string]any{
			"LayersSize": 300,
			"Images":     []map[string]any{{"Id": webImage, "Size": 300, "SharedSize": 0, "Containers": 1}},
			"Containers": []map[string]any{{"Id": webID, "SizeRw": 5, "State": "running"}},
			"Volumes":    []map[string]any{{"Name": "shop_data", "UsageData": map[string]any{"Size": 2048, "RefCount": 1}}, {"Name": "old_data", "UsageData": map[string]any{"Size": 512, "RefCount": 0}}},
			"BuildCache": []map[string]any{},
		})
	case "/volumes":
		reply(w, map[string]any{"Volumes": []map[string]any{
			{"Name": "shop_data", "Driver": "local", "Mountpoint": "/var/lib/docker/volumes/shop_data/_data", "CreatedAt": "2027-01-02T03:04:05Z", "Scope": "local", "Labels": map[string]string{"com.docker.compose.project": "shop"}},
			{"Name": "old_data", "Driver": "local", "Mountpoint": "/var/lib/docker/volumes/old_data/_data", "Scope": "local", "Labels": nil},
		}})
	case "/images/json":
		reply(w, []map[string]any{
			{"Id": webImage, "RepoTags": []string{"nginx:1.27"}, "RepoDigests": []string{"nginx@sha256:abc"}, "Created": 1_790_000_000, "Size": 300},
			{"Id": "sha256:3333", "RepoTags": []string{"<none>:<none>"}, "RepoDigests": []string{"<none>@<none>"}, "Created": 1_795_000_000, "Size": 100},
		})
	case "/networks":
		reply(w, []map[string]any{
			{"Id": "net0", "Name": "bridge", "Driver": "bridge", "Scope": "local", "Created": "2027-01-01T00:00:00Z", "IPAM": map[string]any{"Config": []map[string]any{{"Subnet": "172.17.0.0/16", "Gateway": "172.17.0.1"}}}},
			{"Id": "net1", "Name": "shop_default", "Driver": "bridge", "Scope": "local", "Created": "2027-01-02T00:00:00Z", "Labels": map[string]string{"com.docker.compose.network": "default", "com.docker.compose.project": "shop"}, "IPAM": map[string]any{"Config": []map[string]any{{"Subnet": "172.20.0.0/16", "Gateway": "172.20.0.1"}}}},
		})
	default:
		if strings.HasPrefix(r.URL.Path, "/images/") {
			reply(w, map[string]any{"Id": webImage, "RepoDigests": []string{"nginx@sha256:abc"}, "Created": "2027-01-01T00:00:00Z", "Size": 300})
			return
		}
		http.NotFound(w, r)
	}
}

// container answers a request about a container, by name or ID, with the
// rest of the path after its name.
func (e *engine) container(w http.ResponseWriter, r *http.Request, id, rest string, running bool) {
	switch id {
	case "web", webID:
	case "backup", backupID:
		if rest == "json" {
			reply(w, map[string]any{
				"Id": backupID, "Name": "/backup", "Created": "2027-01-01T00:00:00Z", "Image": "sha256:2222",
				"State":      map[string]any{"Status": "exited", "ExitCode": 1, "OOMKilled": true, "StartedAt": "2027-01-01T00:00:01Z", "FinishedAt": "2027-01-01T00:05:00Z"},
				"Config":     map[string]any{"Image": "restic", "Env": []string{"RESTIC_PASSWORD=hunter-two"}},
				"HostConfig": map[string]any{"RestartPolicy": map[string]any{"Name": "no"}},
			})
			return
		}
		if rest == "start" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotModified)
		return
	default:
		notFound(w, id)
		return
	}
	switch {
	case rest == "json":
		state := map[string]any{
			"Status": "running", "Running": running, "Pid": 4242, "StartedAt": "2027-03-01T08:00:00Z", "FinishedAt": "0001-01-01T00:00:00Z",
			"Health": map[string]any{"Status": "healthy", "FailingStreak": 0, "Log": []map[string]any{
				{"Start": "2027-03-01T09:00:00Z", "End": "2027-03-01T09:00:01Z", "ExitCode": 0, "Output": "ok from the check"},
			}},
		}
		if !running {
			state["Status"], state["Running"] = "exited", false
		}
		reply(w, map[string]any{
			"Id": webID, "Name": "/web", "Created": "2027-01-01T00:00:00Z", "Path": "nginx", "Args": []string{"-g", "daemon off;"},
			"Image": webImage, "Platform": "linux", "Driver": "overlayfs", "RestartCount": 2, "State": state,
			"Config": map[string]any{
				"Hostname": "abc", "User": "nginx", "Tty": false, "Image": "nginx:1.27", "WorkingDir": "/",
				"Env":          []string{"DB_PASSWORD=swordfish", "TZ=UTC", "EMPTY"},
				"Cmd":          []string{"nginx", "-g", "daemon off;"},
				"Entrypoint":   []string{"/docker-entrypoint.sh"},
				"ExposedPorts": map[string]any{"80/tcp": map[string]any{}, "443/tcp": map[string]any{}},
				"Labels": map[string]string{
					"com.docker.compose.project": "shop", "com.docker.compose.service": "web",
					"com.docker.compose.project.working_dir": "/srv/shop", "com.example.api-key": "letmein", "maintainer": "ops",
				},
				"Healthcheck": map[string]any{"Test": []string{"CMD-SHELL", "curl -f http://localhost/ || exit 1"}, "Interval": 30_000_000_000, "Timeout": 5_000_000_000, "Retries": 3},
			},
			"HostConfig": map[string]any{
				"NetworkMode": "shop_default", "RestartPolicy": map[string]any{"Name": "unless-stopped"},
				"Memory": 536_870_912, "NanoCpus": 1_500_000_000, "PidsLimit": 100, "CapDrop": []string{"ALL"},
				"LogConfig": map[string]any{"Type": "json-file"},
			},
			"NetworkSettings": map[string]any{
				"Ports": map[string]any{
					"80/tcp":  []map[string]any{{"HostIp": "0.0.0.0", "HostPort": "8080"}, {"HostIp": "::", "HostPort": "8080"}},
					"443/tcp": nil,
				},
				"Networks": map[string]any{"shop_default": map[string]any{
					"NetworkID": "net1", "IPAddress": "172.20.0.2", "IPPrefixLen": 16, "Gateway": "172.20.0.1", "MacAddress": "02:42:ac:14:00:02",
					"Aliases": []string{"web", "web", "abc"},
				}},
			},
			"Mounts": []map[string]any{{"Type": "volume", "Name": "shop_data", "Source": "/var/lib/docker/volumes/shop_data/_data", "Destination": "/data", "Driver": "local", "Mode": "z", "RW": true}},
		})
	case rest == "top":
		reply(w, map[string]any{"Titles": []string{"USER", "PID", "COMMAND"}, "Processes": [][]string{{"root", "1", "nginx -g daemon off;"}}})
	case rest == "logs":
		_, _ = w.Write(append(append(
			frame(1, "2027-03-01T10:00:00.5Z first line\n"),
			frame(2, "2027-03-01T10:00:01Z \x1b[31mred error\x1b[0m\n")...),
			frame(1, "2027-03-01T10:00:02Z last line\n")...))
		if r.URL.Query().Get("follow") == "1" {
			// A log that follows stays open.
			_ = http.NewResponseController(w).Flush()
			<-r.Context().Done()
		}
	case rest == "stats" && r.URL.Query().Get("stream") == "0":
		e.mu.Lock()
		e.cpu, e.sys = e.cpu+500_000_000, e.sys+8_000_000_000
		cpu, sys := e.cpu, e.sys
		e.mu.Unlock()
		reply(w, statsSample(time.Now(), cpu, sys))
	case rest == "stats":
		e.stream(w, r)
	case r.Method == http.MethodPost && rest == "start":
		w.WriteHeader(http.StatusNotModified)
	case r.Method == http.MethodPost && (rest == "stop" || rest == "restart"):
		e.mu.Lock()
		e.webRunning = rest == "restart"
		e.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

// statsSample returns a sample taken at a time, of a container that has used
// cpu nanoseconds of the sys the machine's four CPUs had.
func statsSample(at time.Time, cpu, sys uint64) map[string]any {
	return map[string]any{
		"read":         at.UTC().Format(time.RFC3339Nano),
		"cpu_stats":    map[string]any{"cpu_usage": map[string]any{"total_usage": cpu}, "system_cpu_usage": sys, "online_cpus": 4},
		"memory_stats": map[string]any{"usage": 1000, "limit": 8000, "stats": map[string]any{"inactive_file": 200}},
		"networks":     map[string]any{"eth0": map[string]any{"rx_bytes": 100, "tx_bytes": 50}},
		"pids_stats":   map[string]any{"current": 5},
	}
}

// stream sends a sample each second, of a container using a quarter of the
// machine's four CPUs, until the client goes away or web stops.
func (e *engine) stream(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	var cpu, sys uint64
	for {
		e.mu.Lock()
		running := e.webRunning
		e.mu.Unlock()
		cpu, sys = cpu+500_000_000, sys+8_000_000_000
		sample := statsSample(time.Now(), cpu, sys)
		if !running {
			sample = map[string]any{"read": "0001-01-01T00:00:00Z"}
		}
		line, err := json.Marshal(sample)
		if err != nil {
			return
		}
		if _, err := w.Write(append(line, '\n')); err != nil || rc.Flush() != nil {
			return
		}
		select {
		case <-time.After(time.Second):
		case <-r.Context().Done():
			return
		}
	}
}

// serveConn serves HTTP on one connection, as Docker does on a socket, and
// returns when the connection closes.
func serveConn(c net.Conn, h http.Handler) {
	done := make(chan struct{})
	srv := &http.Server{Handler: h, ReadHeaderTimeout: time.Second}
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

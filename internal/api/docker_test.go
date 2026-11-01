package api_test

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/api"
	"github.com/manasseh-fikadu/bield/internal/docker"
	"github.com/manasseh-fikadu/bield/internal/ssh/sshtest"
	"github.com/manasseh-fikadu/bield/internal/store"
)

// dockerEnv is an sshEnv with a fake Docker on the machine Bield runs on, and
// another on alice's laptop, which Bield reaches over SSH.
type dockerEnv struct {
	*sshEnv
	hub, laptop *engine
}

// newDockerEnv makes a dockerEnv. In a synctest bubble, call it after
// opening st there. If noHub is true, the machine Bield runs on has no Docker.
func newDockerEnv(t *testing.T, st *store.Store, noHub bool) *dockerEnv {
	t.Helper()
	e := &dockerEnv{sshEnv: newSSHEnv(t, st, nil), hub: newEngine(), laptop: newEngine()}
	e.sshd.HandleSocket("/var/run/docker.sock", func(c net.Conn) { serveConn(c, e.laptop) })
	// The check finds the socket.
	e.sshd.Handle(func(x *sshtest.Exec) int {
		if strings.Contains(x.Command, "docker.sock") {
			_, _ = x.Stdout.Write([]byte("docker=/var/run/docker.sock\n"))
		}
		return 0
	})
	e.setUp(t)
	if _, err := e.opts.SSH.Check(t.Context(), "laptop"); err != nil {
		t.Fatal(err)
	}
	var local *docker.Client
	if !noHub {
		srv := httptest.NewTestServer(t, e.hub)
		tr, ok := srv.Client().Transport.(*http.Transport)
		if !ok {
			t.Fatalf("the test server's client has a %T", srv.Client().Transport)
		}
		local = docker.NewClient(tr.DialContext)
	}
	e.opts.Docker = docker.NewFleet(local, e.opts.SSH.DialDocker)
	t.Cleanup(e.opts.Docker.Close)
	e.h = api.New(e.opts)
	return e
}

// docker serves a request as the caller at an address, with no body for a GET.
func (e *dockerEnv) do(t *testing.T, method, path string, from netip.Addr) *httptest.ResponseRecorder {
	t.Helper()
	if method == http.MethodGet {
		return serve(e.h, newRequest(t, method, path, from))
	}
	return send(t, e.h, method, path, from, "")
}

// errorOf returns the message of an error response.
func errorOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body api.ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body, err)
	}
	return body.Error
}

func TestDocker_overview(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	for _, dev := range []string{"bield", "laptop"} {
		t.Run(dev, func(t *testing.T) {
			var admin, viewer api.DockerOverview
			get(t, e.h, "/api/devices/"+dev+"/docker/overview", laptopIP, &admin)
			get(t, e.h, "/api/devices/"+dev+"/docker/overview", phoneIP, &viewer)
			if admin.Version != "29.7.1" || admin.APIVersion != "1.55" || admin.CPUs != 4 || admin.Cgroup != "v2 (systemd)" ||
				admin.Containers != (api.DockerCounts{Total: 2, Running: 1, Stopped: 1}) || admin.Images != 2 {
				t.Errorf("overview = %+v", admin)
			}
			if admin.Kernel == "" || admin.RootDir == "" || len(admin.Warnings) != 1 {
				t.Errorf("admin's overview = %+v, want the kernel, root dir and warnings", admin)
			}
			// Viewers see the overview without what's about the machine's setup.
			if viewer.Version != admin.Version || viewer.Containers != admin.Containers {
				t.Errorf("viewer's overview = %+v, want the same counts as the admin's", viewer)
			}
			if viewer.Kernel != "" || viewer.RootDir != "" || len(viewer.Warnings) != 0 {
				t.Errorf("viewer's overview = %+v, want no kernel, root dir or warnings", viewer)
			}
			// 300 for the layers, 5 for the container, 2560 for the volumes.
			d := admin.Disk
			if d == nil || d.Total != 300+5+2048+512 || d.Images.Reclaimable != 0 || d.Volumes.Reclaimable != 512 || d.Volumes.Active != 1 {
				t.Errorf("disk = %+v, want the daemon's counts", d)
			}
		})
	}
}

func TestDocker_containers(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	var got api.DockerContainerList
	get(t, e.h, "/api/devices/bield/docker/containers", phoneIP, &got)
	if len(got.Containers) != 2 || got.Containers[0].Name != "backup" || got.Containers[1].Name != "web" {
		t.Fatalf("containers = %+v, want backup and web by name", got.Containers)
	}
	backup, web := got.Containers[0], got.Containers[1]
	if backup.State != api.ContainerStateExited || backup.ExitCode == nil || *backup.ExitCode != 1 || !backup.OOMKilled ||
		backup.Stats != nil || backup.RestartPolicy != "no" {
		t.Errorf("backup = %+v, want it exited 1, killed for memory, and not measured", backup)
	}
	if web.ID != webID || web.State != api.ContainerStateRunning || web.Health != "healthy" || web.Project != "shop" || web.Service != "web" ||
		web.RestartCount != 2 || web.RestartPolicy != "unless-stopped" || !web.MemLimitSet || web.Mounts != 1 ||
		len(web.Networks) != 1 || web.Networks[0] != "shop_default" || web.StartedAt == nil || web.ExitCode != nil {
		t.Errorf("web = %+v", web)
	}
	// 8080 once for IPv4 and IPv6, 9000 on loopback only, and 443 unpublished.
	wantPorts := []api.ContainerPort{
		{Private: 80, Public: 8080, Type: "tcp"},
		{Private: 443, Type: "tcp"},
		{IP: "127.0.0.1", Private: 9000, Public: 9000, Type: "tcp"},
	}
	if len(web.Ports) != 3 || web.Ports[0] != wantPorts[0] || web.Ports[1] != wantPorts[1] || web.Ports[2] != wantPorts[2] {
		t.Errorf("web's ports = %+v, want %+v", web.Ports, wantPorts)
	}
	// 0.5 s of CPU on the machine's four CPUs that gave 8 s, 1000 bytes
	// less 200 of cache, and 5 processes. The second sample is a second
	// later than the first, so the traffic didn't change.
	s := web.Stats
	if s == nil || s.CPU == nil || *s.CPU < 24.9 || *s.CPU > 25.1 || s.MemUsed == nil || *s.MemUsed != 800 ||
		s.MemLimit == nil || *s.MemLimit != 8000 || s.Pids == nil || *s.Pids != 5 || s.CPUs != 4 {
		t.Errorf("web's stats = %+v", s)
	}
	if n := e.hub.hit("GET /containers/json"); n != 1 {
		t.Errorf("Docker listed %d times, want 1", n)
	}
}

func TestDocker_detail(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	var admin, viewer api.DockerContainerDetail
	get(t, e.h, "/api/devices/bield/docker/containers/web", laptopIP, &admin)
	phone := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/bield/docker/containers/web", phoneIP))
	if phone.Code != http.StatusOK {
		t.Fatalf("GET as a viewer = %d %s", phone.Code, phone.Body)
	}
	if err := json.Unmarshal(phone.Body.Bytes(), &viewer); err != nil {
		t.Fatal(err)
	}

	if admin.Name != "web" || admin.Image != "nginx:1.27" || admin.ImageID != webImage || admin.ImageDigest != "nginx@sha256:abc" ||
		admin.ImageSize == nil || *admin.ImageSize != 300 || admin.Project != "shop" || admin.ComposeDir != "/srv/shop" ||
		admin.User != "nginx" || admin.RestartCount != 2 || admin.State.Pid != 4242 || admin.State.ExitCode != nil || !admin.State.Running {
		t.Errorf("detail = %+v", admin)
	}
	if r := admin.Resources; r.Memory != 536_870_912 || r.CPUs != 1.5 || r.PidsLimit != 100 {
		t.Errorf("resources = %+v, want 512 MiB, 1.5 CPUs and 100 processes", r)
	}
	if p := admin.RestartPolicy; p == nil || p.Name != "unless-stopped" {
		t.Errorf("restart policy = %+v", p)
	}
	if got := admin.Security.CapDrop; len(got) != 1 || got[0] != "ALL" {
		t.Errorf("capDrop = %v", got)
	}
	if got := admin.Ports; len(got) != 2 || got[0] != (api.ContainerPort{Private: 80, Public: 8080, Type: "tcp"}) || got[1] != (api.ContainerPort{Private: 443, Type: "tcp"}) {
		t.Errorf("ports = %+v, want 8080→80 once and 443 not published", got)
	}
	if len(admin.Exposed) != 2 || admin.Exposed[0] != "443/tcp" || admin.Exposed[1] != "80/tcp" {
		t.Errorf("exposed = %v", admin.Exposed)
	}
	if len(admin.Networks) != 1 || admin.Networks[0].IP != "172.20.0.2" || strings.Join(admin.Networks[0].Aliases, ",") != "web,abc" {
		t.Errorf("networks = %+v, want one with its aliases once each", admin.Networks)
	}
	if len(admin.Mounts) != 1 || admin.Mounts[0].Destination != "/data" || admin.Mounts[0].Size == nil || *admin.Mounts[0].Size != 2048 {
		t.Errorf("mounts = %+v, want the volume at /data with its size", admin.Mounts)
	}
	if hc := admin.Healthcheck; hc == nil || hc.Interval != 30_000 || hc.Timeout != 5_000 || hc.Retries != 3 || len(hc.Test) != 2 {
		t.Errorf("healthcheck = %+v", hc)
	}
	if h := admin.Health; h == nil || h.Status != "healthy" || len(h.Log) != 1 || h.Log[0].Output != "ok from the check" {
		t.Errorf("health = %+v", h)
	}

	// An admin gets what can hold a secret. EMPTY has no =, so no value.
	if len(admin.Env) != 3 || admin.Env[0] != (api.DockerEnvVar{Key: "DB_PASSWORD", Value: "swordfish"}) || admin.Env[2] != (api.DockerEnvVar{Key: "EMPTY"}) {
		t.Errorf("env = %+v", admin.Env)
	}
	if admin.SecretLabels["com.example.api-key"] != "letmein" || admin.Labels["maintainer"] != "ops" || len(admin.Cmd) != 3 || len(admin.Entrypoint) != 1 {
		t.Errorf("labels = %v, secret labels = %v, cmd = %v", admin.Labels, admin.SecretLabels, admin.Cmd)
	}

	// A viewer gets the container without any of it, and not even the names
	// of its environment variables.
	if viewer.Name != "web" || viewer.Image != "nginx:1.27" || viewer.Labels["maintainer"] != "ops" || viewer.Project != "shop" || !viewer.State.Running {
		t.Errorf("viewer's detail = %+v", viewer)
	}
	if len(viewer.Env) != 0 || len(viewer.SecretLabels) != 0 || viewer.Path != "" || len(viewer.Args) != 0 || len(viewer.Cmd) != 0 ||
		len(viewer.Entrypoint) != 0 || viewer.Healthcheck == nil || len(viewer.Healthcheck.Test) != 0 {
		t.Errorf("viewer's detail has what can hold a secret: %+v", viewer)
	}
	if viewer.Health == nil || viewer.Health.Log[0].Output != "" || viewer.Health.Log[0].ExitCode != 0 {
		t.Errorf("viewer's health = %+v, want the results without their output", viewer.Health)
	}
	for _, secret := range []string{"swordfish", "letmein", "DB_PASSWORD", "daemon off", "curl -f", "ok from the check", "docker-entrypoint"} {
		if strings.Contains(phone.Body.String(), secret) {
			t.Errorf("a viewer's detail has %q in it: %s", secret, phone.Body)
		}
	}
}

func TestDocker_whoCanUseWhat(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	const base = "/api/devices/bield/docker/"
	tests := []struct {
		method, path string
		viewer       bool
	}{
		{http.MethodGet, "overview", true},
		{http.MethodGet, "containers", true},
		{http.MethodGet, "containers/web", true},
		{http.MethodGet, "containers/web/history", true},
		{http.MethodGet, "volumes", true},
		{http.MethodGet, "images", true},
		{http.MethodGet, "networks", true},
		{http.MethodGet, "containers/web/inspect", false},
		{http.MethodGet, "containers/web/top", false},
		{http.MethodGet, "containers/web/logs", false},
		{http.MethodGet, "containers/web/logs/download", false},
		{http.MethodPost, "containers/web/start", false},
		{http.MethodPost, "containers/web/stop", false},
		{http.MethodPost, "containers/web/restart", false},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			// carol has no role, so she gets nothing.
			if rec := e.do(t, tt.method, base+tt.path, tabletIP); rec.Code != http.StatusForbidden {
				t.Errorf("as someone with no role = %d, want 403", rec.Code)
			}
			want := http.StatusForbidden
			if tt.viewer {
				want = http.StatusOK
			}
			if rec := e.do(t, tt.method, base+tt.path, phoneIP); rec.Code != want {
				t.Errorf("as a viewer = %d %s, want %d", rec.Code, rec.Body, want)
			}
		})
	}
	// Nothing a viewer or stranger asked for reached the logs, the processes
	// or the containers.
	for _, path := range []string{"/containers/web/logs", "/containers/web/top", "/containers/web/json"} {
		if n := e.hub.hit("GET " + path); path != "/containers/web/json" && n != 0 {
			t.Errorf("Docker was asked for %s %d times, want 0", path, n)
		}
	}
	if n := e.hub.hit("POST /containers/web/stop"); n != 0 {
		t.Errorf("a viewer stopped web %d times", n)
	}
}

func TestDocker_badIDsNeverReachDocker(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	routes := []struct{ method, suffix string }{
		{http.MethodGet, ""}, {http.MethodGet, "/inspect"}, {http.MethodGet, "/top"}, {http.MethodGet, "/logs"},
		{http.MethodGet, "/logs/download"}, {http.MethodGet, "/history"}, {http.MethodGet, "/stats"},
		{http.MethodPost, "/start"}, {http.MethodPost, "/stop"}, {http.MethodPost, "/restart"},
	}
	// The id is decoded before the handler sees it, so %2F and %2E are a
	// slash and a dot.
	for _, id := range []string{"%2E%2E", "..%2Fimages%2Fjson", "a%2Fb", "%2e%2e%2f%2e%2e", "-x", ".hidden", "a%20b", "a%3Fb", "a%23b", "%00", strings.Repeat("a", 129)} {
		for _, rt := range routes {
			rec := e.do(t, rt.method, "/api/devices/bield/docker/containers/"+id+rt.suffix, laptopIP)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s with the id %q = %d %s, want 400", rt.method+" "+rt.suffix, id, rec.Code, rec.Body)
			}
		}
	}
	if n := e.hub.total(); n != 0 {
		t.Errorf("Docker was asked %d times, want nothing to get past the check", n)
	}
}

func TestDocker_unavailable(t *testing.T) {
	t.Parallel()
	t.Run("the machine Bield runs on has no Docker", func(t *testing.T) {
		t.Parallel()
		e := newDockerEnv(t, testStore(t), true)
		rec := e.do(t, http.MethodGet, "/api/devices/bield/docker/overview", laptopIP)
		if rec.Code != http.StatusConflict || !strings.Contains(errorOf(t, rec), "no Docker socket") {
			t.Errorf("GET = %d %s, want 409 about the socket", rec.Code, rec.Body)
		}
		// Another machine's Docker is still there.
		if rec := e.do(t, http.MethodGet, "/api/devices/laptop/docker/overview", laptopIP); rec.Code != http.StatusOK {
			t.Errorf("GET laptop = %d %s, want 200", rec.Code, rec.Body)
		}
	})
	t.Run("no SSH access", func(t *testing.T) {
		t.Parallel()
		e := newDockerEnv(t, testStore(t), false)
		rec := e.do(t, http.MethodGet, "/api/devices/server/docker/overview", laptopIP)
		if rec.Code != http.StatusConflict || !strings.Contains(errorOf(t, rec), "SSH access isn't set up for server") {
			t.Errorf("GET = %d %s, want 409 about SSH", rec.Code, rec.Body)
		}
	})
	t.Run("no Docker socket on the machine", func(t *testing.T) {
		t.Parallel()
		e := newDockerEnv(t, testStore(t), false)
		e.sshd.Handle(func(*sshtest.Exec) int { return 0 })
		if _, err := e.opts.SSH.Check(t.Context(), "laptop"); err != nil {
			t.Fatal(err)
		}
		rec := e.do(t, http.MethodGet, "/api/devices/laptop/docker/overview", laptopIP)
		if rec.Code != http.StatusConflict || !strings.Contains(errorOf(t, rec), "no Docker socket on laptop") {
			t.Errorf("GET = %d %s, want 409 about the socket", rec.Code, rec.Body)
		}
	})
	t.Run("no such device", func(t *testing.T) {
		t.Parallel()
		e := newDockerEnv(t, testStore(t), false)
		if rec := e.do(t, http.MethodGet, "/api/devices/nobody/docker/overview", laptopIP); rec.Code != http.StatusNotFound {
			t.Errorf("GET = %d %s, want 404", rec.Code, rec.Body)
		}
	})
	t.Run("Docker's own errors", func(t *testing.T) {
		t.Parallel()
		e := newDockerEnv(t, testStore(t), false)
		rec := e.do(t, http.MethodGet, "/api/devices/bield/docker/containers/nope", laptopIP)
		if rec.Code != http.StatusNotFound || errorOf(t, rec) != "No such container: nope" {
			t.Errorf("GET = %d %s, want Docker's 404 and its message", rec.Code, rec.Body)
		}
		rec = e.do(t, http.MethodPost, "/api/devices/bield/docker/containers/nope/stop", laptopIP)
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST = %d %s, want 404", rec.Code, rec.Body)
		}
	})
}

func TestDocker_inspectAndTop(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	// The whole inspection is what Docker wrote, with fields Bield doesn't
	// read.
	var raw map[string]any
	get(t, e.h, "/api/devices/laptop/docker/containers/web/inspect", laptopIP, &raw)
	if raw["Id"] != webID || raw["Platform"] != "linux" {
		t.Errorf("inspect = %v, want Docker's document", raw)
	}
	var top api.DockerProcesses
	get(t, e.h, "/api/devices/laptop/docker/containers/web/top", laptopIP, &top)
	if len(top.Titles) != 3 || len(top.Processes) != 1 || top.Processes[0][2] != "nginx -g daemon off;" {
		t.Errorf("top = %+v", top)
	}
	if got := e.laptop.query("/containers/web/top"); got != "ps_args=aux" {
		t.Errorf("Docker was asked for %q, want ps with its CPU and memory columns", got)
	}
}

func TestDocker_resources(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)

	var vols []api.DockerVolume
	get(t, e.h, "/api/devices/bield/docker/volumes", phoneIP, &vols)
	if len(vols) != 2 || vols[0].Name != "old_data" || vols[1].Name != "shop_data" {
		t.Fatalf("volumes = %+v, want old_data and shop_data", vols)
	}
	if v := vols[1]; v.Project != "shop" || v.Size == nil || *v.Size != 2048 || len(v.UsedBy) != 1 || v.UsedBy[0].Name != "web" || !v.UsedBy[0].Running || v.Created == nil {
		t.Errorf("shop_data = %+v", v)
	}
	if v := vols[0]; len(v.UsedBy) != 0 || v.Labels == nil || v.Created != nil {
		t.Errorf("old_data = %+v, want it unused, with no labels", v)
	}

	var images []api.DockerImage
	get(t, e.h, "/api/devices/bield/docker/images", phoneIP, &images)
	if len(images) != 2 || !images[0].Dangling || images[0].Size != 100 || len(images[0].Tags) != 0 || len(images[0].Digests) != 0 {
		t.Fatalf("images = %+v, want the newer, untagged one first", images)
	}
	if i := images[1]; i.Dangling || i.Tags[0] != "nginx:1.27" || len(i.UsedBy) != 1 || i.UsedBy[0].ID != webID {
		t.Errorf("nginx = %+v", i)
	}

	var nets []api.DockerNetwork
	get(t, e.h, "/api/devices/bield/docker/networks", phoneIP, &nets)
	if len(nets) != 2 || nets[0].Name != "shop_default" || nets[1].Name != "bridge" {
		t.Fatalf("networks = %+v, want the one with a container first", nets)
	}
	if n := nets[0]; n.Project != "shop" || len(n.Subnets) != 1 || n.Subnets[0].Subnet != "172.20.0.0/16" || len(n.Containers) != 1 ||
		n.Containers[0].Name != "web" || n.Containers[0].IP != "172.20.0.2" || n.Containers[0].MAC == "" {
		t.Errorf("shop_default = %+v", n)
	}
	if n := nets[1]; n.Project != "" || len(n.Containers) != 0 || n.Containers == nil {
		t.Errorf("bridge = %+v, want no project and no containers", n)
	}
}

func TestDocker_actions(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)

	var res api.DockerActionResult
	if rec := e.do(t, http.MethodPost, "/api/devices/laptop/docker/containers/web/stop", laptopIP); rec.Code != http.StatusOK ||
		json.Unmarshal(rec.Body.Bytes(), &res) != nil || !res.Changed {
		t.Fatalf("stop = %d %s, want 200 and a change", rec.Code, rec.Body)
	}
	// Docker waits as long as the container's own stop timeout says, as it
	// does for `docker stop`: Bield doesn't give a time.
	if got := e.laptop.query("/containers/web/stop"); got != "" {
		t.Errorf("Docker was told %q, want no time given for the container to stop", got)
	}
	if rec := e.do(t, http.MethodPost, "/api/devices/laptop/docker/containers/web/restart", laptopIP); rec.Code != http.StatusOK {
		t.Fatalf("restart = %d %s", rec.Code, rec.Body)
	}
	// Docker answers 304 for a container that runs already: nothing changed,
	// so there's nothing to log.
	res = api.DockerActionResult{Changed: true}
	if rec := e.do(t, http.MethodPost, "/api/devices/laptop/docker/containers/web/start", laptopIP); rec.Code != http.StatusOK ||
		json.Unmarshal(rec.Body.Bytes(), &res) != nil || res.Changed {
		t.Errorf("start = %d %s, want 200 and no change", rec.Code, rec.Body)
	}
	// By ID too.
	if rec := e.do(t, http.MethodPost, "/api/devices/laptop/docker/containers/"+backupID+"/start", laptopIP); rec.Code != http.StatusOK {
		t.Errorf("start by ID = %d %s", rec.Code, rec.Body)
	}

	var msgs []string
	for _, ev := range e.events(t, api.EventKindContainer) {
		if ev.Actor != alice || ev.DeviceID != "laptop" || ev.Level != string(api.EventLevelInfo) {
			t.Errorf("event %+v, want one by alice about the laptop", ev)
		}
		msgs = append(msgs, ev.Message)
	}
	if want := "Started backup on laptop|Restarted web on laptop|Stopped web on laptop"; strings.Join(msgs, "|") != want {
		t.Errorf("events = %q, want %q", msgs, want)
	}
}

func TestDocker_history(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	now := time.Now()
	row := func(device, name string, ago time.Duration, cpu float64) store.ContainerMetricsRow {
		return store.ContainerMetricsRow{DeviceID: device, Name: name, At: now.Add(-ago), CPU: &cpu, Mem: new(1000.0)}
	}
	err := e.st.AddContainerMetrics(t.Context(), []store.ContainerMetricsRow{
		row(store.HubID, "web", 10*time.Minute, 10),
		row(store.HubID, "web", 9*time.Minute, 30),
		row(store.HubID, "web", 3*time.Hour, 50),
		row(store.HubID, "backup", 10*time.Minute, 99),
		row("laptop", "web", 10*time.Minute, 77),
	})
	if err != nil {
		t.Fatal(err)
	}
	var got api.DockerHistory
	get(t, e.h, "/api/devices/bield/docker/containers/web/history?range=1h", phoneIP, &got)
	if got.Step != time.Minute.Milliseconds() || len(got.CPU) != 2 || got.CPU[0].V == nil || *got.CPU[0].V != 10 || *got.CPU[1].V != 30 ||
		len(got.Mem) != 2 || got.NetRx[0].V != nil {
		t.Errorf("1h history = %+v, want the hub's web for the last hour", got)
	}
	// By ID: it's kept under the container's name.
	get(t, e.h, "/api/devices/bield/docker/containers/"+webID+"/history?range=24h", phoneIP, &got)
	// The two recent rows share a five minute step or have one each,
	// depending on the time of day. The one from three hours ago is first.
	if n := len(got.CPU); n < 2 || n > 3 || got.CPU[0].V == nil || *got.CPU[0].V != 50 {
		t.Errorf("24h history by ID = %+v, want the row from 3 hours ago, and then the recent ones", got.CPU)
	}
	// The laptop's is another history.
	get(t, e.h, "/api/devices/laptop/docker/containers/web/history", phoneIP, &got)
	if len(got.CPU) != 1 || *got.CPU[0].V != 77 {
		t.Errorf("laptop's history = %+v", got.CPU)
	}
	// A container that Docker forgot still has its history.
	get(t, e.h, "/api/devices/bield/docker/containers/gone/history", phoneIP, &got)
	if len(got.CPU) != 0 {
		t.Errorf("history of a container that never was = %+v", got.CPU)
	}
	if rec := e.do(t, http.MethodGet, "/api/devices/bield/docker/containers/web/history?range=1y", phoneIP); rec.Code != http.StatusBadRequest {
		t.Errorf("history for a year = %d, want 400", rec.Code)
	}
}

func TestDocker_logDownload(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	rec := e.do(t, http.MethodGet, "/api/devices/laptop/docker/containers/web/logs/download", laptopIP)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, `attachment; filename="web-20`) || !strings.HasSuffix(cd, `.log"`) {
		t.Errorf("Content-Disposition = %q, want an attachment named for the container and the time", cd)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", rec.Header().Get("X-Content-Type-Options"))
	}
	// Terminal escapes are gone, and both streams are in, in order.
	want := "2027-03-01T10:00:00.5Z first line\n2027-03-01T10:00:01Z red error\n2027-03-01T10:00:02Z last line\n"
	if rec.Body.String() != want {
		t.Errorf("body = %q, want %q", rec.Body, want)
	}
	q := e.laptop.query("/containers/" + webID + "/logs")
	for _, part := range []string{"follow=0", "stdout=1", "stderr=1", "tail=all", "timestamps=1"} {
		if !strings.Contains(q, part) {
			t.Errorf("Docker was asked for %q, want %s in it", q, part)
		}
	}

	rec = e.do(t, http.MethodGet, "/api/devices/laptop/docker/containers/web/logs/download?timestamps=0&stderr=0&tail=2&since=15m", laptopIP)
	if want := "first line\nred error\nlast line\n"; rec.Body.String() != want {
		t.Errorf("body without timestamps = %q, want %q", rec.Body, want)
	}
	q = e.laptop.query("/containers/" + webID + "/logs")
	for _, part := range []string{"stderr=0", "tail=2", "since="} {
		if !strings.Contains(q, part) {
			t.Errorf("Docker was asked for %q, want %s in it", q, part)
		}
	}

	// A client that takes gzip gets it.
	req := newRequest(t, http.MethodGet, "/api/devices/laptop/docker/containers/web/logs/download", laptopIP)
	req.Header.Set("Accept-Encoding", "gzip")
	rec = serve(e.h, req)
	zr, err := gzip.NewReader(rec.Body)
	if err != nil || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("gzip download: %v, Content-Encoding = %q", err, rec.Header().Get("Content-Encoding"))
	}
	var sb strings.Builder
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		sb.WriteString(sc.Text() + "\n")
	}
	if !strings.Contains(sb.String(), "red error") || strings.Contains(sb.String(), "\x1b") {
		t.Errorf("gzip body = %q", sb.String())
	}

	for _, bad := range []string{"tail=-1", "tail=lots", "since=yesterday", "stdout=0&stderr=0"} {
		if rec := e.do(t, http.MethodGet, "/api/devices/laptop/docker/containers/web/logs/download?"+bad, laptopIP); rec.Code != http.StatusBadRequest {
			t.Errorf("download with %s = %d %s, want 400", bad, rec.Code, rec.Body)
		}
	}
}

// eventsOf reads the events of a stream until it ends.
func eventsOf(t *testing.T, events <-chan event) []event {
	t.Helper()
	var out []event
	for {
		select {
		case e, ok := <-events:
			if !ok {
				return out
			}
			out = append(out, e)
		default:
			return out
		}
	}
}

func TestDocker_logs(t *testing.T) {
	for _, gz := range []bool{true, false} {
		name := "plain"
		if gz {
			name = "gzip"
		}
		t.Run(name, func(t *testing.T) {
			st := testStore(t)
			synctest.Test(t, func(t *testing.T) {
				e := newDockerEnv(t, st, false)
				events := openEvents(t, e.tn, e.h, laptopIP, "/api/devices/laptop/docker/containers/web/logs?follow=0&tail=50&since=1h", gz)
				synctest.Wait()
				got := eventsOf(t, events)
				if len(got) != 3 || got[0].name != "start" || got[1].name != "lines" || got[2].name != "end" {
					t.Fatalf("events = %+v, want start, lines and end", got)
				}
				var start api.DockerLogStart
				var lines []api.DockerLogLine
				var end api.DockerStreamEnd
				for i, dst := range []any{&start, &lines, &end} {
					if err := json.Unmarshal([]byte(got[i].data), dst); err != nil {
						t.Fatalf("decode %s %q: %v", got[i].name, got[i].data, err)
					}
				}
				if start.TTY || !start.Running {
					t.Errorf("start = %+v, want a running container without a terminal", start)
				}
				want := []api.DockerLogLine{
					{S: 1, T: "2027-03-01T10:00:00.5Z", M: "first line"},
					{S: 2, T: "2027-03-01T10:00:01Z", M: "\x1b[31mred error\x1b[0m"},
					{S: 1, T: "2027-03-01T10:00:02Z", M: "last line"},
				}
				if len(lines) != 3 || lines[0] != want[0] || lines[1] != want[1] || lines[2] != want[2] {
					t.Errorf("lines = %+v, want %+v", lines, want)
				}
				if end.Reason != "End of log" {
					t.Errorf("end = %+v", end)
				}
				q := e.laptop.query("/containers/" + webID + "/logs")
				for _, part := range []string{"follow=0", "tail=50", "since=", "timestamps=1"} {
					if !strings.Contains(q, part) {
						t.Errorf("Docker was asked for %q, want %s in it", q, part)
					}
				}
			})
		})
	}
}

func TestDocker_logsFollow(t *testing.T) {
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		e := newDockerEnv(t, st, false)
		events := openEvents(t, e.tn, e.h, laptopIP, "/api/devices/laptop/docker/containers/web/logs", true)
		time.Sleep(time.Second)
		synctest.Wait()
		// A log that follows starts with the latest lines, and stays open.
		got := eventsOf(t, events)
		if len(got) != 2 || got[0].name != "start" || got[1].name != "lines" {
			t.Fatalf("events = %+v, want start and lines, and the stream open", got)
		}
		if q := e.laptop.query("/containers/" + webID + "/logs"); !strings.Contains(q, "follow=1") || !strings.Contains(q, "tail=500") {
			t.Errorf("Docker was asked for %q, want a followed tail of 500", q)
		}
		// Nothing happens for a while, and the stream is kept alive.
		time.Sleep(30 * time.Second)
		synctest.Wait()
		for _, ev := range eventsOf(t, events) {
			// The reader here takes the comment that keeps the stream alive
			// for an event with no name.
			if ev.name != "" {
				t.Errorf("event %+v, want only comments to keep the stream alive", ev)
			}
		}
		// Demoting the admin ends the stream. Bield keeps one admin.
		if _, err := e.st.SetRole(context.Background(), bob, store.Admin); err != nil {
			t.Fatal(err)
		}
		if _, err := e.st.SetRole(context.Background(), alice, store.Viewer); err != nil {
			t.Fatal(err)
		}
		time.Sleep(api.RoleCheckInterval)
		synctest.Wait()
		for _, ev := range eventsOf(t, events) {
			if ev.name != "" {
				t.Errorf("got event %+v after alice stopped being an admin, want the stream to end", ev)
			}
		}
		if ev, ok := <-events; ok {
			t.Errorf("got event %+v after alice stopped being an admin, want the stream to end", ev)
		}
	})
}

func TestDocker_logsOfNothing(t *testing.T) {
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		e := newDockerEnv(t, st, false)
		events := openEvents(t, e.tn, e.h, laptopIP, "/api/devices/laptop/docker/containers/web/logs?stdout=0&stderr=0", true)
		synctest.Wait()
		got := eventsOf(t, events)
		if len(got) != 2 || got[1].name != "end" || !strings.Contains(got[1].data, "Select stdout or stderr") {
			t.Errorf("events = %+v, want start, and an end that says to choose a stream", got)
		}
		if n := e.laptop.hit("GET /containers/" + webID + "/logs"); n != 0 {
			t.Errorf("Docker was asked for logs %d times, want 0", n)
		}
	})
}

func TestDocker_stats(t *testing.T) {
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		e := newDockerEnv(t, st, false)
		// Viewers can watch a container's stats.
		events := openEvents(t, e.tn, e.h, phoneIP, "/api/devices/laptop/docker/containers/web/stats", true)
		time.Sleep(3500 * time.Millisecond)
		synctest.Wait()
		got := eventsOf(t, events)
		if len(got) < 3 || got[0].name != "backlog" || got[1].name != "sample" {
			t.Fatalf("events = %+v, want a backlog and then a sample a second", got)
		}
		var backlog []api.DockerStats
		if err := json.Unmarshal([]byte(got[0].data), &backlog); err != nil || backlog == nil {
			t.Errorf("backlog = %q, %v, want a list", got[0].data, err)
		}
		var first, second api.DockerStats
		if err := json.Unmarshal([]byte(got[1].data), &first); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(got[2].data), &second); err != nil {
			t.Fatal(err)
		}
		if first.CPU != nil || second.CPU == nil || *second.CPU < 24.9 || *second.CPU > 25.1 || second.TS-first.TS != 1000 ||
			second.MemUsed == nil || *second.MemUsed != 800 {
			t.Errorf("samples = %+v and %+v, want 25%% CPU in the second", first, second)
		}

		// A second viewer shares the stream and starts with what happened.
		more := readEvents(t, e.tn, laptopIP, "/api/devices/laptop/docker/containers/web/stats", false)
		synctest.Wait()
		late := eventsOf(t, more)
		if len(late) == 0 || late[0].name != "backlog" {
			t.Fatalf("a second viewer's events = %+v", late)
		}
		var lateBacklog []api.DockerStats
		if err := json.Unmarshal([]byte(late[0].data), &lateBacklog); err != nil || len(lateBacklog) < 3 {
			t.Errorf("a second viewer's backlog = %d samples (%v), want what came before", len(lateBacklog), err)
		}
		if n := e.laptop.hit("GET /containers/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/stats"); n != 1 {
			t.Errorf("Docker was asked for the stream %d times, want once for both", n)
		}

		// The container stops: the stream says so, and ends.
		if rec := e.do(t, http.MethodPost, "/api/devices/laptop/docker/containers/web/stop", laptopIP); rec.Code != http.StatusOK {
			t.Fatalf("stop = %d %s", rec.Code, rec.Body)
		}
		time.Sleep(10 * time.Second)
		synctest.Wait()
		var last event
		for _, ev := range eventsOf(t, events) {
			last = ev
		}
		if last.name != "end" || !strings.Contains(last.data, "The container stopped") {
			t.Errorf("last event = %+v, want end: the container stopped", last)
		}
	})
}

func TestDocker_statsOfAStoppedContainer(t *testing.T) {
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		e := newDockerEnv(t, st, false)
		events := openEvents(t, e.tn, e.h, laptopIP, "/api/devices/laptop/docker/containers/backup/stats", true)
		synctest.Wait()
		got := eventsOf(t, events)
		if len(got) != 2 || got[0].name != "backlog" || got[0].data != "[]" || got[1].name != "end" || !strings.Contains(got[1].data, "isn't running") {
			t.Errorf("events = %+v, want an empty backlog and an end", got)
		}
	})
}

func TestDocker_statsEndWhenTheRoleDoes(t *testing.T) {
	st := testStore(t)
	synctest.Test(t, func(t *testing.T) {
		e := newDockerEnv(t, st, false)
		events := openEvents(t, e.tn, e.h, phoneIP, "/api/devices/laptop/docker/containers/web/stats", true)
		synctest.Wait()
		if _, err := e.st.RemoveUser(context.Background(), bob); err != nil {
			t.Fatal(err)
		}
		time.Sleep(api.RoleCheckInterval + time.Second)
		synctest.Wait()
		// What came before is fine. The stream must have ended.
		eventsOf(t, events)
		if ev, ok := <-events; ok {
			t.Errorf("got event %+v after bob lost his role, want the stream to end", ev)
		}
	})
}

func TestDocker_deviceFlag(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	feed := func(local bool) api.Snapshot {
		opts := api.FeedOptions{Tailnet: e.opts.Snapshot, Store: e.st, SSH: e.opts.SSH.Access}
		if local {
			opts.Docker = func() docker.Reading { return docker.Reading{} }
		}
		return api.NewFeed(opts).Latest()
	}
	for _, tt := range []struct {
		name  string
		local bool
		want  map[string]bool
	}{
		// The machine Bield runs on has Docker if it reads it, and the laptop
		// if the check found a socket. Tailscale doesn't say what the server
		// runs, and Bield has no access to it.
		{"with a Docker socket here", true, map[string]bool{"bield": true, "laptop": true, "server": false, "phone": false}},
		{"without one", false, map[string]bool{"bield": false, "laptop": true, "server": false, "phone": false}},
	} {
		snap := feed(tt.local)
		for id, want := range tt.want {
			if got := findDevice(t, snap, id).Docker; got != want {
				t.Errorf("%s: %s has Docker: %v, want %v", tt.name, id, got, want)
			}
		}
	}
	// Viewers get the flag, but not how Bield logs in.
	dev, ok := api.Redact(findDevice(t, feed(true), "laptop")).(api.Device)
	if !ok || !dev.Docker || dev.SSH != nil {
		t.Errorf("a viewer gets docker %v and ssh %+v, want the flag and no access", dev.Docker, dev.SSH)
	}

	// A check that finds no socket takes it away.
	e.sshd.Handle(func(*sshtest.Exec) int { return 0 })
	if _, err := e.opts.SSH.Check(t.Context(), "laptop"); err != nil {
		t.Fatal(err)
	}
	if findDevice(t, feed(true), "laptop").Docker {
		t.Error("the laptop has Docker after a check that found no socket")
	}
}

// In embedded mode, the machine Bield runs on is two nodes. Only the machine's
// own has Docker, so a list of machines with Docker has it once.
func TestDocker_deviceFlagOfTheHostMachine(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	status, err := e.tn.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The test tailnet's server is the machine Bield runs on.
	for i := range status.Peers {
		status.Peers[i].SameMachine = status.Peers[i].ID == "server"
	}
	e.tn.SetStatus(*status)
	snap := api.NewFeed(api.FeedOptions{
		Tailnet: e.opts.Snapshot, Store: e.st, SSH: e.opts.SSH.Access,
		Docker: func() docker.Reading { return docker.Reading{} },
	}).Latest()
	for id, want := range map[string]bool{"bield": false, "server": true, "laptop": true, "phone": false} {
		if got := findDevice(t, snap, id).Docker; got != want {
			t.Errorf("%s has Docker: %v, want %v", id, got, want)
		}
	}
}

// A request that names no device and a container that can't be one is
// answered once.
func TestDocker_answersOnce(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	for _, path := range []string{
		"/api/devices/nobody/docker/containers/a%20b/inspect",
		"/api/devices/nobody/docker/containers/web/logs",
		"/api/devices/server/docker/containers/a%20b",
	} {
		rec := e.do(t, http.MethodGet, path, laptopIP)
		dec := json.NewDecoder(rec.Body)
		var first api.ErrorBody
		if err := dec.Decode(&first); err != nil || first.Error == "" {
			t.Errorf("GET %s = %d %q, want an error body", path, rec.Code, rec.Body)
			continue
		}
		if dec.More() {
			t.Errorf("GET %s answered twice: %q", path, rec.Body)
		}
	}
	rec := e.do(t, http.MethodPost, "/api/devices/nobody/docker/containers/a%20b/stop", laptopIP)
	if rec.Code != http.StatusNotFound || strings.Count(rec.Body.String(), `"error"`) != 1 {
		t.Errorf("POST = %d %q, want one 404", rec.Code, rec.Body)
	}
}

// How Bield reaches a machine is for admins, as a device's SSH access is. A
// viewer who can see the machine's Docker is told only that it can't.
func TestDocker_viewersAreNotToldHowBieldReachesAMachine(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	e.sshd.Drop()
	e.sshd.Refuse(true)
	admin := e.do(t, http.MethodGet, "/api/devices/laptop/docker/overview", laptopIP)
	viewer := e.do(t, http.MethodGet, "/api/devices/laptop/docker/overview", phoneIP)
	if admin.Code < 500 && admin.Code != http.StatusConflict || viewer.Code != admin.Code {
		t.Fatalf("with SSH down, admin got %d %s and viewer %d %s, want the same failure", admin.Code, admin.Body, viewer.Code, viewer.Body)
	}
	if got := errorOf(t, viewer); got != "Bield can't reach Docker on this machine right now" {
		t.Errorf("a viewer was told %q", got)
	}
	// The admin hears why, and it isn't the viewer's message.
	if got := errorOf(t, admin); got == errorOf(t, viewer) || got == "" {
		t.Errorf("an admin was told %q, want the reason", got)
	}
	// What Docker answers to a bad request is for anyone.
	e.sshd.Refuse(false)
	if rec := e.do(t, http.MethodGet, "/api/devices/bield/docker/containers/nope", phoneIP); rec.Code != http.StatusNotFound || errorOf(t, rec) != "No such container: nope" {
		t.Errorf("a viewer asking for a container that isn't there got %d %s", rec.Code, rec.Body)
	}
	// And the messages about how a machine is set up.
	rec := e.do(t, http.MethodGet, "/api/devices/server/docker/overview", phoneIP)
	if rec.Code != http.StatusConflict || strings.Contains(rec.Body.String(), "SSH") {
		t.Errorf("a viewer asking for a machine with no SSH access got %d %s", rec.Code, rec.Body)
	}
	if rec := e.do(t, http.MethodGet, "/api/devices/server/docker/overview", laptopIP); !strings.Contains(rec.Body.String(), "SSH access isn't set up for server") {
		t.Errorf("an admin got %s, want to hear that SSH isn't set up", rec.Body)
	}
}

func TestDocker_volumeLabelsThatLookLikeCredentialsAreForAdmins(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	var admin, viewer []api.DockerVolume
	get(t, e.h, "/api/devices/bield/docker/volumes", laptopIP, &admin)
	rec := serve(e.h, newRequest(t, http.MethodGet, "/api/devices/bield/docker/volumes", phoneIP))
	if err := json.Unmarshal(rec.Body.Bytes(), &viewer); err != nil {
		t.Fatal(err)
	}
	find := func(vols []api.DockerVolume) api.DockerVolume {
		for _, v := range vols {
			if v.Name == "shop_data" {
				return v
			}
		}
		t.Fatal("no shop_data")
		return api.DockerVolume{}
	}
	if v := find(admin); v.SecretLabels["backup.password"] != "volpw-test" || v.Labels["tier"] != "gold" || v.Labels["backup.password"] != "" {
		t.Errorf("admin's labels = %v, secret = %v", v.Labels, v.SecretLabels)
	}
	if v := find(viewer); v.Labels["tier"] != "gold" || len(v.SecretLabels) != 0 || strings.Contains(rec.Body.String(), "volpw-test") {
		t.Errorf("viewer's labels = %v, secret = %v, body %s", v.Labels, v.SecretLabels, rec.Body)
	}
}

// Docker acts on a container once it's told to, so the activity log says so
// even when the admin's browser went away while it did.
func TestDocker_actionIsLoggedWhenTheAdminLeaves(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	e.laptop.hold = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req := newRequestBody(t, http.MethodPost, "/api/devices/laptop/docker/containers/web/stop", laptopIP, strings.NewReader("")).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.h.ServeHTTP(rec, req)
	}()
	for range 500 {
		if e.laptop.hit("POST /containers/web/stop") > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e.laptop.hit("POST /containers/web/stop") == 0 {
		t.Fatal("Docker was never asked to stop web")
	}
	cancel()
	time.Sleep(50 * time.Millisecond)
	close(e.laptop.hold)
	<-done
	events := e.events(t, api.EventKindContainer)
	if len(events) != 1 || events[0].Message != "Stopped web on laptop" || events[0].Actor != alice {
		t.Errorf("events = %+v, want Stopped web on laptop, by alice", events)
	}
}

// A download that breaks in the middle ends in an error, not in a file that
// looks whole.
func TestDocker_logDownloadThatBreaks(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	rec := httptest.NewRecorder()
	req := newRequest(t, http.MethodGet, "/api/devices/bield/docker/containers/backup/logs/download", laptopIP)
	var aborted any
	func() {
		defer func() { aborted = recover() }()
		e.h.ServeHTTP(rec, req)
	}()
	if err, _ := aborted.(error); !errors.Is(err, http.ErrAbortHandler) {
		t.Errorf("the response ended with %v (and %d %q), want it cut off", aborted, rec.Code, rec.Body)
	}
}

func TestDocker_containerWithNoExposedPorts(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	rec := e.do(t, http.MethodGet, "/api/devices/bield/docker/containers/backup", laptopIP)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	// Lists are lists, never null, since the web app's types say so.
	for _, field := range []string{`"exposed":[]`, `"ports":[]`, `"networks":[]`, `"mounts":[]`, `"dns":[]`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("the detail has no %s: %s", field, rec.Body)
		}
	}
}

// What a container keeps secret is read in the open by an admin, and the log
// says so: once for a processes list that refreshes, and not at all for a
// viewer, who is refused.
func TestDocker_secretReadsAreLogged(t *testing.T) {
	t.Parallel()
	e := newDockerEnv(t, testStore(t), false)
	base := "/api/devices/laptop/docker/containers/web/"
	for _, path := range []string{"inspect", "inspect", "top", "top", "top", "logs/download", "logs/download"} {
		if rec := e.do(t, http.MethodGet, base+path, laptopIP); rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
		}
	}
	for _, path := range []string{"inspect", "top", "logs", "logs/download"} {
		if rec := e.do(t, http.MethodGet, base+path, phoneIP); rec.Code != http.StatusForbidden {
			t.Fatalf("a viewer's GET %s = %d, want 403", path, rec.Code)
		}
	}
	var msgs []string
	for _, ev := range e.events(t, api.EventKindContainer) {
		if ev.Actor != alice || ev.DeviceID != "laptop" {
			t.Errorf("event %+v, want one by alice about the laptop", ev)
		}
		msgs = append(msgs, ev.Message)
	}
	want := "Downloaded the logs of web on laptop|Listed the processes of web on laptop|Inspected web on laptop"
	if strings.Join(msgs, "|") != want {
		t.Errorf("events = %q, want %q", msgs, want)
	}
}

package docker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/manasseh-fikadu/bield/internal/docker"
)

// fakeDocker answers the Engine API's list and inspect with the containers
// it holds, by ID.
type fakeDocker struct {
	mu         sync.Mutex
	containers []map[string]any
	inspect    map[string]map[string]any
	// broken makes every request fail.
	broken bool
}

func (f *fakeDocker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.broken {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"the daemon is restarting"}`))
		return
	}
	switch {
	case r.URL.Path == "/containers/json" && r.URL.Query().Get("all") == "1":
		reply(w, f.containers)
	case strings.HasPrefix(r.URL.Path, "/containers/") && strings.HasSuffix(r.URL.Path, "/json"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/json")
		in, ok := f.inspect[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"No such container"}`))
			return
		}
		reply(w, in)
	default:
		http.NotFound(w, r)
	}
}

func reply(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{
		containers: []map[string]any{
			{
				"Id": "bbbbbbbbbbbb2222", "Names": []string{"/web-app-1"}, "Image": "nginx:1.27", "State": "running",
				"Status": "Up 2 hours (healthy)", "Created": 1_800_000_000,
				"Labels": map[string]string{"com.docker.compose.project": "web", "com.docker.compose.service": "app"},
				"Ports": []map[string]any{
					{"IP": "0.0.0.0", "PrivatePort": 80, "PublicPort": 8080, "Type": "tcp"},
					{"IP": "::", "PrivatePort": 80, "PublicPort": 8080, "Type": "tcp"},
					{"PrivatePort": 443, "Type": "tcp"},
				},
			},
			{
				"Id": "aaaaaaaaaaaa1111", "Names": []string{"/backup"}, "Image": "restic", "State": "exited",
				"Status": "Exited (1) 5 minutes ago", "Created": 1_800_000_000,
			},
			{
				// Removed between the list and its inspection.
				"Id": "cccccccccccc3333", "Names": []string{"/gone"}, "Image": "busybox", "State": "running",
				"Status": "Up 1 second (unhealthy)", "Created": 1_800_000_000,
			},
		},
		inspect: map[string]map[string]any{
			"bbbbbbbbbbbb2222": {
				"RestartCount": 2,
				"State": map[string]any{
					"ExitCode": 0, "StartedAt": "2027-01-15T08:00:00.5Z", "FinishedAt": "0001-01-01T00:00:00Z",
					"Health": map[string]any{"Status": "healthy"},
				},
				"HostConfig": map[string]any{"RestartPolicy": map[string]any{"Name": "unless-stopped"}},
			},
			"aaaaaaaaaaaa1111": {
				"State":      map[string]any{"ExitCode": 1, "OOMKilled": true, "StartedAt": "2027-01-15T07:00:00Z", "FinishedAt": "2027-01-15T07:55:00Z"},
				"HostConfig": map[string]any{"RestartPolicy": map[string]any{"Name": ""}},
			},
		},
	}
}

// client returns a Client for a fake Docker on the in-memory network.
func client(t *testing.T, f http.Handler) *docker.Client {
	t.Helper()
	srv := httptest.NewTestServer(t, f)
	tr, ok := srv.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("the test server's client has a %T", srv.Client().Transport)
	}
	return docker.NewClient(tr.DialContext)
}

func TestClient_Containers(t *testing.T) {
	t.Parallel()
	got, err := client(t, newFakeDocker()).Containers(t.Context())
	if err != nil {
		t.Fatalf("Containers() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Containers() = %+v, want 3", got)
	}
	backup, gone, web := got[0], got[1], got[2]
	if backup.Name != "backup" || backup.State != "exited" || backup.ExitCode != 1 || !backup.OOMKilled ||
		backup.RestartPolicy != "no" || !backup.FinishedAt.Equal(time.Date(2027, 1, 15, 7, 55, 0, 0, time.UTC)) {
		t.Errorf("backup = %+v, want it exited 1 and killed for memory, without a restart policy", backup)
	}
	if web.Name != "web-app-1" || web.Project != "web" || web.Service != "app" || web.Health != "healthy" ||
		web.RestartCount != 2 || web.RestartPolicy != "unless-stopped" || !web.FinishedAt.IsZero() ||
		!web.StartedAt.Equal(time.Date(2027, 1, 15, 8, 0, 0, 5e8, time.UTC)) || !web.Created.Equal(time.Unix(1_800_000_000, 0)) {
		t.Errorf("web = %+v, want its compose labels, health, restarts and times", web)
	}
	if len(web.Ports) != 2 || web.Ports[0] != (docker.Port{IP: "0.0.0.0", Private: 80, Public: 8080, Type: "tcp"}) ||
		web.Ports[1] != (docker.Port{Private: 443, Type: "tcp"}) {
		t.Errorf("web's ports = %+v, want 8080→80 once, and 443 unpublished", web.Ports)
	}
	if gone.Health != "unhealthy" || gone.RestartPolicy != "" {
		t.Errorf("gone = %+v, want what the list said", gone)
	}
}

func TestClient_errors(t *testing.T) {
	t.Parallel()
	f := newFakeDocker()
	f.broken = true
	_, err := client(t, f).Containers(t.Context())
	if err == nil || !strings.Contains(err.Error(), "the daemon is restarting") {
		t.Errorf("Containers() error = %v, want Docker's message", err)
	}
}

func TestCollector(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		f := newFakeDocker()
		c := docker.NewCollector(client(t, f), nil)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go c.Run(ctx)
		synctest.Wait()
		<-c.Changes()
		r := c.Reading()
		if len(r.Containers) != 3 || r.Err != nil || !r.At.Equal(time.Now()) {
			t.Fatalf("Reading() = %+v, want 3 containers now", r)
		}

		// A read that fails keeps the containers from before.
		f.mu.Lock()
		f.broken = true
		f.mu.Unlock()
		time.Sleep(docker.Interval)
		synctest.Wait()
		r = c.Reading()
		if len(r.Containers) != 3 || r.Err == nil || !r.At.Equal(time.Now().Add(-docker.Interval)) {
			t.Errorf("after a failed read, Reading() = %+v, want the containers from before and an error", r)
		}
	})
}

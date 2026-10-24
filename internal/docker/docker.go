// Package docker reads the containers on the machine Roost runs on, from the
// Docker Engine API on its socket. Managing them comes with #29.
package docker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Interval is how often a Collector reads the containers.
const Interval = 10 * time.Second

const (
	// defaultSocket is where Docker listens, unless DOCKER_HOST says.
	defaultSocket = "/var/run/docker.sock"
	// timeout is how long reading the containers can take.
	timeout = 15 * time.Second
	// maxInspecting is how many containers are inspected at once.
	maxInspecting = 8
)

// Socket returns the path of the Docker socket on this machine, from
// DOCKER_HOST or the default, and whether there's one. Roost only talks to
// a Docker on a unix socket.
func Socket() (string, bool) {
	path := defaultSocket
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		p, ok := strings.CutPrefix(host, "unix://")
		if !ok {
			return "", false
		}
		path = p
	}
	info, err := os.Stat(path) //nolint:gosec // G703: DOCKER_HOST is Roost's own environment.
	return path, err == nil && info.Mode()&os.ModeSocket != 0
}

// A Container is what Docker says of a container.
type Container struct {
	ID   string
	Name string
	// Image is the image it was created from, as it was named.
	Image string
	// State is "created", "running", "paused", "restarting", "removing",
	// "exited" or "dead".
	State string
	// Status says it in words, such as "Up 2 hours (healthy)".
	Status string
	// Health is "healthy", "unhealthy" or "starting", or "" if it has no
	// health check.
	Health string
	// ExitCode is how it last exited.
	ExitCode  int
	OOMKilled bool
	// RestartCount is how many times Docker restarted it.
	RestartCount int
	// RestartPolicy is "no", "always", "unless-stopped" or "on-failure".
	RestartPolicy string
	Created       time.Time
	// StartedAt and FinishedAt are zero if it never started or finished.
	StartedAt, FinishedAt time.Time
	// Project and Service are its Docker Compose project and service, if it
	// has them.
	Project, Service string
	Ports            []Port
}

// A Port is a port a container listens on, and the host port that leads to
// it, if one does.
type Port struct {
	// IP is the host address the port is published on, if it is.
	IP string
	// Private is the container's port, and Public the host's, or 0.
	Private, Public uint16
	// Type is "tcp", "udp" or "sctp".
	Type string
}

// A Client talks to the Docker Engine API.
type Client struct {
	http *http.Client
}

// NewClient returns a Client that connects with dial, such as to the socket
// SocketDialer returns.
func NewClient(dial func(ctx context.Context, network, addr string) (net.Conn, error)) *Client {
	return &Client{http: &http.Client{Transport: &http.Transport{
		DialContext:     dial,
		MaxIdleConns:    maxInspecting,
		IdleConnTimeout: 30 * time.Second,
	}}}
}

// SocketDialer returns a dial function that connects to the unix socket at
// path, whatever address it's asked for.
func SocketDialer(path string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		conn, err := d.DialContext(ctx, "unix", path)
		if errors.Is(err, syscall.EACCES) || errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("%w; add the user Roost runs as to the docker group, and restart Roost", err)
		}
		return conn, err
	}
}

type listed struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort uint16 `json:"PrivatePort"`
		PublicPort  uint16 `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
}

type inspected struct {
	RestartCount int `json:"RestartCount"`
	State        struct {
		ExitCode   int    `json:"ExitCode"`
		OOMKilled  bool   `json:"OOMKilled"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
		Health     *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	HostConfig struct {
		RestartPolicy struct {
			Name string `json:"Name"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
}

// Containers returns every container, running or not, by name.
func (c *Client) Containers(ctx context.Context) ([]Container, error) {
	var list []listed
	if err := c.get(ctx, "/containers/json?all=1", &list); err != nil {
		return nil, fmt.Errorf("list the containers: %w", err)
	}
	out := make([]Container, len(list))
	var (
		wg    sync.WaitGroup
		slots = make(chan struct{}, maxInspecting)
	)
	for i, l := range list {
		out[i] = fromList(l)
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			var in inspected
			// A container removed since the list just keeps what the list said.
			if err := c.get(ctx, "/containers/"+url.PathEscape(l.ID)+"/json", &in); err == nil {
				addInspect(&out[i], in)
			}
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b Container) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

func fromList(l listed) Container {
	name := l.ID
	if len(name) > 12 {
		name = name[:12]
	}
	if len(l.Names) > 0 {
		name = strings.TrimPrefix(l.Names[0], "/")
	}
	c := Container{
		ID:      l.ID,
		Name:    name,
		Image:   l.Image,
		State:   l.State,
		Status:  l.Status,
		Health:  healthFromStatus(l.Status),
		Created: time.Unix(l.Created, 0),
		Project: l.Labels["com.docker.compose.project"],
		Service: l.Labels["com.docker.compose.service"],
	}
	for _, p := range l.Ports {
		port := Port{IP: p.IP, Private: p.PrivatePort, Public: p.PublicPort, Type: p.Type}
		// Docker lists a port published on every address twice, for IPv4 and
		// IPv6.
		if !slices.ContainsFunc(c.Ports, func(q Port) bool {
			return q.Private == port.Private && q.Public == port.Public && q.Type == port.Type && anyAddr(q.IP) && anyAddr(port.IP)
		}) {
			c.Ports = append(c.Ports, port)
		}
	}
	return c
}

func anyAddr(ip string) bool { return ip == "0.0.0.0" || ip == "::" || ip == "" }

func addInspect(c *Container, in inspected) {
	c.RestartCount = in.RestartCount
	c.ExitCode = in.State.ExitCode
	c.OOMKilled = in.State.OOMKilled
	c.RestartPolicy = cmp.Or(in.HostConfig.RestartPolicy.Name, "no")
	c.StartedAt = dockerTime(in.State.StartedAt)
	c.FinishedAt = dockerTime(in.State.FinishedAt)
	if h := in.State.Health; h != nil && h.Status != "none" {
		c.Health = h.Status
	}
}

// dockerTime reads a time as Docker writes it, which is the zero year when
// something hasn't happened.
func dockerTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || t.Year() < 2000 {
		return time.Time{}
	}
	return t
}

// healthFromStatus reads the health from a status such as "Up 2 hours
// (healthy)", for when inspecting the container fails.
func healthFromStatus(status string) string {
	for _, h := range []string{"unhealthy", "healthy", "health: starting"} {
		if strings.Contains(status, "("+h+")") {
			return strings.TrimPrefix(h, "health: ")
		}
	}
	return ""
}

func (c *Client) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Message string `json:"message"`
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if json.Unmarshal(body, &e) == nil && e.Message != "" {
			return fmt.Errorf("docker answered %d: %s", resp.StatusCode, e.Message)
		}
		return fmt.Errorf("docker answered %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(v)
}

// A Reading is what a Collector last learned.
type Reading struct {
	// Containers are the containers, by name, as the last read that worked
	// found them. It's nil before the first.
	Containers []Container
	// At is when a read last worked, or zero.
	At time.Time
	// Err is why the latest read failed, if it did.
	Err error
}

// A Collector reads the containers every Interval, and keeps them. It's
// safe for concurrent use.
type Collector struct {
	client  *Client
	log     *slog.Logger
	changes chan struct{}

	mu      sync.Mutex
	reading Reading
}

// NewCollector returns a Collector that reads with client. Nothing is read
// until Run.
func NewCollector(client *Client, log *slog.Logger) *Collector {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Collector{client: client, log: log, changes: make(chan struct{}, 1)}
}

// Run reads the containers now and then every Interval, until ctx ends.
func (c *Collector) Run(ctx context.Context) {
	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		c.collect(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Reading returns what the collector knows now.
func (c *Collector) Reading() Reading {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.reading
	r.Containers = slices.Clone(r.Containers)
	return r
}

// Changes receives a value after each read, whether or not it worked. Only
// one goroutine should receive from it.
func (c *Collector) Changes() <-chan struct{} { return c.changes }

func (c *Collector) collect(ctx context.Context) {
	readCtx, cancel := context.WithTimeout(ctx, timeout)
	containers, err := c.client.Containers(readCtx)
	cancel()
	if ctx.Err() != nil {
		return
	}
	c.mu.Lock()
	switch {
	case err != nil:
		if c.reading.Err == nil {
			c.log.Warn("read the Docker containers", "error", err)
		}
		c.reading.Err = err
	default:
		if c.reading.Err != nil {
			c.log.Info("read the Docker containers again")
		}
		if containers == nil {
			containers = []Container{}
		}
		c.reading = Reading{Containers: containers, At: time.Now()}
	}
	c.mu.Unlock()
	select {
	case c.changes <- struct{}{}:
	default:
	}
}

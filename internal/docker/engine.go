package docker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// The Raw types are what the Engine API says, with the fields Roost reads.

// RawContainer is a container in the list.
type RawContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	Command string            `json:"Command"`
	Created int64             `json:"Created"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Labels  map[string]string `json:"Labels"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort uint16 `json:"PrivatePort"`
		PublicPort  uint16 `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
	NetworkSettings struct {
		Networks map[string]struct {
			NetworkID  string `json:"NetworkID"`
			IPAddress  string `json:"IPAddress"`
			MacAddress string `json:"MacAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
	Mounts []struct {
		Type string `json:"Type"`
		Name string `json:"Name"`
	} `json:"Mounts"`
}

// RawInspect is a container as it is inspected.
type RawInspect struct {
	ID           string   `json:"Id"`
	Name         string   `json:"Name"`
	Created      string   `json:"Created"`
	Path         string   `json:"Path"`
	Args         []string `json:"Args"`
	Image        string   `json:"Image"`
	Platform     string   `json:"Platform"`
	Driver       string   `json:"Driver"`
	RestartCount int      `json:"RestartCount"`
	State        struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		Paused     bool   `json:"Paused"`
		Restarting bool   `json:"Restarting"`
		OOMKilled  bool   `json:"OOMKilled"`
		Dead       bool   `json:"Dead"`
		Pid        int    `json:"Pid"`
		ExitCode   int    `json:"ExitCode"`
		Error      string `json:"Error"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
		Health     *struct {
			Status        string `json:"Status"`
			FailingStreak int    `json:"FailingStreak"`
			Log           []struct {
				Start    string `json:"Start"`
				End      string `json:"End"`
				ExitCode int    `json:"ExitCode"`
				Output   string `json:"Output"`
			} `json:"Log"`
		} `json:"Health"`
	} `json:"State"`
	Config struct {
		Hostname     string              `json:"Hostname"`
		User         string              `json:"User"`
		Tty          bool                `json:"Tty"`
		Env          []string            `json:"Env"`
		Cmd          []string            `json:"Cmd"`
		Entrypoint   []string            `json:"Entrypoint"`
		Image        string              `json:"Image"`
		WorkingDir   string              `json:"WorkingDir"`
		Labels       map[string]string   `json:"Labels"`
		ExposedPorts map[string]struct{} `json:"ExposedPorts"`
		Healthcheck  *struct {
			Test        []string `json:"Test"`
			Interval    int64    `json:"Interval"`
			Timeout     int64    `json:"Timeout"`
			StartPeriod int64    `json:"StartPeriod"`
			Retries     int      `json:"Retries"`
		} `json:"Healthcheck"`
	} `json:"Config"`
	HostConfig struct {
		NetworkMode   string `json:"NetworkMode"`
		RestartPolicy struct {
			Name              string `json:"Name"`
			MaximumRetryCount int    `json:"MaximumRetryCount"`
		} `json:"RestartPolicy"`
		AutoRemove        bool     `json:"AutoRemove"`
		Memory            int64    `json:"Memory"`
		MemoryReservation int64    `json:"MemoryReservation"`
		MemorySwap        int64    `json:"MemorySwap"`
		NanoCPUs          int64    `json:"NanoCpus"`
		CPUQuota          int64    `json:"CpuQuota"`
		CPUPeriod         int64    `json:"CpuPeriod"`
		CPUShares         int64    `json:"CpuShares"`
		CPUSetCPUs        string   `json:"CpusetCpus"`
		PidsLimit         *int64   `json:"PidsLimit"`
		ShmSize           int64    `json:"ShmSize"`
		Privileged        bool     `json:"Privileged"`
		ReadonlyRootfs    bool     `json:"ReadonlyRootfs"`
		CapAdd            []string `json:"CapAdd"`
		CapDrop           []string `json:"CapDrop"`
		SecurityOpt       []string `json:"SecurityOpt"`
		DNS               []string `json:"Dns"`
		ExtraHosts        []string `json:"ExtraHosts"`
		LogConfig         struct {
			Type string `json:"Type"`
		} `json:"LogConfig"`
	} `json:"HostConfig"`
	NetworkSettings struct {
		Ports    map[string][]struct{ HostIP, HostPort string } `json:"Ports"`
		Networks map[string]struct {
			NetworkID         string   `json:"NetworkID"`
			IPAddress         string   `json:"IPAddress"`
			IPPrefixLen       int      `json:"IPPrefixLen"`
			Gateway           string   `json:"Gateway"`
			MacAddress        string   `json:"MacAddress"`
			GlobalIPv6Address string   `json:"GlobalIPv6Address"`
			IPv6Gateway       string   `json:"IPv6Gateway"`
			Aliases           []string `json:"Aliases"`
			DNSNames          []string `json:"DNSNames"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
	Mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Driver      string `json:"Driver"`
		Mode        string `json:"Mode"`
		RW          bool   `json:"RW"`
		Propagation string `json:"Propagation"`
	} `json:"Mounts"`
}

// RawInfo is what the Docker daemon says of itself.
type RawInfo struct {
	Name              string   `json:"Name"`
	ServerVersion     string   `json:"ServerVersion"`
	OperatingSystem   string   `json:"OperatingSystem"`
	KernelVersion     string   `json:"KernelVersion"`
	Architecture      string   `json:"Architecture"`
	NCPU              int      `json:"NCPU"`
	MemTotal          int64    `json:"MemTotal"`
	Driver            string   `json:"Driver"`
	LoggingDriver     string   `json:"LoggingDriver"`
	CgroupVersion     string   `json:"CgroupVersion"`
	CgroupDriver      string   `json:"CgroupDriver"`
	DockerRootDir     string   `json:"DockerRootDir"`
	Containers        int      `json:"Containers"`
	ContainersRunning int      `json:"ContainersRunning"`
	ContainersPaused  int      `json:"ContainersPaused"`
	ContainersStopped int      `json:"ContainersStopped"`
	Images            int      `json:"Images"`
	Warnings          []string `json:"Warnings"`
}

// RawUsage is the total of one kind of thing in the disk usage, which newer
// daemons add next to the lists.
type RawUsage struct {
	ActiveCount int64 `json:"ActiveCount"`
	TotalCount  int64 `json:"TotalCount"`
	TotalSize   int64 `json:"TotalSize"`
	Reclaimable int64 `json:"Reclaimable"`
}

// RawDf is what the daemon says of the disk space it uses.
type RawDf struct {
	LayersSize *int64 `json:"LayersSize"`
	Images     []struct {
		ID         string `json:"Id"`
		Size       int64  `json:"Size"`
		SharedSize int64  `json:"SharedSize"`
		Containers int64  `json:"Containers"`
	} `json:"Images"`
	Containers []struct {
		ID     string `json:"Id"`
		SizeRw int64  `json:"SizeRw"`
		State  string `json:"State"`
	} `json:"Containers"`
	Volumes []struct {
		Name      string `json:"Name"`
		UsageData *struct {
			Size     int64 `json:"Size"`
			RefCount int64 `json:"RefCount"`
		} `json:"UsageData"`
	} `json:"Volumes"`
	BuildCache []struct {
		Size int64 `json:"Size"`
	} `json:"BuildCache"`
	ImageUsage      *RawUsage `json:"ImageUsage"`
	ContainerUsage  *RawUsage `json:"ContainerUsage"`
	VolumeUsage     *RawUsage `json:"VolumeUsage"`
	BuildCacheUsage *RawUsage `json:"BuildCacheUsage"`
}

// RawVolume is a volume.
type RawVolume struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Mountpoint string            `json:"Mountpoint"`
	CreatedAt  string            `json:"CreatedAt"`
	Scope      string            `json:"Scope"`
	Labels     map[string]string `json:"Labels"`
}

// RawImage is an image in the list.
type RawImage struct {
	ID          string   `json:"Id"`
	RepoTags    []string `json:"RepoTags"`
	RepoDigests []string `json:"RepoDigests"`
	Created     int64    `json:"Created"`
	Size        int64    `json:"Size"`
}

// RawImageInfo is an image as it is inspected.
type RawImageInfo struct {
	ID          string   `json:"Id"`
	RepoDigests []string `json:"RepoDigests"`
	Created     string   `json:"Created"`
	Size        int64    `json:"Size"`
}

// RawNetwork is a network.
type RawNetwork struct {
	ID         string            `json:"Id"`
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Scope      string            `json:"Scope"`
	Internal   bool              `json:"Internal"`
	Attachable bool              `json:"Attachable"`
	EnableIPv6 bool              `json:"EnableIPv6"`
	Created    string            `json:"Created"`
	Labels     map[string]string `json:"Labels"`
	IPAM       struct {
		Config []struct {
			Subnet  string `json:"Subnet"`
			Gateway string `json:"Gateway"`
		} `json:"Config"`
	} `json:"IPAM"`
}

// Top is a container's processes, as ps writes them.
type Top struct {
	Titles    []string   `json:"Titles"`
	Processes [][]string `json:"Processes"`
}

// ErrBadID is the error for a container name or ID that can't be one.
var ErrBadID = errors.New("a container's name or ID is letters, digits, underscores, dots and dashes")

// containerPath returns the path of a container's resource.
func containerPath(id, suffix string) (string, error) {
	if !ValidID(id) {
		return "", ErrBadID
	}
	return "/containers/" + url.PathEscape(id) + suffix, nil
}

// getContainer reads a resource of a container into v.
func (c *Client) getContainer(ctx context.Context, id, suffix string, query url.Values, v any) error {
	path, err := containerPath(id, suffix)
	if err != nil {
		return err
	}
	return c.getJSON(ctx, path, query, v)
}

// List returns every container, running or not, as the list says it.
func (c *Client) List(ctx context.Context) ([]RawContainer, error) {
	var out []RawContainer
	err := c.getJSON(ctx, "/containers/json", url.Values{"all": {"1"}}, &out)
	return out, err
}

// Inspect returns a container, by name or ID.
func (c *Client) Inspect(ctx context.Context, id string) (*RawInspect, error) {
	var out RawInspect
	if err := c.getContainer(ctx, id, "/json", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Info returns what the daemon says of itself.
func (c *Client) Info(ctx context.Context) (*RawInfo, error) {
	var out RawInfo
	if err := c.getJSON(ctx, "/info", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// APIVersion returns the version of the Engine API the daemon speaks.
func (c *Client) APIVersion(ctx context.Context) (string, error) {
	var out struct {
		APIVersion string `json:"ApiVersion"`
	}
	err := c.getJSON(ctx, "/version", nil, &out)
	return out.APIVersion, err
}

// dfTimeout is how long the disk usage can take: the daemon walks every
// layer and volume.
const dfTimeout = 2 * time.Minute

// Df returns the disk space the daemon uses.
func (c *Client) Df(ctx context.Context) (*RawDf, error) {
	var out RawDf
	if err := c.getJSONWithin(ctx, dfTimeout, "/system/df", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Top returns a container's processes, with ps's CPU and memory columns if
// the container's ps has them.
func (c *Client) Top(ctx context.Context, id string) (*Top, error) {
	var out Top
	err := c.getContainer(ctx, id, "/top", url.Values{"ps_args": {"aux"}}, &out)
	if e, ok := errors.AsType[*Error](err); ok && e.Status != http.StatusConflict {
		// The container's ps may not take those arguments.
		out = Top{}
		err = c.getContainer(ctx, id, "/top", nil, &out)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ImageInfo returns an image, by name or ID.
func (c *Client) ImageInfo(ctx context.Context, ref string) (*RawImageInfo, error) {
	var out RawImageInfo
	// An image's name has slashes in it that the path keeps.
	if err := c.getJSON(ctx, "/images/"+(&url.URL{Path: ref}).EscapedPath()+"/json", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Volumes returns the volumes.
func (c *Client) Volumes(ctx context.Context) ([]RawVolume, error) {
	var out struct {
		Volumes []RawVolume `json:"Volumes"`
	}
	err := c.getJSON(ctx, "/volumes", nil, &out)
	return out.Volumes, err
}

// Images returns the images.
func (c *Client) Images(ctx context.Context) ([]RawImage, error) {
	var out []RawImage
	err := c.getJSON(ctx, "/images/json", nil, &out)
	return out, err
}

// Networks returns the networks.
func (c *Client) Networks(ctx context.Context) ([]RawNetwork, error) {
	var out []RawNetwork
	err := c.getJSON(ctx, "/networks", nil, &out)
	return out, err
}

// Sample returns a container's stats at this moment, without waiting for a
// second sample, so it has no earlier sample of its own to work out rates
// from.
func (c *Client) Sample(ctx context.Context, id string) (*RawStats, error) {
	var out RawStats
	q := url.Values{"stream": {"0"}, "one-shot": {"1"}}
	if err := c.getContainerWithin(ctx, 10*time.Second, id, "/stats", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) getContainerWithin(ctx context.Context, d time.Duration, id, suffix string, query url.Values, v any) error {
	path, err := containerPath(id, suffix)
	if err != nil {
		return err
	}
	return c.getJSONWithin(ctx, d, path, query, v)
}

// StatsStream starts reading a container's stats, one JSON sample a second,
// until ctx ends or the reader is closed.
func (c *Client) StatsStream(ctx context.Context, id string) (io.ReadCloser, error) {
	path, err := containerPath(id, "/stats")
	if err != nil {
		return nil, err
	}
	resp, err := c.stream(ctx, path, url.Values{"stream": {"1"}})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// An Action is something to do to a container.
type Action string

// The actions.
const (
	Start   Action = "start"
	Stop    Action = "stop"
	Restart Action = "restart"
)

// stopGrace is how many seconds Docker waits for a container to stop on its
// own before it kills it, as `docker stop` does.
const stopGrace = 10

// actionTimeout is how long an action can take: a stop waits out its grace
// period first.
const actionTimeout = 60 * time.Second

// Do starts, stops or restarts a container. It reports whether anything
// changed: Docker answers that a container is already started or stopped.
func (c *Client) Do(ctx context.Context, id string, a Action) (changed bool, err error) {
	path, err := containerPath(id, "/"+string(a))
	if err != nil {
		return false, err
	}
	var q url.Values
	switch a {
	case Start:
	case Stop, Restart:
		q = url.Values{"t": {strconv.Itoa(stopGrace)}}
	default:
		return false, errors.New("docker: unknown action " + string(a))
	}
	status, err := c.post(ctx, actionTimeout, path, q)
	return status != http.StatusNotModified, err
}

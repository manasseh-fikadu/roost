package api

import (
	"cmp"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/manasseh-fikadu/roost/internal/docker"
)

// secretLabel matches the names of labels that may hold a credential.
var secretLabel = regexp.MustCompile(`(?i)pass|secret|token|key|pwd|auth|credential`)

// exitedStatus reads the exit code from a status such as "Exited (1) 5
// minutes ago".
var exitedStatus = regexp.MustCompile(`^Exited \((-?\d+)\)`)

func dockerStats(s docker.Stats) *DockerStats {
	return &DockerStats{
		TS: s.At.UnixMilli(), CPU: s.CPU, MemUsed: s.MemUsed, MemLimit: s.MemLimit,
		NetRx: s.NetRx, NetTx: s.NetTx, NetRxTotal: s.NetRxTotal, NetTxTotal: s.NetTxTotal,
		BlkRead: s.BlkRead, BlkWrite: s.BlkWrite, BlkReadRate: s.BlkReadRate, BlkWriteRate: s.BlkWriteRate,
		Pids: s.Pids, CPUs: s.CPUs,
	}
}

// containerPorts converts a container's ports. A port is published on a
// particular address only if it says so: every address is the usual.
func containerPorts(ports []docker.Port) []ContainerPort {
	out := make([]ContainerPort, len(ports))
	for i, p := range ports {
		out[i] = ContainerPort{Private: int(p.Private), Public: int(p.Public), Type: p.Type}
		if !docker.AnyAddr(p.IP) {
			out[i].IP = p.IP
		}
	}
	return out
}

func dockerRow(e docker.Entry) DockerContainerRow {
	c, in := e.Container, e.Inspect
	row := DockerContainerRow{
		ID: c.ID, Name: c.Name(), Image: c.Image, ImageID: c.ImageID,
		Created: time.Unix(c.Created, 0), State: ContainerState(c.State), Status: c.Status,
		Health:  docker.HealthFromStatus(c.Status),
		Project: c.Labels["com.docker.compose.project"], Service: c.Labels["com.docker.compose.service"],
		Ports: containerPorts(c.PortList()), Networks: slices.Sorted(maps.Keys(c.NetworkSettings.Networks)),
		Mounts: len(c.Mounts),
	}
	if row.Networks == nil {
		row.Networks = []string{}
	}
	if in == nil {
		if m := exitedStatus.FindStringSubmatch(c.Status); m != nil {
			code, _ := strconv.Atoi(m[1])
			row.ExitCode = &code
		}
	} else {
		if in.State.Health != nil && in.State.Health.Status != "none" {
			row.Health = in.State.Health.Status
		}
		row.StartedAt, row.FinishedAt = timePtr(docker.ParseTime(in.State.StartedAt)), timePtr(docker.ParseTime(in.State.FinishedAt))
		row.RestartCount, row.OOMKilled, row.Error = in.RestartCount, in.State.OOMKilled, in.State.Error
		row.RestartPolicy = in.HostConfig.RestartPolicy.Name
		row.MemLimitSet = in.HostConfig.Memory > 0
		if !in.State.Running {
			code := in.State.ExitCode
			row.ExitCode = &code
		}
	}
	if e.Stats != nil {
		row.Stats = dockerStats(*e.Stats)
	}
	return row
}

func dockerOverview(info docker.Info, disk *docker.DiskUsage) DockerOverview {
	out := DockerOverview{
		Name: info.Name, Version: info.ServerVersion, APIVersion: info.APIVersion, OS: info.OperatingSystem,
		Kernel: info.KernelVersion, Arch: info.Architecture, CPUs: info.NCPU, MemTotal: info.MemTotal,
		StorageDriver: info.Driver, LoggingDriver: info.LoggingDriver, RootDir: info.DockerRootDir,
		Containers: DockerCounts{Total: info.Containers, Running: info.ContainersRunning, Paused: info.ContainersPaused, Stopped: info.ContainersStopped},
		Images:     info.Images, Warnings: info.Warnings,
	}
	switch {
	case info.CgroupVersion != "" && info.CgroupDriver != "":
		out.Cgroup = "v" + info.CgroupVersion + " (" + info.CgroupDriver + ")"
	case info.CgroupVersion != "":
		out.Cgroup = "v" + info.CgroupVersion
	default:
		out.Cgroup = info.CgroupDriver
	}
	if disk != nil {
		d := dockerDisk(disk)
		out.Disk = &d
	}
	return out
}

func dockerDisk(d *docker.DiskUsage) DockerDisk {
	group := func(g docker.DiskGroup) DockerDiskGroup {
		return DockerDiskGroup{Size: g.Size, Count: g.Count, Active: g.Active, Reclaimable: g.Reclaimable}
	}
	return DockerDisk{
		Images: group(d.Images), Containers: group(d.Containers), Volumes: group(d.Volumes), BuildCache: group(d.BuildCache),
		Total: d.Total, At: d.At,
	}
}

// dockerDetail converts an inspected container. image is its image, and disk
// the disk usage, which sizes its volumes. Either may be nil.
func dockerDetail(c *docker.RawInspect, image *docker.RawImageInfo, disk *docker.DiskUsage) DockerContainerDetail {
	h := c.HostConfig
	out := DockerContainerDetail{
		ID: c.ID, Name: strings.TrimPrefix(c.Name, "/"), Image: c.Config.Image, ImageID: c.Image,
		Created: docker.ParseTime(c.Created), Platform: c.Platform, Driver: c.Driver,
		State: DockerState{
			Status: c.State.Status, Running: c.State.Running, Paused: c.State.Paused, Restarting: c.State.Restarting,
			OOMKilled: c.State.OOMKilled, Dead: c.State.Dead, Pid: c.State.Pid, Error: c.State.Error,
			StartedAt: timePtr(docker.ParseTime(c.State.StartedAt)), FinishedAt: timePtr(docker.ParseTime(c.State.FinishedAt)),
		},
		RestartCount: c.RestartCount,
		Path:         c.Path, Args: c.Args, Cmd: c.Config.Cmd, Entrypoint: c.Config.Entrypoint,
		WorkingDir: c.Config.WorkingDir, User: c.Config.User, Hostname: c.Config.Hostname, TTY: c.Config.Tty,
		AutoRemove: h.AutoRemove, LogDriver: h.LogConfig.Type, NetworkMode: h.NetworkMode,
		Ports: inspectedPorts(c), Exposed: nonNil(slices.Sorted(maps.Keys(c.Config.ExposedPorts))),
		Networks: []DockerContainerNetwork{}, Mounts: []DockerMount{},
		DNS: nonNil(h.DNS), ExtraHosts: nonNil(h.ExtraHosts),
		Security: DockerSecurity{
			Privileged: h.Privileged, ReadonlyRootfs: h.ReadonlyRootfs,
			CapAdd: nonNil(h.CapAdd), CapDrop: nonNil(h.CapDrop), SecurityOpt: nonNil(h.SecurityOpt),
		},
		Resources: DockerResources{
			Memory: h.Memory, MemoryReservation: h.MemoryReservation, MemorySwap: h.MemorySwap,
			CPUShares: h.CPUShares, Cpuset: h.CPUSetCPUs, ShmSize: h.ShmSize,
		},
	}
	if !c.State.Running {
		code := c.State.ExitCode
		out.State.ExitCode = &code
	}
	if image != nil {
		out.ImageSize = &image.Size
		out.ImageCreated = timePtr(docker.ParseTime(image.Created))
		if len(image.RepoDigests) > 0 {
			out.ImageDigest = image.RepoDigests[0]
		}
	}
	switch {
	case h.NanoCPUs > 0:
		out.Resources.CPUs = float64(h.NanoCPUs) / 1e9
	case h.CPUQuota > 0 && h.CPUPeriod > 0:
		out.Resources.CPUs = float64(h.CPUQuota) / float64(h.CPUPeriod)
	}
	if h.PidsLimit != nil && *h.PidsLimit > 0 {
		out.Resources.PidsLimit = *h.PidsLimit
	}
	if rp := h.RestartPolicy; rp.Name != "" {
		out.RestartPolicy = &DockerRestartPolicy{Name: rp.Name, MaxRetries: rp.MaximumRetryCount}
	}
	if hl := c.State.Health; hl != nil {
		out.Health = &DockerHealth{Status: hl.Status, FailingStreak: hl.FailingStreak, Log: make([]DockerHealthResult, len(hl.Log))}
		for i, r := range hl.Log {
			out.Health.Log[i] = DockerHealthResult{Start: docker.ParseTime(r.Start), End: docker.ParseTime(r.End), ExitCode: r.ExitCode, Output: r.Output}
		}
	}
	if hc := c.Config.Healthcheck; hc != nil && len(hc.Test) > 0 && hc.Test[0] != "NONE" {
		out.Healthcheck = &DockerHealthcheck{
			Test: hc.Test, Interval: hc.Interval / int64(time.Millisecond), Timeout: hc.Timeout / int64(time.Millisecond),
			StartPeriod: hc.StartPeriod / int64(time.Millisecond), Retries: hc.Retries,
		}
	}
	for _, line := range c.Config.Env {
		k, v, _ := strings.Cut(line, "=")
		out.Env = append(out.Env, DockerEnvVar{Key: k, Value: v})
	}
	out.Labels, out.SecretLabels = splitLabels(c.Config.Labels)
	out.Project, out.Service = c.Config.Labels["com.docker.compose.project"], c.Config.Labels["com.docker.compose.service"]
	out.ComposeDir = c.Config.Labels["com.docker.compose.project.working_dir"]
	out.ComposeFiles = c.Config.Labels["com.docker.compose.project.config_files"]
	for _, name := range slices.Sorted(maps.Keys(c.NetworkSettings.Networks)) {
		n := c.NetworkSettings.Networks[name]
		out.Networks = append(out.Networks, DockerContainerNetwork{
			Name: name, ID: n.NetworkID, IP: n.IPAddress, PrefixLen: n.IPPrefixLen, Gateway: n.Gateway, MAC: n.MacAddress,
			IPv6: n.GlobalIPv6Address, IPv6Gateway: n.IPv6Gateway,
			Aliases: uniqueStrings(n.Aliases), DNSNames: uniqueStrings(n.DNSNames),
		})
	}
	for _, m := range c.Mounts {
		mount := DockerMount{
			Type: m.Type, Name: m.Name, Source: m.Source, Destination: m.Destination, Mode: m.Mode,
			RW: m.RW, Propagation: m.Propagation, Driver: m.Driver,
		}
		if u, ok := disk.VolumeUse(m.Name); ok && m.Name != "" {
			mount.Size = &u.Size
		}
		out.Mounts = append(out.Mounts, mount)
	}
	return out
}

// inspectedPorts returns the ports of an inspected container: the ones it
// listens on, and the host ports and addresses they're published on.
func inspectedPorts(c *docker.RawInspect) []ContainerPort {
	out := []ContainerPort{}
	for spec, bindings := range c.NetworkSettings.Ports {
		port, typ, _ := strings.Cut(spec, "/")
		private, _ := strconv.Atoi(port)
		if typ == "" {
			typ = "tcp"
		}
		if len(bindings) == 0 {
			out = append(out, ContainerPort{Private: private, Type: typ})
		}
		for _, b := range bindings {
			p := ContainerPort{Private: private, Type: typ}
			p.Public, _ = strconv.Atoi(b.HostPort)
			if !docker.AnyAddr(b.HostIP) {
				p.IP = b.HostIP
			}
			// A port published on every address is bound for IPv4 and IPv6.
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	slices.SortFunc(out, func(a, b ContainerPort) int {
		return cmp.Or(cmp.Compare(a.Private, b.Private), cmp.Compare(a.Public, b.Public), cmp.Compare(a.Type, b.Type), cmp.Compare(a.IP, b.IP))
	})
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// uniqueStrings returns s without repeats, in order, and never nil.
func uniqueStrings(s []string) []string {
	out := []string{}
	for _, v := range s {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// dockerRefs returns the containers of a list that match, by name.
func dockerRefs(list []docker.RawContainer, match func(c docker.RawContainer) bool) []DockerRef {
	out := []DockerRef{}
	for _, c := range list {
		if match(c) {
			out = append(out, DockerRef{ID: c.ID, Name: c.Name(), Running: c.State == "running"})
		}
	}
	slices.SortFunc(out, func(a, b DockerRef) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func dockerVolumes(raw []docker.RawVolume, list []docker.RawContainer, disk *docker.DiskUsage) []DockerVolume {
	out := make([]DockerVolume, len(raw))
	for i, v := range raw {
		out[i] = DockerVolume{
			Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint, Scope: v.Scope,
			Created: timePtr(docker.ParseTime(v.CreatedAt)),
			Project: v.Labels["com.docker.compose.project"],
			UsedBy:  dockerRefs(list, func(c docker.RawContainer) bool { return c.UsesVolume(v.Name) }),
		}
		out[i].Labels, out[i].SecretLabels = splitLabels(v.Labels)
		if u, ok := disk.VolumeUse(v.Name); ok {
			out[i].Size = &u.Size
		}
	}
	slices.SortFunc(out, func(a, b DockerVolume) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func dockerImages(raw []docker.RawImage, list []docker.RawContainer) []DockerImage {
	out := make([]DockerImage, len(raw))
	for i, img := range raw {
		tags := []string{}
		for _, t := range img.RepoTags {
			if t != "<none>:<none>" {
				tags = append(tags, t)
			}
		}
		digests := []string{}
		for _, d := range img.RepoDigests {
			if !strings.HasPrefix(d, "<none>") {
				digests = append(digests, d)
			}
		}
		out[i] = DockerImage{
			ID: img.ID, Tags: tags, Digests: digests, Created: time.Unix(img.Created, 0), Size: img.Size,
			Dangling: len(tags) == 0,
			UsedBy:   dockerRefs(list, func(c docker.RawContainer) bool { return c.ImageID == img.ID }),
		}
	}
	slices.SortFunc(out, func(a, b DockerImage) int { return b.Created.Compare(a.Created) })
	return out
}

func dockerNetworks(raw []docker.RawNetwork, list []docker.RawContainer) []DockerNetwork {
	out := make([]DockerNetwork, len(raw))
	for i, n := range raw {
		out[i] = DockerNetwork{
			ID: n.ID, Name: n.Name, Driver: n.Driver, Scope: n.Scope, Internal: n.Internal, Attachable: n.Attachable,
			IPv6: n.EnableIPv6, Created: timePtr(docker.ParseTime(n.Created)), Subnets: []DockerSubnet{},
			Containers: []DockerNetworkContainer{},
		}
		if n.Labels["com.docker.compose.network"] != "" {
			out[i].Project = n.Labels["com.docker.compose.project"]
		}
		for _, c := range n.IPAM.Config {
			if c.Subnet != "" {
				out[i].Subnets = append(out[i].Subnets, DockerSubnet{Subnet: c.Subnet, Gateway: c.Gateway})
			}
		}
		for _, c := range list {
			for name, ep := range c.NetworkSettings.Networks {
				if ep.NetworkID == n.ID || (ep.NetworkID == "" && name == n.Name) {
					out[i].Containers = append(out[i].Containers, DockerNetworkContainer{
						ID: c.ID, Name: c.Name(), Running: c.State == "running", IP: ep.IPAddress, MAC: ep.MacAddress,
					})
					break
				}
			}
		}
		slices.SortFunc(out[i].Containers, func(a, b DockerNetworkContainer) int { return cmp.Compare(a.Name, b.Name) })
	}
	slices.SortFunc(out, func(a, b DockerNetwork) int {
		return cmp.Or(cmp.Compare(len(b.Containers), len(a.Containers)), cmp.Compare(a.Name, b.Name))
	})
	return out
}

// splitLabels returns the labels that are safe to show to anyone, and those
// whose names look like they hold a credential, which only admins get.
func splitLabels(in map[string]string) (plain, secret map[string]string) {
	plain, secret = map[string]string{}, map[string]string{}
	for k, v := range in {
		if secretLabel.MatchString(k) {
			secret[k] = v
		} else {
			plain[k] = v
		}
	}
	return plain, secret
}

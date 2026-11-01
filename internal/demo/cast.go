package demo

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/manasseh-fikadu/roost/internal/usage"
)

// tailnetName and dnsSuffix are the made-up tailnet's name, and the end of
// its machines' names.
const (
	tailnetName = "example.com"
	dnsSuffix   = "tail-demo.ts.net"
)

// The people whose machines are on the tailnet. Alex is the one who looks at
// Roost, so every visitor is Alex.
const (
	alex = "alex@example.com"
	sam  = "sam@example.com"
)

// A machine is one made-up device: what it is, how busy it is and where it
// sits on the tailnet.
type machine struct {
	// index is where the machine is in the tailnet. The first is Roost's own.
	index int
	// id is the node's ID, and ip4 and ip6 its tailnet addresses.
	id          string
	ip4, ip6    netip.Addr
	host        string
	os          string
	osVersion   string
	kernel      string
	model, arch string
	// owner is who it belongs to, or "" if it has tags.
	owner string
	tags  []string

	cores             int
	memGB, diskGB     float64
	swapGB            float64
	cpu, cpuSwing     float64 // percent
	mem, memSwing     float64
	disk, diskPerDay  float64
	rx, tx            float64 // bytes per second at a busy time
	temp              float64 // degrees at rest, or 0 if it has no sensor
	peak              float64 // the hour (UTC) it is busiest
	battery           bool
	bursty            bool
	uptime            time.Duration
	rtt               time.Duration
	relay             string
	direct            bool
	endpoint          netip.AddrPort
	joined            time.Duration
	keyIn             time.Duration // how long until its key expires, or 0
	offlineFor        time.Duration // how long ago it left, or 0 if it's on
	exitNode          bool
	tailscaleSSH      bool
	agent, ssh, hasDk bool
	// ignoreOffline marks a machine whose absence isn't worth an alert.
	ignoreOffline bool
	// name, kind and notes are what an admin set for it.
	name, kind, notes string

	containers []container
	workloads  []workload
	incidents  []incident
}

// A workload is something that uses a machine's CPU and memory.
type workload struct {
	kind  usage.Kind
	name  string
	cpu   float64 // its share of what the machine uses, from 0 to 1
	memMB float64
	procs int
	top   []string
}

// An incident is a stretch of time when a machine strains. from and to are
// measured from when the demo started, so a negative from is in the past and
// a to above zero is still going on.
type incident struct {
	from, to time.Duration
	// cpu, mem and disk are the percentage points it adds at its worst.
	cpu, mem, disk float64
	// ramp makes it build up over its span, like a leak, instead of
	// being there all at once.
	ramp bool
}

// A container is a made-up Docker container.
type container struct {
	name, image string
	project     string
	ports       []port
	volumes     []string
	env         []string
	// cpu is the share of one CPU it uses on average, in percent, and memMB
	// the memory.
	cpu, memMB float64
	// state is running, exited or restarting, and health healthy, unhealthy
	// or "" for one with no health check.
	state, health string
	exitCode      int
	oom           bool
	restarts      int
	started       time.Duration // how long ago it started
	restart       string
}

type port struct {
	private, public uint16
	ip              string
}

func srv(name, image, project string, cpu, memMB float64, ports ...port) container {
	return container{
		name: name, image: image, project: project, cpu: cpu, memMB: memMB, ports: ports,
		state: "running", restart: "unless-stopped", started: -time.Hour,
	}
}

func (c container) healthy() container   { c.health = "healthy"; return c }
func (c container) unhealthy() container { c.health = "unhealthy"; return c }
func (c container) up(d time.Duration) container {
	c.started = d
	return c
}
func (c container) volume(names ...string) container { c.volumes = names; return c }
func (c container) crashed(code int, oom bool, restarts int) container {
	c.state, c.exitCode, c.oom, c.restarts = "exited", code, oom, restarts
	return c
}

func base(name string, cpu, memMB float64) workload {
	return workload{kind: usage.KindService, name: name, cpu: cpu, memMB: memMB, procs: 1}
}

func app(name string, cpu, memMB float64, procs int, top ...string) workload {
	return workload{kind: usage.KindApp, name: name, cpu: cpu, memMB: memMB, procs: procs, top: top}
}

func svc(name string, cpu, memMB float64, procs int, top ...string) workload {
	return workload{kind: usage.KindService, name: name, cpu: cpu, memMB: memMB, procs: procs, top: top}
}

func user(name string, cpu, memMB float64, procs int, top ...string) workload {
	return workload{kind: usage.KindUserService, name: name, cpu: cpu, memMB: memMB, procs: procs, top: top}
}

// The first machines of every demo. A bigger demo adds generated ones.
var baseCast = []machine{
	{
		host: "roost", os: "linux", osVersion: "Debian GNU/Linux 13 (trixie)", kernel: "6.12.43+deb13-amd64",
		model: "Hetzner CX22", arch: "x86_64", owner: alex,
		cores: 2, memGB: 3.7, swapGB: 2, diskGB: 38, cpu: 6, cpuSwing: 5, mem: 41, memSwing: 4, disk: 44, diskPerDay: 0.08,
		rx: 180e3, tx: 260e3, peak: 15, uptime: 61 * 24 * time.Hour, rtt: 0, relay: "fra", direct: true,
		joined: 214 * 24 * time.Hour, hasDk: true, name: "Roost",
		containers: []container{
			srv("roost", "ghcr.io/manasseh-fikadu/roost:latest", "", 3, 96).healthy().up(-61 * 24 * time.Hour).volume("roost_data"),
			srv("caddy", "caddy:2.10", "edge", 0.4, 38, port{80, 80, "0.0.0.0"}, port{443, 443, "0.0.0.0"}).up(-61 * 24 * time.Hour).volume("caddy_data"),
			srv("watchtower", "containrrr/watchtower:1.7", "", 0.1, 24).up(-9 * 24 * time.Hour),
		},
		workloads: []workload{
			svc("roost.service", 0.5, 110, 1, "roost serve"), svc("tailscaled.service", 0.2, 60, 1, "tailscaled"),
			svc("docker.service", 0.1, 90, 1, "dockerd"), svc("ssh.service", 0.0, 8, 1, "sshd"),
			svc("systemd-journald.service", 0.05, 40, 1, "systemd-journald"),
		},
	},
	{
		host: "nas", os: "linux", osVersion: "Debian GNU/Linux 13 (trixie)", kernel: "6.12.43+deb13-amd64",
		model: "HP ProLiant MicroServer Gen10", arch: "x86_64", owner: "", tags: []string{"tag:server"},
		cores: 4, memGB: 15.5, swapGB: 4, diskGB: 7450, cpu: 14, cpuSwing: 12, mem: 58, memSwing: 5, disk: 89.6, diskPerDay: 0.3,
		rx: 4.2e6, tx: 2.6e6, temp: 41, peak: 20, uptime: 38 * 24 * time.Hour, rtt: 6 * time.Millisecond, direct: true,
		endpoint: netip.MustParseAddrPort("192.168.1.20:41641"), joined: 330 * 24 * time.Hour,
		agent: true, ssh: true, hasDk: true, name: "NAS", notes: "Media and backups. The 8 TB array is almost full: buy bigger disks.",
		containers: []container{
			srv("jellyfin", "jellyfin/jellyfin:10.10", "media", 9, 780, port{8096, 8096, "0.0.0.0"}).healthy().volume("jellyfin_config").up(-12 * 24 * time.Hour),
			srv("immich-server", "ghcr.io/immich-app/immich-server:v1.131", "photos", 6, 1450, port{2283, 2283, "0.0.0.0"}).healthy().volume("immich_upload"),
			srv("immich-db", "tensorchord/pgvecto-rs:pg16", "photos", 2, 420).healthy().volume("immich_pgdata"),
			srv("paperless", "ghcr.io/paperless-ngx/paperless-ngx:2.15", "docs", 1.4, 690, port{8000, 8000, "0.0.0.0"}).healthy().volume("paperless_data"),
			srv("syncthing", "syncthing/syncthing:1.29", "", 1.1, 180, port{8384, 8384, "127.0.0.1"}).up(-23 * 24 * time.Hour).volume("syncthing_config"),
			srv("qbittorrent", "lscr.io/linuxserver/qbittorrent:5.0", "media", 4, 520, port{8080, 8080, "0.0.0.0"}).unhealthy().up(-26 * time.Minute),
			srv("duplicati", "lscr.io/linuxserver/duplicati:2.1", "", 0, 0).crashed(137, true, 3).volume("duplicati_config"),
		},
		workloads: []workload{
			svc("smbd.service", 0.25, 160, 4, "smbd"), svc("docker.service", 0.1, 150, 1, "dockerd"),
			svc("zfs-zed.service", 0.05, 22, 1, "zed"), svc("tailscaled.service", 0.1, 60, 1, "tailscaled"),
		},
		incidents: []incident{{from: -36 * time.Hour, to: -34 * time.Hour, cpu: 70}},
	},
	{
		host: "homelab", os: "linux", osVersion: "Ubuntu 24.04.3 LTS", kernel: "6.8.0-79-generic",
		model: "Beelink EQ12", arch: "x86_64", owner: "", tags: []string{"tag:server"},
		cores: 4, memGB: 15.4, swapGB: 4, diskGB: 476, cpu: 9, cpuSwing: 6, mem: 49, memSwing: 4, disk: 52, diskPerDay: 0.1,
		rx: 650e3, tx: 420e3, temp: 52, peak: 19, uptime: 74 * 24 * time.Hour, rtt: 4 * time.Millisecond, direct: true,
		endpoint: netip.MustParseAddrPort("192.168.1.31:41641"), joined: 400 * 24 * time.Hour, tailscaleSSH: true,
		agent: true, ssh: true, hasDk: true, name: "Homelab",
		containers: []container{
			srv("homeassistant", "ghcr.io/home-assistant/home-assistant:2025.8", "", 5, 710, port{8123, 8123, "0.0.0.0"}).healthy().volume("ha_config"),
			srv("pihole", "pihole/pihole:2025.07", "dns", 1.2, 130, port{53, 53, "0.0.0.0"}, port{80, 8081, "0.0.0.0"}).healthy().volume("pihole_etc"),
			srv("vaultwarden", "vaultwarden/server:1.34", "", 0.2, 70, port{80, 8082, "127.0.0.1"}).healthy().volume("vw_data"),
			srv("mqtt", "eclipse-mosquitto:2", "iot", 0.2, 12, port{1883, 1883, "0.0.0.0"}), //nolint:misspell // the image is called that.
			srv("zigbee2mqtt", "koenkk/zigbee2mqtt:2.5", "iot", 0.6, 190),
			srv("grafana", "grafana/grafana:12.1", "monitoring", 1.4, 240, port{3000, 3000, "0.0.0.0"}).healthy().volume("grafana_data"),
		},
		workloads: []workload{
			svc("docker.service", 0.1, 160, 1, "dockerd"), svc("tailscaled.service", 0.1, 58, 1, "tailscaled"),
			svc("chrony.service", 0.01, 4, 1, "chronyd"), svc("cron.service", 0.0, 3, 1, "cron"),
		},
	},
	{
		host: "pi-garage", os: "linux", osVersion: "Debian GNU/Linux 12 (bookworm)", kernel: "6.12.25+rpt-rpi-v8",
		model: "Raspberry Pi 4 Model B Rev 1.5", arch: "aarch64", owner: alex,
		cores: 4, memGB: 3.7, swapGB: 0.5, diskGB: 29, cpu: 4, cpuSwing: 3, mem: 31, memSwing: 3, disk: 38, diskPerDay: 0.02,
		rx: 30e3, tx: 22e3, temp: 49, peak: 12, uptime: 118 * 24 * time.Hour, rtt: 19 * time.Millisecond, relay: "fra",
		joined: 280 * 24 * time.Hour, offlineFor: 3*time.Hour + 20*time.Minute, agent: true, ssh: true,
		name: "Garage Pi", notes: "Door sensor and the camera feed. Sits on the old wifi extender.",
		workloads: []workload{base("garage-door.service", 0.4, 40), base("motion.service", 0.5, 120), base("tailscaled.service", 0.1, 50)},
	},
	{
		host: "pi-kitchen", os: "linux", osVersion: "Debian GNU/Linux 13 (trixie)", kernel: "6.12.34+rpt-rpi-2712",
		model: "Raspberry Pi 5 Model B Rev 1.0", arch: "aarch64", owner: sam,
		cores: 4, memGB: 7.9, swapGB: 2, diskGB: 58, cpu: 12, cpuSwing: 8, mem: 27, memSwing: 3, disk: 21, diskPerDay: 0.03,
		rx: 120e3, tx: 40e3, temp: 55, peak: 18, uptime: 26 * 24 * time.Hour, rtt: 8 * time.Millisecond, direct: true,
		endpoint: netip.MustParseAddrPort("192.168.1.44:41641"), joined: 90 * 24 * time.Hour, agent: true, ssh: true,
		name: "Kitchen display",
		workloads: []workload{
			app("chromium", 0.7, 640, 9, "chromium"), svc("tailscaled.service", 0.05, 50, 1, "tailscaled"),
			user("pipewire.service", 0.1, 20, 1, "pipewire"),
		},
	},
	{
		host: "vps-fra", os: "linux", osVersion: "Debian GNU/Linux 13 (trixie)", kernel: "6.12.43+deb13-amd64",
		model: "Hetzner CPX11", arch: "x86_64", owner: alex, tags: nil,
		cores: 2, memGB: 1.9, swapGB: 1, diskGB: 38, cpu: 11, cpuSwing: 8, mem: 63, memSwing: 5, disk: 62, diskPerDay: 0.15,
		rx: 420e3, tx: 1.1e6, peak: 14, uptime: 140 * 24 * time.Hour, rtt: 24 * time.Millisecond, relay: "fra", direct: true,
		endpoint: netip.MustParseAddrPort("203.0.113.40:41641"), joined: 300 * 24 * time.Hour, exitNode: true,
		agent: true, ssh: true, hasDk: true, name: "Frankfurt VPS", bursty: true,
		containers: []container{
			srv("ghost", "ghost:5-alpine", "blog", 2.2, 330, port{2368, 2368, "127.0.0.1"}).healthy().volume("ghost_content"),
			srv("ghost-db", "mysql:8.4", "blog", 1.4, 410).healthy().volume("ghost_db"),
			srv("nginx", "nginx:1.28-alpine", "blog", 0.3, 18, port{80, 80, "0.0.0.0"}, port{443, 443, "0.0.0.0"}),
		},
		workloads: []workload{
			svc("docker.service", 0.1, 120, 1, "dockerd"), svc("fail2ban.service", 0.05, 30, 1, "fail2ban-server"),
			svc("tailscaled.service", 0.1, 58, 1, "tailscaled"),
		},
	},
	{
		host: "vps-nyc", os: "linux", osVersion: "Ubuntu 24.04.3 LTS", kernel: "6.8.0-79-generic",
		model: "DigitalOcean Droplet", arch: "x86_64", owner: alex,
		cores: 1, memGB: 0.96, swapGB: 1, diskGB: 24, cpu: 8, cpuSwing: 6, mem: 71, memSwing: 4, disk: 57, diskPerDay: 0.12,
		rx: 210e3, tx: 330e3, peak: 13, uptime: 22 * 24 * time.Hour, rtt: 88 * time.Millisecond, relay: "nyc",
		joined: 160 * 24 * time.Hour, agent: true, ssh: true, name: "New York VPS", bursty: true,
		workloads: []workload{
			svc("uptime-kuma.service", 0.6, 160, 1, "node"), svc("tailscaled.service", 0.1, 52, 1, "tailscaled"),
			svc("caddy.service", 0.1, 34, 1, "caddy"),
		},
		incidents: []incident{{from: -27 * time.Hour, to: -25*time.Hour - 20*time.Minute, cpu: 85}},
	},
	{
		host: "desktop", os: "linux", osVersion: "Fedora Linux 42 (Workstation Edition)", kernel: "6.15.9-201.fc42.x86_64",
		model: "Custom desktop (Ryzen 7 7700)", arch: "x86_64", owner: alex,
		cores: 16, memGB: 31, swapGB: 8, diskGB: 930, cpu: 6, cpuSwing: 10, mem: 36, memSwing: 12, disk: 64, diskPerDay: 0.2,
		rx: 800e3, tx: 200e3, temp: 44, peak: 21, uptime: 6 * 24 * time.Hour, rtt: 3 * time.Millisecond, direct: true,
		endpoint: netip.MustParseAddrPort("192.168.1.12:41641"), joined: 520 * 24 * time.Hour, agent: true, ssh: true, hasDk: true,
		name: "Desktop", kind: "desktop", bursty: true,
		containers: []container{
			srv("postgres-dev", "postgres:17", "dev", 1, 180, port{5432, 5432, "127.0.0.1"}).healthy().volume("pgdev_data"),
			srv("redis-dev", "redis:8-alpine", "dev", 0.3, 14, port{6379, 6379, "127.0.0.1"}),
		},
		workloads: []workload{
			app("firefox", 0.35, 2400, 14, "firefox"), app("code", 0.2, 1500, 9, "code"),
			user("pipewire.service", 0.05, 30, 1, "pipewire"), svc("docker.service", 0.05, 200, 1, "dockerd"),
		},
		incidents: []incident{{from: -5 * time.Hour, to: -4*time.Hour - 30*time.Minute, cpu: 62}},
	},
	{
		host: "macbook", os: "macOS", osVersion: "macOS 15.6.1", kernel: "Darwin 24.6.0",
		model: "MacBook Pro (14-inch, 2023)", arch: "arm64", owner: alex,
		cores: 10, memGB: 16, swapGB: 4, diskGB: 926, cpu: 11, cpuSwing: 14, mem: 68, memSwing: 8, disk: 71, diskPerDay: 0.2,
		rx: 1.6e6, tx: 350e3, temp: 46, peak: 13, battery: true, uptime: 9 * 24 * time.Hour, rtt: 5 * time.Millisecond, direct: true,
		endpoint: netip.MustParseAddrPort("192.168.1.58:41641"), joined: 460 * 24 * time.Hour, agent: true, ssh: true,
		name: "MacBook Pro", bursty: true,
		workloads: []workload{
			app("Safari", 0.3, 1900, 12, "Safari Web Content"), app("Slack", 0.2, 900, 6, "Slack Helper"),
			app("Terminal", 0.1, 140, 3, "zsh"), app("Xcode", 0.15, 1800, 4, "Xcode"), user("Dock", 0.05, 80, 1, "Dock"),
		},
	},
	{
		host: "work-mac", os: "macOS", osVersion: "macOS 15.5", kernel: "Darwin 24.5.0",
		model: "MacBook Air (M2, 2022)", arch: "arm64", owner: alex,
		cores: 8, memGB: 16, swapGB: 3, diskGB: 460, cpu: 8, cpuSwing: 10, mem: 61, memSwing: 7, disk: 58, diskPerDay: 0.1,
		rx: 900e3, tx: 180e3, temp: 39, peak: 11, battery: true, uptime: 3 * 24 * time.Hour, rtt: 71 * time.Millisecond, relay: "ams",
		joined: 200 * 24 * time.Hour, keyIn: 6*24*time.Hour + 3*time.Hour, agent: true, ssh: true, name: "Work laptop",
		workloads: []workload{app("Google Chrome", 0.4, 2100, 20, "Google Chrome Helper"), app("zoom.us", 0.2, 600, 4, "zoom.us"), app("Mail", 0.05, 300, 2, "Mail")},
	},
	{
		host: "iphone", os: "iOS", osVersion: "iOS 18.6", model: "iPhone 15 Pro", arch: "arm64", owner: alex,
		cores: 6, memGB: 8, diskGB: 256, cpu: 0, mem: 0, disk: 0, rtt: 41 * time.Millisecond, relay: "fra", battery: true,
		uptime: 12 * 24 * time.Hour, joined: 340 * 24 * time.Hour, name: "iPhone",
	},
	{
		host: "ipad", os: "iOS", osVersion: "iPadOS 18.6", model: "iPad Air (M2)", arch: "arm64", owner: sam,
		cores: 8, memGB: 8, diskGB: 256, rtt: 0, relay: "fra", battery: true, uptime: 30 * 24 * time.Hour,
		joined: 150 * 24 * time.Hour, offlineFor: 26 * time.Hour, ignoreOffline: true, name: "Sam's iPad",
	},
}

// template is a kind of machine that a big demo makes many of.
type template struct {
	prefix      string
	os          string
	osVersion   string
	kernel      string
	model, arch string
	cores       int
	memGB       float64
	diskGB      float64
	cpu, swing  float64
	mem         float64
	disk        float64
	rx, tx      float64
	temp        float64
	battery     bool
	agent       bool
	docker      bool
	ssh         bool
	rtt         time.Duration
	relay       string
}

var templates = []template{
	{
		prefix: "srv", os: "linux", osVersion: "Debian GNU/Linux 13 (trixie)", kernel: "6.12.43+deb13-amd64", model: "Dell PowerEdge R250", arch: "x86_64",
		cores: 8, memGB: 31, diskGB: 1800, cpu: 18, swing: 14, mem: 55, disk: 48, rx: 2e6, tx: 1.5e6, temp: 47, agent: true, docker: true, ssh: true,
		rtt: 9 * time.Millisecond,
	},
	{
		prefix: "vm", os: "linux", osVersion: "Ubuntu 24.04.3 LTS", kernel: "6.8.0-79-generic", model: "KVM virtual machine", arch: "x86_64",
		cores: 2, memGB: 3.8, diskGB: 60, cpu: 9, swing: 8, mem: 47, disk: 41, rx: 250e3, tx: 190e3, agent: true, ssh: true, rtt: 22 * time.Millisecond,
	},
	{
		prefix: "vps", os: "linux", osVersion: "Debian GNU/Linux 12 (bookworm)", kernel: "6.1.0-37-amd64", model: "Hetzner CX22", arch: "x86_64",
		cores: 2, memGB: 3.7, diskGB: 38, cpu: 12, swing: 9, mem: 52, disk: 39, rx: 320e3, tx: 640e3, agent: true, docker: true, ssh: true,
		rtt: 31 * time.Millisecond, relay: "fra",
	},
	{
		prefix: "pi", os: "linux", osVersion: "Debian GNU/Linux 13 (trixie)", kernel: "6.12.34+rpt-rpi-v8", model: "Raspberry Pi 4 Model B Rev 1.4", arch: "aarch64",
		cores: 4, memGB: 3.7, diskGB: 29, cpu: 5, swing: 4, mem: 28, disk: 33, rx: 40e3, tx: 25e3, temp: 52, agent: true, ssh: true, rtt: 12 * time.Millisecond,
	},
	{
		prefix: "laptop", os: "macOS", osVersion: "macOS 15.6.1", kernel: "Darwin 24.6.0", model: "MacBook Pro (16-inch, 2023)", arch: "arm64",
		cores: 12, memGB: 32, diskGB: 926, cpu: 10, swing: 12, mem: 64, disk: 66, rx: 1.2e6, tx: 300e3, temp: 42, battery: true, agent: true, ssh: true,
		rtt: 14 * time.Millisecond, relay: "ams",
	},
	{
		prefix: "desktop", os: "linux", osVersion: "Fedora Linux 42 (Workstation Edition)", kernel: "6.15.9-201.fc42.x86_64", model: "Custom desktop", arch: "x86_64",
		cores: 12, memGB: 31, diskGB: 930, cpu: 7, swing: 11, mem: 38, disk: 59, rx: 700e3, tx: 160e3, temp: 43, agent: true, ssh: true, rtt: 6 * time.Millisecond,
	},
	{
		prefix: "phone", os: "android", osVersion: "Android 15", model: "Pixel 9", arch: "arm64", cores: 8, memGB: 12, diskGB: 128,
		battery: true, rtt: 38 * time.Millisecond, relay: "fra",
	},
}

// generated returns machine number i of a demo with more machines than the
// base cast, made from a template and seed.
func generated(i int, seed uint64) machine {
	pick := func(k int64) float64 { return unit(seed, int64(i)*31+k) }
	t := templates[int(pick(0)*float64(len(templates)))%len(templates)]
	// Now and then a machine is offline, so a big demo has some to look at.
	m := machine{
		host: fmt.Sprintf("%s-%03d", t.prefix, i), os: t.os, osVersion: t.osVersion, kernel: t.kernel, model: t.model, arch: t.arch,
		cores: t.cores, memGB: t.memGB, swapGB: t.memGB / 4, diskGB: t.diskGB,
		cpu: t.cpu * (0.5 + pick(1)), cpuSwing: t.swing * (0.5 + pick(2)),
		mem: clamp(t.mem*(0.7+pick(3)*0.6), 5, 92), memSwing: 4,
		disk: clamp(t.disk*(0.6+pick(4)*0.8), 5, 93), diskPerDay: pick(5) * 0.2,
		rx: t.rx * (0.4 + pick(6)*1.2), tx: t.tx * (0.4 + pick(7)*1.2), temp: t.temp, peak: 10 + pick(8)*10,
		battery: t.battery, uptime: time.Duration(1+int(pick(9)*90)) * 24 * time.Hour, bursty: pick(10) < 0.3,
		rtt: time.Duration(float64(t.rtt) * (0.5 + pick(11)*1.5)), relay: t.relay, direct: t.relay == "" || pick(12) < 0.5,
		joined: time.Duration(10+int(pick(13)*400)) * 24 * time.Hour,
		agent:  t.agent && pick(14) < 0.85, ssh: t.ssh && pick(15) < 0.9, hasDk: t.docker,
	}
	if i%2 == 0 {
		m.owner = alex
	} else {
		m.owner = sam
	}
	if t.prefix == "srv" || t.prefix == "vm" || t.prefix == "vps" {
		m.owner, m.tags = "", []string{"tag:server"}
	}
	if pick(16) < 0.08 {
		m.offlineFor = time.Duration(20+int(pick(17)*3000)) * time.Minute
	}
	if m.hasDk {
		m.containers = []container{
			srv("app", "ghcr.io/example/app:1."+strconv.Itoa(i%9), "app", 2+pick(18)*6, 180+pick(19)*400, port{8080, 8080, "0.0.0.0"}).healthy(),
			srv("db", "postgres:17", "app", 1+pick(20)*3, 220+pick(21)*300).healthy().volume("db_data"),
			srv("cache", "redis:8-alpine", "app", 0.3, 20),
		}
	}
	m.workloads = []workload{svc("tailscaled.service", 0.1, 55, 1, "tailscaled"), svc("ssh.service", 0.01, 8, 1, "sshd")}
	if t.os == "macOS" {
		m.workloads = []workload{app("Safari", 0.4, 1800, 12, "Safari Web Content"), app("Slack", 0.2, 800, 6, "Slack Helper")}
	}
	if m.agent || m.ssh {
		m.workloads = append(m.workloads, svc("app.service", 0.6, 300, 3, "app"))
	}
	return m
}

// newCast returns the machines of a demo with n of them, numbered and given
// their IDs and addresses.
func newCast(n int, seed uint64) []*machine {
	out := make([]*machine, 0, n)
	for i := range n {
		var m machine
		if i < len(baseCast) {
			m = baseCast[i]
		} else {
			m = generated(i, seed)
		}
		m.index = i
		m.id = nodeID(i, seed)
		m.ip4 = netip.AddrFrom4([4]byte{100, 101, byte(i / 250), byte(i%250 + 1)})
		m.ip6 = netip.AddrFrom16([16]byte{0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0, 11: byte(i >> 8), 12: byte(i)})
		out = append(out, &m)
	}
	return out
}

// nodeID returns the ID of the node of machine i, which looks like the ones
// Tailscale makes.
func nodeID(i int, seed uint64) string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var b strings.Builder
	b.WriteByte('n')
	x := mix(seed ^ uint64(i)*0x51ed27) //nolint:gosec // G115: i is a machine number.
	for range 10 {
		b.WriteByte(letters[x%uint64(len(letters))])
		x /= uint64(len(letters))
		if x == 0 {
			x = mix(uint64(i) + seed) //nolint:gosec // G115: i is a machine number.
		}
	}
	b.WriteString("CNTRL")
	return b.String()
}

// dns returns the machine's MagicDNS name.
func (m *machine) dns() string { return m.host + "." + dnsSuffix }

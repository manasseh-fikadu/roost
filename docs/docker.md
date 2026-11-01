# Roost in Docker

The image is `ghcr.io/manasseh-fikadu/roost`, for linux/amd64 and linux/arm64. Each release has a tag with its version, such as `0.1.0`, and one with its minor version, `0.1`. `latest` is the newest release, and a release candidate never moves `latest` or the minor tag. Pin a version in production, so that upgrades happen when you say.

The image holds the `roost` binary on [distroless](https://github.com/GoogleContainerTools/distroless) `static`, which is CA certificates, time zone data and nothing else: no shell and no package manager. It runs as the user `65532`, keeps its data in `/data`, and starts `roost serve`.

There are two ways to run it, which are the two ways Roost joins your tailnet ([Roost and Tailscale](tailscale.md) compares them).

## Embedded mode

Roost joins the tailnet as its own machine, so it works on any host with Docker. [`compose.yaml`](../compose.yaml) is all it takes:

```sh
curl -fsSLO https://raw.githubusercontent.com/manasseh-fikadu/roost/main/compose.yaml
docker compose up -d
docker compose logs roost
```

The first time, the log has a link to log Roost in to Tailscale. Open it, and Roost is at `https://roost.<your-tailnet>.ts.net`. To skip the link, create a `.env` file next to `compose.yaml` with an [auth key](https://tailscale.com/kb/1085/auth-keys), which Roost only uses the first time it starts:

```sh
TS_AUTHKEY=tskey-auth-...
```

A key for a tagged node means Roost has no owner to make its admin, so also set `ROOST_ADMINS` in `.env` ([Admins and viewers](tailscale.md#admins-and-viewers)). `ROOST_HOSTNAME` renames the node. With [Headscale](headscale.md), set `ROOST_CONTROL_URL` to your server's address in `.env` too, and use a pre-auth key for `TS_AUTHKEY`.

Roost also serves a login page inside the container, on `127.0.0.1:4700`, which isn't published, so the link in the log is the way in. If you do publish it, publish it on the host's loopback only (`127.0.0.1:4700:4700`) and set `ROOST_SETUP_LISTEN=0.0.0.0:4700`: until Roost is logged in, anyone who can open that page can add it to their own tailnet.

## Host mode

If the host already runs Tailscale, Roost can use it, and show up as the host. [`compose.host.yaml`](../compose.host.yaml) does that:

```sh
curl -fsSLO https://raw.githubusercontent.com/manasseh-fikadu/roost/main/compose.host.yaml
docker compose -f compose.host.yaml up -d
```

Roost listens on the host's tailnet IPs, so the container uses the host's network (`network_mode: host`), and it talks to tailscaled through its socket. The file mounts the directory `/var/run/tailscale`, not the socket, because tailscaled makes a new socket when it restarts, and a mount of the old one would stay dead. Tailscale on the host has to use a TUN device (the default). Roost is then at port 4700 on the host's tailnet IPs, or the port in `ROOST_PORT`.

The container runs as `65532`, which tailscaled doesn't let fetch the host's HTTPS certificate, so Roost serves plain HTTP, which WireGuard still encrypts, and says so in its log. To get HTTPS, run the container as root with `user: "0"`.

## Data

Both files keep Roost's data in the volume `roost-data`: the database, the embedded node's identity, and Roost's SSH key. A new named volume copies the image's `/data`, which belongs to `65532`. A bind mount doesn't, so give it to that user first:

```sh
mkdir data && sudo chown 65532:65532 data    # then use ./data:/data instead of roost-data:/data
```

[Upgrading](upgrading.md) covers backups and moving the volume.

## Managing the machine's containers

To see and control the host's containers, mount Docker's socket in the `volumes` of the service, and give the container's user the socket's group, since the socket is only open to `root` and `docker`:

```yaml
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    group_add:
      - "999"    # the number `stat -c %g /var/run/docker.sock` prints
```

Mounting the socket read-only (`:ro`) doesn't make the Docker API read-only. The flag only stops the container from replacing the socket file; every request through it still works. Whoever can use the API can start a container that mounts the host's `/`, so access to the socket is as good as root on the host, for Roost's admins and for anything that takes over the container. Leave the mount out if Roost shouldn't manage Docker on that machine.

## Measuring the host

Roost measures the machine it runs on from `/proc` and `/sys`. Inside a container, those are the container's view. This is what the Hub page shows for the host, from a container with no extra setup:

| | Without setup | What fixes it |
| --- | --- | --- |
| CPU, memory, load, uptime and the disk that holds `/` | The host's, since the container shares its kernel | |
| Processes | Only Roost's own | The host's `/proc` |
| Host name | The container's ID | `hostname:` |
| Network traffic | The container's, not the host's | `network_mode: host` |
| Operating system | `Distroless` | The host's `/etc/os-release` |
| Names of the processes' users | Numbers | The host's `/etc/passwd` |

Mounting the host's `/proc` over the container's isn't allowed, so mount them somewhere else, read-only, and tell Roost where with `ROOST_HOST_ROOT`:

```yaml
services:
  roost:
    hostname: my-server              # the host's name
    network_mode: host               # for the host's network traffic; host mode already has it
    environment:
      ROOST_HOST_ROOT: /host
    volumes:
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - /etc/os-release:/host/etc/os-release:ro
      - /etc/passwd:/host/etc/passwd:ro
```

Roost then reads `/host/proc`, `/host/sys` and `/host/etc`. It only reads them, and the mounts let it read what any user on the host can read. For the host's processes alone, there is a shorter way: `pid: host` shows them in the container's own `/proc`, with no mounts and no `ROOST_HOST_ROOT`.

The alternative is to leave the container alone and run [the agent](../agent) on the host. In embedded mode, the host is a device like any other, so it reports its metrics to Roost through the agent.

## Health

The image checks `roost healthcheck` every 30 seconds, which asks the login page inside the container whether Roost is up. Roost counts as healthy while it waits to be logged in, since restarting it wouldn't help. The check doesn't test the tailnet. Host mode has no login page, so `compose.host.yaml` turns the check off.

## Running Roost's commands

Everything the binary does works in the container, with `/data` as the data directory:

```sh
docker compose exec roost roost admin list
docker compose exec roost roost admin add alice@example.com
docker compose exec roost roost backup - > roost-backup.tar.gz
```

## Upgrading

```sh
docker compose pull
docker compose up -d
```

The new version migrates the database when it starts. Save a backup first, as [Upgrading](upgrading.md) shows, and to go back, pin the old tag and restore it.

## Building the image yourself

The Dockerfile only copies in binaries that GoReleaser built. To build the same image from a checkout, with the web app and every release archive:

```sh
goreleaser release --snapshot --clean --skip=publish
```

It tags the images `ghcr.io/manasseh-fikadu/roost:<version>-amd64` and `-arm64` on your machine, and pushes nothing.

# Installing Roost

Roost is one binary with its web app inside. Pick the way that fits the machine it will run on:

| Where | How |
| --- | --- |
| A Linux server with systemd | [The install script](#the-install-script) |
| Docker | [Roost in Docker](docker.md) |
| macOS, or Linux without systemd | [A release archive](#release-archives) |
| Anywhere with Go and Bun | [Build from source](../README.md#build-from-source) |

Whichever you pick, Roost has no password and serves the app only on your tailnet, so the next step is logging it in to Tailscale. [Roost and Tailscale](tailscale.md) explains the two ways it joins your tailnet, who becomes its first admin and how a tailnet policy has to look.

## The install script

On a Linux server with systemd, as root:

```sh
curl -fsSL https://raw.githubusercontent.com/manasseh-fikadu/roost/main/install.sh | sudo bash
```

The script downloads the latest release for the machine's architecture (amd64 or arm64), checks it against the release's `checksums.txt`, and sets up:

| What | Where |
| --- | --- |
| The binary | `/usr/local/bin/roost` |
| A system user and group with no login | `roost` |
| The data directory: the database, Roost's Tailscale node and its SSH key | `/var/lib/roost`, readable only by `roost` |
| Settings, as environment variables | `/etc/roost/roost.env`, readable only by root |
| The service, which runs as `roost` with no privileges | `/etc/systemd/system/roost.service` |

Then it starts the service. In the default mode, Roost joins the tailnet as a new machine called `roost`, which needs a login the first time: open <http://127.0.0.1:4700> on the same machine, or use the link in the log.

```sh
journalctl -u roost -n 20
```

Settings go in the environment of the script. Put them after `sudo`, which drops any that come before it:

```sh
curl -fsSL https://raw.githubusercontent.com/manasseh-fikadu/roost/main/install.sh | sudo TS_AUTHKEY=tskey-auth-... ROOST_ADMINS=alice@example.com bash
```

| Variable | What it does |
| --- | --- |
| `ROOST_VERSION` | The release to install, such as `v0.1.0`. The default is the latest. |
| `ROOST_TAILSCALE` | `embedded` (the default) or `host`. |
| `ROOST_HOSTNAME` | The name of Roost's node on the tailnet in embedded mode. |
| `ROOST_CONTROL_URL` | The address of your [Headscale](headscale.md) server, to log Roost's node in to it instead of to Tailscale, in embedded mode. |
| `ROOST_ADMINS` | Logins to make Roost's first admins, separated by commas. |
| `TS_AUTHKEY` | A Tailscale [auth key](https://tailscale.com/kb/1085/auth-keys), or a Headscale pre-auth key, so Roost joins the tailnet without a login. |

The script saves these in `/etc/roost/roost.env` the first time only. After that, edit the file, and run `sudo systemctl restart roost`. [Roost and Tailscale](tailscale.md) says what each one means. `TS_AUTHKEY` is only used while Roost has no node yet, so you can delete it from the file once Roost is on the tailnet.

### Running it

```sh
sudo systemctl status roost       # is it running
journalctl -u roost -f            # the log
sudo systemctl restart roost
```

`roost admin` and `roost backup` work on the data directory, so run them as the `roost` user and tell them where it is:

```sh
sudo -u roost env ROOST_DATA_DIR=/var/lib/roost roost admin list
sudo -u roost env ROOST_DATA_DIR=/var/lib/roost roost backup /var/lib/roost/backups/roost.tar.gz
```

To upgrade, run the install command again. [Upgrading](upgrading.md) covers that, backups and rolling back.

### Using the machine's Tailscale

If the machine already runs Tailscale, Roost can use it instead of joining as a second node:

```sh
curl -fsSL https://raw.githubusercontent.com/manasseh-fikadu/roost/main/install.sh | sudo ROOST_TAILSCALE=host bash
```

Roost then listens on the machine's tailnet IPs, on port 4700. For HTTPS, tailscaled has to let the `roost` user fetch the machine's certificate, so make `roost` the Tailscale operator with `sudo tailscale set --operator=roost`. Otherwise Roost serves plain HTTP, which WireGuard still encrypts. See [Host mode](tailscale.md#host-mode).

### Docker on the same machine

To manage this machine's containers from Roost, give the `roost` user access to Docker's socket and restart the service:

```sh
sudo usermod -aG docker roost
sudo systemctl restart roost
```

Membership in the `docker` group is the same as root on the machine, since anyone who can use the Docker API can start a container that owns it, and Roost's admins can use it. Only do this on a machine where that is acceptable.

### Removing it

```sh
sudo systemctl disable --now roost
sudo rm /etc/systemd/system/roost.service /usr/local/bin/roost
sudo systemctl daemon-reload
```

That leaves the data in place, so installing again picks up where you left off. To delete it too, which is the database, the node and the SSH key:

```sh
sudo rm -rf /var/lib/roost /etc/roost
sudo userdel roost
```

In embedded mode, Roost's node stays in your tailnet's [machine list](https://login.tailscale.com/admin/machines) until you remove it there.

## Release archives

Every [release](https://github.com/manasseh-fikadu/roost/releases) has an archive for Linux and for macOS, each for amd64 and arm64, named `roost_<version>_<os>_<arch>.tar.gz`, and a `checksums.txt`. Check the one you download, and run it:

```sh
sha256sum --check --ignore-missing checksums.txt    # on macOS: shasum -a 256 --check --ignore-missing checksums.txt
tar -xzf roost_0.1.0_linux_amd64.tar.gz
./roost serve
```

Nothing keeps it running for you, so use your system's service manager, such as a launchd job on macOS. Roost measures the machine it runs on only on Linux. On macOS it still manages the tailnet, and other machines report their metrics with [the agent](../agent).

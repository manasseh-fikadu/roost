# Installing Bield

Bield is one binary with its web app inside. Pick the way that fits the machine it will run on:

| Where | How |
| --- | --- |
| A Linux server with systemd | [The install script](#the-install-script) |
| Docker | [Bield in Docker](docker.md) |
| macOS, or Linux without systemd | [A release archive](#release-archives) |
| Anywhere with Go and Bun | [Build from source](../README.md#build-from-source) |

Whichever you pick, Bield has no password and serves the app only on your tailnet, so the next step is logging it in to Tailscale. [Bield and Tailscale](tailscale.md) explains the two ways it joins your tailnet, who becomes its first admin and how a tailnet policy has to look.

## The install script

On a Linux server with systemd, as root:

```sh
curl -fsSL https://raw.githubusercontent.com/manasseh-fikadu/bield/main/install.sh | sudo bash
```

The script downloads the latest release for the machine's architecture (amd64 or arm64), checks it against the release's `checksums.txt`, and sets up:

| What | Where |
| --- | --- |
| The binary | `/usr/local/bin/bield` |
| A system user and group with no login | `bield` |
| The data directory: the database, Bield's Tailscale node and its SSH key | `/var/lib/bield`, readable only by `bield` |
| Settings, as environment variables | `/etc/bield/bield.env`, readable only by root |
| The service, which runs as `bield` with no privileges | `/etc/systemd/system/bield.service` |

Then it starts the service. In the default mode, Bield joins the tailnet as a new machine called `bield`, which needs a login the first time: open <http://127.0.0.1:4700> on the same machine, or use the link in the log.

```sh
journalctl -u bield -n 20
```

Settings go in the environment of the script. Put them after `sudo`, which drops any that come before it:

```sh
curl -fsSL https://raw.githubusercontent.com/manasseh-fikadu/bield/main/install.sh | sudo TS_AUTHKEY=tskey-auth-... BIELD_ADMINS=alice@example.com bash
```

| Variable | What it does |
| --- | --- |
| `BIELD_VERSION` | The release to install, such as `v0.1.0`. The default is the latest. |
| `BIELD_TAILSCALE` | `embedded` (the default) or `host`. |
| `BIELD_HOSTNAME` | The name of Bield's node on the tailnet in embedded mode. |
| `BIELD_CONTROL_URL` | The address of your [Headscale](headscale.md) server, to log Bield's node in to it instead of to Tailscale, in embedded mode. |
| `BIELD_ADMINS` | Logins to make Bield's first admins, separated by commas. |
| `TS_AUTHKEY` | A Tailscale [auth key](https://tailscale.com/kb/1085/auth-keys), or a Headscale pre-auth key, so Bield joins the tailnet without a login. |

The script saves these in `/etc/bield/bield.env` the first time only. After that, edit the file, and run `sudo systemctl restart bield`. [Bield and Tailscale](tailscale.md) says what each one means, and the [configuration reference](configuration.md) lists every other variable. `TS_AUTHKEY` is only used while Bield has no node yet, so you can delete it from the file once Bield is on the tailnet.

### Running it

```sh
sudo systemctl status bield       # is it running
journalctl -u bield -f            # the log
sudo systemctl restart bield
```

`bield admin` and `bield backup` work on the data directory, so run them as the `bield` user and tell them where it is:

```sh
sudo -u bield env BIELD_DATA_DIR=/var/lib/bield bield admin list
sudo -u bield env BIELD_DATA_DIR=/var/lib/bield bield backup /var/lib/bield/backups/bield.tar.gz
```

To upgrade, run the install command again. [Upgrading](upgrading.md) covers that, backups and rolling back.

### Using the machine's Tailscale

If the machine already runs Tailscale, Bield can use it instead of joining as a second node:

```sh
curl -fsSL https://raw.githubusercontent.com/manasseh-fikadu/bield/main/install.sh | sudo BIELD_TAILSCALE=host bash
```

Bield then listens on the machine's tailnet IPs, on port 4700. For HTTPS, tailscaled has to let the `bield` user fetch the machine's certificate, so make `bield` the Tailscale operator with `sudo tailscale set --operator=bield`. Otherwise Bield serves plain HTTP, which WireGuard still encrypts. See [Host mode](tailscale.md#host-mode).

### Docker on the same machine

To manage this machine's containers from Bield, give the `bield` user access to Docker's socket and restart the service:

```sh
sudo usermod -aG docker bield
sudo systemctl restart bield
```

Membership in the `docker` group is the same as root on the machine, since anyone who can use the Docker API can start a container that owns it, and Bield's admins can use it. Only do this on a machine where that is acceptable.

### Removing it

[Uninstalling Bield](uninstalling.md) covers every way of running it, and what Bield leaves on your tailnet and your other machines. For the install script:

```sh
sudo systemctl disable --now bield
sudo rm /etc/systemd/system/bield.service /usr/local/bin/bield
sudo systemctl daemon-reload
```

That leaves the data in place, so installing again picks up where you left off. To delete it too, which is the database, the node and the SSH key:

```sh
sudo rm -rf /var/lib/bield /etc/bield
sudo userdel bield
```

In embedded mode, Bield's node stays in your tailnet's [machine list](https://login.tailscale.com/admin/machines) until you remove it there.

## Release archives

Every [release](https://github.com/manasseh-fikadu/bield/releases) has an archive for Linux and for macOS, each for amd64 and arm64, named `bield_<version>_<os>_<arch>.tar.gz`, and a `checksums.txt`. Check the one you download, and run it:

```sh
sha256sum --check --ignore-missing checksums.txt    # on macOS: shasum -a 256 --check --ignore-missing checksums.txt
tar -xzf bield_0.1.0_linux_amd64.tar.gz
./bield serve
```

Nothing keeps it running for you, so use your system's service manager, such as a launchd job on macOS. Bield measures the machine it runs on only on Linux. On macOS it still manages the tailnet, and other machines report their metrics with [the agent](../agent).

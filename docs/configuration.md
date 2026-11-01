# Configuration reference

Every flag and environment variable that Bield, its install script and its agent read. What admins change in the app is on [the Settings page](settings.md).

## The bield command

```text
bield serve         Run the Bield hub
bield admin         Add, remove and list admins and viewers
bield backup        Save a copy of Bield's data
bield healthcheck   Check that a running Bield answers (for Docker)
bield version       Print the version
```

`bield <command> -h` prints a command's flags. Bield writes its log to standard error, as text. There's no log level to set.

## bield serve

A flag wins over its environment variable, and the variable wins over the default. An empty variable counts as unset. A malformed `BIELD_PORT` only fails the start if `--port` doesn't replace it.

| Flag | Environment variable | Default | What it does |
| --- | --- | --- | --- |
| `--tailscale` | `BIELD_TAILSCALE` | `embedded` | How Bield joins the tailnet. `embedded` runs a node of its own inside Bield. `host` uses the `tailscaled` on this machine. See [Bield and Tailscale](tailscale.md). |
| `--hostname` | `BIELD_HOSTNAME` | `bield` | The name of Bield's node on the tailnet, and the first part of its MagicDNS name. Embedded mode only. |
| `--control-url` | `BIELD_CONTROL_URL` | Tailscale's | The control server that the embedded node logs in to, such as a [Headscale](headscale.md) server. It has to be an `http://` or `https://` address with a host and maybe a port, and no path. Bield refuses it in host mode, where `tailscale up --login-server` decides. |
| `--setup-listen` | `BIELD_SETUP_LISTEN` | `127.0.0.1:4700` | Where the login page listens in embedded mode. It shows the login link until the node is on the tailnet, and a link to Bield after. Keep it on loopback, or anyone who reaches it can add Bield to their own tailnet. |
| `--port` | `BIELD_PORT` | `4700` | The port Bield serves on in host mode, on each of the machine's tailnet IPs. Embedded mode always uses 443 (80 without HTTPS). |
| `--data-dir` | `BIELD_DATA_DIR` | `~/.config/bield` | Where Bield keeps its data, listed under [the data directory](#the-data-directory). The default is `bield` in the user's config directory, which is `$XDG_CONFIG_HOME` if it's set, and `~/Library/Application Support` on macOS. |
| `--host-root` | `BIELD_HOST_ROOT` | `/` | The directory whose `proc/` and `sys/` Bield measures its machine from. Only for a container that mounts the host's under another path, such as `/host`. See [Docker](docker.md#measuring-the-host). |
| `--admins` | `BIELD_ADMINS` | none | Logins to make Bield's first admins, separated by commas, along with the owner of Bield's node. Bield reads it only while it has no admins. See [Admins and viewers](tailscale.md#admins-and-viewers). |
| `--demo` | `BIELD_DEMO` | off | Run against a made-up tailnet. Takes `true` or `false`. See [Demo mode](demo.md). |
| `--demo-listen` | `BIELD_DEMO_LISTEN` | `127.0.0.1:4700` | Where the demo is served. |
| `--demo-devices` | `BIELD_DEMO_DEVICES` | `12` | How many machines the demo has, from 2 to 1000. |
| `--demo-days` | `BIELD_DEMO_DAYS` | `7` | Days of history the demo starts with, above 0 and up to 90. |
| `--dev-ui` | `BIELD_DEV_UI` | none | The address of a Vite dev server to proxy the web app to. For working on Bield, and only admins get it. `make dev` sets it. |

The Docker image sets `BIELD_DATA_DIR=/data`, and the install script sets it to `/var/lib/bield`.

## bield admin, backup and healthcheck

| Command | Flag | Environment variable | What it does |
| --- | --- | --- | --- |
| `bield admin` | `--data-dir` | `BIELD_DATA_DIR` | Where the database is. It works while Bield is running. |
| `bield backup` | `--data-dir` | `BIELD_DATA_DIR` | Where the data is. |
| `bield healthcheck` | `--setup-listen` | `BIELD_SETUP_LISTEN` | The login page's address, as `bield serve` got it. |

The subcommands of `bield admin` are in [Admins and viewers](tailscale.md#admins-and-viewers), and `bield backup` is in [Upgrading, backups and rolling back](upgrading.md#backing-up).

## Variables that Tailscale and the system read

These aren't Bield's, but Bield's behavior depends on them.

| Variable | What it does |
| --- | --- |
| `TS_AUTHKEY` (or `TS_AUTH_KEY`) | An [auth key](https://tailscale.com/kb/1085/auth-keys) or a Headscale pre-auth key. The embedded node uses it to log in without a link, but only while it has no saved node. After that, the variable does nothing, so it can go. |
| `TS_CONTROL_URL` | The control server of the embedded node, if `--control-url` isn't set. |
| `TS_NO_LOGS_NO_SUPPORT` | Set to `true` to stop the embedded node from uploading its logs to Tailscale. |
| `DOCKER_HOST` | Where Bield finds Docker on the machine it runs on. Only `unix://` addresses work. The default is `/var/run/docker.sock`. |
| `XDG_RUNTIME_DIR` | Where Bield finds the systemd session of the user it runs as, to watch that user's units. Without a session bus there, Bield watches system units only. |

## The data directory

| Path | What it holds |
| --- | --- |
| `bield.db`, `bield.db-wal`, `bield.db-shm` | The database: settings, roles, history, alerts, the activity log, and the secrets of notification channels. |
| `bield.db.bak` | The database as it was before the last upgrade that changed it. |
| `tailscale/` | The embedded node's identity. |
| `ssh/id_ed25519` | Bield's SSH key. |
| `backups/` | Backups that the install script saves before an upgrade. |

If the directory doesn't exist, Bield creates it so that only its own user can read it. It holds keys and secrets, so keep it that way. See [Upgrading, backups and rolling back](upgrading.md).

## The install script

`install.sh` reads these from its environment. Put them after `sudo`, which drops any that come before it. Bield's own settings from the list are saved in `/etc/bield/bield.env` the first time, and any other `bield serve` variable can go in that file too, one `NAME=value` per line. The script refuses values with spaces, quotes, backslashes or `$`, and systemd's rules for environment files apply to anything you add by hand.

| Variable | What it does |
| --- | --- |
| `BIELD_VERSION` | The release to install, such as `v0.1.0`. The default is the latest. |
| `BIELD_TAILSCALE` | `embedded` (the default) or `host`. |
| `BIELD_HOSTNAME` | The name of Bield's node, in embedded mode. |
| `BIELD_CONTROL_URL` | The address of a Headscale server, in embedded mode. |
| `BIELD_ADMINS` | Logins to make the first admins. |
| `TS_AUTHKEY` | An auth key, so Bield joins the tailnet without a login. |
| `BIELD_RELEASES_URL` | Where releases are downloaded from, if you mirror them. The default is the project's GitHub releases. Not saved in `bield.env`. |

More in [Installing Bield](install.md#the-install-script).

## The agent

The agent reads a file of `NAME=value` lines, which `~/.config/bield-agent/env` is unless `BIELD_AGENT_CONF` names another path. The install command writes it, so you rarely edit it. See [The agent](agent.md).

| Setting | Default | What it does |
| --- | --- | --- |
| `SERVER_URL` | none | Bield's address, such as `https://bield.example.ts.net`. Required. |
| `SECRET` | none | The agent's own secret, which it got when it enrolled. Required, and kept in a file only its user can read. |
| `INTERVAL` | `15` | Seconds between heartbeats. |

## For people working on Bield

| Variable | What it does |
| --- | --- |
| `BIELD_DEV_UI` | See `--dev-ui` above. |
| `BIELD_LOAD_DEVICES`, `BIELD_LOAD_DAYS`, `BIELD_LOAD_SECONDS` | How many devices (200) and days of history (7) `make load` runs Bield with, and how many seconds (30) it measures. See [Benchmarks](benchmark.md). |

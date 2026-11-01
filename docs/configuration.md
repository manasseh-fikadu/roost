# Configuration reference

Every flag and environment variable that Roost, its install script and its agent read. What admins change in the app is on [the Settings page](settings.md).

## The roost command

```text
roost serve         Run the Roost hub
roost admin         Add, remove and list admins and viewers
roost backup        Save a copy of Roost's data
roost healthcheck   Check that a running Roost answers (for Docker)
roost version       Print the version
```

`roost <command> -h` prints a command's flags. Roost writes its log to standard error, as text. There's no log level to set.

## roost serve

A flag wins over its environment variable, and the variable wins over the default. An empty variable counts as unset. A malformed `ROOST_PORT` only fails the start if `--port` doesn't replace it.

| Flag | Environment variable | Default | What it does |
| --- | --- | --- | --- |
| `--tailscale` | `ROOST_TAILSCALE` | `embedded` | How Roost joins the tailnet. `embedded` runs a node of its own inside Roost. `host` uses the `tailscaled` on this machine. See [Roost and Tailscale](tailscale.md). |
| `--hostname` | `ROOST_HOSTNAME` | `roost` | The name of Roost's node on the tailnet, and the first part of its MagicDNS name. Embedded mode only. |
| `--control-url` | `ROOST_CONTROL_URL` | Tailscale's | The control server that the embedded node logs in to, such as a [Headscale](headscale.md) server. It has to be an `http://` or `https://` address with a host and maybe a port, and no path. Roost refuses it in host mode, where `tailscale up --login-server` decides. |
| `--setup-listen` | `ROOST_SETUP_LISTEN` | `127.0.0.1:4700` | Where the login page listens in embedded mode. It shows the login link until the node is on the tailnet, and a link to Roost after. Keep it on loopback, or anyone who reaches it can add Roost to their own tailnet. |
| `--port` | `ROOST_PORT` | `4700` | The port Roost serves on in host mode, on each of the machine's tailnet IPs. Embedded mode always uses 443 (80 without HTTPS). |
| `--data-dir` | `ROOST_DATA_DIR` | `~/.config/roost` | Where Roost keeps its data, listed under [the data directory](#the-data-directory). The default is `roost` in the user's config directory, which is `$XDG_CONFIG_HOME` if it's set, and `~/Library/Application Support` on macOS. |
| `--host-root` | `ROOST_HOST_ROOT` | `/` | The directory whose `proc/` and `sys/` Roost measures its machine from. Only for a container that mounts the host's under another path, such as `/host`. See [Docker](docker.md#measuring-the-host). |
| `--admins` | `ROOST_ADMINS` | none | Logins to make Roost's first admins, separated by commas, along with the owner of Roost's node. Roost reads it only while it has no admins. See [Admins and viewers](tailscale.md#admins-and-viewers). |
| `--demo` | `ROOST_DEMO` | off | Run against a made-up tailnet. Takes `true` or `false`. See [Demo mode](demo.md). |
| `--demo-listen` | `ROOST_DEMO_LISTEN` | `127.0.0.1:4700` | Where the demo is served. |
| `--demo-devices` | `ROOST_DEMO_DEVICES` | `12` | How many machines the demo has, from 2 to 1000. |
| `--demo-days` | `ROOST_DEMO_DAYS` | `7` | Days of history the demo starts with, above 0 and up to 90. |
| `--dev-ui` | `ROOST_DEV_UI` | none | The address of a Vite dev server to proxy the web app to. For working on Roost, and only admins get it. `make dev` sets it. |

The Docker image sets `ROOST_DATA_DIR=/data`, and the install script sets it to `/var/lib/roost`.

## roost admin, backup and healthcheck

| Command | Flag | Environment variable | What it does |
| --- | --- | --- | --- |
| `roost admin` | `--data-dir` | `ROOST_DATA_DIR` | Where the database is. It works while Roost is running. |
| `roost backup` | `--data-dir` | `ROOST_DATA_DIR` | Where the data is. |
| `roost healthcheck` | `--setup-listen` | `ROOST_SETUP_LISTEN` | The login page's address, as `roost serve` got it. |

The subcommands of `roost admin` are in [Admins and viewers](tailscale.md#admins-and-viewers), and `roost backup` is in [Upgrading, backups and rolling back](upgrading.md#backing-up).

## Variables that Tailscale and the system read

These aren't Roost's, but Roost's behavior depends on them.

| Variable | What it does |
| --- | --- |
| `TS_AUTHKEY` (or `TS_AUTH_KEY`) | An [auth key](https://tailscale.com/kb/1085/auth-keys) or a Headscale pre-auth key. The embedded node uses it to log in without a link, but only while it has no saved node. After that, the variable does nothing, so it can go. |
| `TS_CONTROL_URL` | The control server of the embedded node, if `--control-url` isn't set. |
| `TS_NO_LOGS_NO_SUPPORT` | Set to `true` to stop the embedded node from uploading its logs to Tailscale. |
| `DOCKER_HOST` | Where Roost finds Docker on the machine it runs on. Only `unix://` addresses work. The default is `/var/run/docker.sock`. |
| `XDG_RUNTIME_DIR` | Where Roost finds the systemd session of the user it runs as, to watch that user's units. Without a session bus there, Roost watches system units only. |

## The data directory

| Path | What it holds |
| --- | --- |
| `roost.db`, `roost.db-wal`, `roost.db-shm` | The database: settings, roles, history, alerts, the activity log, and the secrets of notification channels. |
| `roost.db.bak` | The database as it was before the last upgrade that changed it. |
| `tailscale/` | The embedded node's identity. |
| `ssh/id_ed25519` | Roost's SSH key. |
| `backups/` | Backups that the install script saves before an upgrade. |

If the directory doesn't exist, Roost creates it so that only its own user can read it. It holds keys and secrets, so keep it that way. See [Upgrading, backups and rolling back](upgrading.md).

## The install script

`install.sh` reads these from its environment. Put them after `sudo`, which drops any that come before it. Roost's own settings from the list are saved in `/etc/roost/roost.env` the first time, and any other `roost serve` variable can go in that file too, one `NAME=value` per line. The script refuses values with spaces, quotes, backslashes or `$`, and systemd's rules for environment files apply to anything you add by hand.

| Variable | What it does |
| --- | --- |
| `ROOST_VERSION` | The release to install, such as `v0.1.0`. The default is the latest. |
| `ROOST_TAILSCALE` | `embedded` (the default) or `host`. |
| `ROOST_HOSTNAME` | The name of Roost's node, in embedded mode. |
| `ROOST_CONTROL_URL` | The address of a Headscale server, in embedded mode. |
| `ROOST_ADMINS` | Logins to make the first admins. |
| `TS_AUTHKEY` | An auth key, so Roost joins the tailnet without a login. |
| `ROOST_RELEASES_URL` | Where releases are downloaded from, if you mirror them. The default is the project's GitHub releases. Not saved in `roost.env`. |

More in [Installing Roost](install.md#the-install-script).

## The agent

The agent reads a file of `NAME=value` lines, which `~/.config/roost-agent/env` is unless `ROOST_AGENT_CONF` names another path. The install command writes it, so you rarely edit it. See [The agent](agent.md).

| Setting | Default | What it does |
| --- | --- | --- |
| `SERVER_URL` | none | Roost's address, such as `https://roost.example.ts.net`. Required. |
| `SECRET` | none | The agent's own secret, which it got when it enrolled. Required, and kept in a file only its user can read. |
| `INTERVAL` | `15` | Seconds between heartbeats. |

## For people working on Roost

| Variable | What it does |
| --- | --- |
| `ROOST_DEV_UI` | See `--dev-ui` above. |
| `ROOST_LOAD_DEVICES`, `ROOST_LOAD_DAYS`, `ROOST_LOAD_SECONDS` | How many devices (200) and days of history (7) `make load` runs Roost with, and how many seconds (30) it measures. See [Benchmarks](benchmark.md). |

# Roost

Roost is a self-hosted control plane for the machines on your tailnet. It's one binary. Run it on a server, and it joins your [Tailscale](https://tailscale.com) tailnet (or a [Headscale](https://github.com/juanfont/headscale) one) and shows every machine in one place: how healthy it is, what's wrong with it, what runs on it. From the same page, you can open a terminal on it, browse its files and restart its containers. There's no password and no account to make, because your tailnet is the login.

![Roost's overview: a map of the tailnet, a health score and the open alerts](docs/images/overview.png)

> [!NOTE]
> Roost is close to its first release, v0.1. Until that's tagged, the install script, the Docker image and the release archives below don't exist yet, so [build it from source](#build-from-source) or try the [demo](#try-the-demo).

## What it does

- **Devices.** Every machine on the tailnet, on a live map, with how traffic reaches it (direct, through a relay) and the round-trip time.
- **Health.** CPU, memory, disk, network and what's using them, for each machine, with up to 90 days of history. The machine Roost runs on is measured by Roost itself, if it's Linux, and the others by a small bash [agent](docs/agent.md).
- **Checks.** Website, port and DNS checks, and a ping of every device, rolled up into a health score.
- **Alerts.** For offline machines, full disks, expiring Tailscale keys, failing checks, crashed containers and failed services. They reach your phone through ntfy, Telegram, email, Discord, Slack or any webhook ([Notifications](docs/notifications.md)).
- **Docker.** Containers, logs, live stats, images, volumes and networks on any machine Roost can reach, and start, stop and restart.
- **Terminal and files.** A shell in the browser, which keeps running in tmux after you close the tab, and a file manager that edits and previews ([SSH access](docs/ssh.md)).
- **Roles and a log.** Admins and read-only viewers, decided by tailnet identity, and an activity log of who did what.

<p>
  <img src="docs/images/device.png" alt="A device's page, with metrics, an alert and history charts" width="49%">
  <img src="docs/images/docker.png" alt="The Docker page, with containers grouped by Compose project" width="49%">
</p>

It works on a phone, and it's keyboard-friendly: `Ctrl+K` (`⌘K` on a Mac) searches pages, devices and actions, and `?` lists the shortcuts.

## Try the demo

The demo is a made-up tailnet with 12 machines and a week of history. It needs no Tailscale and touches nothing on your machine.

```sh
make build            # or download a release archive, when there is one
./roost serve --demo  # then open http://127.0.0.1:4700
```

See [Demo mode](docs/demo.md). Every screenshot here comes from it.

## Quickstart

Roost needs a tailnet and a machine to run on. It takes about two minutes.

**With Docker**, which works on any host:

```sh
curl -fsSLO https://raw.githubusercontent.com/manasseh-fikadu/roost/main/compose.yaml
docker compose up -d
docker compose logs roost    # open the link in it, to log Roost in to Tailscale
```

**On a Linux server with systemd**, one command installs Roost and runs it as a service:

```sh
curl -fsSL https://raw.githubusercontent.com/manasseh-fikadu/roost/main/install.sh | sudo bash
```

Then open <http://127.0.0.1:4700> on that machine, and log Roost in to Tailscale. To skip the login, give the command or the `.env` file an [auth key](https://tailscale.com/kb/1085/auth-keys) as `TS_AUTHKEY`.

**On a Mac, or Linux without systemd**, download the [release archive](https://github.com/manasseh-fikadu/roost/releases) for your system, check it against `checksums.txt`, and run `./roost serve`.

After that, Roost is at `https://roost.<your-tailnet>.ts.net` (plain `http://` until you turn on HTTPS certificates for the tailnet), and whoever logged it in is its first admin. With an auth key for a tagged node, nobody owns it, so set `ROOST_ADMINS` too. Its machines are there already. Next:

1. Open a machine, and **Install the agent** for its CPU, memory and disk.
2. **Set up SSH** on it, for the terminal, the files and Docker.
3. Add a **notification channel** in Settings, so alerts find you.

Roost opens a setup page the first time, which walks through the same steps. The options for each way of installing are in [Installing Roost](docs/install.md) and [Roost in Docker](docs/docker.md). [Roost and Tailscale](docs/tailscale.md) says how it joins the tailnet, with the policy it needs. Roost's data is in `~/.config/roost`, `/var/lib/roost` or `/data`, depending on how you run it.

## The pages

| Page | What it does |
| --- | --- |
| Overview | The tailnet at a glance: a map, the health score, open alerts, every machine |
| Devices | Every machine, with its metrics, history, connection and notes. **Agents** installs and removes agents |
| Alerts | What's wrong now: acknowledge, snooze and add notes. Resolved alerts too |
| Checks | Website, port and DNS checks, plus pings, and the health score they make |
| Docker | Containers, volumes, images and networks of a machine, and their logs and stats |
| Terminal | A shell on a machine, in the browser |
| Files | A file manager for a machine's disk |
| Activity | Devices coming and going, alerts, and what people did |
| Settings | Thresholds, notifications, watched services, retention, and who has a role |

[A tour of Roost](docs/pages.md) describes each one.

## The security model

Roost logs in to your machines, so whoever controls Roost controls them. These are the rules it follows, in plain words:

- **Only your tailnet can reach it.** Roost serves the app on the tailnet and nowhere else, apart from a login page that answers on the machine itself, and it has no passwords. For each connection, it asks Tailscale which device it comes from and which user owns that device. Nothing in a request can claim to be someone else.
- **Two roles.** Admins can do everything, including opening a shell on every machine with SSH access, so treat the admin list like sudoers. Viewers get a read-only dashboard, without logs, files, commands or secrets. Anyone else gets nothing. A device Tailscale can't name, such as a tagged server, is anonymous.
- **It's hard to trick a browser into acting.** Anything that changes something needs a header that other websites can't send, and a strict content security policy keeps scripts and frames out.
- **Machines are untrusted.** An agent gets a secret of its own that only works from its own machine, and it can report metrics and do nothing else. Everything a machine says is shown as text. Roost's SSH key only works from Roost's addresses, and it pins each machine's host key.
- **Docker is limited on purpose.** Roost can list, read, start, stop and restart containers. It can't create, exec or prune.
- **It keeps a log.** Terminals opened, files changed, containers restarted, settings and roles changed: each is a line in the activity log.
- **What it holds is sensitive.** The database has notification tokens, unencrypted, and Roost's data directory has its SSH key. Both are readable only by Roost's user. Keep the machine as you would a bastion host.

Roost hasn't had an outside review yet. [SECURITY.md](SECURITY.md) has the full threat model and says how to report a problem privately.

## Documentation

| | |
| --- | --- |
| [Installing Roost](docs/install.md), [Docker](docs/docker.md) | The ways to run it |
| [Tailscale](docs/tailscale.md), [Headscale](docs/headscale.md) | Joining the tailnet, signing in, admins and viewers |
| [The agent](docs/agent.md) | Per-machine metrics |
| [SSH access](docs/ssh.md) | Terminals, files and Docker on other machines |
| [Notifications](docs/notifications.md) | Where alerts go, and when |
| [Settings](docs/settings.md) | Every setting, with its default |
| [Configuration reference](docs/configuration.md) | Every flag and environment variable |
| [Upgrading and backups](docs/upgrading.md), [Uninstalling](docs/uninstalling.md) | Keeping it running, and removing it |
| [FAQ](docs/faq.md) | Questions, and what to check when something's wrong |

The full list is in [docs/](docs/README.md).

## Build from source

You need Go 1.27.3 or newer, Node 22.17 or newer, and [Bun](https://bun.sh).

```sh
make build      # builds ./roost with the web UI inside
./roost serve   # joins your tailnet as a new machine named roost
```

The first time, open <http://127.0.0.1:4700> on the same machine to log Roost in to Tailscale. After that, Roost is at `https://roost.<your-tailnet>.ts.net`. To use the machine's own Tailscale instead, run `./roost serve --tailscale host`. To use Headscale, add `--control-url https://headscale.example.com`.

There's no password: Roost asks Tailscale who each connection comes from. Whoever logs Roost in to Tailscale becomes its first admin, and `roost admin add <login>` adds more. Viewers get a read-only dashboard: add them with `roost admin add-viewer <login>`, or let anyone on the tailnet view with `roost admin who-can-view tailnet`. See [Signing in](docs/tailscale.md#signing-in) and [Admins and viewers](docs/tailscale.md#admins-and-viewers).

Roost keeps its data in `~/.config/roost` (`~/Library/Application Support/roost` on macOS). To put it somewhere else, use `--data-dir` or set `ROOST_DATA_DIR`.

## Develop

```sh
make dev     # runs Vite, and the Go server, which proxies the web UI to it
make demo    # the same, against the made-up tailnet
make test    # Go and web tests, then svelte-check
make lint    # golangci-lint, shellcheck and prettier
make types   # regenerates the web app's API types after you change internal/api/types.go
```

Open the URL that the Go server logs (`roost is running url=...`), which is on your tailnet, not Vite's, from a device that belongs to one of Roost's admins. The Go server doesn't reload on changes yet, so restart `make dev` after editing Go code.

| Path | What's in it |
| --- | --- |
| `cmd/roost` | The `roost` command |
| `internal/` | The hub's packages |
| `web/` | The SvelteKit app, which `make build` embeds in the binary |
| `agent/` | The bash agent that devices run to report their metrics |
| `install.sh` | Installs Roost as a systemd service |
| `Dockerfile`, `compose*.yaml` | The Docker image, which GoReleaser builds, and examples that run it |
| `docs/` | The documentation, and the screenshots in it, which `roost serve --demo` made |
| `.goreleaser.yaml` | How a release is built |

## Contribute

See [CONTRIBUTING.md](CONTRIBUTING.md). Please report security problems privately, as described in [SECURITY.md](SECURITY.md).

## License

Roost is licensed under the [GNU Affero General Public License v3.0](LICENSE). The agent in `agent/` is under the [MIT License](agent/LICENSE), so you can reuse it anywhere.

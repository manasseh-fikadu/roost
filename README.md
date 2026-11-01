# Roost

A simple, self-hosted control plane for every machine on your tailnet: devices, health, alerts, Docker, files and terminals in one place, with your tailnet as the login.

> [!NOTE]
> Roost is in early development and doesn't do anything useful yet. The [roadmap to v0.1](https://github.com/manasseh-fikadu/roost/issues/1) tracks progress.

## Install

On a Linux server with systemd, one command installs Roost and runs it as a service:

```sh
curl -fsSL https://raw.githubusercontent.com/manasseh-fikadu/roost/main/install.sh | sudo bash
```

With Docker, use [`compose.yaml`](compose.yaml) (or [`compose.host.yaml`](compose.host.yaml) to use the host's Tailscale), which runs `ghcr.io/manasseh-fikadu/roost`. Every [release](https://github.com/manasseh-fikadu/roost/releases) also has archives for Linux and macOS, on amd64 and arm64, with checksums. See [Installing Roost](docs/install.md), [Roost in Docker](docs/docker.md), and [Upgrading, backups and rolling back](docs/upgrading.md).

## Build from source

You need Go 1.27.3 or newer, Node 22.17 or newer, and [Bun](https://bun.sh).

```sh
make build      # builds ./roost with the web UI inside
./roost serve   # joins your tailnet as a new machine named roost
```

The first time, open <http://127.0.0.1:4700> on the same machine to log Roost in to Tailscale. After that, Roost is at `https://roost.<your-tailnet>.ts.net`. To use the machine's own Tailscale instead, run `./roost serve --tailscale host`. [docs/tailscale.md](docs/tailscale.md) explains both modes and the tailnet policy Roost needs.

There's no password: Roost asks Tailscale who each connection comes from. Whoever logs Roost in to Tailscale becomes its first admin, and `roost admin add <login>` adds more. Viewers get a read-only dashboard: add them with `roost admin add-viewer <login>`, or let anyone on the tailnet view with `roost admin who-can-view tailnet`. See [Signing in](docs/tailscale.md#signing-in) and [Admins and viewers](docs/tailscale.md#admins-and-viewers).

Roost keeps its data in `~/.config/roost` (`~/Library/Application Support/roost` on macOS). To put it somewhere else, use `--data-dir` or set `ROOST_DATA_DIR`.

## Develop

```sh
make dev     # runs Vite, and the Go server, which proxies the web UI to it
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
| `.goreleaser.yaml` | How a release is built |

## Contribute

See [CONTRIBUTING.md](CONTRIBUTING.md). Please report security problems privately, as described in [SECURITY.md](SECURITY.md).

## License

Roost is licensed under the [GNU Affero General Public License v3.0](LICENSE). The agent in `agent/` is under the [MIT License](agent/LICENSE), so you can reuse it anywhere.

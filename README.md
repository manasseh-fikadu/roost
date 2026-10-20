# Roost

A simple, self-hosted control plane for every machine on your tailnet: devices, health, alerts, Docker, files and terminals in one place, with your tailnet as the login.

> [!NOTE]
> Roost is in early development and doesn't do anything useful yet. The [roadmap to v0.1](https://github.com/manasseh-fikadu/roost/issues/1) tracks progress.

## Build from source

You need Go 1.27 or newer, Node 22.17 or newer, and [Bun](https://bun.sh).

```sh
make build      # builds ./roost with the web UI inside
./roost serve   # serves it on http://127.0.0.1:4700
```

## Develop

```sh
make dev     # runs the Go server on :4700 and Vite, which proxies /api to it
make test    # Go tests, then svelte-check
make types   # regenerates the web app's API types after you change internal/api/types.go
```

The Go server doesn't reload on changes yet, so restart `make dev` after editing Go code.

| Path | What's in it |
| --- | --- |
| `cmd/roost` | The `roost` command |
| `internal/` | The hub's packages |
| `web/` | The SvelteKit app, which `make build` embeds in the binary |
| `agent/` | The bash agent that devices run to report their metrics |

## Contribute

See [CONTRIBUTING.md](CONTRIBUTING.md). Please report security problems privately, as described in [SECURITY.md](SECURITY.md).

## License

Roost is licensed under the [GNU Affero General Public License v3.0](LICENSE). The agent in `agent/` is under the [MIT License](agent/LICENSE), so you can reuse it anywhere.

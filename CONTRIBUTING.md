# Contributing to Roost

Thanks for helping. Roost is early, and the [roadmap to v0.1](https://github.com/manasseh-fikadu/roost/issues/1) lists the work in order, one issue at a time.

## Before you start

- For anything bigger than a small fix, comment on the issue first (or open one), so we agree on the approach before you write code.
- Report security problems privately, as described in the [security policy](SECURITY.md), never in a public issue.

## Set up

You need Go 1.27.3 or newer, Node 22.17 or newer, and [Bun](https://bun.sh). To run `make lint`, you also need [golangci-lint](https://golangci-lint.run/) v2 and [ShellCheck](https://www.shellcheck.net/).

```sh
git clone https://github.com/manasseh-fikadu/roost
cd roost
make dev
```

`make dev` runs the Vite dev server and the Go server, which serves Roost on your tailnet and proxies the web UI to Vite. Open the URL the Go server logs (`roost is running url=...`), not Vite's, so the API sees requests come over the tailnet the way it does in production. The first time, Roost joins your tailnet as a new machine: open <http://127.0.0.1:4700> to log it in. Roost makes whoever logs it in an admin, and only admins get the UI from Vite. If the machine you work on has Tailscale, `ROOST_TAILSCALE=host make dev` uses it instead and skips the login, but then open Roost from another device, since in host mode Roost signs no one in from its own machine. See [docs/tailscale.md](docs/tailscale.md) for both modes.

The Go server doesn't reload on changes, so restart `make dev` after editing Go code.

| Command | What it does |
| --- | --- |
| `make test` | Runs the Go tests with the race detector, the web tests with `bun test`, then `svelte-check` |
| `make lint` | Runs golangci-lint, ShellCheck and Prettier |
| `make build` | Builds `./roost` with the web UI embedded |
| `make demo` | Runs Roost against a made-up tailnet, with the web UI from Vite, so you need no Tailscale (see [docs/demo.md](docs/demo.md)) |
| `make e2e` | Runs the Playwright smoke test against the demo, which needs `make build` first |
| `make screenshots` | Retakes the screenshots in `docs/images` from the demo, which needs `make build` first |
| `make bench`, `make load` | Measure the store's queries, and Roost with 200 devices and 90 days of history (see [docs/benchmark.md](docs/benchmark.md)) |
| `make types` | Regenerates the web app's API types from `internal/api/types.go` |
| `make help` | Lists every target |

## Writing code

- **Go:** format with `gofmt` and prefer the standard library; a new dependency needs a reason. `.golangci.yml` sets the linters. If you have to silence one, say which and why: `//nolint:gosec // reason`.
- **Web:** use Svelte 5 runes and SvelteKit 3 only. SvelteKit 3 changed a lot, and most examples online are for version 2, so check [CLAUDE.md](CLAUDE.md) for the old patterns and their replacements. Run `bun run format` in `web/` before committing.
- **Web UI:** build pages from the components in `web/src/lib/ui` and the charts in `web/src/lib/charts`, which `make dev` shows together at `/design`. Pages read live data from `live` in `web/src/lib/live.svelte.ts`, and send changes with `mutate` from `web/src/lib/api/client.ts`, which shows any error as a toast. A page only admins can open gets `adminOnly` in `web/src/lib/shell/nav.ts`.
- **API routes:** register each route in `routes` in `internal/api/api.go` with the role it needs. A route that reads says whether it's for `anyone`, `viewers` or `admins`, and a route that changes something is always for admins. `TestRoutes` checks every route against that. Viewers never get logs, files or secrets, so a route that returns those is for admins, and a response field that can carry a secret, such as an environment variable or a command line, gets the tag `roost:"secret"`. The API leaves it empty for everyone but admins, so handlers don't check who's asking. In the web app, wrap admin-only controls in `AdminOnly`.
- **API types:** change `internal/api/types.go` and run `make types`. Never edit `web/src/lib/api/types.ts` by hand.
- **Database:** to change the schema, add the next numbered file to `internal/store/migrations`, such as `0002_add_device_notes.sql`. On startup, Roost copies the database to `roost.db.bak` and runs the new files in one transaction, so a failed upgrade leaves the database as it was. Never edit or renumber a migration once it's merged; add another one. A new table that keeps history also needs a line in `retention` in `internal/store/prune.go`, so its old rows get deleted. A history that gets long (metrics and pings are kept for up to 90 days) is summed up into buckets, as `internal/store/rollup.go` does.
- **Docs:** a pull request that adds or changes a flag, an environment variable or a setting also updates [docs/configuration.md](docs/configuration.md) or [docs/settings.md](docs/settings.md), so every one stays documented. The screenshots in `docs/images` are from the demo, and `make screenshots` retakes them when a page changes enough to show.
- **Agent:** `agent/` has to run on macOS's bash 3.2 and with any POSIX awk, using only `curl` and standard system tools.

## Tests

- Put tests in a `_test.go` file next to the code they test, as table tests with a `name` for each case. Test through the package's exported API (`package foo_test`) where you can, and call `t.Parallel()` unless a test shares state.
- Unit tests never reach a real tailnet, SSH server or outside network. For HTTP, use `httptest.NewRecorder`, or `httptest.NewTestServer`, which runs on an in-memory network.
- **Time:** don't add a clock interface. Code calls `time.Now`, timers and tickers directly, and a test that depends on time runs inside `synctest.Test` from `testing/synctest`. That gives the test a fake clock that jumps ahead whenever every goroutine is blocked, so a test of a 5-minute alert window takes microseconds.
- **Tailscale and SSH:** code that needs them takes a small interface instead of the real client. The package that wraps the real client also ships a fake, in a test package named the way the standard library names `httptest` and `fstest` (for example `internal/tailnet/tailnettest`). Collector and alert tests run against these fakes. Fakes block on channels rather than sockets, so they work inside `synctest`.
- **Web:** keep logic that doesn't need the page, such as formatting or chart geometry, in plain `.ts` modules, and test it in a `.test.ts` file next to it with `bun test`, as table tests with `test.each`.
- Tests that need a real tailnet or SSH server go in files tagged `//go:build integration`, and run with `go test -tags integration ./...`. CI doesn't run them.

## Commits and pull requests

- Write commit subjects as [Conventional Commits](https://www.conventionalcommits.org/) without a scope, for example `feat: add disk alerts` or `fix: keep the terminal open on resize`.
- Sign off every commit (see below).
- Keep each pull request to one issue, and link it with `Closes #123`. Give the pull request a Conventional Commits title too, because a squash merge turns it into the commit subject.
- Run `make test` and `make lint` before you push. CI runs both on every pull request, along with govulncheck, a build of the binary and a check that every commit is signed off.

## Releases

A release is a tag. When `main` is green, push one:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The [Release workflow](.github/workflows/release.yml) then runs GoReleaser (`.goreleaser.yaml`), which builds the web app and the binary for Linux and macOS on amd64 and arm64, publishes the archives, `checksums.txt` and a changelog as a GitHub release, and pushes the Docker image to `ghcr.io/manasseh-fikadu/roost`. A tag with a suffix, such as `v0.1.0-rc.1`, is a pre-release, which doesn't move the image's `latest` tag.

The changelog is made from the commit subjects since the last tag, grouped as features (`feat:`), fixes (`fix:`) and performance (`perf:`), then the rest. `docs:`, `test:`, `chore:`, `ci:` and `style:` commits are left out, so write a subject that makes sense to someone who only reads the changelog.

The `Release build` job in CI runs the same GoReleaser build without publishing, and starts the image it makes, so a change that breaks the release fails the pull request. To do the same on your machine, install [GoReleaser](https://goreleaser.com/install/), Docker and Bun, and run:

```sh
goreleaser release --snapshot --clean --skip=publish
```

Everything lands in `dist/`, and the images stay in your local Docker. `GORELEASER_VERSION` is pinned by hand in `ci.yml` and `release.yml`, so change it in both. The first time a tag pushes the image, check on GitHub that the package is public (the repository's *Packages*, then *Package settings*), or no one can pull it. `install.sh` builds the archive names from `uname`, so change the `name_template` in `.goreleaser.yaml` and the script together.

## Sign your work

Roost uses the [Developer Certificate of Origin](https://developercertificate.org/) (DCO). The `Signed-off-by` line on a commit certifies that you wrote the change, or otherwise have the right to submit it under the project's licenses. Add it with `-s`:

```sh
git commit -s -m "fix: keep the terminal open on resize"
```

The line uses the name and email in your git config. If you forgot it, `git commit --amend -s` signs off your last commit, and `git rebase --signoff main` signs off every commit on your branch.

## License

Your contributions are licensed the same way as the files you change: [AGPL-3.0](LICENSE) for everything outside `agent/`, and [MIT](agent/LICENSE) for `agent/`.

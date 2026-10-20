# Contributing to Roost

Thanks for helping. Roost is early, and the [roadmap to v0.1](https://github.com/manasseh-fikadu/roost/issues/1) lists the work in order, one issue at a time.

## Before you start

- For anything bigger than a small fix, comment on the issue first (or open one), so we agree on the approach before you write code.
- Report security problems privately, as described in the [security policy](SECURITY.md), never in a public issue.

## Set up

You need Go 1.27 or newer, Node 22.17 or newer, and [Bun](https://bun.sh). To run `make lint`, you also need [golangci-lint](https://golangci-lint.run/) v2 and [ShellCheck](https://www.shellcheck.net/).

```sh
git clone https://github.com/manasseh-fikadu/roost
cd roost
make dev
```

`make dev` runs the Go server on 127.0.0.1:4700 and the Vite dev server, which proxies `/api` to it. Open the URL Vite prints. The Go server doesn't reload on changes, so restart `make dev` after editing Go code.

| Command | What it does |
| --- | --- |
| `make test` | Runs the Go tests with the race detector, then `svelte-check` |
| `make lint` | Runs golangci-lint, ShellCheck and Prettier |
| `make build` | Builds `./roost` with the web UI embedded |
| `make types` | Regenerates the web app's API types from `internal/api/types.go` |
| `make help` | Lists every target |

## Writing code

- **Go:** format with `gofmt` and prefer the standard library; a new dependency needs a reason. `.golangci.yml` sets the linters. If you have to silence one, say which and why: `//nolint:gosec // reason`.
- **Web:** use Svelte 5 runes and SvelteKit 3 only. SvelteKit 3 changed a lot, and most examples online are for version 2, so check [CLAUDE.md](CLAUDE.md) for the old patterns and their replacements. Run `bun run format` in `web/` before committing.
- **API types:** change `internal/api/types.go` and run `make types`. Never edit `web/src/lib/api/types.ts` by hand.
- **Agent:** `agent/` has to run on macOS's bash 3.2 and with any POSIX awk, using only `curl` and standard system tools.

## Tests

- Put tests in a `_test.go` file next to the code they test, as table tests with a `name` for each case. Test through the package's exported API (`package foo_test`) where you can, and call `t.Parallel()` unless a test shares state.
- Unit tests never reach a real tailnet, SSH server or outside network. For HTTP, use `httptest.NewRecorder`, or `httptest.NewTestServer`, which runs on an in-memory network.
- **Time:** don't add a clock interface. Code calls `time.Now`, timers and tickers directly, and a test that depends on time runs inside `synctest.Test` from `testing/synctest`. That gives the test a fake clock that jumps ahead whenever every goroutine is blocked, so a test of a 5-minute alert window takes microseconds.
- **Tailscale and SSH:** code that needs them takes a small interface instead of the real client. The package that wraps the real client also ships a fake, in a test package named the way the standard library names `httptest` and `fstest` (for example `internal/tailnet/tailnettest`). Collector and alert tests run against these fakes. Fakes block on channels rather than sockets, so they work inside `synctest`.
- Tests that need a real tailnet or SSH server go in files tagged `//go:build integration`, and run with `go test -tags integration ./...`. CI doesn't run them.

## Commits and pull requests

- Write commit subjects as [Conventional Commits](https://www.conventionalcommits.org/) without a scope, for example `feat: add disk alerts` or `fix: keep the terminal open on resize`.
- Sign off every commit (see below).
- Keep each pull request to one issue, and link it with `Closes #123`. Give the pull request a Conventional Commits title too, because a squash merge turns it into the commit subject.
- Run `make test` and `make lint` before you push. CI runs both on every pull request, along with govulncheck, a build of the binary and a check that every commit is signed off.

## Sign your work

Roost uses the [Developer Certificate of Origin](https://developercertificate.org/) (DCO). The `Signed-off-by` line on a commit certifies that you wrote the change, or otherwise have the right to submit it under the project's licenses. Add it with `-s`:

```sh
git commit -s -m "fix: keep the terminal open on resize"
```

The line uses the name and email in your git config. If you forgot it, `git commit --amend -s` signs off your last commit, and `git rebase --signoff main` signs off every commit on your branch.

## License

Your contributions are licensed the same way as the files you change: [AGPL-3.0](LICENSE) for everything outside `agent/`, and [MIT](agent/LICENSE) for `agent/`.

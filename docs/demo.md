# Demo mode

`bield serve --demo` runs Bield against a tailnet that doesn't exist. It needs no Tailscale account, no other machine and no Docker, and it never touches the machine it runs on.

```sh
bield serve --demo
```

Then open <http://127.0.0.1:4700>. You're Alex, an admin, on a MacBook, and the tailnet has 12 machines: a home server and a mini PC with Docker, two Raspberry Pis (one of them offline), two VPSes, a desktop, two laptops, a phone and a tablet. A week of history is already there: metrics, round-trip times, check results, alerts that opened and cleared, and an activity log. A few things are wrong on purpose, so the alerts, checks and Docker pages have something to show.

It's for trying Bield, for screenshots, for the landing page's live demo, and for testing the web app. Nothing it shows is real, and nothing it does leaves the process.

## What it is

The demo doesn't swap out any of Bield. Its checks, alerts, notifications, Docker pages and SSH layer run as they do on a real tailnet, against stand-ins for what's outside Bield:

- The tailnet is the same fake the tests use (`internal/tailnet/tailnettest`), kept up to date with each machine's state and round-trip times.
- Each machine's agent sends real heartbeats and process lists, as `agent.sh` does.
- Each machine's sshd is the fake the tests use (`internal/ssh/sshtest`). It lets in Bield's key and serves a made-up Docker Engine, so Bield finds Docker the way it does on a real machine.
- The checks' targets and the places notifications go to are in-memory servers, and `systemctl` answers for the machine Bield pretends to run on.

What each machine does is a function of the time, so the history the demo starts with and what it shows live are one line.

## What's turned off

A demo has no machines to open a shell on, a file system to browse or a Docker to change. The terminal, the files, starting, stopping and restarting containers, changing SSH access, installing and removing agents, and changing who can use Bield answer with a notice. The web app says so on every page, and its buttons for them are off. Everything else works, against the made-up machines: acknowledge an alert, add a check, change a threshold or the retention.

The demo answers to any host name, since there's no tailnet name to hold requests to, and it takes everyone for Alex. Don't run it where you don't want that: by default it listens on `127.0.0.1` only.

## Options

| Flag | Environment variable | Default | |
| --- | --- | --- | --- |
| `--demo` | `BIELD_DEMO` | off | Run the demo. |
| `--demo-listen` | `BIELD_DEMO_LISTEN` | `127.0.0.1:4700` | Where to serve it. |
| `--demo-devices` | `BIELD_DEMO_DEVICES` | `12` | How many machines the tailnet has, Bield's own included. Past 12, Bield makes more from templates. |
| `--demo-days` | `BIELD_DEMO_DAYS` | `7` | How many days of history to start with, up to 90. It can be a fraction. |

The demo keeps its database in a temporary directory and deletes it when it stops, so it starts afresh each time. It ignores `--data-dir`. To run it for the public, put it behind a reverse proxy, and restart it now and then (a systemd timer or Docker's restart policy will do), so that what visitors changed goes away.

## Development

`make demo` runs the demo with Vite, as `make dev` does for a real tailnet. `make build && make e2e` runs the Playwright smoke test against the built binary, which is what CI does: it visits every page on a desktop and on a phone, and checks that none of them logs an error.

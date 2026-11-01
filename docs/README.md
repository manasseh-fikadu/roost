# Bield documentation

Start with the [README](../README.md) for what Bield is and a two-minute quickstart. These pages go deeper.

## Set up

| Page | What's in it |
| --- | --- |
| [Installing Bield](install.md) | The install script, release archives, and the options for each |
| [Bield in Docker](docker.md) | Compose files, data, the Docker socket, measuring the host |
| [Bield and Tailscale](tailscale.md) | Embedded and host mode, HTTPS, the tailnet policy, signing in, admins and viewers |
| [Bield and Headscale](headscale.md) | Running Bield against a Headscale server |
| [Demo mode](demo.md) | A made-up tailnet to try Bield without one |

## Use it

| Page | What's in it |
| --- | --- |
| [A tour of Bield](pages.md) | What each page does |
| [The agent](agent.md) | Per-machine metrics: what it sends, how to install, update, revoke and remove it |
| [SSH access, terminals and files](ssh.md) | Setting up Bield's key or Tailscale SSH, the terminal, the file manager |
| [Notifications](notifications.md) | ntfy, Telegram, email, Discord, Slack and webhooks, and when they fire |
| [Settings](settings.md) | Every setting on the Settings page, with defaults and limits |

## Run it

| Page | What's in it |
| --- | --- |
| [Configuration reference](configuration.md) | Every flag and environment variable, the data directory |
| [Upgrading, backups and rolling back](upgrading.md) | Upgrading, `bield backup`, restoring, going back a version |
| [Uninstalling](uninstalling.md) | Removing Bield, its node, its key and the agents |
| [FAQ](faq.md) | Common questions and what to check when something doesn't work |
| [Benchmarks](benchmark.md) | What Bield costs with 200 devices and 90 days of history |

The threat model and how to report a vulnerability are in [SECURITY.md](../SECURITY.md). To work on Bield, see [CONTRIBUTING.md](../CONTRIBUTING.md).

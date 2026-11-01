# FAQ

## Setting up

### Do I need a Tailscale account?

You need a tailnet, which is Tailscale's, or a [Headscale](headscale.md) server that you run. Bield has no passwords. It asks the tailnet who is connecting. To look around first, `bield serve --demo` needs no account ([Demo mode](demo.md)).

### Do I have to open a port or set up a reverse proxy?

No. Bield listens only on the tailnet. In embedded mode that's port 443 of its own node (with port 80 redirecting to it), or port 80 alone when the tailnet has no HTTPS certificates, and in host mode it's port 4700 on the machine's tailnet addresses. The login page that embedded mode shows at first is on `127.0.0.1:4700`, which is the machine itself. Bield trusts the tailnet to say who is connecting, so don't publish it on the open internet.

### Embedded or host mode?

Use embedded if you aren't sure. It works anywhere, including in a container, and Bield shows up as its own machine. Use host mode when the machine already runs Tailscale and you want Bield to be that machine. [Bield and Tailscale](tailscale.md) has the table.

### Bield asks me to log in to Tailscale every time it starts.

It forgot its node, which lives in `tailscale/` in the data directory. In Docker that means the container has no volume for `/data`, or a different one each time. Use the volume in `compose.yaml`. With the install script, check that `/var/lib/bield` survives.

### The page says it's not secure, or the browser warns about HTTP.

Bield serves HTTPS when the tailnet has HTTPS certificates turned on. Turn on MagicDNS and HTTPS in the [DNS settings](https://login.tailscale.com/admin/dns). Without them, Bield serves plain HTTP, which WireGuard still encrypts. Headscale makes no certificates, so it's always HTTP there. See [HTTPS](tailscale.md#https).

### Which systems does it run on?

The release archives are for Linux and macOS, on amd64 and arm64, and the Docker image is for Linux. Bield measures the machine it runs on, and watches its systemd, only on Linux. Docker works wherever Bield can reach the socket. On a Mac it still manages the tailnet, and the [agent](agent.md) reports the Mac's metrics. There's no Windows build, and no agent for Windows, iOS or Android, though those devices show up in the device list.

## Signing in and roles

### I'm signed in, but Bield says I can't view it.

Bield knows you, and you have no role. By default, only admins and viewers can see anything. An admin runs `bield admin add-viewer you@example.com`, or sets who can view to the whole tailnet. [Admins and viewers](tailscale.md#admins-and-viewers) has the details. The page shows the command to run.

### Bield thinks I'm anonymous.

A request is anonymous when Tailscale can't say which user it comes from: a tagged device, an address Tailscale doesn't know, or Bield's own machine in host mode. In host mode, open Bield from another device. `GET /api/me` says who Bield thinks you are, and why not.

### I'm locked out, and no admin can sign in.

On the machine Bield runs on, `bield admin add you@example.com` adds an admin straight in the database, even while Bield is running. For the install script, run it as the `bield` user with `BIELD_DATA_DIR` set ([Installing Bield](install.md#running-it)). In Docker: `docker compose exec bield bield admin add you@example.com`.

### Who is the first admin?

Whoever logs Bield in to Tailscale, and anyone in `BIELD_ADMINS`. If Bield's node is tagged, it has no owner, so set `BIELD_ADMINS`.

## Machines and metrics

### A machine shows no CPU or memory.

Tailscale doesn't report those. Install [the agent](agent.md) on it. Bield measures the machine it runs on by itself, if it's Linux.

### A laptop or phone keeps raising "offline" alerts.

Turn off **Alert when offline** in the device's settings (it's off for phones and tablets already), or raise the offline threshold for it or for everything, in [Settings](settings.md#alerts). A device is only alerted about once Bield has seen it online.

### The terminal or files page says the machine has no SSH access.

Bield has to log in to the machine first. Open the machine's page and set up the **SSH access** card ([SSH access](ssh.md)).

### The terminal says the host key changed.

Bield pinned the machine's SSH host key the first time and refuses a different one. If you reinstalled the machine, press **Trust the new key** on its SSH access card. If you didn't, find out why before you do. See [Host keys](ssh.md#host-keys).

### Docker says Bield can't reach it.

The user Bield runs as can't use the Docker socket. With the install script, `sudo usermod -aG docker bield` and restart ([Installing Bield](install.md#docker-on-the-same-machine)). In a container, mount the socket and add its group ([Bield in Docker](docker.md#managing-the-machines-containers)). For another machine, the SSH user must be able to use Docker. The `docker` group is as good as root, so decide that on purpose.

### Why can't a viewer see a container's logs?

Viewers get a smaller view of containers, on purpose: no logs, commands, environment variables or inspection, since those can hold secrets. Admins get all of it. See [What viewers see](pages.md#what-viewers-see).

## Data and privacy

### Where does Bield keep its data?

In the data directory: a SQLite database, the embedded node's identity, and Bield's SSH key. [The configuration reference](configuration.md#the-data-directory) lists it, and [Upgrading, backups and rolling back](upgrading.md) says how to back it up and restore it, including onto another machine.

### Does Bield send my data anywhere?

Bield has no analytics and doesn't check for updates. What leaves your machine:

- The embedded node talks to Tailscale (or your Headscale server), like any node. It also uploads its logs to Tailscale unless you set `TS_NO_LOGS_NO_SUPPORT=true`.
- The three default checks in the group Internet reach `1.1.1.1`, `cloudflare.com` and `google.com`. Delete them on the Checks page to stop that.
- Notification channels send to the services you set up.

### Is it safe to store notification tokens in Bield?

They're in the database as text, and the database file is readable only by Bield's user. Backups hold them too. Bield never shows a saved secret again. See [Notifications](notifications.md).

### How much does it cost to run?

With 12 devices it uses about 0.4% of a core and 59 MB of memory. With 200 devices and 90 days of history, about 3% of a core and 129 MB, and a database of 580 MB. The numbers are from a small VPS. See [the benchmark](benchmark.md).

## Alerts and notifications

### How do I get alerts on my phone?

Add an ntfy channel in Settings, Notifications, and subscribe to its topic in the ntfy app. Telegram, email, Discord, Slack and a generic webhook work too ([Notifications](notifications.md)).

### An alert opened and resolved by itself. Is something wrong?

An alert resolves a minute after its condition goes away, and a condition that comes and goes stays one alert. If one keeps flapping, raise the threshold or the number of failures in a row, or lengthen the CPU window.

### I acknowledged an alert, and it came back.

Acknowledging an alert is for that alert. If the problem goes away and returns after the alert resolved, it's a new alert. A snooze ends at its time, and the alert then counts as open again if the problem is still there.

## Upgrading and developing

### How do I upgrade, and can I go back?

Run the install command again, or pull the new image. Bield saves a backup first and migrates the database when it starts. Going back needs the older binary and the backup from before the upgrade. See [Upgrading, backups and rolling back](upgrading.md).

### Bield says "the database was written by a newer version".

You started an older Bield on a database that a newer one has already changed. Bield refuses, so it can't damage it. Install the newer version again, or [roll back](upgrading.md#rolling-back) with a backup from before the upgrade.

### Is there an API?

The web app uses an HTTP API under `/api`. It isn't documented or versioned yet, so it can change in any release, and it's better not to build on it. A request that changes something needs an `X-Bield` header ([Programs that call the API](tailscale.md#programs-that-call-the-api)).

### How do I report a bug or a security problem?

Bugs go in a [GitHub issue](https://github.com/manasseh-fikadu/bield/issues). Security problems go through GitHub's private reporting, as [SECURITY.md](../SECURITY.md) says.

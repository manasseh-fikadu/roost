# Settings

**Settings** is for admins. It has six tabs, and `/settings?tab=alerts` (or `notifications`, `units`, `retention`, `access`) links to one. Each tab saves on its own, Bield uses a change at once, and every change is a line in the [activity log](pages.md#activity). Viewers don't see the page. If two admins edit at the same time, each save only writes the fields its admin changed.

Environment variables and flags are a separate thing. They set up how Bield starts, and they're in the [configuration reference](configuration.md).

## General

| Setting | Default | What it does |
| --- | --- | --- |
| Name | `Bield` | What this Bield is called. It's in the sidebar and in the title of every page, and notification messages say it. Up to 40 characters on one line, with no space at either end. |
| Public URL | empty | The address that people and agents reach Bield at, if it differs from Bield's own address on the tailnet, such as a name from a proxy. Bield writes it into the agent's install command and into the links in notifications. Empty means Bield's own address on the tailnet. It takes a scheme, a host and maybe a port, such as `https://bield.example.ts.net`, and no path. |

The **First-run setup** card on this tab reopens [the setup](pages.md#first-run-setup).

## Alerts

Bield raises an alert when a rule's condition is true, and resolves it after the condition has been gone for a minute. Turning a rule off resolves its open alerts and hides the matching warning from the dashboard. The thresholds apply to every device unless [a device overrides them](#per-device-thresholds).

| Rule | Alert | Setting | Default | Severity |
| --- | --- | --- | --- | --- |
| Device offline | A device has left the tailnet. | Minutes offline before an alert (1 to 10080) | 10 | Warning |
| Key expiring | A device's Tailscale key is about to expire. | Days left before it does (1 to 365) | 14 | Warning, critical in the last 48 hours |
| Agent stopped | An [agent](agent.md) hasn't sent a heartbeat for 90 seconds. | | | Warning |
| Disk full | A device's disk is filling up. | Warning at, critical at (percent) | 85, 95 | Warning, then critical |
| Memory | A device uses most of its memory. | Threshold (percent) | 92 | Warning |
| CPU | A device has been busy for a while. | Threshold (percent), over minutes (1 to 60) | 90, 5 | Warning |
| Checks | A [check](pages.md#checks) keeps failing. | Failures in a row before an alert (1 to 100) | 2 | Critical, and warning for a device that doesn't answer pings |
| Containers | A Docker container on Bield's machine stopped unexpectedly. | | | Critical or warning, see below |
| Systemd units | A unit on Bield's machine failed, or a watched one isn't running. | | | Warning |
| Bield's own jobs | Something Bield gathers, such as the Tailscale status or Docker, keeps failing. | | | Warning |

All ten rules are on by default. A few details:

- A device only alerts when it's offline if it has been seen before and its **Alert when offline** switch is on. That switch is on for everything except phones and tablets.
- Disk, memory and CPU alerts need measurements. Bield measures the machine it runs on if that's Linux, and every other machine needs [the agent](agent.md). The CPU figure is the average over the window you set.
- Container alerts are about the containers on the machine Bield runs on. A container that is unhealthy or restarting alerts as a warning. One that is dead, was killed for using too much memory, or exited with an error alerts as critical. One that exited cleanly (exit code 0, 130, 137 or 143) alerts as a warning only if its restart policy is `always` or `unless-stopped`, so a one-shot job that finishes doesn't alert.
- A unit alerts as failed whether Bield watches it or not. A [watched unit](#watched-units) that is inactive also alerts.
- **Restore defaults**, on the Alerts and Retention tabs, puts that tab's values back. It doesn't save until you press **Save changes**.

### Per-device thresholds

A device's page has an **Edit** button, which opens its settings: a display name, its type, notes (viewers can read them), the **Alert when offline** switch, and thresholds for **Disk warning**, **Disk critical**, **Memory**, **CPU** and **Offline after**. A field you leave empty uses the setting above. A critical disk level below the warning one is raised to the warning one. The CPU window and the other thresholds aren't a device's to change.

## Notifications

Channels, reminders and quiet hours. See [Notifications](notifications.md).

| Setting | Default | What it does |
| --- | --- | --- |
| Remind every N hours | 0 (off) | Sends another message about an alert that is still open and not acknowledged. From 1 to 168. |
| Quiet hours | off | Holds messages back from a start time to an end time. Defaults to 22:00 until 07:00 when you turn it on. |
| Time zone | your browser's | The zone the quiet hours are in, such as `Europe/Berlin`. If you empty the field, it's the zone Bield's machine is in. |
| Still send critical alerts | on | Lets critical messages through the quiet hours. |

## Watched units

Bield lists the systemd units of the machine it runs on on the hub's page, and alerts when a watched one isn't running. Failed units always show, watched or not. The tab lists the services the machine has, with a search box, so you can pick from them. A unit is either a system unit, such as `ssh.service`, or one of the user Bield runs as, which Bield writes as `user:app.service` in the settings.

The default list is `ssh.service`, `sshd.service`, `docker.service`, `tailscaled.service` and `cron.service`. Bield leaves out the ones the machine doesn't have, since `ssh` is `sshd` on some systems.

This needs systemd, and for user units a running user session. On a machine without systemd, such as macOS or a container, the tab says Bield can't read systemd and the rule has nothing to do.

## Retention

Bield deletes history older than these limits within the hour. Shortening one asks you to confirm, because the deletion can't be undone.

| History | Default | Most | What it covers |
| --- | --- | --- | --- |
| Activity log | 30 days | 365 | Events, and resolved alerts with their notes. |
| Device history | 7 days | 90 | What devices used, and the round-trip times to them. |
| Check results | 7 days | 90 | Each result of every check, and the health score. |

Device history keeps the last 48 hours as measured and averages of 5 minutes and of an hour for the rest. The per-process breakdown (what used a machine's CPU) and what each container used are kept for 7 days at most, whatever you set. [Benchmarks](benchmark.md) says what 90 days costs.

## Access

Who can use Bield: the people with a role, and **Who can view**. It's the same as `bield admin` on the command line, and it's explained in [Admins and viewers](tailscale.md#admins-and-viewers). You can add a login as an admin or a viewer, change someone's role, or remove it. Bield always keeps one admin, and asks you to confirm before you demote yourself.

**Who can view** is `listed` by default, which means only people with a role. `tailnet` makes anyone who signs in from the tailnet a viewer.

The **Tailscale** card shows the mode Bield joined the tailnet in. It's read only, since it's set when Bield starts.

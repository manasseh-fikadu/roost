# Security policy

Roost opens shells, moves files and controls containers on every machine it manages, so a security bug in Roost can be a security bug on all of them. Thank you for reporting one privately.

## Report a vulnerability

Please don't open a public issue. Report it through GitHub instead: on the [Security tab](https://github.com/manasseh-fikadu/roost/security), choose **Report a vulnerability**. Include:

- what an attacker can do, and what access they need first (a device on the tailnet, a viewer account, a device running the agent, and so on)
- steps to reproduce it, or a proof of concept
- the Roost version (`roost version`) and how it's set up

What happens next:

- You'll get a reply within 7 days.
- We'll work out a fix and a disclosure date with you. The aim is a fixed release within 90 days of your report, sooner for serious problems.
- Once the fix is out, a GitHub security advisory publishes the details, with a CVE where one applies. It credits you, unless you'd rather not be named.

## Supported versions

Roost hasn't had a release yet. After the first one, security fixes go into the latest release only, so upgrading is how you get them.

| Version | Gets security fixes |
| --- | --- |
| Latest release | Yes |
| Older releases | No |

## Scope

These are the kinds of problems we most want to hear about:

- Using Roost without the tailnet identity it expects, or with more rights than your role gives you (viewer versus admin).
- Running commands, reading or writing files, or controlling containers on a machine beyond what your role allows.
- Agent enrollment and tokens: posing as another device, or using a device's token for anything beyond reporting its metrics.
- The install script and the agent: any way for someone other than the user who installed them to run code through them.
- Secrets or tokens showing up in logs, the web UI or the API.

These are out of scope:

- Bugs in Tailscale, Headscale, Docker or the operating system that Roost doesn't make worse. Please report those to their maintainers.
- Admins doing things admins are allowed to do. Opening a shell on a managed machine is a feature.
- Missing hardening, such as a security header, without a way to exploit it.

## Threat model

Roost is a control plane for a tailnet: it logs in to your machines over SSH, so whoever controls Roost controls them. This is what it trusts, what it checks and what it leaves to you. It hasn't had an outside review yet. If you try to break it, [report what you find](#report-a-vulnerability).

### Who can do what

- **The tailnet is the login.** Roost listens only on the tailnet, and asks Tailscale who is on the other end of each connection. There are no passwords or cookies, and no header can change the answer. Someone Tailscale can't name is anonymous, and so are tagged devices, which belong to no one, and Roost's own machine in host mode, since any program there would otherwise act as its owner.
- **Anonymous and role-less callers** get the app's files, the health check, the install scripts for agents (with a code that works only on the machine it was made for), and nothing else. **Viewers** see the overview, with the secret fields left out: container environments, commands, check targets and the like are tagged and emptied for everyone but admins. **Admins** can do everything below.
- **An admin has a shell on every machine that has SSH access set up, as the user Roost logs in as.** Treat the admin list like sudoers. Roost doesn't try to confine admins, and the limits on what they can do (see below) are guard rails against mistakes and against what a page or a file could trick a browser into, not walls. Anyone who can use an admin's device, or run a program on it, can act as that admin.
- **Reaching Roost is up to your Tailscale policy.** Every device on the tailnet can open Roost's port, and gets the answer above. Use ACLs if only some devices should.

### In the browser

- A request must name Roost itself in its `Host` header, so a page that points its own domain at Roost's address can't talk to it as your device.
- Anything that changes something needs the `X-Roost` header, which a page on another site can't send without Roost's permission, which it never gives. WebSockets, which browsers let any page open, must come from a page on Roost (their `Origin` is checked).
- Every response says `nosniff`, `Referrer-Policy: no-referrer` and `Cross-Origin-Resource-Policy: same-origin`. The app's pages have a strict `Content-Security-Policy`: scripts only from Roost itself and the one inline script that starts the app (by its hash, with no `unsafe-inline` or `unsafe-eval`), no plugins, no remote images, and `frame-ancestors 'none'`, so another site can't frame the terminal and click through it.
- Files from a machine are served from Roost's own address, so they get a CSP that runs no script, even in an SVG, and `nosniff`. The Markdown viewer builds its page from the text, with no raw HTML, and the Word viewer shows its document in a sandboxed frame.

### Machines and agents

- A machine enrolls with an install command whose code works for an hour, on one machine, once. The agent gets a secret that works only from the machine it was made for, and Roost keeps only a hash of codes and secrets. An agent can report its metrics and nothing else: it can't make Roost run or fetch anything.
- Everything a machine says is untrusted: heartbeats, file names, container names, process lists, log lines, the banner of an SSH server. Roost shows it as text, never as HTML, and limits its size and how often it comes (a few requests a second per machine, and a heartbeat is capped at 256 KiB).
- Commands over SSH are fixed `sh` scripts. A name, a path or a session is passed as an argument, in single quotes that every login shell reads the same (sh, dash, bash, zsh and fish), never joined into the script. The tests run names with quotes, newlines, a leading `-`, `$()`, backticks, backslashes and unicode through each of them. tmux session names and working directories are checked, since tmux reads `#` in them as a format.
- Roost pins a machine's SSH host key the first time it connects, and refuses to log in if it changes until an admin resets it. Its key lives in the data directory (mode 0600), and the line it adds to `authorized_keys` only works from Roost's own addresses.

### Files, Docker and what Roost reaches out to

- **Files** go over SFTP as the SSH user, so the machine's own permissions are the limit. Names that arrive from a request can't hold a slash or be `..`, and uploads are limited to 10 GiB a file. Deleting, copying, moving, renaming, archiving and searching treat a symlink as the link, not as the folder or file it points to, and system folders, the home folder and the ones above it can't be deleted or moved. Overwriting and editing a file does follow a link to the file it points to, as a shell would.
- **Docker** is reached through the machine's own socket over SSH. A container ID has to be 1 to 128 letters, digits, `_`, `.` or `-` and start with a letter or digit. Roost calls a fixed set of read endpoints (list, inspect, top, logs, stats, and the lists of images, volumes and networks) and can start, stop and restart a container. There's no way to send Docker anything else, so no create, exec or prune.
- **Checks and notifications** connect from Roost's machine to wherever an admin points them. That's the point of them, so loopback and private addresses are open. Link-local addresses are not (169.254.0.0/16 and fe80::/10, where clouds put their metadata services), nor are the well-known metadata names. A check keeps only a status line, never the body of what it fetched, it doesn't follow redirects, and what a server that isn't HTTP said isn't shown. A notification channel's test shows its admin up to 300 characters of an error reply, so a webhook can read a little of what answers on a private address. A name that an admin's own DNS points at a link-local address isn't caught, since an admin can reach it from a terminal anyway.

### The activity log

It records who did what and when: terminals opened, files uploaded, edited, created, moved, copied, renamed, deleted and archived, files and container logs read (once for a file or container while it's being read, however many requests that takes), container actions, role and settings changes, checks, notification channels, SSH access and host keys (including a key pinned for the first time), and agents installed and revoked. File and container names appear with control characters written out, so a file can't make a line that no one wrote.

It doesn't record what was typed in a terminal or what a file held, or requests that were refused or failed. It isn't tamper-proof: admins can change how long it's kept, and anyone with the data directory can edit the database. It's there so admins can see what the others did, not to hold an admin to account against their will.

### What Roost doesn't defend against

- **A compromised Roost machine.** It holds an SSH key that can log in to every machine that has access set up, and the database with notification tokens, which isn't encrypted. Keep the data directory private (Roost creates it that way), and treat the machine like a bastion host.
- **A compromised admin account or device**, for the reason above.
- **Tailscale or Headscale itself**, and the machines' own SSH servers and operating systems.
- **A tailnet member who wants to make Roost slow.** The routes for agents have rate limits. The rest are for signed-in roles, which an admin gives out.

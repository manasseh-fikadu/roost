# SSH access, terminals and files

Bield opens a shell, moves files and reads Docker on a machine by logging in to it over SSH. Nothing is installed on the machine for this, and only admins can use it. This page covers setting that access up, what Bield does with it, and what to watch for.

A machine has SSH access when the **Status** row of the **SSH access** card on its page is green. Without it, [the agent](agent.md) still reports metrics, but **Terminal**, **Files** and the other machines' **Docker** pages have nothing to log in with.

## Two ways to log in

| | Bield's key | Tailscale SSH |
| --- | --- | --- |
| How it works | Bield logs in with an SSH key of its own, which you authorize on the machine | Tailscale's own SSH server on the machine lets Bield in, as your tailnet policy says |
| Needs on the machine | `sshd`, and a line in the user's `authorized_keys` | `tailscale set --ssh`, and a rule in the tailnet policy |
| Port | The one you give, 22 by default | 22 |

Bield's key is an Ed25519 key that Bield makes the first time it needs one, in `ssh/id_ed25519` in the [data directory](configuration.md#the-data-directory). It has the comment `bield@<node name>`.

### Setting up Bield's key

Open the machine's page and find the **SSH access** card. It shows a command to run on that machine, as the user Bield should log in as. It adds one line to that user's `~/.ssh/authorized_keys`, if it isn't there already, and fixes the modes of `~/.ssh` and the file. The line looks like this:

```text
from="100.101.0.5,fd7a:115c:a1e0::5",no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA... bield@bield
```

The key only works from Bield's own tailnet addresses, so a stolen copy of the key is no use from another machine. For the machine Bield runs on, the line also lists the loopback addresses. Agent and X11 forwarding are off for it. Port forwarding is left on, because Bield reaches the Docker socket through it.

Then tell Bield which user that was, and press **Connect**. The port is 22 unless you change it. On a Mac, turn on **Remote Login** first, under System Settings, General, Sharing.

Bield then checks the login and shows its state, the login it uses, the host key, and whether the machine has tmux and Docker. It checks a new machine at once, a failing one every 5 minutes, and a working one every hour. **Remove access** in the card's menu forgets the machine. It doesn't remove the line from `authorized_keys`. Delete the line that ends in `bield@...` yourself.

### The machine Bield runs on

Bield reaches its own machine on `127.0.0.1`, and falls back to the machine's tailnet address if sshd only listens there. The card offers **Authorize Bield as `<user>`**, which adds the key to the `authorized_keys` of the user Bield runs as, with one click.

- **Install script:** Bield runs as `bield`, an account with no login shell, and sshd doesn't run commands for such an account. Don't authorize `bield`. Run the command the card shows as your own user, and enter that user.
- **Docker:** inside a container, `127.0.0.1` is the container itself, which has no SSH server, so the loopback login doesn't reach the host. If the host is on the tailnet, set it up as an ordinary machine.

### Tailscale SSH

If a machine runs [Tailscale SSH](https://tailscale.com/kb/1193/tailscale-ssh) (`sudo tailscale set --ssh`), the card says so and only asks for the user. Bield never uses its key there. Bield tries it, and the tailnet policy decides. If the policy says no, the card says why and shows a rule to add under `ssh` in the policy file:

| Why | What it means |
| --- | --- |
| The rule needs a check | The policy's `check` action wants someone to confirm in a browser, which Bield can't do. Use an `accept` rule for Bield. |
| The policy doesn't let Bield's node in | Bield's node, or its tag, isn't a source in any `ssh` rule for that machine. |
| The policy doesn't let that user in | Or the machine has no account for it. |

The rule it shows is a starting point. A tagged device can't be the source of a `check` rule, and a rule can't name an untagged machine that belongs to another user, so the card sometimes leaves a placeholder such as `tag:servers`. On Headscale, the user is written `alice@`, as its policy wants ([Bield and Headscale](headscale.md#tailscale-ssh)). Press **Check** on the machine once the policy is saved.

Tailscale SSH answers on port 22 of the node's tailnet address, so a machine whose own sshd listens somewhere else can only be reached with Bield's key.

### Host keys

The first time Bield connects to a machine, it pins the machine's host key and writes the fingerprint in the [activity log](pages.md#activity). If the key changes later, Bield refuses to log in, because that can mean someone is posing as the machine. The card shows the pinned key and the offered one. After a reinstall, press **Trust the new key**. **Forget the host key** in the menu pins whatever the machine offers next, which is only safe if you trust the network right now.

## Terminal

**Terminal** is a shell in the browser, in tabs. Pick a machine at the top, press **New**, and choose a **tmux session** or a **Shell that ends with its tab**. The **Terminal** button on a machine's page does it for you: it opens a tab at once.

If the machine has tmux, a tmux tab is a session called `web-1`, `web-2` and so on. Closing the tab leaves the session running, and what's in it too, so a build keeps going. Bield saves tmux tabs in the browser, so a reload brings them back. The **Terminal** button on a machine's page attaches to the newest `web-` session that no one is using, or starts a new one. **Sessions** lists every tmux session on the machine, so you can attach to one in a tab or end it. A plain shell, which is all a machine without tmux has, ends with its connection, and its tab doesn't come back after a reload.

The terminal shows what the shell sends, and Bield never records what you type. The [activity log](pages.md#activity) says that a terminal was opened, by whom, and on which machine.

**Open a terminal here** in the file manager starts a terminal in the folder you're looking at.

## Files

**Files** is a file manager for a machine's disk, over SFTP as the SSH user. The machine's own permissions are the limit, so a user that can't read a folder can't read it here either.

- Browse with a path bar, bookmarks, search in a folder and sorting. Hidden files can be shown.
- Open a file to preview it: text and code with syntax colors, Markdown, images, video, audio, PDFs, Word documents and spreadsheets.
- Edit text files in the browser. If the file changed on the machine since you opened it, Bield refuses the save and tells you, instead of overwriting it.
- Upload files or a folder, by dragging or from the menu. Uploads can be up to 10 GiB a file, they're written to a hidden temporary file, and the name changes when the upload is complete.
- Download a file, or a selection as a `.tar.gz`.
- Rename, duplicate, copy, move, delete and make folders or empty files. System folders, the home folder and the ones above it can't be deleted or moved.
- Keyboard: arrows, `Enter`, `Space`, `Backspace` for up a folder, `/` to search, `F2` to rename, `Delete`. Press `?` for the list.

Remote images in Markdown files don't load, since Bield's [content security policy](../SECURITY.md#in-the-browser) allows images from Bield only. The activity log has one line when files are uploaded, edited, moved, copied, renamed, deleted or archived, and one when a file's content is read.

## Docker on other machines

The Docker page reaches another machine's Docker through the same SSH login, by forwarding its Docker socket. Nothing needs to listen on a port, and you don't have to turn on Docker's TCP API. Bield finds the system socket, a rootless one and Docker Desktop's. The SSH user has to be able to use the socket. For the system Docker, that means being in the `docker` group, which is as good as root on the machine. See [the Docker section of SECURITY.md](../SECURITY.md#files-docker-and-what-bield-reaches-out-to).

## Install an agent over SSH

If a machine has SSH access, the dialog for adding its agent can run the install for you instead of showing you a command to paste. See [The agent](agent.md#installing-it).

## Good practice

- Use a user that has only what you want admins to have. Whatever that user can do, every admin can do from Bield.
- Don't hand out SSH to your tailnet just because Bield has it. The `from=` option makes the key useless anywhere but Bield.
- Back up the data directory, since `ssh/id_ed25519` is in it. Without the key, you set access up again.

## Tried, and not tried

Tried: Bield's key against OpenSSH on Linux, tmux, the Docker socket through the SSH forward, and a Tailscale SSH policy that holds the login for a `check`, on a real tailnet.

Not tried on a real tailnet: a Tailscale SSH login that the policy accepts. The tests cover that, and the other refusals, with a fake server. Not tried at all: a Mac as the target, and a machine other than Bield's own, reached over the tailnet with Bield's key.

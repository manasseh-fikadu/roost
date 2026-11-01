# Uninstalling Bield

Bield leaves traces in three places: the machine it runs on, your tailnet, and the machines it manages. This page goes through each. Do them in this order, because the first step needs Bield to be running.

If you might come back, save a backup before you delete anything: `bield backup` (see [Upgrading, backups and rolling back](upgrading.md#backing-up)). A backup holds Bield's node identity and SSH key, so restoring it brings the whole thing back.

## 1. Remove the agents

An agent keeps running and sends heartbeats to an address that no longer answers. On each machine that runs one:

```sh
curl -fsSL https://bield.example.ts.net/api/agent/uninstall.sh | bash
```

It stops the agent, tells Bield to forget it, and deletes the agent's files. The Agents page in Bield has an **Uninstall** button for machines it has SSH access to, which does the same. See [The agent](agent.md#uninstalling).

If Bield is already gone, the agent still has to go. On Linux:

```sh
systemctl --user disable --now bield-agent.service
rm -f ~/.config/systemd/user/bield-agent.service
rm -rf ~/.local/share/bield-agent ~/.config/bield-agent
systemctl --user daemon-reload
```

On macOS:

```sh
launchctl bootout "gui/$(id -u)/dev.bield.agent"
rm -f ~/Library/LaunchAgents/dev.bield.agent.plist ~/Library/Logs/bield-agent.log
rm -rf ~/.config/bield-agent "$HOME/Library/Application Support/bield-agent"
```

## 2. Remove Bield

### Install script

```sh
sudo systemctl disable --now bield
sudo rm /etc/systemd/system/bield.service /usr/local/bin/bield
sudo systemctl daemon-reload
```

That leaves the data in place, so installing again picks up where you left off. To delete it too, which is the database, Bield's tailnet node and its SSH key, and the settings file:

```sh
sudo rm -rf /var/lib/bield /etc/bield
sudo userdel bield
```

If you gave the `bield` user access to Docker, `userdel` takes that with it. See [Installing Bield](install.md#removing-it).

### Docker

```sh
docker compose down        # stops Bield, and keeps its data in the volume
docker compose down -v     # also deletes the volume: the database, node and SSH key
docker rmi ghcr.io/manasseh-fikadu/bield
```

If you mounted a directory in place of the volume, delete the directory yourself.

### A release archive or a build from source

Stop the process, and delete the `bield` binary. The data is in `~/.config/bield` (`~/Library/Application Support/bield` on macOS), or in the directory you gave with `--data-dir` or `BIELD_DATA_DIR`. Delete it to forget everything. If you wrote a launchd job or a systemd unit for it, remove that too.

## 3. Clean up your tailnet

- **Embedded mode:** Bield's node stays in your [machine list](https://login.tailscale.com/admin/machines) until you remove it there. On Headscale, run `headscale nodes delete`. Deleting the data directory doesn't remove it.
- **Host mode:** Bield wasn't a node. If you ran `tailscale set --operator=bield` or similar so that Bield could fetch the HTTPS certificate, run `sudo tailscale set --operator=` to clear it.
- **Policy:** take out the `tag:bield` entries you added to `tagOwners` and `grants` in the [tailnet policy file](tailscale.md#tailnet-policy), and any `ssh` rule you added for Bield.
- **Auth keys:** revoke the auth key you made for Bield, if it hasn't expired.

## 4. Take Bield's key off the machines

For every machine where you set up Bield's key, delete the line that ends in `bield@<name>` from the `authorized_keys` of the user Bield logged in as:

```sh
sed -i '/ bield@/d' ~/.ssh/authorized_keys     # on macOS: sed -i '' ...
```

Check the file afterwards. The line starts with `from="` and mentions `no-agent-forwarding`.

Machines that Bield reached with Tailscale SSH have nothing to remove, except the policy rule from step 3.

## What's left

Bield keeps nothing outside its data directory, apart from what the steps above remove. The embedded node uploaded its logs to Tailscale unless you set `TS_NO_LOGS_NO_SUPPORT=true`, and Tailscale's own retention applies to them.

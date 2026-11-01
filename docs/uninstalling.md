# Uninstalling Roost

Roost leaves traces in three places: the machine it runs on, your tailnet, and the machines it manages. This page goes through each. Do them in this order, because the first step needs Roost to be running.

If you might come back, save a backup before you delete anything: `roost backup` (see [Upgrading, backups and rolling back](upgrading.md#backing-up)). A backup holds Roost's node identity and SSH key, so restoring it brings the whole thing back.

## 1. Remove the agents

An agent keeps running and sends heartbeats to an address that no longer answers. On each machine that runs one:

```sh
curl -fsSL https://roost.example.ts.net/api/agent/uninstall.sh | bash
```

It stops the agent, tells Roost to forget it, and deletes the agent's files. The Agents page in Roost has an **Uninstall** button for machines it has SSH access to, which does the same. See [The agent](agent.md#uninstalling).

If Roost is already gone, the agent still has to go. On Linux:

```sh
systemctl --user disable --now roost-agent.service
rm -f ~/.config/systemd/user/roost-agent.service
rm -rf ~/.local/share/roost-agent ~/.config/roost-agent
systemctl --user daemon-reload
```

On macOS:

```sh
launchctl bootout "gui/$(id -u)/dev.roost.agent"
rm -f ~/Library/LaunchAgents/dev.roost.agent.plist ~/Library/Logs/roost-agent.log
rm -rf ~/.config/roost-agent "$HOME/Library/Application Support/roost-agent"
```

## 2. Remove Roost

### Install script

```sh
sudo systemctl disable --now roost
sudo rm /etc/systemd/system/roost.service /usr/local/bin/roost
sudo systemctl daemon-reload
```

That leaves the data in place, so installing again picks up where you left off. To delete it too, which is the database, Roost's tailnet node and its SSH key, and the settings file:

```sh
sudo rm -rf /var/lib/roost /etc/roost
sudo userdel roost
```

If you gave the `roost` user access to Docker, `userdel` takes that with it. See [Installing Roost](install.md#removing-it).

### Docker

```sh
docker compose down        # stops Roost, and keeps its data in the volume
docker compose down -v     # also deletes the volume: the database, node and SSH key
docker rmi ghcr.io/manasseh-fikadu/roost
```

If you mounted a directory in place of the volume, delete the directory yourself.

### A release archive or a build from source

Stop the process, and delete the `roost` binary. The data is in `~/.config/roost` (`~/Library/Application Support/roost` on macOS), or in the directory you gave with `--data-dir` or `ROOST_DATA_DIR`. Delete it to forget everything. If you wrote a launchd job or a systemd unit for it, remove that too.

## 3. Clean up your tailnet

- **Embedded mode:** Roost's node stays in your [machine list](https://login.tailscale.com/admin/machines) until you remove it there. On Headscale, run `headscale nodes delete`. Deleting the data directory doesn't remove it.
- **Host mode:** Roost wasn't a node. If you ran `tailscale set --operator=roost` or similar so that Roost could fetch the HTTPS certificate, run `sudo tailscale set --operator=` to clear it.
- **Policy:** take out the `tag:roost` entries you added to `tagOwners` and `grants` in the [tailnet policy file](tailscale.md#tailnet-policy), and any `ssh` rule you added for Roost.
- **Auth keys:** revoke the auth key you made for Roost, if it hasn't expired.

## 4. Take Roost's key off the machines

For every machine where you set up Roost's key, delete the line that ends in `roost@<name>` from the `authorized_keys` of the user Roost logged in as:

```sh
sed -i '/ roost@/d' ~/.ssh/authorized_keys     # on macOS: sed -i '' ...
```

Check the file afterwards. The line starts with `from="` and mentions `no-agent-forwarding`.

Machines that Roost reached with Tailscale SSH have nothing to remove, except the policy rule from step 3.

## What's left

Roost keeps nothing outside its data directory, apart from what the steps above remove. The embedded node uploaded its logs to Tailscale unless you set `TS_NO_LOGS_NO_SUPPORT=true`, and Tailscale's own retention applies to them.

# Roost and Tailscale

Roost gets everything about your tailnet from Tailscale: the devices, how traffic reaches each one, and who is signing in. It connects in one of two modes, set with `--tailscale` or `ROOST_TAILSCALE`.

| | Embedded (default) | Host |
| --- | --- | --- |
| How it connects | Roost runs its own Tailscale node inside the process | Roost talks to the `tailscaled` already running on the machine |
| Shows up on the tailnet as | A new machine, `roost` by default | The machine it runs on |
| Needs on the machine | Nothing, so it works in a container | Tailscale, logged in, using a TUN device |
| Serves the app on | `https://roost.<tailnet>.ts.net`, port 443 | Port 4700 on the machine's tailnet IPs |

Either way, Roost serves the app only on the tailnet. Nothing on the machine's other networks can reach it.

## Embedded mode

```sh
roost serve
```

The first time, Roost isn't on your tailnet yet. Open the setup page at <http://127.0.0.1:4700> on the same machine, then log in to Tailscale with the link or the QR code. Roost also prints the login link in its log. To skip the browser, start Roost with an [auth key](https://tailscale.com/kb/1085/auth-keys) in `TS_AUTHKEY`:

```sh
TS_AUTHKEY=tskey-auth-... roost serve
```

After that first login, Roost remembers its node in `tailscale/` inside the data directory, and the setup page just links to Roost on the tailnet. The setup page shows only the login state. It never serves the app or the API, and it only answers requests addressed to `localhost` or an IP address, so another website can't read the login link. To serve it on a different address, such as in a container, use `--setup-listen` or `ROOST_SETUP_LISTEN`. Until Roost is logged in, anyone who can open the setup page can add Roost to their own tailnet, so keep it to your machine: in a container, publish the port on the host's loopback only (`-p 127.0.0.1:4700:4700`), or use `TS_AUTHKEY`. Roost warns in its log when the page listens on more than loopback.

The node's name is `roost`, or whatever you set with `--hostname` or `ROOST_HOSTNAME`.

### HTTPS

Turn on MagicDNS and HTTPS certificates in the [DNS settings](https://login.tailscale.com/admin/dns) of the admin console. Roost then serves the app on port 443 with its own certificate and redirects port 80 to it. Without them, it serves plain HTTP on port 80. WireGuard still encrypts that traffic, but browsers treat the page as insecure.

### Tailnet policy

If you log Roost in as yourself, its node has your access, and nothing more is needed. For a node that belongs to no one, give it a tag. Use an auth key that applies the tag, set `ROOST_ADMINS` (see [Admins](#admins)), and allow the tag in your [tailnet policy file](https://login.tailscale.com/admin/acls):

```jsonc
{
  "tagOwners": {
    "tag:roost": ["autogroup:admin"],
  },
  "grants": [
    // Everyone on the tailnet can open Roost.
    { "src": ["autogroup:member"], "dst": ["tag:roost"], "ip": ["tcp:443", "tcp:80"] },
    // Tagged servers that run the agent can send it their metrics.
    { "src": ["tag:server"], "dst": ["tag:roost"], "ip": ["tcp:443", "tcp:80"] },
    // Roost can reach every device, to ping it and to open SSH.
    { "src": ["tag:roost"], "dst": ["*"], "ip": ["tcp:22", "icmp:*"] },
  ],
}
```

The second grant matters even before you use SSH. Tailscale only tells a node about the peers its policy lets it talk to, so Roost can only list and ping the devices it can reach. Tagged nodes also don't expire, while a node logged in as a user needs a new login when its key expires (after 180 days by default).

### The machine Roost runs on

In embedded mode, Roost is its own node, so the machine it runs on shows up separately if it's on the tailnet too. Roost marks that device as the same machine when one of its tailnet IPs is on a local network interface. This works when the machine's Tailscale uses a TUN device, which is the default. In a container, or with userspace networking, Roost can't see those addresses, and the machine shows up like any other device. Roost doesn't compare host names, since its own node is named after Roost. On Linux, Roost measures that machine itself, so it turns away an agent there.

## Host mode

```sh
roost serve --tailscale host
```

Roost uses the machine's Tailscale through its local socket and listens on the machine's tailnet IPs, port 4700 by default (`--port` or `ROOST_PORT`). Tailscale must be running with a TUN device, not `--tun=userspace-networking`, so that those IPs exist on the machine. If Tailscale is logged out, Roost waits for `tailscale up`.

Roost serves HTTPS with the machine's certificate if the tailnet has HTTPS turned on and Tailscale lets Roost fetch it. Root can always fetch it. For another user, make that user the Tailscale operator:

```sh
sudo tailscale set --operator=$USER
```

Otherwise Roost falls back to plain HTTP and says so in its log. If Roost may fetch the certificate but fetching it fails, for example because Let's Encrypt is slow, Roost serves HTTPS anyway and asks for the certificate again on each connection.

## Signing in

Your tailnet is the login. For every request, Roost asks Tailscale which device the connection comes from and which user owns that device. It never reads who you are from a header, so nothing in a request can claim to be someone else.

- A device that belongs to a user signs that user in, as everywhere on Tailscale: anyone using your laptop is you to Roost.
- Tagged devices, which belong to no one, and addresses Tailscale doesn't know are anonymous.
- Roost's own node is anonymous too. In host mode, that node is the machine Roost runs on, so open Roost from another device. Any program on that machine can connect to Roost, and it shouldn't count as the machine's owner.
- `GET /api/me` says who Roost thinks you are, or why you're anonymous.

Tailscale identifies devices, not the people or programs using them, so anything running on a device you're logged in to acts as you. If a server runs other people's code, give its Tailscale node a tag, so that connections from it are anonymous.

### Admins and viewers

Roost has two roles:

- **Admins** can do everything: open terminals, change files, control Docker, use SSH access, and change settings, including who has a role.
- **Viewers** see the read-only dashboard. They can't change anything, and they don't see logs, files, process command lines or other secrets such as containers' environment variables.

Anyone else, and anyone anonymous, can only see that Roost is there and why they can't view it.

One setting decides who else can view Roost. By default, it's `listed`: only Roost's admins and viewers can, and anyone else who signs in sees the command an admin can run to make them a viewer. Set it to `tailnet` to make anyone who signs in from the tailnet a viewer unless they have a role of their own. That includes people you share Roost's machine with, since Tailscale names them too, so with `tailnet`, sharing that machine shares the dashboard of your whole tailnet.

When Roost has no admins yet, it makes the first ones as soon as it's on the tailnet: the user who owns Roost's node, which is whoever logged it in, and any logins in `ROOST_ADMINS` or `--admins`, separated by commas. A node logged in with a tagged auth key has no owner, so give it `ROOST_ADMINS`:

```sh
TS_AUTHKEY=tskey-auth-... ROOST_ADMINS=alice@example.com roost serve
```

After that, Roost ignores `ROOST_ADMINS`. Manage roles with `roost admin` on the machine Roost runs on. It changes the database directly, so it works while Roost is running and when no admin can sign in. Roost always keeps one admin, so it won't remove the last one or make them a viewer.

```sh
roost admin list                        # the admins and viewers, and who else can view Roost
roost admin add alice@example.com       # make alice an admin
roost admin add-viewer bob@example.com  # make bob a viewer
roost admin remove bob@example.com      # take bob's role away
roost admin who-can-view tailnet        # let anyone on the tailnet view; "listed" (the default) undoes it
```

Admins can make the same changes through `GET` and `PATCH /api/access` and `PUT` and `DELETE /api/access/users/<login>`, which the Settings page will use.

Only admins can see `GET /api/debug/tailnet`, and during development, the web UI that Roost proxies from Vite.

### Programs that call the API

A request that changes something, which is any method but GET, HEAD and OPTIONS, needs an `X-Roost` header with any value. A page on another website can't add one, so it can't make your browser change anything in Roost.

Roost also only answers requests addressed to its own name or tailnet IPs, so a website can't reach it by pointing its own domain at Roost's address, and it only accepts WebSockets opened by its own pages.

## What Roost watches

| What | How often | Where it comes from |
| --- | --- | --- |
| Devices and their state | Every 10 seconds | Tailscale's status |
| The path to each online device (direct, DERP relay or peer relay) and its round-trip time | Every minute | A Tailscale ping |
| UDP, IPv6, the public IP and the nearest DERP region | Every 10 minutes | A netcheck, as `tailscale netcheck` runs it |

Tailscale's local API has no netcheck, so Roost runs the check in its own process, as the `tailscale` command does. It runs on the same machine as the node, so it sees the same network. `GET /api/debug/tailnet` shows admins what Roost currently knows.

## Logs

Like every Tailscale client, the embedded node uploads its logs to Tailscale, which their support relies on. To turn that off, set `TS_NO_LOGS_NO_SUPPORT=true`.

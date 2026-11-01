# Bield and Tailscale

Bield gets everything about your tailnet from Tailscale: the devices, how traffic reaches each one, and who is signing in. It connects in one of two modes, set with `--tailscale` or `BIELD_TAILSCALE`.

| | Embedded (default) | Host |
| --- | --- | --- |
| How it connects | Bield runs its own Tailscale node inside the process | Bield talks to the `tailscaled` already running on the machine |
| Shows up on the tailnet as | A new machine, `bield` by default | The machine it runs on |
| Needs on the machine | Nothing, so it works in a container | Tailscale, logged in, using a TUN device |
| Serves the app on | `https://bield.<tailnet>.ts.net`, port 443 | Port 4700 on the machine's tailnet IPs |

Either way, Bield serves the app only on the tailnet. Nothing on the machine's other networks can reach it.

The tailnet doesn't have to be Tailscale's own: Bield also works with [Headscale](headscale.md), a self-hosted control server for the same clients. Where this page says "log in to Tailscale", Headscale's page says what to do instead.

## Embedded mode

```sh
bield serve
```

The first time, Bield isn't on your tailnet yet. Open the setup page at <http://127.0.0.1:4700> on the same machine, then log in to Tailscale with the link or the QR code. Bield also prints the login link in its log. To skip the browser, start Bield with an [auth key](https://tailscale.com/kb/1085/auth-keys) in `TS_AUTHKEY`:

```sh
TS_AUTHKEY=tskey-auth-... bield serve
```

After that first login, Bield remembers its node in `tailscale/` inside the data directory, and the setup page just links to Bield on the tailnet. The setup page shows only the login state. It never serves the app or the API, and it only answers requests addressed to `localhost` or an IP address, so another website can't read the login link. To serve it on a different address, such as in a container, use `--setup-listen` or `BIELD_SETUP_LISTEN`. Until Bield is logged in, anyone who can open the setup page can add Bield to their own tailnet, so keep it to your machine: in a container, publish the port on the host's loopback only (`-p 127.0.0.1:4700:4700`), or use `TS_AUTHKEY`. Bield warns in its log when the page listens on more than loopback.

The node's name is `bield`, or whatever you set with `--hostname` or `BIELD_HOSTNAME`. To log in to a control server other than Tailscale's, such as Headscale, set `--control-url` or `BIELD_CONTROL_URL` to its address ([Bield and Headscale](headscale.md)).

### HTTPS

Turn on MagicDNS and HTTPS certificates in the [DNS settings](https://login.tailscale.com/admin/dns) of the admin console. Bield then serves the app on port 443 with its own certificate and redirects port 80 to it. Without them, it serves plain HTTP on port 80. WireGuard still encrypts that traffic, but browsers treat the page as insecure. Headscale never makes certificates, so on Headscale it's always plain HTTP.

### Tailnet policy

If you log Bield in as yourself, its node has your access, and nothing more is needed. For a node that belongs to no one, give it a tag. Use an auth key that applies the tag, set `BIELD_ADMINS` (see [Admins and viewers](#admins-and-viewers)), and allow the tag in your [tailnet policy file](https://login.tailscale.com/admin/acls):

```jsonc
{
  "tagOwners": {
    "tag:bield": ["autogroup:admin"],
  },
  "grants": [
    // Everyone on the tailnet can open Bield.
    { "src": ["autogroup:member"], "dst": ["tag:bield"], "ip": ["tcp:443", "tcp:80"] },
    // Tagged servers that run the agent can send it their metrics.
    { "src": ["tag:server"], "dst": ["tag:bield"], "ip": ["tcp:443", "tcp:80"] },
    // Bield can reach every device, to ping it and to open SSH.
    { "src": ["tag:bield"], "dst": ["*"], "ip": ["tcp:22", "icmp:*"] },
  ],
}
```

The last grant matters even before you use SSH. Tailscale only tells a node about the peers its policy lets it talk to, so Bield can only list and ping the devices it can reach. If a machine's sshd listens on another port, add that port to it. Tagged nodes also don't expire, while a node logged in as a user needs a new login when its key expires (after 180 days by default).

### The machine Bield runs on

In embedded mode, Bield is its own node, so the machine it runs on shows up separately if it's on the tailnet too. Bield marks that device as the same machine when one of its tailnet IPs is on a local network interface. This works when the machine's Tailscale uses a TUN device, which is the default. In a container, or with userspace networking, Bield can't see those addresses, and the machine shows up like any other device. Bield doesn't compare host names, since its own node is named after Bield. On Linux, Bield measures that machine itself, so it turns away an agent there. Both nodes share the machine's SSH access: Bield logs in on 127.0.0.1, or on the machine's tailnet IP if sshd only listens there, and can add its key for the user it runs as with one click.

## Host mode

```sh
bield serve --tailscale host
```

Bield uses the machine's Tailscale through its local socket and listens on the machine's tailnet IPs, port 4700 by default (`--port` or `BIELD_PORT`). Tailscale must be running with a TUN device, not `--tun=userspace-networking`, so that those IPs exist on the machine. If Tailscale is logged out, Bield waits for `tailscale up`.

Bield serves HTTPS with the machine's certificate if the tailnet has HTTPS turned on and Tailscale lets Bield fetch it. Root can always fetch it. For another user, make that user the Tailscale operator:

```sh
sudo tailscale set --operator=$USER
```

Otherwise Bield falls back to plain HTTP and says so in its log. If Bield may fetch the certificate but fetching it fails, for example because Let's Encrypt is slow, Bield serves HTTPS anyway and asks for the certificate again on each connection.

## Signing in

Your tailnet is the login. For every request, Bield asks Tailscale which device the connection comes from and which user owns that device. It never reads who you are from a header, so nothing in a request can claim to be someone else.

- A device that belongs to a user signs that user in, as everywhere on Tailscale: anyone using your laptop is you to Bield.
- Tagged devices, which belong to no one, and addresses Tailscale doesn't know are anonymous.
- Bield's own node is anonymous too. In host mode, that node is the machine Bield runs on, so open Bield from another device. Any program on that machine can connect to Bield, and it shouldn't count as the machine's owner.
- `GET /api/me` says who Bield thinks you are, or why you're anonymous.

Tailscale identifies devices, not the people or programs using them, so anything running on a device you're logged in to acts as you. If a server runs other people's code, give its Tailscale node a tag, so that connections from it are anonymous.

### Admins and viewers

Bield has two roles:

- **Admins** can do everything: open terminals, change files, control Docker, use SSH access, and change settings, including who has a role.
- **Viewers** see the read-only dashboard. They can't change anything, and they don't see logs, files, process command lines or other secrets such as containers' environment variables.

Anyone else, and anyone anonymous, can only see that Bield is there and why they can't view it.

One setting decides who else can view Bield. By default, it's `listed`: only Bield's admins and viewers can, and anyone else who signs in sees the command an admin can run to make them a viewer. Set it to `tailnet` to make anyone who signs in from the tailnet a viewer unless they have a role of their own. That includes people you share Bield's machine with, since Tailscale names them too, so with `tailnet`, sharing that machine shares the dashboard of your whole tailnet.

When Bield has no admins yet, it makes the first ones as soon as it's on the tailnet: the user who owns Bield's node, which is whoever logged it in, and any logins in `BIELD_ADMINS` or `--admins`, separated by commas. A login is an email address such as `alice@example.com`, or on Headscale, which names a user without an email by their name, just `alice`. A node logged in with a tagged auth key has no owner, so give it `BIELD_ADMINS`:

```sh
TS_AUTHKEY=tskey-auth-... BIELD_ADMINS=alice@example.com bield serve
```

After that, Bield ignores `BIELD_ADMINS`. Manage roles with `bield admin` on the machine Bield runs on. It changes the database directly, so it works while Bield is running and when no admin can sign in. Bield always keeps one admin, so it won't remove the last one or make them a viewer.

```sh
bield admin list                        # the admins and viewers, and who else can view Bield
bield admin add alice@example.com       # make alice an admin
bield admin add-viewer bob@example.com  # make bob a viewer
bield admin remove bob@example.com      # take bob's role away
bield admin who-can-view tailnet        # let anyone on the tailnet view; "listed" (the default) undoes it
```

Admins can make the same changes in the Access tab of [Settings](settings.md#access), which uses `GET` and `PATCH /api/access` and `PUT` and `DELETE /api/access/users/<login>`.

Only admins can see `GET /api/debug/tailnet`, and during development, the web UI that Bield proxies from Vite.

### Programs that call the API

A request that changes something, which is any method but GET, HEAD and OPTIONS, needs an `X-Bield` header with any value. A page on another website can't add one, so it can't make your browser change anything in Bield.

Bield also only answers requests addressed to its own name or tailnet IPs, so a website can't reach it by pointing its own domain at Bield's address, and it only accepts WebSockets opened by its own pages.

## What Bield watches

| What | How often | Where it comes from |
| --- | --- | --- |
| Devices and their state | Every 10 seconds | Tailscale's status |
| The path to each online device (direct, DERP relay or peer relay) and its round-trip time | Every minute | A Tailscale ping |
| UDP, IPv6, the public IP and the nearest DERP region | Every 10 minutes | A netcheck, as `tailscale netcheck` runs it |

Tailscale's local API has no netcheck, so Bield runs the check in its own process, as the `tailscale` command does. It runs on the same machine as the node, so it sees the same network. `GET /api/debug/tailnet` shows admins what Bield currently knows.

## Logs

Like every Tailscale client, the embedded node uploads its logs to Tailscale, which their support relies on. To turn that off, set `TS_NO_LOGS_NO_SUPPORT=true`.

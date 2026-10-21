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

If you log Roost in as yourself, its node has your access, and nothing more is needed. For a node that belongs to no one, give it a tag. Use an auth key that applies the tag, and allow the tag in your [tailnet policy file](https://login.tailscale.com/admin/acls):

```jsonc
{
  "tagOwners": {
    "tag:roost": ["autogroup:admin"],
  },
  "grants": [
    // Everyone on the tailnet can open Roost.
    { "src": ["autogroup:member"], "dst": ["tag:roost"], "ip": ["tcp:443", "tcp:80"] },
    // Roost can reach every device, to ping it and to open SSH.
    { "src": ["tag:roost"], "dst": ["*"], "ip": ["tcp:22", "icmp:*"] },
  ],
}
```

The second grant matters even before you use SSH. Tailscale only tells a node about the peers its policy lets it talk to, so Roost can only list and ping the devices it can reach. Tagged nodes also don't expire, while a node logged in as a user needs a new login when its key expires (after 180 days by default).

### The machine Roost runs on

In embedded mode, Roost is its own node, so the machine it runs on shows up separately if it's on the tailnet too. Roost marks that device as the same machine when one of its tailnet IPs is on a local network interface. This works when the machine's Tailscale uses a TUN device, which is the default. In a container, or with userspace networking, Roost can't see those addresses, and the machine shows up like any other device. Roost doesn't compare host names, since its own node is named after Roost.

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

## What Roost watches

| What | How often | Where it comes from |
| --- | --- | --- |
| Devices and their state | Every 10 seconds | Tailscale's status |
| The path to each online device (direct, DERP relay or peer relay) and its round-trip time | Every minute | A Tailscale ping |
| UDP, IPv6, the public IP and the nearest DERP region | Every 10 minutes | A netcheck, as `tailscale netcheck` runs it |

Tailscale's local API has no netcheck, so Roost runs the check in its own process, as the `tailscale` command does. It runs on the same machine as the node, so it sees the same network. `GET /api/debug/tailnet` shows what Roost currently knows.

## Logs

Like every Tailscale client, the embedded node uploads its logs to Tailscale, which their support relies on. To turn that off, set `TS_NO_LOGS_NO_SUPPORT=true`.

# Bield and Headscale

[Headscale](https://github.com/juanfont/headscale) is an open source control server that Tailscale's own clients can log in to instead of Tailscale's. Bield works with it in both [modes](tailscale.md). Bield only talks to the Tailscale client, so what changes is what the control server tells that client. This page covers what to do differently, and what you'll see. It was tried against Headscale 0.29.7.

## Embedded mode

Give Bield the address of your Headscale server, with `--control-url` or `BIELD_CONTROL_URL`:

```sh
bield serve --control-url https://headscale.example.com
```

Then log Bield in, as you would on Tailscale, in one of two ways:

- **The setup page.** Open <http://127.0.0.1:4700>, and then its login link. Headscale's page shows a command to run on the Headscale server. It names the user that Bield's node will belong to:

  ```sh
  headscale auth register --auth-id <id> --user alice
  ```

- **A pre-auth key.** Make one on the Headscale server, and start Bield with it in `TS_AUTHKEY`. `--user` takes the user's ID, which `headscale users list` shows:

  ```sh
  headscale preauthkeys create --user 1 --expiration 1h
  TS_AUTHKEY=... bield serve --control-url https://headscale.example.com
  ```

  For a node with a tag, add `--tags tag:bield` (the tag has to be in `tagOwners` in your policy). A tagged node has no owner, so also set `BIELD_ADMINS`, as in [Admins and viewers](tailscale.md#admins-and-viewers).

Bield remembers its node in `tailscale/` in the data directory, along with the server it logged in to. To move a Bield that is on a Tailscale tailnet to Headscale, stop it, remove that directory, and start it again with `--control-url`. It joins as a new node and keeps its database. Roles belong to logins, and your Headscale users' logins aren't the ones Tailscale had, so add the new ones with `bield admin add`.

## Host mode

Log the machine in to Headscale with `tailscale up --login-server https://headscale.example.com`, and then run `bield serve --tailscale host`. Bield reads which control server the machine uses from its `tailscaled`, so `--control-url` isn't for host mode, and Bield refuses it there.

## Logins

Headscale names a user by their email address if they have one, and otherwise by their name, so a login can be `bob@example.com` or `alice`. Bield takes a login as it comes, wherever it asks for one: `BIELD_ADMINS=alice,bob@example.com`, `bield admin add alice`, and the access settings. The owner of Bield's node still becomes its first admin.

A Headscale policy names a user without an email as `alice@`, with an `@` on the end, and rejects `alice`. When Bield shows you an SSH rule to add, it writes the user that way.

## HTTPS

Headscale doesn't make HTTPS certificates for node names, so Bield serves plain HTTP: on port 80 in embedded mode, and on `--port` (4700) in host mode, at the node's MagicDNS name. WireGuard still encrypts it, but browsers call the page insecure. Bield works the same either way, except that its copy buttons use a fallback, since the browser's clipboard API needs HTTPS. In embedded mode, the log says why Bield isn't using HTTPS.

## What differs from Tailscale

| | Tailscale | Headscale |
| --- | --- | --- |
| Tailnet name, which admins see | The tailnet's name, such as `example.com` | Always `headscale` |
| MagicDNS names | `<node>.<tailnet>.ts.net` | `<node>.<base_domain>`, the `dns.base_domain` in Headscale's config |
| HTTPS certificates | Yes, if the tailnet turns them on | No |
| Logins | Email addresses | The email, or else the name |
| Key expiry | 180 days by default | None, unless `node.expiry` is set, or `headscale nodes expire` gives a node one |
| DERP relays | Tailscale's | Whatever your server's map has: Tailscale's, its own embedded region (ID 999, code `headscale`), or both |
| Logging a node in | A login link | A link that shows a `headscale` command, or a pre-auth key |
| Admin console | login.tailscale.com | The `headscale` command and the policy file |

Bield shows the key expiry of each node, and warns before one expires, in the same way. The advice in the warning points at `headscale nodes expire --disable` instead of the admin console.

## Tailscale SSH

Headscale can run [Tailscale SSH](https://tailscale.com/docs/features/tailscale-ssh) policies, and a device that runs it shows up in Bield as it does on Tailscale. When Bield can't log in, it shows the rule to add under `ssh` in your policy file. Headscale reads the file again when you reload it, with `sudo systemctl reload headscale` or by sending it `SIGHUP`, so it needs no restart. Press Check on the device afterwards.

## Tried, and not tried

Tried against Headscale 0.29.7 with a user with an email and one without, a tagged node, and a policy file:

- Embedded mode, logged in with the setup page, with a pre-auth key, and with a tagged pre-auth key plus `BIELD_ADMINS`
- Host mode, with the machine logged in to Headscale
- Signing in, the roles of both kinds of login, and a viewer not seeing the tailnet name or server
- Enrolling an agent from a node
- Tailscale SSH with an `accept` rule, and with a rule that refuses, which Bield's rule fixes
- Key expiry, and the warning for it

Not tried:

- Logging in with OIDC, where a user's login comes from your identity provider. Bield accepts any login without spaces, so it should work, and the tests include a login that is a URL.
- A Headscale behind HTTPS, and one with only its embedded DERP server, which needs HTTPS to work
- Tailscale SSH rules with the `check` action
- Other Headscale versions

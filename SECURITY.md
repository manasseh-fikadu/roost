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

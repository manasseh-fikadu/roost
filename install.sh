#!/bin/bash
# SPDX-License-Identifier: AGPL-3.0-only
# Installs Bield on a Linux server that runs systemd, or upgrades it. It sets up
# the bield binary in /usr/local/bin, a bield user, the data directory
# /var/lib/bield and a systemd service. Run it as root:
#
#   curl -fsSL https://raw.githubusercontent.com/manasseh-fikadu/bield/main/install.sh | sudo bash
#
# Run it again to upgrade: it saves a backup first, and the migrations in the
# new version take care of the database. Settings go in the environment of the
# command, and are kept in /etc/bield/bield.env the first time:
#
#   BIELD_VERSION     the release to install, such as v0.1.0 (default: the latest)
#   BIELD_TAILSCALE   "embedded" (default) or "host"
#   BIELD_HOSTNAME    the name of Bield's node on the tailnet in embedded mode
#   BIELD_CONTROL_URL the address of a Headscale server to log in to, in embedded mode
#   BIELD_ADMINS      logins to make admins, separated by commas
#   TS_AUTHKEY        a Tailscale or Headscale auth key, to skip logging Bield in by hand
#
# BIELD_RELEASES_URL replaces where releases are downloaded from, for a mirror.
set -euo pipefail

RELEASES_URL=${BIELD_RELEASES_URL:-https://github.com/manasseh-fikadu/bield/releases}
BIN=/usr/local/bin/bield
DATA_DIR=/var/lib/bield
ENV_DIR=/etc/bield
ENV_FILE=$ENV_DIR/bield.env
UNIT=/etc/systemd/system/bield.service
# How many backups from earlier upgrades to keep.
KEEP_BACKUPS=5

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

[ "$(uname -s)" = Linux ] || die "this installs Bield as a systemd service, so it only runs on Linux. On other systems, download the archive from $RELEASES_URL and run ./bield serve."
[ "$(id -u)" -eq 0 ] || die "run this as root, such as: curl -fsSL <url> | sudo bash"
for tool in curl tar useradd systemctl runuser install; do
  command -v "$tool" >/dev/null || die "$tool is required"
done
[ -d /run/systemd/system ] || die "systemd isn't running on this machine, so there's no service to set up"

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) die "no Bield release for $(uname -m); there are amd64 and arm64 ones" ;;
esac

# The values go into a systemd environment file, so keep them to one plain word.
for var in BIELD_TAILSCALE BIELD_HOSTNAME BIELD_CONTROL_URL BIELD_ADMINS TS_AUTHKEY; do
  value=${!var-}
  case $value in
    *[[:space:]\"\'\\\$]*) die "$var can't have spaces, quotes, backslashes or \$ in it" ;;
  esac
done
case ${BIELD_TAILSCALE:-embedded} in
  embedded | host) ;;
  *) die "BIELD_TAILSCALE is \"$BIELD_TAILSCALE\", but it has to be embedded or host" ;;
esac

version=${BIELD_VERSION:-}
if [ -z "$version" ]; then
  # The latest release redirects to its own page, which names the tag.
  latest=$(curl -fsSL --connect-timeout 15 -o /dev/null -w '%{url_effective}' "$RELEASES_URL/latest") ||
    die "couldn't look up the latest release at $RELEASES_URL/latest"
  version=${latest##*/}
fi
case $version in
  [0-9]*) version=v$version ;;
esac
case $version in
  v[0-9]*) ;;
  *) die "\"$version\" isn't a release like v0.1.0 (see $RELEASES_URL)" ;;
esac

installed=
if [ -x "$BIN" ]; then
  installed=$("$BIN" version 2>/dev/null || true)
  # A service that isn't running gets the full install again, to start it.
  if [ "$installed" = "$version" ] && systemctl is-enabled --quiet bield 2>/dev/null &&
    systemctl is-active --quiet bield; then
    say "Bield $version is already installed."
    exit 0
  fi
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# The bield user runs the backup from here.
chmod 755 "$work"
archive=bield_${version#v}_linux_${arch}.tar.gz
say "Downloading Bield $version for linux/$arch ..."
curl -fsSL --connect-timeout 15 --retry 3 -o "$work/$archive" "$RELEASES_URL/download/$version/$archive" ||
  die "couldn't download $RELEASES_URL/download/$version/$archive"
curl -fsSL --connect-timeout 15 --retry 3 -o "$work/checksums.txt" "$RELEASES_URL/download/$version/checksums.txt" ||
  die "couldn't download the checksums for $version"
(cd "$work" && grep -F " $archive" checksums.txt | sha256sum --check --status -) ||
  die "$archive doesn't match its checksum, so it isn't installed"
tar -xzf "$work/$archive" -C "$work" bield || die "$archive has no bield in it"
chmod 755 "$work/bield"
"$work/bield" version >/dev/null || die "the downloaded bield doesn't run on this machine"

id bield >/dev/null 2>&1 ||
  useradd --system --user-group --home-dir "$DATA_DIR" --no-create-home --shell /usr/sbin/nologin bield
install -d -o bield -g bield -m 0700 "$DATA_DIR"

# Save the data before anything changes. This bield makes the backup, since it
# can't migrate the database, and an older one might not have the command.
backup=
if [ -f "$DATA_DIR/bield.db" ]; then
  stamp=$(date +%Y%m%d-%H%M%S)
  backup=$DATA_DIR/backups/bield-before-${version}-$stamp.tar.gz
  say "Saving a backup to $backup ..."
  out=$(runuser -u bield -- env BIELD_DATA_DIR="$DATA_DIR" "$work/bield" backup "$backup" 2>&1) ||
    die "couldn't save a backup, so nothing is changed: $out"
  # Keep the newest few, and none of the others.
  find "$DATA_DIR/backups" -maxdepth 1 -name 'bield-before-*.tar.gz' -printf '%T@ %p\n' |
    sort -rn | tail -n "+$((KEEP_BACKUPS + 1))" | cut -d' ' -f2- | xargs -r rm -f --
fi

# Replace the binary in one step, so a crash never leaves half a file.
install -m 0755 "$work/bield" "$BIN.new"
mv -f "$BIN.new" "$BIN"

fresh=
if [ ! -e "$ENV_FILE" ]; then
  fresh=1
  install -d -m 0755 "$ENV_DIR"
  (
    umask 077
    {
      say "# Bield's settings, which systemd reads when it starts the service. After a change,"
      say "# run: sudo systemctl restart bield"
      say "# The list is in docs/tailscale.md, and bield serve -h."
      for var in BIELD_TAILSCALE BIELD_HOSTNAME BIELD_CONTROL_URL BIELD_ADMINS TS_AUTHKEY; do
        if [ -n "${!var-}" ]; then
          say "$var=${!var}"
        fi
      done
      [ -n "${BIELD_TAILSCALE-}" ] || say "#BIELD_TAILSCALE=host"
      [ -n "${BIELD_HOSTNAME-}" ] || say "#BIELD_HOSTNAME=bield"
      [ -n "${BIELD_CONTROL_URL-}" ] || say "#BIELD_CONTROL_URL=https://headscale.example.com"
      [ -n "${BIELD_ADMINS-}" ] || say "#BIELD_ADMINS=alice@example.com"
      [ -n "${TS_AUTHKEY-}" ] || say "#TS_AUTHKEY=tskey-auth-..."
    } >"$ENV_FILE.new"
  )
  mv -f "$ENV_FILE.new" "$ENV_FILE"
fi

cat >"$UNIT.new" <<EOF
[Unit]
Description=Bield, a control plane for the machines on your tailnet
Documentation=https://github.com/manasseh-fikadu/bield
After=network-online.target tailscaled.service
Wants=network-online.target

[Service]
User=bield
Group=bield
Environment=BIELD_DATA_DIR=$DATA_DIR
EnvironmentFile=-$ENV_FILE
ExecStart=$BIN serve
Restart=on-failure
RestartSec=5
# Makes $DATA_DIR if it's gone.
StateDirectory=bield
StateDirectoryMode=0700

# Bield needs no privileges, so it gets none, and only its data can be written.
NoNewPrivileges=true
CapabilityBoundingSet=
PrivateTmp=true
PrivateDevices=true
ProtectSystem=strict
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
RestrictRealtime=true
LockPersonality=true

[Install]
WantedBy=multi-user.target
EOF
mv -f "$UNIT.new" "$UNIT"
systemctl daemon-reload
systemctl enable bield
systemctl restart bield

sleep 3
if ! systemctl is-active --quiet bield; then
  journalctl -u bield -n 15 --no-pager >&2 || true
  die "Bield didn't start; the log above says why. If it says the database was written by a newer version, the release you installed is older than the data: see docs/upgrading.md#roll-back. Backups are in $DATA_DIR/backups."
fi

if [ "$installed" = "$version" ]; then
  say "Reinstalled Bield $version and started it."
elif [ -n "$installed" ]; then
  say "Upgraded Bield from $installed to $version."
  [ -z "$backup" ] || say "The backup from before is $backup."
else
  say "Installed Bield $version."
fi
if [ -n "$fresh" ]; then
  say
  if [ -n "${TS_AUTHKEY-}" ]; then
    say "Bield is logging in to Tailscale with the auth key. Its address is in the log:"
    say "  journalctl -u bield -n 20"
  elif [ "${BIELD_TAILSCALE:-embedded}" = host ]; then
    say "Bield uses this machine's Tailscale. Its address is in the log:"
    say "  journalctl -u bield -n 20"
  else
    say "Log Bield in to your tailnet. Open http://127.0.0.1:4700 on this machine, or use the"
    say "link in the log:"
    say "  journalctl -u bield -n 20"
  fi
fi

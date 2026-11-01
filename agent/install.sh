#!/bin/bash
# SPDX-License-Identifier: MIT
# Installs the Bield agent for the current user: a LaunchAgent on macOS, and
# a systemd user service on Linux. No root needed. The hub serves this script
# only for a valid enrollment code, which Add agent on its Agents page makes.
set -euo pipefail

SERVER_URL=__SERVER_URL__
# The code works on one machine, for an hour. The agent trades it for a
# secret of its own.
CODE=__CODE__

__LAYOUT__

command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }

# The code goes through stdin (-K -), so it doesn't show up in `ps`. Bield
# refuses POSTs without an X-Bield header.
echo "Enrolling this machine with $SERVER_URL ..."
out=$(printf 'header = "Authorization: Bearer %s"\n' "$CODE" |
  curl -sS -m 30 -K - -w '\n%{http_code}' -X POST -H 'X-Bield: agent' "$SERVER_URL/api/agent/enroll") || {
  echo "Couldn't reach $SERVER_URL." >&2
  exit 1
}
status=${out##*$'\n'}
body=${out%$'\n'*}
if [ "$status" != 200 ]; then
  # Bield's errors look like {"error":"..."}.
  msg=$(printf '%s' "$body" | sed -n 's/^{"error":"\(.*\)"}$/\1/p')
  echo "Bield didn't enroll this machine (HTTP $status): ${msg:-$body}" >&2
  exit 1
fi
SECRET=$(printf '%s' "$body" | sed -n 's/^{"secret":"\([A-Z2-7]*\)"}$/\1/p')
if [ -z "$SECRET" ]; then
  echo "Bield's answer has no secret in it: $body" >&2
  exit 1
fi

mkdir -p "$BIN_DIR" "$CONF_DIR"
chmod 700 "$CONF_DIR"
cat > "$BIN_DIR/agent.sh" <<'BIELD_AGENT_EOF'
__AGENT_SCRIPT__
BIELD_AGENT_EOF
chmod 755 "$BIN_DIR/agent.sh"

# Only this user can read the secret. A new file gets mode 0600, and the
# rename replaces an older env whatever its mode was.
(
  umask 077
  printf 'SERVER_URL=%q\nSECRET=%q\nINTERVAL=15\n' "$SERVER_URL" "$SECRET" > "$CONF_DIR/env.new"
)
chmod 600 "$CONF_DIR/env.new"
mv -f "$CONF_DIR/env.new" "$CONF_DIR/env"

echo "Sending the first heartbeat ..."
if ! /bin/bash "$BIN_DIR/agent.sh" --once; then
  echo "The first heartbeat failed, so the agent isn't running. Fix what it says above, and run the install command again: it works on this machine until an hour after Bield made it." >&2
  exit 1
fi

if [ "$(uname -s)" = "Darwin" ]; then
  mkdir -p "$(dirname "$PLIST")"
  cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>/bin/bash</string>
    <string>$BIN_DIR/agent.sh</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ProcessType</key><string>Background</string>
  <key>LowPriorityIO</key><true/>
  <key>StandardOutPath</key><string>$LOG</string>
  <key>StandardErrorPath</key><string>$LOG</string>
</dict>
</plist>
EOF
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  launchctl bootstrap "gui/$(id -u)" "$PLIST"
  echo "Installed LaunchAgent $LABEL (logs: $LOG)."
else
  mkdir -p "$UNIT_DIR"
  cat > "$UNIT" <<EOF
[Unit]
Description=Bield agent (heartbeats to $SERVER_URL)
After=network-online.target

[Service]
ExecStart=/bin/bash %h/.local/share/$NAME/agent.sh
Restart=always
RestartSec=10
Nice=10

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable "$NAME.service"
  # restart, not start, so that running this again replaces an agent that's
  # already running with the new one.
  systemctl --user restart "$NAME.service"
  echo "Installed systemd user service $NAME (logs: journalctl --user -u $NAME)."
  if command -v loginctl >/dev/null && [ "$(loginctl show-user "$USER" -p Linger --value 2>/dev/null)" != "yes" ]; then
    echo "Tip: run 'sudo loginctl enable-linger $USER' to keep it running while you're logged out."
  fi
fi
echo "Done. This machine will appear as reporting on the dashboard within a few seconds."

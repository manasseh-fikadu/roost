#!/bin/bash
# SPDX-License-Identifier: MIT
# Installs (or with --uninstall, removes) the Roost agent for the current user.
# macOS: LaunchAgent. Linux: systemd user service. No root needed.
# The hub serves this script, and its Agents page has the command to run it.
set -euo pipefail

SERVER_URL=__SERVER_URL__
# The token isn't in the script, which anyone on the tailnet can download.
TOKEN="${ROOST_AGENT_TOKEN:-}"
NAME="roost-agent"
LABEL="dev.roost.agent"
CONF_DIR="$HOME/.config/$NAME"

case "$(uname -s)" in
  Darwin)
    BIN_DIR="$HOME/Library/Application Support/$NAME"
    PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
    LOG="$HOME/Library/Logs/$NAME.log"
    ;;
  Linux)
    BIN_DIR="$HOME/.local/share/$NAME"
    UNIT_DIR="$HOME/.config/systemd/user"
    UNIT="$UNIT_DIR/$NAME.service"
    ;;
  *)
    echo "Unsupported OS: $(uname -s)" >&2
    exit 1
    ;;
esac

if [ "${1:-}" = "--uninstall" ]; then
  if [ "$(uname -s)" = "Darwin" ]; then
    launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
    rm -f "$PLIST"
  else
    systemctl --user disable --now "$NAME.service" 2>/dev/null || true
    rm -f "$UNIT"
    systemctl --user daemon-reload 2>/dev/null || true
  fi
  rm -rf "$BIN_DIR" "$CONF_DIR"
  echo "Removed $NAME."
  exit 0
fi

command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
if [ -z "$TOKEN" ]; then
  echo "ROOST_AGENT_TOKEN isn't set. Copy the install command from the Agents page in Roost, which sets it." >&2
  exit 1
fi

mkdir -p "$BIN_DIR" "$CONF_DIR"
cat > "$BIN_DIR/agent.sh" <<'ROOST_AGENT_EOF'
__AGENT_SCRIPT__
ROOST_AGENT_EOF
chmod 755 "$BIN_DIR/agent.sh"

(
  umask 077
  printf 'SERVER_URL=%q\nTOKEN=%q\nINTERVAL=15\n' "$SERVER_URL" "$TOKEN" > "$CONF_DIR/env"
)

echo "Testing connection to $SERVER_URL ..."
if ! /bin/bash "$BIN_DIR/agent.sh" --once; then
  echo "The first heartbeat failed, so the agent isn't running. Fix what it says above, and run the install command again." >&2
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
Description=Roost agent (heartbeats to $SERVER_URL)
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
  systemctl --user enable --now "$NAME.service"
  echo "Installed systemd user service $NAME (logs: journalctl --user -u $NAME)."
  if command -v loginctl >/dev/null && [ "$(loginctl show-user "$USER" -p Linger --value 2>/dev/null)" != "yes" ]; then
    echo "Tip: run 'sudo loginctl enable-linger $USER' to keep it running while you're logged out."
  fi
fi
echo "Done. This machine will appear as reporting on the dashboard within a few seconds."

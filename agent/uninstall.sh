#!/bin/bash
# SPDX-License-Identifier: MIT
# Removes the Roost agent for the current user, and asks the hub to forget
# it. The hub serves this script to anyone.
set -euo pipefail

__LAYOUT__

# Stop the agent first, so no heartbeat arrives after the hub forgets it.
if [ "$(uname -s)" = "Darwin" ]; then
  launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
  rm -f "$PLIST"
else
  systemctl --user disable --now "$NAME.service" 2>/dev/null || true
  rm -f "$UNIT"
  systemctl --user daemon-reload 2>/dev/null || true
fi

# The agent's secret is in env, and goes through stdin (-K -) so it doesn't
# show up in `ps`.
if [ -r "$CONF_DIR/env" ] && command -v curl >/dev/null; then
  status=$(
    # shellcheck source=/dev/null
    . "$CONF_DIR/env"
    printf 'header = "Authorization: Bearer %s"\n' "${SECRET:-}" |
      curl -sS -m 10 -K - -o /dev/null -w '%{http_code}' -X DELETE -H 'X-Roost: agent' \
        "${SERVER_URL:?}/api/agent/enrollment"
  ) || true
  case "$status" in
    2??) echo "Roost forgot this machine's agent." ;;
    # An admin revoked the agent, or it's from before enrollment and has no
    # secret of its own. Roost forgot a revoked agent already.
    401) echo "Roost doesn't know this agent's secret: it was revoked, or it's from before enrollment. If Roost's Agents page still lists it, revoke it there." ;;
    000 | "") echo "Couldn't reach Roost, so revoke the agent on its Agents page." >&2 ;;
    *) echo "Roost answered HTTP $status, so revoke the agent on its Agents page." >&2 ;;
  esac
fi

rm -rf "$BIN_DIR" "$CONF_DIR"
echo "Removed $NAME."

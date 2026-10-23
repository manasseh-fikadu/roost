# shellcheck shell=bash disable=SC2034
# SPDX-License-Identifier: MIT
# Where the agent lives on a machine. The hub puts this at the top of
# install.sh and uninstall.sh, so they agree.
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

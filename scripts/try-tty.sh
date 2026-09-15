#!/usr/bin/env bash
# Spare-TTY smoke for worldr-shell (Intel Mesa / abox).
#
# From Plasma:
#   1. Ctrl+Alt+F3  → log in on tty3
#   2. cd to this repo (or set WORLDR_ROOT)
#   3. ./scripts/try-tty.sh
#   4. When it exits (15s default): Ctrl+Alt+F1 or F2 back to Plasma
#
# Do not run this from a graphical session. It refuses if WAYLAND_DISPLAY
# or DISPLAY is set, or if XDG_SESSION_TYPE is wayland/x11.

set -euo pipefail

DURATION="${DURATION:-15s}"
BACKEND="${BACKEND:-auto}"
ROOT="${WORLDR_ROOT:-}"

if [[ -z "$ROOT" ]]; then
	ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi

echo "worldr try-tty: root=$ROOT backend=$BACKEND duration=$DURATION"

if [[ -n "${WAYLAND_DISPLAY:-}" || -n "${DISPLAY:-}" ]]; then
	echo "refusing: graphical session env is set" >&2
	echo "  WAYLAND_DISPLAY=${WAYLAND_DISPLAY:-} DISPLAY=${DISPLAY:-}" >&2
	echo "This script is for a spare TTY. Ctrl+Alt+F3, log in, then run again." >&2
	echo "Do not pass --take-over-display from Plasma." >&2
	exit 2
fi

case "${XDG_SESSION_TYPE:-}" in
wayland|x11)
	echo "refusing: XDG_SESSION_TYPE=${XDG_SESSION_TYPE} looks graphical." >&2
	echo "Switch to tty3 (Ctrl+Alt+F3) so the session type is tty." >&2
	exit 2
	;;
esac

if [[ ! -e /dev/dri ]]; then
	echo "warning: no /dev/dri — vk-display/drm will fail. Need video/render + a GPU." >&2
fi

if [[ -z "${XDG_RUNTIME_DIR:-}" ]]; then
	export XDG_RUNTIME_DIR="/run/user/$(id -u)"
	mkdir -p "$XDG_RUNTIME_DIR"
	echo "set XDG_RUNTIME_DIR=$XDG_RUNTIME_DIR"
fi

cd "$ROOT"
if [[ ! -x bin/worldr-shell ]]; then
	echo "building bin/worldr-shell (CGO_ENABLED=1)…"
	export CGO_ENABLED=1
	make build
fi

echo
echo "Starting worldr-shell. First try is duration-capped ($DURATION)."
echo "Panel / overview / launcher / workspaces / effects use CompositeDesktop on this path too."
echo "When it exits: Ctrl+Alt+F1 or F2 → Plasma. If wedged: another TTY, pkill worldr-shell."
echo

exec ./bin/worldr-shell --backend="$BACKEND" --duration="$DURATION" "$@"

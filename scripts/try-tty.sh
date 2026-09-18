#!/usr/bin/env bash
# Bounded, recorded real-display qualification from a spare Linux TTY.
set -euo pipefail

WORLDR_DURATION="${DURATION:-15s}"
WORLDR_BACKEND="${BACKEND:-vk-display}"
WORLDR_CARD="${CARD:-}"
WORLDR_PROJECT="${WORLDR_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
WORLDR_STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
WORLDR_REPORT="${REPORT_DIR:-$WORLDR_PROJECT/dist/direct-$WORLDR_STAMP}"
WORLDR_STATE="${STATE:-$WORLDR_REPORT/workspace.json}"
WORLDR_METRICS="${METRICS:-$WORLDR_REPORT/metrics.json}"

if [[ -n "${WAYLAND_DISPLAY:-}" || -n "${DISPLAY:-}" || "${XDG_SESSION_TYPE:-}" == wayland || "${XDG_SESSION_TYPE:-}" == x11 ]]; then
    echo 'Refusing direct display in a graphical session. Use --backend=nested here.' >&2
    echo 'For physical display testing, switch to a spare TTY and log in there.' >&2
    exit 2
fi
case "$WORLDR_BACKEND" in
    vk-display|drm) ;;
    *) echo 'BACKEND must be vk-display or drm for this TTY script.' >&2; exit 2 ;;
esac
cd "$WORLDR_PROJECT"
make build
mkdir -p "$WORLDR_REPORT"
{
    printf 'worldr direct-display qualification\n'
    printf 'started_utc=%s\n' "$WORLDR_STAMP"
    printf 'backend=%s\n' "$WORLDR_BACKEND"
    printf 'duration=%s\n' "$WORLDR_DURATION"
    printf 'card=%s\n' "${WORLDR_CARD:-auto}"
    printf 'kernel=%s\n' "$(uname -srmo)"
    printf 'tty=%s\n' "$(tty 2>/dev/null || printf unknown)"
    printf 'session_type=%s\n' "${XDG_SESSION_TYPE:-tty}"
} >"$WORLDR_REPORT/system.txt"

list_args=(--list-outputs)
if [[ -n "$WORLDR_CARD" ]]; then list_args+=(--card="$WORLDR_CARD"); fi
./bin/worldr-shell "${list_args[@]}" >"$WORLDR_REPORT/outputs.txt" 2>&1

args=(--backend="$WORLDR_BACKEND" --duration="$WORLDR_DURATION" --take-over-display \
      --project="$WORLDR_PROJECT" --terminal --state="$WORLDR_STATE" --metrics="$WORLDR_METRICS")
if [[ -n "$WORLDR_CARD" ]]; then args+=(--card="$WORLDR_CARD"); fi
printf 'Starting native scene workspace for %s. Ctrl+Q quits. Return to your desktop with its VT shortcut.\n' "$WORLDR_DURATION"
printf 'Qualification artifacts: %s\n' "$WORLDR_REPORT"
set +e
./bin/worldr-shell "${args[@]}" "$@" 2>&1 | tee "$WORLDR_REPORT/worldr.log"
status=${PIPESTATUS[0]}
set -e
{
    printf 'exit_status=%d\n' "$status"
    printf 'finished_utc=%s\n' "$(date -u +%Y%m%dT%H%M%SZ)"
    printf 'metrics_present=%s\n' "$([[ -s "$WORLDR_METRICS" ]] && printf yes || printf no)"
    printf 'state_present=%s\n' "$([[ -s "$WORLDR_STATE" ]] && printf yes || printf no)"
} >"$WORLDR_REPORT/result.txt"
if (( status == 0 )); then
    printf 'Direct-display run completed. Record the visual/input checklist in %s/checklist.txt.\n' "$WORLDR_REPORT"
    cat >"$WORLDR_REPORT/checklist.txt" <<'EOF'
Mark PASS/FAIL and add hardware-specific observations:
[ ] image appears on every selected connector at the intended mode
[ ] pointer and keyboard remain responsive; Ctrl+Q exits
[ ] terminal text, resize, drag, depth movement and inertial throw behave correctly
[ ] moving or focusing another window does not cancel an existing throw
[ ] VT switch away/back restores input and rendering
[ ] suspend/resume restores input and rendering
[ ] connector unplug/replug preserves the surviving workspace and recovers the output
[ ] no corruption, stuck cursor, process leak or unexpected desktop-session interference
EOF
fi
exit "$status"

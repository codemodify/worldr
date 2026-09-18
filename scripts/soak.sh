#!/usr/bin/env bash
# Restart-and-resume reliability trial. Defaults to one hour in three runs.
set -euo pipefail

WORLDR_ROOT="${WORLDR_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
WORLDR_BACKEND="${BACKEND:-headless}"
WORLDR_DURATION="${DURATION:-20m}"
WORLDR_CYCLES="${CYCLES:-3}"
WORLDR_STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
WORLDR_REPORT="${REPORT_DIR:-$WORLDR_ROOT/dist/soak-$WORLDR_STAMP}"
WORLDR_PYTHON="${WORLDR_PYTHON:-python3}"
WORLDR_VERIFY="$WORLDR_ROOT/scripts/soak-verify.py"

case "$WORLDR_BACKEND" in
    headless|nested) ;;
    *) printf 'BACKEND must be headless or nested; use scripts/try-tty.sh for direct display.\n' >&2; exit 2 ;;
esac
if ! [[ "$WORLDR_CYCLES" =~ ^[1-9][0-9]*$ ]] || (( WORLDR_CYCLES > 100 )); then
    printf 'CYCLES must be between 1 and 100.\n' >&2
    exit 2
fi
if ! command -v "$WORLDR_PYTHON" >/dev/null 2>&1; then
    printf 'WORLDR_PYTHON must name a Python 3 interpreter.\n' >&2
    exit 2
fi
if ! "$WORLDR_PYTHON" -c 'import sys; raise SystemExit(sys.version_info < (3, 8))'; then
    printf 'WORLDR_PYTHON must name Python 3.8 or newer.\n' >&2
    exit 2
fi

cd "$WORLDR_ROOT"
mkdir -p "$WORLDR_REPORT"
WORLDR_REPORT="$(cd "$WORLDR_REPORT" && pwd -P)"
if [[ -n "$(find "$WORLDR_REPORT" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
    printf 'REPORT_DIR must be empty: %s\n' "$WORLDR_REPORT" >&2
    exit 2
fi
state="$WORLDR_REPORT/workspace.json"
expected="$WORLDR_REPORT/expected-checkpoint.json"
dataset="$WORLDR_REPORT/live-signals.csv"
note="$WORLDR_REPORT/soak-observations.worldr-note.md"
untitled="$WORLDR_REPORT/untitled-recovery.txt"
native_app="$WORLDR_REPORT/worldr-native-instrument"
task_sentinel="/tmp/worldr-soak-task-$WORLDR_STAMP-$$"
task_command="touch $task_sentinel"
run_log="$WORLDR_REPORT/run.txt"
trial_complete=false

if [[ -e "$task_sentinel" ]]; then
    printf 'task replay sentinel already exists: %s\n' "$task_sentinel" >&2
    exit 2
fi

{
    printf 'worldr restart-and-resume soak\n'
    printf 'started_utc=%s\nbackend=%s\nduration_per_cycle=%s\ncycles=%s\n' "$WORLDR_STAMP" "$WORLDR_BACKEND" "$WORLDR_DURATION" "$WORLDR_CYCLES"
    printf 'workloads=Files,native terminal,inert task recipe,model,research,hosted AXIAL,file note,dirty untitled note,native SDK instrument\n'
    printf 'kernel=%s\n' "$(uname -srmo)"
} >"$run_log"

record_trial_result() {
    local status=$?
    trap - EXIT
    if [[ "$trial_complete" != true && -e "$run_log" ]]; then
        printf 'finished_utc=%s\nstatus=fail\nexit_code=%d\n' "$(date -u +%Y%m%dT%H%M%SZ)" "$status" >>"$run_log"
    fi
    exit "$status"
}
trap record_trial_result EXIT

make build
go build -o "$native_app" ./examples/native-instrument
cat >"$dataset" <<'EOF'
sample,temperature,pressure,efficiency
0,21.4,101.2,0.82
1,22.1,102.4,0.86
2,23.0,102.0,0.89
3,22.6,101.5,0.88
EOF
cat >"$note" <<'EOF'
# Restart observations

This file-backed native note belongs to the bounded Worldr soak trial.
EOF
cat >"$untitled" <<'EOF'
Unsaved checkpoint note.
This exact text exists only inside the workspace session after seeding.
EOF
chmod 0600 "$dataset" "$note" "$untitled"
chmod 0700 "$native_app"

validate_run_artifacts() {
    local label="$1" metrics="$2" snapshot="$3" log="$4"
    local artifact
    for artifact in "$metrics" "$snapshot" "$state" "$log"; do
        if [[ ! -s "$artifact" ]]; then
            printf '%s did not produce %s\n' "$label" "$artifact" >&2
            return 1
        fi
    done
    "$WORLDR_PYTHON" "$WORLDR_VERIFY" run "$metrics" "$snapshot" "$WORLDR_BACKEND" 1280 800
    if ! grep -Fq 'Worldr native-app SDK v1' "$log"; then
        printf '%s did not negotiate the public native-app SDK\n' "$label" >&2
        return 1
    fi
    if grep -Fq 'native app dev.worldr.example.instrument stopped:' "$log"; then
        printf '%s lost the native SDK instrument\n' "$label" >&2
        return 1
    fi
    if grep -Fq 'session restore:' "$log"; then
        printf '%s reported a session restore failure\n' "$label" >&2
        return 1
    fi
    if [[ -e "$task_sentinel" ]]; then
        printf '%s executed an inert restored terminal task\n' "$label" >&2
        printf 'detected_utc=%s\nlabel=%s\nsentinel=%s\n' "$(date -u +%Y%m%dT%H%M%SZ)" "$label" "$task_sentinel" >"$WORLDR_REPORT/task-replay-detected.txt"
        return 1
    fi
}

seed_session_workflows() {
    "$WORLDR_PYTHON" "$WORLDR_VERIFY" seed "$state" "$expected" "$note" "$untitled" "$task_command"
}

validate_session_workflows() {
    local cycle="$1"
    local establish=()
    if (( cycle == 1 )); then
        establish=(--establish-layout)
    fi
    "$WORLDR_PYTHON" "$WORLDR_VERIFY" session "$state" "$expected" "$WORLDR_ROOT" "$dataset" "$note" "$untitled" "$task_command" "${establish[@]}"
}

# Produce a normal workspace envelope before adding bounded note/task fixtures.
# The first measured cycle therefore performs a real process restart and lets
# Worldr validate, restore and rewrite every seeded record itself.
bootstrap_metrics="$WORLDR_REPORT/bootstrap-metrics.json"
bootstrap_snapshot="$WORLDR_REPORT/bootstrap.png"
bootstrap_log="$WORLDR_REPORT/bootstrap.log"
printf 'bootstrap workspace checkpoint\n' | tee -a "$run_log"
./bin/worldr-shell --backend="$WORLDR_BACKEND" --frames=4 \
    --width=1280 --height=800 --fps=60 --gpu-memory-mib=1024 \
    --project="$WORLDR_ROOT" --terminal --model="$WORLDR_ROOT/examples/models/mount.obj" \
    --research="$dataset" --axial --native-app="$native_app" --state="$state" --autosave=0 \
    --metrics="$bootstrap_metrics" --snapshot="$bootstrap_snapshot" \
    >"$bootstrap_log" 2>&1
validate_run_artifacts "bootstrap" "$bootstrap_metrics" "$bootstrap_snapshot" "$bootstrap_log"
seed_session_workflows

for ((cycle=1; cycle<=WORLDR_CYCLES; cycle++)); do
    metrics="$WORLDR_REPORT/metrics-$cycle.json"
    snapshot="$WORLDR_REPORT/final-$cycle.png"
    printf 'soak cycle %d/%d\n' "$cycle" "$WORLDR_CYCLES" | tee -a "$run_log"
    ./bin/worldr-shell --backend="$WORLDR_BACKEND" --duration="$WORLDR_DURATION" \
        --width=1280 --height=800 --fps=60 --gpu-memory-mib=1024 \
        --project="$WORLDR_ROOT" --terminal --model="$WORLDR_ROOT/examples/models/mount.obj" \
        --research="$dataset" --axial --native-app="$native_app" --state="$state" --autosave=2s \
        --metrics="$metrics" --snapshot="$snapshot" \
        >>"$WORLDR_REPORT/worldr-$cycle.log" 2>&1
    validate_run_artifacts "cycle $cycle" "$metrics" "$snapshot" "$WORLDR_REPORT/worldr-$cycle.log"
    validate_session_workflows "$cycle"
    # A changing same-path dataset exercises restore-time reload across process
    # lifetimes. In-process file-watch refresh has separate focused coverage.
    printf '%d,%d,%d,0.%02d\n' "$((cycle+3))" "$((22+cycle))" "$((101+cycle))" "$((88+cycle))" >>"$dataset"
done

printf 'finished_utc=%s\nstatus=pass\n' "$(date -u +%Y%m%dT%H%M%SZ)" | tee -a "$run_log"
trial_complete=true
printf 'Soak passed; artifacts: %s\n' "$WORLDR_REPORT"

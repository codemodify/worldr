#!/usr/bin/env bash
# Run app navigation with a separate, restorable workspace.
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$root"

if [[ -n "${VULKAN_HEADERS:-}" ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I${VULKAN_HEADERS}/include"
elif [[ ! -f /usr/include/vulkan/vulkan.h && -f /tmp/worldr-vulkan-headers/include/vulkan/vulkan.h ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I/tmp/worldr-vulkan-headers/include"
fi

navigator_state=dist/navigator/workspace.json
navigator_fresh=false
navigator_expect_state=false
for arg in "$@"; do
    if [[ "$navigator_expect_state" == true ]]; then
        navigator_state=$arg
        navigator_expect_state=false
        continue
    fi
    case "$arg" in
        --state|-state) navigator_expect_state=true ;;
        --state=*|-state=*) navigator_state=${arg#*=} ;;
        --fresh|-fresh) navigator_fresh=true ;;
        --fresh=*|-fresh=*) navigator_fresh=${arg#*=} ;;
        --) break ;;
    esac
done

# A failed build must not leave a seed file that looks like a saved session.
mkdir -p bin dist/navigator
go build -o bin/worldr-shell ./cmd/worldr-shell

seed=()
case "$navigator_fresh" in
    1|t|T|TRUE|true|True) seed=(--project="$root" --research=examples/data/orbit-signals.csv --model=examples/models/mount.obj --axial) ;;
esac
# A crash checkpoint counts as a saved session too. Do not reopen starter
# apps after the user deliberately closes them and saves an empty workspace.
if [[ -z "$navigator_state" || ! -e "$navigator_state" && ! -e "$navigator_state.autosave" ]]; then
    seed=(--project="$root" --research=examples/data/orbit-signals.csv --model=examples/models/mount.obj --axial)
    if [[ -n "$navigator_state" ]]; then
        mkdir -p -- "$(dirname -- "$navigator_state")"
        # Keep the initial project assignments separate from generic Navigator
        # defaults. CLI apps bind to these stable keys after the layout loads.
        # No-clobber also protects a state created since the existence check.
        (umask 077; cp -n -- examples/navigator-desktop/workspace.json "$navigator_state")
    fi
elif [[ ! -e "$navigator_state.autosave" ]] && cmp -s -- examples/navigator-desktop/workspace.json "$navigator_state"; then
    # If the first launch failed before saving, retry the pristine template's
    # starter apps. An actual saved session has a different envelope.
    seed=(--project="$root" --research=examples/data/orbit-signals.csv --model=examples/models/mount.obj --axial)
fi

exec ./bin/worldr-shell --experience=navigator --backend=nested \
    --width=1280 --height=820 --state=dist/navigator/workspace.json \
    "${seed[@]}" "$@"

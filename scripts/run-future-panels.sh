#!/usr/bin/env bash
# Run layered native applications in their own persistent spatial workspace.
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$root"

if [[ -n "${VULKAN_HEADERS:-}" ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I${VULKAN_HEADERS}/include"
elif [[ ! -f /usr/include/vulkan/vulkan.h && -f /tmp/worldr-vulkan-headers/include/vulkan/vulkan.h ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I/tmp/worldr-vulkan-headers/include"
fi

panels_state=dist/future-panels/workspace.json
panels_fresh=false
panels_expect_state=false
for arg in "$@"; do
    if [[ "$panels_expect_state" == true ]]; then
        panels_state=$arg
        panels_expect_state=false
        continue
    fi
    case "$arg" in
        --state|-state) panels_expect_state=true ;;
        --state=*|-state=*) panels_state=${arg#*=} ;;
        --fresh|-fresh) panels_fresh=true ;;
        --fresh=*|-fresh=*) panels_fresh=${arg#*=} ;;
        --) break ;;
    esac
done

# Build before creating a seed, so a failed build cannot masquerade as a saved
# session. No external applications or additional example binaries are needed.
mkdir -p bin dist/future-panels
go build -o bin/worldr-desktop ./cmd/worldr-desktop

seed=()
case "$panels_fresh" in
    1|t|T|TRUE|true|True) seed=(--project="$root" --terminal --research=examples/data/orbit-signals.csv --model=examples/models/mount.obj --axial) ;;
esac
# A recovery checkpoint is an existing session. Do not reopen starter tools
# after the user has deliberately closed them, including an empty workspace.
if [[ -z "$panels_state" || ! -e "$panels_state" && ! -e "$panels_state.autosave" ]]; then
    seed=(--project="$root" --terminal --research=examples/data/orbit-signals.csv --model=examples/models/mount.obj --axial)
    if [[ -n "$panels_state" ]]; then
        mkdir -p -- "$(dirname -- "$panels_state")"
        # Stable native keys bind to the authored depth/size placements. Keep
        # the seed private and never replace a concurrently created session.
        (umask 077; cp -n -- examples/future-panels/workspace.json "$panels_state")
    fi
elif [[ ! -e "$panels_state.autosave" ]] && cmp -s -- examples/future-panels/workspace.json "$panels_state"; then
    # Retry starter content if a first launch stopped before its first save.
    seed=(--project="$root" --terminal --research=examples/data/orbit-signals.csv --model=examples/models/mount.obj --axial)
fi

exec ./bin/worldr-desktop --experience=workspace --backend=nested \
    --width=1440 --height=900 --skin=future-panels \
    --state=dist/future-panels/workspace.json "${seed[@]}" "$@"

#!/usr/bin/env bash
# Build and run one reference interface in its own persistent workspace.
set -euo pipefail
case "${1:-}" in
    advanced) preset=advanced; width=900; height=740 ;;
    hologram) preset=hologram; width=1000; height=790 ;;
    *) echo 'Usage: run-skinned-desktop.sh advanced|hologram [worldr-shell options]' >&2; exit 2 ;;
esac
shift
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$root"

if [[ -n "${VULKAN_HEADERS:-}" ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I${VULKAN_HEADERS}/include"
elif [[ ! -f /usr/include/vulkan/vulkan.h && -f /tmp/worldr-vulkan-headers/include/vulkan/vulkan.h ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I/tmp/worldr-vulkan-headers/include"
fi

mkdir -p bin "dist/$preset-desktop"
go build -o bin/worldr-shell ./cmd/worldr-shell
go build -o "bin/$preset-desktop" "./examples/$preset-desktop"
if [[ ! -f "dist/$preset-desktop/state.json" ]]; then
    cp "examples/$preset-desktop/layout.json" "dist/$preset-desktop/state.json"
fi

exec ./bin/worldr-shell --backend=nested --width="$width" --height="$height" \
    --state="dist/$preset-desktop/state.json" --skin="$preset" \
    --native-app="./bin/$preset-desktop" "$@"

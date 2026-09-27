#!/usr/bin/env bash
# Run the reference desktop in its own saved workspace.
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$root"

if [[ -n "${VULKAN_HEADERS:-}" ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I${VULKAN_HEADERS}/include"
elif [[ ! -f /usr/include/vulkan/vulkan.h && -f /tmp/worldr-vulkan-headers/include/vulkan/vulkan.h ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I/tmp/worldr-vulkan-headers/include"
fi

mkdir -p bin dist/merrick-desktop
go build -o bin/worldr-shell ./cmd/worldr-shell
go build -o bin/merrick-desktop ./examples/merrick-desktop
if [[ ! -f dist/merrick-desktop/state.json ]]; then
    cp examples/merrick-desktop/layout.json dist/merrick-desktop/state.json
fi

exec ./bin/worldr-shell --backend=nested --width=1280 --height=560 \
    --state=dist/merrick-desktop/state.json --skin=merrick \
    --native-app=./bin/merrick-desktop "$@"

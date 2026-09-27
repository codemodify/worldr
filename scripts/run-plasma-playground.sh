#!/usr/bin/env bash
# Run the native fluid-surface reference with its own persistent layout.
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd -- "$root"
if [[ -n "${VULKAN_HEADERS:-}" ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I${VULKAN_HEADERS}/include"
elif [[ ! -f /usr/include/vulkan/vulkan.h && -f /tmp/worldr-vulkan-headers/include/vulkan/vulkan.h ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I/tmp/worldr-vulkan-headers/include"
fi
mkdir -p bin dist/plasma
go build -o bin/worldr-shell ./cmd/worldr-shell
exec ./bin/worldr-shell --experience=plasma --backend=nested \
    --width=884 --height=720 --state=dist/plasma/playground.json "$@"

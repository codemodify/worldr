#!/usr/bin/env bash
# Build and run the standalone GPU terminal. No desktop session is required.
set -euo pipefail
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
if [[ -n "${VULKAN_HEADERS:-}" ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I${VULKAN_HEADERS}/include"
elif [[ ! -f /usr/include/vulkan/vulkan.h && -f /tmp/worldr-vulkan-headers/include/vulkan/vulkan.h ]]; then
    export CGO_CFLAGS="${CGO_CFLAGS:-} -I/tmp/worldr-vulkan-headers/include"
fi
(cd -- "$root" && mkdir -p bin && go build -o bin/worldr-terminal ./cmd/worldr-terminal)
exec "$root/bin/worldr-terminal" "$@"

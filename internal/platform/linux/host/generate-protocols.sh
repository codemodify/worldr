#!/bin/sh
set -eu
cd "$(dirname "$0")"
protocol_root=${WORLDR_WAYLAND_PROTOCOLS_DIR:-$(pkg-config --variable=pkgdatadir wayland-protocols)}
wayland-scanner client-header "$protocol_root/stable/xdg-shell/xdg-shell.xml" xdg-shell-client-protocol.h
wayland-scanner private-code "$protocol_root/stable/xdg-shell/xdg-shell.xml" xdg-shell-protocol.c
wayland-scanner client-header "$protocol_root/unstable/text-input/text-input-unstable-v3.xml" text-input-v3-client-protocol.h
wayland-scanner private-code "$protocol_root/unstable/text-input/text-input-unstable-v3.xml" text-input-v3-protocol.c
wayland-scanner client-header "$protocol_root/staging/fractional-scale/fractional-scale-v1.xml" fractional-scale-v1-client-protocol.h
wayland-scanner private-code "$protocol_root/staging/fractional-scale/fractional-scale-v1.xml" fractional-scale-v1-protocol.c
wayland-scanner client-header "$protocol_root/stable/viewporter/viewporter.xml" viewporter-client-protocol.h
wayland-scanner private-code "$protocol_root/stable/viewporter/viewporter.xml" viewporter-protocol.c

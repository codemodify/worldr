#!/bin/sh
set -eu
cd "$(dirname "$0")"
protocols=$(pkg-config --variable=pkgdatadir wayland-protocols)
wayland-scanner server-header "$protocols/stable/xdg-shell/xdg-shell.xml" xdg-shell-server-protocol.h
wayland-scanner private-code "$protocols/stable/xdg-shell/xdg-shell.xml" xdg-shell-protocol.c
wayland-scanner server-header "$protocols/unstable/xdg-decoration/xdg-decoration-unstable-v1.xml" xdg-decoration-server-protocol.h
wayland-scanner private-code "$protocols/unstable/xdg-decoration/xdg-decoration-unstable-v1.xml" xdg-decoration-protocol.c
wayland-scanner server-header "$protocols/unstable/linux-dmabuf/linux-dmabuf-unstable-v1.xml" linux-dmabuf-server-protocol.h
wayland-scanner private-code "$protocols/unstable/linux-dmabuf/linux-dmabuf-unstable-v1.xml" linux-dmabuf-protocol.c
wayland-scanner server-header "$protocols/stable/viewporter/viewporter.xml" viewporter-server-protocol.h
wayland-scanner private-code "$protocols/stable/viewporter/viewporter.xml" viewporter-protocol.c
wayland-scanner server-header "$protocols/staging/fractional-scale/fractional-scale-v1.xml" fractional-scale-server-protocol.h
wayland-scanner private-code "$protocols/staging/fractional-scale/fractional-scale-v1.xml" fractional-scale-protocol.c

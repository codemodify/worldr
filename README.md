# worldr

Linux-first cinematic desktop: owned Vulkan engine + Wayland compositor.
The UI toolkit is deferred. Foreign Wayland and X11 clients are the first apps.

**Status:** TTY/seat harden on `feat/tty-seat-harden` (stacked on workspaces).
Safe demo stays nested. Real display: spare TTY `scripts/try-tty.sh`.
See [docs/RUN-ABOX.md](docs/RUN-ABOX.md).

## Quick links

- [docs/RUN-ABOX.md](docs/RUN-ABOX.md) — **Arch + Intel (abox) build/run, TTY safety**
- [ARCHITECTURE.md](ARCHITECTURE.md) — locked decisions, layers, binding choice
- [LICENSE](LICENSE) — The Free License (TFL)

## Build order

0. Scaffold (done)
1. DRM/KMS + Vulkan clear-to-screen in Go (**this branch**)
2. Minimal Wayland server (surface → window actor) (**this branch**)
3. SSD borders + pointer focus (**this branch**, simple)
4. XWayland — spike (`--xwayland`, rootless Xwayland + tiny XWM → actors)
5. Compiz-style theater v0 — map/unmap scale+fade (`--effects`)
6. Expose/overview v0 — F12 grid (`--overview-demo`)
7. Panel + launcher v0 — bottom bar, F1 spawns foot
8. Workspaces v0 — 3 desktops, pager + Ctrl+Alt+←/→
9. TTY/seat harden — spare VT vk-display/drm (`scripts/try-tty.sh`)
10. Revisit UI toolkit — deferred

## Build

Go 1.22+, GCC, `libvulkan`, `libdrm` (CGO). On Arch see [docs/RUN-ABOX.md](docs/RUN-ABOX.md).

```sh
export CGO_ENABLED=1
make build   # bin/worldr-shell, bin/worldr-session
make test
```

Safe nested compositor (existing Wayland session — recommended first try):

```sh
./bin/worldr-shell --backend=wayland-client --duration=30s
# window theater on (default). --effects=off to disable.
# focus the worldr window, F1 (or Super+Space / panel apps) → foot
# other terminal still works:
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export WAYLAND_DISPLAY=wayland-1   # use the name the shell printed
foot
# F12 or panel grid for expose; second foot from the launcher
# pager dots or Ctrl+Alt+←/→ for workspaces (new windows land on the active desktop)
# X11 (optional):
./bin/worldr-shell --backend=wayland-client --xwayland --duration=60s
DISPLAY=:N xeyes    # N from the shell's "xwayland: DISPLAY=" line
```

Spare TTY (real display — do not run this on top of your desktop):

```sh
# Ctrl+Alt+F3, login, then:
./scripts/try-tty.sh
# back to Plasma: Ctrl+Alt+F1 or F2
```

## License

[The Free License](https://licenseplanet.net/the-free-license). See [LICENSE](LICENSE).

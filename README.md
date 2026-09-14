# worldr

Linux-first cinematic desktop: owned Vulkan engine + Wayland compositor.
The UI toolkit is deferred. Foreign Wayland and X11 clients are the first apps.

**Status:** window theater v0 on `feat/window-effects-v0` (stacked on XWayland).
Safe demo remains `--backend=wayland-client` + `foot` — windows scale+fade on
map/unmap. `--effects=off` disables. `--xwayland` + `DISPLAY=:N xeyes` is
optional. See [docs/RUN-ABOX.md](docs/RUN-ABOX.md).

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
6. Revisit UI toolkit — deferred

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
# other terminal:
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export WAYLAND_DISPLAY=wayland-1   # use the name the shell printed
foot
# X11 (optional):
./bin/worldr-shell --backend=wayland-client --xwayland --duration=60s
DISPLAY=:N xeyes    # N from the shell's "xwayland: DISPLAY=" line
```

Spare TTY (real display — do not run this on top of your desktop without reading the safety notes):

```sh
./bin/worldr-shell --backend=vk-display --duration=15s
```

## License

[The Free License](https://licenseplanet.net/the-free-license). See [LICENSE](LICENSE).

# worldr

Linux-first cinematic desktop: owned Vulkan engine + Wayland compositor.
The UI toolkit is deferred. Foreign Wayland and X11 clients are the first apps.

**Status:** 0.9.7-dev on `cursor/tty-vk-display-soak-c92c` (stacked on .desktop launcher). TTY / vk-display soak harden; **Ctrl+Q** quits.
Nested compositor is the safe demo. Real display: spare TTY via `scripts/try-tty.sh`.
Human history: [CHANGELOG.md](CHANGELOG.md). Abox notes: [docs/RUN-ABOX.md](docs/RUN-ABOX.md).

## Quick links

- [CHANGELOG.md](CHANGELOG.md) — PRs #1–#10 / 0.1→0.9
- [docs/RUN-ABOX.md](docs/RUN-ABOX.md) — Arch + Intel (abox) build/run, TTY safety
- [ARCHITECTURE.md](ARCHITECTURE.md) — locked decisions, layers, binding choice
- [LICENSE](LICENSE) — The Free License (TFL)

## Build

Go 1.22+, GCC, `libvulkan`, `libdrm` (CGO). On Arch see [docs/RUN-ABOX.md](docs/RUN-ABOX.md).

```sh
export CGO_ENABLED=1
make build   # bin/worldr-shell, bin/worldr-session
make test
```

## Nested demo (recommended)

Existing Wayland session. Clients appear inside the worldr window.

```sh
./bin/worldr-shell --backend=wayland-client --duration=30s
# focus the worldr window, F1 (or Super+Space / panel apps) → foot
# other terminal still works:
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export WAYLAND_DISPLAY=wayland-1   # name the shell printed
foot
# F12 or panel grid = expose; pager / Ctrl+Alt+←/→ = workspaces
# quit: Ctrl+Q (bare Q/Esc do not quit while a client is open)
# optional X11:
./bin/worldr-shell --backend=wayland-client --xwayland --duration=60s
DISPLAY=:N xeyes    # N from the shell's "xwayland: DISPLAY=" line
```

`--backend=auto` picks nested when `WAYLAND_DISPLAY` is set.

## Spare TTY (real display)

Do not run this on top of Plasma. Ctrl+Alt+F3, login, then:

```sh
./scripts/try-tty.sh
# back to Plasma: Ctrl+Alt+F1 or F2
```

The script refuses a graphical session. `vk-display` / `drm` refuse unless `--take-over-display`.

## License

[The Free License](https://licenseplanet.net/the-free-license). See [LICENSE](LICENSE).

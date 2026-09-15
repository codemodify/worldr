# Changelog

Human notes for worldr 0.1 → 0.9 (PRs #1–#10) and the 0.9.1+ stubs.
Stacked on `dev`. Nested compositor on Plasma is the safe demo; real display is a spare TTY.

## 0.9.10-dev — dmabuf GPU sample (no scanout bypass)

- `zwp_linux_dmabuf_v1` still advertised. LINEAR mmap and Vulkan readback remain the CPU fallback (nested, drm, ABGR, theater).
- On `vk-display`, ARGB/XRGB client buffers are retained as `VkImage` and **blitted in the compositor pass** after the CPU desktop upload. Intel implicit sync via layout transition. No linux-drm-syncobj; no KMS plane scanout (TODO).
- Bad fourcc / empty / tiled-without-import refuse. shm path unchanged.
- No IME.

## 0.9.9-dev — clipboard MIME (text/plain)

- `wl_data_device_manager` now does real selection: `wl_data_source.offer`, `set_selection`, `data_offer` / `selection`, `data_offer.receive` → `data_source.send` (UTF-8 bytes on the fd).
- `text/plain` and `text/plain;charset=utf-8` alias. Same path for `zwp_primary_selection` (foot mouse-select / middle-click).
- In-compositor only: copy/paste between worldr clients. Nested host clipboard bridge is a follow-up (host nest does not bind `wl_data_device`).
- No IME.

## 0.9.8-dev — xdg_popup + wl_subsurface

- `xdg_surface.get_popup` sends `xdg_popup.configure` + `xdg_surface.configure`. `reposition` (v3) sends `repositioned` then a new configure. `grab` dismisses the popup on an outside click.
- `wl_subcompositor` / `wl_subsurface`: position, sync (apply on parent commit) and desync. Child actors stack above the parent.
- Pointer hit-test prefers the top-most buffer (menus get enter/button). SSD chrome is skipped for popups and subsurfaces.
- No IME.

## 0.9.7-dev — TTY / vk-display soak

- Finite 2s GPU waits on `vk-display` acquire/present/teardown so a lost DRM master does not hang the spare VT forever.
- DRM session `fd` starts at `-1` (calloc 0 was stdin). `--card` must be `/dev/dri/cardN`, not a render node.
- `--backend=auto` refuses leftover `XDG_SESSION_TYPE=wayland|x11` without a host socket (would have picked vk-display).
- `scripts/try-tty.sh` accepts `CARD=`, warns when only render nodes exist. Exact abox soak steps in [docs/RUN-ABOX.md](docs/RUN-ABOX.md).
- No IME.

## 0.9.6-dev — XDG .desktop launcher

- Launcher scans `$XDG_DATA_HOME` / `$XDG_DATA_DIRS` `applications/*.desktop` (`Name=`, `Exec=` with field codes stripped, optional `Icon=`).
- Drops `Hidden=true`, `NoDisplay=true`, `Terminal=true`, non-`Application`. Light `OnlyShowIn` / `NotShowIn` when `XDG_CURRENT_DESKTOP` is set. Known X11 bins (`xeyes`, `xterm`, …) only when `--xwayland`.
- Empty scan falls back to `foot` / `weston-simple-shm` (plus `xeyes` / `xterm` with Xwayland).
- No IME work in this release.

## 0.9.5-dev — pointer button press/release hygiene

- Track per-client `wl_pointer.button` state. A client only gets a release if it previously got the matching press while focused.
- Leave / focus steal: send matching releases for buttons still down (Mutter-style), then `wl_pointer.leave`. Implicit grab keeps the press surface until release so motion does not hop.
- Nested host path: drop a Plasma/KWin release that was never pressed on the worldr window; leave-while-down synthesizes one matching release.
- Foot’s `stray button release event (compositor bug?)` should be gone. See [docs/RUN-ABOX.md](docs/RUN-ABOX.md).

## 0.9.4-dev — full US xkb keymap

- Replace the stub keymap (only q/w/e, a/s/d, z) with a compiled US layout (`keymap_us.xkb`). Typing **top** in foot works; AD05/AD09/AD10 have t/o/p.
- `wl_keyboard.key` stays XKB (evdev+8): nested host codes pass through; TTY evdev gets +8.
- **Ctrl+Q** quits; normal typing goes to the focused client.

## 0.9.3-dev — quit is Ctrl+Q

- Bare **Q** / **Esc** no longer quit while a client is on the desktop (typing `q` in foot used to kill worldr).
- **Ctrl+Q** is the explicit quit chord. Esc still closes launcher/overview. Esc on an empty desktop still quits.
- Nested: do not send `wl_pointer.button` release to a client without a matching press (foot “stray button release”).

## 0.9.2-dev — sticky launcher (nested apps click)

- Nested: host Wayland seat is the only pointer (and key) source. Evdev Click is no longer OR-merged with `TakeInput()` — that double edge opened then immediately closed the launcher.
- After open (panel **apps** or F1), ignore outside-click dismiss until pointer Release. **apps** while open closes explicitly (150ms debounce on the opening press).

## 0.9.1-dev — docs + fractional-scale stub

- `CHANGELOG.md` and a shorter root README (nested demo + `scripts/try-tty.sh`).
- Advertise `wp_fractional_scale_manager_v1`. `get_fractional_scale` sends `preferred_scale` **120** (1.0).
- Shell still composites at integer buffer scale. This is enough to drop foot’s “no fractional scale” warning.

## 0.9.0-dev — TTY / seat harden (PR #10)

`feat/tty-seat-harden` on workspaces.

- `vk-display` / `drm` refuse under Plasma (`WAYLAND_DISPLAY` / `DISPLAY` / `XDG_SESSION_TYPE`) unless `--take-over-display`. Clear spare-TTY guidance.
- `--backend=auto`: nested when `WAYLAND_DISPLAY` is set; else vk-display → drm if `/dev/dri/card*` exists; else headless.
- `scripts/try-tty.sh` refuses a graphical session. Spare VT (Ctrl+Alt+F3) is the real-display path.
- Nested `CompositeDesktop` unchanged. Abox: refuse + auto + script confirmed.

## 0.8.0-dev — Workspaces v0 (PR #9)

`feat/workspaces-v0` on panel-launcher.

- 3 virtual desktops (clamped 2–4). Pager dots on the panel. Ctrl+Alt+←/→ or pager to switch.
- Slide is a visual offset; actors keep their home X. New windows land on the active desktop.
- Overview (F12) is current-desktop only. Focus skips other workspaces.

## 0.7.0-dev — Panel + launcher (PR #8)

`feat/panel-launcher-v0` on overview.

- Bottom panel (36px): clock, focused title, **apps** / **grid**, pager later.
- In-shell launcher (F1 / Super+Space / **apps**). Hardcoded `foot`, `weston-simple-shm`.
- Child env strips host `WAYLAND_DISPLAY` so the client hits the worldr socket.

## 0.6.0-dev — Expose / overview (PR #7)

`feat/overview-expose-v0` on window-effects.

- F12 (or Super+Tab if the host does not steal Super) toggles an expose grid.
- Esc leaves overview; it does not quit the shell. `--overview-demo` auto-enters after the first map.
- SSD polish: title bar + accent + focused glow.

## 0.5.0-dev — Window theater (PR #6)

`feat/window-effects-v0` on xwayland.

- Map/unmap scale+fade (`--effects=high|low|off`). Focus lift/shadow pulse.
- Nested path stays primary. Abox: theater line with foot connected.

## 0.4.0-dev — XWayland spike (PR #5)

`feat/xwayland-spike` on nested-compositor-present.

- `--xwayland` starts rootless Xwayland + a tiny XWM. `xeyes` / `xterm` map as SSD actors.
- `xwayland_shell_v1` advertised. Not a full EWMH WM.

## 0.3.0-dev — Nested compositor present (PR #4)

`feat/nested-compositor-present` on client-harden.

- Host Wayland window **is** the compositor seat. Clients (foot) appear inside that window on Plasma.
- Pointer/keys while the window is focused are forwarded into worldr.

## 0.2.x — Client harden + linux-dmabuf (PRs #2–#3)

- **#2** `feat/linux-dmabuf-and-clients`: `zwp_linux_dmabuf_v1`, shm + dmabuf → actor pixels, first real clients (foot, weston-simple-shm).
- **#3** `feat/client-harden`: `wp_cursor_shape_manager_v1`, `xdg_activation_v1` token `done`, primary-selection stub, KWin nest bind clamp.

## 0.1.0 — Phase 0 scaffold (PR #1)

`cursor/phase0-scaffold-eeef` on `dev`.

- Go module, Vulkan/DRM C ABI, `worldr-shell` / `worldr-session`, headless + nested + vk-display/drm backends.
- Minimal Wayland server, SSD, scene/actors. Locked: owned compositor, no wgpu / Smithay / wlroots.

## How to try

Nested (safe, current desktop):

```sh
./bin/worldr-shell --backend=wayland-client --duration=30s
# F1 → foot; F12 overview; pager or Ctrl+Alt+←/→ workspaces
```

Spare TTY (real display — not on top of Plasma):

```sh
./scripts/try-tty.sh
```

See [README.md](README.md) and [docs/RUN-ABOX.md](docs/RUN-ABOX.md).

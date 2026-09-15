# Changelog

Human notes for worldr 0.1 → 0.9 (PRs #1–#10) and the 0.9.1+ stubs.
Stacked on `dev`. Nested compositor on Plasma is the safe demo; real display is a spare TTY.

## 0.9.31-dev — vk-display overlay/cursor when DRM master is available

- `--backend=vk-display` tries a planes-only DRM sidecar (`drmSetMaster`, no primary `SetCrtc`) and `VK_EXT_acquire_drm_display` so overlay + hardware cursor share the master fd with `VK_KHR_display`.
- Primary stays the Vulkan swapchain (GPU blit). Miss (no master, no acquire ext, atomic reject) prints the existing `kms overlay fallback` / `kms cursor fallback` and composes.
- Nested / headless unchanged. No IME.

## 0.9.30-dev — Vulkan timeline wait on every present path

- `--backend=drm` opens a headless Vulkan session (same offscreen ICD as nest/headless) so `wp_linux_drm_syncobj` acquire waits use `vkWaitSemaphores` before KMS primary scanout, overlay, and compose upload. DRM `SYNCOBJ_TIMELINE` ioctl stays the fallback.
- Commit-time wait (`applySyncobjAcquire`) uses `Server.Waiter` even when `HasDMABuf` is false (Import still optional). Present-time `waitActorSync` already used `p.vk`; drm no longer leaves that nil.
- Cursor plane is compositor ARGB (no client fence). vk-display / nest / headless unchanged. No IME.

## 0.9.29-dev — drag-and-drop between clients

- `wl_data_device.start_drag` → `data_offer` / `enter` / `motion` / `drop` / `leave` for text and image MIME we already support (`text/plain`, png/jpeg/webp/bmp). Copy action only. Dest `receive` + `finish` completes `data_source.send` / `dnd_finished`.
- Drop on empty desktop / panel cancels the source (`cancelled`). No icon-canvas drop target yet (follow-up).
- No IME. No nest-host DND bridge.

## 0.9.28-dev — icon theme Inherits= + librsvg

- `index.theme` `[Icon Theme] Inherits=` is walked (comma list, recursive, cycle-safe) before hicolor. PNG still wins over SVG in each theme.
- Optional CGO `librsvg-2.0` (`-tags=librsvg` when `pkg-config --exists librsvg-2.0`; `make build`/`make test`). Path-heavy breeze/adwaita SVGs raster through cairo. Without headers/tag, the simple SVG path stays.
- No IME. No full `index.theme` Directory/Size inheritance graph.

## 0.9.27-dev — Chromium/Qt protocol surface

- `zwp_linux_dmabuf_v1` feedback is LINEAR-only on nest/headless (`CanGPUComposite` false) so Chromium/Brave allocate mmap-able buffers instead of Intel-tiled ones that became a black `create_immed` placeholder. vk-display still advertises tiled modifiers.
- `create_immed` import failure keeps the client fd when present (scanout / later mmap) instead of always painting black. Placeholder remains the last resort (no fd) so the GPU process is not disconnected.
- `wl_compositor` v6: `wl_surface.enter(output)` on first map and `preferred_buffer_scale` (and on scale change). `xdg_toplevel.configure` includes `activated`. `xdg_activation.activate` focuses, re-configures, and sends `wl_keyboard.enter`. First toplevel map does the same.
- `zxdg_decoration` still forces SSD (`set_mode` / `unset_mode`) and re-sends `configure` with each toplevel configure.
- Remaining GPU flag if ozone still dies after this: `brave --ozone-platform=wayland --disable-gpu`. No IME. Disks still deferred.

## 0.9.26-dev — Qt Wayland init SEGV (ark / Brave)

- Root cause: `wl_data_device.selection(null)` (and primary `selection(null)`) was sent immediately on `get_data_device` during the client's first `wl_display_roundtrip`. Qt6 `QWaylandDataDevice` then calls `platformIntegration()->clipboard()` before `createPlatformIntegration` has published the integration → SIGSEGV in `libQt6WaylandClient`.
- Protocol: send `selection` only immediately before `wl_keyboard.enter`, or when the selection changes while that client has keyboard focus. Null marshal is still `object` id 0 (4-byte zero). Seat v8 + keymap/repeat_info + cursor-shape were not the crash.
- `WAYLAND_DEBUG=1 ark`: second `sync` must reach `callback.done`; `wl_data_device.selection` must not appear before `wl_keyboard.enter`. After `get_keyboard`: `keymap` + `repeat_info`.
- No IME. GNOME Disks still deferred (GTK, not this Qt path).

## 0.9.25-dev — SVG icons + JPEG/WebP clipboard

- XDG `Icon=` / theme lookup rasters simple SVG (rect/circle/ellipse/polygon/path) when no PNG exists (`scalable/` and sized `.svg`). PNG still wins. Cached like PNG.
- Clipboard offers/receives `image/jpeg` and `image/webp` alongside png/bmp (exact MIME in-compositor; nest host forwards when advertised). `image/jpg` aliases jpeg.
- No IME. No full SVG filters/text. No toolkit rsvg.

## 0.9.24-dev — Compiz theater polish

- Map-in: slightly longer ease-out scale+fade, plus a short rise on `--effects=high`. Map-out scales/fades toward the panel title slot (cheap minimize-to-panel). Focus pulse adds a magenta glow ring (lift+shadow kept).
- Workspace switch uses ease-in-out (softer start/settle) and a light dim on the sliding desktops. `--effects=low` stays fade-only; `off` is still instant.
- Frame loop reuses actor/occupancy/GPU-layer slices and the key-consume map. Overview filters compact in-place. No IME. No Brave/Ark.

## 0.9.23-dev — Vulkan timeline wait + overlay/cursor planes

- `wp_linux_drm_syncobj` acquire points wait on a Vulkan timeline semaphore (`VK_KHR_external_semaphore_fd` + `vkWaitSemaphores`) before dmabuf sample, `vkCmdBlit`, and KMS scanout. DRM `SYNCOBJ_TIMELINE` ioctl remains the fallback. Release is signaled after present, not at commit.
- Overlay and cursor plane eligibility (`scanout.EvaluatePlanes`): windowed ARGB/XRGB dmabuf → overlay; small visible cursor → cursor plane; fullscreen still primary. Nested/shm/headless stay compose. vk-display is eligible (Intel first) but `NeedDRM` — `VK_KHR_display` usually holds master, so we compose.
- `--backend=drm` tries overlay + hardware cursor atomic commits; failure prints `kms overlay fallback` / `kms cursor fallback` and uses the existing blit path.
- No IME. No Brave/Ark work.

## 0.9.22-dev — nest input deadlock

- P0: 0.9.21 froze the nested Plasma window (visible, ~0% CPU, no click/key). `readLoop` holds `Window.mu` across `handle()`; pointer enter called `EnsureHostCursor` which locked the same mutex. `TakeInput` then waited forever.
- Enter now `set_shape(default)` / shm `set_cursor` **without** re-locking, using the enter serial. Frame loop only toggles host cursor when a client custom image/shape appears or clears.
- Host arrow stays visible. Still no IME.

## 0.9.21-dev — nest cursor, Brave, fractional SSD

- Nested Plasma: bind host `wp_cursor_shape_manager_v1` (v1) and `set_shape(default)` on pointer enter (shm arrow `set_cursor` fallback). Stop sending a null host cursor. Client `set_cursor` / cursor-shape still drive the software overlay; null `set_cursor` keeps the default arrow.
- Chromium/Brave: `wl_output` v4 now sends `name` + `description` before `done`. `zxdg_decoration` `unset_mode` still ACKs SSD. `wp_viewport.set_source` is parsed (ignored crop; dest + geometry size the window). `zwp_linux_dmabuf_v1.create_immed` import failure installs a black placeholder instead of disconnecting the GPU process.
- GTK4 / GNOME Disks at nest scale 1.75: honor `xdg_surface.set_window_geometry` width/height (was dropped). `ApplyWindowGeometry` crops CSD shadow padding so SSD hugs the real client edge. Pointer events stay in surface-local coords (geometry offset).
- Temporary Brave flag if GPU bring-up still dies: `--ozone-platform=wayland --disable-gpu`. No IME.

## 0.9.20-dev — linux-drm-syncobj

- Advertise `wp_linux_drm_syncobj_manager_v1` when a local DRM node reports `DRM_CAP_SYNCOBJ_TIMELINE`.
- `get_timeline` imports the client fd; `set_acquire_point` / `set_release_point` parse hi/lo. Commit tries a short DRM timeline wait, then a release signal. Failure keeps **implicit sync** (Intel).
- No Vulkan `VK_KHR_timeline_semaphore` wait yet (TODO). NVIDIA/AMD best-effort (same ioctls). No IME.

## 0.9.19-dev — image clipboard MIME

- `wl_data_device` and `zwp_primary_selection` offer/receive `image/png` (and `image/bmp` when advertised). In-compositor paste is exact-MIME.
- Nest host bridge forwards `image/png` both ways when the host offers it, alongside `text/plain` (text path unchanged). Caps: 1MiB text, 8MiB image.
- No IME. No JPEG/WebP.

## 0.9.18-dev — XDG icon theme

- Resolve `.desktop` `Icon=` (and window `AppID` / `xdg_toplevel_icon.set_name`) via the current theme + **hicolor** under `$XDG_DATA_HOME` / `$XDG_DATA_DIRS` (png-first; absolute paths; pixmaps fallback). SVG is not rasterized.
- Launcher rows and the panel/SSD title slot draw the theme PNG. A client `xdg_toplevel_icon` buffer still wins. Missing name → default glyph.
- No IME. No icon-theme index.theme inheritance graph.

## 0.9.17-dev — KMS dmabuf scanout bypass

- When a single visible client is fullscreen opaque ARGB/XRGB (buffer == CRTC, not scaled) and the backend is `drm` or `vk-display`, worldr tries **primary-plane scanout** (`drmPrimeFDToHandle` + `AddFB2` + atomic commit, `SetCrtc` fallback) instead of the CPU desktop upload.
- Ineligible (windowed, popup, shm, overview, launcher, workspace slide, theater, nested/headless) stays on the existing blit/composite path.
- Intel Arrow Lake is first. NVIDIA/AMD use the same helpers; `AddFB2` may fail and we blit. `vk-display` typically cannot steal DRM master from `VK_KHR_display` — eligibility still runs, then GPU blit.
- shm and nested present are unchanged. No IME. No multi-plane overlay assignment.

## 0.9.16-dev — nest host scale

- Nested `--backend=wayland-client` binds host `wl_output` (v≤2) and `wp_fractional_scale_manager_v1` when advertised.
- `preferred_scale` / `wl_output.scale` follow the host (fractional 120ths preferred). Updates on host scale change.
- `--scale` still overrides. vk-display/drm stay 1.0 unless `--scale`.
- No IME.

## 0.9.15-dev — nest host clipboard bridge

- Nested `--backend=wayland-client` binds host `wl_data_device_manager` (and `zwp_primary_selection` when advertised).
- worldr `set_selection` of `text/plain` is offered to Plasma; host selection is imported so foot paste works both ways.
- Echo of our own host `set_selection` is ignored. No IME.

## 0.9.14-dev — workspaces polish

- One shortcut scheme: **Ctrl+Alt+←/→** switches desktop (wraps); **Ctrl+Alt+Shift+←/→** moves the focused window and follows. No Super+1..N.
- Panel pager shows **N/M** plus occupied dots (bright = has windows, brand = current). Overview labels `desk N/M`. Empty desktops stay addressable.
- Move takes the focused toplevel and its Owner children (popups/subsurfaces). Reuses the existing ~260ms slide.
- No IME. This finishes the original P1 list.

## 0.9.13-dev — xdg_toplevel_icon

- Advertise `xdg_toplevel_icon_manager_v1`. Bind sends `icon_size` 16 and 24, then `done`.
- `create_icon` / `add_buffer` (shm snapshot) / `set_name` (stored, no theme lookup) / `set_icon` (null unsets). Closest buffer to 16px is used.
- SSD title bar and the panel title slot draw the client icon, or a default glyph when unset.
- No IME. No icon-theme loader.

## 0.9.12-dev — wp_fractional_scale preferred scale

- `wp_fractional_scale_manager_v1` still advertised. `preferred_scale` is now driven by the output scale in 120ths (1.0 → 120, 1.25 → 150, 1.5 → 180, 2.0 → 240).
- `wl_output.scale` is the nearest integer (1.25 → 1, 1.5 → 2). Single output only.
- Default **1.0** on nested and vk-display (nest does not bind host `wl_output`, so Plasma scale is unknown). `--scale=1.5` for HiDPI soak.
- `wl_surface.set_buffer_scale` and `wp_viewport.set_destination` set the logical window; a larger buffer is scaled into that rect (shm at scale 1 unchanged). GPU sample skips dest≠buffer (CPU blit).
- No IME.

## 0.9.11-dev — XWayland EWMH / focus / stacking

- Tiny XWM now advertises `_NET_SUPPORTED` / `_NET_SUPPORTING_WM_CHECK` (`worldr`) plus `_NET_ACTIVE_WINDOW`, `_NET_CLIENT_LIST`, `_NET_WM_WINDOW_TYPE`, `_NET_CLOSE_WINDOW`.
- `WM_NAME` / `_NET_WM_NAME` / `WM_CLASS` map onto worldr actor title and app id (no more default `X11` once the client sets them).
- Click-to-focus raises the actor and sends `SetInputFocus` + `WM_TAKE_FOCUS` when listed. `_NET_ACTIVE_WINDOW` / ConfigureRequest stack-Above raise in the scene.
- `WM_DELETE_WINDOW` is sent for `_NET_CLOSE_WINDOW` when the client listed the protocol.
- Override-redirect, `WM_TRANSIENT_FOR`, and popup-ish window types skip SSD and keep the X11 position (xterm/xcalc menus).
- Not a full ICCCM/EWMH WM (no reparenting, pager, struts, or IME).

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

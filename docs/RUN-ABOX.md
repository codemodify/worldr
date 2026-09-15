# Run worldr-shell on abox (Arch, Intel Arrow Lake)

First tryable build: a `worldr-shell` binary that **builds with Go** and either

1. **clears the screen** via Vulkan `VK_KHR_display` or DRM/KMS, or
2. **nests** as a Wayland client on Plasma **and hosts clients** in that window, or
3. **shows a compositor seat** on a spare TTY / headless.

This machine: Intel Arrow Lake iGPU, Mesa 26.2.2, Vulkan 1.4, Arch Linux.

## Safety — do not steal your current session by accident

`vk-display` and `drm` take **DRM master** and will blank / replace the active
VT. If `WAYLAND_DISPLAY` or `DISPLAY` is set, `worldr-shell` **refuses** those
backends unless you pass `--take-over-display`.

**Preferred first try (safe):** nested compositor window on your current desktop
(`--backend=wayland-client` or `--backend=nested`), then `foot` on the printed
`WAYLAND_DISPLAY`.

**Preferred real-display try:** a **spare TTY** (tty3 via **Ctrl+Alt+F3**),
not the VT that is already running Plasma/Hyprland/Sway/GNOME.

| Key | What it does |
| --- | --- |
| Ctrl+Alt+F1…F7 | Switch virtual terminals. Your existing graphical session stays on its VT. |
| Ctrl+C | Stop `worldr-shell` if it is in the foreground on that TTY. |
| Ctrl+Q | Quit the compositor (explicit chord). Bare **Q** never quits — type in foot freely. |
| F12 | Toggle expose/overview (nested window must be focused). Super+Tab if the host does not steal Super. Also the panel **grid** button. |
| F1 | Open the in-shell launcher (Super+Space if the host does not steal Super). Also the panel **apps** button. |
| Ctrl+Alt+←/→ | Switch virtual desktop (pager dots if the host steals this combo). |
| `--duration=15s` | Always exits — use this the first time on a TTY. |

If the TTY appears wedged: another TTY (`Ctrl+Alt+F4`), `pkill worldr-shell`,
then **Ctrl+Alt+F1** or **F2** back to Plasma. The DRM backend tries to restore
the previous CRTC on exit.

## Packages (Arch)

```sh
sudo pacman -S --needed \
  go gcc pkgconf \
  vulkan-headers vulkan-icd-loader vulkan-intel vulkan-tools \
  mesa libdrm wayland wayland-protocols \
  librsvg \
  weston \
  xorg-xwayland xorg-xeyes xterm xorg-xcalc
```

`weston` provides `weston-simple-shm`. `xorg-xwayland` + `xterm` / `xorg-xeyes` / `xorg-xcalc` are for `--xwayland`.

`vulkan-radeon` / `nvidia-utils` are fine later; **abox is Intel first**.

Groups (logind `uaccess` usually covers the active VT; still useful):

```sh
sudo usermod -aG video,render,input "$USER"
# re-login
```

Confirm the ICD:

```sh
vulkaninfo --summary
# expect a Mesa Intel device, Vulkan 1.4
ls /dev/dri/
```

## Build

Needs CGO, `libvulkan`, and `libdrm` (the C ABI boundary). Optional `librsvg`
(`pkg-config librsvg-2.0`) turns on `-tags=librsvg` in `make` for real SVG
icons; without it the simple raster is used. No huge vendored trees.

```sh
git clone https://github.com/codemodify/worldr.git
cd worldr
# this branch (stacked on .desktop launcher):
git checkout cursor/tty-vk-display-soak-c92c

export CGO_ENABLED=1
make build
# → bin/worldr-shell  bin/worldr-session
```

Or: `go build -o bin/worldr-shell ./cmd/worldr-shell`

List GPUs (no display takeover):

```sh
./bin/worldr-shell --list-devices
```

## Recommended safe try (nested compositor on Plasma)

This is the **desktop demo**: a window on your existing session that *is* the
worldr compositor. Clients you launch against the printed socket appear **inside
that window** with SSD.

```sh
# keep your Plasma/KWin WAYLAND_DISPLAY (usually wayland-0) for this process
./bin/worldr-shell --backend=wayland-client --duration=60s
# alias: --backend=nested
```

The shell prints something like:

```
present: backend=wayland-client size=1280x720 …
wayland compositor: WAYLAND_DISPLAY=wayland-1  (example: WAYLAND_DISPLAY=wayland-1 foot)
nested compositor: clients appear inside this window. Keep this WAYLAND_DISPLAY=wayland-0 for the host; use WAYLAND_DISPLAY=wayland-1 for foot/weston-simple-shm.
```

**Other terminal** (same user, same `XDG_RUNTIME_DIR`):

```sh
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export WAYLAND_DISPLAY=wayland-1   # must be the name the shell printed, not wayland-0
foot
# or: weston-simple-shm
```

You should see a bottom panel (clock + **apps** / **grid**) and foot (or the
shm client) with a cyan/magenta SSD frame inside the
`worldr-shell (nested compositor)` window. On map it **scale+fades+rises in** (~280ms);
on close it scale+fades toward the panel (~240ms, minimize-to-panel).
Clicking another window gives a short lift/shadow/glow pulse. Workspace
switch is ease-in-out with a light dim. Pointer and keys while that window is focused are forwarded
into worldr (title-bar drag still works). Pointer buttons are paired
per client: a nested host release is forwarded only after a matching
press, and leave-while-down emits that matching release. Foot should
not log `stray button release event (compositor bug?)`.

```sh
./bin/worldr-shell --backend=wayland-client --effects=off --duration=60s   # no theater
./bin/worldr-shell --backend=wayland-client --effects=low --duration=60s   # fade only
```

The same `CompositeDesktop` path is used on `--backend=vk-display` / `drm`.

### Expose / overview (v0)

With **two or more** clients on the **active desktop**, focus the
`worldr-shell (nested compositor)` window and press **F12**. Actors on that
desktop animate into a grid (other workspaces stay hidden). Click a tile
(or ←/→ / Tab, then Enter) to focus and leave.
**Esc** leaves overview without quitting. **Ctrl+Q** quits the compositor.
Bare **Q** / **Esc** never quit while a client is on the desktop.

| Key | Action |
| --- | --- |
| F12 | Toggle overview (works with forwarded nested `wl_keyboard` evdev or evdev+8) |
| Super+Tab | Same toggle if KWin does not steal Super |
| ← → Tab | Move selection in overview |
| Enter | Focus selection and exit |
| Esc | Exit overview (does not quit). On an empty desktop, still quits. |
| Ctrl+Q | Quit the compositor |

Smoke without pressing keys (video-less):

```sh
./bin/worldr-shell --backend=wayland-client --overview-demo --duration=20s
# other terminals: two foots on the printed WAYLAND_DISPLAY
```

Overview auto-enters ~0.6s after the first window maps.

### Panel + launcher (v0)

A **bottom panel** is always composited (clock, `worldr` label, focused
window title, **apps**, **grid**). The compositor output is the area above
the bar so new windows do not sit under it.

**Launch foot without a second terminal:** focus the nested worldr window,
press **F1** (or click **apps**), highlight `foot`, Enter. The child gets
the printed `WAYLAND_DISPLAY` (host `wayland-0` is stripped). Super+Space
is the same bind when KWin does not steal Super.

**Menus (0.9.8):** in foot, right-click the terminal. The context menu is an
`xdg_popup` (no SSD title bar) stacked above the window. Clicks on the menu
should reach the popup surface.

**Clipboard (0.9.19 / 0.9.25):** in foot, select text, Ctrl+Shift+C, then Ctrl+Shift+V
(or another worldr client). Middle-click pastes the primary selection.

**Drag-and-drop (0.9.29):** drag text or a supported image (`png`/`jpeg`/`webp`/`bmp`)
from one worldr client onto another (copy action). Drop on empty desktop/panel
cancels — no icon-canvas target yet.
**Images:** copy `image/png`, `image/jpeg`, `image/webp`, or `image/bmp` between
worldr clients (exact MIME). Nested under Plasma: `text/plain` still works;
those image types are forwarded both ways when the host advertises them.
vk-display/drm stay in-compositor only.

**Scale (0.9.16 / 0.9.21):** nested under Plasma, worldr reads host `wl_output.scale`
and `wp_fractional_scale` `preferred_scale` (120ths) and drives its own
output. A 150% Plasma panel → `preferred_scale` 180, `wl_output.scale` 2.
abox try saw **1.75** (`preferred_scale=210/120`, `wl_output.scale=2`).
`--scale=1.5` (or `1.25` / `2`) still overrides. vk-display/drm stay **1.0**
unless `--scale` is set. **0.9.21:** `xdg_surface.set_window_geometry` width/height
crop the actor so GTK4 CSD shadows are not wrapped in SSD.

**KMS scanout (0.9.17 / 0.9.23 / 0.9.30):** on a spare TTY, `--backend=drm`, start `kitty` (or
another GL client) and fullscreen it. One opaque ARGB/XRGB buffer that covers
the output should print `kms scanout: primary dmabuf`. Windowed dmabuf clients
may print `kms overlay` (overlay plane + desktop on primary). A small software
cursor may print `kms cursor` (hardware cursor plane). Failure of either stays
on the blit path (`kms overlay fallback` / `kms cursor fallback`). Overview,
launcher, workspace slide, theater, or shm (`foot`) stay compose.
`--backend=vk-display` prints `kms scanout fallback` / overlay fallback
(Vulkan holds DRM master) and keeps the GPU blit. **0.9.30:** drm opens an
offscreen Vulkan ICD so acquire waits are `vkWaitSemaphores` (log
`linux-drm-syncobj: … Vulkan timeline wait`), not ioctl-only. NVIDIA/AMD: same
try; expect fallback if `AddFB2` rejects the modifier. Nested is unchanged.

**Icons (0.9.18):** `xdg_toplevel_icon` buffers still win when the client sets
one. Otherwise worldr looks up `.desktop` `Icon=` / `AppID` in `$XDG_ICON_THEME`
then **hicolor** (`…/icons/<theme>/<size>/apps/<name>.png`, then `pixmaps/`).
Launcher rows show those PNGs, or a rasterized SVG when no PNG exists.
Missing → default glyph. Complex SVG (filters/text) stays the default glyph.
`$XDG_ICON_THEME=breeze` (Plasma) or `Adwaita` (GNOME) if unset. Foot often
has `utilities-terminal` / `foot` in hicolor on Arch.

| Key / click | Action |
| --- | --- |
| F1 | Toggle launcher |
| Super+Space | Same toggle if the host allows Super |
| Panel **apps** | Open launcher (stays open). Click **apps** again to close. Outside-click dismisses only after pointer release. |
| Panel **grid** | Toggle overview (same as F12) |
| ↑ ↓ Tab | Move launcher selection |
| Enter / click row | Spawn that command |
| Esc | Close launcher (does not quit) |

The list is scanned from XDG `.desktop` files (`~/.local/share/applications`
and `$XDG_DATA_DIRS/applications`). `Name=` is shown; `Exec=` is launched
with `%f` / `%F` / `%u` / `%U` field codes stripped. `Hidden` / `NoDisplay`
/ `Terminal=true` entries are skipped. If the scan is empty, the fallback
is `foot`, `weston-simple-shm`, and `xeyes` / `xterm` / `xcalc` when `--xwayland`
is on. Missing binaries log `launcher: … not on PATH` and the shell keeps
running.

### Workspaces (0.9.14)

**2–4** virtual desktops, default **3** (`--workspaces=N`). New clients
(launcher or an external `foot`) spawn on the **active** desktop. F12
overview lists **only that desktop** and paints `desk N/M`. Empty desktops
stay addressable (switch, pager click, and move-onto).

One scheme (no Super+1..N): **Ctrl+Alt+←/→** switches; **Ctrl+Alt+Shift+←/→**
moves the focused window and follows (wraps). Evdev or evdev+8. The desktop
layer slides horizontally (~260ms, reused). Plasma often steals Ctrl+Alt+arrows
— use the pager dots in the nested window, or soak on a spare TTY.

The panel pager shows **N/M** plus dots: dim = empty, bright = occupied,
brand/larger = current.

```sh
./bin/worldr-shell --backend=wayland-client --duration=60s
# F1 → foot on desktop 0
# click the second pager dot (or Ctrl+Alt+→)
# F1 → foot on desktop 1
# focus the first foot, Ctrl+Alt+Shift+→  (window follows to desktop 1)
```

| Key / click | Action |
| --- | --- |
| Ctrl+Alt+→ | Next desktop (wraps; empty dest OK) |
| Ctrl+Alt+← | Previous desktop (wraps; empty dest OK) |
| Ctrl+Alt+Shift+→ | Move focused window to next desktop and follow |
| Ctrl+Alt+Shift+← | Move focused window to previous desktop and follow |
| Pager **N/M** + dots | Current index / count; click a dot to jump |

`--compositor=false` restores the old clear-only debug window (no socket).

Host-side bind is clamped (`min(our_max, advertised)`): compositor, shm,
`xdg_wm_base`, `wl_seat` (v≤5), `wl_data_device_manager` (v≤3),
`zwp_primary_selection_device_manager_v1`, `wl_output` (v≤2), and
`wp_fractional_scale_manager_v1` when advertised. Advertise/bind lines go
to stderr as `wayland-client: global …` / `bind …`.

If the host window fails to map, paste those lines. Workaround: spare TTY
`scripts/try-tty.sh`.

### X11 apps via XWayland (spike)

Same nested window, plus a rootless Xwayland child attached to the **worldr**
socket (not Plasma’s Xwayland) and a tiny compositing XWM (`-wm` fd,
`CompositeRedirectSubwindows` + MapRequest / ConfigureRequest).
X11 windows become actors via `xwayland_shell_v1`. Managed normals get SSD;
titles come from `WM_NAME` / `_NET_WM_NAME` and `WM_CLASS`. Override-redirect
and transients (menus, tooltips) skip SSD and keep the client position.

```sh
./bin/worldr-shell --backend=wayland-client --xwayland --duration=60s
```

The shell prints `xwayland: DISPLAY=:N`. **Other terminal:**

```sh
export DISPLAY=:N          # the number the shell printed — not $DISPLAY from Plasma
export XDG_RUNTIME_DIR=/run/user/$(id -u)
xeyes
# or: xterm
# harder: xcalc (menus should appear without SSD; click another xterm to restack)
```

Do **not** set `WAYLAND_DISPLAY` for xeyes/xterm (they are X11 clients).
Do **not** use the host `DISPLAY=:0` — that is Plasma, not worldr.

If `Xwayland` is missing: `pacman -S xorg-xwayland`. X11 clients are **not** on
a stock Arch desktop — also `sudo pacman -S xorg-xeyes xterm xorg-xcalc`. The compositor
still hosts foot. 0.9.11: `_NET_SUPPORTED` / active window / titles / click-to-focus
and raise; override-redirect and `WM_TRANSIENT_FOR` skip SSD. Still not a full
ICCCM WM (no reparenting, no pager/struts, no IME). xterm Ctrl+right-click or
xcalc menus are the harder check.
worldr now requests a free `DISPLAY` starting at `:1` so it does not clash with
Plasma’s `:0` (avoids `_XSERVTransSocketUNIXCreateListener: server already running`).

Fullscreen nested (still inside your compositor):

```sh
./bin/worldr-shell --backend=wayland-client --fullscreen-client --duration=15s
```

## Real display on a spare TTY (vk-display soak)

`vk-display` / `drm` take DRM master. **Daily demo stays nested** on Plasma
(`--backend=wayland-client`). This path is the architecture’s real compositor
seat on **bare-metal Intel iGPU** (abox / Mesa). The cloud agent does not have
a TTY; soak on the machine after merge.

`--backend=auto` prefers **nested** when `WAYLAND_DISPLAY` is set. On a TTY
with `/dev/dri/card*` and no graphical session env it tries **vk-display**,
then **drm**. Takeover is refused unless `--take-over-display`. A leftover
`XDG_SESSION_TYPE=wayland` **without** a host socket also refuses (that used
to pick vk-display and steal the GPU). `--card` must be `/dev/dri/cardN`
(not `renderD*`). GPU waits on vk-display time out after **2s** so a lost
master does not hang the VT forever.

Panel, overview, launcher, workspaces, and effects use the same
`CompositeDesktop` path as nested.

### Exact abox soak (Intel iGPU)

Leave Plasma on its VT. Do **not** pass `--take-over-display` from the desktop.

1. **Ctrl+Alt+F3** → tty3. Log in (real logind session).
2. Groups if needed (once): `sudo usermod -aG video,render "$USER"` then re-login.
3. Confirm the node and ICD:

```sh
ls /dev/dri/card*          # need cardN, not only renderD128
vulkaninfo --summary       # Mesa Intel, Vulkan 1.4
echo "seat=$XDG_SEAT vt=$XDG_VTNR type=$XDG_SESSION_TYPE"
# type should be tty (or empty). WAYLAND_DISPLAY and DISPLAY must be unset.
```

4. `cd` to the worldr tree and soak **vk-display** first (15s cap):

```sh
./scripts/try-tty.sh
# explicit:
DURATION=15s BACKEND=vk-display ./scripts/try-tty.sh
# second GPU / wrong card:
# CARD=/dev/dri/card1 DURATION=15s BACKEND=drm ./scripts/try-tty.sh
```

The script builds `bin/worldr-shell` if needed, refuses a graphical session
env, sets `XDG_RUNTIME_DIR`, and runs `--duration=15s`.

5. Success looks like a dark cinematic clear + bottom panel, and:

```
present: backend=vk-display size=… device=…
seat: kind=tty …
desktop: CompositeDesktop (panel, overview, launcher, workspaces, effects) …
wayland compositor: WAYLAND_DISPLAY=wayland-1
```

6. From **another TTY or SSH** (same user), attach a client:

```sh
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export WAYLAND_DISPLAY=wayland-1   # name the shell printed
foot                               # or F1 on the TTY if evdev keys work
```

7. When the duration expires (or **Ctrl+Q** / Ctrl+C): **Ctrl+Alt+F1** or **F2**
   back to Plasma.

**If the TTY looks wedged:** **Ctrl+Alt+F4**, `pkill worldr-shell`, then F1/F2.
vk-display present/teardown waits at most 2s; if KMS is still blank, a VT
switch usually restores Plasma’s CRTC.

**vk-display failed, drm fallback:**

```sh
DURATION=15s BACKEND=drm ./scripts/try-tty.sh
```

Manual equivalent:

```sh
unset WAYLAND_DISPLAY DISPLAY
export XDG_RUNTIME_DIR=/run/user/$(id -u)
./bin/worldr-shell --backend=vk-display --duration=15s --color=#0b1020
# fallback: --backend=drm --duration=15s
```

### If you insist on running from the graphical session

```sh
./bin/worldr-shell --backend=vk-display --take-over-display --duration=10s
```

This can yank the GPU from your running compositor. Prefer **tty3**.

## Headless / CI

```sh
./bin/worldr-shell --backend=headless --duration=2s
```

Creates a Vulkan instance if an ICD exists; with no GPU it still runs a blank
compositor seat (if `--compositor` is on, default) and exits. Useful on VMs
without `/dev/dri`.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--backend` | `auto` | `vk-display` \| `drm` \| `wayland-client` \| `nested` \| `headless` |
| `--take-over-display` | false | Allow vk-display/drm while WAYLAND_DISPLAY/DISPLAY or XDG_SESSION_TYPE=wayland\|x11 |
| `--duration` | 0 (until signal) | Safety timer |
| `--color` | `#0b1020` | Clear color |
| `--compositor` | true | Listen as Wayland server (on for `wayland-client`/`nested` too) |
| `--xwayland` | false | Launch rootless Xwayland on the worldr socket |
| `--effects` | `high` | Window theater: `high` (scale+fade+focus pulse) \| `low` (fade) \| `off` |
| `--overview-demo` | false | Auto-enter expose after the first window maps |
| `--workspaces` | `3` | Virtual desktops (clamped 2–4) |
| `--scale` | `0` (auto) | Output scale `1` / `1.25` / `1.5` / `2`. Auto: nest follows host `wl_output` / `wp_fractional_scale`; vk-display/drm stay 1.0. Explicit `--scale` always wins. |
| `--wayland-display` | first free `wayland-N` | Socket name |
| `--ssd` | true | Server-side decoration chrome |
| `--card` | first `/dev/dri/cardN` | DRM device |
| `--list-devices` | | Print Vulkan devices and exit |

## Binding choice

Vulkan and DRM are a **thin owned C wrapper** (`internal/platform/linux/native`)
linked against `libvulkan` and `libdrm`. Generated no-cgo bindings
(`lukem570/vulkan-go`) omit `VK_KHR_display`. wgpu is not used.

## Client matrix (abox)

PR #2 (`feat/linux-dmabuf-and-clients`) **builds** on abox. Headless reports
`linux-dmabuf: Vulkan import + readback enabled`. **foot connected and
disconnected cleanly** against the compositor.

| Client | Buffer | Expected now | Notes |
| --- | --- | --- | --- |
| `weston-simple-shm` | wl_shm | **Works** | First smoke test |
| `foot` | wl_shm | **Works (confirmed on abox)** | Seat + full US xkb + SSD. Type freely; **Ctrl+Q** quits worldr. Pointer press/release is paired per client — the `stray button release event (compositor bug?)` warning should be gone (0.9.5). **Right-click** should open the context menu (`xdg_popup`, 0.9.8) without a title bar. **Copy/paste** (0.9.19): Ctrl+Shift+C / Ctrl+Shift+V `text/plain` between worldr clients and, when nested, Plasma ↔ foot. `image/png` (and `image/bmp`) between clients; nest host forwards png when advertised. Mouse-select + middle-click uses primary (bridged if the host advertises it). Cursors, activation, **fractional-scale** (0.9.16: nest follows Plasma HiDPI; `--scale` overrides). **Icons** (0.9.18 / **0.9.28**): theme PNG then SVG from `.desktop` `Icon=` / `AppID` (`Inherits=` + hicolor; librsvg if `make` found it); `xdg_toplevel_icon` buffer still wins. **Workspaces** (0.9.14): Ctrl+Alt+←/→ switch, Ctrl+Alt+Shift+←/→ move+follow. Still expected: text-input/IME. |
| `kitty` | linux-dmabuf (GL) | **Try** | On **vk-display** (0.9.10+): Vulkan import + GPU blit. **0.9.17**: fullscreen ARGB/XRGB may KMS-scanout on `--backend=drm` (`kms scanout: primary dmabuf`). vk-display logs `kms scanout fallback` (Vulkan holds DRM master) then blits. Nested still CPU. |
| `alacritty` | linux-dmabuf | **Try** | Same as kitty; may want more EGL/Vulkan extras |
| `firefox` | dmabuf + gtk extras | **Unlikely** | Popups/subsurfaces exist (0.9.8); still needs clipboard MIME, idle-inhibit, etc. |
| `brave` / Chromium | ozone Wayland + dmabuf | **Try (0.9.27)** | 0.9.26 fixed Qt-shim `selection(nil)` SEGV. 0.9.27: nest dmabuf feedback is LINEAR-only; `create_immed` keeps the fd; `wl_surface.enter` + `preferred_buffer_scale`; `xdg_toplevel` `activated` + `xdg_activation` focus. Prefer GPU (`--ozone-platform=wayland`). If the GPU process still dies: `--disable-gpu`. |
| `ark` | Qt6 Wayland | **Try (0.9.27)** | 0.9.26: no `selection` before `keyboard.enter`. 0.9.27: surface.enter, preferred_buffer_scale, activated configure, activation focus. Window should stay up. |
| `gnome-disks` | GTK4 + fractional scale | **Try (0.9.21)** | 0.9.20 at nest host scale **1.75** left a huge empty gap between content and SSD (window geometry width/height was dropped; CSD shadow counted as the client). 0.9.21 crops to `set_window_geometry`. |
| X11 apps | XWayland | **Try (`--xwayland`)** | Rootless `Xwayland` + EWMH-ish XWM. `DISPLAY=:N xeyes` / `xterm` map as SSD actors with real titles. `xcalc` (or xterm’s Ctrl+right-click menu) should be chrome-less. Click-to-focus raises + `SetInputFocus`. |

GPU-accelerated path (vk-display, 0.9.10): client dmabuf → Vulkan import → **sample/blit in the compositor pass**. **0.9.17 KMS scanout:** one fullscreen opaque ARGB/XRGB dmabuf on `--backend=drm` → `drmPrimeFDToHandle` + `AddFB2` + atomic/`SetCrtc` (Intel first; NVIDIA/AMD best-effort). Ineligible or `vk-display` (no second DRM master) → existing blit. Nested/shm unchanged.

Start `kitty` only after the shell prints `linux-dmabuf: Vulkan import + readback enabled`.

### Nested compositor on Plasma/KWin (abox)

PR #3 (`feat/client-harden`) maps a host window: 514 frames, exit 0, binds
clamped to compositor/shm/`xdg_wm_base`. This branch **adds the compositor
into that window**.

`auto` in a graphical session now picks this path (nested + socket), not a
clear-only debug rectangle.

Host binds (clamped): compositor, shm, `xdg_wm_base`, `wl_seat` ≤ v5 (pointer +
keyboard forwarded into worldr), `wl_output` ≤ v2, `wp_fractional_scale` when
advertised, and `wp_cursor_shape_manager_v1` v1 (default arrow on enter; shm
`set_cursor` fallback). Still skipped: viewporter, dmabuf.

**Success:** window titled `worldr-shell (nested compositor)` + bottom panel
with pager dots. **F1 → foot** on desktop 0, switch desktop, **F1 → foot**
on desktop 1.

**If the host window dies:** paste `wayland-client: global/bind` lines.
Workaround — real display on **tty3**:

```sh
# Ctrl+Alt+F3, login, then:
./scripts/try-tty.sh
```

## Known gaps

- XWayland (0.9.11): `--xwayland` rootless + tiny XWM + `xwayland_shell_v1`. EWMH basics (`_NET_SUPPORTED`, active window, titles/class, delete/take-focus). Not a full ICCCM WM (no reparenting/pager). Overlay menus skip SSD.
- `xdg_popup` + `wl_subsurface` stacking (0.9.8): menus/tooltips/dropdowns. Positioner uses size + anchor + offset (no constraint/flip). Foot right-click menu is the abox check.
- Clipboard (0.9.19 / **0.9.25**): `text/plain` + `image/png` / `image/jpeg` / `image/webp` / `image/bmp` between worldr clients. Nested: Plasma ↔ worldr for text and those images when the host advertises them. Primary bridged if advertised. vk-display/drm have no host to bind.
- Drag-and-drop (**0.9.29**): `wl_data_device.start_drag` between worldr clients for those same MIME types (copy). Empty-desktop drop cancels (no icon canvas — follow-up). No nest-host DND bridge.
- dmabuf (0.9.17+ / **0.9.23** / **0.9.30**): GPU sample on vk-display; **KMS primary scanout** for one fullscreen ARGB/XRGB on `--backend=drm`; **overlay** for one windowed dmabuf when the card has an overlay plane; **cursor plane** for a small ARGB cursor. **0.9.20 / 0.9.23 / 0.9.30:** `wp_linux_drm_syncobj_manager_v1` when `DRM_CAP_SYNCOBJ_TIMELINE` (log `linux-drm-syncobj: … advertised`). Acquire waits on a Vulkan timeline (`vkWaitSemaphores`) before blit/sample/scanout on **every** present path that has a Vulkan session — including `--backend=drm`, which now opens an offscreen ICD so it is no longer ioctl-only. DRM ioctl is the fallback. Release is signaled after present. Miss → implicit sync. vk-display extra planes are eligible but usually compose (`VK_KHR_display` holds master). Nested host still CPU-composites. Soak: spare TTY, `kitty` — look for the Vulkan syncobj line (not “ioctl wait”) plus `kms scanout` / `kms overlay` / blit.
- SSD is thicker accent + title gradient + focused glow (still not a toolkit)
- Software cursor (0.9.21 / **0.9.22** / **0.9.23**): nest binds host `wp_cursor_shape_manager_v1` and `set_shape(default)` on enter (shm arrow fallback) using the enter serial, **without taking `Window.mu` again** (0.9.21 deadlocked `TakeInput` vs `readLoop` — frozen nest, no click/key). Client `set_cursor` / cursor-shape still draw the software overlay; null `set_cursor` keeps the default arrow. `--backend=drm` tries a hardware cursor plane when the image is ≤256×256; miss stays software. vk-display / nested stay software (no DRM master for extra planes).
- Nested demo: host pointer/keys while the worldr window is focused; evdev still used on TTY. Unmatched host `wl_pointer.button` releases are dropped (foot stray-release warning should be gone).
- Brave / ark (**0.9.26** / **0.9.27**): do **not** send `wl_data_device.selection` on `get_data_device` (0.9.26). 0.9.27 nest `zwp_linux_dmabuf` feedback is LINEAR-only so ozone can mmap; `create_immed` keeps the client fd; first map sends `wl_surface.enter` + `preferred_buffer_scale` (compositor v6) and `xdg_toplevel.configure` with `activated`. `xdg_activation.activate` focuses + `wl_keyboard.enter`. `WAYLAND_DEBUG=1`: after `get_keyboard` expect `keymap` + `repeat_info`; after first commit expect `wl_surface.enter`. Remaining flag if GPU still dies: `brave --ozone-platform=wayland --disable-gpu`. GNOME Disks still deferred (GTK). No IME.
- Fractional SSD (0.9.21): window geometry width/height crop CSD padding at non-integer scales (1.75 nest). Viewport dest + buffer-scale + preferred_scale still size the logical surface.
- Keymap is a full US layout (`keymap_us.xkb`). **Ctrl+Q** quits; normal typing goes to the focused client. No IME (`zwp_text_input`) yet.
- Fractional scale (0.9.16 / 0.9.21): nest follows host `preferred_scale` / `wl_output.scale`. `--scale` overrides. vk-display/drm default 1.0. Viewport / `set_buffer_scale` / window geometry size the logical window. Single worldr output.
- Window icons (0.9.18 / **0.9.25** / **0.9.28**): `xdg_toplevel_icon` shm buffers on SSD + panel win when set. Else XDG theme PNG, then SVG (`Icon=` / `AppID` / `set_name`) walking `index.theme` `Inherits=` then hicolor. librsvg when built with `librsvg2-dev` + `make` (`-tags=librsvg`); else the simple raster. No full Directory/Size graph. No IME (`zwp_text_input`).
- Compiz theater (**0.9.24**): hardcoded scale/fade/rise map-in, minimize-to-panel unmap, focus glow, ease-in-out workspace slide. No wobbly/cube. `--effects=off|low|high`. Frame loop reuses slices (no per-frame actor-list storm).
- Launcher reads XDG `.desktop` files and theme PNGs for `Icon=` (0.9.18). `Terminal=true` apps skipped; no ibus/fcitx IME
- Panel is CPU-composited chrome (not a toolkit)
- Workspaces (0.9.14): Ctrl+Alt+←/→ switch, Ctrl+Alt+Shift+←/→ move+follow. No Super+1..N, no drag-to-desktop, no per-output set. Overview is current-desktop only (shows `desk N/M`). Empty desktops stay addressable.
- vk-display/drm need DRM master on a spare VT (scripts/try-tty.sh). CI exercises refuse / no-DRM / render-node `--card` paths only; soak vk-display on abox after merge.
- UI toolkit still deferred

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
  weston \
  xorg-xwayland xorg-xeyes xterm
```

`weston` provides `weston-simple-shm`. `xorg-xwayland` + `xterm` / `xorg-xeyes` are for `--xwayland`.

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

Needs CGO, `libvulkan`, and `libdrm` (the C ABI boundary). No huge vendored trees.

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
`worldr-shell (nested compositor)` window. On map it **scale+fades in** (~260ms);
on close it scale+fades out (~220ms). Clicking another window gives a short
lift/shadow pulse. Pointer and keys while that window is focused are forwarded
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

**Clipboard (0.9.9):** in foot, select text, Ctrl+Shift+C, then Ctrl+Shift+V
(or another worldr client). Middle-click pastes the primary selection.
This stays inside worldr — it does not copy into the Plasma/KWin clipboard.

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
is `foot`, `weston-simple-shm`, and `xeyes` / `xterm` when `--xwayland`
is on. Missing binaries log `launcher: … not on PATH` and the shell keeps
running.

### Workspaces (v0)

**2–4** virtual desktops, default **3** (`--workspaces=N`). New clients
(launcher or an external `foot`) spawn on the **active** desktop. F12
overview lists **only that desktop**.

Switch with **Ctrl+Alt+←/→** (evdev or evdev+8) or click the panel **pager
dots**. The desktop layer slides horizontally (~260ms). Plasma often steals
Ctrl+Alt+arrows — use the dots in the nested window.

```sh
./bin/worldr-shell --backend=wayland-client --duration=60s
# F1 → foot on desktop 0
# click the second pager dot (or Ctrl+Alt+→)
# F1 → foot on desktop 1
```

| Key / click | Action |
| --- | --- |
| Ctrl+Alt+→ | Next desktop (wraps) |
| Ctrl+Alt+← | Previous desktop (wraps) |
| Pager dot | Jump to that desktop |

`--compositor=false` restores the old clear-only debug window (no socket).

Host-side bind is still clamped (`min(our_max, advertised)`): compositor, shm,
`xdg_wm_base`, and `wl_seat` (v≤5) so clicks/keys work. Advertise/bind lines go
to stderr as `wayland-client: global …` / `bind …`.

If the host window fails to map, paste those lines. Workaround: spare TTY
`scripts/try-tty.sh`.

### X11 apps via XWayland (spike)

Same nested window, plus a rootless Xwayland child attached to the **worldr**
socket (not Plasma’s Xwayland) and a tiny compositing XWM (`-wm` fd,
`CompositeRedirectSubwindows` + MapRequest / ConfigureRequest).
X11 windows become actors + SSD via `xwayland_shell_v1`.

```sh
./bin/worldr-shell --backend=wayland-client --xwayland --duration=60s
```

The shell prints `xwayland: DISPLAY=:N`. **Other terminal:**

```sh
export DISPLAY=:N          # the number the shell printed — not $DISPLAY from Plasma
export XDG_RUNTIME_DIR=/run/user/$(id -u)
xeyes
# or: xterm
```

Do **not** set `WAYLAND_DISPLAY` for xeyes/xterm (they are X11 clients).
Do **not** use the host `DISPLAY=:0` — that is Plasma, not worldr.

If `Xwayland` is missing: `pacman -S xorg-xwayland`. X11 clients are **not** on
a stock Arch desktop — also `sudo pacman -S xorg-xeyes xterm`. The compositor
still hosts foot. Known spike limits: no full EWMH (no `_NET_WM_*` desktop, no
reparenting), override-redirect / popups may mis-size, titles default to `X11`.
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
| `foot` | wl_shm | **Works (confirmed on abox)** | Seat + full US xkb + SSD. Type freely; **Ctrl+Q** quits worldr. Pointer press/release is paired per client — the `stray button release event (compositor bug?)` warning should be gone (0.9.5). **Right-click** should open the context menu (`xdg_popup`, 0.9.8) without a title bar. **Copy/paste** (0.9.9): Ctrl+Shift+C / Ctrl+Shift+V between worldr clients (`text/plain`); mouse-select + middle-click uses primary. Not bridged to Plasma’s clipboard yet. Cursors, activation, fractional-scale 120. Still expected: `xdg-toplevel-icon`, text-input/IME. |
| `kitty` | linux-dmabuf (GL) | **Try** | GPU path: Vulkan import + CPU readback. Needs `linux-dmabuf: Vulkan import` in the shell log. LINEAR mmap fallback if the buffer is linear. |
| `alacritty` | linux-dmabuf | **Try** | Same as kitty; may want more EGL/Vulkan extras |
| `firefox` | dmabuf + gtk extras | **Unlikely** | Popups/subsurfaces exist (0.9.8); still needs clipboard MIME, idle-inhibit, etc. |
| X11 apps | XWayland | **Try (`--xwayland`)** | Rootless `Xwayland` + tiny XWM on the worldr socket. `DISPLAY=:N xeyes` / `xterm` should map as SSD actors. |

GPU-accelerated path: client dmabuf → `VK_EXT_external_memory_dma_buf` import → copy to linear host image → actor pixels → existing SSD + focus + present. shm remains the fallback.

Start `kitty` only after the shell prints `linux-dmabuf: Vulkan import + readback enabled`.

### Nested compositor on Plasma/KWin (abox)

PR #3 (`feat/client-harden`) maps a host window: 514 frames, exit 0, binds
clamped to compositor/shm/`xdg_wm_base`. This branch **adds the compositor
into that window**.

`auto` in a graphical session now picks this path (nested + socket), not a
clear-only debug rectangle.

Host binds (clamped): compositor, shm, `xdg_wm_base`, `wl_seat` ≤ v5 (pointer +
keyboard forwarded into worldr). Still skipped: output, viewporter, dmabuf,
cursor-shape.

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

- XWayland spike: `--xwayland` rootless + tiny XWM + `xwayland_shell_v1` (not a full EWMH WM)
- `xdg_popup` + `wl_subsurface` stacking (0.9.8): menus/tooltips/dropdowns. Positioner uses size + anchor + offset (no constraint/flip). Foot right-click menu is the abox check.
- Clipboard (0.9.9): `text/plain` between worldr clients. Nested host clipboard (Plasma ↔ worldr) is a follow-up — the nest client does not bind host `wl_data_device`.
- No zero-copy GPU composite (import is readback)
- SSD is thicker accent + title gradient + focused glow (still not a toolkit)
- Software cursor: `wp_cursor_shape` theme + client shm hotspot (no hardware plane)
- Nested demo: host pointer/keys while the worldr window is focused; evdev still used on TTY. Unmatched host `wl_pointer.button` releases are dropped (foot stray-release warning should be gone).
- Keymap is a full US layout (`keymap_us.xkb`). **Ctrl+Q** quits; normal typing goes to the focused client. No IME (`zwp_text_input`) yet.
- Fractional scale stub: `preferred_scale` 120 (1.0); still integer composite. No `xdg-toplevel-icon` or IME (`zwp_text_input`)
- Compiz theater v0 is hardcoded (no plugin graph); `--effects=off` disables
- Launcher reads XDG `.desktop` files (no icon theme yet; `Terminal=true` apps skipped; no ibus/fcitx IME)
- Panel is CPU-composited chrome (not a toolkit)
- Workspaces v0: no drag-to-desktop, no per-output set, overview is current-desktop only
- vk-display/drm need DRM master on a spare VT (scripts/try-tty.sh). CI exercises refuse / no-DRM / render-node `--card` paths only; soak vk-display on abox after merge.
- UI toolkit still deferred

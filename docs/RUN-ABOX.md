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

**Preferred real-display try:** a **spare TTY** (tty2), not the VT that is
already running Hyprland/Sway/GNOME/KDE.

| Key | What it does |
| --- | --- |
| Ctrl+Alt+F1…F7 | Switch virtual terminals. Your existing graphical session stays on its VT. |
| Ctrl+C | Stop `worldr-shell` if it is in the foreground on that TTY. |
| Esc or Q | Quit if evdev can open a keyboard (`input` group). In overview, Esc leaves the grid first. |
| F12 | Toggle expose/overview (nested window must be focused). Super+Tab if the host does not steal Super. |
| `--duration=15s` | Always exits — use this the first time on a TTY. |

If the TTY appears wedged: another TTY (`Ctrl+Alt+F3`), `pkill worldr-shell`,
then switch back. The DRM backend tries to restore the previous CRTC on exit.

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
# this branch (stacked on XWayland / nested compositor):
git checkout feat/overview-expose-v0

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

You should see foot (or the shm client) with a cyan/magenta SSD frame inside the
`worldr-shell (nested compositor)` window. On map it **scale+fades in** (~260ms);
on close it scale+fades out (~220ms). Clicking another window gives a short
lift/shadow pulse. Pointer and keys while that window is focused are forwarded
into worldr (title-bar drag still works).

```sh
./bin/worldr-shell --backend=wayland-client --effects=off --duration=60s   # no theater
./bin/worldr-shell --backend=wayland-client --effects=low --duration=60s   # fade only
```

The same `CompositeDesktop` path is used on `--backend=vk-display` / `drm`.

### Expose / overview (v0)

With **two or more** clients on the worldr socket, focus the
`worldr-shell (nested compositor)` window and press **F12**. Actors animate
into a grid. Click a tile (or ←/→ / Tab, then Enter) to focus and leave.
**Esc** leaves overview without quitting. **Esc** or **Q** outside overview
still quits.

| Key | Action |
| --- | --- |
| F12 | Toggle overview (works with forwarded nested `wl_keyboard` evdev or evdev+8) |
| Super+Tab | Same toggle if KWin does not steal Super |
| ← → Tab | Move selection in overview |
| Enter | Focus selection and exit |
| Esc | Exit overview; if already on the desktop, quit |
| Q | Quit (desktop only) |

Smoke without pressing keys (video-less):

```sh
./bin/worldr-shell --backend=wayland-client --overview-demo --duration=20s
# other terminals: two foots on the printed WAYLAND_DISPLAY
```

Overview auto-enters ~0.6s after the first window maps.

`--compositor=false` restores the old clear-only debug window (no socket).

Host-side bind is still clamped (`min(our_max, advertised)`): compositor, shm,
`xdg_wm_base`, and `wl_seat` (v≤5) so clicks/keys work. Advertise/bind lines go
to stderr as `wayland-client: global …` / `bind …`.

If the host window fails to map, paste those lines. Workaround: spare TTY
`--backend=vk-display --duration=15s`.

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

## Real display on a spare TTY (tryable compositor)

1. Leave your desktop running. Switch to tty2: **Ctrl+Alt+F2**.
2. Log in on that TTY (a real logind session).
3. Ensure a runtime dir:

```sh
export XDG_RUNTIME_DIR=/run/user/$(id -u)
mkdir -p "$XDG_RUNTIME_DIR"
cd /path/to/worldr
```

4. First time, time-box it:

```sh
./bin/worldr-shell --backend=vk-display --duration=15s --color=#0b1020
```

If `VK_KHR_display` is missing or not DRM master:

```sh
./bin/worldr-shell --backend=drm --duration=15s
```

`auto` on a TTY (no `WAYLAND_DISPLAY`/`DISPLAY`) tries `vk-display`, then `drm`,
then headless.

You should see a dark cinematic clear. The process also advertises a Wayland
socket (default `wayland-1`, never `wayland-0` if that name is taken):

```
wayland compositor: WAYLAND_DISPLAY=wayland-1
```

5. From **another TTY or SSH session** (not from the desktop that still owns
   `wayland-0` unless you export the new display):

```sh
export XDG_RUNTIME_DIR=/run/user/$(id -u)
export WAYLAND_DISPLAY=wayland-1
weston-simple-shm          # shm path (always)
foot                       # CONFIRMED on abox: connect + disconnect clean
kitty                      # GPU path: linux-dmabuf import (needs Vulkan on the shell)
```

A client surface should appear as a window actor with a cyan/magenta SSD frame.
`linux-dmabuf` is advertised (LINEAR + common Intel modifiers). Tiled GPU buffers
are imported via Vulkan and read back into the same SSD/composite path as shm.

6. Switch back to your desktop VT (often **Ctrl+Alt+F1** or F7) after the
   process exits.

### If you insist on running from the graphical session

```sh
./bin/worldr-shell --backend=vk-display --take-over-display --duration=10s
```

This can yank the GPU from your running compositor. Prefer tty2.

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
| `--take-over-display` | false | Allow DRM/Vulkan display while a session env is set |
| `--duration` | 0 (until signal) | Safety timer |
| `--color` | `#0b1020` | Clear color |
| `--compositor` | true | Listen as Wayland server (on for `wayland-client`/`nested` too) |
| `--xwayland` | false | Launch rootless Xwayland on the worldr socket |
| `--effects` | `high` | Window theater: `high` (scale+fade+focus pulse) \| `low` (fade) \| `off` |
| `--overview-demo` | false | Auto-enter expose after the first window maps |
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
| `foot` | wl_shm | **Works (confirmed on abox)** | Seat + xkb + SSD. Remaining foot warnings should drop for server-side cursors (`wp_cursor_shape` + `wl_pointer.set_cursor`), XDG activation (token `done`), and primary selection (stub). Still expected: fractional scale, `xdg-toplevel-icon`, text-input/IME. |
| `kitty` | linux-dmabuf (GL) | **Try** | GPU path: Vulkan import + CPU readback. Needs `linux-dmabuf: Vulkan import` in the shell log. LINEAR mmap fallback if the buffer is linear. |
| `alacritty` | linux-dmabuf | **Try** | Same as kitty; may want more EGL/Vulkan extras |
| `firefox` | dmabuf + gtk extras | **Unlikely** | Needs clipboard, popups, subsurfaces, idle-inhibit, etc. |
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

**Success:** window titled `worldr-shell (nested compositor)` + `foot` visible
inside it with SSD.

**If the host window dies:** paste `wayland-client: global/bind` lines.
Workaround — real display on **tty2**:

```sh
# Ctrl+Alt+F2, login, then:
unset WAYLAND_DISPLAY DISPLAY
export XDG_RUNTIME_DIR=/run/user/$(id -u)
./bin/worldr-shell --backend=vk-display --duration=15s
```

## Known gaps

- XWayland spike: `--xwayland` rootless + tiny XWM + `xwayland_shell_v1` (not a full EWMH WM)
- No `xdg_popup` / real subsurface stacking
- Clipboard / primary selection objects bind; no MIME transfer yet
- No zero-copy GPU composite (import is readback)
- SSD is thicker accent + title gradient + focused glow (still not a toolkit)
- Software cursor: `wp_cursor_shape` theme + client shm hotspot (no hardware plane)
- Nested demo: host pointer/keys while the worldr window is focused; evdev still used on TTY
- Pointer/keyboard keymap sent to clients is a tiny US map
- No fractional scaling, `xdg-toplevel-icon`, or IME (`zwp_text_input`)
- Compiz theater v0 is hardcoded (no plugin graph); `--effects=off` disables
- UI toolkit still deferred

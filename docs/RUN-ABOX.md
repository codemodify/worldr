# Run worldr-shell on abox (Arch, Intel Arrow Lake)

First tryable build: a `worldr-shell` binary that **builds with Go** and either

1. **clears the screen** via Vulkan `VK_KHR_display` or DRM/KMS, or
2. **shows a compositor seat** waiting for a Wayland client, or
3. **nests** as a Wayland client inside your existing session (safe debug path).

This machine: Intel Arrow Lake iGPU, Mesa 26.2.2, Vulkan 1.4, Arch Linux.

## Safety — do not steal your current session by accident

`vk-display` and `drm` take **DRM master** and will blank / replace the active
VT. If `WAYLAND_DISPLAY` or `DISPLAY` is set, `worldr-shell` **refuses** those
backends unless you pass `--take-over-display`.

**Preferred first try (safe):** nested window in your current desktop.

**Preferred real-display try:** a **spare TTY** (tty2), not the VT that is
already running Hyprland/Sway/GNOME/KDE.

| Key | What it does |
| --- | --- |
| Ctrl+Alt+F1…F7 | Switch virtual terminals. Your existing graphical session stays on its VT. |
| Ctrl+C | Stop `worldr-shell` if it is in the foreground on that TTY. |
| Esc or Q | Quit if evdev can open a keyboard (`input` group). |
| `--duration=15s` | Always exits — use this the first time on a TTY. |

If the TTY appears wedged: another TTY (`Ctrl+Alt+F3`), `pkill worldr-shell`,
then switch back. The DRM backend tries to restore the previous CRTC on exit.

## Packages (Arch)

```sh
sudo pacman -S --needed \
  go gcc pkgconf \
  vulkan-headers vulkan-icd-loader vulkan-intel vulkan-tools \
  mesa libdrm wayland wayland-protocols \
  weston   # provides weston-simple-shm for a client test
```

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
# this branch:
git checkout feat/linux-dmabuf-and-clients

export CGO_ENABLED=1
make build
# → bin/worldr-shell  bin/worldr-session
```

Or: `go build -o bin/worldr-shell ./cmd/worldr-shell`

List GPUs (no display takeover):

```sh
./bin/worldr-shell --list-devices
```

## Safe nested try (existing session)

```sh
./bin/worldr-shell --backend=wayland-client --duration=20s
```

A 1280×720 (or `--width`/`--height`) window titled `worldr-shell (nested debug)`
should fill with the cinematic clear color (`--color=#0b1020`). This uses
**wl_shm**, not Vulkan WSI. It is **not** the compositor path.

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
foot                       # expected: shm + seat/keyboard + SSD
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
| `--backend` | `auto` | `vk-display` \| `drm` \| `wayland-client` \| `headless` |
| `--take-over-display` | false | Allow DRM/Vulkan display while a session env is set |
| `--duration` | 0 (until signal) | Safety timer |
| `--color` | `#0b1020` | Clear color |
| `--compositor` | true | Listen as Wayland server (off for `wayland-client`) |
| `--wayland-display` | first free `wayland-N` | Socket name |
| `--ssd` | true | Server-side decoration chrome |
| `--card` | first `/dev/dri/cardN` | DRM device |
| `--list-devices` | | Print Vulkan devices and exit |

## Binding choice

Vulkan and DRM are a **thin owned C wrapper** (`internal/platform/linux/native`)
linked against `libvulkan` and `libdrm`. Generated no-cgo bindings
(`lukem570/vulkan-go`) omit `VK_KHR_display`. wgpu is not used.

## Client matrix (abox expectations)

| Client | Buffer | Expected now | Notes |
| --- | --- | --- | --- |
| `weston-simple-shm` | wl_shm | **Works** | First smoke test |
| `foot` | wl_shm | **Likely** | Needs seat + xkb keymap (we send a minimal US map) + xdg_decoration SSD |
| `kitty` | linux-dmabuf (GL) | **Try** | GPU path: Vulkan import + CPU readback. Needs `linux-dmabuf: Vulkan import` in the shell log. LINEAR mmap fallback if the buffer is linear. |
| `alacritty` | linux-dmabuf | **Try** | Same as kitty; may want more EGL/Vulkan extras |
| `firefox` | dmabuf + gtk extras | **Unlikely** | Needs clipboard, popups, subsurfaces, idle-inhibit, etc. |
| X11 apps | XWayland | **No** | Next after dmabuf hardening — not hooked up |

GPU-accelerated path: client dmabuf → `VK_EXT_external_memory_dma_buf` import → copy to linear host image → actor pixels → existing SSD + focus + present. shm remains the fallback.

Start `kitty` only after the shell prints `linux-dmabuf: Vulkan import + readback enabled`.

## Known gaps

- No XWayland (next after this if you want legacy X11 apps)
- No `xdg_popup` / real subsurface stacking
- No clipboard (data device is a stub so binds succeed)
- No zero-copy GPU composite (import is readback)
- SSD is a colored frame + title hit region
- Pointer/keyboard via evdev (`input` group); keymap is a tiny US map
- Compiz effects and UI toolkit still deferred

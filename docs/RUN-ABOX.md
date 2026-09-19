# Build and run on abox

The default experience is a general spatial workspace with native PTY terminals,
a read-only native project browser, and compatible Wayland applications.
AXIAL / 07 is available from the general workspace APPS rail or `--axial` as a
hosted native 3D tool with retained meshes, shared depth/picking and session
restore. It also remains available as a standalone engineering study with retained meshes
and an instrument surface.

## Arch dependencies

```sh
sudo pacman -S --needed go gcc pkgconf vulkan-headers vulkan-icd-loader libdrm wayland libxkbcommon libvterm fontconfig pango libxcb seatd libinput systemd-libs vulkan-intel
make build
make test-gpu
```

The bring-up target is Intel Arrow Lake with Mesa. Use the appropriate Vulkan
driver on other GPUs. `./bin/worldr-shell --list-devices` reports devices.
Debian/Ubuntu builds need `libvulkan-dev`, `libdrm-dev`, `libwayland-dev`,
`libxkbcommon-dev`, `libvterm-dev` (libvterm 0.3+), `libfontconfig1-dev`, `libpango1.0-dev`,
`libxcb1-dev`, `libxcb-composite0-dev`, `libxcb-res0-dev`, `libxcb-xfixes0-dev`, `libseat-dev`,
`libinput-dev`, and `libudev-dev` with Go, a C toolchain,
pkg-config, and a driver.

If the Vulkan loader is installed but its headers are supplied by a separate
Vulkan-Headers checkout, point the build at that checkout:

```sh
make VULKAN_HEADERS=/path/to/Vulkan-Headers build
make VULKAN_HEADERS=/path/to/Vulkan-Headers test
```

Use the actual local checkout path; this does not replace the other development
libraries or the Vulkan driver.

Shader development needs a shader compiler; `make shaders` regenerates the
checked-in binaries from GLSL. Generated XDG-shell protocol C/header files are
also checked in, so normal builds need no protocol scanner. Font data comes
from the Go module dependency; native terminal glyphs missing from Go Mono use
installed fonts through Fontconfig. Installing a font with the desired symbols
provides coverage; this does not add text shaping or color emoji support.

## In Plasma or another Wayland desktop

```sh
./bin/worldr-shell --backend=nested --project=. --terminal
./bin/worldr-shell --backend=nested --duration=20s --demo
./bin/worldr-shell --backend=nested --experience=axial --state=documents/axial.json
./bin/worldr-shell --backend=nested --app=foot
./bin/worldr-shell --backend=nested --app=foot --app=foot -- --config=/dev/null
./bin/worldr-shell --backend=nested --terminal --app=foot
```

Nested presentation uses a Vulkan swapchain on the host's surface. No per-frame
readback or shared-memory framebuffer transport is involved. The host supplies
logical XKB keys, modifiers, pointer input, resize, and close events.

The commands below use the general workspace unless `--experience=axial` is
specified. `--demo` selects AXIAL automatically unless an experience was explicitly
selected; it cannot be combined with `--experience=workspace`.

Drag a window’s top grip to move it; release while moving to throw it. The window
coasts smoothly to a stop. Grab again or press Escape to stop a coast. Scroll
while dragging to change depth; Ctrl+Z restores the whole gesture. Reduced Motion
keeps direct dragging and disables throws. Super+drag also moves content planes
when the host does not reserve that chord. Super+drag on empty workspace pans the
camera, and Super+wheel changes the hovered window's depth without focusing it.
Resize with the bottom-right grip or Super+secondary drag over window content;
the logical client size and saved spatial frame update together. The three
controls on a window's top grip hide it completely, toggle its Read view, or
request that its provider close it. The square control and Win/Super +
double-click use the same Read action; repeat the gesture there to return to the
workspace. Overview and portals retrieve minimized windows.

`--project=.` opens a native directory list and UTF-8 file preview. Use Up/Refresh,
arrows/Enter/Backspace, or double-click an entry. Copy Path or Ctrl+Shift+C copies
the selected path. The read-only preview is limited to 1 MiB per file and 5000
entries per directory; child symlinks and special files are never opened.

In AXIAL, drag to orbit and click a part or its tree entry to inspect it. E explodes the
assembly, Space pauses time, left/right scrub time, F focuses the model, and R
resets it. Ctrl+Z undoes an edit; Ctrl+Shift+Z or Ctrl+Y redoes it. A full drag is
one edit. Escape cancels an active gesture or restores the normal view.
Ctrl+Alt+Q or the host window close button exits; Ctrl+Q also exits when the
workspace owns the keyboard. Ordinary shortcuts reach a focused application.

Use the Cinematic/Adaptive header controls or P to switch presentation. Cinematic
keeps spatial framing and stronger wire/rim accents; Adaptive calms them during
focus (F) and restores them when leaving focus. The model has a blue/silver
palette, light-responsive highlights and smoothly shaded cylinder walls. Its
world-space grid and rings hide in Read/Overview and fade during Adaptive focus.
Both modes retain the same content and controls. Selective depth-aware background
glow, directional shadows, moving fill lights, opt-in final-frame highlight
bloom, thin-glass transmission/refraction and depth-composited holographic mesh
projections are implemented. Reduced Motion freezes the projection phase. The
compositor keeps the global finish neutral to preserve exact client/UI pixels
and cursor bounds. Output is still SDR sRGB; HDR signaling, ICC/wide-gamut
transforms and true volumetric scattering remain future work.

Press B or use the panel depth button to bring the live instrument panel forward
or send it behind the assembly. The panel has its own play/pause and timeline
controls linked to the same synthetic study data. Occluding geometry blocks
new clicks on the panel; an active scrub retains pointer capture. Front/back
placement participates in undo/redo and document persistence. This demonstrates
native content surfaces. Add `--app=foot` for an actual terminal process.

In AXIAL, `--app=foot` takes the instrument panel’s place; the default workspace
has no synthetic study panel. Click
the terminal to type, use **Read selected** for a readable view and **Return to space**
to restore its context. The depth control changes its placement; Compact/Wide
changes the client's configured size so terminal programs reflow. Selection
drags retain the target surface even outside the scene viewport. Clicking the
workspace returns keyboard ownership to its native controls. Ctrl+Alt+Q exits
worldr regardless of who owns the keyboard; Ctrl+Q and Ctrl+S reach the terminal
while it has focus. A foot process exiting withdraws its window and leaves other
applications and the native workspace running.

**Place / Group** enables dragging and Shift+click multi-selection. Grouped
windows retain their relative positions when moved or sent deeper into space.
**Overview** (Ctrl+Alt+O from any app; O with workspace focus) retrieves obscured
apps. Unmodified arrows select thumbnails one step per fresh press; modified
arrows are consumed. Enter/keypad Enter or Escape returns to the prior view
without restoring application keyboard focus. A second fresh Enter opens Read
and grants typing focus to the selected app, even if it was obscured in space.
Held overview keys and their releases cannot type into the returned application.
**Portals** in the desktop footer, or Ctrl+Alt+G from any app, opens all live
window groups across named spaces. Use arrows and Enter to travel, or use
Ctrl+Alt+Left/Right directly. Portal travel centers the camera and selection
without moving windows or granting application focus; zooming out leaves the
windows free of automatic application-name cards.
**New Terminal** or Ctrl+Alt+Enter creates an independent native shell
and selects it; click its content or press a fresh Enter to read and type. The shortcut works from a focused
native or legacy app, including with keypad Enter. This control is available even without `--terminal`
and when no applications remain. **Close Selected** requests closing only the
active window, even within a selected group; a legacy client can show an
unsaved-work dialog before closing. Neither process action is undoable.
Repeated `--app` options share trailing arguments; `--apps=profile.json` gives
each launch its own stable ID and argument list. See the README profile example.

`--terminal` starts a native PTY shell with worldr-owned presentation. Use
Ctrl+Shift+C/V for selected text and paste, and Shift to override a TUI's mouse
tracking for selection/scrollback. Native, legacy and desktop clipboards share
offers; data transfers happen on explicit paste. Native text transfer is bounded
to 1 MiB and two seconds, and focus loss cancels a pending native paste. Native
shell exit keeps the final output visible until Close Selected dismisses it.
Native glyphs use embedded Go Mono with bounded Fontconfig fallback and retained
RGBA surfaces. Text shaping, color emoji and soft-wrap-aware selection remain ahead.

The native shell points compatible GUI programs at worldr's private server.
Run these inside that shell after installing the relevant applications:

```sh
foot --config=/dev/null &
QT_QPA_PLATFORM=wayland konsole --separate --nofork &
chromium --ozone-platform=wayland --disable-gpu \
  --user-data-dir="$(mktemp -d -t worldr-chromium.XXXXXX)" --no-first-run about:blank &
```

The separate Chromium profile prevents an existing browser instance from taking
the launch. It is a temporary directory created for this command. The command
above deliberately requests software client rendering; supported GPU clients can
instead use the bounded explicit-modifier DMA-BUF path. Worldr renders its scene with Vulkan
in both cases. The private server is also available when a terminal was opened
through New Terminal instead of `--terminal`.

X11 support is opt-in and requires the `xorg-xwayland` package on Arch or
`xwayland` on Debian. Use `--x11-app=xmessage -- 'Hello from X11'` for an explicit
launch, `"x11": true` in an application profile, or `--xwayland --terminal` to
allow X11 programs launched from the native shell. The private Xwayland process
has its own authentication cookie. It uses glamor DMA-BUF buffers when the
Xwayland binary, Vulkan importer and accessible DRM render node share an
explicit XRGB/ARGB modifier. If those capabilities are absent, or glamor fails
during startup, Worldr starts it with software SHM instead. The X11 `CLIPBOARD`
selection is bridged lazily to Wayland, native and nested-host clipboard
endpoints. XDND versions 3–5 provide copy-only drags between managed X11
windows; cross-protocol X11/Wayland drags are not implemented.

At most 32 live windows and 32 saved placements are supported. Closed windows
retain their saved placements, so old layout entries can fill the document with
fewer live windows. Launch failures show a temporary notice below the header.
If a new terminal cannot fit the saved layout, it is closed immediately and the
prior layout is preserved. Reopening native launch slots reuses their placement;
it starts a fresh shell rather than restoring process memory.

Use **Forget Closed Placements** in the row below the header to free entries
belonging to closed windows. It appears only when such entries exist and works
even with no live apps. Live positions, groups and selection remain intact.
Ctrl+Z restores forgotten entries if the combined layout still fits; a capacity
conflict shows a notice instead of hiding a new live window. Redo leaves any
reopened window intact. Cleanup is always an explicit action.

Real foot workflows cover typing, resize and spatial management. Isolated
Chromium/Konsole tests cover content, menus, nested submenus and dialogs, and
Chromium verifies XDG shell v3 negotiation. A socket-level test covers explicit
and reactive popup repositioning through configure acknowledgement and parent
resize; popup pixels remain clipped to their root application image. Raw protocol
coverage also verifies clipped buffer damage, scaled surface damage, immutable
earlier frames and full-copy fallback for clients that omit damage. Opt-in
Xwayland is tested with real xmessage windows; nested native fields support
text-input-v3 IME.
Wayland drag-and-drop is tested between two Chromium processes. Explicit
single-plane XRGB/ARGB/XBGR/ABGR DMA-BUF buffers in 8888 and 2101010 layouts are
copied GPU-to-GPU into retained LINEAR snapshots when Vulkan supports the exact
format/modifier pair. This covers LINEAR and capability-gated non-LINEAR
modifiers with one modifier plane, up to 256 advertised pairs; unadvertised
pairs fall back in the client, normally to SHM. Multi-plane YUV, primary
selection, cross-protocol X11/Wayland drag-and-drop and arbitrary DMA-BUF
formats are not provided. A real Xwayland gate verifies both a nonempty glamor
render and forced SHM fallback. The XDND gate transfers a real UTF-8 selection
between two authenticated X11 clients and covers cancellation, target unmap,
legacy version completion and the two-second unfinished-drop bound.
To run the real foot and Vulkan integration tests after installing foot:

```sh
make test-compat
make test-integration
make test-nested
WORLDR_TEST_DMABUF=1 WORLDR_TEST_DMABUF_MODIFIERS=1 \
  go test -v ./internal/platform/linux/apps ./internal/platform/linux/native -run DMABuf
WORLDR_TEST_TOOLKITS=1 go test -v ./internal/platform/linux/apps \
  -run 'Test(ToolkitProbe|ChromiumPopupInputAndDismissal|ChromiumClientCursorChangesAndHides|KonsoleDialogAndPopupDisconnect)$'
WORLDR_TEST_SYSTEM_FONTS=1 go test ./internal/nativeapps -run TestSystemFontFallbackBoxDrawing
```

The second command requires Chromium and Konsole for all cases to run; missing
executables are skipped. It uses isolated profiles and local pages on a private
Wayland server, including popup, dialog and cursor cases beyond the first-frame probe.
The font check requires an installed font with rounded box-drawing glyphs.
`make test-compat` includes real native-shell launch, close/reopen and clipboard
tests as well as foot and Vulkan integration. Install Vim and Bash to exercise
the optional terminal editor and foreground-job tests. `make test-integration`
adds real media, Chromium/Konsole, Xwayland and DMA-BUF workflows. `make
test-nested` drives a private Wayland compositor, so its scripted input cannot
reach the host desktop.

With `--state`, an existing document loads and Ctrl+S or normal exit saves it.
A missing file starts fresh and is created on save. Camera, placement, selection,
presentation and Reduced Motion persist; AXIAL also saves its study timeline.
Workspace and AXIAL documents have distinct identities and cannot be interchanged.
Undo history does not persist. Saving cancels a held pointer gesture and settles
a released throw at its current position; velocity is never saved. No default document file is
written when the option is absent. Invalid/mismatched state files are rejected.

## Headless rendering and capture

```sh
make preview
./bin/worldr-shell --backend=headless --frames=181 --demo --snapshot=dist/study.png
./bin/worldr-shell --backend=nested --duration=10s --snapshot=dist/current.png
```

A Vulkan device is required. Software Vulkan such as Lavapipe can check rendering
correctness, but its timing is not hardware GPU performance. Export renders the
current document into a separate offscreen target and reads that image back;
it does not add readback to the interactive presentation loop.

Reported submit/wait time is observed on the CPU. It is not a GPU timestamp or
an input-to-photon measurement. Presentation teardown waits for device idleness
so in-flight resources are not destroyed; there is no finite teardown deadline.

## Direct display on a spare TTY

Save current work and switch to a spare TTY (for example Ctrl+Alt+F3), log in, and
run:

```sh
./scripts/try-tty.sh
# Or select the KMS primary node explicitly:
CARD=/dev/dri/card1 BACKEND=vk-display DURATION=15s ./scripts/try-tty.sh
# Inspect connectors without taking ownership, then select/order them explicitly:
./bin/worldr-shell --list-outputs
./bin/worldr-shell --backend=vk-display --output=508 --output=517
```

`vk-display` presents the scene through a Vulkan display swapchain.
`BACKEND=drm` is a diagnostic fallback that reads an offscreen Vulkan image back
into a DRM dumb buffer. The script refuses graphical-session variables. Return
to the existing desktop using its VT shortcut, commonly Ctrl+Alt+F1 or F2.

The redesigned physical-display/VT path is currently unverified. It needs real
hardware with suitable device permissions and supported Vulkan display extensions.
Passing offscreen or nested tests does not validate KMS, VT switching, display
restoration, suspend/resume or input permissions. Direct mode uses libseat for
DRM/input leases and libinput/udev for hotplug, relative and absolute devices.
It closes graphics and input before acknowledging a seat disable, recreates them
after enable, and cancels held keyboard/pointer state across the transition.
Connected DRM outputs form one bounded desktop (up to eight); `--output` fixes
their left-to-right order. A connector change rebuilds the output set while the
workspace and applications remain alive.

Direct keyboard mapping still assumes US XKB with application repeat defaults
of 25 Hz after 600 ms. It does not import an existing seat's held keys or lock
state. Ctrl+Alt+F1 through F12 asks libseat to switch sessions. Physical
multi-monitor, VT and suspend/resume behavior still requires a spare-TTY hardware
run; color management, direct-session IME and an AT-SPI desktop-bus adapter remain
unfinished. Login sessions expose native Files, media, terminal and model-control
semantics through a private `0600` JSON Unix socket for an external adapter.
GPU targets use 4×
MSAA where supported, with a feature-checked 1× fallback.

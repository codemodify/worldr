# worldr

A Linux computing environment inspired by the workspaces of **Avatar**, **Iron
Man**, **Minority Report**, **Moon**, and **Her**: directly manipulable objects,
live instruments, expressive motion, and a coherent place to work.

The visual direction is primarily cinematic, led by Iron Man and Avatar.
The intended workspace combines real terminals and browsers with native tools
in navigable space. [Product direction and next milestone](docs/WORKSPACE.md).

**0.10.0-dev is an architecture reset.** The core is a native scene system, a
Vulkan renderer, and a host that runs an experience through an explicit contract.
The former CPU window compositor and desktop shell are available in Git history.

The default experience is a **general spatial workspace** for native tools and
legacy applications. Open a native project browser, arrange terminals around it,
and drag or throw windows through the space. An empty workspace stays available
when its last application closes.

The vertical **APPS** rail on the right launches eight built-in tools: Files,
Terminal, Photo, Media, Model, Research, Notes, and AXIAL. Files, Terminal,
Notes, and AXIAL open directly. Photo, Media, Model, and Research focus Files in a matching
choose-file mode; folders remain navigable, Escape restores the full listing,
and opening a supported file hands it to the selected native viewer.

A centered, vertical cyan 3D DNA double helix turns slowly behind the workspace, completing one
rotation every 30 seconds. It stays behind application content and never captures
input. Reduced Motion freezes the DNA; Adaptive Read fades it out.

**AXIAL / 07** is a hosted native 3D application in the general workspace; use
its APPS-rail control or start with `--axial`. It can be moved, grouped, sent
through depth, saved and restored like the other tools. `--experience=axial`
retains the deterministic standalone presentation used by demonstrations. This interactive
turbine study lets you rotate an assembly, inspect components, animate an
exploded view, and scrub a timeline connected to the model and response chart.
Geometry is procedural and signals are synthetic; it demonstrates interaction,
not a physics solver. Its live instrument panel shares the model's space: bring
it forward or send it behind the assembly, and use its playback and scrub
controls wherever they remain visible. The model inspector separately hosts
file-backed 3D content alongside other apps.

## Build and run

Requires Go 1.22+, GCC, pkg-config, development libraries for Vulkan, libdrm,
Wayland client/server, xkbcommon, libvterm 0.3+, Fontconfig, Pango/Cairo, XCB (Composite, Res and XFixes), libseat, libinput, libudev, and libmpv 0.37+,
plus a Vulkan driver.
Debian/Ubuntu development package names include `libvulkan-dev`, `libdrm-dev`,
`libwayland-dev`, `libxkbcommon-dev`, `libvterm-dev`, `libfontconfig1-dev`, `libpango1.0-dev`, and
`libmpv-dev`, `libxcb1-dev`, `libxcb-composite0-dev`, `libxcb-res0-dev`, `libxcb-xfixes0-dev`,
`libseat-dev`, `libinput-dev`, and `libudev-dev`; Arch uses
`vulkan-headers`, `vulkan-icd-loader`, `libdrm`, `wayland`, `libxkbcommon`, `libvterm`,
`fontconfig`, `pango`, `libxcb`, `seatd`, `libinput`, `systemd-libs`, and `mpv`.

Generated shader binaries and Wayland protocol sources are checked in. Ordinary
builds need no shader compiler or protocol generator. The embedded Go font comes
from the Go module dependencies.

```sh
make build
make test
make install DESTDIR=/tmp/worldr-package     # stage binaries and desktop/session entries
make test-integration                    # real toolkits, media, Xwayland and DMA-BUF
make test-nested                         # private Wayland input/recovery workflow
./bin/worldr-shell                         # nested when WAYLAND_DISPLAY is set
./bin/worldr-shell --backend=nested --duration=30s
./bin/worldr-shell --backend=nested --app=foot
./bin/worldr-shell --backend=nested --terminal
./bin/worldr-shell --backend=nested --project=. --terminal
./bin/worldr-shell --backend=nested --research=examples/data/orbit-signals.csv
go build -o bin/worldr-native-instrument ./examples/native-instrument
./bin/worldr-shell --backend=nested --native-app=./bin/worldr-native-instrument
./bin/worldr-shell --backend=nested --experience=axial
```

`make install` installs `worldr-shell`, the supervising `worldr-session`
launcher, a display-manager entry under `share/wayland-sessions`, and a nested
launcher under `share/applications`. A display manager performs authentication;
`worldr-session` validates its private runtime directory, chooses direct Vulkan,
forwards shutdown signals, and never switches users.

Install `foot` to use the terminal compatibility path. It is a separate terminal
process, connected through a private Wayland socket and rendered as a surface
in the workspace. Click it to type, or press Enter from the workspace to read
and type in the selected app. **Read selected / Return to space** provides
a readable working view. Drag the visible grip above a window to move it, or
use **Super+primary drag** over its content when the host desktop permits that
shortcut. Drag the visible bottom-right grip to resize, or use
**Super+secondary drag** anywhere over the window. Both the spatial frame and
the client logical size follow the gesture, and the saved layout restores that
size after reconnecting. **Win/Super + double-click** toggles the window's Read
view. **Place / Group** also enables content dragging; Shift+click selects
several apps and **Group selected** makes them move together. Scroll during a
drag or use **Depth −/+** to move the selection through space;
**Super+wheel** moves the hovered window (and its explicit group) without
focusing it. **Super+primary drag** on empty workspace pans left, right, up or
down. **Overview**
retrieves hidden apps. **Size: Compact / Wide** resizes application content
independently of its spatial placement.

Native terminals, foot and Konsole use cyan frames with chamfered corners,
layered rails and an integrated top drag plate. The frame stays outside the
terminal content and follows its depth and placement.

Release a moving window to throw it: its motion decays exponentially, slows to a
crawl, and stops. Clicking or grabbing it again stops its glide; other windows
keep moving independently while you select, type in, drag or throw another one.
Escape from the workspace stops gliding windows. A grouped selection travels
together, and each drag plus its glide forms one undoable move. Escape during
a held drag cancels that gesture.
**Reduced Motion** disables throws while preserving direct dragging. Ordinary
content dragging and scrolling remain with the application outside Place mode
and the exact Super+primary shortcut.

`--project=.` opens native Files rooted at the current directory.
Select a file to preview UTF-8 text with line numbers; Enter, double-click or
**Open** enters a directory. Backspace or **Up** returns toward the chosen root.
Arrow keys select entries, Tab switches between list and preview, and the wheel
or Page Up/Down scrolls the active pane. Left/right arrows pan the preview.
**Refresh**, F5 or Ctrl+R reloads the directory; **Copy path** or Ctrl+Shift+C
copies the selected path. Listings and previews load on a worker so ordinary
filesystem reads do not occupy the workspace's frame loop.

Open a video with **Enter**, **Open**, or a double-click to play it in the native
media player. Playback starts in place without taking keyboard focus, changing
selection, or entering Read mode. Its cyan angular frame contains play/pause, stop, ten-second skips,
a seek timeline, mute and volume. **Space** toggles playback, **Left/Right** seek
ten seconds, and **M** toggles mute while the player is focused. Opening another
video opens another independent player (up to four); closing it releases the decoder,
audio output and file. MP4, MKV, WebM, MOV and other supported local containers
use libmpv's codecs. Selecting a file alone does not start playback.

The initial media path uses software decoding/scaling into retained RGBA
textures, with libmpv handling audio/video synchronization. Direct GPU decoder
imports, HDR color management, subtitles and playlists remain future work.
Worldr disables libmpv configuration and its standalone Lua/UI services; the
workspace owns all input, controls and presentation.
`WORLDR_TEST_MEDIA=1 go test ./internal/media ./internal/mediaapp ./internal/app`
enables real playback checks using FFmpeg-generated, silent test fixtures.

Open a photo the same way to place the image directly in the workspace, with a
slim cyan bracket on the left and short returns along the top and bottom. The
right side stays open; there is no title bar or image toolbar. Click and drag anywhere on the
photo to move or throw it. The complete image keeps its original aspect ratio;
there are no manual zoom, pan or rotation controls. JPEG, PNG, WebP, BMP and the
first frame of GIF files are supported, with JPEG EXIF orientation applied
automatically. Opening photos preserves your current focus and workspace view.
Loading runs on a worker; malformed or oversized images show an error while
preserving the previous photo.
The initial limits are 64 MiB encoded, 40 megapixels and 16,384 pixels per side.

CSV, TSV and explicitly named `.worldr-data.json` files open in the native
research workbench. Its observation table, 2D chart and pickable 3D points share
one selection; numeric X/Y/Z axes, chart mode, orbit and zoom remain independent
per dashboard. It watches the anchored source for changes and restores exact
dashboard slots and view state. The repeatable `--research=PATH` option opens a
dataset directly. Limits and accepted formats are documented in the
[research workbench guide](docs/RESEARCH.md).

Files named `*.worldr-note.md` open in the native note editor. It provides
multiline UTF-8 editing, grapheme-safe navigation and deletion, IME composition,
selection, undo/redo, and the shared clipboard. Ctrl+S replaces a file through
a synced temporary file and refuses to overwrite a copy changed by another
program. **New native note** in the launcher creates an untitled session buffer;
it has no implicit filesystem destination and is recoverable only when the
workspace itself is saved with `--state`. Up to eight 48 KiB notes retain their
exact text, selection, scroll position, file identity and dirty state. See the
[native notes guide](docs/NOTES.md).

**Terminal Here** or Ctrl+Shift+Enter in Files opens an independent native shell
in the selected directory. With a regular file selected, it uses the current
folder. The new terminal appears in the workspace while Files keeps its focus;
click the terminal to type. Directory startup uses the browser's opened folder,
so a rename cannot redirect the shell to an unrelated replacement path.

The browser displays up to 5,000 directory entries and the first 1 MiB of a text
file, with visible truncation notices. It lists symlinks without following them
below the chosen root, and does not preview special files. Ctrl+F filters the
folder, image rows receive bounded thumbnails, and a 2-second refresh notices
changes. New folder, Rename, Move, Duplicate, Trash/Restore and Undo provide
recoverable file operations. Explicit `.worldr-note.md` documents use the native
editor; general text/source editing and recursive content search remain ahead.
With `--state`, Files resumes its root, current folder and selected item. An
explicit different `--project` chooses a new root; repeating the saved root keeps
its saved navigation.

While an application owns the keyboard, its normal keys and Ctrl shortcuts go to
that application. Click workspace controls to return keyboard ownership to
worldr. **Ctrl+Alt+Q always exits worldr**. Application arguments follow `--`:

```sh
./bin/worldr-shell --backend=nested --app=foot -- --config=/dev/null
./bin/worldr-shell --backend=nested --app=foot --app=foot -- --config=/dev/null
make test-compat                          # requires foot and Vulkan
```

Repeat `--app` to launch several processes with the same trailing arguments.
For different applications and arguments, use `--apps=workspace-apps.json`:

```json
{
  "version": 1,
  "applications": [
    {"id": "terminal", "command": "foot", "args": ["--config=/dev/null"]},
    {"id": "console", "command": "konsole", "args": ["--separate", "--nofork"]}
  ]
}
```

Profile IDs keep layout associations stable when launch order changes. The
workspace supports 32 live windows and retains up to 32 saved window placements.
Closing a window preserves its placement. A document containing old placements
can therefore reach its limit with fewer live windows. Launch failures show a
temporary notice and preserve the existing layout; a new terminal that cannot
be placed is closed immediately.

Install `Xwayland` to enable X11 compatibility explicitly:

```sh
./bin/worldr-shell --backend=nested --x11-app=xmessage -- 'Hello from X11'
./bin/worldr-shell --backend=nested --xwayland --terminal
WORLDR_TEST_XWAYLAND=1 go test ./internal/platform/linux/xwayland ./internal/app
```

`--x11-app` is repeatable and shares trailing arguments with `--app`. A profile
entry can use `"x11": true` for its own arguments and stable launch ID. The
managed Xwayland uses a private authentication cookie and never reuses the host
desktop's display. It selects glamor DMA-BUF buffers only when Xwayland, the
Vulkan importer and an accessible DRM render node share an explicit XRGB/ARGB
modifier; startup or GBM failure retries once with software SHM. With
`--xwayland`, native shells receive both private display addresses. XRes
associates local X11 client PIDs with launch IDs. Focus, resize, Unicode titles,
transient metadata and WM_DELETE are bridged. The X11 `CLIPBOARD` selection
participates in the same lazy clipboard broker as Wayland, native and host
endpoints, with TARGETS negotiation and bounded transfers. XDND versions 3–5
route copy-only drags between exact managed X11 source and destination windows.
X11 windows remain separate workspace surfaces.

**Forget Closed Placements** appears below the header when closed windows have
saved positions. It explicitly frees those entries while preserving all live
windows, their groups and selection. Ctrl+Z restores forgotten entries when
capacity permits; undo never drops a newly opened window to make room. Redo
keeps any window that has reopened in the meantime.

`--terminal` opens a worldr-native terminal backed by a real PTY and libvterm.
It runs `$SHELL` (or `/bin/sh`) directly, with colors, alternate-screen programs,
scrollback, resizing, selection and Ctrl+Shift+C/V copy/paste. Combine it with
`--app=foot` or `--apps` to use native and legacy apps together. Shift overrides
a terminal program's mouse tracking for selection and history scrolling.
Compatible Wayland apps launched from the native shell connect back to this
workspace through its private server; the native terminal itself uses no
Wayland client. Exited shells keep their final output visible. The current
terminal rasterizes changed glyph rows into a retained surface; the GPU places
it in the workspace.
Go Mono supplies the base fonts. On Linux, missing glyphs use installed fonts
through Fontconfig, with bounded caches and unchanged terminal cell sizes.
Shared labels and editable fields use Pango/HarfBuzz shaping and nested
text-input-v3 IME. The terminal keeps a fixed cell grid; available glyph and
color-emoji coverage depends on installed fonts.

**New Terminal** or **Ctrl+Alt+Enter** opens an independent native shell, even
while an application has focus or worldr was started without `--terminal`.
It selects the new window and preserves Read mode. Click its content or press
a fresh Enter from the workspace to start typing. **Close Selected** requests closing only the active window,
including when several windows are grouped or selected. Legacy apps can show
their own unsaved-work confirmation before disappearing. Closing a naturally
exited native shell dismisses its retained output. Launch and close are process
actions and are not undoable.

In nested sessions, clipboard offers flow among native terminals, Wayland and
X11 legacy apps, and the host desktop. Contents move on an explicit paste or client request,
never just because an offer appears. Native terminals exchange text up to 1 MiB
with a two-second transfer deadline. Changing focus cancels an outstanding
native paste. Primary selection is not implemented. Wayland data drag-and-drop
keeps keyboard focus at the source, repicks spatial destinations, and transfers
accepted COPY/MOVE payloads; isolated Chromium pages exercise a real cross-client
drop. Managed X11 clients also support X11-to-X11 XDND copy: at most 64 MIME
types are retained, destinations must acknowledge acceptance, and unfinished
drops fail after two seconds. The payload remains a direct client-to-client
XdndSelection transfer. X11-to-Wayland drags and MOVE/LINK actions are not
implemented.

Legacy applications can supply transparent pointer cursors, including text,
resize and explicitly hidden cursors. Cursor choice follows pointer hover and
capture independently of keyboard focus; workspace controls retain worldr's arrow.

The ordinary shortcuts below apply when the workspace owns the keyboard.
Ctrl+Alt+G, Ctrl+Alt+Left/Right, Ctrl+Alt+O, Ctrl+Alt+Enter and Ctrl+Alt+Q remain
available while an application has focus. Portals map live window groups across
named spaces. Their jumps center the saved camera target without moving windows
or granting application focus. They stay in the explicit portal atlas and
reserved shortcuts instead of placing title cards over zoomed-out windows.
**Help** in the footer opens an on-screen guide; F1 does the same from the
workspace. Focused applications keep their own F1. Closing the guide returns
to workspace input; click an app or press Enter to resume typing.

| Action | Control |
| --- | --- |
| Open / close the workspace shortcut guide | Help in the footer, or F1 from the workspace; Escape closes |
| Pan the spatial workspace | Super+primary drag on empty workspace |
| Orbit the spatial scene | Drag the scene rotation pad in the bottom-right corner |
| Zoom the spatial scene | Scroll over the scene; application scrolling keeps its usual behavior |
| Open the spatial portal atlas | Portals in the footer, or Ctrl+Alt+G from the workspace or a focused app |
| Travel directly among window groups | Ctrl+Alt+Left / Ctrl+Alt+Right |
| Move / throw a window | Drag its top grip; Super+primary drag where the host permits; or drag content in Place mode |
| Change depth while dragging | Scroll with the drag held |
| Change a hovered window's depth | Super+wheel; grouped windows stay together |
| Resize a window | Drag its bottom-right grip, or Super+secondary drag over its content |
| Toggle a window's Read view | Win/Super + double-click its content |
| Minimize / Read / close a window | Use the three controls on its top grip; minimize hides all window chrome but remains recoverable through Overview and portals, while the square opens the same Read view as Win/Super + double-click |
| Stop a released window's glide | Grab again, or Escape from the workspace |
| Inspect an AXIAL component | Click it or its tree entry; keys 1–3 |
| Explode / assemble in AXIAL | E |
| Play / pause AXIAL | Space |
| Scrub AXIAL time | Drag the timeline; left / right arrows outside Overview |
| Bring AXIAL's instrument panel forward / send it back | B, or the panel depth button |
| Find all applications | Ctrl+Alt+O from any app, or Overview; O while workspace owns keyboard |
| Select an overview window | Unmodified arrow keys; one step per fresh press |
| Leave overview without changing placement or granting app focus | Enter / keypad Enter, or Escape |
| Read and type in the selected window | A fresh Enter / keypad Enter outside Overview; focused apps keep normal Enter behavior |
| Open an independent native shell | Ctrl+Alt+Enter, or New Terminal in the header |
| Request closing the active window | Close Selected |
| Remove saved positions belonging only to closed windows | Forget Closed Placements; Ctrl+Z to undo |
| Focus the object / read the selected app | F |
| Switch presentation mode | Cinematic / Adaptive controls in the header, or P |
| Reduce transitions, freeze background motion and disable throws | Full Motion / Reduced Motion in the header, or Shift+P |
| Reset workspace view / reset AXIAL study | R |
| Undo / redo | Ctrl+Z / Ctrl+Shift+Z or Ctrl+Y |
| Cancel an active gesture / reset the view | Escape |
| Save a configured document | Ctrl+S |
| Quit | Ctrl+Alt+Q; Ctrl+Q with workspace focus; or close the host window |

Presentation is selectable in both experiences. **Cinematic** keeps spatial
guides and brighter mesh accents visible. **Adaptive** keeps that framing while
exploring and calms it for Read mode; in AXIAL it also calms the model's cyan rim
accents during focus (F). Switching modes preserves content, placement, camera,
and available controls. Cinematic is the default;
these are the first presentation controls, with the richer concept-art finish
still ahead. The separate **Reduced Motion** preference completes explosion and
framing transitions immediately and disables window throws. It preserves study
playback, direct manipulation and application content; Space still pauses AXIAL.
The preference is saved, undoable, and retained when resetting the view or study.

AXIAL now uses a cool blue/silver palette, highlights that respond to the camera
and light, smooth shading on cylindrical walls, and a grid and rings anchored in
world space. Caps and blade edges retain sharp shading boundaries. The world-space
guides hide in Read and Overview and fade during Adaptive focus. Selective GPU
glow accents active window borders, guides and two housing light bands. It follows
scene occlusion and stays behind opaque app content, preserving terminal and
browser pixels. Adaptive focus removes the glow and retains a crisp focus border.
The material pass now combines the directional key with up to four moving fill
lights and supports depth-peeled thin-glass transmission. Lit translucent meshes
can explicitly sample the opaque scene through bounded refraction, with at most
an 18-pixel normal-directed bend and an optional fixed five-tap frosted
footprint of up to six pixels. The shared backdrop excludes screen overlays and
the host cursor. Zero refraction preserves the previous transmission-only result
and exact client/legacy texture pixels. Translucent native meshes can also use a
depth-composited holographic projection with world-space scan bands, an
opaque-depth contact cue and a Reduced Motion-aware phase; it cannot affect app
textures, shaped overlays or the host cursor. The renderer also has an opt-in
bounded final-frame highlight bloom and SDR
exposure/saturation/contrast finish. The mixed compositor workspace keeps that
finish neutral so legacy app pixels, opaque native UI and host cursors retain
their exact color and bounds. Physical HDR, ICC profiles and wide-gamut output
remain future work.

The ambient halo uses one radial-gradient fan, and translucent world-space
guides test depth without hiding the opaque content behind them. Short Intel
ARL before/after timings and exact commands are recorded in
[performance observations](docs/PERFORMANCE.md).

Persistence is opt-in:

```sh
./bin/worldr-shell --backend=nested --project=. --terminal --state=documents/workspace.json
./bin/worldr-shell --backend=nested --state=documents/workspace.json
./bin/worldr-shell --backend=nested --experience=axial --state=documents/axial.json
```

An existing document loads before the display opens. Missing primary and recovery
files start a new workspace or study; Ctrl+S and normal exit save to the chosen path. Saves atomically
replace a private file. Workspace state uses the `worldr.workspace` identity and
stores the camera, presentation and motion preferences, application positions,
depths, groups, selection, read/overview mode and size preferences. AXIAL state
uses `worldr.axial` and also stores study selection, time and instrument panel
depth. Keep separate files: one experience rejects the other's state.

The version-2 envelope adds a native session manifest; version-1 layout files
still load. It reopens Files at its current folder and selection, independently
placed photos/videos with playback settings, and model inspectors with their
view, measurements and notes. Named spaces retain their contents and cameras.
Native terminals reopen their working directories in stable slots with fresh
shells. Commands, running jobs, terminal scrollback and process memory are not
replayed. Legacy apps still require explicit `--app` arguments or an `--apps`
profile; reuse their launch order or profile IDs to recover their placements.

With `--state`, `--autosave=5s` is the default. A single background writer creates
`PATH.autosave` recovery checkpoints without interrupting dragging or gliding;
`--autosave=0` disables these writes. Startup prefers a newer valid recovery, or
a valid recovery when the primary file is missing or invalid. Invalid recovery
falls back to a valid primary with a notice. Ctrl+S and normal exit write the
primary and remove the recovery after any pending write finishes. `PATH.lock`
prevents two worldr instances from using the same state path concurrently.

`--fresh` loads the primary layout but skips recovery and saved native apps,
then starts only the apps requested on the command line. Missing saved resources
are reported and skipped; their references stay pending with their placements
for a later launch. Files falls back to its root if its saved subfolder is gone.
**Forget Closed Placements** also discards pending references for forgotten keys.

Undo history and animation interpolation are not persisted. Manual saving cancels an
unfinished held drag and stops a released window's glide at its current position
before writing. Throw velocity is never saved or replayed. Without `--state`,
worldr does not create document, recovery or lock files.

Older documents without a presentation field load as Cinematic. Resetting the
study preserves the chosen mode; undo/redo can restore a previous mode selection.

Render a reproducible preview without opening a window:

```sh
make preview                              # dist/worldr-preview.png
./bin/worldr-shell --backend=headless --demo --frames=181 --snapshot=dist/exploded.png
make test-gpu                             # requires hardware or software Vulkan
```

Headless mode still requires Vulkan. `--demo` selects AXIAL automatically unless
`--experience` is explicitly supplied; `--demo --experience=workspace` is rejected.
The demo uses a fixed clock and the same semantic actions as interaction.
`--snapshot` exports the current scene through
an offscreen Vulkan render; interactive presentation does not need readback.
`--backend=wayland-client` remains an alias for `nested`.

## Current scope

- Immutable meshes are uploaded once and retained by the renderer. GPU shaders
  perform model/camera transforms, directional lighting, material highlights,
  view-dependent rim accents, and wire rendering. Unlit meshes and application
  content keep their existing rendering.
- Ordered 2D overlay and 3D camera passes combine typography, controls, and models.
- Supported devices use 4× MSAA with stable per-sample overlay coverage; devices
  missing the required features use 1×. Content interiors retain sharp sampling.
- Hierarchical scene nodes support selection through ray/BVH intersection.
- Opaque RGBA content surfaces share depth with native meshes. Pointer rays map
  visible surface hits into image coordinates, including transformed hierarchies.
  Retained images upload changed regions; new render sessions recover full content.
- A host/experience boundary separates platform input, display, and file ownership
  from domain actions, typed documents, and undoable interaction.
- Nested presentation uses a Vulkan swapchain on the host Wayland surface, with
  no per-frame CPU image readback or shared-memory framebuffer transport.
- Direct Vulkan display and a diagnostic DRM readback backend remain available.
- A separate Wayland compatibility host supplies SHM application images and a
  bounded explicit-modifier 8888/2101010 XRGB/ARGB/XBGR/ABGR DMA-BUF path, raw keyboard input, pointer capture,
  scrolling, clipboard exchange and data drag-and-drop.
  Real foot workflows test independent typing, resizing, grouped placement,
  overview retrieval, persistence, and disconnect handling.

This remains a developing workspace, not a complete desktop. A bounded public
native application SDK v1 is available for independently built tools; see
[native development](docs/NATIVE-DEVELOPMENT.md) and the
[reference instrument](examples/native-instrument/README.md). It provides
process lifecycle, retained RGBA/mesh resources, input, IME and semantic trees;
v1 does not provide sandboxing, direct GPU handles or general host file APIs.
Isolated Chromium and Konsole tests exercise real content, input, menus, nested
submenus, dismissal, data drag-and-drop and separate dialog windows over the
private Wayland host. Chromium tests use an isolated profile and verify XDG
shell v3 negotiation. Explicit and reactive popup repositioning is covered by
an isolated socket-level protocol test, including configure acknowledgement and
parent resize. Popup content is constrained or clipped to its parent
application's image. General desktop compatibility is not established. Opt-in
Xwayland has real-client rendering,
input, resize, lifecycle, authentication and focus-isolation tests. Shared native
fields support text-input-v3 IME in nested sessions. Supported single-plane
LINEAR and capability-gated non-LINEAR 8888/2101010 XRGB/ARGB/XBGR/ABGR
DMA-BUF clients take a retained LINEAR GPU snapshot without CPU image readback; other
buffers use the protected SHM path.
Explicit spatial transparency uses bounded depth peeling; linear SDR output
and directional shadows are implemented. Shaped native fields and a bounded,
private JSON accessibility-semantic stream are available for external adapters.
An AT-SPI desktop-bus adapter, HDR output and physical multi-monitor/VT/suspend
qualification remain unfinished. Direct mode now uses libseat/libinput, releases devices before
acknowledging a seat disable, rescans input and DRM connectors, and composes up to
eight outputs into one desktop. Physical direct-display operation of this runtime
is still unverified. GPU presentation teardown waits for device idleness; it
has no guaranteed finite deadline.

Run details: [docs/RUN-ABOX.md](docs/RUN-ABOX.md). Design and remaining work:
[ARCHITECTURE.md](ARCHITECTURE.md). Film research:
[docs/VISION.md](docs/VISION.md). History: [CHANGELOG.md](CHANGELOG.md).
Building native tools: [docs/NATIVE-DEVELOPMENT.md](docs/NATIVE-DEVELOPMENT.md).
Accessibility adapter contract: [docs/ACCESSIBILITY.md](docs/ACCESSIBILITY.md).
Current implementation selection: [docs/BACKLOG.md](docs/BACKLOG.md).

## License

[The Free License](LICENSE). Dependencies retain their respective licenses.

## Spaces, native inspection and measurement

**Ctrl+Alt+Space** opens the searchable tools/spaces menu. Create or rename named
spaces, transfer windows/groups, reopen Files or find a window across spaces.
Sixteen spaces retain independent cameras and layouts; hidden tools keep running.

```sh
./bin/worldr-shell --backend=nested --project=examples/models --model=examples/models/mount.obj --terminal --state=documents/engineering.json
```

The native inspector opens triangle OBJ/STL and `.worldr-model.json`. Select
components, drag to orbit, scroll to zoom, measure between picked surface points,
add annotations and save a separate tool document. It shares workspace depth
and picking with every other window. Photos now open in up to eight independent
bracketed viewers; their total retained source budget is 64 MP.

The native terminal adds Find (Ctrl+Shift+F), bookmarks (Ctrl+Shift+M/B), command
blocks (Ctrl+Shift+K), pins (Ctrl+Shift+P/R), and reusable Tasks
(Ctrl+Shift+T). Save a command from Runs with `T`; `S` stages it without Enter,
while Ctrl+Enter explicitly executes it. Recipes restore as inert definitions
and never replay on startup. Optional shell integration adds command boundaries
and live task status without changing ordinary PTY programs. See
[native terminal task workflows](docs/TERMINAL-WORKFLOWS.md).

`--metrics=dist/performance.json` records complete-run timing histograms, sampled
Go heap, process peak RSS and Vulkan allocation counters. `--gpu-memory-mib=1024`
sets the GPU allocation budget. Measurements distinguish CPU render-return time
from physical input-to-photon latency. See [performance notes](docs/PERFORMANCE.md)
and the [rendering contract](docs/RENDERING.md).

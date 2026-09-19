# Changelog

## 0.10.0-dev — native scene architecture reset

- Raised the private application server to XDG shell v3 and implemented copied
  explicit popup positioners, parent-size constraints, parent-configure
  metadata acceptance and reactive
  reconstraining after committed ancestor resizes. Repositioned geometry takes
  effect only after its matching configure acknowledgement; an isolated wire
  test covers event order, positioner lifetime, unchanged-placement suppression
  and parent shrink, while the gated Chromium workflow verifies v3 negotiation.
- Added commit-atomic incremental SHM damage. Buffer-space rectangles are
  clipped, surface-space damage follows the newly committed integer scale, and
  each partial update produces a fresh immutable snapshot without reading
  unrelated client pixels. Viewport-mapped surface damage and incompatible
  attachments keep the conservative full-copy path.
- Isolated multi-output device recovery by carrying the failed connector ID
  through renderer errors and rebuilding only that output's Vulkan session.
  Healthy outputs retain their device resources; failures without connector
  context still use the conservative all-output recovery path.
- Bounded display-manager shutdown around the shell process group, pins the
  unreaped leader until straggling helpers are killed, and lets a second signal
  bypass the grace interval. A kernel parent-death kill, backed by a pinned
  creating thread, prevents an unexpectedly lost supervisor from leaving the
  shell as an orphaned display owner.
- Bridged the private Xwayland `CLIPBOARD` selection into the shared lazy
  clipboard broker. XFixes ownership tracking, TARGETS negotiation, stale-offer
  rejection and bounded descriptor-backed transfers work in both X11-to-Wayland
  and Wayland-to-X11 directions; an isolated wire-level Xwayland test covers the
  protocol without relying on the bridge's cached state.
- Added capability-gated Xwayland glamor. Linux-DMABUF v4 advertises a sealed
  exact-pair format table plus the renderer's main and tranche DRM device;
  Xwayland starts glamor only for a common explicit XRGB/ARGB modifier on that
  accessible device and retries once with SHM when startup disables acceleration.
  A real xmessage test verifies a nonempty GPU-backed render and forced fallback.
- Added copy-only XDND versions 3–5 between exact managed X11 windows. MIME
  discovery is bounded to 64 entries, target acceptance gates the drop, payloads
  use the standard client-to-client XdndSelection path, and unfinished drops
  fail after two seconds. Real two-client coverage verifies UTF-8 payload,
  legacy completion, cancellation, owner/target lifetime and unmap cleanup.

- Replaced desktop-wide empty-space orbiting with a dedicated bottom-right
  scene rotation pad. The compact gridded reticle shows live yaw and pitch,
  ordinary background drags leave the workspace still, and AXIAL retains its
  direct model-orbit gesture.
- Gave generic, cinematic and open photo-bracket window frames real rearward
  side depth while keeping all geometry outside client pixels and out of input
  picking. Window momentum continues independently during camera rotation.
- Removed the zoom-dependent L-shaped application title cards and their pointer
  targets from the workspace. Portal navigation remains available through the
  footer atlas and reserved shortcuts; native app-authored spatial annotations
  retain their prior behavior.
- Changed managed window chrome so Minimize removes the window and all of its
  chrome from Space while keeping it recoverable through Overview and portals.
  The square control now performs the same target-aware Read transition as
  Win/Super+double-click, and the duplicate header Read button is gone.

- Added the public `sdk/nativeapp/v1` process contract, a nonblocking host
  adapter, repeatable `--native-app` launch option and an external reference
  instrument. Version 1 has validated retained texture/mesh lifetimes, bounded
  surface and memory budgets, normalized input/IME events and semantic trees.
  Repeated app manifests receive distinct stable namespaces, semantic roles
  survive accessibility export, and surface-independent seat broadcasts are
  restricted to keyboard metadata so pointer traffic cannot terminate an app.
- Added a linked native research workbench for bounded CSV, TSV and explicit
  `.worldr-data.json` datasets. Tables, 2D plots and pickable 3D observations
  share selection and retain axes, mode, orbit, zoom and live file refresh.
- Added a native `.worldr-note.md` editor with grapheme-safe multiline input,
  IME, clipboard, undo/redo, semantics, exact dirty-buffer recovery, durable
  atomic saves and external-change detection.
- Added the portal atlas and direct group travel across named spaces. Camera
  targets persist, travel is undoable and navigation does not grant application
  keyboard focus.
- Added a terminal Tasks deck for bounded command recipes captured from OSC 133
  blocks. Recipes can be inspected, staged and explicitly run; restoration
  retains definitions without sending anything to the PTY.
- Expanded the restart/resume trial to Files, PTY, inert tasks, model, research,
  hosted AXIAL, two notes and the public SDK instrument. The default
  three-process, one-hour
  run completed 215,965 frames with stable resource peaks, zero graphics
  recoveries and all state/artifact checks passing before AXIAL joined the
  workload; the current expanded path passed its two-cycle CI gate.

- Added a bounded cinematic rendering finish: up to four view-local point fill
  lights, depth-peeled thin-glass transmission, opt-in opaque-backdrop refraction
  with an 18-pixel maximum bend and a fixed five-tap frosted footprint of up to
  six pixels, fixed-cost final-frame highlight bloom, and an optional
  filmic/exposure/saturation/contrast SDR output transform. Refraction applies
  only beneath explicit lit native glass, excludes overlays and the host cursor,
  and uses one shared color target included in the transparency budget. Zero
  refraction preserves the prior transmission path and exact client/legacy
  texture pixels. The zero output transform preserves prior linear sRGB output;
  HDR signaling, ICC profiles and wide-gamut scanout are not claimed.
- Added an opt-in native hologram material for translucent meshes. It combines
  true depth-peeling/opaque occlusion with view-dependent coverage, world-space
  scan bands and a narrow opaque-depth contact cue. The workspace freezes its
  transient scan phase under Reduced Motion, while application pixels, overlays
  and the host cursor remain outside the material shader. This is a bounded
  surface treatment; it does not claim volumetric scattering or physical HDR.
- Added hosted retained 3D objects, mesh picking and labels, plus file-backed
  OBJ/STL inspection with component selection, measurements, annotations and
  atomic tool documents. Multiple inspectors coexist with ordinary windows.
- Moved AXIAL / 07 onto that hosted spatial-application path with APPS-rail
  launch, workspace placement and exact inert session restore while retaining
  the standalone deterministic harness. Closing the hosted study now retires
  its panel and all four procedural meshes; reopening uses fresh resource IDs.
- Added named spaces with independent cameras/contents and a searchable
  Ctrl+Alt+Space launcher, group transfer and Undo.
- Added shared Pango/HarfBuzz native controls, grapheme editing, semantic focus
  trees and nested text-input-v3 composition with stale-focus protection.
- Added linear SDR color, directional shadows and bounded per-pixel transparency,
  validated with Vulkan synchronization checks. Added allocation accounting,
  resource budgets, device recovery hooks and transactional target updates.
- Added terminal Find/bookmarks, optional OSC 133 command blocks/folds and pins;
  Files search, thumbnails, periodic refresh and recoverable operations; and
  independent photo/video collections with exact saved slots.
- Added bounded complete-run performance reports and recovery retry limits.
- Added transactional synchronized Wayland subsurfaces, fractional scaling,
  viewports, clipboard and drag-and-drop. A bounded one-plane explicit-modifier
  8888/2101010 XRGB/ARGB/XBGR/ABGR DMA-BUF path snapshots compatible client
  buffers directly on the GPU and survives renderer recovery. LINEAR and
  device-specific non-LINEAR pairs are advertised only after exact per-device
  import, transfer, LINEAR export/import and sampling checks; retained snapshots
  stay LINEAR for recovery and cross-renderer ownership.
- Added a bounded native accessibility export: Files, media, terminal and model
  semantics are mapped to public window IDs and streamed as versioned JSON on a
  private Unix socket. An AT-SPI desktop-bus adapter remains separate work.
- Added a supervised `worldr-session`, display-manager and nested desktop entries,
  plus staged `make install`. The wrapper validates the runtime directory,
  authorizes direct display explicitly and forwards session shutdown signals.
- Added a private Xwayland bridge with authenticated startup, focus, resize,
  keyboard/pointer input, titles and close/unmap/remap lifecycle. Real foot,
  Chromium, Konsole and xmessage acceptance tests cover the supported subset.
- Added libseat-owned direct sessions and libinput/udev device discovery,
  ordered release/disable acknowledgement/reopen, VT switching, connector and
  input hotplug, plus an extended desktop across up to eight selected outputs.
- Added an isolated nested input/recovery test and a 60-second continuously
  playing mixed-workspace benchmark. Physical spare-TTY, multi-monitor and
  suspend/resume qualification remains outstanding.

- Added version-2 native session manifests while preserving version-1 layout
  loading. Files resumes its folder/selection, photos reopen, videos restore
  playback settings, and native terminal slots start fresh shells in saved
  working directories. Commands, jobs and process memory are not replayed;
  legacy apps still require explicit launch arguments or profiles. Missing
  resources show notices and retain pending references with their placements.
- Added `--autosave=5s` recovery checkpoints with one background writer and
  non-disruptive workspace snapshots; `--autosave=0` disables periodic writes.
  Startup selects a newer valid `.autosave`, with fallback between recovery and
  primary after validation. Manual saves flush checkpoints and clear recovery.
  `--fresh` loads the primary layout and explicit CLI apps while skipping saved
  native content/recovery. A `.lock` prevents concurrent use of one state path.

- Applied the cinematic cyan frame and matching drag grip to Files. Read-mode
  camera framing includes its outer rails instead of showing the plain outline.

- Added Files → Terminal Here and Ctrl+Shift+Enter. A selected directory starts
  an independent native PTY there; selecting a regular file uses its current
  folder. Startup borrows an anchored directory descriptor, preserving directory
  identity across renames without changing other shells or the parent process.
  Opening leaves Files, selection and camera focus in place.
- Unified provider polling, reverse-order shutdown and texture retirement in
  the application hub. Native and compatibility apps share optional lifecycle
  contracts, including cleanup after partial startup and idempotent teardown.

- Added an open cyan photo bracket along the left side with partial top and
  bottom returns. The frame stays outside the image and follows spatial
  placement; the viewer keeps direct photo dragging and has no image toolbar.

- Removed the armor and sparking-wire backdrop; the rotating DNA remains.
- Simplified photos to image-first surfaces: no enclosing window, title bar or
  zoom/pan/rotation controls. An open cyan bracket marks the left edge with
  partial top and bottom returns. Photos preserve their full aspect ratio and
  can be moved or thrown by dragging the image itself. Decoding, automatic EXIF
  orientation, safe replacement and opening without stealing focus remain.

- Restyled the native video player with a layered teal chassis, cyan rails,
  raised title tabs, segmented transport keys, a separate volume deck and a
  hexagonal timeline thumb. Paused video has a concentric-ring play overlay;
  the surrounding hex grid stays outside the picture. Controls remain usable
  across compact, tall and wide window sizes, without changing playback focus.

- Videos opened from Files now start in place without selecting the player,
  taking keyboard focus or entering Read mode. Reopening preserves placement.
- Files opens JPEG, PNG, WebP, BMP and GIF first frames through its anchored
  descriptor path. Bounded asynchronous decoding applies JPEG orientation,
  preserves the prior photo on failure, cancels stale loads and leaves workspace
  focus unchanged.

- Added a native media player with angular cyan chrome, a seek timeline,
  pause/replay, stop, ten-second skipping, mute and volume. Opening a video in
  Files (Enter, Open, or double-click) plays it in a native workspace surface.
  libmpv owns decoding and audio/video timing; RGBA frames use the existing
  retained texture path. Reopening replaces playback in the same placement.
  File opening remains asynchronous and anchored to the browser root; only
  explicit opens transfer a regular-file descriptor to the player. Embedded
  playback disables libmpv's configuration, user scripts and built-in Lua/UI
  services so those workers cannot capture input or outlive the native player.

- Fixed window throws stopping when another window was clicked. Windows and
  groups now coast independently; clicking their own content or grip stops them.
  Each throw keeps one Undo entry in gesture order, without rewinding unrelated
  movement or selection. Focused terminal input remains routed to the terminal.
- Sped up the DNA background rotation to one revolution every 30 seconds.

- Added a slowly rotating cyan DNA double helix as the spatial desktop background.
  Retained smooth geometry renders in a separate earlier camera pass, keeping
  application pixels and input untouched at every window depth. Reduced Motion
  freezes its pose; Adaptive Read fades it out.

- Added terminal-specific cyan frames with chamfered corners, paired rails, edge
  markings and an integrated drag plate, plus matching native terminal chrome.
  Decorations use retained scene geometry outside the full client rectangle.
  Read framing includes the border; dragging, throwing and focus cues remain.

- Made the general spatial workspace the default; AXIAL remains selectable with
  `--experience=axial`, and `--demo` selects it automatically. The desktop owns
  no study geometry/instrument and has its own validated saved-state identity.
- Added a read-only native project browser through `--project=PATH`, with directory
  navigation, bounded UTF-8 previews, scrolling, resize and Copy Path. Background
  reads are cancellable and stale results cannot replace a newer selection.
- Added occlusion-aware window grips and Super+drag, plus inertial throws that
  coast to a stop. Groups retain their arrangement; a grab or Escape stops motion.
  A complete drag and coast share one undo record. Reduced Motion disables
  throws, and persistence records only the settled position.

- Added explicit per-mesh GPU background glow, depth-occluded and clipped to each
  camera viewport. Active app borders, sparse guides and housing light bands
  author emission; opaque app content and later overlays retain their normal
  rendering. Adaptive focus removes halos while keeping its crisp focus border.
  Effect passes skip zero-emission frames and use bounded retained resources.
  HDR bloom, tone mapping and light transport remain future work.
- Added an on-screen shortcut guide through Help or workspace F1. It blocks
  underlying input, consumes held keys through dismissal and leaves typing focus
  with the workspace. Focused applications retain their own F1 behavior.
- Added transparent legacy application cursors with hotspot, integer scale,
  explicit hiding, focus/serial validation and bounded lifetime-managed images.
  A retained premultiplied image overlay pass handles cursor alpha without changing
  opaque scene content. Tests include real foot/Chromium requests and a foot-to-GPU
  workspace workflow; hovering and keyboard focus remain independent.
- Added independent, persisted Reduced Motion through the header or Shift+P.
  Explosion and framing transitions resolve immediately; explicit playback and
  application content remain under their own controls. Undo preserves unrelated
  playback, reset preserves the preference, and older documents keep full motion.
- Added bounded Fontconfig fallback for missing native terminal glyphs on Linux,
  retaining embedded Go Mono for supported characters and fixed cell metrics.
  Font caches and file reads have explicit limits; fonts/faces are released with
  their renderer. Installed coverage is required; shaping/color emoji are still ahead.
- Replaced collapsed direct-display input with an ordered evdev queue preserving
  raw keys, event-time modifiers/positions, extra buttons and both wheel axes.
  High-resolution scrolling avoids duplicate detents; lost reports or device
  disconnects cancel focus/repeat and pointer capture. Synthetic packet/pipe and
  app-routing tests cover the path; physical VT/seat operation remains unverified.
- Replaced ten overlapping ambient-halo discs with one radial-gradient fan,
  retaining 4× MSAA and interactive presentation without readback. Added
  depth-read-only guide meshes, deferred after opaque content so faint lines
  no longer cut holes through it. Intel ARL short-run timings, exact commands
  and measurement limits are in [performance observations](docs/PERFORMANCE.md).
- Added per-instance direct-light material highlights, roughness, metal tint and
  view-dependent colored rims using the existing renderer pass. Zero materials,
  unlit geometry and opaque application content preserve their previous behavior.
- Added validated per-vertex normals, smooth AXIAL cylinder walls with hard caps
  and blades, a cool blue/silver palette, and procedural world-space grid/rings.
  World-space guides hide in Read/Overview; Adaptive focus calms guides and rim
  accents. HDR bloom and color-managed glass remain future work.
- Added 4× MSAA with per-sample overlay coverage and a feature-checked 1×
  fallback. Resolve writes directly to the existing output target; retained
  textures survive resize and terminal content interiors stay sharply sampled.
- Added New Terminal and Close Selected controls, plus Ctrl+Alt+Enter for a new
  native terminal and Ctrl+Alt+O for overview while applications own the keyboard.
  Both chords consume repeats and their matching release. Native shells can launch compatible GUI
  apps back into worldr's private application host. Escape restores the prior
  space/read view without granting application keyboard focus.
- Added a native terminal manager with independent PTYs, reusable layout slots,
  distinct runtime identities and retained output after shell exit. New Terminal
  remains available without `--terminal` or any existing apps. Close Selected
  targets only the active window and lets legacy clients confirm unsaved work.
- Added arrow navigation of live overview thumbnails using their rendered grid,
  with one move per fresh press and a centered single-window layout. Enter or
  keypad Enter returns without typing focus; another fresh Enter explicitly
  opens Read and focuses the selected app. Held commands and their releases
  remain consumed across view/focus changes; ordinary focused-app Enter is intact.
- Added transient launch-error notices and visible capacity feedback. The live
  workspace and saved layout each have a 32-window limit; a newly launched
  terminal excluded by old saved placements is closed without changing those
  placements. Launch/close and notices remain outside document undo.
- Added explicit Forget Closed Placements cleanup, preserving live positions,
  groups and selection even for currently unrenderable provider surfaces. Undo
  retains subsequently registered keys and refuses capacity conflicts without
  partial changes; redo protects reopened windows. The control remains available
  in an empty workspace and does not overlap launch/error notices.
- Added multiple application surfaces, free placement, depth controls, grouping,
  overview retrieval, and persisted stable layout keys. Repeated `--app` launches
  share arguments; `--apps` profiles provide independent arguments and named IDs.
- Added lazy clipboard exchange between the nested desktop and application
  providers, with echo suppression and stale-offer checks. Native text paste is
  bounded to 1 MiB/two seconds and tied to the original terminal's focus; a later
  focus change or reopened launch slot cannot receive that pending paste.
- Added XDG popup composition and input routing. Real Chromium and Konsole tests
  cover dropdowns, context menus, nested menus, dialog windows and parent cleanup.
  These use private SHM Wayland clients, with a separate profile and software
  rendering for Chromium. Popup pixels remain constrained or clipped to the
  root application image; this is not general desktop compatibility.
- Added `--terminal`, a worldr-native PTY/libvterm terminal with monospace cell
  presentation, colors, alternate screens, history, resize, selection, copy/paste
  and focus-scoped key repeat. It runs alongside legacy applications. Native
  glyph rows currently rasterize on the CPU into retained GPU content surfaces.
- Added an opt-in `--app=foot` compatibility path on a private Wayland server.
  A real terminal shares native scene depth, receives raw input, and supports
  spatial front/back placement, read/return, client resize, and saved view
  preferences. Ctrl+Alt+Q exits worldr while application keys stay with the client.
  This established the initial SHM path later extended by toolkit, DMA-BUF and
  private Xwayland support above.
- Added retained RGBA content surfaces sharing the native mesh depth buffer,
  changed-region uploads, and ray-to-texture pointer mapping. Surfaces are
  opaque in this milestone. AXIAL's live instrument panel has working playback
  and scrub controls plus undoable, persisted front/back placement (B).
- Added selectable Cinematic/Adaptive presentation through header controls or P.
  Adaptive fades scene guides and mesh accents during focus. The preference is
  saved with the document, supports undo/redo, and survives study reset. Existing
  documents default to Cinematic.
- Replaced the CPU window compositor and shell with a native scene/experience
  runtime; AXIAL / 07 is its first procedural engineering study.
- Retained immutable mesh resources on the GPU. Model/camera transforms,
  directional lighting, and barycentric wire rendering now run in shaders.
  Ordered overlay and camera passes combine text, controls, and 3D content.
- Added hierarchical transforms and ray/BVH picking without projecting or
  shading every mesh triangle on the CPU each frame.
- Replaced nested CPU readback transport with libwayland/XKB host integration
  and a Vulkan WSI swapchain. Normal nested presentation has no CPU framebuffer.
  Generated XDG-shell protocol source is checked in.
- Separated host display/input/file ownership from the experience contract.
  AXIAL keys, UI, gestures, and demo invoke semantic actions. Typed versioned
  documents use stable component IDs; undo/redo groups gestures into one edit
  and preserves unrelated playback progress.
- Added opt-in `--state PATH` loading and atomic private-file saves with Ctrl+S
  or normal exit. Invalid envelopes/documents are rejected transactionally.
  Undo history and presentation interpolation are not serialized.
- Snapshot export uses a separate offscreen Vulkan render. Direct display and
  the diagnostic DRM readback backend remain, with physical TTY validation still
  outstanding for the redesigned runtime.
- Build dependencies now include Wayland client/server, Fontconfig, xkbcommon and libvterm
  0.3+ development libraries. Removed the former application compositor, window
  actor engine, shell/session wrapper, and unused transport packages.
- Compatibility currently targets foot, Chromium, Konsole and private Xwayland
  windows. HDR color management, arbitrary DMA-BUF formats, an AT-SPI adapter
  and full desktop services remain work ahead. Vulkan presentation
  teardown waits for device idleness without a guaranteed finite shutdown deadline.

The entries below describe the retired 0.9 compositor prototype.

## 0.9.36-dev — mesh wobble + burn dissolve

- `--effects=high`: title-drag uses a real 8×6 deformable mesh (springs to rest + neighbors; grab vertex pins to the pointer). The cheap spring offset is gone. Mesh + pack buffers are reused (no per-frame alloc storm).
- Close / unmap on high is a Compiz-style burn (hash-front dissolve + ember band + rising sparks, 720ms). Click the SSD × or quit the client (`Scene.Remove`). `xdg_toplevel.close` is sent so the client exits; burn starts immediately.
- `--effects=low` stays fade-only; `off` is still instant. No IME. No plugin graph.

## 0.9.35-dev — nest clipboard / scale edges

- Host `wl_data_device.selection(null)` now clears the nest clipboard (and primary). Worldr `set_selection(null)` clears the Plasma offer. Host-originated clear does not echo back.
- Host `set_selection` waits for a seat serial (pointer/keyboard enter or button). KWin rejects serial 0; the offer is retried on the next serial.
- Bind every host `wl_output`. Nest `wl_surface.enter` picks that output’s integer scale when `preferred_scale` is absent (frac still wins). `OnHostScale` emits the already-known scale so a late listener is not stuck at 1.0.
- Extra text aliases (`STRING`, `text/plain;charset=utf8`). No IME. No greetd/PAM.

## 0.9.34-dev — session / login wrapper

- `worldr-session` starts a Wayland session around `worldr-shell`: `XDG_RUNTIME_DIR`, `XDG_SESSION_TYPE=wayland`, `XDG_CURRENT_DESKTOP` / `XDG_SESSION_DESKTOP` / `DESKTOP_SESSION` (default `worldr`). Forwards SIGINT/SIGTERM. Remaining args go to the shell.
- `--login` prompts for a username; `--user NAME` autologin. Both must match the current uid (no PAM / no user switch). `--print-env` / `--dry-run` for checks. `--shell` overrides the sibling/PATH lookup.
- Display managers: `contrib/wayland-sessions/worldr.desktop` (`Exec=worldr-session`). `scripts/try-tty.sh` now launches via `worldr-session`. Isolation / greetd / PAM still later. No IME.

## 0.9.33-dev — multi-monitor / per-output scale

- `--outputs=N` (1–4) tiles N logical `wl_output`s left-to-right across the present framebuffer. Each has geometry, `name` (`WL-1`…), `scale`, and `wp_fractional_scale` from that output.
- `--output-scales=1,1.5` sets per-output scale. Empty: `--scale` / nest host applies to every output. Host scale updates all unless `--scale` or `--output-scales` is set.
- New windows map on the output under the cursor (`PlaceNewIn`). Dragging a window across a seam sends `wl_surface.leave` / `enter` and a new `preferred_scale`.
- Desktop draws a thin divider on interior seams. One physical FB still (no real DRM connectors). No IME.

## 0.9.32-dev — richer Compiz theater (wobbly / cube / expose)

- `--effects=high`: moving a window adds a decaying spring offset (cheap wobbly, no mesh). Workspace slide foreshortens like a cube face (scale + hinge pull). `--effects=low` stays fade-only; `off` is instant.
- Expose: ease-in-out enter/leave, selected tile scales up, title under the cell, ↑/↓ move by row. `LayoutGridInto` + presenter `cellsBuf` so the grid is not allocated every frame.
- Still no plugin graph / real 3D cube / mesh jelly. No IME.

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

- Added timeout handling to GPU display operations. This historical release does not define current teardown guarantees; the current Vulkan presenter waits for device idleness before destroying resources.
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

## Current build and run instructions

Entries for 0.9 and earlier describe retired implementations. Use [README.md](README.md)
and [docs/RUN-ABOX.md](docs/RUN-ABOX.md) for the current experience and controls.

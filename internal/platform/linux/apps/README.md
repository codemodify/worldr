This package owns a private Wayland server for application compatibility. It
does not own rendering, camera placement, host windows, processes or the desktop
session. All calls run on one owner goroutine; no process-wide environment
variables are changed. The Unix socket lives in a fresh mode-0700 directory.

`Poll` dispatches without waiting and returns currently mapped toplevels. A
revision identifies immutable, tightly packed, opaque RGBA pixels. Surface IDs
remain stable across buffer resizing and are never reused within a server.
`AppID` is the client's bounded xdg application ID; `PID` comes from the Wayland
connection credentials. Neither is a persistent surface identity or a security
principal. Metadata can change independently of image revisions.
`CloseSurface` asks one mapped toplevel to close; a client can decline or open
a confirmation dialog. It does not kill the process or close sibling windows.
Already removed surface IDs are harmless. `RequestClose` remains the request
for all clients during workspace shutdown.
Pointer coordinates and requested sizes use logical surface coordinates;
`LogicalWidth`/`LogicalHeight` account for buffer scale and viewport destinations.
`SetScale120` advertises fractional density without forcing a client resize;
clients choose their buffers and double-buffered viewport crop/destination.
SHM snapshots retain source pixel density after cropping. SHM buffers are
copied under libwayland's SIGBUS protection, then immediately released. Snapshot
pixels stay valid after subsequent polls and client disconnects. Callers must not
modify them. A frame callback currently acknowledges the copied commit; this is
not presentation timing or a claim that the frame reached a physical display.
Explicit `damage_buffer` rectangles are clipped and copied over the previous
immutable snapshot. Surface-space damage is converted with the scale committed
in the same transaction; viewport-mapped surface damage conservatively takes a
full copy. A size, format or attach-offset change also takes a full copy. Damage
is consumed by exactly one commit, including synchronized subsurface commits.

`Cursor` exposes the pointer-focused client's committed cursor separately from
opaque application content. Cursor pixels retain premultiplied RGBA transparency;
buffer scale and logical hotspot control their display size and alignment.
`Set=false` selects the workspace fallback, while `Hidden=true` with `Set=true`
explicitly hides it. Only the pointer-focused client and its latest enter serial
can change the choice. Cursor roles persist until the surface is destroyed;
pointer leave, focused-window unmap/destruction, and disconnect clear the choice.
Image identity/revision allow retained textures across hotspot changes. Cursor
buffers are limited to 512 pixels per axis; their snapshots share the image
memory limit below. Basic cursor subsurfaces compose within the cursor buffer.
During a drag, `Cursor.DragIcon` exposes the source client's SHM drag image and
accumulated attach offset. Drag images may be as large as 4096 pixels per axis;
Chromium can use a screenshot of the entire dragged element. Viewport logical
dimensions, rather than integer buffer scale alone, determine overlay size.
Protocol tests cover invalid serials/roles, client isolation, alpha, scale,
hotspot offsets, explicit hiding and lifetime; isolated toolkit tests verify
foot's cursor and Chromium CSS crosshair/text/hidden cursor changes.

The tested client is foot with an ordinary shell. The integration test checks
actual glyph pixels, keys reaching the shell, resize and PTY dimensions, immutable
snapshots, local clipboard transfer, external clipboard FD import/export, and
disconnect cleanup. A two-client test closes one window and verifies that the
other remains mapped and receives shell input:

```sh
WORLDR_TEST_APPS=1 go test ./internal/platform/linux/apps
```

Clipboard bridging exposes metadata and pipes, without reading or storing
clipboard contents. `ClipboardOffer` includes a revision and stable current
offer ID; `ReceiveClipboard` borrows the caller's descriptor and rejects stale
IDs. `OfferClipboard` advertises external MIME types. A client's paste produces
an owned descriptor through `PollClipboardRequests`, which the caller must relay
or close. `ExternalID` identifies echoed external selections so the application
controller can avoid loops. At most 64 requests can wait; shutdown closes all
undrained descriptors.

Drag-and-drop accepts the exact held-button serial belonging to the source
surface. While `DragActive`, the workspace repicks the destination and calls
`Pointer` with that root's logical coordinates; keyboard focus stays at the
source. `Pointer(0)` leaves a destination; `CancelDrag` cancels without dropping
and drains held buttons. COPY/MOVE actions, MIME acceptance, pipe transfer,
post-drop completion, version-1 destinations, and source/destination loss are
covered. ASK actions have no picker and are rejected. An isolated two-process
Chromium test drags UTF-8 text between HTML pages and verifies both the actual
drop payload and the source's successful completion.

`SetDMABufImporterForDevice` optionally advertises linux-dmabuf version 4 using
the renderer's validated DRM render node. Its sealed format-table memfd contains
only the exact supported format/modifier pairs, with main-device and tranche
feedback for that node. The compatibility `SetDMABufImporter` entry point has
no device identity and therefore advertises version 3. The implemented subset is
one memory plane, an explicit modifier, XRGB/ARGB/XBGR/ABGR in 8888 and 2101010
layouts, flags zero, normal buffer transform, and 4096 pixels per axis. LINEAR
is supported where available; a non-LINEAR pair is advertised only after Vulkan
confirms exact modifier import, one modifier plane and transfer-source support,
plus LINEAR export/import and sampling for the retained result. Advertisement is
bounded to 256 pairs. The synchronous importer borrows the FD, waits for its
implicit fence, and GPU-copies into an owned immutable LINEAR snapshot before
`wl_buffer.release`; there is no CPU pixel readback. Unsupported pairs are not
advertised. GPU cursor/drag-icon buffers remain unsupported.

GPU-backed trees expose `Surface.Layers`, ordered back-to-front including the
root, with clipped logical rectangles, source UV endpoints, immutable textures,
and format-derived opacity. SHM children can coexist with GPU layers, including
below-parent ordering. The host draws child layers within the root extent;
input remains rooted in the server. `RetiredTextures` transfers replaced or
destroyed snapshots for native resource release followed by `Texture.Close`.
Drain it after server shutdown too. Limits are 32 visible layers per root,
64 pending DMA-BUF parameter objects, 256 client DMA-BUF buffers, and the
renderer-wide 256 MiB exported-snapshot budget. Real GBM/Wayland/Vulkan tests
check 8-bit and advertised 10-bit channel order, a real advertised non-LINEAR
modifier when available, exact protocol modifier delivery, LINEAR snapshot
ownership, X-format opacity, buffer reuse, retained GPU rendering, crop/order,
synchronized child publication, malformed FD isolation, and
replacement/disconnect retirement:

```sh
WORLDR_TEST_DMABUF=1 go test \
  ./internal/platform/linux/apps ./internal/platform/linux/native -run DMABuf
```

Set `WORLDR_TEST_DMABUF_MODIFIERS=1` as well to require non-LINEAR hardware
coverage rather than skipping that case on devices that expose only LINEAR.

The optional Xwayland manager uses `AssociateX11` to match a raw `wl_surface`
to the managed process's exact peer PID and object ID. This never guesses by
dimensions. `UpdateX11`, `WithdrawX11`, and `X11Window` maintain metadata and
mapping; XWM focus, configure, close policy and process lifetime belong to the
separate `xwayland` package. Native xdg surfaces cannot have their role stolen.

The XFixes-backed X11 `CLIPBOARD` endpoint joins the same application-level
broker. It negotiates `TARGETS` without reading content, relays bytes only after
a paste request, rejects stale owners, and bounds each non-INCR transfer to
1 MiB and two seconds. The separate `xwayland` package routes bounded copy-only
XDND versions 3–5 between exact managed X11 windows; payloads stay in the
standard client-to-client XdndSelection transfer. Cross-protocol XDND, primary
selection and clipboard INCR remain outside this subset.

`WORLDR_TEST_TOOLKITS=1 go test -v ./internal/platform/linux/apps`
uses a temporary Chromium profile with GPU disabled and a local HTML file, plus
an isolated Konsole configuration. It checks initial mapping and actual keyboard
input into the page or shell. It also verifies Chromium dropdown pointer and
keyboard selection, XDG shell v3 negotiation, context menus, outside-click
dismissal, and closing a parent with an open popup. Konsole checks cover a
separate Qt dialog, a context menu, nested submenu pointer routing, and
disconnect with an open menu. This does not
establish general browser or arbitrary toolkit compatibility.

Advertised globals: wl_compositor 4, wl_subcompositor 1, built-in wl_shm,
xdg_wm_base 3, wl_output 2, wl_seat 5, wl_data_device_manager 3,
zxdg_decoration_manager_v1 1, wp_viewporter 1, wp_fractional_scale_manager_v1 1,
and optional zwp_linux_dmabuf_v1 3 or 4. Version 4 is exposed only with a
validated renderer render node and device feedback. Foot negotiates server-side decorations.
Primary selection and legacy-client IME are not implemented. The server negotiates
XDG shell version 3. Popup placement implements positioner anchors, gravity,
offsets, flip/slide/resize constraints, reactive rules and explicit repositioning.
Grab serials must come from recent input to the same root surface. Popup buffers
are composited into their root's snapshot with premultiplied-alpha blending;
snapshot alpha remains opaque. Pointer coordinates include nested window-geometry
offsets. Clicking outside a grabbing popup dismisses it without activating the
parent on that click, and popup keyboard focus returns to its parent on dismissal.

**Popups and subsurfaces are contained within the main buffer.** Its dimensions
stay stable while menus open. Explicit reposition requests copy their positioner,
emit `repositioned`, popup configure and surface configure in protocol order, and
do not move until the client acknowledges that configure. A parent-size hint is
used for that requested placement. The parent-configure serial is accepted as
metadata but does not delay placement; later reactive updates use committed
ancestor geometry. Reactive popups are reconstrained after those bounds change,
with no configure when the constrained placement is unchanged. Oversized menus,
clients that prohibit fitting adjustments, and popups that extend into the
surrounding spatial workspace remain clipped at the root bounds.
Ordinary Qt dialogs are exposed as separate toplevel surfaces. Arbitrary
subsurface trees and general browser/toolkit compatibility are not yet validated.
Synchronized subsurfaces retain immutable content updates and the exact child
updates captured by each parent commit. Pixels, viewport, input regions, child
membership, position and stacking apply together; unrelated sibling commits do
not expose cached state. Frame callbacks follow the corresponding applied
generation, and inherited desynchronization releases eligible cached updates.
The server retains at most 512 content updates; bounded input regions support
64 union/subtraction rectangles and are copied when assigned to a surface.
The buffer transform must be normal. Explicit SHM damage updates only the clipped
union over a fresh immutable snapshot; commits without damage retain the full-copy
compatibility path. Limits are 4096 pixels per axis, 128 live wl_surfaces, 32
toplevels, and 128 MiB of server-owned image storage.

The generated protocol files are checked in, so ordinary builds need only
wayland-server and xkbcommon development libraries. Regenerate with
`sh internal/platform/linux/apps/generate.sh`. The xdg-shell interface symbols
are namespaced by this package's cgo CFLAGS to coexist with the independent
Wayland client bindings in the host package.

# Native development in worldr

Worldr has two native boundaries. Internal Go contracts build the workspace,
AXIAL, terminal and Files into the shell. The public, versioned
[`sdk/nativeapp/v1`](../sdk/nativeapp/v1) contract runs an independently built
native application as a child process. Neither path needs Wayland or X11.
The v1 process boundary is an application protocol and lifecycle boundary; it
is not a security sandbox or a stable C ABI.

| Starting point | Current example | Appropriate integration |
| --- | --- | --- |
| Public `sdk/nativeapp/v1.Application` | [Orbital instrument](../examples/native-instrument/main.go) | An external native tool with retained pixels, spatial meshes and semantic controls |
| Scene-owning `experience.Experience` | [AXIAL workspace](../internal/workspace/workspace.go) | A scene, camera, models, instruments and interaction |
| Hosted `experience.Applications` provider | [Native terminal](../internal/nativeapps/provider.go), [project browser](../internal/projectapp/provider.go) | Independently focusable content planes arranged by the workspace |

## The public native-app SDK

An external app implements `nativeapp.Application`, optionally implements the
typed startup, update, focus, input, resize, surface-close and shutdown
interfaces, and calls `nativeapp.Serve(context, app, os.Stdin, os.Stdout)`.
Build and launch the reference app with:

```sh
go build -o bin/worldr-native-instrument ./examples/native-instrument
./bin/worldr-shell --backend=nested --native-app=./bin/worldr-native-instrument
```

`--native-app` is repeatable up to four times. Worldr executes each value
directly without a command shell, negotiates exactly protocol version 1, and
gives each provider up to eight surfaces. Repeated processes with the same
manifest ID receive deterministic `instance-2` and later workspace key
namespaces in their per-manifest launch order; the first instance retains the
original key namespace. Standard output belongs exclusively to the
length-prefixed JSON protocol; write logs to standard error. The host
runs pipe I/O on a bounded worker. A slow app therefore cannot stall rendering,
and an input backlog beyond 256 requests terminates that provider explicitly.
Startup negotiation is limited to three seconds and shutdown grace to 500 ms.

Each `Snapshot` contains the complete live surface list and deltas for retained
resources. A new RGBA8 texture begins at revision 1 with a full image. Later
revisions advance by one and can send a tightly packed damage rectangle; a
resize requires another full image. Meshes are immutable and replacement uses
a new resource ID. An app removes all surface/object references before listing
a texture or mesh for retirement. The SDK validator checks this state across
snapshots before data leaves the app, and the host validates it independently
before creating renderer resources.

Spatial mesh materials can opt into bounded thin-glass transmission by setting
`Material.Transmission` in `[0,1]` together with `Translucent`. It reduces the
diffuse body response while preserving direct highlight and rim lighting.
`Material.Refraction` in `[0,1]` requires a lit, translucent mesh with nonzero
`Transmission`; it samples the opaque scene through that mesh with at most an
18-output-pixel normal-directed bend. `Material.RefractionBlur` in `[0,1]`
requires nonzero `Refraction` and adds a fixed five-tap frosted footprint of at
most six output pixels.

The backdrop is one opaque-scene prepass sampled only beneath explicit native
glass. It excludes other transparent layers, screen-space overlays and the host
cursor. The host does not modify the application's retained texture bytes. Zero
refraction and blur preserve the prior transmission-only rendering and exact
client/legacy texture pixels; opting in affects only the composed area covered by
the glass. These optional fields are additive to the v1 JSON contract.

The in-process renderer also has `render.Material.Hologram`, a translucent-mesh
projection treatment with an opaque-depth contact cue and a host-owned animated
phase. It is intentionally not part of the public `sdk/nativeapp/v1` JSON
contract. The v1 protocol remains stable; a future SDK revision can expose the
effect together with an explicit presentation-motion contract rather than making
an external app infer the user's Reduced Motion preference.

The v1 budgets are deliberately finite: 8 surfaces, 32 textures, 64 MiB of
retained RGBA pixels, 32 meshes, 250,000 vertices, 750,000 indices, 4,096
spatial objects per surface, and 512 semantic nodes across a snapshot. Texture
dimensions are at most 4,096×4,096. Protocol packets are capped at 96 MiB,
including JSON/base64 overhead. Invalid IDs, revisions, references, UTF-8,
transforms, material values, semantic bounds and text-input offsets reject the
snapshot without partially changing the visible surface list.

Surfaces carry a runtime ID and app-local stable key. Worldr prefixes the key
with the manifest ID for placement persistence and uses a deterministic hash
suffix only when the valid composite would exceed the workspace's 256-byte key
limit. The host owns window movement/depth and sends pointer coordinates in
texture pixels. Physical key names plus evdev
codes, XKB state, scrolling, cancellation, text commits/preedit and 3D pick
identity cross the input contract. A surface can publish a focused text context
and a bounded semantic tree independently of its pixels. Semantic data is ready
for host adaptation; it is not yet exported through AT-SPI.

The process inherits the normal environment and receives
`WORLDR_NATIVE_APP=stdio` and `WORLDR_NATIVE_APP_VERSION=1`. Version 1 does not
grant direct Vulkan handles, arbitrary host file access, clipboard services,
network isolation or process sandboxing. Use the existing Wayland/Xwayland
compatibility path for unmodified desktop software. Future incompatible SDKs
will use a separate versioned package and protocol rather than changing v1.

## A scene experience

[Experience](../internal/experience/experience.go) supplies `Info`, `Atlas`,
`Update`, `Draw`, `Handle`, and `Close`. The [host](../internal/app/run.go) owns
display/input adapters, the Vulkan session, timing, submission, file access and
shutdown. [main.go](../cmd/worldr-shell/main.go) selects `workspace.NewDesktop` by default,
or `workspace.New` for `--experience=axial`. Adding another experience requires
extending that wiring.
Hosted tools add `SpatialContent` to their application surface; they do not need
to fork the workspace or become a separate experience. The file-backed
[model inspector](../internal/modelapp/provider.go) demonstrates this contract.

[Scene](../internal/scene/scene.go) holds transformed `Node` objects containing
either a [Mesh](../internal/scene/mesh.go) or a texture surface. Cameras drive
projection and picking. [Canvas](../internal/scene/canvas.go) combines scene
commands with screen controls. AXIAL's [input](../internal/workspace/input.go)
maps interaction into [typed actions](../internal/workspace/actions.go), so
buttons, shortcuts and demonstrations share validated behavior and undo rules.

Experience calls belong to one host goroutine. A frame returned by `Draw` is
borrowed: finish submission before another `Draw` or `Close`. Atlas pixels remain
immutable for that lifetime. Keep polling and updates bounded so one tool cannot
block presentation or input delivery.

## A hosted native application

[Applications](../internal/experience/applications.go) exposes `Surfaces`,
`Focus`, `Send`, and `Resize`. Each `ApplicationSurface` supplies a runtime `ID`,
a stable layout `Key`, title, app identity and retained `render.Texture`.
`Focus(0)` releases keyboard focus. `Send` pointer coordinates are texture pixels;
the provider converts them into its own content coordinates. Resize changes
content resolution independently of spatial placement.

The terminal [Provider](../internal/nativeapps/provider.go) polls the
[PTY/libvterm backend](../internal/terminal/terminal.go), rasterizes real cells
through its [renderer](../internal/nativeapps/renderer.go), and updates damaged
texture regions. Its [Manager](../internal/nativeapps/manager.go) owns separate
sessions, launch slots, focus, closure and retired resources. The UI provider is
in-process; the PTY shell is a child process. This path has no Wayland bridge.

The project [provider](../internal/projectapp/provider.go), selected by `--project`,
uses a cancellable background reader with generation-tagged results. It bounds
UTF-8 previews to 1 MiB and directory listings to 5000 entries. On Linux a retained
root descriptor anchors child access; symlinks and special files are not followed.
UI state and texture mutation remain on the host goroutine. Files supports bounded
current-folder search, visible-row image thumbnails and a 2-second refresh.
Anchored worker operations create folders, rename, move, duplicate, trash/restore
and undo with no silent overwrite. Search/dialog fields use the shared UI and
clipboard/IME contracts; explicit path copying uses the same clipboard broker.

`SetTerminalHandler` handles Terminal Here on the host goroutine. The browser
opens the selected directory on its reader worker and lends its descriptor only
for the duration of the callback; it closes the descriptor on success, failure,
cancellation or a stale result. The host preflights live-window and saved-layout
capacity before launching a process. `nativeapps.Manager.LaunchTerminalInDirectory`
passes the descriptor to `terminal.Options.Directory`; the synchronous PTY startup
uses that opened directory without reopening its display path or changing the
parent's working directory. A regular-file selection uses the current folder.
Launching preserves focus and the view, and later ordinary terminal launches
retain their default directory and environment.

An explicit video or photo Open transfers an anchored read-only regular-file descriptor
through the browser's asynchronous reader to `SetOpenHandler`. The native
[media provider](../internal/mediaapp/provider.go) owns the descriptor on success
and closes its [libmpv backend](../internal/media/player_linux.go) before closing
the file. Stale/canceled browser results close unclaimed descriptors. The host
registers the player in the same hub after an `ApplicationPlacementChecker`
preflight. Opening preserves keyboard focus, selection, camera and Read mode;
`ApplicationActivator` is reserved for explicit user presentation decisions.
The host registers a photo/video `Collection`, creating another independent
window for each explicit Open. Stable sparse keys preserve each saved placement.

The media backend needs libmpv 0.37+ and uses its software render API. Decode,
scaling and RGBA conversion run on the CPU; the workspace samples a retained
texture through Vulkan. libmpv owns playback timing and system audio. Player
commands are asynchronous, events are drained with a bound, and configuration,
user scripts, built-in Lua/UI services, external reference loading and network
protocols are disabled for this local-file path. Unknown built-in-service
options are tolerated for compatibility with older supported libmpv releases.
Chrome is cached per size; frames and live controls update the image. Player
content is bounded to 1920×1080. Reduced Motion does not pause user-requested
media. GPU decode imports and color-managed HDR are not provided.

The [photo provider](../internal/photoapp/provider.go) reserves a loading surface,
then decodes an anchored descriptor on one worker with bounded request/result
queues. Limits are 64 MiB encoded, 16,384 pixels per side and 40 MP. JPEG, PNG,
WebP, BMP and the first GIF frame are supported, with JPEG EXIF orientation.
Stale loads cannot overwrite newer images; a failed replacement preserves the
current image and reports the error. The photo requests `DragContent`
presentation: the workspace owns photo dragging, including throw momentum, and
suppresses the grip and Overview title. Its native frame style uses a retained
cyan left bracket with partial top/bottom returns, entirely outside image pixels.
Other providers can request `Frameless` to suppress all frame geometry.
The photo provider fits the whole image
inside the requested size and gives its retained texture the same aspect ratio,
compositing transparency only within the image. It has no manual viewing controls.
Textures update only when the image, status or requested size changes. Successful replacement gives the image a fresh runtime ID
while preserving its layout key; retired textures follow the host release path.

For another native tool, implement a provider and register it in the host's
provider list and [application hub](../internal/app/application_hub.go). Wire
background work through optional `ApplicationPoller`, resource teardown through
`ApplicationProviderCloser`, and GPU texture retirement through
`ApplicationTextureRetirer`. The hub polls providers in registration order,
drains their retired IDs after the experience withdraws old surfaces, and closes
providers once in reverse order. Pending retirement IDs must survive provider
closure until drained; session shutdown releases any remaining GPU resources.
The host also uses this teardown path after a partial startup failure.
These interfaces keep simple providers valid without custom run-loop branches.
`ApplicationLaunchCatalog` advertises tools to the searchable launcher; terminal,
Files and the native note editor provide entries. Optional launcher,
closer and cursor interfaces are in the same experience contract file.

The hub remaps provider IDs; the [workspace](../internal/workspace/applications.go)
owns placement, depth picking, pointer capture and keyboard focus. Raw keycodes,
XKB metadata, button codes, scroll and cancellation survive the event boundary.
The terminal's `Seat` method receives keyboard metadata even while unfocused;
physical keys reach only the focused input owner. Follow that distinction when
handling text input. Selection and camera movement do not grant typing focus.

## Resources, rendering and saved state

[Render types](../internal/render/types.go) separate three command kinds:

- `SceneCommand` submits retained meshes and spatial texture planes sharing depth.
  Content is opaque by default; explicit `Translucent` enables depth-peeled alpha.
- `OverlayCommand` draws screen triangles using the coverage atlas.
- `ImageCommand` blends a retained **premultiplied RGBA** screen image without
  depth testing or writes. Client cursors use this route; it does not change the
  explicit spatial-surface alpha policy.

Create immutable geometry once and reuse it through transforms/materials.
For a luminous accent, set a mesh node's `Glow` RGB channels in [0,1]. Emission
is explicit and belongs to that node; bright text and textures never emit
automatically. The effect adds a depth-occluded, viewport-clipped background halo
behind normal scene content. Zero skips it, and a frame supports at most four
emitting camera commands. The separate transparency and shadow paths are documented in
[RENDERING.md](RENDERING.md); HDR output is not provided.
[Texture](../internal/render/texture.go) copies RGBA input, retains identity and
revision, and supports changed rectangles through `Update` or replacement through
`Replace`. The [Vulkan adapter](../internal/platform/linux/native/frame.go)
uploads geometry once and texture changes as needed. Removing a scene node alone
does not release its GPU resources: coordinate `ReleaseGeometry`/`ReleaseTexture`
with the host after removing references, or let session shutdown release them.
Interactive rendering needs no full-frame CPU readback.

Use `Stateful` for an experience document; the [host state layer](../internal/app/state.go)
owns file I/O. AXIAL's [document](../internal/workspace/document.go) validates before
installation and persists stable application keys and placement, never runtime
IDs, GPU handles, active captures or process state. The general workspace uses a separate [document](../internal/workspace/desktop.go)
without study data. The shared action reducer records domain edits; playback
ticks and presentation interpolation remain transient. A drag and its inertial
throw share one undo record. Timestamp-derived release velocity and analytic
exponential friction remain transient; manual saving settles at the current position.

The host's [session coordinator](../internal/app/session.go) records provider
resource references in a version-2 envelope alongside the experience payload.
Version-1 layout envelopes remain readable. Files exposes `SessionState`,
`SessionState.Validate` and `NewSession` for root/folder/selection restoration;
pending listings preserve the requested selection. Photo state records its path;
media state adds position, pause, volume and mute. Native terminal state contains
a stable slot key, working directory, and bounded inert task-recipe definitions.
Restore starts a fresh configured shell through a borrowed directory descriptor,
with no saved command replay, pending dispatch, live task status, or terminal
screen history. Legacy process launches remain explicit CLI/profile actions.
Resource paths are verified against open descriptors where available.

`Checkpointer.CheckpointState` snapshots without changing focus, history or live
gestures. The [autosaver](../internal/app/autosave.go) captures owned bytes on the
host and permits one background `writeState` at a time; it never calls
`SaveState` on its worker. Unfinished gestures are excluded, while released
throws are captured without stopping them. Successful identical snapshots are
skipped, and failed writes retry on a later interval. Manual save flushes the
worker before replacing the primary, resetting its baseline and removing recovery.

`--state` enables the default `--autosave=5s`; `--autosave=0` disables periodic
writes. Startup validates `.autosave` and primary candidates before transactional
installation, choosing a newer valid recovery or falling back after a bad file.
`--fresh` loads only the primary layout and explicit CLI apps, skipping saved
native resources and recovery. A `.lock` guards concurrent use of the state path.
Missing resources are reported and kept pending with retained placements;
replacement or forgetting the closed placement removes the old association.

## Hosted 3D content and shared controls

`ApplicationSurface.Spatial` borrows `SpatialContent` on the host goroutine.
Objects have nonzero provider-local IDs and an optional parent which precedes
its child. The backing texture supplies controls at Z=0. One application width
is one local unit; +Y is up and +Z faces the user. Meshes remain immutable and
shared while transforms/materials change. Up to 4096 objects and 256 labels fit
one validated application payload. `Event.SpatialObject`, `SpatialTriangle` and
`SpatialPoint` identify a picked mesh and app-local hit. Pointer gestures retain
the backing plane's pixel coordinates. `ApplicationGeometryRetirer` releases
mesh IDs only after the provider removes all references.

In-process providers may opt a translucent mesh into `Material.Hologram`. The
workspace supplies its own `Scene.EffectPhase`, freezes that phase under Reduced
Motion and leaves provider playback untouched. This treatment is mesh-only:
application control textures, legacy buffers, shaped overlays and the host cursor
never enter its shader.

Explicit `FrameCinematic` and `FramePhotoBracket` preserve the individual app
identities. `nativeui.Painter` supplies shaped labels, buttons, panels, menus and
fields on retained RGBA surfaces; `Controller` supplies keyboard/pointer focus
and semantic roles. `Field` edits graphemes, selection and transient IME preedit.
Linux uses Pango/HarfBuzz/font fallback. Non-cgo builds retain limited Go-font
rendering for portable contract tests. Terminal cells remain a fixed grid.

Providers expose `TextInput(id)` only for their focused editable field, using a
new context ID each time focus changes. The hub qualifies that ID and converts
it back before sending text. The workspace projects the candidate rectangle
through the current camera; the nested host translates framebuffer pixels to
logical display coordinates. Text commits apply surrounding deletions and
insertion atomically; stale field transactions are rejected even within one
input poll batch. Ordinary XKB/compose input remains available without the
optional compositor protocol. `--accessibility-socket=/absolute/path` exports
versioned, bounded JSON snapshots of native semantic trees, application focus and
application-pixel bounds. The display-manager launcher enables a private `0600`
socket automatically. This is an adapter boundary, not direct AT-SPI registration.

The model inspector supports triangulated OBJ and ASCII/binary STL (32 MiB,
200,000 triangles, 256 components per file). Up to eight inspectors share an
800,000 accepted-triangle budget and one active parser/mesh builder. Measurements
use source coordinates; Units labels the model's units rather than rescaling its
geometry. Save writes a separate versioned `.worldr-model.json` referencing the
source plus view, measurements and annotations. The authored sample in
`examples/models/mount.obj` can be opened with `--model=examples/models/mount.obj`.

Photo collections allow eight viewers and 64 million retained source pixels,
with serialized decode admission. Video collections allow four independent
players. Their slot-1 state remains in the compatible `photo`/`media` manifest
fields; additional sparse slots use `photos`/`videos`. Restore never compacts gaps.

# Advanced and Hologram reference interfaces

These two native applications extend the Merrick reference desktop using the
same public skin, control, and application SDKs. Each runs in its own saved
workspace. Their window frames and controls remain configurable through the
**Skins** button at the bottom right.

## Switch experiments

Use the shared **Experiments** button at the bottom left, or **Ctrl+Alt+E**, to
list and switch between the experiments. Click an entry, or select it with the
arrow keys and press Enter. Escape closes the menu. The entries are ordered:

1. AXIAL / 07
2. Spatial Workspace
3. Skin Studio
4. Merrick Desktop
5. Advanced Studio
6. Hologram Disk Management
7. Plasma Fluid Surfaces
8. Workspace Navigator

Each experiment has a separate saved layout. Switching saves the current layout
before opening the selected experiment; returning restores its layout and
reopens supported saved native applications. Experiments restart when selected:
SDK demonstration control values reset, and restored terminals start fresh
shell sessions. Running processes are not retained, and external Wayland/X11
applications are not automatically reopened. Live skin changes within an
experiment continue to preserve its running controls.

## Advanced Studio

```sh
./scripts/run-advanced-desktop.sh
```

The steel-blue design includes a compact masthead, seven navigation tabs,
architectural hero, inset three-column panels, project selection, a simulated
demo reel, searchable updates, and a local email form. Navigation changes the
selected section. The reel can play, pause, and reset. The email field supports
keyboard editing and input-method composition; submission validates locally.
The example does not send mail or make network requests.

The host owns the thin beveled window frame and minimize/Read/close controls.
See [the app guide](../examples/advanced-desktop/README.md) for protocol and
interaction details.

## Hologram Disk Management

```sh
./scripts/run-hologram-desktop.sh
```

The luminous interface has sortable volume columns, synchronized table/block
selection, perspective partition blocks, usage/details views, sample refresh,
grid controls, and partition filters. It uses fictional disk fixtures; actions
change the visualization and never touch storage devices.

The grid, particles, isometric faces, neon edges, and labels are drawn in code.
They respond to palette changes and selection. See
[the app guide](../examples/hologram-desktop/README.md).

## Layout, themes, and captures

Each launcher builds its two binaries and copies the supplied `layout.json`
only when its saved workspace does not exist. Subsequent launches retain window
positions and reselect the named reference skin. The examples retain control
values through live skin changes; their demonstration data resets on restart.
Saved workspaces live at `dist/advanced-desktop/state.json` and
`dist/hologram-desktop/state.json`. Merrick's saved workspace is independent.

Options after the launcher override the default host flags, for example:

```sh
./scripts/run-hologram-desktop.sh --backend=headless --duration=2s \
  --snapshot=dist/hologram-desktop/capture.png
./bin/advanced-desktop --snapshot=dist/advanced-desktop/client.png
./bin/hologram-desktop --snapshot=dist/hologram-desktop/client.png
```

The shared packages describe compact window metrics, glyphs, material layers,
and semantic colors. `desktop.chrome: "corner-tools"` reserves a compact row
for Tools, Windows, Skins, and Help; `desktop.backdrop: "quiet-gradient"` paints
the package's `desktop-background` and `desktop-glow` colors. Custom package IDs
can use both treatments. Empty fields retain the ordinary workspace.

## Architectural artwork

The final banner is embedded from
`examples/advanced-desktop/assets/architecture.png`. It was generated with the
built-in image-generation tool from the user's Advanced reference. No generated
bitmap is used for the Hologram interface.

Exact prompt:

> Use case: precise-object-edit. Asset type: architectural hero banner for a native steel-blue desktop application. The first supplied image is the 2Advanced Studios reference with a steel-blue website and wide architectural photo panel. Extract and reconstruct ONLY the architectural image inside that first reference's large central banner: a dramatic low-angle view under crossing massive concrete and brushed-steel bridge beams, diagonal structural beams forming triangular openings to a cloudy gray sky, deep navy recesses and cool slate-blue monochrome toning. Remove every word, logo, interface border, controls, decorative circles, and all the surrounding website. Reconstruct the underlying architecture cleanly. The second supplied image is a neon disk interface: ignore it completely; it is not a style reference for this asset. Output only the clean architectural panorama, edge-to-edge, in a wide 3:1 landscape composition (approximately 1536x512 or 2304x768). Preserve the reference's muted blue-gray photographic/early-2000s digital artwork mood and perspective. No text, no people, no neon, no UI, no frame, no watermark.

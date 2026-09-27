# Window and control skins

Worldr uses one versioned skin package for host window chrome and participating
native application controls. Select **Settings → Skins** from the scene
controller's gear, or launch with `--skin=merrick`, `advanced`, `hologram`,
`plasma`, or `future-panels`. Settings also edits the accent, control shapes, and type size live.
The complete package, including custom recipes, is saved with `--state`.
Existing workspaces retain their legacy appearance until a skin is selected.

| Preset | Window and control language |
| --- | --- |
| Merrick | Sculpted silver desktop, slate side tabs, vertical window titles and compact blue controls |
| Advanced | Steel-blue studio panels, metallic inset edges and compact beveled controls |
| Hologram | Cyan/violet double rails, luminous edges and transparent control outlines |
| Plasma | Rounded surfaces, spacious controls and left-hand window buttons |
| Future Panels | Translucent slate sheets, muted green text, narrow chrome and a deep spatial field |

These are starting designs inspired by the visual references. Plasma's native
chrome uses the existing renderer's glass materials where authored; the preset
does not implement the linked web demo's liquid merging or spring physics.

## Run the working showcase

For the seven-window Merrick reference desktop:

```sh
./scripts/run-merrick-desktop.sh
```

The script builds both native binaries and seeds a separate saved workspace at
`dist/merrick-desktop/state.json`. Later runs preserve its layout. Use the
**Merrick** or **Settings** side tab to change skins; **Programs** inside the
desktop reopens closed demo surfaces. The calendar, profile controls, document
pages and message tabs are interactive. See the
[demo guide](../examples/merrick-desktop/README.md) and
[embedded artwork and generation prompts](MERRICK-ASSETS.md).

The [Advanced Studio and Hologram Disk Management demos](REFERENCE-DEMOS.md)
provide two more complete native reference interfaces. Run them with
`./scripts/run-advanced-desktop.sh` and `./scripts/run-hologram-desktop.sh`.

The general control showcase remains available separately:

```sh
make build
go build -o bin/skin-studio ./examples/skin-studio
./bin/worldr-shell --backend=nested --skin=merrick \
  --native-app=./bin/skin-studio --state=dist/skin-studio.json
```

Use `--backend=headless --duration=2s --snapshot=dist/skin-studio.png` to render
without a display. On systems with unpacked Vulkan headers, build with
`make build VULKAN_HEADERS=/path/to/Vulkan-Headers`.

Skin Studio contains functioning buttons, tabs, a searchable channel list,
switches, checkboxes, radio controls, a slider, progress, and a simulated signal
plot. Run/Pause controls the analysis; Gain changes the plot; Lock prevents gain
edits; profile and channel choices change the waveform. Tab, Space, and arrow
keys use the shared controller. Skin changes preserve these values and focus.
The host owns dragging, resizing, minimizing, closing, and the maximize glyph's
**Read** action. Read fits the window and its authored chrome to the viewport.

## Author a package

Export the five complete presets, edit a copy, and load it by path:

```sh
./bin/skin-studio --export-skins=dist/skin-packages
# Edit dist/skin-packages/merrick.json; give it your own ID and name.
./bin/worldr-shell --backend=nested --skin=dist/skin-packages/merrick.json \
  --native-app=./bin/skin-studio --state=dist/custom-desktop.json

# Export the actual control painter without starting a host:
./bin/skin-studio --skin=dist/skin-packages/merrick.json \
  --snapshot=dist/custom-controls.png
```

`sdk/skin/v1` exposes `Skin`, `Builtin`, `Builtins`, `Decode`, `Encode`, `Clone`,
and `Validate`. IDs are extensible; renderers consume the recipes rather than
switching on a preset name. Packages describe:

- Palette tokens and literal `#RRGGBB` / `#RRGGBBAA` colors.
- Typography, spacing, control metrics, and content insets.
- Separate frame, titlebar, drag grip, resize grip, and window-button recipes.
- Window-button side, order, sizes and gaps; independent vector icon paths.
- Top or vertical left title tabs, independent bottom rails, and optional
  client-pixel chrome density for narrow tools and small palettes.
- Desktop chrome and backdrop treatments, selected by package data.
- Per-component layers with rectangular, rounded, chamfered, bracketed,
  notched, or custom polygon geometry.
- Hover, press, focus, selection, disabled, and invalid-state overrides.
- Flat, gradient, glass, and emissive material intent.
- Bounded embedded image and font assets for the public CPU painter.

Optional component recipes include `switch-track`, `switch-thumb`,
`slider-track`, `slider-fill`, `slider-thumb`, `radio-indicator`,
`checkbox-indicator`, `progress-fill`, `meter-fill`, and `scrollbar-thumb`.
These let a package replace the moving parts as well as a control's background.

Geometry and layer bounds use normalized coordinates. Stroke widths, insets,
typography, and window-layout metrics use logical pixels. Window chrome stays
outside the client image; picking follows the same layout used for painting.
Set `window.layout.scale_mode` to `client-pixels` to keep chrome at its authored
pixel density across differently sized clients. An omitted value keeps the
original 960-unit frame density. `desktop.chrome: "slate-tabs"` selects the side
rails; `desktop.backdrop: "sculpted-silver"` selects the embedded silver artwork.
An empty desktop treatment uses the ordinary workspace. Authored backdrops
temporarily replace ambient artwork without altering saved environment choices.
`desktop.chrome: "corner-tools"` supplies compact Tools, Windows, Skins, and Help
buttons. `desktop.backdrop: "quiet-gradient"` draws package palette tokens
`desktop-background` and `desktop-glow`, with semantic color fallbacks. Optional
`desktop.backdrop: "panel-field"` supplies retained, unpickable world-space sheets
and enables per-pixel alpha on application surfaces. The optional `field-panel`,
`field-slate`, `field-line`, and `field-light` tokens customize its colors.
See [Future Panels](FUTURE-PANELS.md) for the launcher and spatial controls. Optional
`window.layout.grip_width` sets a compact grip length in logical pixels; zero
retains the original proportional grip.
Validation rejects unknown fields, malformed geometry, missing palette/icon
references, unsupported versions, invalid assets, and oversized packages.
Packages are limited to 1 MiB; they do not load external file paths or scripts.

## App integration and renderer capabilities

For a public native SDK app, advertise `Manifest.Skins: true`, implement
`SetSkin(skin.Skin) error`, and call `nativeui.ThemeFromSkin`. Apply the resulting
theme to both `Painter.SetTheme` and `Controller.SetTheme`. The latter keeps
slider/scrollbar hit geometry aligned with the painted control. Close painters
when no longer needed. `examples/skin-studio` is a complete implementation;
`sdk/nativeui/v1/gallery` supplies its reusable reference interface and the
Settings preview.

The host sends full packages only to apps that opt in. Older `ControlThemes`
clients still receive their original family/shape preference messages. Selecting
a legacy Windows or Themes choice exits the complete skin; skin-aware apps
receive a compatible legacy package.

| Surface | Current support |
| --- | --- |
| Host window chrome | Procedural geometry, glyphs, gradients, glow and native glass materials; image layers use their procedural paint fallback |
| Public nativeui controls | Recipes, state styles, scalable type, embedded fonts/images, alpha and gradients; glass uses a CPU fallback without scene refraction |
| Built-in Files, Notes, Models, Research, Terminal | Shared control recipes, palette, spacing and type through the native text bridge; Pango uses font family/system fallback |
| Document text, terminal cells, photos and video | Application-owned content and typography |
| Third-party Wayland/X11 apps | Host frames can change; client controls require application/toolkit integration |

Saved skin changes do not enter layout Undo, move applications, or modify their
documents. Runtime caches are bounded, and replaced host geometry is retired
from the renderer.

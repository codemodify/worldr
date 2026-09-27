# Portable skin packages

`sdk/skin/v1` is a standard-library-only JSON contract shared by workspace
window chrome and application controls. A package has an extensible `id`,
`version: 1`, semantic colors, scalable typography, density metrics, layered
component recipes, window-part layout, vector icons, material intent and optional
embedded images/fonts. No plugin code or external file/network access is needed.

```go
s, err := skin.Builtin("merrick") // also advanced, hologram, plasma, future-panels
if err != nil { return err }
s.ID, s.Name = "dev.example.my-desk", "My desk"
s.Palette["accent"] = "#a8d5ff"
if err := skin.Encode(writer, s); err != nil { return err }
// Later: selected, err := skin.Decode(reader)
```

`Builtin` also resolves the legacy `instrument`, `aperture`, `glass` and
`telemetry` preference IDs. `Builtins` returns the five complete presets.
`Clone` owns all nested maps, paths and embedded asset bytes.

Recipes contain ordered layers. A layer can fill, outline, use a material, or
draw an embedded image inside `rect`, `rounded`, `chamfered`, `bracketed`,
`notched`, or arbitrary polygon geometry. `Bounds` and polygon points are
normalized to the component; an omitted bounds is the whole component.
Corner/radius/notch values are fractions of its shorter side. Stroke widths,
content insets and window layout dimensions are logical pixels. Colors are
`#RRGGBB` or straight-alpha `#RRGGBBAA`; the renderer performs premultiplication.
Layer/state/material opacity defaults to one when omitted or zero. Explicit
transparent colors represent zero alpha.

State styles override the first layer's fill/stroke and the component text.
Layers above it retain their authored accents. States compose in this order:
hovered, pressed, selected, focused, invalid, disabled. Control state and
application data remain independent of the skin.

Optional control subparts have independent recipes: `switch-track`,
`switch-thumb`, `slider-track`, `slider-fill`, `slider-thumb`, `radio-indicator`,
`checkbox-indicator`, `progress-fill`, `meter-fill`, and `scrollbar-thumb`.
The `radio` and `checkbox` recipes describe the mark itself. A missing subpart
uses the widget's semantic fallback, never an unrelated button recipe. Fill
recipes can specify contrasting text colors for progress labels over the fill.

Window parts use the same recipes and their own layout and named button icons.
The host keeps frame geometry outside client pixels, and derives functional
button targets from the selected window layout. Applications derive control
paint and input geometry together through the native UI painter/controller.
`window.layout.scale_mode: "client-pixels"` keeps chrome at its authored density
on small tools and large documents; omission retains the historical 960-unit
client width. `title_side: "left"` and `title_width` create a vertical title tab,
`bottom_height` adds a lower rail, and optional `grip_width` reserves a compact
logical-pixel grip in a top titlebar. Omitted dimensions retain host defaults.

Optional desktop renderer traits are `desktop.chrome: "slate-tabs"` or
`"corner-tools"`, and `desktop.backdrop: "sculpted-silver"`, `"quiet-gradient"`,
or `"panel-field"`.
The quiet gradient uses `desktop-background` and `desktop-glow` palette colors.
Empty traits retain the default desktop. These traits are independent of package
IDs, so exported custom packages can use the same placement and background.

Future Panels uses `panel-field` for retained translucent sheets in the 3D
workspace, with ordinary desktop navigation. Its optional scene tokens are
`field-panel`, `field-slate`, `field-line`, `field-light` and `field-fog`, alongside
`desktop-background` and `desktop-glow`. Slate/indigo sheet colors carry alpha;
technical text stays opaque and pale green. Window glass requests only slight
blur/refraction and restrained edge light. Frames and control recipes use thin,
mostly rectangular geometry; client-pixel layout retains usable control targets.

Advanced includes `page-background`, `page-panel`, `page-inset`, `page-edge`,
`page-text`, `page-muted` and `heading-text` for application-drawn content.
Advanced and Hologram also provide `graph-grid`, `graph-line`, `graph-fill`,
`status-online` and `status-alert`; Hologram adds `glow-cyan` and `glow-violet`.
These optional tokens complement the common palette and do not change controls'
behavior.

The CPU painter implements layered alpha, linear gradients, vector geometry,
clipped image layers, scalable embedded Sans/Monospace fonts and optional
OpenType assets. Glass material blur/refraction requires GPU scene support;
the CPU fallback uses transparency. Emission becomes bright layered edges on
the CPU. A renderer need not pretend to implement a GPU effect it lacks.
Host window mesh rendering uses authored fill/stroke fallbacks for image layers.

Assets use `kind: "image"` with PNG/JPEG or `kind: "font"` with TTF/OTF data,
base64 encoded by JSON. A typography `asset` selects a font and a layer `asset`
selects an image. Packages are limited to 1 MiB including formatted JSON and
images to 4 million decoded pixels in total. Validation also bounds layer/path
counts, metrics and resources and checks references. Unsupported schema
versions, unknown fields on decode and malformed values fail before adoption.

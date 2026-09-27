# Worldr native UI SDK v1

The shared skin API applies a complete appearance package to these controls:

```go
selected, err := skin.Builtin("plasma") // sdk/skin/v1
if err != nil { return err }
theme, err := nativeui.ThemeFromSkin(selected)
if err != nil { return err }
ui, err := nativeui.NewPainter(theme)
if err != nil { return err }
defer ui.Close()
var controller nativeui.Controller
defer controller.Close()
if err := controller.SetTheme(theme); err != nil { return err }
```

Call both `Painter.SetTheme` and `Controller.SetTheme` when changing skins.
`Painter.Layout` returns the same slider track and scrollbar thumb used for
pointer input. Layered component recipes, state styles, fonts, vector icons,
embedded images and custom skin IDs are interpreted at runtime. The four
complete presets are Merrick, Advanced, Hologram and Plasma. Existing palette
and shape preferences remain available below.

Scalable embedded Sans/Monospace fonts honor font size and weight; a skin can
embed a TTF/OTF font. Full script shaping/IME editing remains available in the
host's native text backend. `DrawControlBackground`, `ControlContentBounds` and
`ControlTextColor` let such backends share recipes without replacing their
text editor. CPU glass uses alpha; scene refraction requires a GPU renderer.

`sdk/nativeui/v1` paints portable controls into a native application's RGBA
framebuffer. It complements the additive control-theme preference in
`sdk/nativeapp/v1`; manifests and applications which do not opt in remain
compatible and keep their own presentation.

```go
theme, err := nativeui.Builtin(nativeui.Telemetry, nativeui.Notched)
if err != nil {
	return err
}
ui, err := nativeui.NewPainter(theme)
if err != nil {
	return err
}

button := nativeui.Control{
	ID: "scan", Kind: nativeui.KindButton,
	Bounds: image.Rect(20, 20, 180, 54),
	Label: "RUN SCAN", Icon: nativeui.IconPlay,
}
if err := ui.DrawButton(framebuffer, button); err != nil {
	return err
}
```

The four palette families are `Instrument`, `Aperture`, `Glass`, and
`Telemetry`. Geometry is an independent choice: `Chamfered`, `Bracketed`,
`Slab`, or `Notched`. `Theme` exposes semantic palette, metric, and typography
tokens. `Theme.Validate`, `Painter.SetTheme`, `FillShape`, and `StrokeShape`
reject malformed values before drawing.

Worldr's **Settings → Themes** surface uses these same family and shape names as
the saved workspace design/profile selector and shows them in a live control
gallery. A native app opts into live changes with `Manifest.ControlThemes` and
implements `ControlThemeHandler`:

```go
func (a *application) Manifest() nativeapp.Manifest {
	return nativeapp.Manifest{
		ID: "dev.example.application", Name: "Example",
		ControlThemes: true,
	}
}

func (a *application) SetControlTheme(preference nativeapp.ControlTheme) error {
	theme, err := nativeui.FromControlTheme(preference)
	if err != nil {
		return err
	}
	return a.ui.SetTheme(theme)
}
```

Worldr sends the saved family and shape through the additive v1 `theme` request
and sends later Settings changes live. The request is only sent to applications
which advertise support. Older and non-theme-aware apps continue choosing their
own presentation.

The painter includes:

- labels and portable vector icons;
- buttons, icon buttons, fields, multiline text-area visuals, switches,
  checkboxes, radio choices, sliders, progress bars, and meters;
- tabs, segmented choices, menus, list rows, tree rows, table rows, scrollbars,
  and splitters;
- panels, cards, toolbars, dialogs, popovers, tooltips, badges, and separators.

Every paint operation clips to both its control rectangle and destination image.
The default scalable text face is deliberately portable. Apps that require rich shaping
can draw their own text while retaining the theme, shapes, control states, and
controller.

`Controller` consumes `nativeapp.Event` directly. Call `SetControls` after each
layout change, route incoming events through `Handle`, and apply returned
activations or range values to application state. `Decorate` adds the
controller's current hover, pressed, and focus states before painting.
`Semantics` produces the unchanged native-app v1 semantic tree. Toggle-like
controls map to v1 buttons with `Selected`; range controls map to v1 sliders.
The application continues to own text editing, IME transactions, and domain
state. `SetControlsWithin` also validates every node against the current
framebuffer before publishing its semantic tree.

See [`examples/native-instrument`](../../../examples/native-instrument) for a
real out-of-process native surface whose run control repaints immediately when
the workspace theme changes.

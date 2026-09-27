package skin

// futurePanels authors quiet, nearly planar sheets for a deep workspace.
// The host supplies the surrounding field through an optional desktop trait;
// controls and window geometry remain ordinary portable skin recipes.
func futurePanels() (Skin, error) {
	// Builtin returns a new owned package, including the complete control and
	// icon vocabulary. Replacing its authored recipes leaves no shared state.
	s, err := Builtin("advanced")
	if err != nil {
		return Skin{}, err
	}
	s.ID, s.Name = "future-panels", "Future Panels"
	s.Description = "Translucent slate and indigo sheets, restrained green technical text and thin ice-blue edges in a deep panel field."
	s.Desktop = Desktop{Backdrop: "panel-field"}
	s.Palette = palette("#09121BD8", "#172330B8", "#203040DF", "#293F4DEE", "#101D28F5", "#B6DCE6", "#87A994", "#52677788", "#B9D4B8", "#829B95", "#30483DBF", "#526765", "#9ABD92", "#D3C09A", "#D79493")
	for token, color := range map[string]Color{
		"chrome-base": "#0C1822B8", "chrome-edge": "#9ABCCB", "chrome-text": "#C3D9CA",
		"page-background": "#09121BD8", "page-panel": "#172330B8", "page-inset": "#070E17DF", "page-edge": "#52677788", "page-text": "#B9D4B8", "page-muted": "#829B95", "heading-text": "#CAE2D2",
		"graph-grid": "#30454B", "graph-line": "#91B08E", "graph-fill": "#20353788", "status-online": "#9ABD92", "status-alert": "#D3C09A",
		"desktop-background": "#03070B", "desktop-glow": "#233346",
		"field-panel": "#16234448", "field-slate": "#36414E5C", "field-line": "#839F8748", "field-light": "#C7E5FF", "field-fog": "#243848",
	} {
		s.Palette[token] = color
	}
	s.Typography = Typography{Family: "Monospace", Size: 13, LineHeight: 19, Weight: 400}
	s.Metrics = Metrics{Padding: 8, Gap: 6, ControlHeight: 30, Stroke: 1, Corner: 2, Notch: 0, Icon: 16}
	s.Materials = map[string]Material{
		"panel":        {Kind: "flat"},
		"window-glass": {Kind: "glass", Secondary: "chrome-base", Opacity: .84, Blur: .035, Refraction: .015},
		"edge":         {Kind: "emissive", Glow: .06},
	}
	states := studioStates()
	states["hovered"] = StateStyle{Fill: "hover", Stroke: "accent", Text: "accent"}
	states["selected"] = StateStyle{Fill: "selection", Stroke: "accent-alt", Text: "text"}
	control := Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "chamfered", Corner: .045}, Fill: "raised", Stroke: "border", StrokeWidth: .8},
	}, ContentInsets: Insets{Top: 4, Right: 8, Bottom: 4, Left: 8}, TextColor: "text", States: states}
	panel := Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "chamfered", Corner: .006}, Fill: "surface", Stroke: "border", StrokeWidth: .75},
	}, ContentInsets: Insets{Top: 8, Right: 8, Bottom: 8, Left: 8}, TextColor: "text", States: states}
	installStudioControls(&s, control, panel, Geometry{Kind: "rect"})
	// Rows remain one quiet reading plane; selection and focus carry the
	// emphasis instead of an inset metallic frame around every item.
	for _, name := range []string{"menu-item", "list-row", "tree-row", "table-row"} {
		r := cloneRecipe(control)
		r.Layers[0] = Layer{Geometry: Geometry{Kind: "rect"}, Fill: "surface"}
		s.Controls[name] = r
	}
	s.Window.Layout = WindowLayout{ScaleMode: "client-pixels", TitlebarHeight: 32, BorderWidth: 3, ButtonWidth: 32, ButtonHeight: 24, ButtonGap: 4, ResizeSize: 20, GripWidth: 24, ButtonsSide: "right", ButtonOrder: []string{"minimize", "maximize", "close"}}
	s.Window.Frame = Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "chamfered", Corner: .005}, Fill: "chrome-base", Stroke: "border", StrokeWidth: .8, Material: "window-glass"},
		{Bounds: Box{X: .08, W: .17, H: .002}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-edge", Material: "edge", Opacity: .65},
		{Bounds: Box{X: .72, Y: .998, W: .19, H: .002}, Geometry: Geometry{Kind: "rect"}, Fill: "accent-alt", Opacity: .65},
	}, TextColor: "chrome-text", States: map[string]StateStyle{"focused": {Stroke: "chrome-edge"}}}
	s.Window.Titlebar = Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "rect"}, Fill: "chrome-base", Material: "window-glass"},
		{Bounds: Box{Y: .97, W: .74, H: .02}, Geometry: Geometry{Kind: "rect"}, Fill: "border"},
	}, TextColor: "chrome-text"}
	s.Window.Grip = Recipe{Layers: []Layer{
		{Bounds: Box{X: .12, Y: .36, W: .22, H: .05}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-edge"},
		{Bounds: Box{X: .45, Y: .36, W: .43, H: .05}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-edge"},
		{Bounds: Box{X: .12, Y: .59, W: .55, H: .05}, Geometry: Geometry{Kind: "rect"}, Fill: "accent-alt"},
	}, TextColor: "chrome-text"}
	s.Window.Resize = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "rect"}, Fill: "chrome-base"}}, Icon: "resize", TextColor: "muted", States: map[string]StateStyle{"hovered": {Text: "accent"}}}
	s.Window.Buttons = make(map[string]Recipe, 3)
	for _, name := range []string{"minimize", "maximize", "close"} {
		s.Window.Buttons[name] = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "rect"}, Fill: "chrome-base"}}, Icon: "window-" + name, TextColor: "chrome-text", States: map[string]StateStyle{"hovered": {Fill: "hover", Text: "accent"}, "pressed": {Fill: "pressed", Text: "accent-alt"}, "focused": {Stroke: "accent"}}}
	}
	s.Icons["window-minimize"] = Icon{Paths: []Path{path(Point{.22, .68}, Point{.78, .68})}, StrokeWidth: 1.4}
	s.Icons["window-maximize"] = Icon{Paths: []Path{{Points: []Point{{.24, .24}, {.76, .24}, {.76, .76}, {.24, .76}}, Closed: true}}, StrokeWidth: 1.3}
	s.Icons["window-close"] = Icon{Paths: []Path{path(Point{.26, .26}, Point{.74, .74}), path(Point{.74, .26}, Point{.26, .74})}, StrokeWidth: 1.4}
	if err := s.Validate(); err != nil {
		return Skin{}, err
	}
	return s, nil
}

package skin

// Merrick's contrast comes from pale working surfaces framed by dark, compact
// hardware. The authored paths, rails and title placement remain editable data
// in exported packages; renderers do not need to recognize this preset's ID.
func configureMerrickDesk(s *Skin) {
	s.Description = "Pale records, slate-navy bevelled rails, vertical title tabs and compact instrument controls."
	s.Desktop = Desktop{Chrome: "slate-tabs", Backdrop: "sculpted-silver"}
	s.Palette = palette("#DCDCE4", "#CACBD5", "#344B68", "#536F8E", "#1A2B43", "#6894BD", "#B2CBE0", "#304157", "#202735", "#586477", "#416A91", "#858C99", "#4B817B", "#AC843D", "#AC3548")
	for name, value := range map[string]Color{
		"chrome-base": "#263349", "chrome-raised": "#435169", "chrome-edge": "#8994A5", "chrome-shadow": "#101A2B",
		"chrome-text": "#DCE1E9", "chrome-slot": "#1C314C", "chrome-mark": "#8DABCB", "chrome-light": "#596B83",
		"paper": "#E4E4EB", "paper-edge": "#9BA4B3",
		"desktop-rail": "#49566E", "desktop-shadow": "#192939", "desktop-edge": "#778591", "desktop-text": "#D4DEE2", "desktop-active": "#657389",
	} {
		s.Palette[name] = value
	}
	s.Typography = Typography{Family: "Sans", Size: 14, LineHeight: 18, Weight: 400}
	s.Metrics = Metrics{Padding: 6, Gap: 4, ControlHeight: 25, Stroke: 1, Corner: 2, Notch: 4, Icon: 14}
	s.Materials = map[string]Material{
		"panel":        {Kind: "linear-gradient", Secondary: "chrome-slot", Angle: 90},
		"chrome-bevel": {Kind: "linear-gradient", Secondary: "chrome-shadow", Angle: 90},
		"paper":        {Kind: "flat"},
	}
	darkStates := map[string]StateStyle{
		"hovered": {Fill: "hover", Stroke: "chrome-mark", Text: "chrome-text"},
		"pressed": {Fill: "pressed", Stroke: "chrome-edge", Text: "chrome-text", Offset: Point{Y: 1}},
		"focused": {Stroke: "chrome-mark"}, "selected": {Fill: "selection", Stroke: "chrome-mark", Text: "chrome-text"},
		"disabled": {Fill: "chrome-base", Stroke: "disabled", Text: "disabled", Opacity: .65}, "invalid": {Stroke: "danger"},
	}
	dark := Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "rect"}, Fill: "raised", Stroke: "chrome-shadow", StrokeWidth: 1, Material: "panel"},
		{Bounds: Box{X: .015, Y: .035, W: .97, H: .045}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-edge", Opacity: .7},
	}, ContentInsets: Insets{Top: 3, Right: 5, Bottom: 3, Left: 5}, TextColor: "chrome-text", States: darkStates}
	for _, name := range []string{"button", "icon-button", "switch", "tab", "segment", "menu-item", "list-row", "tree-row", "table-row", "scrollbar", "splitter", "toolbar", "badge", "progress", "meter"} {
		s.Controls[name] = cloneRecipe(dark)
	}
	paper := Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "rect"}, Fill: "surface", Stroke: "paper-edge", StrokeWidth: 1}}, ContentInsets: Insets{Top: 6, Right: 6, Bottom: 6, Left: 6}, TextColor: "text", States: map[string]StateStyle{"focused": {Stroke: "accent"}, "disabled": {Text: "disabled"}}}
	for _, name := range []string{"panel", "card", "dialog", "popover", "tooltip"} {
		s.Controls[name] = cloneRecipe(paper)
	}
	for _, name := range []string{"field", "text-area"} {
		r := cloneRecipe(paper)
		r.Layers[0].Fill, r.Layers[0].Stroke = "paper", "border"
		r.States["invalid"] = StateStyle{Stroke: "danger"}
		s.Controls[name] = r
	}
	s.Controls["slider"] = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "rect"}}}, TextColor: "text"}
	s.Controls["tab"] = Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "path", Points: []Point{{0, 1}, {0, .15}, {.05, 0}, {.95, 0}, {1, .15}, {1, 1}}}, Fill: "raised", Stroke: "chrome-shadow", StrokeWidth: 1, Material: "panel"},
		{Bounds: Box{X: .035, Y: .025, W: .93, H: .045}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-edge"},
	}, ContentInsets: Insets{Top: 2, Right: 5, Bottom: 2, Left: 5}, TextColor: "chrome-text", States: darkStates}
	for _, name := range []string{"slider-track", "switch-track"} {
		s.Controls[name] = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "rect"}, Fill: "chrome-slot", Stroke: "chrome-shadow", StrokeWidth: 1}}, TextColor: "chrome-text", States: darkStates}
	}
	for _, name := range []string{"slider-fill", "progress-fill", "scrollbar-thumb", "switch-thumb", "slider-thumb"} {
		s.Controls[name] = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "rect"}, Fill: "accent", Stroke: "chrome-edge", StrokeWidth: 1}}, TextColor: "chrome-text", States: map[string]StateStyle{"disabled": {Fill: "disabled"}}}
	}
	for _, name := range []string{"radio", "checkbox"} {
		r := s.Controls[name]
		r.Layers[0].Fill, r.Layers[0].Stroke, r.Layers[0].Material = "paper", "border", ""
		r.TextColor = "text"
		s.Controls[name] = r
	}
	s.Window.Layout = WindowLayout{
		TitleSide: "left", TitleWidth: 32, BottomHeight: 30, ScaleMode: "client-pixels",
		TitlebarHeight: 26, BorderWidth: 7, ButtonWidth: 24, ButtonHeight: 20, ButtonGap: 3, ResizeSize: 20,
		ButtonsSide: "right", ButtonOrder: []string{"minimize", "maximize", "close"},
	}
	// The lower edge steps upward near the right end, leaving a projecting
	// asymmetric rail instead of a uniform rectangular frame.
	outline := Geometry{Kind: "path", Points: []Point{{0, 0}, {1, 0}, {1, .962}, {.955, .962}, {.925, 1}, {.025, 1}, {0, .97}}}
	s.Window.Frame = Recipe{Layers: []Layer{
		{Geometry: outline, Fill: "chrome-raised", Stroke: "chrome-shadow", StrokeWidth: 1.8, Material: "chrome-bevel"},
		{Bounds: Box{X: .007, Y: .006, W: .986, H: .982}, Geometry: Geometry{Kind: "path", Points: []Point{{0, 0}, {1, 0}, {1, .965}, {.95, .965}, {.92, 1}, {0, 1}}}, Stroke: "chrome-edge", StrokeWidth: .8, Opacity: .7},
		{Bounds: Box{X: .034, Y: .981, W: .10, H: .011}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-mark"},
		{Bounds: Box{X: .153, Y: .981, W: .004, H: .011}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-mark"},
		{Bounds: Box{X: .174, Y: .981, W: .004, H: .011}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-mark"},
	}, TextColor: "chrome-text", States: map[string]StateStyle{"focused": {Stroke: "chrome-mark"}, "hovered": {Stroke: "chrome-edge"}}}
	s.Window.Titlebar = Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "path", Points: []Point{{0, 0}, {1, 0}, {1, 1}, {.58, 1}, {0, .86}}}, Fill: "chrome-base", Stroke: "chrome-shadow", StrokeWidth: 1.2, Material: "chrome-bevel"},
		{Bounds: Box{X: .08, Y: .035, W: .055, H: .74}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-edge", Opacity: .65},
	}, TextColor: "chrome-text", States: map[string]StateStyle{"hovered": {Stroke: "chrome-mark"}, "pressed": {Stroke: "chrome-mark"}}}
	s.Window.Grip = Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "rect"}, Fill: "chrome-slot", Stroke: "chrome-shadow", StrokeWidth: .7},
		{Bounds: Box{X: .18, Y: .2, W: .13, H: .6}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-mark"},
		{Bounds: Box{X: .44, Y: .2, W: .13, H: .6}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-mark"},
		{Bounds: Box{X: .70, Y: .2, W: .13, H: .6}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-mark"},
	}, TextColor: "chrome-text"}
	s.Window.Resize = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "path", Points: []Point{{0, 0}, {1, 0}, {1, .65}, {.65, 1}, {0, 1}}}, Fill: "chrome-slot", Stroke: "chrome-edge", StrokeWidth: .7}}, Icon: "resize", TextColor: "chrome-text"}
	for _, name := range []string{"minimize", "maximize", "close"} {
		r := cloneRecipe(dark)
		r.Icon = "window-" + name
		s.Window.Buttons[name] = r
	}
	s.Icons["window-minimize"] = Icon{Paths: []Path{path(Point{.18, .65}, Point{.82, .65})}, StrokeWidth: 1.8}
	s.Icons["window-maximize"] = Icon{Paths: []Path{path(Point{.18, .5}, Point{.82, .5}), path(Point{.5, .18}, Point{.5, .82})}, StrokeWidth: 1.6}
	s.Icons["window-close"] = Icon{Paths: []Path{path(Point{.23, .23}, Point{.77, .77}), path(Point{.23, .77}, Point{.77, .23})}, StrokeWidth: 1.8}
}

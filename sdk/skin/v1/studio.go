package skin

// These complete appearances are recipes, not renderer branches. The optional
// page/graph tokens also let applications color their own drawings consistently.
func configureAdvancedStudio(s *Skin) {
	s.Description = "Compact slate-blue instruments with matte metallic bevels and thin inset double frames."
	s.Desktop = Desktop{Chrome: "corner-tools", Backdrop: "quiet-gradient"}
	s.Palette = palette("#202C40", "#3A4860", "#52637D", "#657892", "#29384F", "#AEC2DE", "#C4AE80", "#7F90AA", "#DEE5EF", "#A6B3C7", "#536D91", "#64738A", "#91BBA9", "#DFC78D", "#DB9498")
	for token, color := range map[string]Color{
		"chrome-base": "#344158", "chrome-raised": "#61718A", "chrome-edge": "#A9B8CE", "chrome-shadow": "#111C2F", "chrome-text": "#E0E8F2",
		"page-background": "#303E55", "page-panel": "#3A4962", "page-inset": "#25344B", "page-edge": "#8191AA", "page-text": "#D8E0EB", "page-muted": "#ACBACD", "heading-text": "#E7EDF5",
		"graph-grid": "#455674", "graph-line": "#B0C7E1", "graph-fill": "#415675", "status-online": "#91BBA9", "status-alert": "#DFC78D",
		"desktop-background": "#152135", "desktop-glow": "#495C79",
	} {
		s.Palette[token] = color
	}
	s.Typography = Typography{Family: "Sans", Size: 13, LineHeight: 18, Weight: 400}
	s.Metrics = Metrics{Padding: 7, Gap: 5, ControlHeight: 28, Stroke: 1, Corner: 3, Notch: 5, Icon: 15}
	s.Materials = map[string]Material{
		"panel": {Kind: "linear-gradient", Secondary: "surface", Angle: 90},
		"metal": {Kind: "linear-gradient", Secondary: "chrome-base", Angle: 90},
	}
	states := studioStates()
	control := Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "chamfered", Corner: .10}, Fill: "raised", Stroke: "chrome-shadow", StrokeWidth: 1, Material: "panel"},
		{Bounds: Box{X: .012, Y: .06, W: .976, H: .86}, Geometry: Geometry{Kind: "chamfered", Corner: .075}, Stroke: "chrome-edge", StrokeWidth: .65, Opacity: .65},
	}, ContentInsets: Insets{Top: 3, Right: 7, Bottom: 3, Left: 7}, TextColor: "text", States: states}
	panel := Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "chamfered", Corner: .018}, Fill: "surface", Stroke: "chrome-shadow", StrokeWidth: 1.4},
		{Bounds: Box{X: .004, Y: .008, W: .992, H: .984}, Geometry: Geometry{Kind: "chamfered", Corner: .014}, Stroke: "border", StrokeWidth: .8},
	}, ContentInsets: Insets{Top: 7, Right: 7, Bottom: 7, Left: 7}, TextColor: "text", States: states}
	installStudioControls(s, control, panel, Geometry{Kind: "chamfered", Corner: .12})
	s.Window.Layout = WindowLayout{ScaleMode: "client-pixels", TitlebarHeight: 34, BorderWidth: 8, ButtonWidth: 34, ButtonHeight: 24, ButtonGap: 4, ResizeSize: 22, GripWidth: 28, ButtonsSide: "right", ButtonOrder: []string{"minimize", "maximize", "close"}}
	s.Window.Frame = Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "chamfered", Corner: .014}, Fill: "chrome-raised", Stroke: "chrome-shadow", StrokeWidth: 1.4, Material: "metal"},
		{Bounds: Box{X: .002, Y: .003, W: .996, H: .994}, Geometry: Geometry{Kind: "chamfered", Corner: .011}, Stroke: "chrome-edge", StrokeWidth: .8},
		{Bounds: Box{X: .005, Y: .007, W: .990, H: .986}, Geometry: Geometry{Kind: "chamfered", Corner: .008}, Stroke: "chrome-shadow", StrokeWidth: .75},
	}, TextColor: "chrome-text", States: map[string]StateStyle{"focused": {Stroke: "accent"}}}
	s.Window.Titlebar = Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "rect"}, Fill: "chrome-raised", Material: "metal"},
		{Bounds: Box{Y: .05, W: 1, H: .025}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-edge", Opacity: .65},
		{Bounds: Box{Y: .97, W: 1, H: .03}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-shadow"},
	}, TextColor: "chrome-text"}
	s.Window.Grip = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "chamfered", Corner: .13}, Fill: "chrome-base", Stroke: "chrome-shadow", StrokeWidth: .7}}, TextColor: "chrome-text"}
	for i := 0; i < 3; i++ {
		s.Window.Grip.Layers = append(s.Window.Grip.Layers, Layer{Bounds: Box{X: .2 + float64(i)*.23, Y: .25, W: .075, H: .5}, Geometry: Geometry{Kind: "rect"}, Fill: "chrome-edge"})
	}
	s.Window.Resize = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "chamfered", Corner: .15}, Fill: "chrome-base", Stroke: "border", StrokeWidth: .8}}, Icon: "resize", TextColor: "chrome-text"}
	for _, name := range []string{"minimize", "maximize", "close"} {
		r := cloneRecipe(control)
		r.Icon = "window-" + name
		s.Window.Buttons[name] = r
	}
	// Metallic caps and the overlapping restore squares distinguish this
	// system's controls from Hologram's open single-line symbols.
	s.Icons["window-minimize"] = Icon{Paths: []Path{path(Point{.2, .62}, Point{.8, .62}), path(Point{.2, .79}, Point{.8, .79})}, StrokeWidth: 1.5}
	s.Icons["window-maximize"] = Icon{Paths: []Path{{Points: []Point{{.18, .32}, {.69, .32}, {.69, .82}, {.18, .82}}, Closed: true}, path(Point{.35, .18}, Point{.83, .18}, Point{.83, .65})}, StrokeWidth: 1.3}
	s.Icons["window-close"] = Icon{Paths: []Path{path(Point{.24, .24}, Point{.76, .76}), path(Point{.24, .76}, Point{.76, .24}), path(Point{.1, .85}, Point{.9, .85})}, StrokeWidth: 1.5}
}

func configureHologramMonitor(s *Skin) {
	s.Description = "Near-black monitoring panels with thin cyan-violet double outlines and luminous line controls."
	s.Desktop = Desktop{Chrome: "corner-tools", Backdrop: "quiet-gradient"}
	s.Palette = palette("#010715", "#07101F", "#0B1B2D", "#10283B", "#061321", "#36E4F3", "#A873FF", "#24485F", "#E9F6FF", "#8092A8", "#10364C", "#3B5167", "#5FDEB3", "#FFE23B", "#FF769B")
	for token, color := range map[string]Color{
		"chrome-base": "#030B19", "chrome-edge": "#55DDEB", "chrome-shadow": "#01040B", "chrome-text": "#F0FAFF",
		"graph-grid": "#15213B", "graph-line": "#38E8F5", "graph-fill": "#082B41", "status-online": "#5FDEB3", "status-alert": "#FFE23B",
		"glow-cyan": "#3BE7F7", "glow-violet": "#A477FF", "desktop-background": "#01040D", "desktop-glow": "#0B1833",
	} {
		s.Palette[token] = color
	}
	s.Typography = Typography{Family: "Sans", Size: 15, LineHeight: 21, Weight: 400}
	s.Metrics = Metrics{Padding: 9, Gap: 7, ControlHeight: 32, Stroke: 1, Corner: 4, Notch: 6, Icon: 18}
	s.Materials = map[string]Material{
		"panel":              {Kind: "flat"},
		"edge":               {Kind: "emissive", Glow: .32},
		"violet-edge":        {Kind: "emissive", Glow: .32},
		"window-edge":        {Kind: "emissive", Glow: .42},
		"window-violet-edge": {Kind: "emissive", Glow: .42},
		"window-glass":       {Kind: "glass", Secondary: "chrome-base", Opacity: .97, Blur: .015, Refraction: .02},
	}
	states := studioStates()
	states["hovered"] = StateStyle{Fill: "hover", Stroke: "accent", Glow: .36}
	states["focused"] = StateStyle{Stroke: "accent", Glow: .3}
	control := Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "chamfered", Corner: .16}, Fill: "raised", Stroke: "border", StrokeWidth: .85},
		{Bounds: Box{X: .02, Y: .12, W: .96, H: .76}, Geometry: Geometry{Kind: "chamfered", Corner: .12}, Stroke: "accent", StrokeWidth: .55, Opacity: .52, Material: "edge"},
	}, ContentInsets: Insets{Top: 4, Right: 9, Bottom: 4, Left: 9}, TextColor: "text", States: states}
	panel := Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "chamfered", Corner: .025}, Fill: "surface", Stroke: "border", StrokeWidth: 1},
		{Bounds: Box{X: .005, Y: .006, W: .99, H: .988}, Geometry: Geometry{Kind: "chamfered", Corner: .022}, Stroke: "accent", StrokeWidth: .6, Opacity: .32, Material: "edge"},
	}, ContentInsets: Insets{Top: 9, Right: 9, Bottom: 9, Left: 9}, TextColor: "text", States: states}
	installStudioControls(s, control, panel, Geometry{Kind: "chamfered", Corner: .15})
	s.Window.Layout = WindowLayout{ScaleMode: "client-pixels", TitlebarHeight: 50, BorderWidth: 10, ButtonWidth: 54, ButtonHeight: 40, ButtonGap: 8, ResizeSize: 24, GripWidth: 26, ButtonsSide: "right", ButtonOrder: []string{"minimize", "maximize", "close"}}
	s.Window.Frame = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "chamfered", Corner: .018}, Fill: "chrome-base", Stroke: "border", StrokeWidth: .7, Material: "window-glass"}}, TextColor: "chrome-text", States: map[string]StateStyle{"focused": {Glow: .4}}}
	// Closing each half at the middle is harmless: the host removes the client
	// aperture. This keeps cyan left rails and violet right rails in editable
	// generic paths instead of adding a skin-specific shader or frame branch.
	left := Geometry{Kind: "path", Points: []Point{{.5, 0}, {.013, 0}, {0, .018}, {0, .982}, {.013, 1}, {.5, 1}}}
	right := Geometry{Kind: "path", Points: []Point{{.5, 0}, {.987, 0}, {1, .018}, {1, .982}, {.987, 1}, {.5, 1}}}
	for _, edge := range []struct {
		shape           Geometry
		color, material string
	}{{left, "glow-cyan", "window-edge"}, {right, "glow-violet", "window-violet-edge"}} {
		s.Window.Frame.Layers = append(s.Window.Frame.Layers,
			Layer{Geometry: edge.shape, Stroke: edge.color, StrokeWidth: 1.2, Material: edge.material},
			Layer{Bounds: Box{X: .003, Y: .0035, W: .994, H: .993}, Geometry: edge.shape, Stroke: edge.color, StrokeWidth: .7, Opacity: .72, Material: edge.material})
	}
	s.Window.Titlebar = Recipe{Layers: []Layer{
		{Geometry: Geometry{Kind: "rect"}, Fill: "chrome-base"},
		{Bounds: Box{Y: .98, W: 1, H: .015}, Geometry: Geometry{Kind: "rect"}, Fill: "border"},
	}, TextColor: "chrome-text"}
	s.Window.Grip = Recipe{Layers: []Layer{
		{Bounds: Box{X: .05, Y: .2, W: .72, H: .05}, Geometry: Geometry{Kind: "rect"}, Fill: "accent", Material: "edge"},
		{Bounds: Box{X: .05, Y: .47, W: .52, H: .05}, Geometry: Geometry{Kind: "rect"}, Fill: "accent", Material: "edge"},
		{Bounds: Box{X: .05, Y: .74, W: .32, H: .05}, Geometry: Geometry{Kind: "rect"}, Fill: "accent-alt", Material: "violet-edge"},
	}, TextColor: "chrome-text"}
	s.Window.Resize = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "chamfered", Corner: .22}, Fill: "chrome-base", Stroke: "accent-alt", StrokeWidth: .75, Material: "violet-edge"}}, Icon: "resize", TextColor: "accent-alt"}
	for _, name := range []string{"minimize", "maximize", "close"} {
		s.Window.Buttons[name] = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "chamfered", Corner: .1}, Fill: "chrome-base"}}, Icon: "window-" + name, TextColor: "chrome-text", States: map[string]StateStyle{"hovered": {Fill: "hover", Text: "accent", Glow: .25}, "pressed": {Fill: "pressed", Text: "accent-alt"}, "focused": {Text: "accent"}}}
	}
	s.Icons["window-minimize"] = Icon{Paths: []Path{path(Point{.08, .65}, Point{.92, .65})}, StrokeWidth: 1.8}
	s.Icons["window-maximize"] = Icon{Paths: []Path{{Points: []Point{{.10, .10}, {.90, .10}, {.90, .90}, {.10, .90}}, Closed: true}}, StrokeWidth: 1.7}
	s.Icons["window-close"] = Icon{Paths: []Path{path(Point{.14, .14}, Point{.86, .86}), path(Point{.86, .14}, Point{.14, .86})}, StrokeWidth: 1.8}
}

func studioStates() map[string]StateStyle {
	return map[string]StateStyle{"hovered": {Fill: "hover", Stroke: "accent"}, "pressed": {Fill: "pressed", Stroke: "accent", Offset: Point{Y: 1}}, "focused": {Stroke: "accent"}, "selected": {Fill: "selection", Stroke: "accent"}, "disabled": {Fill: "surface", Stroke: "disabled", Text: "disabled", Opacity: .65}, "invalid": {Stroke: "danger"}}
}

func installStudioControls(s *Skin, control, panel Recipe, geometry Geometry) {
	for _, name := range []string{"button", "icon-button", "switch", "tab", "segment", "menu-item", "list-row", "tree-row", "table-row", "scrollbar", "splitter", "toolbar", "badge", "progress", "meter"} {
		s.Controls[name] = cloneRecipe(control)
	}
	for _, name := range []string{"panel", "card", "dialog", "popover", "tooltip"} {
		s.Controls[name] = cloneRecipe(panel)
	}
	for _, name := range []string{"field", "text-area"} {
		r := cloneRecipe(control)
		r.Layers[0].Fill, r.Layers[0].Material = "background", ""
		s.Controls[name] = r
	}
	for _, name := range []string{"slider-track", "switch-track", "checkbox", "radio"} {
		r := Recipe{Layers: []Layer{{Geometry: geometry, Fill: "background", Stroke: "border", StrokeWidth: 1}}, TextColor: "text", States: studioStates()}
		if name == "radio" {
			r.Layers[0].Geometry = Geometry{Kind: "rounded", Radius: .5}
		}
		s.Controls[name] = r
	}
	for _, name := range []string{"slider-fill", "progress-fill", "scrollbar-thumb", "switch-thumb", "slider-thumb", "radio-indicator", "meter-fill"} {
		r := Recipe{Layers: []Layer{{Geometry: geometry, Fill: "accent"}}, TextColor: "background", States: map[string]StateStyle{"disabled": {Fill: "disabled"}, "hovered": {Fill: "accent-alt"}}}
		if name == "radio-indicator" {
			r.Layers[0].Geometry = Geometry{Kind: "rounded", Radius: .5}
		}
		if name == "meter-fill" {
			r.Layers[0].Fill = "success"
			r.States["selected"] = StateStyle{Fill: "warning"}
			r.States["invalid"] = StateStyle{Fill: "danger"}
		}
		s.Controls[name] = r
	}
	s.Controls["slider"] = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "rect"}}}, TextColor: "text"}
}

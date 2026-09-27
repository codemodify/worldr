package skin

import "fmt"

// Builtins returns independent packages for the complete appearance systems.
// The older identifiers remain available through Builtin for saved preferences.
func Builtins() []Skin {
	result := make([]Skin, 0, 5)
	for _, id := range []string{"merrick", "advanced", "hologram", "plasma", "future-panels"} {
		s, _ := Builtin(id)
		result = append(result, s)
	}
	return result
}

func Builtin(id string) (Skin, error) {
	if id == "future-panels" {
		return futurePanels()
	}
	base := id
	switch id {
	case "instrument":
		base = "advanced"
	case "aperture":
		base = "hologram"
	case "glass":
		base = "plasma"
	case "telemetry":
		base = "advanced"
	}
	if base != "merrick" && base != "advanced" && base != "hologram" && base != "plasma" {
		return Skin{}, fmt.Errorf("skin: unknown built-in %q", id)
	}
	s := Skin{Version: Version, ID: id, Typography: Typography{Family: "Sans", Size: 14, LineHeight: 20, Weight: 400}, Metrics: Metrics{Padding: 10, Gap: 8, ControlHeight: 36, Stroke: 1, Corner: 5, Notch: 7, Icon: 18}, Controls: map[string]Recipe{}, Icons: standardIcons(), Materials: map[string]Material{}}
	var geometry Geometry
	switch base {
	case "merrick":
		s.Name = "Merrick"
		s.Description = "Slate desk panels, indexed tabs and quiet ivory controls."
		s.Palette = palette("#131a20", "#293640", "#394953", "#445764", "#1d2932", "#d4e1e4", "#9fb8c5", "#637982", "#f1f3ed", "#a2b2b9", "#516877", "#65737b", "#8fc5ac", "#d6b774", "#e58b82")
		geometry = Geometry{Kind: "rect"}
		s.Materials["panel"] = Material{Kind: "linear-gradient", Secondary: "surface", Angle: 90}
		s.Window.Layout = WindowLayout{TitlebarHeight: 52, BorderWidth: 10, ButtonWidth: 58, ButtonHeight: 36, ButtonGap: 4, ResizeSize: 36, ButtonsSide: "right", ButtonOrder: []string{"minimize", "maximize", "close"}}
	case "advanced":
		s.Name = "Advanced"
		s.Description = "Dense steel-blue instrument panels with stepped plates and inset edges."
		s.Palette = palette("#0c1720", "#1b3545", "#35596b", "#416f81", "#102b39", "#93d6e5", "#d7ac61", "#577e8e", "#e4f0f2", "#8eacb7", "#315c70", "#526775", "#78c1a7", "#e6bc70", "#e8867f")
		s.Typography = Typography{Family: "Monospace", Size: 13, LineHeight: 18, Weight: 400}
		s.Metrics = Metrics{Padding: 7, Gap: 5, ControlHeight: 30, Stroke: 1, Corner: 6, Notch: 9, Icon: 16}
		geometry = Geometry{Kind: "chamfered", Corner: .18}
		s.Materials["panel"] = Material{Kind: "linear-gradient", Secondary: "surface", Angle: 90}
		s.Window.Layout = WindowLayout{TitlebarHeight: 54, BorderWidth: 14, ButtonWidth: 60, ButtonHeight: 38, ButtonGap: 3, ResizeSize: 40, ButtonsSide: "right", ButtonOrder: []string{"minimize", "maximize", "close"}}
	case "hologram":
		s.Name = "Hologram"
		s.Description = "Open cyan rails, violet signal edges and luminous translucent layers."
		s.Palette = palette("#07101de8", "#102333c4", "#16374bd8", "#214e65e6", "#174155f2", "#5af3ff", "#ba85ff", "#398aab", "#e5fcff", "#80b4c9", "#25506ccf", "#425b70", "#67f1c0", "#ffd382", "#ff809e")
		s.Typography = Typography{Family: "Monospace", Size: 14, LineHeight: 20, Weight: 400}
		s.Metrics = Metrics{Padding: 11, Gap: 9, ControlHeight: 38, Stroke: 1, Corner: 3, Notch: 10, Icon: 18}
		geometry = Geometry{Kind: "bracketed", Corner: .12}
		s.Materials["panel"] = Material{Kind: "glass", Secondary: "surface", Opacity: .86, Glow: .4, Blur: .14, Refraction: .12}
		s.Materials["edge"] = Material{Kind: "emissive", Glow: 1.2}
		s.Window.Layout = WindowLayout{TitlebarHeight: 58, BorderWidth: 9, ButtonWidth: 60, ButtonHeight: 40, ButtonGap: 8, ResizeSize: 38, ButtonsSide: "right", ButtonOrder: []string{"minimize", "maximize", "close"}}
	case "plasma":
		s.Name = "Plasma"
		s.Description = "Rounded blue cards, warm action accents and spacious, soft controls."
		s.Palette = palette("#101728", "#1c2941", "#2c3d59", "#385274", "#152740", "#83baff", "#ffb782", "#4d6384", "#f1f5ff", "#9fadc6", "#344e74", "#596980", "#8cd5ac", "#ffd185", "#ff929c")
		s.Typography = Typography{Family: "Sans", Size: 16, LineHeight: 23, Weight: 400}
		s.Metrics = Metrics{Padding: 13, Gap: 10, ControlHeight: 42, Stroke: 1, Corner: 14, Notch: 0, Icon: 20}
		geometry = Geometry{Kind: "rounded", Radius: .36}
		s.Materials["panel"] = Material{Kind: "linear-gradient", Secondary: "surface", Angle: 70}
		s.Window.Layout = WindowLayout{TitlebarHeight: 60, BorderWidth: 12, ButtonWidth: 54, ButtonHeight: 38, ButtonGap: 9, ResizeSize: 38, ButtonsSide: "left", ButtonOrder: []string{"close", "minimize", "maximize"}}
	}
	states := map[string]StateStyle{"hovered": {Fill: "hover", Stroke: "accent"}, "pressed": {Fill: "pressed", Stroke: "accent", Offset: Point{Y: 1}}, "focused": {Stroke: "accent"}, "selected": {Fill: "selection", Stroke: "accent"}, "disabled": {Fill: "surface", Stroke: "disabled", Text: "disabled", Opacity: .65}, "invalid": {Stroke: "danger"}}
	control := Recipe{Layers: []Layer{{Geometry: geometry, Fill: "raised", Stroke: "border", StrokeWidth: 1, Material: "panel"}}, ContentInsets: Insets{Top: s.Metrics.Padding / 2, Right: s.Metrics.Padding, Bottom: s.Metrics.Padding / 2, Left: s.Metrics.Padding}, TextColor: "text", States: states}
	if base == "advanced" {
		control.Layers = append(control.Layers, Layer{Bounds: Box{X: .025, Y: .1, W: .95, H: .8}, Geometry: Geometry{Kind: "chamfered", Corner: .12}, Stroke: "border", StrokeWidth: 1, Opacity: .65}, Layer{Bounds: Box{X: .07, Y: 0, W: .18, H: .07}, Geometry: Geometry{Kind: "rect"}, Fill: "accent-alt"})
	} else if base == "merrick" {
		control.Layers = append(control.Layers, Layer{Bounds: Box{X: .015, Y: 0, W: .97, H: .035}, Geometry: Geometry{Kind: "rect"}, Fill: "text", Opacity: .3})
	} else if base == "hologram" {
		control.Layers = append(control.Layers, Layer{Bounds: Box{X: .04, Y: .9, W: .3, H: .045}, Geometry: Geometry{Kind: "rect"}, Fill: "accent-alt", Material: "edge"})
	}
	for _, name := range []string{"button", "icon-button", "field", "text-area", "switch", "checkbox", "radio", "slider", "progress", "meter", "tab", "segment", "menu-item", "list-row", "tree-row", "table-row", "scrollbar", "splitter", "panel", "card", "toolbar", "dialog", "popover", "tooltip", "badge"} {
		s.Controls[name] = cloneRecipe(control)
	}
	for _, name := range []string{"field", "text-area"} {
		r := s.Controls[name]
		r.Layers[0].Fill = "background"
		r.Layers[0].Material = ""
		s.Controls[name] = r
	}
	for _, name := range []string{"panel", "card", "dialog", "popover", "toolbar"} {
		r := s.Controls[name]
		r.Layers[0].Fill = "surface"
		r.ContentInsets = Insets{Top: s.Metrics.Padding, Right: s.Metrics.Padding, Bottom: s.Metrics.Padding, Left: s.Metrics.Padding}
		for i := range r.Layers {
			if i > 0 && name != "toolbar" && r.Layers[i].Bounds.H > 0 && r.Layers[i].Bounds.H < .1 {
				r.Layers[i].Bounds.H = .008
				if r.Layers[i].Bounds.Y >= .9 {
					r.Layers[i].Bounds.Y, r.Layers[i].Bounds.H = .986, .006
				}
			}
			g := &r.Layers[i].Geometry
			if g.Kind == "rounded" {
				g.Radius = .045
				if name == "toolbar" {
					g.Radius = .2
				}
			}
			if g.Kind == "chamfered" || g.Kind == "bracketed" {
				g.Corner = .025
			}
		}
		s.Controls[name] = r
	}
	if base == "plasma" {
		for _, name := range []string{"field", "text-area", "list-row", "tree-row", "table-row", "menu-item"} {
			r := s.Controls[name]
			r.Layers[0].Geometry.Radius = .2
			s.Controls[name] = r
		}
	}
	if base == "merrick" {
		r := s.Controls["tab"]
		r.Layers[0].Geometry = Geometry{Kind: "path", Points: []Point{{0, 1}, {0, .22}, {.09, 0}, {.91, 0}, {1, .22}, {1, 1}}}
		s.Controls["tab"] = r
	}
	// Behavior-independent subparts let a skin change control silhouettes and
	// indicator materials without requiring another widget implementation.
	part := func(g Geometry, fill, stroke string) Recipe {
		return Recipe{Layers: []Layer{{Geometry: g, Fill: fill, Stroke: stroke, StrokeWidth: 1}}, TextColor: "text", States: map[string]StateStyle{"disabled": {Fill: "disabled", Stroke: "disabled", Opacity: .65}, "focused": {Stroke: "accent"}, "hovered": {Stroke: "accent"}, "selected": {Stroke: "accent"}}}
	}
	s.Controls["switch-track"] = part(geometry, "background", "border")
	s.Controls["switch-thumb"] = part(geometry, "muted", "")
	r := s.Controls["switch-thumb"]
	r.States["selected"] = StateStyle{Fill: "accent"}
	s.Controls["switch-thumb"] = r
	r = s.Controls["switch-track"]
	r.States["selected"] = StateStyle{Fill: "selection", Stroke: "accent"}
	s.Controls["switch-track"] = r
	s.Controls["slider-track"] = part(Geometry{Kind: "rounded", Radius: .5}, "border", "")
	s.Controls["slider-fill"] = part(Geometry{Kind: "rounded", Radius: .5}, "accent", "")
	s.Controls["slider-thumb"] = part(geometry, "accent", "")
	s.Controls["radio"] = part(Geometry{Kind: "rounded", Radius: .5}, "background", "border")
	s.Controls["radio-indicator"] = part(Geometry{Kind: "rounded", Radius: .5}, "accent", "")
	s.Controls["checkbox"] = part(geometry, "background", "border")
	s.Controls["checkbox-indicator"] = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "rect"}}}, TextColor: "accent", Icon: "check", States: map[string]StateStyle{"disabled": {Text: "disabled"}}}
	s.Controls["progress-fill"] = part(geometry, "accent", "")
	r = s.Controls["progress-fill"]
	r.TextColor = "background"
	s.Controls["progress-fill"] = r
	s.Controls["meter-fill"] = part(geometry, "success", "")
	r = s.Controls["meter-fill"]
	r.TextColor = "background"
	r.States["selected"] = StateStyle{Fill: "warning"}
	r.States["invalid"] = StateStyle{Fill: "danger"}
	s.Controls["meter-fill"] = r
	s.Controls["scrollbar-thumb"] = part(geometry, "accent", "")
	if base == "plasma" {
		for _, name := range []string{"switch-track", "switch-thumb", "slider-thumb"} {
			r := s.Controls[name]
			r.Layers[0].Geometry = Geometry{Kind: "rounded", Radius: .5}
			s.Controls[name] = r
		}
	}
	s.Window.Frame = cloneRecipe(s.Controls["panel"])
	// Window frames use full-window dimensions, unlike compact control faces.
	// Their corner radii must leave an unobstructed rectangular client aperture.
	for i := range s.Window.Frame.Layers {
		g := &s.Window.Frame.Layers[i].Geometry
		if g.Kind == "rounded" {
			g.Radius = .025
		}
		if g.Kind == "chamfered" {
			g.Corner = .025
		}
		if g.Kind == "bracketed" {
			g.Corner = .012
		}
	}
	s.Window.Titlebar = cloneRecipe(control)
	s.Window.Titlebar.Layers[0].Fill = "raised"
	s.Window.Grip = Recipe{Layers: []Layer{{Geometry: Geometry{Kind: "rect"}, Fill: "border"}, {Bounds: Box{X: .15, Y: .35, W: .7, H: .15}, Geometry: Geometry{Kind: "rect"}, Fill: "accent"}}, TextColor: "text"}
	switch base {
	case "merrick":
		s.Window.Grip.Layers = []Layer{{Geometry: Geometry{Kind: "rect"}, Fill: "surface"}, {Bounds: Box{X: .08, Y: .2, W: .23, H: .65}, Geometry: Geometry{Kind: "rect"}, Fill: "border"}, {Bounds: Box{X: .385, Y: .2, W: .23, H: .65}, Geometry: Geometry{Kind: "rect"}, Fill: "accent"}, {Bounds: Box{X: .69, Y: .2, W: .23, H: .65}, Geometry: Geometry{Kind: "rect"}, Fill: "border"}}
	case "advanced":
		s.Window.Grip.Layers = []Layer{{Geometry: Geometry{Kind: "chamfered", Corner: .2}, Fill: "surface", Stroke: "border", StrokeWidth: 1}}
		for i := 0; i < 7; i++ {
			s.Window.Grip.Layers = append(s.Window.Grip.Layers, Layer{Bounds: Box{X: .12 + float64(i)*.115, Y: .24, W: .035, H: .52}, Geometry: Geometry{Kind: "rect"}, Fill: "accent"})
		}
	case "hologram":
		s.Window.Grip.Layers = []Layer{{Geometry: Geometry{Kind: "bracketed", Corner: .15}, Fill: "background", Stroke: "accent", StrokeWidth: 1, Material: "edge"}, {Bounds: Box{X: .2, Y: .43, W: .6, H: .06}, Geometry: Geometry{Kind: "rect"}, Fill: "accent-alt", Material: "edge"}}
	case "plasma":
		s.Window.Grip.Layers = []Layer{{Geometry: Geometry{Kind: "rounded", Radius: .5}, Fill: "raised"}, {Bounds: Box{X: .2, Y: .35, W: .6, H: .3}, Geometry: Geometry{Kind: "rounded", Radius: .5}, Fill: "muted"}}
		s.Materials["window-glass"] = Material{Kind: "glass", Secondary: "surface", Opacity: .84, Blur: .2, Refraction: .08}
		s.Window.Frame.Layers[0].Material = "window-glass"
		s.Window.Titlebar.Layers[0].Material = "window-glass"
	}
	s.Window.Resize = cloneRecipe(control)
	s.Window.Resize.Icon = "resize"
	s.Window.Buttons = map[string]Recipe{}
	for _, name := range []string{"minimize", "maximize", "close"} {
		r := cloneRecipe(control)
		r.Icon = "window-" + name
		s.Window.Buttons[name] = r
	}
	installWindowIcons(&s, base)
	if base == "plasma" {
		r := s.Window.Buttons["close"]
		r.Layers[0].Fill = "accent-alt"
		r.TextColor = "background"
		for _, name := range []string{"selected", "hovered", "pressed"} {
			state := r.States[name]
			state.Text = "text"
			r.States[name] = state
		}
		s.Window.Buttons["close"] = r
	}
	if id != base {
		s.Name = map[string]string{"instrument": "Instrument", "aperture": "Aperture", "glass": "Glass", "telemetry": "Telemetry"}[id]
		s.Description = "Compatibility appearance for the saved " + id + " preference."
	}
	switch base {
	case "merrick":
		configureMerrickDesk(&s)
	case "advanced":
		configureAdvancedStudio(&s)
	case "hologram":
		configureHologramMonitor(&s)
	}
	if err := s.Validate(); err != nil {
		return Skin{}, err
	}
	return s, nil
}

func palette(values ...string) map[string]Color {
	names := []string{"background", "surface", "raised", "hover", "pressed", "accent", "accent-alt", "border", "text", "muted", "selection", "disabled", "success", "warning", "danger"}
	p := make(map[string]Color, len(names))
	for i, n := range names {
		p[n] = Color(values[i])
	}
	return p
}
func cloneRecipe(r Recipe) Recipe {
	r.Layers = append([]Layer(nil), r.Layers...)
	for i := range r.Layers {
		r.Layers[i].Geometry.Points = append([]Point(nil), r.Layers[i].Geometry.Points...)
	}
	if r.States != nil {
		copy := make(map[string]StateStyle, len(r.States))
		for k, v := range r.States {
			copy[k] = v
		}
		r.States = copy
	}
	return r
}
func path(points ...Point) Path { return Path{Points: points} }
func glyph(paths ...Path) Icon  { return Icon{Paths: paths, StrokeWidth: 1.5} }
func standardIcons() map[string]Icon {
	return map[string]Icon{
		"add":           glyph(path(Point{.2, .5}, Point{.8, .5}), path(Point{.5, .2}, Point{.5, .8})),
		"remove":        glyph(path(Point{.2, .5}, Point{.8, .5})),
		"close":         glyph(path(Point{.22, .22}, Point{.78, .78}), path(Point{.78, .22}, Point{.22, .78})),
		"check":         glyph(path(Point{.15, .5}, Point{.4, .75}, Point{.85, .2})),
		"play":          glyph(Path{Points: []Point{{.25, .15}, {.85, .5}, {.25, .85}}, Closed: true, Fill: true}),
		"pause":         glyph(path(Point{.32, .18}, Point{.32, .82}), path(Point{.68, .18}, Point{.68, .82})),
		"stop":          glyph(Path{Points: []Point{{.23, .23}, {.77, .23}, {.77, .77}, {.23, .77}}, Closed: true, Fill: true}),
		"back":          glyph(path(Point{.72, .18}, Point{.3, .5}, Point{.72, .82}), path(Point{.25, .18}, Point{.25, .82})),
		"forward":       glyph(path(Point{.28, .18}, Point{.7, .5}, Point{.28, .82}), path(Point{.75, .18}, Point{.75, .82})),
		"chevron-right": glyph(path(Point{.3, .18}, Point{.72, .5}, Point{.3, .82})),
		"search":        glyph(Path{Points: []Point{{.2, .2}, {.55, .15}, {.7, .4}, {.55, .65}, {.2, .6}, {.1, .4}}, Closed: true}, path(Point{.58, .6}, Point{.88, .9})),
		"settings":      glyph(Path{Points: []Point{{.3, .12}, {.7, .12}, {.9, .5}, {.7, .88}, {.3, .88}, {.1, .5}}, Closed: true}, Path{Points: []Point{{.4, .35}, {.6, .35}, {.65, .5}, {.6, .65}, {.4, .65}, {.35, .5}}, Closed: true}),
		"resize":        glyph(path(Point{.2, .8}, Point{.8, .2}), path(Point{.5, .8}, Point{.8, .5})),
	}
}
func installWindowIcons(s *Skin, style string) {
	min := glyph(path(Point{.2, .68}, Point{.8, .68}))
	max := glyph(Path{Points: []Point{{.22, .22}, {.78, .22}, {.78, .78}, {.22, .78}}, Closed: true})
	close := s.Icons["close"]
	switch style {
	case "merrick":
		min = glyph(Path{Points: []Point{{.18, .57}, {.82, .57}, {.82, .72}, {.18, .72}}, Closed: true, Fill: true})
		max = glyph(path(Point{.2, .7}, Point{.2, .24}, Point{.78, .24}, Point{.78, .7}), path(Point{.2, .38}, Point{.78, .38}))
		close = glyph(path(Point{.25, .22}, Point{.75, .78}), path(Point{.75, .22}, Point{.25, .78}), path(Point{.18, .85}, Point{.82, .85}))
	case "advanced":
		min = glyph(path(Point{.18, .58}, Point{.35, .75}, Point{.82, .75}), path(Point{.3, .42}, Point{.78, .42}))
		max = glyph(Path{Points: []Point{{.22, .34}, {.34, .22}, {.78, .22}, {.78, .66}, {.66, .78}, {.22, .78}}, Closed: true}, path(Point{.38, .38}, Point{.62, .38}, Point{.62, .62}))
		close = glyph(path(Point{.2, .18}, Point{.45, .45}, Point{.2, .78}), path(Point{.8, .18}, Point{.55, .45}, Point{.8, .78}), path(Point{.4, .85}, Point{.6, .85}))
	case "hologram":
		min = glyph(path(Point{.2, .42}, Point{.5, .7}, Point{.8, .42}), path(Point{.32, .85}, Point{.68, .85}))
		max = glyph(path(Point{.15, .4}, Point{.15, .15}, Point{.4, .15}), path(Point{.6, .15}, Point{.85, .15}, Point{.85, .4}), path(Point{.85, .6}, Point{.85, .85}, Point{.6, .85}), path(Point{.4, .85}, Point{.15, .85}, Point{.15, .6}))
		close = glyph(path(Point{.15, .15}, Point{.37, .37}), path(Point{.63, .63}, Point{.85, .85}), path(Point{.85, .15}, Point{.63, .37}), path(Point{.37, .63}, Point{.15, .85}), Path{Points: []Point{{.5, .4}, {.6, .5}, {.5, .6}, {.4, .5}}, Closed: true, Fill: true})
	case "plasma":
		min = glyph(path(Point{.25, .5}, Point{.75, .5}))
		min.StrokeWidth = 2.5
		max = glyph(path(Point{.22, .48}, Point{.22, .22}, Point{.48, .22}), path(Point{.52, .78}, Point{.78, .78}, Point{.78, .52}), path(Point{.25, .25}, Point{.75, .75}))
		max.StrokeWidth = 2
		close.StrokeWidth = 2.5
	}
	s.Icons["window-minimize"], s.Icons["window-maximize"], s.Icons["window-close"] = min, max, close
}

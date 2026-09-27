package workspace

import (
	"math"
	"sort"

	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
)

// Theme selector geometry is kept out of settings.go so the visual catalog can
// grow without coupling the modal's input router to every preview primitive.
var (
	settingsThemeInstrument = box{452, 247, 182, 54}
	settingsThemeAperture   = box{654, 247, 182, 54}
	settingsThemeGlass      = box{856, 247, 182, 54}
	settingsThemeTelemetry  = box{1060, 247, 182, 54}

	settingsShapeChamfered = box{452, 326, 182, 48}
	settingsShapeBracketed = box{654, 326, 182, 48}
	settingsShapeSlab      = box{856, 326, 182, 48}
	settingsShapeNotched   = box{1060, 326, 182, 48}
)

type controlThemePalette struct {
	background, surface, hover, accent, secondary, text, muted uint32
	corner, notch                                              float32
}

func paletteForControlTheme(family controlThemeFamily) controlThemePalette {
	theme, err := nativeui.Builtin(nativeui.Family(family), nativeui.Chamfered)
	if err != nil {
		theme = nativeui.DefaultTheme()
	}
	rgb := func(red, green, blue uint8) uint32 { return uint32(red)<<16 | uint32(green)<<8 | uint32(blue) }
	p := theme.Palette
	return controlThemePalette{
		background: rgb(p.Background.R, p.Background.G, p.Background.B),
		surface:    rgb(p.Surface.R, p.Surface.G, p.Surface.B),
		hover:      rgb(p.Hover.R, p.Hover.G, p.Hover.B),
		accent:     rgb(p.Accent.R, p.Accent.G, p.Accent.B),
		secondary:  rgb(p.AccentAlt.R, p.AccentAlt.G, p.AccentAlt.B),
		text:       rgb(p.Text.R, p.Text.G, p.Text.B),
		muted:      rgb(p.Muted.R, p.Muted.G, p.Muted.B),
		corner:     float32(theme.Metrics.Corner),
		notch:      float32(theme.Metrics.Notch),
	}
}

func (w *Workspace) drawControlThemeSettingsPreview() {
	w.drawSettingsPreviewHeader("THEMES", "NATIVE CONTROL SYSTEM", "SELECT + PREVIEW SDK CONTROLS")
	w.drawPreviewFrame(settingsPreviewBounds)
	w.text(452, 231, 9, "CONTROL THEME", muted, .82)
	familyBounds := [...]box{settingsThemeInstrument, settingsThemeAperture, settingsThemeGlass, settingsThemeTelemetry}
	selected := w.controlTheme.normalized()
	for i, choice := range controlThemeChoices {
		w.drawControlThemeChoice(familyBounds[i], choice, choice.family == selected.Family)
	}

	w.text(452, 310, 9, "SHAPE GRAMMAR", muted, .82)
	shapeBounds := [...]box{settingsShapeChamfered, settingsShapeBracketed, settingsShapeSlab, settingsShapeNotched}
	for i, choice := range controlShapeChoices {
		w.drawControlShapeChoice(shapeBounds[i], choice, choice.shape == selected.Shape, paletteForControlTheme(selected.Family))
	}
	w.drawControlGallery(box{452, 394, 790, 238}, selected)
}

func (w *Workspace) drawControlThemeChoice(b box, choice controlThemeChoice, selected bool) {
	p := paletteForControlTheme(choice.family)
	alpha := float32(.68)
	if selected {
		alpha = .96
	}
	w.rect(b.x, b.y, b.w, b.h, p.background, .98)
	w.rect(b.x, b.y, 4, b.h, p.accent, alpha)
	w.line(b.x, b.y, b.x+b.w, b.y, 1.2, p.accent, alpha)
	if selected {
		w.line(b.x, b.y+b.h, b.x+b.w, b.y+b.h, 1.2, p.accent, .72)
	}
	w.text(b.x+14, b.y+11, 11, choice.title, p.text, 1)
	w.text(b.x+14, b.y+32, 7, choice.description, p.muted, .84)
}

func (w *Workspace) drawControlShapeChoice(b box, choice controlShapeChoice, selected bool, p controlThemePalette) {
	w.rect(b.x, b.y, b.w, b.h, p.background, .96)
	w.drawControlShape(box{b.x + 9, b.y + 8, 42, 31}, choice.shape, p, selected)
	color := p.muted
	if selected {
		color = p.text
	}
	w.text(b.x+61, b.y+8, 10, choice.title, color, 1)
	w.text(b.x+61, b.y+26, 7, choice.description, p.muted, .82)
}

func (w *Workspace) drawControlShape(b box, shape controlShape, p controlThemePalette, active bool) {
	alpha := float32(.45)
	if active {
		alpha = .96
	}
	points := controlShapePolygon(b, shape, p)
	fillAlpha := float32(.42)
	if active {
		fillAlpha = .62
	}
	w.fillControlShapePolygon(points, p.surface, fillAlpha)

	if shape == controlShapeBracketed {
		// Match nativeui.StrokeShape: the body remains filled while four open
		// corner rails describe the asymmetric bracketed silhouette.
		corner := min(p.corner, max(float32(0), min(b.w, b.h)/3))
		reachX, reachY := max(float32(3), b.w/4), max(float32(3), b.h/3)
		x0, y0, x1, y1 := b.x, b.y, b.x+b.w, b.y+b.h
		for _, edge := range [][4]float32{
			{x0, y0 + min(corner, reachY), x0, y0 + reachY},
			{x0, y0 + min(corner, reachY), x0 + min(corner, reachX), y0},
			{x0 + min(corner, reachX), y0, x0 + reachX, y0},
			{x1 - reachX, y0, x1, y0}, {x1, y0, x1, y0 + reachY},
			{x0, y1 - reachY, x0, y1}, {x0, y1, x0 + reachX, y1},
			{x1 - reachX, y1, x1 - min(corner, reachX), y1},
			{x1 - min(corner, reachX), y1, x1, y1 - min(corner, reachY)},
			{x1, y1 - min(corner, reachY), x1, y1 - reachY},
		} {
			w.line(edge[0], edge[1], edge[2], edge[3], 1.2, p.accent, alpha)
		}
		return
	}
	for index, point := range points {
		next := points[(index+1)%len(points)]
		w.line(point.x, point.y, next.x, next.y, 1.1, p.accent, alpha)
	}
}

type controlShapePoint struct{ x, y float32 }

// controlShapePolygon mirrors sdk/nativeui/v1.shapePoints in workspace design
// coordinates. Keeping this small representation local avoids exporting painter
// internals while making the settings preview faithful to the SDK geometry.
func controlShapePolygon(b box, shape controlShape, p controlThemePalette) []controlShapePoint {
	x0, y0, x1, y1 := b.x, b.y, b.x+b.w, b.y+b.h
	corner := min(p.corner, max(float32(0), min(b.w, b.h)/3))
	notch := min(p.notch, max(float32(0), min(b.w/4, b.h/3)))
	switch shape {
	case controlShapeSlab:
		return []controlShapePoint{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
	case controlShapeBracketed:
		return []controlShapePoint{{x0 + corner, y0}, {x1, y0}, {x1, y1 - corner}, {x1 - corner, y1}, {x0, y1}, {x0, y0 + corner}}
	case controlShapeNotched:
		mid := y0 + b.h/2
		return []controlShapePoint{{x0 + corner, y0}, {x1 - corner, y0}, {x1, y0 + corner}, {x1, mid - notch}, {x1 - notch, mid}, {x1, mid + notch}, {x1, y1 - corner}, {x1 - corner, y1}, {x0 + corner, y1}, {x0, y1 - corner}, {x0, y0 + corner}}
	default:
		return []controlShapePoint{{x0 + corner, y0}, {x1 - corner, y0}, {x1, y0 + corner}, {x1, y1 - corner}, {x1 - corner, y1}, {x0 + corner, y1}, {x0, y1 - corner}, {x0, y0 + corner}}
	}
}

func (w *Workspace) fillControlShapePolygon(points []controlShapePoint, rgb uint32, alpha float32) {
	if len(points) < 3 || alpha <= 0 {
		return
	}
	minimumY, maximumY := points[0].y, points[0].y
	for _, point := range points[1:] {
		minimumY, maximumY = min(minimumY, point.y), max(maximumY, point.y)
	}
	intersections := make([]float32, 0, len(points))
	for row := int(math.Floor(float64(minimumY))); row < int(math.Ceil(float64(maximumY))); row++ {
		intersections = intersections[:0]
		scanY := float32(row) + .5
		for index, a := range points {
			b := points[(index+1)%len(points)]
			if a.y == b.y || scanY < min(a.y, b.y) || scanY >= max(a.y, b.y) {
				continue
			}
			x := a.x + (scanY-a.y)*(b.x-a.x)/(b.y-a.y)
			intersections = append(intersections, x)
		}
		sort.Slice(intersections, func(i, j int) bool { return intersections[i] < intersections[j] })
		for index := 0; index+1 < len(intersections); index += 2 {
			left, right := intersections[index], intersections[index+1]
			if right > left {
				w.rect(left, float32(row), right-left, 1, rgb, alpha)
			}
		}
	}
}

// The gallery deliberately shows stateful controls rather than decorative
// mock telemetry. It is the design contract shared with the public native UI
// toolkit and gives each family/shape combination a useful comparison surface.
func (w *Workspace) drawControlGallery(b box, settings controlThemeSettings) {
	p := paletteForControlTheme(settings.Family)
	w.rect(b.x, b.y, b.w, b.h, p.background, .985)
	w.line(b.x, b.y, b.x+b.w, b.y, 1.3, p.accent, .55)
	w.text(b.x+15, b.y+12, 9, "LIVE CONTROL GALLERY", p.muted, .9)

	button := box{b.x + 15, b.y + 38, 154, 40}
	w.drawControlShape(button, settings.Shape, p, true)
	w.text(button.x+31, button.y+13, 11, "RUN ANALYSIS", p.text, 1)

	field := box{b.x + 187, b.y + 38, 224, 40}
	w.rect(field.x, field.y, field.w, field.h, p.surface, .72)
	w.line(field.x, field.y+field.h, field.x+field.w, field.y+field.h, 1.5, p.accent, .86)
	w.text(field.x+13, field.y+13, 10, "Search instruments", p.muted, .9)
	w.line(field.x+field.w-22, field.y+11, field.x+field.w-22, field.y+29, 1.1, p.accent, .72)

	toggle := box{b.x + 430, b.y + 42, 58, 30}
	w.rect(toggle.x, toggle.y, toggle.w, toggle.h, p.accent, .2)
	w.circle(toggle.x+toggle.w-15, toggle.y+15, 8, 8, p.accent, .96)
	w.text(toggle.x+70, toggle.y+8, 9, "LINKED", p.text, 1)

	checkX, checkY := b.x+568, b.y+46
	w.drawControlShape(box{checkX, checkY, 22, 22}, settings.Shape, p, true)
	w.line(checkX+5, checkY+11, checkX+10, checkY+16, 1.5, p.accent, 1)
	w.line(checkX+10, checkY+16, checkX+18, checkY+6, 1.5, p.accent, 1)
	w.circle(b.x+690, checkY+11, 11, 1.2, p.accent, .9)
	w.circle(b.x+690, checkY+11, 4, 4, p.accent, .92)

	trackX, trackY, trackW := b.x+15, b.y+103, float32(302)
	w.line(trackX, trackY, trackX+trackW, trackY, 3, p.muted, .24)
	w.line(trackX, trackY, trackX+trackW*.62, trackY, 3, p.accent, .9)
	w.circle(trackX+trackW*.62, trackY, 5, 5, p.accent, 1)
	w.text(trackX, trackY+14, 8, "SLIDER  62", p.muted, .9)

	progressX := b.x + 342
	w.rect(progressX, trackY-3, 244, 7, p.surface, .9)
	w.rect(progressX, trackY-3, 174, 7, p.secondary, .82)
	w.text(progressX, trackY+14, 8, "PROGRESS  71%", p.muted, .9)

	badge := box{b.x + 617, trackY - 13, 74, 27}
	w.drawControlShape(badge, settings.Shape, p, true)
	w.text(badge.x+18, badge.y+8, 8, "ACTIVE", p.text, 1)
	w.rect(b.x+708, badge.y, 55, badge.h, p.surface, .72)
	w.text(b.x+721, badge.y+8, 8, "04", p.secondary, 1)

	tabsY := b.y + 144
	for i, label := range []string{"SIGNAL", "MODEL", "LOG"} {
		tab := box{b.x + 15 + float32(i)*112, tabsY, 102, 32}
		alpha := float32(.3)
		if i == 0 {
			alpha = .9
			w.rect(tab.x, tab.y, tab.w, tab.h, p.surface, .78)
		}
		w.line(tab.x, tab.y+tab.h, tab.x+tab.w, tab.y+tab.h, 1.4, p.accent, alpha)
		w.text(tab.x+19, tab.y+10, 9, label, p.text, .9)
	}

	list := box{b.x + 374, tabsY, 389, 72}
	w.rect(list.x, list.y, list.w, list.h, p.surface, .34)
	for i, label := range []string{"01  ORBITAL VECTOR", "02  MATERIAL SCAN", "03  THERMAL FIELD"} {
		y := list.y + float32(i)*24
		if i == 1 {
			w.rect(list.x, y, list.w, 24, p.hover, .72)
			w.rect(list.x, y, 3, 24, p.accent, .92)
		}
		w.text(list.x+12, y+7, 8, label, p.text, .88)
	}
}

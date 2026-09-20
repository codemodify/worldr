package workspace

import (
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/scene"
)

// selectedWindowBorderStyle is deliberately defensive for workspaces assembled
// directly in tests. Loaded desktop state is validated before it reaches here.
func (w *Workspace) selectedWindowBorderStyle() windowBorderStyle {
	style := w.windows.Border.normalized()
	if !style.valid() {
		return windowBorderInstrument
	}
	return style
}

type windowBorderVisual struct {
	tint      uint32
	idleAlpha float32
}

type windowChromePalette struct {
	plate, edge, glyph, detail scene.Color
}

func chromePaletteFor(style windowBorderStyle) windowChromePalette {
	switch style {
	case windowBorderAperture:
		return windowChromePalette{
			plate: scene.ColorHex(0x0a2732, .90), edge: scene.ColorHex(0x45e6f4, .96),
			glyph: scene.ColorHex(0xc9faff, 1), detail: scene.ColorHex(0x245668, .88),
		}
	case windowBorderGlass:
		return windowChromePalette{
			plate: scene.ColorHex(0x17313d, .70), edge: scene.ColorHex(0x70e8f5, .76),
			glyph: scene.ColorHex(0xe1fbff, .96), detail: scene.ColorHex(0x987cff, .62),
		}
	case windowBorderTelemetry:
		return windowChromePalette{
			plate: scene.ColorHex(0x171b1d, .96), edge: scene.ColorHex(0xf6a927, .96),
			glyph: scene.ColorHex(0xbaf8fb, 1), detail: scene.ColorHex(0xc84ceb, .78),
		}
	default:
		return windowChromePalette{
			plate: scene.ColorHex(0x123b48, .96), edge: scene.ColorHex(0x5ed5e8, .90),
			glyph: scene.ColorHex(0xc4f8ff, 1), detail: scene.ColorHex(0x3faabd, .78),
		}
	}
}

func windowChromeGlow(style windowBorderStyle, strength float32) [3]float32 {
	accent := chromePaletteFor(style).edge
	return [3]float32{accent.R * strength, accent.G * strength, accent.B * strength}
}

type windowBorderDragMeshKey struct {
	style     windowBorderStyle
	cinematic bool
}

type windowBorderFrameMeshKey struct {
	style  windowBorderStyle
	aspect uint16
}

const windowBorderAspectSteps = 40

func quantizedWindowBorderAspect(aspect float32) (uint16, float32) {
	if aspect <= 0 || math.IsNaN(float64(aspect)) || math.IsInf(float64(aspect), 0) {
		aspect = 1.6
	}
	// Quantization prevents a continuous resize from allocating one immutable
	// GPU mesh per pointer sample. The supported resize range has at most 181
	// retained ratios per style while 1/40 steps remain visually continuous.
	bucket := int(math.Round(float64(aspect * windowBorderAspectSteps)))
	bucket = max(windowBorderAspectSteps/2, min(windowBorderAspectSteps*5, bucket))
	return uint16(bucket), float32(bucket) / windowBorderAspectSteps
}

func (w *Workspace) windowBorderAspect(surface experience.ApplicationSurface) float32 {
	if i := w.m.applicationState.index(surface.Key); i >= 0 {
		placement := w.m.applicationState.Layouts[i]
		if placement.Width > 0 && placement.Height > 0 {
			return float32(placement.Width) / float32(placement.Height)
		}
	}
	if surface.ContentAspect > 0 && !math.IsInf(float64(surface.ContentAspect), 0) && !math.IsNaN(float64(surface.ContentAspect)) {
		return surface.ContentAspect
	}
	if surface.Texture != nil {
		width, height := surface.Texture.Size()
		if width > 0 && height > 0 {
			return float32(width) / float32(height)
		}
	}
	return 1.6
}

func (w *Workspace) windowBorderDragHandleMesh(style windowBorderStyle, cinematic bool) *scene.Mesh {
	style = style.normalized()
	if !style.valid() {
		style = windowBorderInstrument
	}
	key := windowBorderDragMeshKey{style: style, cinematic: cinematic}
	if w.windowBorderDragHandleMeshes == nil {
		w.windowBorderDragHandleMeshes = make(map[windowBorderDragMeshKey]*scene.Mesh, len(windowBorderChoices)*2)
	}
	if mesh := w.windowBorderDragHandleMeshes[key]; mesh != nil {
		return mesh
	}
	var (
		mesh *scene.Mesh
		err  error
	)
	if cinematic {
		if style == windowBorderInstrument && w.terminalDragHandleMesh != nil {
			mesh = w.terminalDragHandleMesh
		} else {
			mesh, err = terminalDragHandleMeshFor(style)
			if style == windowBorderInstrument {
				w.terminalDragHandleMesh = mesh
			}
		}
	} else if style == windowBorderInstrument && w.applicationDragHandleMesh != nil {
		mesh = w.applicationDragHandleMesh
	} else {
		mesh, err = applicationDragHandleMeshFor(style)
		if style == windowBorderInstrument {
			w.applicationDragHandleMesh = mesh
		}
	}
	if err != nil {
		panic(err)
	}
	w.windowBorderDragHandleMeshes[key] = mesh
	return mesh
}

func (w *Workspace) windowBorderControlMesh(style windowBorderStyle) *scene.Mesh {
	style = style.normalized()
	if !style.valid() {
		style = windowBorderInstrument
	}
	if w.windowBorderControlMeshes == nil {
		w.windowBorderControlMeshes = make(map[windowBorderStyle]*scene.Mesh, len(windowBorderChoices))
	}
	if mesh := w.windowBorderControlMeshes[style]; mesh != nil {
		return mesh
	}
	mesh := w.applicationWindowControlMesh
	var err error
	if style != windowBorderInstrument || mesh == nil {
		mesh, err = buildApplicationWindowControlMeshFor(style)
		if style == windowBorderInstrument {
			w.applicationWindowControlMesh = mesh
		}
	}
	if err != nil {
		panic(err)
	}
	w.windowBorderControlMeshes[style] = mesh
	return mesh
}

func (w *Workspace) windowBorderResizeMesh(style windowBorderStyle) *scene.Mesh {
	style = style.normalized()
	if !style.valid() {
		style = windowBorderInstrument
	}
	if w.windowBorderResizeMeshes == nil {
		w.windowBorderResizeMeshes = make(map[windowBorderStyle]*scene.Mesh, len(windowBorderChoices))
	}
	if mesh := w.windowBorderResizeMeshes[style]; mesh != nil {
		return mesh
	}
	mesh := w.applicationResizeHandleMesh
	var err error
	if style != windowBorderInstrument || mesh == nil {
		mesh, err = buildApplicationResizeHandleMeshFor(style)
		if style == windowBorderInstrument {
			w.applicationResizeHandleMesh = mesh
		}
	}
	if err != nil {
		panic(err)
	}
	w.windowBorderResizeMeshes[style] = mesh
	return mesh
}

func borderVisualFor(style windowBorderStyle) windowBorderVisual {
	switch style {
	case windowBorderAperture:
		return windowBorderVisual{tint: 0xc9faff, idleAlpha: .54}
	case windowBorderGlass:
		return windowBorderVisual{tint: 0xe1fbff, idleAlpha: .50}
	case windowBorderTelemetry:
		// The mesh contains its cyan and amber channels. A neutral tint keeps
		// both intact while interaction is expressed through light and opacity.
		return windowBorderVisual{tint: 0xffffff, idleAlpha: .62}
	default:
		return windowBorderVisual{tint: 0x8ce9ff, idleAlpha: .60}
	}
}

func (w *Workspace) windowBorderFrameMesh(style windowBorderStyle) *scene.Mesh {
	return w.windowBorderFrameMeshForAspect(style, 1.6)
}

func (w *Workspace) windowBorderFrameMeshForAspect(style windowBorderStyle, aspect float32) *scene.Mesh {
	style = style.normalized()
	if !style.valid() {
		style = windowBorderInstrument
	}
	aspectKey, aspect := quantizedWindowBorderAspect(aspect)
	key := windowBorderFrameMeshKey{style: style, aspect: aspectKey}
	if w.windowBorderFrameMeshes == nil {
		w.windowBorderFrameMeshes = make(map[windowBorderFrameMeshKey]*scene.Mesh, len(windowBorderChoices))
	}
	if mesh := w.windowBorderFrameMeshes[key]; mesh != nil {
		return mesh
	}
	var (
		mesh *scene.Mesh
		err  error
	)
	switch style {
	case windowBorderAperture:
		mesh, err = apertureFrameMeshForAspect(aspect)
	case windowBorderGlass:
		mesh, err = glassFrameMeshForAspect(aspect)
	case windowBorderTelemetry:
		mesh, err = telemetryFrameMeshForAspect(aspect)
	default:
		_, defaultAspect := quantizedWindowBorderAspect(1.6)
		if aspect == defaultAspect && w.terminalFrameMesh != nil {
			mesh = w.terminalFrameMesh
		} else {
			mesh, err = terminalFrameMeshForAspect(aspect)
			if aspect == defaultAspect {
				w.terminalFrameMesh = mesh
			}
		}
	}
	if err != nil {
		panic(err) // Constant chrome geometry is a programming error.
	}
	w.windowBorderFrameMeshes[key] = mesh
	return mesh
}

// apertureFrameMesh takes the sparse interrupted rails from 211.jpg and
// 225.webp. The large gaps are intentional: the corners establish the window
// without surrounding it with a conventional rectangle.
func apertureFrameMesh() (*scene.Mesh, error) {
	return apertureFrameMeshForAspect(1.6)
}

func apertureFrameMeshForAspect(aspect float32) (*scene.Mesh, error) {
	g := frameGeometry{aspect: aspect}
	cyan := scene.ColorHex(0x45e6f4, .98)
	hot := scene.ColorHex(0xc9faff, 1)
	dim := scene.ColorHex(0x245668, .78)
	side := scene.ColorHex(0x102c36, .72)

	paths := [][]framePoint{
		{{-.19, .558}, {-.482, .558}, {-.538, .482}, {-.538, .18}},
		{{-.538, -.12}, {-.538, -.482}, {-.482, -.558}, {-.30, -.558}},
		{{.20, .558}, {.482, .558}, {.538, .482}, {.538, .19}},
		{{.538, -.14}, {.538, -.482}, {.482, -.558}, {.28, -.558}},
	}
	for _, path := range paths {
		g.wall(.105, side, false, path...)
		g.stroke(.0033, cyan, path...)
	}
	// An inner interrupted rim and opposing bright acquisition marks give the
	// light frame enough hierarchy at both compact and wide window sizes.
	for _, path := range [][]framePoint{
		{{-.505, .31}, {-.505, .505}, {-.30, .505}},
		{{-.505, -.20}, {-.505, -.505}, {-.36, -.505}},
		{{.505, .34}, {.505, .505}, {.37, .505}},
		{{.505, -.25}, {.505, -.505}, {.33, -.505}},
	} {
		g.stroke(.0012, dim, path...)
	}
	g.stroke(.0050, hot, framePoint{-.438, .558}, framePoint{-.326, .558})
	g.stroke(.0040, hot, framePoint{.538, -.37}, framePoint{.538, -.25})
	g.stroke(.0014, dim, framePoint{-.15, -.543}, framePoint{.08, -.543})
	for i := 0; i < 5; i++ {
		y := -.068 + float32(i)*.034
		length := float32(.006)
		if i == 2 {
			length = .013
		}
		g.rect(.526, y, .526+length, y+.0015, hot)
	}
	return g.mesh()
}

// glassFrameMesh is a retained translucent slab, inspired by 104.webp,
// 161.jpg, 167.jpg and 198.jpg. It uses layered geometry rather than renderer
// blur, keeping the look deterministic and inexpensive.
func glassFrameMesh() (*scene.Mesh, error) {
	return glassFrameMeshForAspect(1.6)
}

func glassFrameMeshForAspect(aspect float32) (*scene.Mesh, error) {
	g := frameGeometry{aspect: aspect}
	outer := []framePoint{{-.482, .568}, {.482, .568}, {.546, .488}, {.546, -.488}, {.482, -.568}, {-.482, -.568}, {-.546, -.488}, {-.546, .488}}
	glass := scene.ColorHex(0x0a2431, .66)
	side := scene.ColorHex(0x06151e, .60)
	rim := scene.ColorHex(0x70e8f5, .82)
	inner := scene.ColorHex(0xb8f7ff, .50)
	violet := scene.ColorHex(0x987cff, .58)
	const aperture = float32(.505)

	g.wall(.10, side, true, outer...)
	top := clipFramePolygon(outer, 1, aperture, true)
	g.polygon(glass, clipFramePolygon(top, 0, -.19, false)...)
	g.polygon(glass, clipFramePolygon(top, 0, .19, true)...)
	g.polygon(glass, clipFramePolygon(outer, 1, -aperture, false)...)
	middle := clipFramePolygon(clipFramePolygon(outer, 1, aperture, false), 1, -aperture, true)
	g.polygon(glass, clipFramePolygon(middle, 0, -aperture, false)...)
	g.polygon(glass, clipFramePolygon(middle, 0, aperture, true)...)

	for _, sign := range []float32{-1, 1} {
		path := func(points ...framePoint) []framePoint {
			for i := range points {
				points[i].x *= sign
			}
			return points
		}
		g.stroke(.0020, rim, path(framePoint{.19, .556}, framePoint{.478, .556}, framePoint{.533, .482}, framePoint{.533, -.482}, framePoint{.478, -.556}, framePoint{.19, -.556})...)
		g.stroke(.0009, inner, path(framePoint{.19, .507}, framePoint{.507, .507}, framePoint{.507, -.507}, framePoint{.22, -.507})...)
	}
	// A small top notch and a restrained violet spectral glint distinguish the
	// slab from the harder engineering frames without touching app pixels.
	g.stroke(.0012, inner, framePoint{-.19, .556}, framePoint{-.164, .516}, framePoint{.164, .516}, framePoint{.19, .556})
	g.stroke(.0030, violet, framePoint{-.455, -.555}, framePoint{-.35, -.555})
	for _, y := range []float32{-.20, -.16, -.12} {
		g.rect(-.538, y, -.531, y+.018, inner)
	}
	return g.mesh()
}

// telemetryFrameMesh combines the asymmetric brackets and colored status
// channels seen in 02.jpg, 07.jpg, 181.jpg and 209.jpg.
func telemetryFrameMesh() (*scene.Mesh, error) {
	return telemetryFrameMeshForAspect(1.6)
}

func telemetryFrameMeshForAspect(aspect float32) (*scene.Mesh, error) {
	g := frameGeometry{aspect: aspect}
	cyan := scene.ColorHex(0x25e5ed, .95)
	teal := scene.ColorHex(0x0a7785, .82)
	amber := scene.ColorHex(0xf6a927, .98)
	magenta := scene.ColorHex(0xc84ceb, .78)
	plate := scene.ColorHex(0x071d27, .94)
	side := scene.ColorHex(0x041015, .92)

	// Independent plates keep the frame visibly asymmetric while preserving the
	// complete rectangular content aperture.
	g.wall(.14, side, false, framePoint{-.544, -.47}, framePoint{-.544, .47})
	g.wall(.14, side, false, framePoint{-.48, .564}, framePoint{-.19, .564})
	g.wall(.14, side, false, framePoint{.19, -.564}, framePoint{.47, -.564})
	g.rect(-.544, -.47, -.507, .47, plate)
	g.rect(-.48, .507, -.19, .564, plate)
	g.rect(.19, -.564, .47, -.507, plate)
	g.rect(.507, .15, .544, .47, plate)

	g.stroke(.0040, cyan, framePoint{-.534, -.35}, framePoint{-.534, .47}, framePoint{-.478, .554}, framePoint{-.27, .554})
	g.stroke(.0014, teal, framePoint{-.520, -.44}, framePoint{-.520, .505}, framePoint{-.47, .536}, framePoint{-.34, .536})
	g.stroke(.0033, cyan, framePoint{.205, -.553}, framePoint{.46, -.553}, framePoint{.534, -.47}, framePoint{.534, -.31})
	g.stroke(.0012, teal, framePoint{.27, -.533}, framePoint{.505, -.533}, framePoint{.518, -.455})

	// Offset status capsules, interruption marks and a short magenta channel are
	// geometry only; they do not invent labels or cover application content.
	g.polygon(amber, framePoint{-.438, .546}, framePoint{-.348, .546}, framePoint{-.335, .526}, framePoint{-.425, .526})
	g.rect(-.512, .16, -.507, .31, amber)
	g.rect(-.512, -.24, -.507, -.14, magenta)
	g.stroke(.0030, magenta, framePoint{.50, -.535}, framePoint{.534, -.496}, framePoint{.534, -.405})
	for i := 0; i < 8; i++ {
		y := -.105 + float32(i)*.035
		length := float32(.007)
		color := teal
		if i == 2 || i == 6 {
			length, color = .015, amber
		}
		g.rect(.513, y, .513+length, y+.002, color)
	}
	g.stroke(.0012, teal, framePoint{-.16, -.518}, framePoint{.08, -.518})
	return g.mesh()
}

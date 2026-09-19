package workspace

import (
	"fmt"
	"math"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/presentation"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
)

type box struct{ x, y, w, h float32 }

func (b box) contains(x, y float32) bool { return x >= b.x && x <= b.x+b.w && y >= b.y && y <= b.y+b.h }

type Workspace struct {
	labels                                                            *shapedLabels
	commands                                                          *commandPalette
	commandKeymap                                                     experience.Event
	desktop                                                           bool
	m                                                                 model
	canvas                                                            *scene.Canvas
	scene                                                             *scene.Scene
	backgroundScene                                                   *scene.Scene
	dnaNode                                                           scene.NodeID
	backgroundPhase                                                   time.Duration
	hologramPhase                                                     time.Duration
	nodes                                                             [3]scene.NodeID
	stageNode                                                         scene.NodeID
	accentNode                                                        scene.NodeID
	hologramNode                                                      scene.NodeID
	panelNode                                                         scene.NodeID
	panel                                                             *instrumentPanel
	applications                                                      experience.Applications
	application                                                       experience.ApplicationSurface
	applicationNode                                                   scene.NodeID
	applicationKeyboard                                               bool
	applicationHover                                                  bool
	applicationButtons                                                map[uint32]bool
	applicationX, applicationY                                        float32
	applicationNodes                                                  map[uint64]scene.NodeID
	applicationSpatial                                                map[uint64]*spatialMount
	applicationCaptureObject                                          uint64
	applicationFrames                                                 map[uint64]scene.NodeID
	applicationFrameMesh                                              *scene.Mesh
	photoFrameMesh                                                    *scene.Mesh
	terminalFrameMesh                                                 *scene.Mesh
	terminalDragHandleMesh                                            *scene.Mesh
	applicationDragHandles                                            map[uint64]scene.NodeID
	applicationDragHandleMesh                                         *scene.Mesh
	applicationWindowControls                                         map[uint64]scene.NodeID
	applicationWindowControlMesh                                      *scene.Mesh
	applicationMinimizedBarMesh                                       *scene.Mesh
	applicationResizeHandles                                          map[uint64]scene.NodeID
	applicationResizeHandleMesh                                       *scene.Mesh
	windowDragButtons                                                 map[uint32]bool
	windowThrow                                                       *windowThrow
	applicationKeys                                                   map[uint64]string
	applicationSurfaces                                               []experience.ApplicationSurface
	applicationFocusedID, applicationHoveredID, applicationCapturedID uint64
	applicationLayoutFull                                             bool
	applicationNotice                                                 string
	applicationNoticeRemaining                                        time.Duration
	applicationRestoreKey                                             string
	applicationRestoreSelection                                       uint32
	overviewShortcutHeld                                              bool
	overviewShortcutKey                                               uint32
	overviewNavigationKeys                                            uint8
	portals                                                           portalNavigation
	terminalShortcutKeys                                              uint8
	width, height                                                     int
	scale, ox, oy                                                     float32
	viewport                                                          scene.Viewport
	camera                                                            scene.Camera
	pointer                                                           pointerCapture
	history                                                           []edit
	historyPosition                                                   int
	nextEditID                                                        uint64
	demoStep                                                          int
	presentationBlend                                                 float32
	presentationInitialized                                           bool
	helpOpen                                                          bool
	helpKeys                                                          map[helpKey]bool
	helpButtons                                                       map[uint32]bool
	helpPointer                                                       helpPointerCapture
	applicationDockHover                                              int
}

func New() (*Workspace, error) {
	return newWorkspace(false)
}

// NewDesktop creates the general application workspace. AXIAL's model and
// instrument resources are created only by New, the explicit study experience.
func NewDesktop() (*Workspace, error) {
	return newWorkspace(true)
}

func newWorkspace(desktop bool) (*Workspace, error) {
	c, err := scene.NewCanvas()
	if err != nil {
		return nil, err
	}
	w := &Workspace{desktop: desktop, m: initialModel(), canvas: c, scene: scene.NewScene(), width: 1440, height: 900, scale: 1, applicationDockHover: -1}
	w.portals.pressed = -1
	stage, err := stageMesh()
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("build spatial guides: %w", err)
	}
	w.stageNode = w.scene.Add(0, scene.Node{Mesh: stage, Color: scene.ColorHex(teal, .3), Unlit: true, Unpickable: true, DepthReadOnly: true})
	w.applicationFrameMesh, err = applicationFrameMesh()
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("build application focus frame: %w", err)
	}
	if desktop {
		w.m.playing = false
		if err := w.initializeBackground(); err != nil {
			c.Close()
			return nil, fmt.Errorf("build workspace background: %w", err)
		}
		w.layout(1440, 900)
		w.syncScene()
		return w, nil
	}
	builders := []func() (*scene.Mesh, error){housingMesh, rotorMesh, shaftMesh}
	for i, build := range builders {
		mesh, err := build()
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("build %s: %w", componentIDs[i], err)
		}
		w.nodes[i] = w.scene.Add(0, scene.Node{Transform: scene.Identity(), Mesh: mesh, Color: scene.ColorHex(components[i].color, 1)})
	}
	accent, err := housingAccentMesh()
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("build housing accents: %w", err)
	}
	w.accentNode = w.scene.Add(w.nodes[0], scene.Node{Mesh: accent, Unlit: true, Unpickable: true, DepthReadOnly: true})
	w.hologramNode = w.scene.Add(w.nodes[0], scene.Node{
		Transform: scene.Scale(1.012, 1.012, 1.012), Mesh: w.scene.Node(w.nodes[0]).Mesh,
		Color: scene.ColorHex(0x72dcfa, .26), Unlit: true, Unpickable: true, Translucent: true,
		Material: render.Material{Hologram: .68, RimColor: [3]float32{.18, .78, 1}},
	})
	w.panel, err = newInstrumentPanel()
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("build instrument surface: %w", err)
	}
	w.panelNode = w.scene.Add(0, scene.Node{Transform: panelTransform(w.m.panelBehind), Surface: w.panel.texture})
	w.layout(1440, 900)
	w.syncScene()
	return w, nil
}

func (w *Workspace) Atlas() render.Atlas { return w.canvas.Atlas() }
func (w *Workspace) Close() error {
	if w.labels != nil {
		w.labels.painter.Close()
		w.labels = nil
	}
	if w.commands != nil {
		w.commands.close()
	}
	w.finishWindowThrow()
	w.clearApplicationFocus()
	if w.panel != nil {
		w.panel.close()
	}
	return w.canvas.Close()
}
func (w *Workspace) Update(dt time.Duration) {
	w.syncApplications()
	w.updateWindowThrow(dt)
	if dt > 0 && w.applicationNoticeRemaining > 0 {
		w.applicationNoticeRemaining -= dt
		if w.applicationNoticeRemaining <= 0 {
			w.applicationNotice, w.applicationNoticeRemaining = "", 0
		}
	}
	w.m.update(dt)
	w.updatePresentation(dt)
	w.updateBackground(dt)
	w.updateHologram(dt)
}
func (w *Workspace) Info() experience.Info {
	if w.desktop {
		return experience.Info{ID: "worldr.workspace", Title: "worldr — Spatial workspace", Controls: "use the right app rail to open native tools · Super+drag empty space to pan · drag the bottom-right scene pad to orbit · drag a window grip or Super+primary to move and throw · drag a window's bottom-right grip or Super+secondary to resize · use top-grip controls to minimize, maximize, restore or close · Super+wheel changes hovered-window depth · New Terminal or Ctrl+Alt+Enter opens a shell · Ctrl+Alt+G opens spatial portals · Ctrl+Alt+Left/Right jumps groups · Ctrl+Alt+O finds windows · Enter reads the selected app · P changes presentation · F1 opens Help"}
	}
	if w.applications != nil {
		return experience.Info{ID: "worldr.axial", Title: "worldr — AXIAL / 07", Controls: "New Terminal or Ctrl+Alt+Enter opens a shell · click app or press Enter from workspace to read and type · Ctrl+Alt+O overview from any app · overview: arrows select, Enter or Esc returns · O overview from workspace · drag scene to orbit, scroll scene to zoom · Place: drag apps, Shift+click to select, scroll for depth · Group moves selected apps together · Close Selected requests closing the active window · click workspace to use native shortcuts"}
	}
	return experience.Info{ID: "worldr.axial", Title: "worldr — AXIAL / 07", Controls: "drag to orbit · scroll scene to zoom · click to inspect · surface chart to scrub · B panel depth · E explode · Space pause · arrows scrub · F focus · P presentation · Shift+P reduced motion · R reset · Ctrl+Z undo · Ctrl+Shift+Z redo"}
}
func (w *Workspace) State() State {
	return State{Time: w.m.clock, Playing: w.m.playing, Exploded: w.m.exploded, Focused: w.m.focused, Selected: w.m.selected, Explosion: w.m.explosion, Zoom: w.m.zoom, Yaw: w.m.yaw, Pitch: w.m.pitch, Presentation: w.m.presentation, ReducedMotion: w.m.reducedMotion, PanelBehind: w.m.panelBehind, ApplicationBehind: w.m.applicationBehind, ApplicationReading: w.m.applicationReading, ApplicationWide: w.m.applicationWide}
}
func (w *Workspace) Summary() string {
	if w.desktop {
		return fmt.Sprintf("Spatial workspace · %d applications · %s", len(w.applicationSurfaces), w.m.presentation)
	}
	return fmt.Sprintf("AXIAL / 07 · %s · t=%.2fs · playing=%t · exploded=%t", components[w.m.selected].name, w.m.clock, w.m.playing, w.m.exploded)
}

func (w *Workspace) layout(width, height int) {
	w.width, w.height = width, height
	w.scale = float32(math.Min(float64(width)/1440, float64(height)/900))
	if w.scale <= 0 {
		w.scale = 1
	}
	w.ox = (float32(width) - 1440*w.scale) / 2
	w.oy = (float32(height) - 900*w.scale) / 2
	x, y, vw, vh := float32(253), float32(172), float32(877), float32(570)
	if w.desktop {
		w.viewport = scene.Viewport{X: w.ox + 253*w.scale, Y: w.oy + 166*w.scale, Width: 1135 * w.scale, Height: 615 * w.scale}
		return
	}
	if w.m.focused && w.application.ID == 0 {
		x, y, vw, vh = 110, 125, 1220, 625
	}
	if w.application.ID != 0 && (w.m.applicationReading || w.m.applicationState.Overview || w.m.applicationState.Placing) {
		x, y, vw, vh = 253, 172, 1135, 570
	}
	w.viewport = scene.Viewport{X: w.ox + x*w.scale, Y: w.oy + y*w.scale, Width: vw * w.scale, Height: vh * w.scale}
}

func (w *Workspace) syncScene() {
	w.scene.EffectPhase = float32(float64(w.hologramPhase) / float64(hologramCycle))
	radius := (float32(9.8) + w.m.explosion*1.4) * float32(math.Exp(-float64(w.m.zoom)))
	right, up, normal := applicationBasis()
	target := right.Mul(w.m.cameraX).Add(up.Mul(w.m.cameraY)).Add(normal.Mul(w.m.cameraDepth))
	eyeOffset := scene.Vec3{X: radius * float32(math.Cos(float64(w.m.pitch))) * float32(math.Cos(float64(w.m.yaw))), Y: radius * float32(math.Sin(float64(w.m.pitch))), Z: radius * float32(math.Cos(float64(w.m.pitch))) * float32(math.Sin(float64(w.m.yaw)))}
	w.camera = scene.Camera{Eye: target.Add(eyeOffset), Target: target, Up: scene.Vec3{Y: 1}, FOV: 0.69, Near: 0.1, Far: 100}
	stage := w.scene.Node(w.stageNode)
	stage.Color = scene.ColorHex(teal, .3*w.presentationBlend)
	stage.Hidden = w.desktop || w.presentationBlend == 0 || w.application.ID != 0 && (w.m.applicationReading || w.m.applicationState.Overview)
	stage.Glow = [3]float32{.08 * w.presentationBlend, .28 * w.presentationBlend, .4 * w.presentationBlend}
	if w.desktop {
		w.syncApplicationScene()
		return
	}
	accent := w.scene.Node(w.accentNode)
	accent.Color = scene.ColorHex(0x72dcfa, .85*w.presentationBlend)
	accent.Hidden = stage.Hidden
	accent.Glow = [3]float32{.3 * w.presentationBlend, .85 * w.presentationBlend, w.presentationBlend}
	hologram := w.scene.Node(w.hologramNode)
	hologram.Hidden = stage.Hidden
	hologram.Color = scene.ColorHex(0x72dcfa, .28*w.presentationBlend)
	hologram.Material.Hologram = .68 * w.presentationBlend
	rotation := float32(w.m.clock) * 0.6
	w.scene.Node(w.nodes[0]).Transform = scene.Translate(-w.m.explosion*1.65, 0, 0)
	w.scene.Node(w.nodes[1]).Transform = scene.Translate(w.m.explosion*0.2, 0, 0).Mul(scene.RotateX(rotation))
	w.scene.Node(w.nodes[2]).Transform = scene.Translate(w.m.explosion*1.15, 0, 0).Mul(scene.RotateX(rotation))
	for i, id := range w.nodes {
		n := w.scene.Node(id)
		n.Color = scene.ColorHex(components[i].color, 1)
		n.Material = components[i].appearance
		n.Material.RimStrength *= 0.15 + 0.85*w.presentationBlend
		if i != w.m.selected {
			n.Color.R *= 0.76
			n.Color.G *= 0.78
			n.Color.B *= 0.82
			n.Material.RimStrength *= 0.5
		}
		n.WireColor = scene.Color{}
		n.WireWidth = 0
		if i == w.m.selected {
			n.WireColor = scene.ColorHex(0xb4eaff, 0.025+0.24*w.presentationBlend)
			n.WireWidth = (0.45 + 0.35*w.presentationBlend) * w.scale
		}
	}
	w.scene.Node(w.panelNode).Transform = panelTransform(w.m.panelBehind)
	w.syncApplicationScene()
}

func (w *Workspace) color(rgb uint32, alpha float32) scene.Color { return scene.ColorHex(rgb, alpha) }
func (w *Workspace) rect(x, y, ww, hh float32, rgb uint32, alpha float32) {
	w.canvas.Rect(w.ox+x*w.scale, w.oy+y*w.scale, ww*w.scale, hh*w.scale, w.color(rgb, alpha))
}
func (w *Workspace) line(x, y, xx, yy, width float32, rgb uint32, alpha float32) {
	w.canvas.Line(w.ox+x*w.scale, w.oy+y*w.scale, w.ox+xx*w.scale, w.oy+yy*w.scale, width*w.scale, w.color(rgb, alpha))
}
func (w *Workspace) circle(x, y, r, width float32, rgb uint32, alpha float32) {
	w.canvas.Circle(w.ox+x*w.scale, w.oy+y*w.scale, r*w.scale, width*w.scale, w.color(rgb, alpha))
}
func (w *Workspace) text(x, y, size float32, value string, rgb uint32, alpha float32) {
	w.canvas.Text(w.ox+x*w.scale, w.oy+y*w.scale, size*w.scale, value, w.color(rgb, alpha))
}

const (
	bg    uint32 = 0x080f1a
	ink   uint32 = 0xe0edf7
	muted uint32 = 0x839fb7
	teal  uint32 = 0x7bd5ef
	amber uint32 = 0xdfb87c
)

func (w *Workspace) Draw(width, height int) render.Frame {
	w.beginShapedLabels()
	w.syncApplications()
	if width != w.width || height != w.height {
		w.finishWindowThrow()
	}
	w.initializePresentation()
	w.layout(width, height)
	w.syncScene()
	if w.panel != nil && w.application.ID == 0 {
		w.panel.update(w.m)
	}
	w.canvas.Reset(width, height)
	w.canvas.SetLinearColor(true)
	w.fitApplicationShadow()
	w.canvas.Rect(0, 0, float32(width), float32(height), w.color(bg, 1))
	w.drawSpatialGuides()
	w.drawBackground()
	w.scene.Draw(w.canvas, w.camera, w.viewport)
	w.drawSpatialApplicationLabels()
	w.drawApplicationOverviewLabels()
	w.drawHeader()
	if w.desktop {
		w.drawDesktop()
		w.drawOrbitPad()
		w.drawApplicationDock()
		w.drawApplicationNotice()
		w.drawPortalAtlas()
		w.drawHelp()
		return w.commandFrame(w.shapedFrame(w.canvas.Frame()))
	}
	if w.application.ID != 0 {
		w.drawAssembly()
		if !w.m.applicationReading && !w.m.applicationState.Overview && !w.m.applicationState.Placing {
			w.drawAnalysis()
		}
	} else if !w.m.focused {
		w.drawAssembly()
		w.drawAnalysis()
	} else {
		w.text(42, 136, 14, "AXIAL / 07", muted, 1)
		w.text(42, 163, 28, components[w.m.selected].name, ink, 1)
	}
	w.drawObjectControls()
	w.drawTransport()
	w.drawApplicationNotice()
	w.drawHelp()
	return w.shapedFrame(w.canvas.Frame())
}

func (w *Workspace) drawHeader() {
	// The mark is a small coordinate frame, shared with the workspace itself.
	w.circle(54, 47, 12, 1.6, teal, 1)
	w.line(42, 47, 66, 47, 1.2, teal, 1)
	w.line(54, 35, 54, 59, 1.2, teal, 1)
	w.text(78, 30, 26, "worldr", ink, 1)
	w.line(199, 32, 199, 62, 1, muted, 0.24)
	w.text(226, 30, 12, "NATIVE WORKSPACE", muted, 1)
	subtitle := "Engineering / Axial study"
	if w.desktop {
		subtitle = w.m.applicationState.spaceName(w.m.applicationState.Space) + "  /  TOOLS + SPACES"
	}
	w.shapedText(w.ox+226*w.scale, w.oy+49*w.scale, 17*w.scale, 300*w.scale, subtitle, w.color(ink, 1))
	if _, ok := w.applications.(experience.ApplicationLauncher); ok {
		w.button(applicationLaunchButton, "NEW TERMINAL", false)
	}
	w.text(741, 16, 10, "PRESENTATION  /  P", muted, 0.9)
	w.drawMotionPreference()
	w.button(cinematicButton, "CINEMATIC", w.m.presentation == presentation.Cinematic)
	w.button(adaptiveButton, "ADAPTIVE", w.m.presentation == presentation.Adaptive)
	resetLabel := "RESET  R"
	if w.desktop {
		resetLabel = "RESET VIEW"
	}
	if w.applicationKeyboard && !w.desktop {
		resetLabel = "RESET"
	}
	w.button(box{1110, 30, 123, 37}, resetLabel, false)
	if w.application.ID != 0 {
		label := "READ APP"
		if w.m.applicationReading {
			label = "SPACE"
		}
		w.button(box{1247, 30, 151, 37}, label, w.m.applicationReading)
	} else if !w.desktop {
		w.button(box{1247, 30, 151, 37}, "FOCUS  F", w.m.focused)
	}
	w.line(42, 96, 1398, 96, 1, muted, 0.22)
}

func (w *Workspace) button(b box, label string, active bool) {
	if active {
		w.rect(b.x, b.y, b.w, b.h, teal, 0.11)
	}
	w.line(b.x, b.y+b.h, b.x+b.w, b.y+b.h, 1, muted, 0.3)
	c := muted
	if active {
		c = teal
	}
	w.text(b.x+14, b.y+10, 13, label, c, 1)
}

func (w *Workspace) drawAssembly() {
	if w.application.ID != 0 {
		w.drawApplicationControls()
		return
	}
	w.text(42, 132, 12, "STUDY 001 / ASSEMBLY", muted, 1)
	w.text(39, 164, 49, "AXIAL", ink, 1)
	w.text(44, 225, 14, "An exploration of motion.", muted, 1)
	w.text(43, 280, 12, "COMPONENTS", muted, 1)
	for i, c := range components {
		y := float32(317 + i*68)
		if i == w.m.selected {
			w.rect(32, y-10, 208, 56, teal, 0.075)
			w.rect(32, y-10, 2, 56, teal, 1)
		}
		color := muted
		if i == w.m.selected {
			color = ink
		}
		w.text(43, y+1, 12, fmt.Sprintf("0%d", i+1), muted, 1)
		w.text(75, y, 16, c.name, color, 1)
		w.text(75, y+24, 11, []string{"STATIC / CUTAWAY", "ROTATING / 18 BLADES", "ROTATING / COMMON AXIS"}[i], muted, 0.9)
	}
	w.line(43, 549, 228, 549, 1, muted, 0.22)
	w.text(43, 571, 12, "CONFIGURATION", muted, 1)
	label := "Explode assembly"
	if w.m.exploded {
		label = "Close assembly"
	}
	w.button(box{32, 600, 208, 44}, label, w.m.exploded)
	label = "Bring panel forward  B"
	if !w.m.panelBehind {
		label = "Send panel back  B"
	}
	w.button(panelDepthButton, label, !w.m.panelBehind)
	w.text(43, 706, 12, "E  explode    1–3  select", muted, 0.9)
	w.text(43, 727, 12, "Drag to orbit / scroll to zoom.", muted, 0.9)
	w.text(43, 748, 12, "Scrub the panel's live chart.", muted, 0.9)
}

func (w *Workspace) drawAnalysis() {
	x := float32(1150)
	c := components[w.m.selected]
	w.text(x, 132, 12, "COMPONENT INSPECTOR", muted, 1)
	w.text(x, 166, 23, c.name, ink, 1)
	w.line(x, 207, 1398, 207, 1, muted, 0.22)
	w.text(x, 230, 12, "REFERENCE MATERIAL", muted, 1)
	w.text(x, 250, 17, c.material, ink, 1)
	w.text(x, 296, 12, []string{"OUTER DIAMETER", "BLADE DIAMETER", "AXIAL LENGTH"}[w.m.selected], muted, 1)
	w.text(x-2, 318, 49, c.dimension, ink, 1)
	w.text(x+129, 347, 13, "model units", muted, 1)
	w.text(x, 385, 12, c.detail, muted, 1)
	w.line(x, 422, 1398, 422, 1, muted, 0.22)
	w.text(x, 444, 12, "MODEL RESPONSE", muted, 1)
	value := signal(w.m.selected, w.m.clock)
	w.text(x-1, 469, 38, fmt.Sprintf("%.3f", value), teal, 1)
	w.text(x+136, 492, 12, "normalized", muted, 1)
	w.drawGraph(box{x, 539, 247, 122})
	w.text(x, 683, 12, "Synthetic signal · time linked", muted, 1)
	w.text(x, 706, 12, "Illustrative, not a physics result.", muted, 0.9)
}

func (w *Workspace) drawGraph(b box) {
	for i := 0; i < 4; i++ {
		y := b.y + float32(i)*b.h/3
		w.line(b.x, y, b.x+b.w, y, 0.7, muted, 0.17)
	}
	for i := 0; i < 120; i++ {
		t0, t1 := float64(i)*duration/120, float64(i+1)*duration/120
		x0, x1 := b.x+float32(i)*b.w/120, b.x+float32(i+1)*b.w/120
		y0, y1 := b.y+b.h*(1-float32(signal(w.m.selected, t0))), b.y+b.h*(1-float32(signal(w.m.selected, t1)))
		w.line(x0, y0, x1, y1, 1.5, teal, 0.84)
	}
	x := b.x + float32(w.m.clock/duration)*b.w
	y := b.y + b.h*(1-float32(signal(w.m.selected, w.m.clock)))
	w.line(x, b.y, x, b.y+b.h, 1, amber, 0.42)
	w.circle(x, y, 3.4, 3.4, amber, 1)
}

func (w *Workspace) drawObjectControls() {
	if w.application.ID != 0 && w.m.applicationState.Overview {
		w.text(296, 135, 12, "APPLICATIONS / OVERVIEW", muted, 1)
		return
	}
	if w.application.ID != 0 && w.m.applicationState.Placing {
		w.text(296, 135, 12, "PLACEMENT / DRAG TO ARRANGE", muted, 1)
		return
	}
	if w.application.ID != 0 && w.m.applicationReading {
		w.text(296, 135, 12, "APPLICATION / READING", muted, 1)
		return
	}
	label := "ASSEMBLED"
	if w.m.exploded {
		label = "EXPLODED VIEW"
	}
	w.text(296, 135, 12, label, muted, 1)
	modeLabel := "CINEMATIC / ORBIT"
	if w.m.presentation == presentation.Adaptive {
		modeLabel = "ADAPTIVE / ORBIT"
		if w.m.focused {
			modeLabel = "ADAPTIVE / CALM FOCUS"
		}
	}
	w.text(847, 135, 12, modeLabel, muted, 0.8)
	if w.m.focused {
		return
	}
	w.line(285, 726, 1085, 726, 1, muted, 0.16)
	w.text(289, 742, 12, "PROCEDURAL GEOMETRY", muted, 0.85)
	w.text(835, 742, 12, "03 PARTS / 01 SHARED CLOCK", muted, 0.85)
}

func (w *Workspace) drawTransport() {
	w.line(42, 788, 1398, 788, 1, muted, 0.3)
	label := "PAUSE"
	if !w.m.playing {
		label = "PLAY"
	}
	w.rect(42, 810, 103, 43, teal, 0.10)
	w.text(62, 824, 14, label, teal, 1)
	w.text(169, 808, 12, "TIME", muted, 1)
	w.text(169, 830, 19, fmt.Sprintf("%05.2f s", w.m.clock), ink, 1)
	track := box{307, 824, 1075, 20}
	w.line(track.x, track.y, track.x+track.w, track.y, 2, muted, 0.27)
	progress := track.x + track.w*float32(w.m.clock/duration)
	w.line(track.x, track.y, progress, track.y, 2, teal, 0.8)
	for i := 0; i <= 30; i++ {
		x := track.x + track.w*float32(i)/30
		h := float32(5)
		if i%5 == 0 {
			h = 9
			w.text(x-4, 846, 11, fmt.Sprintf("%02d", i), muted, 0.9)
		}
		w.line(x, track.y+6, x, track.y+6+h, 1, muted, 0.35)
	}
	w.circle(progress, track.y, 5, 5, teal, 1)
	w.drawHelpButton()
	if w.applicationKeyboard {
		w.text(42, 878, 11, "Keyboard sent to application", teal, .9)
		w.text(307, 878, 11, "Click workspace controls to return", muted, .9)
		w.text(1157, 878, 11, "Ctrl+Alt+Q  quit", muted, .9)
		return
	}
	w.text(42, 878, 11, "SPACE  play / pause", muted, 0.9)
	w.text(307, 878, 11, "Ctrl+Z  undo     Ctrl+Shift+Z  redo", muted, 0.9)
	if w.m.applicationState.Overview {
		w.text(1157, 878, 11, "Arrows  select    Enter  return", muted, .9)
	} else {
		w.text(1157, 878, 11, "← →  step     R  reset", muted, .9)
	}
}

// Presentation changes only decorative framing; camera, content and input share
// the same scene in both modes. Adaptive calms the frame during focused work.
func (w *Workspace) presentationTarget() float32 {
	if w.m.presentation == presentation.Adaptive && (w.m.focused || w.application.ID != 0 && w.m.applicationReading) {
		return 0
	}
	return 1
}
func (w *Workspace) initializePresentation() {
	if !w.presentationInitialized || w.m.reducedMotion {
		w.presentationBlend = w.presentationTarget()
		w.presentationInitialized = true
	}
}
func (w *Workspace) updatePresentation(dt time.Duration) {
	w.initializePresentation()
	if dt <= 0 {
		return
	}
	target := w.presentationTarget()
	w.presentationBlend += (target - w.presentationBlend) * float32(1-math.Exp(-dt.Seconds()*12))
	if abs(target-w.presentationBlend) < 0.001 {
		w.presentationBlend = target
	}
}

var (
	cinematicButton = box{727, 32, 173, 35}
	adaptiveButton  = box{912, 32, 173, 35}
	motionButton    = box{912, 10, 173, 20}
)

func (w *Workspace) drawMotionPreference() {
	label, color := "FULL MOTION", muted
	if w.m.reducedMotion {
		label, color = "REDUCED MOTION", teal
		w.rect(motionButton.x, motionButton.y, motionButton.w, motionButton.h, teal, .08)
	}
	w.text(motionButton.x+14, motionButton.y+4, 10, label, color, 1)
	w.line(motionButton.x, motionButton.y+motionButton.h, motionButton.x+motionButton.w, motionButton.y+motionButton.h, 1, muted, .22)
}

func (w *Workspace) drawSpatialGuides() {
	if w.desktop {
		return
	}
	strength := w.presentationBlend
	if strength <= 0 {
		return
	}
	centerX, centerY := float32(696), float32(443)
	if w.m.focused {
		centerX, centerY = 720, 435
	}
	// A single gradient fan supplies ambient framing without repeatedly
	// shading the same translucent area or changing scene depth.
	w.canvas.RadialGradient(w.ox+centerX*w.scale, w.oy+centerY*w.scale, 270*w.scale, w.color(0x4186b6, .14*strength), w.color(0x4186b6, 0))
	for y := float32(186); y < 740; y += 36 {
		for x := float32(280); x < 1120; x += 36 {
			w.rect(x, y, 1, 1, teal, 0.14*strength)
		}
	}
	w.circle(centerX, centerY, 219, 0.8, teal, 0.14*strength)
	w.circle(centerX, centerY, 242, 0.6, teal, 0.10*strength)
	w.line(centerX-260, centerY, centerX+260, centerY, 0.6, teal, 0.13*strength)
	w.line(centerX, centerY-265, centerX, centerY+265, 0.6, teal, 0.13*strength)
	for i := 0; i < 48; i++ {
		a := float64(i) * 2 * math.Pi / 48
		length := float32(4)
		if i%4 == 0 {
			length = 9
		}
		c, s := float32(math.Cos(a)), float32(math.Sin(a))
		w.line(centerX+c*235, centerY+s*235, centerX+c*(235+length), centerY+s*(235+length), 0.75, teal, 0.3*strength)
	}
}

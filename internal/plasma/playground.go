// Package plasma is the native fluid-surface reference experience. Application
// contents remain independent retained images above the shared GPU field.
package plasma

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	fluid "github.com/codemodify/worldr/sdk/fluid/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

const logicalWidth, logicalHeight = float32(884), float32(720)

type labelKey struct {
	text, font          string
	size, width, height int
	ink                 color.RGBA
}
type uiControl struct {
	id     string
	bounds fluid.Rect
}

// Playground implements the ordinary experience contract, including native
// persistence, resize, deterministic demonstration and device-recovery replay.
type Playground struct {
	model               *Model
	canvas              *scene.Canvas
	labels              map[labelKey]*render.Texture
	labelBytes          int
	painters            map[string]*nativeui.Painter
	retired             []uint64
	surfaces            []fluid.Surface
	controls            []uiControl
	width, height       int
	scale, left, top    float32
	time                float32
	pointer             fluid.Point
	pointerActive       bool
	preset              string
	lens                float32
	playing, done, read bool
	pressed             string
	pressValue          float32
	custom              *skin.Skin
	closed              bool
}

func New() (*Playground, error) {
	c, err := scene.NewCanvas()
	if err != nil {
		return nil, err
	}
	p := &Playground{model: NewModel(), canvas: c, labels: make(map[labelKey]*render.Texture), painters: make(map[string]*nativeui.Painter), preset: "lumen", lens: .65, playing: true, scale: 1, width: 884, height: 720}
	p.buildControls()
	return p, nil
}
func (p *Playground) Info() experience.Info {
	return experience.Info{ID: "worldr.plasma", Title: "Worldr · Fluid surfaces", Controls: "Drag panels to join or separate · Tab / arrows move panels · Enter / Space activate · F fusion · S snapping · M motion · 1–3 appearance · R reset"}
}
func (p *Playground) Atlas() render.Atlas { return p.canvas.Atlas() }
func (p *Playground) Update(dt time.Duration) {
	p.model.Update(dt)
	if p.model.Motion && dt > 0 {
		p.time = float32(math.Mod(float64(p.time)+math.Min(dt.Seconds(), .1), 3600))
	}
}
func (p *Playground) Demo(elapsed time.Duration) { p.model.Demo(elapsed) }

func (p *Playground) dropLabels() {
	for _, texture := range p.labels {
		p.retired = append(p.retired, texture.ID())
	}
	clear(p.labels)
	p.labelBytes = 0
	for _, painter := range p.painters {
		painter.Close()
	}
	clear(p.painters)
}
func (p *Playground) RetiredTextures() []uint64 {
	out := p.retired
	p.retired = nil
	return out
}
func (p *Playground) Close() error {
	if p.closed {
		return nil
	}
	p.closed = true
	p.dropLabels()
	return p.canvas.Close()
}

func (p *Playground) style() fluid.Style {
	s := fluid.DefaultStyle()
	s.Blend, s.Refraction, s.Rim, s.Glow, s.Opacity = p.model.Blend*p.scale, p.lens, .9, .35, .84
	s.Background = [3][4]float32{{.018, .066, .083, 1}, {.035, .24, .25, 1}, {.20, .12, .38, 1}}
	s.Frost = .12
	switch p.preset {
	case "studio":
		s.Background = [3][4]float32{{.065, .025, .085, 1}, {.26, .11, .20, 1}, {.32, .21, .13, 1}}
		s.Opacity, s.Frost = .8, .24
	case "slate":
		s.Background = [3][4]float32{{.045, .064, .09, 1}, {.12, .19, .25, 1}, {.17, .20, .29, 1}}
		s.Rim, s.Glow, s.Opacity, s.Frost = .5, .07, .8, .36
	}
	if p.custom != nil {
		for i, name := range []string{"background", "accent", "accent-alt"} {
			v := p.custom.Color(name)
			s.Background[i] = [4]float32{float32(v.R) / 255, float32(v.G) / 255, float32(v.B) / 255, 1}
		}
	}
	return s
}

func (p *Playground) Draw(width, height int) render.Frame {
	if p.closed || width < 1 || height < 1 {
		return render.Frame{}
	}
	// Retire only between frames: earlier commands in this frame may still
	// reference a cached label. A frame adds fewer than 48 bounded labels.
	if len(p.labels) > 160 || p.labelBytes > 16<<20 {
		p.dropLabels()
	}
	if width != p.width || height != p.height {
		p.model.Handle(experience.Event{Kind: experience.PointerCancel})
		p.cancelControl()
		p.dropLabels()
		p.width, p.height = width, height
	}
	p.scale = min(float32(width)/logicalWidth, float32(height)/logicalHeight)
	p.left, p.top = (float32(width)-logicalWidth*p.scale)/2, (float32(height)-logicalHeight*p.scale)/2
	p.canvas.Reset(width, height)
	p.canvas.SetLinearColor(true)
	p.surfaces = p.surfaces[:0]
	for _, panel := range p.model.Panels {
		b := p.screen(panel.Bounds)
		p.surfaces = append(p.surfaces, fluid.Surface{Bounds: b, Radius: panel.Radius * p.scale, Tint: [4]float32{.40, .72, .69, 1}, Fuse: p.model.Fusion && panel.Fuse})
	}
	p.canvas.Fluid(fluid.Field{Bounds: fluid.Rect{Width: float32(width), Height: float32(height)}, Surfaces: p.surfaces, Style: p.style(), Time: p.time, Pointer: p.pointer, PointerActive: p.pointerActive})
	p.buildControls()
	p.drawHeader()
	for _, i := range p.model.order {
		p.drawPanel(i)
	}
	p.drawControls()
	return p.canvas.Frame()
}

func (p *Playground) screen(r fluid.Rect) fluid.Rect {
	return fluid.Rect{X: p.left + r.X*p.scale, Y: p.top + r.Y*p.scale, Width: r.Width * p.scale, Height: r.Height * p.scale}
}
func (p *Playground) round(r fluid.Rect, radius float32, ink scene.Color) {
	r = p.screen(r)
	radius = min(radius*p.scale, min(r.Width, r.Height)/2)
	var points [36][2]float32
	centers := [4][2]float32{{r.X + r.Width - radius, r.Y + radius}, {r.X + r.Width - radius, r.Y + r.Height - radius}, {r.X + radius, r.Y + r.Height - radius}, {r.X + radius, r.Y + radius}}
	for corner, center := range centers {
		for step := 0; step < 9; step++ {
			a := (float64(corner)*.5 - .5 + float64(step)/16) * math.Pi
			points[corner*9+step] = [2]float32{center[0] + radius*float32(math.Cos(a)), center[1] + radius*float32(math.Sin(a))}
		}
	}
	p.canvas.FillConvex(ink, points[:]...)
}
func (p *Playground) line(x, y, w float32, ink scene.Color) {
	p.canvas.Rect(p.left+x*p.scale, p.top+y*p.scale, w*p.scale, p.scale, ink)
}

// Labels rasterize only when text, typography or output scale changes. Dragging
// changes the image command's bounds, never the client's pixel data.
func (p *Playground) text(x, y, w, size float32, value string, serif bool, ink uint32) {
	fontName := "Sans"
	if serif {
		fontName = "Serif"
	}
	px := max(6, min(128, int(math.Round(float64(size*p.scale)))))
	width, height := max(1, int(math.Ceil(float64(w*p.scale)))), max(1, int(math.Ceil(float64((size+10)*p.scale))))
	width, height = min(4096, width), min(4096, height)
	key := labelKey{value, fontName, px, width, height, nativeui.Color(ink)}
	texture := p.labels[key]
	if texture == nil {
		fontKey := fmt.Sprintf("%s/%d", fontName, px)
		painter := p.painters[fontKey]
		if painter == nil {
			theme := nativeui.Cinematic()
			theme.Font, theme.FontSize = fontName, float64(px)
			var err error
			painter, err = nativeui.NewPainter(theme)
			if err != nil {
				return
			}
			p.painters[fontKey] = painter
		}
		pixels := image.NewRGBA(image.Rect(0, 0, width, height))
		if err := painter.DrawLabel(pixels, pixels.Bounds(), value, key.ink); err != nil {
			return
		}
		var err error
		texture, err = render.NewTexture(width, height, pixels.Pix)
		if err != nil {
			return
		}
		p.labels[key] = texture
		p.labelBytes += len(pixels.Pix)
	}
	p.canvas.Image(texture, p.left+x*p.scale, p.top+y*p.scale, float32(width), float32(height))
}

func (p *Playground) drawHeader() {
	p.round(fluid.Rect{X: 24, Y: 20, Width: 836, Height: 62}, 30, scene.ColorHex(0x97ccd1, .24))
	p.round(fluid.Rect{X: 25, Y: 21, Width: 834, Height: 60}, 29, scene.ColorHex(0x081e27, .9))
	p.text(47, 32, 128, 24, "Plasma", true, 0xe1eeeb)
	p.text(168, 42, 220, 12, "Fluid surfaces / Worldr", false, 0x94b8bf)
	for _, c := range p.controls {
		if c.id != "lumen" && c.id != "studio" && c.id != "slate" {
			continue
		}
		if c.id == p.preset && p.custom == nil {
			p.round(c.bounds, 16, scene.ColorHex(0xd9e8e4, .98))
		} else if p.hover(c) {
			p.round(c.bounds, 16, scene.ColorHex(0x729ba6, .16))
		}
		ink := uint32(0xc3d4d8)
		if c.id == p.preset && p.custom == nil {
			ink = 0x183139
		}
		label := map[string]string{"lumen": "Lumen", "studio": "Studio", "slate": "Slate"}[c.id]
		p.text(c.bounds.X+16, c.bounds.Y+3, 62, 13, label, false, ink)
	}
}
func (p *Playground) drawPanel(index int) {
	panel := p.model.Panels[index]
	b := panel.Bounds
	inner := fluid.Rect{X: b.X + 16, Y: b.Y + 16, Width: b.Width - 32, Height: b.Height - 32}
	if index == p.model.Active {
		p.round(fluid.Rect{X: inner.X - 1, Y: inner.Y - 1, Width: inner.Width + 2, Height: inner.Height + 2}, 14, scene.ColorHex(0xa1e2db, .26))
	}
	p.round(inner, 13, scene.ColorHex(0x071d24, .75))
	p.text(b.X+29, b.Y+22, b.Width-54, 16, panel.Title, true, 0xe6ece5)
	body := panel.Body
	switch panel.ID {
	case "inbox":
		if p.read {
			body = "All caught up."
		}
	case "tasks":
		if p.done {
			body = "Release notes shipped."
		}
	case "player":
		if !p.playing {
			body = "Side B · paused"
		}
	}
	p.text(b.X+29, b.Y+51, b.Width-54, 12, body, false, 0xa4bebd)
	status, ink := "Standalone", uint32(0xa2b5bd)
	if p.model.Joined(index) {
		status, ink = "Joined", 0x9fdbcd
	}
	p.text(b.X+29, b.Y+84, 100, 13, status, true, ink)
	if id := panelButton(panel.ID); id != "" {
		for _, c := range p.controls {
			if c.id == id {
				p.round(c.bounds, 10, scene.ColorHex(0x97c4bd, .12))
				if p.hover(c) || p.pressed == c.id {
					p.round(c.bounds, 10, scene.ColorHex(0xa6e0d6, .18))
				}
				label := "Read"
				if id == "task" {
					label = "Done"
					if p.done {
						label = "Undo"
					}
				}
				if id == "play" {
					label = "Pause"
					if !p.playing {
						label = "Play"
					}
				}
				p.text(c.bounds.X+9, c.bounds.Y-1, c.bounds.Width-12, 10, label, false, 0xc0dad3)
			}
		}
	}
}
func panelButton(id string) string {
	switch id {
	case "inbox":
		return "read"
	case "tasks":
		return "task"
	case "player":
		return "play"
	}
	return ""
}

func (p *Playground) buildControls() {
	p.controls = p.controls[:0]
	for i, id := range []string{"lumen", "studio", "slate"} {
		p.controls = append(p.controls, uiControl{id, fluid.Rect{X: 594 + float32(i)*82, Y: 36, Width: 76, Height: 31}})
	}
	for i, id := range []string{"fusion", "snap", "motion"} {
		p.controls = append(p.controls, uiControl{id, fluid.Rect{X: 39 + float32(i)*78, Y: 653, Width: 71, Height: 28}})
	}
	p.controls = append(p.controls, uiControl{"blend", fluid.Rect{X: 321, Y: 650, Width: 151, Height: 36}}, uiControl{"lens", fluid.Rect{X: 510, Y: 650, Width: 151, Height: 36}}, uiControl{"reset", fluid.Rect{X: 755, Y: 653, Width: 85, Height: 28}})
	for _, panel := range p.model.Panels {
		if id := panelButton(panel.ID); id != "" {
			p.controls = append(p.controls, uiControl{id, fluid.Rect{X: panel.Bounds.X + panel.Bounds.Width - 78, Y: panel.Bounds.Y + 87, Width: 49, Height: 21}})
		}
	}
}
func (p *Playground) drawControls() {
	p.text(32, 612, 635, 12, "Drag panels together. Pull apart to separate.", false, 0xabc4c7)
	p.round(fluid.Rect{X: 24, Y: 640, Width: 836, Height: 55}, 20, scene.ColorHex(0x83b5c4, .22))
	p.round(fluid.Rect{X: 25, Y: 641, Width: 834, Height: 53}, 19, scene.ColorHex(0x071c26, .95))
	for _, c := range p.controls {
		switch c.id {
		case "fusion", "snap", "motion", "reset":
			selected := c.id == "fusion" && p.model.Fusion || c.id == "snap" && p.model.Snap || c.id == "motion" && p.model.Motion
			fill := scene.ColorHex(0x7e9daa, .10)
			if selected {
				fill = scene.ColorHex(0x7ddcca, .19)
			}
			p.round(c.bounds, 11, fill)
			if p.hover(c) || p.pressed == c.id {
				p.round(c.bounds, 11, scene.ColorHex(0xbce8e0, .15))
			}
			label := map[string]string{"fusion": "Fusion", "snap": "Snap", "motion": "Motion", "reset": "Reset"}[c.id]
			p.text(c.bounds.X+12, c.bounds.Y+1, c.bounds.Width-14, 11, label, false, 0xc7dddb)
		case "blend", "lens":
			value, label := p.model.Blend/64, fmt.Sprintf("Blend   %.0f px", p.model.Blend)
			if c.id == "lens" {
				value, label = p.lens, fmt.Sprintf("Glass   %.0f%%", p.lens*100)
			}
			p.text(c.bounds.X, c.bounds.Y-7, c.bounds.Width, 11, label, false, 0xa6c9cb)
			p.round(fluid.Rect{X: c.bounds.X, Y: c.bounds.Y + 25, Width: c.bounds.Width, Height: 3}, 1.5, scene.ColorHex(0x6c8e9a, .28))
			p.round(fluid.Rect{X: c.bounds.X, Y: c.bounds.Y + 25, Width: max(1, value*c.bounds.Width), Height: 3}, 1.5, scene.ColorHex(0x9eddd1, .95))
			p.round(fluid.Rect{X: c.bounds.X + value*c.bounds.Width - 4, Y: c.bounds.Y + 22, Width: 9, Height: 9}, 4.5, scene.ColorHex(0xe0f2e7, 1))
		}
	}
	p.text(35, 697, 800, 10, "TAB select · ARROWS move · ENTER activate · F fusion · S snap · M motion · 1–3 appearance · R reset", false, 0x7196a1)
}

func (p *Playground) hover(c uiControl) bool {
	return p.pointerActive && c.bounds.Contains(fluid.Point{X: (p.pointer.X - p.left) / p.scale, Y: (p.pointer.Y - p.top) / p.scale})
}
func (p *Playground) cancelControl() {
	if p.pressed == "blend" {
		p.model.Blend = p.pressValue
	}
	if p.pressed == "lens" {
		p.lens = p.pressValue
	}
	p.pressed = ""
}
func (p *Playground) activate(id string) {
	switch id {
	case "lumen", "studio", "slate":
		p.preset, p.custom = id, nil
	case "fusion":
		p.model.Fusion = !p.model.Fusion
	case "snap":
		p.model.Snap = !p.model.Snap
	case "motion":
		p.model.Motion = !p.model.Motion
	case "reset":
		p.model.Reset()
	case "read":
		p.read = !p.read
	case "task":
		p.done = !p.done
	case "play":
		p.playing = !p.playing
	}
}
func (p *Playground) Handle(e experience.Event) bool {
	if p.closed {
		return false
	}
	if e.Kind == experience.PointerMove || e.Kind == experience.PointerDown || e.Kind == experience.PointerUp {
		if err := (fluid.Point{X: e.X, Y: e.Y}).Validate(); err != nil {
			return false
		}
		p.pointer, p.pointerActive = fluid.Point{X: e.X, Y: e.Y}, true
	}
	if e.Kind == experience.KeyboardCancel {
		return p.model.Handle(e)
	}
	if e.Kind == experience.PointerCancel {
		hadCapture := p.pressed != "" || p.pointerActive
		p.pointerActive = false
		p.cancelControl()
		return p.model.Handle(e) || hadCapture
	}
	if e.Kind == experience.KeyInput && e.Pressed && !e.Modifiers.Has(experience.ModControl) && !e.Modifiers.Has(experience.ModAlt) && !e.Modifiers.Has(experience.ModSuper) {
		if e.Key == experience.KeyEscape && p.pressed != "" {
			p.cancelControl()
			return true
		}
		if (e.Key == "Enter" || e.Key == experience.KeySpace) && !p.model.Dragging() && p.model.Active >= 0 {
			if id := panelButton(p.model.Panels[p.model.Active].ID); id != "" {
				if !e.Repeat {
					p.activate(id)
				}
				return true
			}
		}
		for key, id := range map[experience.Key]string{"F": "fusion", "S": "snap", "M": "motion", "1": "lumen", "2": "studio", "3": "slate", "R": "reset"} {
			if e.Key == key {
				if !e.Repeat {
					p.activate(id)
				}
				return true
			}
		}
	}
	e.X, e.Y = (e.X-p.left)/p.scale, (e.Y-p.top)/p.scale
	if p.model.Dragging() {
		return p.model.Handle(e)
	}
	p.buildControls()
	if p.pressed != "" {
		id := p.pressed
		if e.Kind == experience.PointerMove || e.Kind == experience.PointerUp && e.Button == experience.ButtonPrimary {
			for _, c := range p.controls {
				if c.id == id {
					if id == "blend" || id == "lens" {
						v := max(0, min(1, (e.X-c.bounds.X)/c.bounds.Width))
						if id == "blend" {
							p.model.Blend = v * 64
						} else {
							p.lens = v
						}
					}
					if e.Kind == experience.PointerUp {
						if id != "blend" && id != "lens" && c.bounds.Contains(fluid.Point{X: e.X, Y: e.Y}) {
							p.activate(id)
						}
						p.pressed = ""
					}
				}
			}
		}
		return true
	}
	if e.Kind == experience.PointerDown && e.Button == experience.ButtonPrimary {
		for _, c := range p.controls {
			if p.controlHit(c, fluid.Point{X: e.X, Y: e.Y}) {
				for i, panel := range p.model.Panels {
					if panelButton(panel.ID) == c.id {
						p.model.front(i)
						break
					}
				}
				p.pressed = c.id
				if c.id == "blend" {
					p.pressValue = p.model.Blend
					p.model.Blend = max(0, min(1, (e.X-c.bounds.X)/c.bounds.Width)) * 64
				}
				if c.id == "lens" {
					p.pressValue = p.lens
					p.lens = max(0, min(1, (e.X-c.bounds.X)/c.bounds.Width))
				}
				return true
			}
		}
	}
	return p.model.Handle(e)
}

func (p *Playground) controlHit(c uiControl, point fluid.Point) bool {
	if !c.bounds.Contains(point) {
		return false
	}
	if c.id != "read" && c.id != "task" && c.id != "play" {
		return true
	}
	// A covered button must not intercept input meant for the panel on top.
	for i := len(p.model.order) - 1; i >= 0; i-- {
		panel := p.model.Panels[p.model.order[i]]
		if panel.Bounds.Contains(point) {
			return panelButton(panel.ID) == c.id
		}
	}
	return false
}

type document struct {
	Version int             `json:"version"`
	Layout  json.RawMessage `json:"layout"`
	Preset  string          `json:"preset"`
	Lens    float32         `json:"lens"`
	Playing bool            `json:"playing"`
	Done    bool            `json:"done"`
	Read    bool            `json:"read"`
	Skin    *skin.Skin      `json:"skin,omitempty"`
}

func (p *Playground) SaveState() ([]byte, error) { return p.CheckpointState() }
func (p *Playground) CheckpointState() ([]byte, error) {
	model := *p.model
	if p.pressed == "blend" {
		model.Blend = p.pressValue
	}
	layout, err := model.CheckpointState()
	if err != nil {
		return nil, err
	}
	lens := p.lens
	if p.pressed == "lens" {
		lens = p.pressValue
	}
	return json.Marshal(document{1, layout, p.preset, lens, p.playing, p.done, p.read, p.custom})
}
func (p *Playground) LoadState(data []byte) error {
	var d document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("plasma state must contain one JSON object")
	}
	if d.Version != 1 || d.Preset != "lumen" && d.Preset != "studio" && d.Preset != "slate" || math.IsNaN(float64(d.Lens)) || math.IsInf(float64(d.Lens), 0) || d.Lens < 0 || d.Lens > 1 {
		return fmt.Errorf("invalid plasma appearance")
	}
	if d.Skin != nil {
		if err := d.Skin.Validate(); err != nil {
			return err
		}
	}
	model := NewModel()
	if err := model.LoadState(d.Layout); err != nil {
		return err
	}
	p.model, p.preset, p.lens, p.playing, p.done, p.read, p.custom = model, d.Preset, d.Lens, d.Playing, d.Done, d.Read, d.Skin
	p.pressed = ""
	p.pointerActive = false
	p.buildControls()
	return nil
}
func (p *Playground) SetSkin(s skin.Skin) error {
	if err := s.Validate(); err != nil {
		return err
	}
	own := s.Clone()
	p.custom = &own
	return nil
}

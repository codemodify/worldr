package workspace

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const panelWidth, panelHeight = 512, 320

var (
	panelDepthButton = box{32, 650, 208, 38}
	panelChart       = box{24, 140, 464, 116}
	panelPlay        = box{24, 272, 104, 34}
	panelDepth       = box{296, 272, 192, 34}
)

// The instrument is a native content producer. Its texture contains actual
// study state, while placement, visibility and input belong to the 3D scene.
// It is opaque by design: the current surface contract has whole-quad hit areas.
type instrumentPanel struct {
	texture       *render.Texture
	base, image   *image.RGBA
	faces         map[int]font.Face
	selected      int
	tick          int64
	clock         float64
	playing, back bool
	embedded      bool
	initialized   bool
}

func newInstrumentPanel() (*instrumentPanel, error) {
	p := &instrumentPanel{base: image.NewRGBA(image.Rect(0, 0, panelWidth, panelHeight)), image: image.NewRGBA(image.Rect(0, 0, panelWidth, panelHeight)), faces: make(map[int]font.Face), selected: -1}
	parsed, err := opentype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}
	for _, size := range []int{18, 22, 26, 40} {
		face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingNone})
		if err != nil {
			p.close()
			return nil, err
		}
		p.faces[size] = face
	}
	panelRect(p.image, p.image.Bounds(), 0x102030)
	p.texture, err = render.NewTexture(panelWidth, panelHeight, p.image.Pix)
	if err != nil {
		p.close()
		return nil, err
	}
	return p, nil
}

func (p *instrumentPanel) close() {
	for _, face := range p.faces {
		_ = face.Close()
	}
	p.faces = nil
}

// A fixed world basis faces the initial camera. Orbiting exposes real spatial
// perspective; the panel is not a screen overlay or a camera-facing billboard.
func panelTransform(behind bool) scene.Mat4 {
	yaw, pitch := float64(initialModel().yaw), float64(initialModel().pitch)
	right := scene.Vec3{X: float32(math.Sin(yaw)), Z: -float32(math.Cos(yaw))}
	normal := scene.Vec3{X: float32(math.Cos(pitch) * math.Cos(yaw)), Y: float32(math.Sin(pitch)), Z: float32(math.Cos(pitch) * math.Sin(yaw))}
	up := normal.Cross(right)
	depth := float32(1.75)
	if behind {
		depth = -1.4
	}
	center := right.Mul(2.05).Add(up.Mul(-1.55)).Add(normal.Mul(depth))
	orientation := scene.Mat4{right.X, right.Y, right.Z, 0, up.X, up.Y, up.Z, 0, normal.X, normal.Y, normal.Z, 0, center.X, center.Y, center.Z, 1}
	return orientation.Mul(scene.Scale(3.8, 3.8*panelHeight/panelWidth, 1))
}

func (p *instrumentPanel) update(m model) {
	p.updateMode(m, false)
}

func (p *instrumentPanel) updateEmbedded(m model) {
	p.updateMode(m, true)
}

func (p *instrumentPanel) updateMode(m model, embedded bool) {
	// The engine can animate at display rate while this instrument refreshes
	// its text/marker at 20 Hz. Paused surfaces produce no uploads at all.
	tick := int64(m.clock * 20)
	selectionChanged := p.selected != m.selected || p.embedded != embedded
	if p.initialized && !selectionChanged && p.tick == tick && (m.playing || p.clock == m.clock) && p.playing == m.playing && p.back == m.panelBehind {
		return
	}
	if selectionChanged {
		p.drawBase(m.selected, embedded)
	}
	copy(p.image.Pix, p.base.Pix)
	p.text(p.image, 24, 125, 40, fmt.Sprintf("%.3f", signal(m.selected, m.clock)), teal)
	p.text(p.image, 153, 122, 18, "normalized", muted)
	p.text(p.image, 354, 118, 22, fmt.Sprintf("%05.2f s", m.clock), ink)
	x := 24 + int(m.clock/duration*464)
	panelRect(p.image, image.Rect(x, 146, x+2, 234), 0xa18055)
	y := 146 + int((1-signal(m.selected, m.clock))*88)
	panelRect(p.image, image.Rect(x-3, y-3, x+4, y+4), amber)
	panelRect(p.image, image.Rect(24, 272, 128, 306), 0x1c3548)
	label := "PAUSE"
	if !m.playing {
		label = "PLAY"
	}
	p.text(p.image, 40, 296, 22, label, teal)
	panelRect(p.image, image.Rect(296, 272, 488, 306), 0x1c3548)
	label = "SEND BACK"
	if embedded {
		label = "EXPLODE"
		if m.exploded {
			label = "ASSEMBLE"
		}
	} else if m.panelBehind {
		label = "BRING FORWARD"
	}
	p.text(p.image, 309, 296, 18, label, teal)
	// The header and response curve are cached per selected component. Only
	// the data/control portion is damaged while the shared clock advances.
	dirty := image.Rect(0, 84, panelWidth, panelHeight)
	if selectionChanged || !p.initialized {
		dirty = p.image.Bounds()
	}
	_ = p.texture.Update(dirty, p.image.Pix[dirty.Min.Y*p.image.Stride:])
	p.selected, p.tick, p.playing, p.back, p.initialized = m.selected, tick, m.playing, m.panelBehind, true
	p.clock = m.clock
	p.embedded = embedded
}

func (p *instrumentPanel) drawBase(selected int, embedded bool) {
	panelRect(p.base, p.base.Bounds(), 0x102030)
	panelRect(p.base, image.Rect(0, 0, panelWidth, 3), teal)
	panelRect(p.base, image.Rect(0, panelHeight-2, panelWidth, panelHeight), 0x385b74)
	p.text(p.base, 24, 34, 22, "AXIAL / 07", ink)
	mode := "LIVE SURFACE"
	if embedded {
		mode = "NATIVE 3D APP"
	}
	p.text(p.base, 331, 34, 18, mode, teal)
	p.text(p.base, 24, 73, 26, components[selected].name, ink)
	panelRect(p.base, image.Rect(24, 86, 488, 87), 0x28445d)
	for i := 0; i <= 3; i++ {
		y := 146 + i*88/3
		panelRect(p.base, image.Rect(24, y, 488, y+1), 0x233d53)
	}
	previous := 146 + int((1-signal(selected, 0))*88)
	for x := 25; x <= 488; x++ {
		y := 146 + int((1-signal(selected, float64(x-24)/464*duration))*88)
		lo, hi := previous, y
		if lo > hi {
			lo, hi = hi, lo
		}
		panelRect(p.base, image.Rect(x-1, lo, x+1, hi+2), teal)
		previous = y
	}
	p.text(p.base, 24, 255, 18, "Synthetic signal / drag chart to seek", muted)
}

func (p *instrumentPanel) text(dst *image.RGBA, x, y, size int, value string, rgb uint32) {
	d := font.Drawer{Dst: dst, Src: image.NewUniform(panelColor(rgb)), Face: p.faces[size], Dot: fixed.P(x, y)}
	d.DrawString(value)
}

func panelColor(rgb uint32) color.RGBA {
	return color.RGBA{R: uint8(rgb >> 16), G: uint8(rgb >> 8), B: uint8(rgb), A: 255}
}

func panelRect(dst *image.RGBA, r image.Rectangle, rgb uint32) {
	draw.Draw(dst, r, image.NewUniform(panelColor(rgb)), image.Point{}, draw.Src)
}

func panelAction(x, y float32) (Action, bool) {
	if panelPlay.contains(x, y) {
		return Action{Kind: TogglePlayback}, true
	}
	if panelDepth.contains(x, y) {
		return Action{Kind: TogglePanelDepth}, true
	}
	return Action{}, false
}

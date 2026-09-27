package app

import (
	"fmt"
	"image"
	"image/draw"
	"math"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
)

const (
	experimentBar       = -4
	experimentNoHit     = -3
	experimentButton    = -2
	experimentOutside   = -1
	experimentRowHeight = 46
)

type experimentMenuVisual struct {
	size                   image.Point
	scale                  float32
	open, hovered, busy    bool
	focus, scroll, visible int
	current, status        string
}

// experimentMenu is a host overlay: experiences retain ownership of their
// atlas, input, and borrowed draw slices. Its textures change only with UI state.
type experimentMenu struct {
	open, busy                                            bool
	status, current                                       string
	items                                                 []experimentDefinition
	painter                                               *nativeui.Painter
	scale                                                 float32
	width, height, focus, hover, pressed, scroll, visible int
	shortcut                                              bool
	pressedButton                                         experience.Button
	button, panel                                         image.Rectangle // logical coordinates
	bar                                                   image.Rectangle // framebuffer coordinates
	buttonTexture, panelTexture, barTexture               *render.Texture
	buttonKey, panelKey, barKey                           experimentMenuVisual
	commands                                              []render.Command
	retired                                               []uint64
}

func newExperimentMenu(current string) (*experimentMenu, error) {
	p, err := nativeui.NewPainter(nativeui.Cinematic())
	if err != nil {
		return nil, err
	}
	m := &experimentMenu{current: current, items: experimentCatalog(), painter: p, scale: 1, hover: experimentNoHit, pressed: experimentNoHit}
	m.SetCurrent(current)
	return m, nil
}

func (m *experimentMenu) Close() {
	if m.painter == nil {
		return
	}
	m.painter.Close()
	m.painter = nil
	for _, texture := range []*render.Texture{m.buttonTexture, m.panelTexture, m.barTexture} {
		if texture != nil {
			m.retired = append(m.retired, texture.ID())
		}
	}
	m.buttonTexture, m.panelTexture, m.barTexture, m.commands = nil, nil, nil, nil
}

func (m *experimentMenu) RetiredTextures() []uint64 {
	ids := m.retired
	m.retired = nil
	return ids
}

func (m *experimentMenu) SetCurrent(id string) {
	m.current, m.open, m.busy, m.status = id, false, false, ""
	m.pressed, m.hover = experimentNoHit, experimentNoHit
	for i, item := range m.items {
		if item.ID == id {
			m.focus = i
		}
	}
	m.revealFocus()
}

func (m *experimentMenu) layout(width, height int, scale float32) {
	if scale <= 0 || math.IsNaN(float64(scale)) || math.IsInf(float64(scale), 0) {
		scale = 1
	}
	scale = min(scale, 8)
	if m.width == width && m.height == height && m.scale == scale {
		return
	}
	m.width, m.height, m.scale = width, height, scale
	m.bar = image.Rect(0, m.ContentHeight(), width, height)
	m.pressed, m.hover = experimentNoHit, experimentNoHit
	w, h := int(float32(width)/scale), int(float32(height)/scale)
	m.button = image.Rect(12, max(0, h-44), min(w-12, 164), max(0, h-12))
	available := max(0, m.button.Min.Y-20)
	m.visible = min(len(m.items), max(0, (available-76)/experimentRowHeight))
	panelHeight := min(available, 76+m.visible*experimentRowHeight)
	m.panel = image.Rect(12, m.button.Min.Y-8-panelHeight, min(w-12, 432), m.button.Min.Y-8)
	m.revealFocus()
}

// The host draws and lays out the experience above the shared footer. Input
// coordinates retain their origin, so no application event transform is needed.
func (m *experimentMenu) ContentHeight() int {
	return max(0, m.height-int(math.Round(56*float64(m.scale))))
}

func (m *experimentMenu) revealFocus() {
	if m.focus < m.scroll {
		m.scroll = m.focus
	}
	if m.visible > 0 && m.focus >= m.scroll+m.visible {
		m.scroll = m.focus - m.visible + 1
	}
	m.scroll = max(0, min(m.scroll, len(m.items)-m.visible))
}

func (m *experimentMenu) hit(x, y float32) int {
	p := image.Pt(int(math.Floor(float64(x/m.scale))), int(math.Floor(float64(y/m.scale))))
	if p.In(m.button) {
		return experimentButton
	}
	if image.Pt(int(math.Floor(float64(x))), int(math.Floor(float64(y)))).In(m.bar) {
		return experimentBar
	}
	if !m.open || !p.In(m.panel) {
		return experimentOutside
	}
	row := (p.Y - m.panel.Min.Y - 46) / experimentRowHeight
	if p.Y >= m.panel.Min.Y+46 && row >= 0 && row < m.visible && row+m.scroll < len(m.items) {
		return row + m.scroll
	}
	return experimentNoHit
}

func (m *experimentMenu) Hovering(x, y float32) bool {
	hit := m.hit(x, y)
	return m.open || m.pressed != experimentNoHit || hit == experimentButton || hit == experimentBar
}

func (m *experimentMenu) toggle() {
	if m.busy {
		return
	}
	m.open = !m.open
	m.pressed, m.hover = experimentNoHit, experimentNoHit
	if m.open {
		m.revealFocus()
	}
}

func (m *experimentMenu) activate(index int) string {
	if m.busy || index < 0 || index >= len(m.items) {
		return ""
	}
	if m.items[index].ID == m.current {
		m.open = false
		return ""
	}
	return m.items[index].ID
}

func (m *experimentMenu) handle(e experience.Event) (bool, string) {
	if e.Kind == experience.KeyInput && e.Key == experience.KeyQ && e.Modifiers.Has(experience.ModControl|experience.ModAlt) {
		return false, ""
	}
	// XKB and repeat metadata must reach application bridges even under an
	// overlay, otherwise modifier/keymap state is stale when they regain focus.
	switch e.Kind {
	case experience.KeymapChanged, experience.KeyboardModifiers, experience.KeyboardRepeatInfo:
		return false, ""
	case experience.KeyboardCancel:
		if !m.busy {
			m.open = false
		}
		m.shortcut = false
		m.pressed, m.hover = experimentNoHit, experimentNoHit
		return false, ""
	case experience.PointerCancel:
		captured := m.open || m.pressed != experimentNoHit
		m.pressed, m.hover = experimentNoHit, experimentNoHit
		return captured, ""
	}
	if e.Kind == experience.KeyInput && e.Key == experience.KeyE {
		if !e.Pressed && m.shortcut {
			m.shortcut = false
			return true, ""
		}
		if e.Modifiers.Has(experience.ModControl | experience.ModAlt) {
			if e.Pressed && !e.Repeat {
				m.shortcut = true
				m.toggle()
			}
			return true, ""
		}
	}
	hit := m.hit(e.X, e.Y)
	switch e.Kind {
	case experience.PointerMove:
		m.hover = hit
		if m.open && hit >= 0 {
			m.focus = hit
		}
		return m.open || m.pressed != experimentNoHit || hit == experimentButton || hit == experimentBar, ""
	case experience.PointerDown:
		if !m.open && hit != experimentButton && hit != experimentBar {
			return false, ""
		}
		m.pressed, m.pressedButton = hit, e.Button
		if e.Button == experience.ButtonPrimary {
			if m.open && (hit == experimentOutside || hit == experimentBar) && !m.busy {
				m.open = false
			}
		}
		return true, ""
	case experience.PointerUp:
		captured := experimentNoHit
		if e.Button == m.pressedButton {
			captured, m.pressed, m.pressedButton = m.pressed, experimentNoHit, experience.ButtonNone
		}
		if captured != experimentNoHit && e.Button == experience.ButtonPrimary && captured == hit {
			if hit == experimentButton {
				m.toggle()
			} else if m.open && hit >= 0 {
				return true, m.activate(hit)
			}
		}
		return m.open || captured != experimentNoHit, ""
	case experience.PointerScroll:
		if m.open && e.ScrollY != 0 {
			delta := 1
			if e.ScrollY < 0 {
				delta = -1
			}
			m.scroll = max(0, min(len(m.items)-m.visible, m.scroll+delta))
			if m.visible > 0 {
				m.focus = max(m.scroll, min(m.focus, m.scroll+m.visible-1))
			}
			m.hover, m.pressed = experimentNoHit, experimentNoHit
		}
		return m.open || hit == experimentButton || hit == experimentBar, ""
	case experience.KeyInput:
		if !m.open {
			return false, ""
		}
		if !e.Pressed {
			return true, ""
		}
		switch e.Key {
		case experience.KeyEscape:
			if !m.busy {
				m.open = false
			}
			m.pressed = experimentNoHit
		case experience.KeyUp, experience.KeyDown, experience.KeyTab:
			delta := 1
			if e.Key == experience.KeyUp || (e.Key == experience.KeyTab && e.Modifiers.Has(experience.ModShift)) {
				delta = -1
			}
			if len(m.items) > 0 {
				m.focus = (m.focus + delta + len(m.items)) % len(m.items)
			}
			m.revealFocus()
		case experience.KeyEnter, experience.KeySpace:
			if !e.Repeat {
				return true, m.activate(m.focus)
			}
		}
		return true, ""
	case experience.TextCommit, experience.TextPreedit:
		return m.open, ""
	}
	return false, ""
}

func (m *experimentMenu) pixels(rect image.Rectangle) image.Rectangle {
	s := float64(m.scale)
	return image.Rect(int(math.Round(float64(rect.Min.X)*s)), int(math.Round(float64(rect.Min.Y)*s)), int(math.Round(float64(rect.Max.X)*s)), int(math.Round(float64(rect.Max.Y)*s)))
}

func (m *experimentMenu) raster(rect image.Rectangle, panel bool) *image.RGBA {
	bounds := m.pixels(rect)
	img := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	fill := func(r image.Rectangle, rgb uint32) {
		draw.Draw(img, m.pixels(r), image.NewUniform(nativeui.Color(rgb)), image.Point{}, draw.Src)
	}
	label := func(r image.Rectangle, size float64, text string, rgb uint32) {
		m.painter.Theme.FontSize = min(128, max(6, size*float64(m.scale)))
		_ = m.painter.DrawLabel(img, m.pixels(r), text, nativeui.Color(rgb))
	}
	w, h := rect.Dx(), rect.Dy()
	fill(image.Rect(0, 0, w, h), 0x364451)
	fill(image.Rect(1, 1, w-1, h-1), 0x101923)
	if !panel {
		if m.open || m.hover == experimentButton {
			fill(image.Rect(1, 1, w-1, h-1), 0x233747)
		}
		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				fill(image.Rect(12+x*7, 10+y*7, 17+x*7, 15+y*7), 0x93d8e7)
			}
		}
		label(image.Rect(36, 0, w-9, h), 13, "Experiments", 0xe8eff4)
		return img
	}
	label(image.Rect(14, 8, w-14, 29), 14, "EXPERIMENTS", 0xe8eff4)
	label(image.Rect(14, 28, w-14, 44), 10, "Explore every direction", 0x94a4b3)
	for row := 0; row < m.visible; row++ {
		i := row + m.scroll
		if i >= len(m.items) {
			break
		}
		item, y := m.items[i], 46+row*experimentRowHeight
		if i == m.focus {
			fill(image.Rect(7, y, w-7, y+experimentRowHeight-2), 0x243c4b)
			fill(image.Rect(7, y, 9, y+experimentRowHeight-2), 0x93d8e7)
		}
		label(image.Rect(17, y+4, 42, y+24), 11, fmt.Sprintf("%02d", i+1), 0x8198a9)
		titleEnd := w - 16
		if item.ID == m.current {
			titleEnd -= 66
			label(image.Rect(w-80, y+4, w-14, y+23), 9, "CURRENT", 0x93d8e7)
		}
		label(image.Rect(47, y+3, titleEnd, y+24), 13, item.Title, 0xe8eff4)
		label(image.Rect(47, y+23, w-16, y+40), 10, item.Description, 0x9aaebb)
	}
	footer := "Ctrl+Alt+E  /  Esc to close"
	if m.visible < len(m.items) {
		footer = fmt.Sprintf("%d-%d of %d  /  Scroll or arrow keys", m.scroll+1, min(len(m.items), m.scroll+m.visible), len(m.items))
	}
	if m.busy {
		footer = "Opening experiment..."
	}
	if m.status != "" {
		footer = m.status
	}
	fill(image.Rect(8, h-30, w-8, h-29), 0x2e3b48)
	label(image.Rect(14, h-27, w-14, h-4), 10, footer, 0x94a4b3)
	return img
}

func (m *experimentMenu) updateTexture(texture **render.Texture, rect image.Rectangle, panel bool) {
	if rect.Empty() {
		return
	}
	img := m.raster(rect, panel)
	if *texture == nil {
		*texture, _ = render.NewTexture(img.Rect.Dx(), img.Rect.Dy(), img.Pix)
	} else {
		_ = (*texture).Replace(img.Rect.Dx(), img.Rect.Dy(), img.Pix)
	}
}

func (m *experimentMenu) updateBar() {
	img := image.NewRGBA(image.Rectangle{Max: m.bar.Size()})
	draw.Draw(img, img.Rect, image.NewUniform(nativeui.Color(0x0b141d)), image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(0, 0, img.Rect.Dx(), max(1, int(m.scale))), image.NewUniform(nativeui.Color(0x293a48)), image.Point{}, draw.Src)
	w := int(float32(m.width) / m.scale)
	label := func(left, right int, text string, rgb uint32) {
		r := m.pixels(image.Rect(left, 0, right, 56)).Intersect(img.Rect)
		r.Max.X = min(r.Max.X, r.Min.X+4096)
		m.painter.Theme.FontSize = min(128, max(6, 12*float64(m.scale)))
		_ = m.painter.DrawLabel(img, r, text, nativeui.Color(rgb))
	}
	if w >= 448 {
		for i, item := range m.items {
			if item.ID == m.current {
				label(188, w-168, fmt.Sprintf("%02d / %s", i+1, item.Title), 0xacbac6)
				break
			}
		}
	}
	if w >= 320 {
		label(w-142, w-18, "Ctrl+Alt+E", 0x8297a8)
	}
	if m.barTexture == nil {
		m.barTexture, _ = render.NewTexture(img.Rect.Dx(), img.Rect.Dy(), img.Pix)
	} else {
		_ = m.barTexture.Replace(img.Rect.Dx(), img.Rect.Dy(), img.Pix)
	}
}

func (m *experimentMenu) append(frame render.Frame) render.Frame {
	if m.painter == nil || m.button.Empty() {
		return frame
	}
	barKey := experimentMenuVisual{size: m.bar.Size(), scale: m.scale, current: m.current}
	if barKey != m.barKey {
		m.updateBar()
		m.barKey = barKey
	}
	buttonKey := experimentMenuVisual{size: m.pixels(m.button).Size(), scale: m.scale, open: m.open, hovered: m.hover == experimentButton}
	if buttonKey != m.buttonKey {
		m.updateTexture(&m.buttonTexture, m.button, false)
		m.buttonKey = buttonKey
	}
	if m.open {
		panelKey := experimentMenuVisual{size: m.pixels(m.panel).Size(), scale: m.scale, focus: m.focus, scroll: m.scroll, visible: m.visible, current: m.current, busy: m.busy, status: m.status}
		if panelKey != m.panelKey {
			m.updateTexture(&m.panelTexture, m.panel, true)
			m.panelKey = panelKey
		}
	}
	previous := len(m.commands)
	m.commands = append(m.commands[:0], frame.Commands...)
	if previous > len(m.commands) {
		clear(m.commands[len(m.commands):previous])
	}
	add := func(texture *render.Texture, rect image.Rectangle) {
		if texture == nil || rect.Empty() {
			return
		}
		p := m.pixels(rect)
		m.commands = append(m.commands, render.Command{Kind: render.ImageCommand, Image: render.Image{Texture: texture, Bounds: [4]float32{float32(p.Min.X), float32(p.Min.Y), float32(p.Dx()), float32(p.Dy())}}})
	}
	if m.barTexture != nil {
		m.commands = append(m.commands, render.Command{Kind: render.ImageCommand, Image: render.Image{Texture: m.barTexture, Bounds: [4]float32{float32(m.bar.Min.X), float32(m.bar.Min.Y), float32(m.bar.Dx()), float32(m.bar.Dy())}}})
	}
	add(m.buttonTexture, m.button)
	if m.open {
		add(m.panelTexture, m.panel)
	}
	frame.Commands = m.commands
	return frame
}

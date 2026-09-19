package workspace

import (
	"fmt"
	"image"
	"image/draw"
	"strings"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/textinput"
)

var commandButton = box{215, 48, 310, 34}
var commandBounds = box{365, 195, 720, 506}

type commandEntry struct {
	label, kind, key string
	action           Action
}
type commandPalette struct {
	open, dirty            bool
	field                  *nativeui.Field
	input                  *textinput.Translator
	painter                *nativeui.Painter
	entries                []commandEntry
	selected, top, pressed int
	held                   map[uint32]bool
	image                  *image.RGBA
	texture                *render.Texture
	commands               []render.Command
	fingerprint            string
	epoch                  uint64
	clipboard              nativeui.FieldClipboard
}

func newCommandPalette() (*commandPalette, error) {
	input, err := textinput.New()
	if err != nil {
		return nil, err
	}
	painter, err := nativeui.NewPainter(nativeui.Cinematic())
	if err != nil {
		input.Close()
		return nil, err
	}
	return &commandPalette{field: nativeui.NewField(96), input: input, painter: painter, held: map[uint32]bool{}, image: image.NewRGBA(image.Rect(0, 0, 720, 506)), pressed: -1}, nil
}
func (p *commandPalette) close() { p.input.Close(); p.painter.Close() }
func (w *Workspace) openCommands() bool {
	w.resetApplicationReadClick()
	if w.commands == nil {
		p, err := newCommandPalette()
		if err != nil {
			w.Notify("Could not open tools: " + err.Error())
			return false
		}
		w.commands = p
		if w.commandKeymap.Kind == experience.KeymapChanged {
			p.input.Handle(w.commandKeymap)
		}
	}
	w.clearApplicationFocus()
	w.cancelPointer()
	w.helpOpen = false
	w.portals.open = false
	p := w.commands
	p.open, p.dirty = true, true
	p.epoch++
	p.field.Set("")
	p.selected, p.top, p.pressed = 0, 0, -1
	w.refreshCommands()
	return true
}
func (w *Workspace) refreshCommands() {
	p := w.commands
	if p == nil || !p.open {
		return
	}
	query := strings.TrimSpace(p.field.Text())
	q := strings.ToLower(query)
	var entries []commandEntry
	add := func(e commandEntry) {
		if q == "" || strings.Contains(strings.ToLower(e.label), q) {
			entries = append(entries, e)
		}
	}
	if catalog, ok := w.applications.(experience.ApplicationLaunchCatalog); ok {
		for _, choice := range catalog.ApplicationLaunches() {
			add(commandEntry{label: choice.Title, kind: choice.Kind})
		}
	}
	v := w.m.applicationState
	for i := range v.Spaces {
		if !v.spaceExists(uint8(i)) {
			continue
		}
		name := v.spaceName(uint8(i))
		add(commandEntry{label: "Space / " + name, action: Action{Kind: SwitchSpace, Space: uint8(i)}})
		if uint8(i) != v.Space && v.Selected != 0 {
			add(commandEntry{label: "Move selected/group to / " + name, action: Action{Kind: MoveToSpace, Space: uint8(i)}})
		}
	}
	for _, portal := range w.navigationPortals() {
		if portal.Applications == 0 {
			continue
		}
		label := fmt.Sprintf("Portal / %s / %s", portal.SpaceName, portal.Title)
		if portal.Applications > 1 {
			label += fmt.Sprintf(" / %d windows", portal.Applications)
		}
		add(commandEntry{label: label, action: Action{Kind: NavigatePortal, Space: portal.Space, ApplicationKey: portal.Key}})
	}
	for _, surface := range w.applicationSurfaces {
		i := v.index(surface.Key)
		if i >= 0 {
			add(commandEntry{label: "Window / " + surface.Title + " / " + v.spaceName(v.Layouts[i].Space), key: surface.Key})
		}
	}
	if validSpaceName(query) {
		exists := false
		for i := range v.Spaces {
			exists = exists || strings.EqualFold(v.spaceName(uint8(i)), query)
		}
		if !exists {
			entries = append(entries, commandEntry{label: "Create space / " + query, action: Action{Kind: CreateSpace, SpaceName: query}}, commandEntry{label: "Rename current space / " + query, action: Action{Kind: RenameSpace, Space: v.Space, SpaceName: query}})
		}
	}
	var fingerprint strings.Builder
	for _, e := range entries {
		fingerprint.WriteString(e.label)
		fingerprint.WriteByte(0)
	}
	if fingerprint.String() != p.fingerprint {
		p.dirty = true
		p.fingerprint = fingerprint.String()
	}
	p.entries = entries
	p.selected = max(0, min(p.selected, len(entries)-1))
	if p.selected < p.top {
		p.top = p.selected
	}
	if p.selected >= p.top+8 {
		p.top = p.selected - 7
	}
}
func (w *Workspace) chooseCommand() {
	p := w.commands
	if p.selected < 0 || p.selected >= len(p.entries) {
		return
	}
	e := p.entries[p.selected]
	p.open = false
	var err error
	switch {
	case e.key != "":
		err = w.ActivateApplication(e.key)
	case e.kind != "":
		launcher, ok := w.applications.(experience.ApplicationLauncher)
		if !ok {
			err = fmt.Errorf("tool launching is unavailable")
			break
		}
		if e.kind == "terminal" {
			w.launchTerminal(launcher)
			return
		}
		live := map[string]bool{}
		for _, surface := range w.applicationSurfaces {
			live[surface.Key] = true
		}
		var key string
		key, err = launcher.LaunchApplication(e.kind)
		if err == nil {
			if !live[key] {
				w.PlaceNewApplication(key)
			}
			err = w.ActivateApplication(key)
		}
	default:
		err = w.Dispatch(e.action)
		if err == nil && e.action.Kind == CreateSpace {
			for i, s := range w.m.applicationState.Spaces {
				if s.Name == e.action.SpaceName {
					err = w.Dispatch(Action{Kind: SwitchSpace, Space: uint8(i)})
					break
				}
			}
		}
	}
	if err != nil {
		w.Notify(err.Error())
	}
}
func (w *Workspace) handleCommands(e experience.Event) bool {
	if !w.desktop {
		return false
	}
	if e.Kind == experience.KeymapChanged {
		w.commandKeymap = e
	}
	p := w.commands
	if p != nil && e.Kind == experience.KeyInput && p.held[e.Keycode] && (!p.open || !e.Pressed || !e.Repeat || e.Keycode == 57 && e.Modifiers == experience.ModControl|experience.ModAlt) {
		if !e.Pressed {
			delete(p.held, e.Keycode)
		}
		return true
	}
	if e.Kind == experience.KeyInput && e.Pressed && !e.Repeat && e.Keycode == 57 && e.Modifiers == experience.ModControl|experience.ModAlt {
		if p != nil && p.open {
			p.open = false
		} else {
			w.openCommands()
		}
		if w.commands != nil {
			w.commands.held[e.Keycode] = true
		}
		return true
	}
	if p == nil || !p.open {
		if e.Kind == experience.PointerDown && e.Button == experience.ButtonPrimary && commandButton.contains((e.X-w.ox)/w.scale, (e.Y-w.oy)/w.scale) {
			return w.openCommands()
		}
		if p != nil && (e.Kind == experience.KeymapChanged || e.Kind == experience.KeyboardModifiers || e.Kind == experience.KeyboardCancel) {
			p.input.Handle(e)
		}
		return false
	}
	text := p.input.Handle(e)
	w.bindCommandClipboard()
	if p.clipboard.Handle(e) {
		p.dirty = true
		p.held[e.Keycode] = true
		w.refreshCommands()
		return true
	}
	if e.Kind == experience.KeyboardCancel || e.Kind == experience.PointerCancel {
		p.open = false
		p.held = map[uint32]bool{}
		return true
	}
	if e.Kind == experience.KeyInput {
		if !e.Pressed {
			return true
		}
		p.held[e.Keycode] = true
		switch e.Keycode {
		case 1:
			p.open = false
		case 28:
			if !e.Repeat {
				w.chooseCommand()
			}
		case 103:
			p.selected = max(0, p.selected-1)
			p.dirty = true
		case 108:
			p.selected = min(max(0, len(p.entries)-1), p.selected+1)
			p.dirty = true
		default:
			p.dirty = true
			if p.field.Handle(e, text) {
				p.selected, p.top, p.dirty = 0, 0, true
			}
		}
		w.refreshCommands()
		return true
	}
	if e.Kind == experience.TextCommit || e.Kind == experience.TextPreedit {
		p.field.Handle(e, e.Text)
		p.dirty = true
		w.refreshCommands()
		return true
	}
	x, y := (e.X-w.ox)/w.scale-commandBounds.x, (e.Y-w.oy)/w.scale-commandBounds.y
	switch e.Kind {
	case experience.PointerDown:
		if x < 0 || x > 720 || y < 0 || y > 506 {
			p.open = false
			return true
		}
		if y >= 113 && y < 449 {
			index := p.top + int((y-113)/42)
			if index < len(p.entries) {
				p.pressed = index
				p.selected = index
				p.dirty = true
			}
		} else if y >= 57 && y < 99 {
			_ = p.painter.PlaceCaret(p.field, image.Rect(22, 57, 698, 99), image.Pt(int(x), int(y)), e.Modifiers.Has(experience.ModShift))
			p.dirty = true
		}
	case experience.PointerUp:
		pressed := p.pressed
		p.pressed = -1
		if pressed >= 0 && x >= 22 && x < 698 && y >= 113 && y < 449 && p.top+int((y-113)/42) == pressed {
			w.chooseCommand()
		}
	case experience.PointerScroll:
		if e.ScrollY > 0 {
			p.selected = min(max(0, len(p.entries)-1), p.selected+1)
		} else {
			p.selected = max(0, p.selected-1)
		}
		p.dirty = true
		w.refreshCommands()
	}
	return true
}
func (w *Workspace) commandFrame(frame render.Frame) render.Frame {
	p := w.commands
	if p == nil || !p.open {
		return frame
	}
	w.refreshCommands()
	if p.dirty || p.texture == nil {
		theme := p.painter.Theme
		draw.Draw(p.image, p.image.Rect, image.NewUniform(theme.Background), image.Point{}, draw.Src)
		_ = p.painter.DrawLabel(p.image, image.Rect(22, 12, 698, 47), "TOOLS + SPACES / "+w.m.applicationState.spaceName(w.m.applicationState.Space), theme.Accent)
		_ = p.painter.DrawField(p.image, image.Rect(22, 57, 698, 99), p.field, "Find a tool, window or space", true)
		for i := p.top; i < len(p.entries) && i < p.top+8; i++ {
			y := 113 + (i-p.top)*42
			node := nativeui.Node{ID: fmt.Sprint(i), Role: nativeui.RoleMenuItem, Label: p.entries[i].label, Bounds: image.Rect(22, y, 698, y+38)}
			_ = p.painter.DrawButton(p.image, node, i == p.selected, false)
		}
		_ = p.painter.DrawLabel(p.image, image.Rect(22, 463, 698, 493), "Type to search or name a space · ↑↓ choose · Enter open · Esc close", theme.Muted)
		var err error
		if p.texture == nil {
			p.texture, err = render.NewTexture(720, 506, p.image.Pix)
		} else {
			err = p.texture.Replace(720, 506, p.image.Pix)
		}
		if err != nil {
			w.Notify(err.Error())
			return frame
		}
		p.dirty = false
	}
	p.commands = append(p.commands[:0], frame.Commands...)
	p.commands = append(p.commands, render.Command{Kind: render.ImageCommand, Image: render.Image{Texture: p.texture, Bounds: [4]float32{w.ox + commandBounds.x*w.scale, w.oy + commandBounds.y*w.scale, commandBounds.w * w.scale, commandBounds.h * w.scale}}})
	frame.Commands = p.commands
	return frame
}

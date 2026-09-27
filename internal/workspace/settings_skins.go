package workspace

import (
	"fmt"
	"strings"

	"github.com/codemodify/worldr/internal/render"
	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
	"github.com/codemodify/worldr/sdk/nativeui/v1/gallery"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

const (
	settingsTargetSkins      settingsTarget = 80
	settingsTargetSkinPreset settingsTarget = 90
	settingsTargetSkinAccent settingsTarget = 100
	settingsTargetSkinShape  settingsTarget = 110
	settingsTargetSkinFont   settingsTarget = 120
)

var settingsSkinsButton = box{160, 466, 218, 48}
var skinPresets = [...]string{"merrick", "advanced", "hologram", "plasma", "future-panels"}
var skinAccents = [...]skin.Color{"#63C9ED", "#A394EC", "#EABB6C", "#7FD8AB"}
var skinShapes = [...]string{"authored", "rounded", "chamfered", "notched"}

type skinSettingsPreview struct {
	texture  *render.Texture
	commands []render.Command
	dirty    bool
	err      string
}

func skinChoiceBox(row, column int) box {
	if row == 0 {
		return box{452 + float32(column)*162, 235, 154, 36}
	}
	return box{452 + float32(column)*202, 235 + float32(row)*48, 182, 36}
}

func (w *Workspace) skinSettingsTarget(x, y float32) settingsTarget {
	if w.settingsCategory != settingsSkins {
		return settingsTargetNone
	}
	for row, base := range []settingsTarget{settingsTargetSkinPreset, settingsTargetSkinAccent, settingsTargetSkinShape, settingsTargetSkinFont} {
		count := 4
		if row == 0 {
			count = len(skinPresets)
		}
		for i := 0; i < count; i++ {
			if skinChoiceBox(row, i).contains(x, y) {
				return base + settingsTarget(i)
			}
		}
	}
	return settingsTargetNone
}

func (w *Workspace) applySkinSettingsTarget(target settingsTarget) bool {
	if target == settingsTargetSkins {
		w.settingsCategory = settingsSkins
		return true
	}
	if target >= settingsTargetSkinPreset && target < settingsTargetSkinPreset+settingsTarget(len(skinPresets)) {
		preset, err := skin.Builtin(skinPresets[int(target-settingsTargetSkinPreset)])
		if err == nil {
			err = w.SetSkin(preset)
		}
		if err != nil {
			w.skinPreview.err = err.Error()
		}
		return true
	}
	current := w.CurrentSkin()
	if current == nil {
		preset, err := skin.Builtin("merrick")
		if err != nil {
			return false
		}
		current = &preset
	}
	switch {
	case target >= settingsTargetSkinAccent && target < settingsTargetSkinAccent+4:
		current.Palette["accent"] = skinAccents[int(target-settingsTargetSkinAccent)]
	case target >= settingsTargetSkinShape && target < settingsTargetSkinShape+4:
		kind := skinShapes[int(target-settingsTargetSkinShape)]
		if kind == "authored" {
			if original, err := skin.Builtin(current.ID); err == nil {
				current.Controls = original.Controls
			} else {
				return true
			}
		} else {
			for name, recipe := range current.Controls {
				if len(recipe.Layers) > 0 {
					g := skin.Geometry{Kind: kind, Radius: .3, Corner: .22, Notch: .2}
					switch name {
					case "panel", "card", "dialog", "popover", "tooltip":
						g.Radius, g.Corner, g.Notch = .045, .025, .025
					}
					recipe.Layers[0].Geometry = g
					current.Controls[name] = recipe
				}
			}
		}
	case target >= settingsTargetSkinFont && target < settingsTargetSkinFont+4:
		current.Typography.Size = float64(12 + int(target-settingsTargetSkinFont)*2)
		current.Typography.LineHeight = current.Typography.Size + 6
	default:
		return false
	}
	if err := w.SetSkin(*current); err != nil {
		w.skinPreview.err = err.Error()
	}
	return true
}

func (w *Workspace) drawSkinSettings() {
	w.drawSettingsPreviewHeader("SKINS", "WINDOWS + CONTROLS", "SELECT A COMPLETE APPEARANCE")
	current := w.activeSkin
	shapeChoice := 0
	if current != nil {
		for i, kind := range skinShapes[1:] {
			matches := len(current.Controls) > 0
			for _, recipe := range current.Controls {
				if len(recipe.Layers) == 0 || recipe.Layers[0].Geometry.Kind != kind {
					matches = false
					break
				}
			}
			if matches {
				shapeChoice = i + 1
				break
			}
		}
	}
	labels := [][]string{
		{"MERRICK", "ADVANCED", "HOLOGRAM", "PLASMA", "FUTURE PANELS"},
		{"ACCENT / ICE", "ACCENT / VIOLET", "ACCENT / AMBER", "ACCENT / MINT"},
		{"AUTHORED SHAPES", "ROUND CONTROLS", "CUT CONTROLS", "NOTCHED CONTROLS"},
		{"TYPE / 12 PX", "TYPE / 14 PX", "TYPE / 16 PX", "TYPE / 18 PX"},
	}
	for row, choices := range labels {
		for i, label := range choices {
			b := skinChoiceBox(row, i)
			active := current != nil && (row == 0 && current.ID == skinPresets[i] || row == 1 && current.Palette["accent"] == skinAccents[i] || row == 2 && shapeChoice == i || row == 3 && current.Typography.Size == float64(12+i*2))
			rgb := uint32(teal)
			if row == 1 {
				var v uint32
				_, _ = fmt.Sscanf(string(skinAccents[i]), "#%x", &v)
				rgb = v
			}
			alpha := float32(.18)
			if active {
				alpha = .72
			}
			w.rect(b.x, b.y, b.w, b.h, 0x07131f, .98)
			w.rect(b.x, b.y, 3, b.h, rgb, alpha)
			w.line(b.x, b.y+b.h, b.x+b.w, b.y+b.h, 1, rgb, alpha)
			w.text(b.x+12, b.y+12, 10, label, ink, .95)
		}
	}
	name := "SELECT A PRESET TO APPLY"
	if current != nil {
		name = strings.ToUpper(current.Name) + "  /  LIVE ON WINDOWS AND SUPPORTED APPS"
	}
	w.text(452, 423, 10, name, muted, .95)
	if w.skinPreview.err != "" {
		w.text(452, 788, 9, w.skinPreview.err, amber, .95)
	}
}

// The preview uses the public painter, so Settings displays the actual recipes
// and typography used by native applications, including imported packages.
func (w *Workspace) skinSettingsFrame(frame render.Frame) render.Frame {
	if !w.settingsOpen || w.settingsCategory != settingsSkins {
		return frame
	}
	p := &w.skinPreview
	if p.texture == nil || p.dirty {
		selected := w.CurrentSkin()
		if selected == nil {
			s, err := skin.Builtin("merrick")
			if err != nil {
				p.err = err.Error()
				return frame
			}
			selected = &s
		}
		theme, err := nativeui.ThemeFromSkin(*selected)
		if err != nil {
			p.err = err.Error()
			return frame
		}
		demo, err := gallery.New(theme)
		if err != nil {
			p.err = err.Error()
			return frame
		}
		defer demo.Close()
		pixels, err := demo.Render(790, 340)
		if err != nil {
			p.err = err.Error()
			return frame
		}
		if p.texture == nil {
			p.texture, err = render.NewTexture(790, 340, pixels.Pix)
		} else {
			err = p.texture.Replace(790, 340, pixels.Pix)
		}
		if err != nil {
			p.err = err.Error()
			return frame
		}
		p.dirty = false
		p.err = ""
	}
	p.commands = append(p.commands[:0], frame.Commands...)
	p.commands = append(p.commands, render.Command{Kind: render.ImageCommand, Image: render.Image{Texture: p.texture, Bounds: [4]float32{w.ox + 452*w.scale, w.oy + 442*w.scale, 790 * w.scale, 340 * w.scale}}})
	frame.Commands = p.commands
	return frame
}

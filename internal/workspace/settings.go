package workspace

import "github.com/codemodify/worldr/internal/experience"

// Settings is transient workspace chrome. Its category selection deliberately
// lives outside Document so inspecting another category never creates an edit
// or a saved-state change. Environment switches are desktop preferences and
// persist separately from the workspace's undoable document.
type settingsCategory uint8

const (
	settingsTerminal settingsCategory = iota
	settingsMedia
	settingsEnvironment
)

type settingsTarget uint8

const (
	settingsTargetNone settingsTarget = iota
	settingsTargetBackdrop
	settingsTargetClose
	settingsTargetTerminal
	settingsTargetMedia
	settingsTargetEnvironment
	settingsTargetDNA
	settingsTargetCat
	settingsTargetEyes
)

type settingsPointerCapture struct {
	active, dragged bool
	target          settingsTarget
	x, y            float32
	width, height   int
}

var (
	settingsBounds            = box{138, 78, 1164, 744}
	settingsCloseButton       = box{1234, 98, 44, 32}
	settingsTerminalButton    = box{160, 176, 218, 54}
	settingsMediaButton       = box{160, 242, 218, 54}
	settingsEnvironmentButton = box{160, 308, 218, 54}
	settingsPreviewBounds     = box{432, 222, 830, 430}
	settingsDNAToggle         = box{462, 256, 770, 92}
	settingsCatToggle         = box{462, 370, 770, 92}
	settingsEyesToggle        = box{462, 484, 770, 92}
)

func (w *Workspace) settingsButtonTarget(x, y float32) settingsTarget {
	switch {
	case settingsCloseButton.contains(x, y):
		return settingsTargetClose
	case settingsTerminalButton.contains(x, y):
		return settingsTargetTerminal
	case settingsMediaButton.contains(x, y):
		return settingsTargetMedia
	case settingsEnvironmentButton.contains(x, y):
		return settingsTargetEnvironment
	case w.settingsCategory == settingsEnvironment && settingsDNAToggle.contains(x, y):
		return settingsTargetDNA
	case w.settingsCategory == settingsEnvironment && settingsCatToggle.contains(x, y):
		return settingsTargetCat
	case w.settingsCategory == settingsEnvironment && settingsEyesToggle.contains(x, y):
		return settingsTargetEyes
	default:
		return settingsTargetBackdrop
	}
}

func (w *Workspace) cancelSettingsPointer() {
	w.settingsPointer = settingsPointerCapture{}
}

func (w *Workspace) claimSettingsStrokes() {
	if w.settingsKeys == nil {
		w.settingsKeys = make(map[helpKey]bool)
	}
	if w.settingsButtons == nil {
		w.settingsButtons = make(map[uint32]bool)
	}
	// If another modal or a client already owns a physical stroke, Settings
	// adopts its eventual release. Closing Settings can therefore never resume
	// an input sequence against a window underneath it.
	for key := range w.helpKeys {
		w.settingsKeys[key] = true
	}
	for button := range w.helpButtons {
		w.settingsButtons[button] = true
	}
	for button := range w.applicationButtons {
		w.settingsButtons[button] = true
	}
	for button := range w.windowDragButtons {
		w.settingsButtons[button] = true
	}
	if w.commands != nil {
		for code := range w.commands.held {
			w.settingsKeys[helpKey{code: code}] = true
		}
		w.commands.held = make(map[uint32]bool)
	}
	w.helpKeys = nil
	w.helpButtons = nil
}

func (w *Workspace) openSettings() {
	if !w.desktop {
		return
	}
	w.claimSettingsStrokes()
	w.resetApplicationReadClick()
	w.cancelPointer()
	w.clearApplicationFocus()
	w.applicationDockHover = -1
	w.cancelOrbitControl()
	w.cancelHelpPointer()
	w.helpOpen = false
	if w.commands != nil {
		w.commands.open = false
		w.commands.pressed = -1
	}
	w.cancelSettingsPointer()
	w.settingsOpen = true
}

// closeSettings leaves owned keys and buttons in their drain maps. A release
// after Escape or a close-button activation remains modal-owned, while the
// next fresh stroke can immediately reach the workspace.
func (w *Workspace) closeSettings() {
	w.settingsOpen = false
}

// handleSettings owns complete strokes while the overlay is open. The parent
// input router calls it before command/help/window routing for an open overlay
// or a captured Settings stroke, and before scene chrome for a new press.
func (w *Workspace) handleSettings(event experience.Event) bool {
	if w.settingsOpen && event.Kind == experience.KeymapChanged {
		// Keep the command palette's future text translator synchronized even
		// while Settings owns the rest of the keyboard stream.
		w.commandKeymap = event
		if w.commands != nil {
			w.commands.input.Handle(event)
		}
		return true
	}
	if event.Kind == experience.KeyboardCancel {
		w.settingsKeys = nil
		w.settingsButtons = nil
		w.cancelSettingsPointer()
		return false
	}
	if event.Kind == experience.PointerCancel {
		w.settingsButtons = nil
		w.cancelSettingsPointer()
		return false
	}

	if event.Kind == experience.KeyInput {
		stroke := helpStroke(event)
		owned := w.settingsKeys[stroke]
		if !event.Pressed && (owned || w.settingsOpen) {
			// Commands that began before Settings opened must release their
			// internal held state even though the modal owns the physical stroke.
			w.handleHeldOverviewKey(event)
			w.handleTerminalShortcut(event)
			w.handleOverviewShortcut(event)
		}
		if owned {
			if !event.Pressed {
				delete(w.settingsKeys, stroke)
			}
			return true
		}
		if w.settingsOpen {
			if event.Pressed {
				if w.settingsKeys == nil {
					w.settingsKeys = make(map[helpKey]bool)
				}
				w.settingsKeys[stroke] = true
				if !event.Repeat && event.Modifiers == 0 && (event.Key == experience.KeyEscape || event.Keycode == 1) {
					w.closeSettings()
				}
			} else {
				delete(w.settingsKeys, stroke)
			}
			return true
		}
	}

	x, y := (event.X-w.ox)/w.scale, (event.Y-w.oy)/w.scale
	button := applicationButton(event)
	ownedButton := w.settingsButtons[button]
	draining := false
	for _, owned := range w.settingsButtons {
		draining = draining || owned
	}
	if event.Kind == experience.PointerDown && button != 0 {
		if w.settingsButtons == nil {
			w.settingsButtons = make(map[uint32]bool)
		}
		w.settingsButtons[button] = w.settingsOpen || w.settingsPointer.active || draining
	}
	if event.Kind == experience.PointerUp {
		delete(w.settingsButtons, button)
	}
	if w.settingsPointer.active {
		p := &w.settingsPointer
		if w.width != p.width || w.height != p.height {
			w.cancelSettingsPointer()
			return true
		}
		if event.Kind == experience.PointerMove || event.Kind == experience.PointerUp {
			if abs(x-p.x)+abs(y-p.y) > 4 {
				p.dragged = true
			}
			if event.Kind == experience.PointerUp && button == 272 {
				target, activate := p.target, !p.dragged && w.settingsButtonTarget(x, y) == p.target
				w.cancelSettingsPointer()
				if activate {
					switch target {
					case settingsTargetClose:
						w.closeSettings()
					case settingsTargetTerminal:
						w.settingsCategory = settingsTerminal
					case settingsTargetMedia:
						w.settingsCategory = settingsMedia
					case settingsTargetEnvironment:
						w.settingsCategory = settingsEnvironment
					case settingsTargetDNA:
						w.environment.DNA = !w.environment.DNA
					case settingsTargetCat:
						w.environment.Cat = !w.environment.Cat
					case settingsTargetEyes:
						w.environment.Eyes = !w.environment.Eyes
					}
				}
			}
		}
		return true
	}
	if ownedButton && (event.Kind == experience.PointerDown || event.Kind == experience.PointerUp) ||
		draining && (event.Kind == experience.PointerDown || event.Kind == experience.PointerMove || event.Kind == experience.PointerScroll) {
		return true
	}
	if w.settingsOpen && event.Kind == experience.PointerDown && button == 272 {
		w.settingsButtons[button] = true
		w.settingsPointer = settingsPointerCapture{
			active: true, target: w.settingsButtonTarget(x, y), x: x, y: y,
			width: w.width, height: w.height,
		}
		return true
	}
	return w.settingsOpen
}

func (w *Workspace) drawSettings() {
	if !w.settingsOpen {
		return
	}
	// A dense scrim preserves a trace of the spatial scene while making the
	// modal preview the clear reading plane.
	w.rect(0, 0, 1440, 900, bg, .88)
	w.rect(settingsBounds.x-8, settingsBounds.y-8, settingsBounds.w+16, settingsBounds.h+16, 0x02070d, .72)
	w.rect(settingsBounds.x, settingsBounds.y, settingsBounds.w, settingsBounds.h, 0x0b1724, .985)
	w.rect(settingsBounds.x, settingsBounds.y, 4, settingsBounds.h, teal, .8)
	w.line(settingsBounds.x, settingsBounds.y, settingsBounds.x+settingsBounds.w, settingsBounds.y, 2, teal, .78)
	w.line(settingsBounds.x+28, 149, settingsBounds.x+settingsBounds.w-28, 149, 1, muted, .2)
	w.line(404, 149, 404, 790, 1, muted, .24)

	w.text(168, 103, 12, "WORLDR / CONTROL SURFACE", muted, 1)
	w.text(168, 124, 22, "SETTINGS", ink, 1)
	w.text(160, 158, 10, "CATEGORY", muted, .9)
	w.drawSettingsClose()
	w.drawSettingsCategory(settingsTerminalButton, "TERMINAL", "SHELL SURFACE", w.settingsCategory == settingsTerminal)
	w.drawSettingsCategory(settingsMediaButton, "MEDIA", "PLAYBACK SURFACE", w.settingsCategory == settingsMedia)
	w.drawSettingsCategory(settingsEnvironmentButton, "ENVIRONMENT", "AMBIENT SCENE", w.settingsCategory == settingsEnvironment)
	footer := "TRANSIENT PREVIEW"
	if w.settingsCategory == settingsEnvironment {
		footer = "SAVED WITH WORKSPACE"
	}
	w.text(160, 760, 10, footer, muted, .72)
	w.text(160, 779, 10, "ESC  CLOSE", muted, .72)

	switch w.settingsCategory {
	case settingsMedia:
		w.drawMediaSettingsPreview()
	case settingsEnvironment:
		w.drawEnvironmentSettingsPreview()
	default:
		w.drawTerminalSettingsPreview()
	}
}

func (w *Workspace) drawSettingsClose() {
	b := settingsCloseButton
	w.rect(b.x, b.y, b.w, b.h, teal, .055)
	w.line(b.x, b.y+b.h, b.x+b.w, b.y+b.h, 1, teal, .58)
	w.line(b.x+15, b.y+10, b.x+29, b.y+24, 1.4, teal, .9)
	w.line(b.x+29, b.y+10, b.x+15, b.y+24, 1.4, teal, .9)
}

func (w *Workspace) drawSettingsCategory(b box, title, subtitle string, active bool) {
	if active {
		w.rect(b.x, b.y, b.w, b.h, teal, .11)
		w.rect(b.x, b.y, 3, b.h, teal, .94)
		w.line(b.x, b.y, b.x+b.w, b.y, 1, teal, .38)
		w.line(b.x, b.y+b.h, b.x+b.w, b.y+b.h, 1, teal, .38)
	}
	color := muted
	if active {
		color = ink
	}
	w.text(b.x+18, b.y+10, 14, title, color, 1)
	w.text(b.x+18, b.y+32, 9, subtitle, muted, .78)
}

func (w *Workspace) drawSettingsPreviewHeader(kind, title, description string) {
	w.text(432, 174, 10, kind+" / APPEARANCE", muted, .9)
	w.text(432, 194, 20, title, ink, 1)
	w.text(762, 198, 10, description, muted, .82)
}

func (w *Workspace) drawPreviewFrame(b box) {
	w.rect(b.x, b.y, b.w, b.h, 0x06111d, .98)
	w.rect(b.x+8, b.y+8, b.w-16, b.h-16, 0x091925, .94)
	w.line(b.x, b.y, b.x+68, b.y, 2, teal, .82)
	w.line(b.x, b.y, b.x, b.y+68, 2, teal, .82)
	w.line(b.x+b.w-68, b.y, b.x+b.w, b.y, 2, teal, .82)
	w.line(b.x+b.w, b.y, b.x+b.w, b.y+68, 2, teal, .82)
	w.line(b.x, b.y+b.h-68, b.x, b.y+b.h, 2, teal, .82)
	w.line(b.x, b.y+b.h, b.x+68, b.y+b.h, 2, teal, .82)
	w.line(b.x+b.w-68, b.y+b.h, b.x+b.w, b.y+b.h, 2, teal, .82)
	w.line(b.x+b.w, b.y+b.h-68, b.x+b.w, b.y+b.h, 2, teal, .82)
}

func (w *Workspace) drawTerminalSettingsPreview() {
	w.drawSettingsPreviewHeader("TERMINAL", "CINEMATIC SHELL", "LIVE STYLE PREVIEW")
	b := settingsPreviewBounds
	w.drawPreviewFrame(b)
	w.rect(b.x+22, b.y+21, b.w-44, 36, 0x102b3a, .86)
	w.rect(b.x+22, b.y+21, 6, 36, teal, .82)
	w.text(b.x+45, b.y+31, 11, "TERMINAL / LOCAL SESSION", teal, 1)
	w.text(b.x+b.w-212, b.y+31, 10, "ENC  UTF-8    120x34", muted, .82)
	w.line(b.x+22, b.y+72, b.x+b.w-22, b.y+72, 1, muted, .2)
	w.text(b.x+42, b.y+102, 12, "user@worldr", teal, 1)
	w.text(b.x+146, b.y+102, 12, "~/projects/orbit", muted, 1)
	w.text(b.x+42, b.y+137, 13, "$  worldr inspect --live", ink, 1)
	w.text(b.x+42, b.y+176, 11, "SCENE LINK       ACTIVE", teal, .9)
	w.text(b.x+42, b.y+203, 11, "RENDER PATH      GPU / NATIVE", muted, .88)
	w.text(b.x+42, b.y+230, 11, "WORKSPACE        MAIN", muted, .88)
	w.text(b.x+42, b.y+274, 13, "$  _", ink, 1)
	w.line(b.x+42, b.y+300, b.x+b.w-42, b.y+300, 1, muted, .18)
	w.text(b.x+42, b.y+329, 10, "01  PROCESS", muted, .72)
	w.text(b.x+190, b.y+329, 10, "02  STREAM", muted, .72)
	w.text(b.x+338, b.y+329, 10, "03  SHELL", teal, .92)
	w.line(b.x+42, b.y+360, b.x+366, b.y+360, 2, teal, .35)
	w.line(b.x+366, b.y+360, b.x+b.w-42, b.y+360, 2, muted, .18)
}

func (w *Workspace) drawMediaSettingsPreview() {
	w.drawSettingsPreviewHeader("MEDIA", "CINEMATIC PLAYER", "LIVE STYLE PREVIEW")
	b := settingsPreviewBounds
	w.drawPreviewFrame(b)
	screen := box{b.x + 22, b.y + 21, b.w - 44, 292}
	w.rect(screen.x, screen.y, screen.w, screen.h, 0x07131f, 1)
	for i := 0; i < 6; i++ {
		x := screen.x + float32(i)*screen.w/5
		w.line(x, screen.y, x, screen.y+screen.h, .7, teal, .07)
	}
	for i := 0; i < 4; i++ {
		y := screen.y + float32(i)*screen.h/3
		w.line(screen.x, y, screen.x+screen.w, y, .7, teal, .07)
	}
	cx, cy := screen.x+screen.w/2, screen.y+screen.h/2
	w.circle(cx, cy, 42, 1.6, teal, .82)
	w.circle(cx, cy, 34, .8, teal, .32)
	w.line(cx-10, cy-17, cx-10, cy+17, 2.4, teal, .94)
	w.line(cx-10, cy-17, cx+19, cy, 2.4, teal, .94)
	w.line(cx+19, cy, cx-10, cy+17, 2.4, teal, .94)
	w.text(screen.x+18, screen.y+17, 10, "MEDIA / PREVIEW FEED", teal, .88)
	w.text(screen.x+screen.w-126, screen.y+17, 10, "00:42 / 03:18", muted, .82)
	w.line(b.x+34, b.y+338, b.x+b.w-34, b.y+338, 2, muted, .22)
	w.line(b.x+34, b.y+338, b.x+328, b.y+338, 2, teal, .92)
	w.circle(b.x+328, b.y+338, 4, 4, teal, 1)
	w.text(b.x+42, b.y+362, 12, "<<", muted, .9)
	w.text(b.x+103, b.y+362, 12, "||", teal, 1)
	w.text(b.x+162, b.y+362, 12, ">>", muted, .9)
	w.text(b.x+b.w-260, b.y+362, 10, "AUDIO  82%", muted, .82)
	w.line(b.x+b.w-145, b.y+371, b.x+b.w-42, b.y+371, 2, muted, .2)
	w.line(b.x+b.w-145, b.y+371, b.x+b.w-66, b.y+371, 2, teal, .86)
}

func (w *Workspace) drawEnvironmentSettingsPreview() {
	w.drawSettingsPreviewHeader("ENVIRONMENT", "AMBIENT SCENE", "LIVE WORKSPACE LAYERS")
	w.drawPreviewFrame(settingsPreviewBounds)
	w.drawEnvironmentToggle(settingsDNAToggle, "01", "DNA HELIX", "CENTERED RETAINED 3D LANDMARK", w.environment.DNA)
	w.drawEnvironmentToggle(settingsCatToggle, "02", "RUNNING CAT", "FREE 3D AMBIENT MOTION", w.environment.Cat)
	w.drawEnvironmentToggle(settingsEyesToggle, "03", "CURSOR EYES", "THREE EYES TRACK THE POINTER", w.environment.Eyes)
}

func (w *Workspace) drawEnvironmentToggle(b box, index, title, description string, enabled bool) {
	w.rect(b.x, b.y, b.w, b.h, 0x07131f, .96)
	w.line(b.x, b.y, b.x+b.w, b.y, 1, teal, .22)
	w.line(b.x, b.y+b.h, b.x+b.w, b.y+b.h, 1, teal, .16)
	w.rect(b.x, b.y, 4, b.h, teal, .48)
	w.text(b.x+22, b.y+18, 10, index, teal, .76)
	w.text(b.x+62, b.y+16, 14, title, ink, 1)
	w.text(b.x+62, b.y+45, 10, description, muted, .82)

	toggle := box{b.x + b.w - 112, b.y + 25, 76, 38}
	alpha := float32(.12)
	label, labelColor := "OFF", muted
	knobX := toggle.x + 19
	if enabled {
		alpha = .28
		label, labelColor = "ON", teal
		knobX = toggle.x + toggle.w - 19
	}
	w.rect(toggle.x, toggle.y, toggle.w, toggle.h, teal, alpha)
	w.line(toggle.x, toggle.y, toggle.x+toggle.w, toggle.y, 1, teal, .64)
	w.line(toggle.x, toggle.y+toggle.h, toggle.x+toggle.w, toggle.y+toggle.h, 1, teal, .42)
	w.circle(knobX, toggle.y+toggle.h/2, 8, 8, labelColor, .94)
	w.text(toggle.x-43, toggle.y+11, 10, label, labelColor, .9)
}

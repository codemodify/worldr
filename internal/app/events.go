package app

import (
	"fmt"
	"io"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/input"
	"github.com/codemodify/worldr/internal/platform/linux/host"
)

// A prepared layout is needed before the first input and after every extent
// change. Draw's borrowed storage is discarded here, before any submission.
func prepareInputLayout(work experience.Experience, width, height int) {
	work.Handle(experience.Event{Kind: experience.PointerCancel})
	work.Draw(width, height)
}

func dispatchEvent(work experience.Experience, e experience.Event, statePath string, out io.Writer, saveSession ...func() error) (bool, error) {
	if e.Kind == experience.KeyInput && e.Pressed && !e.Repeat && e.Key == experience.KeyQ &&
		e.Modifiers == experience.ModControl|experience.ModAlt {
		return true, nil
	}
	// A focused application owns its ordinary key bindings, including Ctrl+Q
	// and Ctrl+S. Ctrl+Alt+Q remains an explicit route out of worldr.
	if owner, ok := work.(experience.KeyboardOwner); ok && owner.OwnsKeyboard() &&
		(e.Kind == experience.KeyInput || e.Kind == experience.KeyboardModifiers || e.Kind == experience.KeyboardCancel) {
		work.Handle(e)
		return false, nil
	}
	if isHostShortcut(e, experience.KeyQ) {
		return true, nil
	}
	if isHostShortcut(e, experience.KeyS) {
		if statePath == "" {
			fmt.Fprintln(out, "save: supply --state PATH to choose a document file")
			return false, nil
		}
		// An unfinished gesture is a preview, not an undoable document edit.
		// Saving uses the same cancellation rule as normal exit.
		work.Handle(experience.Event{Kind: experience.PointerCancel})
		save := func() error { return saveState(statePath, work) }
		if len(saveSession) > 0 {
			save = saveSession[0]
		}
		if err := save(); err != nil {
			if len(saveSession) == 0 {
				return false, err
			}
			fmt.Fprintf(out, "save failed: %v\n", err)
			notifyWorkspace(work, "Could not save workspace: "+err.Error())
			return false, nil
		}
		fmt.Fprintf(out, "saved: %s\n", statePath)
		notifyWorkspace(work, "Workspace saved.")
		return false, nil
	}
	work.Handle(e)
	return false, nil
}

func isPointerTransition(e experience.Event) bool {
	return e.Kind == experience.PointerDown || e.Kind == experience.PointerUp
}

func isHostShortcut(e experience.Event, key experience.Key) bool {
	return e.Kind == experience.KeyInput && e.Pressed && !e.Repeat && e.Key == key &&
		e.Modifiers == experience.ModControl
}

func hostEvent(e host.Event) experience.Event {
	v := experience.Event{
		X: e.X, Y: e.Y, Modifiers: experience.Modifiers(e.Modifiers), Pressed: e.Pressed,
		Keycode: e.Keycode, ButtonCode: e.ButtonCode, Time: e.Time,
		Depressed: e.Depressed, Latched: e.Latched, Locked: e.Locked, Group: e.Group,
		ScrollX: e.ScrollX, ScrollY: e.ScrollY, Keymap: e.Keymap,
		RepeatRate: e.RepeatRate, RepeatDelay: e.RepeatDelay,
		Text: e.Text, TextContext: e.TextContext, PreeditBegin: e.PreeditBegin, PreeditEnd: e.PreeditEnd, DeleteBefore: e.DeleteBefore, DeleteAfter: e.DeleteAfter,
	}
	switch e.Kind {
	case host.Move:
		v.Kind = experience.PointerMove
	case host.Down:
		v.Kind, v.Button = experience.PointerDown, pointerButton(e.ButtonCode)
	case host.Up:
		v.Kind, v.Button = experience.PointerUp, pointerButton(e.ButtonCode)
	case host.Cancel:
		v.Kind = experience.PointerCancel
	case host.Key:
		v.Kind, v.Key = experience.KeyInput, symbolKey(e.Code)
	case host.Scroll:
		v.Kind = experience.PointerScroll
	case host.KeyboardCancel:
		v.Kind = experience.KeyboardCancel
	case host.KeymapChanged:
		v.Kind = experience.KeymapChanged
	case host.ModifiersChanged:
		v.Kind = experience.KeyboardModifiers
	case host.RepeatInfo:
		v.Kind = experience.KeyboardRepeatInfo
	case host.TextCommit:
		v.Kind = experience.TextCommit
	case host.TextPreedit:
		v.Kind = experience.TextPreedit
	}
	return v
}

func pointerButton(code uint32) experience.Button {
	switch code {
	case 0x110:
		return experience.ButtonPrimary
	case 0x111:
		return experience.ButtonSecondary
	case 0x112:
		return experience.ButtonMiddle
	}
	return experience.ButtonNone
}

// XKB keysyms belong at this platform boundary, never in an experience.
func symbolKey(code uint32) experience.Key {
	if code >= 'a' && code <= 'z' {
		code -= 'a' - 'A'
	}
	switch code {
	case 'B', 'C', 'E', 'F', 'G', 'O', 'P', 'R', 'S', 'Q', 'Z', 'Y', '1', '2', '3':
		return experience.Key(string(rune(code)))
	case ' ':
		return experience.KeySpace
	case 0xff1b:
		return experience.KeyEscape
	case 0xffbe:
		return experience.KeyF1
	case 0xff51:
		return experience.KeyLeft
	case 0xff53:
		return experience.KeyRight
	}
	return experience.KeyUnknown
}

// The direct-display diagnostic input path currently assumes a US keyboard.
// Nested input uses the compositor's XKB keymap, including layout and modifiers.
type keyState struct {
	held    [768]bool
	locked  uint32
	started time.Time
}

func (s *keyState) event(code uint32, pressed bool) experience.Event {
	if s.started.IsZero() {
		s.started = time.Now()
	}
	if code < uint32(len(s.held)) {
		if pressed && !s.held[code] {
			switch code {
			case 58:
				s.locked ^= 1 << 1 // Lock in the default US XKB keymap.
			case 69:
				s.locked ^= 1 << 4 // Mod2 (Num Lock).
			}
		}
		s.held[code] = pressed
	}
	mods, depressed := s.modifiers()
	key := map[uint32]experience.Key{
		1: experience.KeyEscape, 2: experience.Key1, 3: experience.Key2, 4: experience.Key3, 59: experience.KeyF1,
		16: experience.KeyQ, 18: experience.KeyE, 19: experience.KeyR, 21: experience.KeyY, 24: experience.KeyO, 25: experience.KeyP,
		34: experience.KeyG, 46: experience.KeyC,
		31: experience.KeyS, 33: experience.KeyF, 44: experience.KeyZ, 48: experience.KeyB, 57: experience.KeySpace,
		105: experience.KeyLeft, 106: experience.KeyRight,
	}[code]
	return experience.Event{
		Kind: experience.KeyInput, Key: key, Keycode: code, Modifiers: mods, Pressed: pressed,
		Time: uint32(time.Since(s.started) / time.Millisecond), Depressed: depressed, Locked: s.locked,
	}
}

func (s *keyState) modifiers() (experience.Modifiers, uint32) {
	var mods experience.Modifiers
	var depressed uint32
	if s.held[29] || s.held[97] {
		mods |= experience.ModControl
		depressed |= 1 << 2
	}
	if s.held[42] || s.held[54] {
		mods |= experience.ModShift
		depressed |= 1 << 0
	}
	if s.held[56] || s.held[100] {
		mods |= experience.ModAlt
		depressed |= 1 << 3
	}
	if s.held[125] || s.held[126] {
		mods |= experience.ModSuper
		depressed |= 1 << 6
	}
	return mods, depressed
}

// Conversion keeps each edge in its original queue position. In particular a
// click can acquire application focus before the next key in the same poll,
// and pointer modifiers describe the keys held at that edge, not at poll end.
func (s *keyState) directEvents(dst []experience.Event, events []input.Event) []experience.Event {
	for _, raw := range events {
		if raw.Kind == input.Cancel {
			*s = keyState{}
			dst = append(dst,
				experience.Event{Kind: experience.KeyboardCancel, Time: raw.Time},
				experience.Event{Kind: experience.PointerCancel, Time: raw.Time})
			continue
		}
		if raw.Kind == input.KeyInput {
			e := s.event(raw.Code, raw.Pressed)
			e.Time = raw.Time
			dst = append(dst, e)
			continue
		}
		mods, depressed := s.modifiers()
		e := experience.Event{
			X: float32(raw.X), Y: float32(raw.Y), Time: raw.Time,
			Modifiers: mods, Depressed: depressed, Locked: s.locked,
		}
		switch raw.Kind {
		case input.Move:
			e.Kind = experience.PointerMove
		case input.Down, input.Up:
			e.Kind = experience.PointerDown
			if raw.Kind == input.Up {
				e.Kind = experience.PointerUp
			}
			e.ButtonCode, e.Button = raw.Code, pointerButton(raw.Code)
			e.Pressed = raw.Kind == input.Down
		case input.Scroll:
			e.Kind, e.ScrollX, e.ScrollY = experience.PointerScroll, raw.ScrollX, raw.ScrollY
		default:
			continue
		}
		dst = append(dst, e)
	}
	return dst
}

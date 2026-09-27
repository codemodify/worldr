package app

import "github.com/codemodify/worldr/internal/platform/linux/host"

func windowEvent(e host.Event) Event {
	v := Event{
		X: e.X, Y: e.Y, Modifiers: Modifiers(e.Modifiers), Pressed: e.Pressed,
		Keycode: e.Keycode, ButtonCode: e.ButtonCode, Time: e.Time,
		Depressed: e.Depressed, Latched: e.Latched, Locked: e.Locked, Group: e.Group,
		ScrollX: e.ScrollX, ScrollY: e.ScrollY, Keymap: e.Keymap,
		RepeatRate: e.RepeatRate, RepeatDelay: e.RepeatDelay,
		Text: e.Text, TextContext: e.TextContext, PreeditBegin: e.PreeditBegin, PreeditEnd: e.PreeditEnd,
		DeleteBefore: e.DeleteBefore, DeleteAfter: e.DeleteAfter,
	}
	switch e.Kind {
	case host.Move:
		v.Kind = PointerMove
	case host.Down:
		v.Kind, v.Button = PointerDown, pointerButton(e.ButtonCode)
	case host.Up:
		v.Kind, v.Button = PointerUp, pointerButton(e.ButtonCode)
	case host.Cancel:
		v.Kind = PointerCancel
	case host.Key:
		v.Kind, v.Key = KeyInput, symbolKey(e.Code)
	case host.Scroll:
		v.Kind = PointerScroll
	case host.KeyboardCancel:
		v.Kind = KeyboardCancel
	case host.KeymapChanged:
		v.Kind = KeymapChanged
	case host.ModifiersChanged:
		v.Kind = KeyboardModifiers
	case host.RepeatInfo:
		v.Kind = KeyboardRepeatInfo
	case host.TextCommit:
		v.Kind = TextCommit
	case host.TextPreedit:
		v.Kind = TextPreedit
	}
	return v
}

func pointerButton(code uint32) Button {
	switch code {
	case 0x110:
		return ButtonPrimary
	case 0x111:
		return ButtonSecondary
	case 0x112:
		return ButtonMiddle
	}
	return ButtonNone
}

func symbolKey(code uint32) Key {
	if code >= 'a' && code <= 'z' {
		code -= 'a' - 'A'
	}
	if code >= 'A' && code <= 'Z' || code >= '0' && code <= '9' {
		return Key(string(rune(code)))
	}
	switch code {
	case ' ':
		return KeySpace
	case 0xff0d, 0xff8d:
		return KeyEnter
	case 0xff09, 0xfe20:
		return KeyTab
	case 0xff1b:
		return KeyEscape
	case 0xff08:
		return Key("Backspace")
	case 0xffff:
		return Key("Delete")
	case 0xff50:
		return Key("Home")
	case 0xff57:
		return Key("End")
	case 0xff55:
		return Key("PageUp")
	case 0xff56:
		return Key("PageDown")
	case 0xffbe:
		return KeyF1
	case 0xff51:
		return KeyLeft
	case 0xff53:
		return KeyRight
	case 0xff52:
		return KeyUp
	case 0xff54:
		return KeyDown
	}
	return KeyUnknown
}

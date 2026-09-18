//go:build !linux || !cgo

package textinput

import "github.com/codemodify/worldr/internal/experience"

// Translator uses a US physical-key fallback in builds without XKB support.
// Production Linux/cgo builds consume the host keymap and compose sequences.
type Translator struct{ closed bool }

func New() (*Translator, error) { return &Translator{}, nil }
func (t *Translator) Close() {
	if t != nil {
		t.closed = true
	}
}
func (t *Translator) Handle(event experience.Event) string {
	if t == nil || t.closed || event.Kind != experience.KeyInput || !event.Pressed || event.Modifiers&(experience.ModControl|experience.ModAlt|experience.ModSuper) != 0 {
		return ""
	}
	rows := []struct {
		first uint32
		plain string
		shift string
	}{{2, "1234567890-=", "!@#$%^&*()_+"}, {16, "qwertyuiop[]", "QWERTYUIOP{}"}, {30, "asdfghjkl;'`", "ASDFGHJKL:\"~"}, {43, "\\zxcvbnm,./", "|ZXCVBNM<>?"}, {57, " ", " "}}
	for _, row := range rows {
		if event.Keycode >= row.first && int(event.Keycode-row.first) < len(row.plain) {
			text := row.plain
			if event.Modifiers.Has(experience.ModShift) {
				text = row.shift
			}
			return text[event.Keycode-row.first : event.Keycode-row.first+1]
		}
	}
	return ""
}

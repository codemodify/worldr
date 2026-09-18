// Package textinput translates physical keyboard events into bounded committed
// UTF-8 for native text fields. It does not own focus, shortcuts or repeat timing.
package textinput

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func printable(text string) string {
	if len(text) > 512 || !utf8.ValidString(text) {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, text)
}

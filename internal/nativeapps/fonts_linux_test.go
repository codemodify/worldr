//go:build linux && cgo

package nativeapps

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFallbackRejectsNamedPipeWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "font-pipe")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readFallbackFont(path, maxFallbackFileBytes); err == nil {
		t.Fatal("named pipe accepted as font data")
	}
}

func TestSystemFontFallbackBoxDrawing(t *testing.T) {
	if os.Getenv("WORLDR_TEST_SYSTEM_FONTS") != "1" {
		t.Skip("set WORLDR_TEST_SYSTEM_FONTS=1 to check installed box-drawing fonts")
	}
	r, err := newTerminalRenderer(324, 246)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	for _, character := range []rune{'\u256d', '\u256e', '\u256f', '\u2570'} {
		for style := range r.faces {
			if _, embedded := r.faces[style].GlyphAdvance(character); embedded {
				t.Fatalf("U+%04X is no longer a missing embedded-font fixture", character)
			}
			face := r.fallback.face(character, style)
			if face == r.faces[style] {
				t.Fatalf("installed monospace fallback did not cover U+%04X style %d", character, style)
			}
			if _, _, ok := face.GlyphBounds(character); !ok {
				t.Fatalf("resolved U+%04X has no rasterizable outline", character)
			}
		}
	}
	matcher := newSystemFontMatcher()
	if matcher == nil {
		t.Fatal("second Fontconfig reference failed")
	}
	r.close()
	if _, ok := matcher.Match('\u2502', 0); !ok {
		t.Fatal("closing one renderer destroyed a still-live matcher")
	}
	matcher.Close()
	matcher.Close()
	if _, ok := matcher.Match('\u2502', 0); ok {
		t.Fatal("closed matcher remained active")
	}
	terminalFontconfig.Lock()
	defer terminalFontconfig.Unlock()
	if terminalFontconfig.refs != 0 || terminalFontconfig.config != nil {
		t.Fatal("last renderer left its private Fontconfig configuration alive")
	}
}

//go:build linux && cgo

package nativeui

import (
	"testing"
)

func TestPangoShapesArabicAndMovesCaretVisuallyInRTL(t *testing.T) {
	layout, err := newTextLayout("سلام", "DejaVu Sans", 24, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer layout.close()
	metrics := layout.metrics()
	isolatedWidth := 0
	for _, ch := range "سلام" {
		single, err := newTextLayout(string(ch), "DejaVu Sans", 24, -1)
		if err != nil {
			t.Fatal(err)
		}
		isolatedWidth += single.metrics().Width
		single.close()
	}
	if !metrics.Shaped || metrics.Width == isolatedWidth || metrics.Glyphs == 0 {
		t.Fatalf("Arabic was not contextually shaped: %+v", metrics)
	}
	if layout.caret(0).X <= layout.caret(len("سلام")).X {
		t.Fatal("RTL logical start did not appear on the right")
	}
	if position := visualMove("سلام", 0, -1); position <= 0 {
		t.Fatalf("visual left did not enter RTL text: %d", position)
	}
	mixed, err := newTextLayout("engine العربية 世界 é", "Sans", 18, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer mixed.close()
	if mixed.metrics().Runs < 3 {
		t.Fatalf("mixed scripts did not produce shaped fallback runs: %+v", mixed.metrics())
	}
	stops := graphemeStops("é👩‍💻")
	if len(stops) != 3 || stops[1] != len("é") || stops[2] != len("é👩‍💻") {
		t.Fatalf("Pango grapheme boundaries = %v", stops)
	}
}

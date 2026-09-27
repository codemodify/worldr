package nativeui

import (
	"crypto/sha256"
	"image"
	"image/color"
	"testing"
)

func TestPainterFullControlInventoryRendersForEveryThemeAndShape(t *testing.T) {
	seen := make(map[[32]byte]struct{})
	for _, family := range Families() {
		for _, grammar := range ShapeGrammars() {
			theme, _ := Builtin(family, grammar)
			painter, err := NewPainter(theme)
			if err != nil {
				t.Fatal(err)
			}
			canvas := image.NewRGBA(image.Rect(0, 0, 760, 660))
			calls := []struct {
				name string
				draw func() error
			}{
				{"panel", func() error {
					return painter.DrawPanel(canvas, Control{Bounds: image.Rect(8, 8, 752, 652), Label: "CONTROL GALLERY"})
				}},
				{"label", func() error {
					return painter.DrawLabel(canvas, image.Rect(28, 48, 300, 76), "Native controls / 世界", LabelStyle{})
				}},
				{"icon", func() error {
					return painter.DrawIcon(canvas, image.Rect(310, 48, 338, 76), IconSettings, color.RGBA{})
				}},
				{"button", func() error {
					return painter.DrawButton(canvas, Control{Bounds: image.Rect(28, 86, 198, 120), Label: "RUN SCAN", Icon: IconPlay, State: State{Focused: true}})
				}},
				{"icon button", func() error {
					return painter.DrawIconButton(canvas, Control{Bounds: image.Rect(210, 86, 246, 122), Icon: IconClose, State: State{Hovered: true}})
				}},
				{"field", func() error {
					return painter.DrawField(canvas, Control{Bounds: image.Rect(28, 136, 350, 172), Text: "orbital-vector", State: State{Focused: true}})
				}},
				{"text area", func() error {
					return painter.DrawTextArea(canvas, Control{Bounds: image.Rect(370, 86, 724, 172), Text: "First retained line.\nSecond line wraps safely inside the native surface."})
				}},
				{"switch", func() error {
					return painter.DrawSwitch(canvas, Control{Bounds: image.Rect(28, 188, 260, 224), Label: "LIVE LINK", State: State{Selected: true}})
				}},
				{"checkbox", func() error {
					return painter.DrawCheckbox(canvas, Control{Bounds: image.Rect(280, 188, 460, 224), Label: "SPECTRUM", State: State{Selected: true}})
				}},
				{"radio", func() error {
					return painter.DrawRadio(canvas, Control{Bounds: image.Rect(480, 188, 700, 224), Label: "CHANNEL 07", State: State{Selected: true}})
				}},
				{"slider", func() error {
					return painter.DrawSlider(canvas, Control{Bounds: image.Rect(28, 240, 420, 276), Label: "GAIN", Min: 0, Max: 100, Value: 63, Step: 1})
				}},
				{"progress", func() error {
					return painter.DrawProgress(canvas, Control{Bounds: image.Rect(440, 240, 700, 268), Min: 0, Max: 1, Value: .42, Label: "42%"})
				}},
				{"meter", func() error {
					return painter.DrawMeter(canvas, Control{Bounds: image.Rect(440, 278, 700, 306), Min: 0, Max: 1, Value: .88, Label: "LOAD"})
				}},
				{"tabs", func() error {
					return painter.DrawTabs(canvas, []Control{{Bounds: image.Rect(28, 292, 142, 326), Label: "DATA", State: State{Selected: true}}, {Bounds: image.Rect(144, 292, 258, 326), Label: "MODEL"}})
				}},
				{"segments", func() error {
					return painter.DrawSegments(canvas, []Control{{Bounds: image.Rect(280, 292, 390, 326), Label: "X"}, {Bounds: image.Rect(392, 292, 502, 326), Label: "Y", State: State{Selected: true}}})
				}},
				{"menu", func() error {
					return painter.DrawMenu(canvas, []Control{{Bounds: image.Rect(28, 342, 250, 370), Label: "Open signal", State: State{Hovered: true}}, {Bounds: image.Rect(28, 372, 250, 400), Label: "Archive"}})
				}},
				{"list", func() error {
					return painter.DrawList(canvas, []Control{{Bounds: image.Rect(270, 342, 490, 370), Label: "Observation 01", State: State{Selected: true}}})
				}},
				{"tree", func() error {
					return painter.DrawTree(canvas, []Control{{Bounds: image.Rect(500, 342, 724, 370), Label: "Telemetry", Indent: 1}})
				}},
				{"table", func() error {
					return painter.DrawTable(canvas, []Control{{Bounds: image.Rect(270, 372, 724, 400), Label: "07   VECTOR   93.4"}})
				}},
				{"scrollbar", func() error {
					return painter.DrawScrollbar(canvas, Control{Bounds: image.Rect(706, 414, 724, 570), Min: 0, Max: 100, Value: 30, Page: 20})
				}},
				{"splitter", func() error {
					return painter.DrawSplitter(canvas, Control{Bounds: image.Rect(358, 414, 366, 570), Min: 0, Max: 1, Value: .5})
				}},
				{"card", func() error {
					return painter.DrawCard(canvas, Control{Bounds: image.Rect(28, 414, 344, 470), Label: "CARD"})
				}},
				{"toolbar", func() error {
					return painter.DrawToolbar(canvas, Control{Bounds: image.Rect(380, 414, 690, 456), Label: "TOOLBAR"})
				}},
				{"dialog", func() error {
					return painter.DrawDialog(canvas, Control{Bounds: image.Rect(28, 486, 210, 552), Label: "DIALOG"})
				}},
				{"popover", func() error {
					return painter.DrawPopover(canvas, Control{Bounds: image.Rect(222, 486, 404, 552), Label: "POPOVER"})
				}},
				{"tooltip", func() error {
					return painter.DrawTooltip(canvas, Control{Bounds: image.Rect(416, 486, 598, 530), Label: "TOOLTIP"})
				}},
				{"badge", func() error {
					return painter.DrawBadge(canvas, Control{Bounds: image.Rect(610, 486, 704, 520), Label: "ACTIVE", State: State{Selected: true}})
				}},
				{"separator", func() error { return painter.DrawSeparator(canvas, Control{Bounds: image.Rect(28, 580, 700, 588)}) }},
			}
			for _, call := range calls {
				if err := call.draw(); err != nil {
					t.Fatalf("%s/%s %s: %v", family, grammar, call.name, err)
				}
			}
			signature := sha256.Sum256(canvas.Pix)
			if _, duplicate := seen[signature]; duplicate {
				t.Fatalf("%s/%s did not produce a distinct gallery", family, grammar)
			}
			seen[signature] = struct{}{}
		}
	}
}

func TestPainterClipsTextAndControlsAndRejectsInvalidText(t *testing.T) {
	painter, _ := NewPainter(DefaultTheme())
	canvas := image.NewRGBA(image.Rect(0, 0, 100, 60))
	marker := color.RGBA{R: 2, G: 3, B: 4, A: 255}
	for y := 0; y < 60; y++ {
		for x := 0; x < 100; x++ {
			canvas.SetRGBA(x, y, marker)
		}
	}
	bounds := image.Rect(20, 15, 70, 42)
	if err := painter.DrawButton(canvas, Control{Bounds: bounds, Label: "A very long Unicode button العربية 世界"}); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 60; y++ {
		for x := 0; x < 100; x++ {
			if !image.Pt(x, y).In(bounds) && canvas.RGBAAt(x, y) != marker {
				t.Fatalf("button painted outside bounds at %d,%d", x, y)
			}
		}
	}
	before := append([]byte(nil), canvas.Pix...)
	if err := painter.DrawLabel(canvas, bounds, string([]byte{0xff}), LabelStyle{}); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
	if string(before) != string(canvas.Pix) {
		t.Fatal("invalid label changed destination")
	}
	if err := painter.DrawButton(nil, Control{Bounds: bounds, Label: "x"}); err == nil {
		t.Fatal("nil destination was accepted")
	}
}

func TestEveryPublicDrawOperationClipsToRequestedBounds(t *testing.T) {
	theme := DefaultTheme()
	// The largest valid stroke exposes primitives which would otherwise appear
	// contained at ordinary control sizes.
	theme.Metrics.Stroke = 16
	painter, err := NewPainter(theme)
	if err != nil {
		t.Fatal(err)
	}

	tight := image.Rect(23, 17, 32, 24)
	thinHorizontal := image.Rect(20, 19, 44, 20)
	thinVertical := image.Rect(27, 13, 28, 39)
	control := func(bounds image.Rectangle) Control {
		return Control{
			ID: "control", Bounds: bounds, Label: "X", Text: "X\nY", Placeholder: "P", Icon: IconSettings,
			State: State{Selected: true, Focused: true}, Min: 0, Max: 1, Value: .5, Step: .1, Page: .2, Indent: 64,
		}
	}
	tests := []struct {
		name   string
		bounds image.Rectangle
		draw   func(*image.RGBA) error
	}{
		{"label", tight, func(dst *image.RGBA) error { return painter.DrawLabel(dst, tight, "X", LabelStyle{}) }},
		{"icon", tight, func(dst *image.RGBA) error { return painter.DrawIcon(dst, tight, IconSettings, color.RGBA{}) }},
		{"button", tight, func(dst *image.RGBA) error { return painter.DrawButton(dst, control(tight)) }},
		{"icon button", tight, func(dst *image.RGBA) error { return painter.DrawIconButton(dst, control(tight)) }},
		{"field", tight, func(dst *image.RGBA) error { return painter.DrawField(dst, control(tight)) }},
		{"text area", tight, func(dst *image.RGBA) error { return painter.DrawTextArea(dst, control(tight)) }},
		{"switch", tight, func(dst *image.RGBA) error { return painter.DrawSwitch(dst, control(tight)) }},
		{"checkbox", tight, func(dst *image.RGBA) error { return painter.DrawCheckbox(dst, control(tight)) }},
		{"radio", tight, func(dst *image.RGBA) error { return painter.DrawRadio(dst, control(tight)) }},
		{"slider", thinHorizontal, func(dst *image.RGBA) error { return painter.DrawSlider(dst, control(thinHorizontal)) }},
		{"progress", tight, func(dst *image.RGBA) error { return painter.DrawProgress(dst, control(tight)) }},
		{"meter", tight, func(dst *image.RGBA) error { return painter.DrawMeter(dst, control(tight)) }},
		{"tabs", tight, func(dst *image.RGBA) error { return painter.DrawTabs(dst, []Control{control(tight)}) }},
		{"segments", tight, func(dst *image.RGBA) error { return painter.DrawSegments(dst, []Control{control(tight)}) }},
		{"menu item", tight, func(dst *image.RGBA) error { return painter.DrawMenuItem(dst, control(tight)) }},
		{"list row", tight, func(dst *image.RGBA) error { return painter.DrawListRow(dst, control(tight)) }},
		{"tree row", tight, func(dst *image.RGBA) error { return painter.DrawTreeRow(dst, control(tight)) }},
		{"table row", tight, func(dst *image.RGBA) error { return painter.DrawTableRow(dst, control(tight)) }},
		{"menu", tight, func(dst *image.RGBA) error { return painter.DrawMenu(dst, []Control{control(tight)}) }},
		{"list", tight, func(dst *image.RGBA) error { return painter.DrawList(dst, []Control{control(tight)}) }},
		{"tree", tight, func(dst *image.RGBA) error { return painter.DrawTree(dst, []Control{control(tight)}) }},
		{"table", tight, func(dst *image.RGBA) error { return painter.DrawTable(dst, []Control{control(tight)}) }},
		{"scrollbar", tight, func(dst *image.RGBA) error { return painter.DrawScrollbar(dst, control(tight)) }},
		{"splitter", thinVertical, func(dst *image.RGBA) error { return painter.DrawSplitter(dst, control(thinVertical)) }},
		{"panel", tight, func(dst *image.RGBA) error { return painter.DrawPanel(dst, control(tight)) }},
		{"card", tight, func(dst *image.RGBA) error { return painter.DrawCard(dst, control(tight)) }},
		{"toolbar", tight, func(dst *image.RGBA) error { return painter.DrawToolbar(dst, control(tight)) }},
		{"dialog", tight, func(dst *image.RGBA) error { return painter.DrawDialog(dst, control(tight)) }},
		{"popover", tight, func(dst *image.RGBA) error { return painter.DrawPopover(dst, control(tight)) }},
		{"tooltip", tight, func(dst *image.RGBA) error { return painter.DrawTooltip(dst, control(tight)) }},
		{"badge", tight, func(dst *image.RGBA) error { return painter.DrawBadge(dst, control(tight)) }},
		{"separator", thinVertical, func(dst *image.RGBA) error { return painter.DrawSeparator(dst, control(thinVertical)) }},
	}

	marker := color.RGBA{R: 2, G: 3, B: 4, A: 255}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dst := image.NewRGBA(image.Rect(10, 8, 64, 54))
			for y := dst.Bounds().Min.Y; y < dst.Bounds().Max.Y; y++ {
				for x := dst.Bounds().Min.X; x < dst.Bounds().Max.X; x++ {
					dst.SetRGBA(x, y, marker)
				}
			}
			if err := test.draw(dst); err != nil {
				t.Fatal(err)
			}
			painted := 0
			for y := dst.Bounds().Min.Y; y < dst.Bounds().Max.Y; y++ {
				for x := dst.Bounds().Min.X; x < dst.Bounds().Max.X; x++ {
					if dst.RGBAAt(x, y) == marker {
						continue
					}
					painted++
					if !image.Pt(x, y).In(test.bounds) {
						t.Fatalf("painted outside %v at %d,%d", test.bounds, x, y)
					}
				}
			}
			if painted == 0 {
				t.Fatal("operation painted no pixels, so its clipping path was not exercised")
			}
		})
	}
}

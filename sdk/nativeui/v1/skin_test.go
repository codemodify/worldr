package nativeui

import (
	"bytes"
	"golang.org/x/image/font/gofont/gomono"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func TestSkinRecipesFontsAndIconsAreInterpreted(t *testing.T) {
	s, _ := skin.Builtin("plasma")
	s.ID = "dev.example.custom"
	s.Controls["button"] = skin.Recipe{Layers: []skin.Layer{{Geometry: skin.Geometry{Kind: "rect"}, Fill: "#8bdceaac"}}, TextColor: "text", ContentInsets: skin.Insets{Left: 17, Right: 11, Top: 3, Bottom: 4}}
	theme, err := ThemeFromSkin(s)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPainter(theme)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	dst := image.NewRGBA(image.Rect(0, 0, 100, 60))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	if err := p.DrawControlBackground(dst, "button", image.Rect(10, 10, 90, 50), State{}); err != nil {
		t.Fatal(err)
	}
	want := image.NewRGBA(image.Rect(0, 0, 1, 1))
	draw.Draw(want, want.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(want, want.Bounds(), image.NewUniform(color.NRGBA{139, 220, 234, 172}), image.Point{}, draw.Over)
	got := dst.RGBAAt(40, 30)
	expected := want.RGBAAt(0, 0)
	if absInt(int(got.R)-int(expected.R)) > 1 || absInt(int(got.G)-int(expected.G)) > 1 || absInt(int(got.B)-int(expected.B)) > 1 {
		t.Fatalf("straight alpha composited incorrectly: got %v want %v", got, expected)
	}
	if dst.RGBAAt(9, 30) != (color.RGBA{255, 255, 255, 255}) {
		t.Fatal("recipe escaped control bounds")
	}
	if got := p.ControlContentBounds("button", image.Rect(10, 10, 90, 50)); got != image.Rect(27, 13, 79, 46) {
		t.Fatalf("recipe insets ignored: %v", got)
	}
	smallWidth := p.textWidth("Worldr 012345")
	theme.Typography.Size *= 2
	theme.Typography.LineHeight *= 2
	if err := p.SetTheme(theme); err != nil {
		t.Fatal(err)
	}
	if p.textWidth("Worldr 012345") < smallWidth*18/10 {
		t.Fatal("font size changed metadata but not glyph metrics")
	}
	a, b := image.NewRGBA(image.Rect(0, 0, 40, 40)), image.NewRGBA(image.Rect(0, 0, 40, 40))
	if err := p.DrawNamedIcon(a, a.Bounds(), "window-close", color.RGBA{255, 255, 255, 255}); err != nil {
		t.Fatal(err)
	}
	s.Icons["window-close"] = skin.Icon{Paths: []skin.Path{{Points: []skin.Point{{X: .15, Y: .5}, {X: .85, Y: .5}}}}, StrokeWidth: 3}
	theme, _ = ThemeFromSkin(s)
	if err := p.SetTheme(theme); err != nil {
		t.Fatal(err)
	}
	_ = p.DrawNamedIcon(b, b.Bounds(), "window-close", color.RGBA{255, 255, 255, 255})
	if bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("custom icon paths did not change actual pixels")
	}
}

func TestSkinGradientAndImageLayersDrawActualContent(t *testing.T) {
	s, _ := skin.Builtin("merrick")
	s.Materials["test"] = skin.Material{Kind: "linear-gradient", Secondary: "#000000", Angle: 90}
	s.Controls["button"] = skin.Recipe{Layers: []skin.Layer{{Geometry: skin.Geometry{Kind: "rect"}, Fill: "#ffffff", Material: "test"}}}
	theme, _ := ThemeFromSkin(s)
	p, _ := NewPainter(theme)
	defer p.Close()
	dst := image.NewRGBA(image.Rect(0, 0, 40, 40))
	_ = p.DrawControlBackground(dst, "button", dst.Bounds(), State{})
	if dst.RGBAAt(20, 5).R <= dst.RGBAAt(20, 35).R+100 {
		t.Fatal("gradient material is a flat color")
	}
	asset := image.NewRGBA(image.Rect(0, 0, 2, 1))
	asset.Set(0, 0, color.RGBA{255, 0, 0, 255})
	asset.Set(1, 0, color.RGBA{0, 0, 255, 255})
	var pngData bytes.Buffer
	_ = png.Encode(&pngData, asset)
	s.Assets = map[string]skin.Asset{"two-color": {Kind: "image", MediaType: "image/png", Data: pngData.Bytes()}}
	r := s.Controls["button"]
	r.Layers[0].Asset = "two-color"
	r.Layers[0].Material = ""
	s.Controls["button"] = r
	theme, err := ThemeFromSkin(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetTheme(theme); err != nil {
		t.Fatal(err)
	}
	_ = p.DrawControlBackground(dst, "button", dst.Bounds(), State{})
	if dst.RGBAAt(5, 20).R < 240 || dst.RGBAAt(35, 20).B < 240 {
		t.Fatal("embedded image layer did not render its content")
	}
}

func TestEverySkinUsesTheSameSliderGeometryForPaintAndInput(t *testing.T) {
	for _, s := range skin.Builtins() {
		t.Run(s.ID, func(t *testing.T) {
			theme, err := ThemeFromSkin(s)
			if err != nil {
				t.Fatal(err)
			}
			p, err := NewPainter(theme)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			var c Controller
			defer c.Close()
			if err := c.SetTheme(theme); err != nil {
				t.Fatal(err)
			}
			for _, value := range []float64{0, 25, 50, 100} {
				control := Control{ID: "gain", Kind: KindSlider, Bounds: image.Rect(20, 10, 280, 54), Label: "GAIN", Min: 0, Max: 100, Value: value, Step: 1}
				if err := c.SetControls([]Control{control}); err != nil {
					t.Fatal(err)
				}
				track := p.Layout(control).Track
				x := track.Min.X + int(float64(track.Dx()-1)*value/100+.5)
				action := c.Handle(nativeapp.Event{Kind: nativeapp.PointerDown, Button: nativeapp.ButtonPrimary, X: float32(x), Y: 30})
				if action.Value != value || action.Changed {
					t.Fatalf("clicking knob at value %.0f changed it to %.0f (track %v)", value, action.Value, track)
				}
				c.Handle(nativeapp.Event{Kind: nativeapp.PointerCancel})
			}
		})
	}
}

func TestScrollbarGrabPreservesItsValue(t *testing.T) {
	var c Controller
	defer c.Close()
	p, _ := NewPainter(DefaultTheme())
	defer p.Close()
	control := Control{ID: "scroll", Kind: KindScrollbar, Bounds: image.Rect(0, 0, 20, 200), Min: 0, Max: 100, Value: 50, Page: 100, Step: 1}
	_ = c.SetControls([]Control{control})
	thumb := p.Layout(control).Thumb
	action := c.Handle(nativeapp.Event{Kind: nativeapp.PointerDown, Button: nativeapp.ButtonPrimary, X: 10, Y: float32(thumb.Min.Y + 5)})
	if action.Changed || action.Value != 50 {
		t.Fatalf("scrollbar jumped on grab: %+v", action)
	}
}

func TestLegacyRGBAHelperReturnsPremultipliedColors(t *testing.T) {
	c := RGBA(0x8bdceaac)
	if c.R > c.A || c.G > c.A || c.B > c.A {
		t.Fatalf("invalid premultiplied color: %v", c)
	}
	want := color.RGBAModel.Convert(color.NRGBA{139, 220, 234, 172}).(color.RGBA)
	if c != want {
		t.Fatalf("RGBA helper got %v want %v", c, want)
	}
}

func TestPartialSkinUsesNonrecursiveControlFallback(t *testing.T) {
	s, _ := skin.Builtin("plasma")
	s.Controls = map[string]skin.Recipe{"panel": s.Controls["panel"]}
	theme, err := ThemeFromSkin(s)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPainter(theme)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	dst := image.NewRGBA(image.Rect(0, 0, 80, 40))
	if err := p.DrawButton(dst, Control{Bounds: dst.Bounds(), Label: "Go"}); err != nil {
		t.Fatal(err)
	}
	if dst.RGBAAt(20, 20).A == 0 {
		t.Fatal("missing recipe produced no fallback surface")
	}
}

func TestSplitterDragsPerpendicularToItsDivider(t *testing.T) {
	var c Controller
	defer c.Close()
	control := Control{ID: "split", Kind: KindSplitter, Bounds: image.Rect(198, 0, 202, 300), RangeBounds: image.Rect(0, 0, 400, 300), Min: 0, Max: 1, Value: .5, Step: .01}
	if err := c.SetControls([]Control{control}); err != nil {
		t.Fatal(err)
	}
	first := c.Handle(nativeapp.Event{Kind: nativeapp.PointerDown, Button: nativeapp.ButtonPrimary, X: 200, Y: 120})
	if first.Changed {
		t.Fatal("splitter jumped when grabbed")
	}
	vertical := c.Handle(nativeapp.Event{Kind: nativeapp.PointerMove, X: 200, Y: 200})
	if vertical.Changed {
		t.Fatal("vertical divider changed from a vertical move")
	}
	horizontal := c.Handle(nativeapp.Event{Kind: nativeapp.PointerMove, X: 240, Y: 200})
	if horizontal.Value != .6 {
		t.Fatalf("horizontal drag got %+v", horizontal)
	}
}

func TestEmbeddedFontActuallySelectsItsGlyphMetrics(t *testing.T) {
	s, _ := skin.Builtin("merrick")
	s.Typography.Family = "Sans"
	s.Typography.Asset = "custom-mono"
	s.Assets = map[string]skin.Asset{"custom-mono": {Kind: "font", MediaType: "font/ttf", Data: gomono.TTF}}
	theme, err := ThemeFromSkin(s)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPainter(theme)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.textWidth("iiii") != p.textWidth("WWWW") {
		t.Fatal("embedded monospaced font was ignored")
	}
	delete(s.Assets, "custom-mono")
	s.Typography.Asset = ""
	theme, err = ThemeFromSkin(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetTheme(theme); err != nil {
		t.Fatal(err)
	}
	if p.textWidth("iiii") == p.textWidth("WWWW") {
		t.Fatal("Sans fallback unexpectedly retains custom font")
	}
}

func TestControlSubpartsUseTheirOwnRecipesAndRadioHasNoSquareFrame(t *testing.T) {
	s, _ := skin.Builtin("plasma")
	s.Controls["switch-track"] = skin.Recipe{Layers: []skin.Layer{{Geometry: skin.Geometry{Kind: "rect"}, Fill: "#0000ff"}}}
	s.Controls["switch-thumb"] = skin.Recipe{Layers: []skin.Layer{{Geometry: skin.Geometry{Kind: "rect"}, Fill: "#ff0000"}}}
	theme, err := ThemeFromSkin(s)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewPainter(theme)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	dst := image.NewRGBA(image.Rect(0, 0, 200, 80))
	if err := p.DrawSwitch(dst, Control{Bounds: image.Rect(0, 0, 200, 40), State: State{Selected: true}}); err != nil {
		t.Fatal(err)
	}
	if got := dst.RGBAAt(188, 20); got.R < 240 || got.B > 10 {
		t.Fatalf("switch thumb recipe ignored: %v", got)
	}
	if got := dst.RGBAAt(145, 20); got.B < 240 || got.R > 10 {
		t.Fatalf("switch track recipe ignored: %v", got)
	}
	if err := p.DrawRadio(dst, Control{Bounds: image.Rect(0, 44, 180, 80), State: State{Selected: true}}); err != nil {
		t.Fatal(err)
	}
	// The 28px mark starts at (0,48): its corner must remain transparent.
	if got := dst.RGBAAt(0, 48); got.A != 0 {
		t.Fatalf("radio retained a square background: %v", got)
	}
	if dst.RGBAAt(14, 62).A == 0 {
		t.Fatal("radio indicator was omitted")
	}
}

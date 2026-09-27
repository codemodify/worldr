package skin

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"math"
	"reflect"
	"testing"
)

func TestPackagesRoundTripAndAllowIndependentCustomSkins(t *testing.T) {
	for _, s := range Builtins() {
		if err := s.Validate(); err != nil {
			t.Fatalf("%s: %v", s.ID, err)
		}
		var data bytes.Buffer
		if err := Encode(&data, s); err != nil {
			t.Fatal(err)
		}
		restored, err := Decode(&data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s, restored) {
			t.Fatalf("%s lost recipes in serialization", s.ID)
		}
	}
	s, _ := Builtin("merrick")
	copy := s.Clone()
	copy.ID = "dev.example.papercraft"
	copy.Name = "Papercraft"
	copy.Palette["accent"] = "#ffcc33"
	r := copy.Controls["button"]
	r.Layers[0].Geometry = Geometry{Kind: "path", Points: []Point{{0, 0}, {1, 0}, {.85, 1}, {.15, 1}}}
	copy.Controls["button"] = r
	if err := copy.Validate(); err != nil {
		t.Fatalf("custom ID/geometry rejected: %v", err)
	}
	copy.Window.Layout.ButtonOrder[0] = "close"
	copy.Icons["close"].Paths[0].Points[0].X = .1
	if s.Palette["accent"] == copy.Palette["accent"] || s.Window.Layout.ButtonOrder[0] == "close" || s.Icons["close"].Paths[0].Points[0].X == .1 {
		t.Fatal("Clone retained shared mutable data")
	}
	for _, id := range []string{"instrument", "aperture", "glass", "telemetry"} {
		if _, err := Builtin(id); err != nil {
			t.Fatalf("legacy ID %q: %v", id, err)
		}
	}
}

func TestValidationRejectsBrokenReferencesAndUnboundedPackages(t *testing.T) {
	base, _ := Builtin("plasma")
	cases := map[string]func(*Skin){
		"unknown version":    func(s *Skin) { s.Version++ },
		"missing color":      func(s *Skin) { r := s.Controls["button"]; r.Layers[0].Fill = "missing"; s.Controls["button"] = r },
		"missing image":      func(s *Skin) { r := s.Controls["button"]; r.Layers[0].Asset = "missing"; s.Controls["button"] = r },
		"missing icon":       func(s *Skin) { r := s.Window.Buttons["close"]; r.Icon = "missing"; s.Window.Buttons["close"] = r },
		"nonfinite metric":   func(s *Skin) { s.Metrics.Padding = math.NaN() },
		"out of bounds path": func(s *Skin) { s.Icons["close"].Paths[0].Points[0].X = 2 },
		"duplicate button":   func(s *Skin) { s.Window.Layout.ButtonOrder[1] = s.Window.Layout.ButtonOrder[0] },
		"oversized direct struct": func(s *Skin) {
			data := make([]byte, 850000)
			binary.BigEndian.PutUint32(data, 0x00010000)
			binary.BigEndian.PutUint16(data[4:], 1)
			s.Assets = map[string]Asset{"large": {Kind: "font", MediaType: "font/ttf", Data: data}}
		},
		"invalid font": func(s *Skin) {
			s.Assets = map[string]Asset{"bad": {Kind: "font", MediaType: "font/ttf", Data: []byte("not a font")}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := base.Clone()
			mutate(&s)
			if s.Validate() == nil {
				t.Fatal("accepted invalid skin")
			}
		})
	}
	var data bytes.Buffer
	if err := Encode(&data, base); err != nil {
		t.Fatal(err)
	}
	data.WriteString("{}")
	if _, err := Decode(&data); err == nil {
		t.Fatal("accepted trailing JSON")
	}
	if _, err := Decode(bytes.NewReader(make([]byte, MaxPackageBytes+1))); err == nil {
		t.Fatal("accepted oversized JSON")
	}
}

func TestEmbeddedImagesHaveADecodedPixelBudget(t *testing.T) {
	s, _ := Builtin("merrick")
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2049, 2049))); err != nil {
		t.Fatal(err)
	}
	s.Assets = map[string]Asset{"texture": {Kind: "image", MediaType: "image/png", Data: data.Bytes()}}
	if len(data.Bytes()) > MaxPackageBytes {
		t.Fatal("fixture should be small when compressed")
	}
	if err := s.Validate(); err == nil {
		t.Fatal("accepted oversized decoded image")
	}
}

func TestBuiltinWindowSystemsHaveDistinctGeometryAndIcons(t *testing.T) {
	skins := Builtins()
	for i, a := range skins {
		for _, b := range skins[i+1:] {
			if reflect.DeepEqual(a.Window.Frame, b.Window.Frame) || reflect.DeepEqual(a.Window.Grip, b.Window.Grip) || reflect.DeepEqual(a.Icons["window-close"], b.Icons["window-close"]) {
				t.Fatalf("%s/%s share a frame, grip or close glyph", a.ID, b.ID)
			}
		}
	}
}

func TestOptionalDesktopAndWindowPlacementTraitsValidateAndRoundTrip(t *testing.T) {
	base, _ := Builtin("plasma")
	if base.Desktop != (Desktop{}) || base.Window.Layout.TitleSide != "" || base.Window.Layout.ScaleMode != "" {
		t.Fatal("legacy preset changed its default placement")
	}
	for name, mutate := range map[string]func(*Skin){
		"title side":       func(s *Skin) { s.Window.Layout.TitleSide = "diagonal" },
		"density":          func(s *Skin) { s.Window.Layout.ScaleMode = "arbitrary" },
		"title width":      func(s *Skin) { s.Window.Layout.TitleWidth = math.Inf(1) },
		"bottom height":    func(s *Skin) { s.Window.Layout.BottomHeight = 129 },
		"grip width":       func(s *Skin) { s.Window.Layout.GripWidth = math.NaN() },
		"desktop chrome":   func(s *Skin) { s.Desktop.Chrome = "unknown" },
		"desktop backdrop": func(s *Skin) { s.Desktop.Backdrop = "unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			s := base.Clone()
			mutate(&s)
			if s.Validate() == nil {
				t.Fatal("accepted unknown or unbounded placement trait")
			}
		})
	}
	custom, _ := Builtin("merrick")
	custom.ID = "org.example.vertical-tabs"
	var data bytes.Buffer
	if err := Encode(&data, custom); err != nil {
		t.Fatal(err)
	}
	loaded, err := Decode(&data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Window.Layout, custom.Window.Layout) || loaded.Desktop != custom.Desktop {
		t.Fatal("package lost optional renderer traits")
	}
}

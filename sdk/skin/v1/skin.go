// Package skin defines versioned, renderer-independent Worldr appearance packages.
// Recipes describe geometry and material intent; a renderer can expose a simpler
// fallback for effects such as refraction without changing control behavior.
package skin

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const Version = 1
const MaxPackageBytes = 1 << 20
const MaxAssetPixels = 4 << 20

type Color string
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
type Box struct {
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	W float64 `json:"w,omitempty"`
	H float64 `json:"h,omitempty"`
}
type Insets struct {
	Top    float64 `json:"top,omitempty"`
	Right  float64 `json:"right,omitempty"`
	Bottom float64 `json:"bottom,omitempty"`
	Left   float64 `json:"left,omitempty"`
}

type Typography struct {
	Family     string  `json:"family"`
	Size       float64 `json:"size"`
	LineHeight float64 `json:"line_height"`
	Weight     int     `json:"weight,omitempty"`
	Asset      string  `json:"asset,omitempty"`
}
type Metrics struct {
	Padding       float64 `json:"padding"`
	Gap           float64 `json:"gap"`
	ControlHeight float64 `json:"control_height"`
	Stroke        float64 `json:"stroke"`
	Corner        float64 `json:"corner"`
	Notch         float64 `json:"notch"`
	Icon          float64 `json:"icon"`
}

// Geometry coordinates and corner/radius/notch sizes are normalized. Sizes are
// fractions of the shorter side; Path points are relative to the layer bounds.
type Geometry struct {
	Kind   string  `json:"kind"`
	Radius float64 `json:"radius,omitempty"`
	Corner float64 `json:"corner,omitempty"`
	Notch  float64 `json:"notch,omitempty"`
	Points []Point `json:"points,omitempty"`
}

// Layer uses normalized Bounds (zero means the complete component). Strokes
// and content insets are logical pixels. Zero opacity means the default, one.
// An omitted Fill or Stroke means that operation is absent.
type Layer struct {
	Bounds      Box      `json:"bounds,omitempty"`
	Geometry    Geometry `json:"geometry"`
	Fill        string   `json:"fill,omitempty"`
	Stroke      string   `json:"stroke,omitempty"`
	Material    string   `json:"material,omitempty"`
	Asset       string   `json:"asset,omitempty"`
	StrokeWidth float64  `json:"stroke_width,omitempty"`
	Opacity     float64  `json:"opacity,omitempty"`
}
type StateStyle struct {
	Fill    string  `json:"fill,omitempty"`
	Stroke  string  `json:"stroke,omitempty"`
	Text    string  `json:"text,omitempty"`
	Opacity float64 `json:"opacity,omitempty"`
	Glow    float64 `json:"glow,omitempty"`
	Offset  Point   `json:"offset,omitempty"`
}
type Recipe struct {
	Layers        []Layer               `json:"layers"`
	ContentInsets Insets                `json:"content_insets,omitempty"`
	TextColor     string                `json:"text_color,omitempty"`
	Icon          string                `json:"icon,omitempty"`
	States        map[string]StateStyle `json:"states,omitempty"`
}
type Path struct {
	Points []Point `json:"points"`
	Closed bool    `json:"closed,omitempty"`
	Fill   bool    `json:"fill,omitempty"`
}
type Icon struct {
	Paths       []Path  `json:"paths"`
	StrokeWidth float64 `json:"stroke_width,omitempty"`
}

// Asset data is embedded as base64 by encoding/json. Packages need no external
// file paths or network requests. Renderers may support image/font assets.
type Asset struct {
	Kind      string `json:"kind"`
	MediaType string `json:"media_type"`
	Data      []byte `json:"data"`
}

// Material is portable intent, not a shader program. Flat and linear-gradient
// have CPU implementations. Glass/emissive can fall back to alpha/bright edges;
// Blur and Refraction require an appropriate scene-renderer capability.
type Material struct {
	Kind       string  `json:"kind"`
	Secondary  string  `json:"secondary,omitempty"`
	Angle      float64 `json:"angle,omitempty"`
	Opacity    float64 `json:"opacity,omitempty"`
	Glow       float64 `json:"glow,omitempty"`
	Blur       float64 `json:"blur,omitempty"`
	Refraction float64 `json:"refraction,omitempty"`
}
type WindowLayout struct {
	// TitleSide defaults to top. Left titles occupy an independent vertical
	// tab; window buttons remain in the top rail.
	TitleSide    string  `json:"title_side,omitempty"`
	TitleWidth   float64 `json:"title_width,omitempty"`
	BottomHeight float64 `json:"bottom_height,omitempty"`
	GripWidth    float64 `json:"grip_width,omitempty"`
	// ScaleMode defaults to the historical 960-unit client width. client-pixels
	// uses the live client width so small utility windows retain readable chrome.
	ScaleMode      string   `json:"scale_mode,omitempty"`
	TitlebarHeight float64  `json:"titlebar_height"`
	BorderWidth    float64  `json:"border_width"`
	ButtonWidth    float64  `json:"button_width"`
	ButtonHeight   float64  `json:"button_height"`
	ButtonGap      float64  `json:"button_gap"`
	ResizeSize     float64  `json:"resize_size"`
	ButtonsSide    string   `json:"buttons_side"`
	ButtonOrder    []string `json:"button_order"`
}
type Window struct {
	Frame    Recipe            `json:"frame"`
	Titlebar Recipe            `json:"titlebar"`
	Grip     Recipe            `json:"grip"`
	Resize   Recipe            `json:"resize"`
	Buttons  map[string]Recipe `json:"buttons"`
	Layout   WindowLayout      `json:"layout"`
}

// Desktop selects optional renderer-owned treatments. Empty fields preserve
// the host's normal desktop. The version-one vocabulary is deliberately small:
// slate-tabs supplies slate utility rails; corner-tools supplies a compact
// corner launcher; sculpted-silver supplies the neutral
// sculpted backdrop; quiet-gradient supplies an unobtrusive field from the
// desktop-background and desktop-glow palette tokens; panel-field supplies
// retained translucent sheets in the host's 3D workspace. All colors continue
// to come from the package palette.
type Desktop struct {
	Chrome   string `json:"chrome,omitempty"`
	Backdrop string `json:"backdrop,omitempty"`
}

type Skin struct {
	Version     int                 `json:"version"`
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Desktop     Desktop             `json:"desktop,omitempty"`
	Palette     map[string]Color    `json:"palette"`
	Typography  Typography          `json:"typography"`
	Metrics     Metrics             `json:"metrics"`
	Window      Window              `json:"window"`
	Controls    map[string]Recipe   `json:"controls"`
	Icons       map[string]Icon     `json:"icons"`
	Materials   map[string]Material `json:"materials,omitempty"`
	Assets      map[string]Asset    `json:"assets,omitempty"`
}

func parseColor(value Color) (color.NRGBA, error) {
	s := string(value)
	if len(s) != 7 && len(s) != 9 || len(s) == 0 || s[0] != '#' {
		return color.NRGBA{}, fmt.Errorf("invalid color %q: use #RRGGBB or #RRGGBBAA", value)
	}
	n, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("invalid color %q", value)
	}
	if len(s) == 7 {
		n = n<<8 | 255
	}
	return color.NRGBA{R: byte(n >> 24), G: byte(n >> 16), B: byte(n >> 8), A: byte(n)}, nil
}

// Color resolves a validated palette token or literal, retaining straight alpha.
// Invalid or absent tokens return transparent black; Validate catches them.
func (s Skin) Color(token string) color.NRGBA {
	v := Color(token)
	if c, ok := s.Palette[token]; ok {
		v = c
	}
	c, _ := parseColor(v)
	return c
}
func (s Skin) Clone() Skin {
	b, _ := json.Marshal(s)
	var copy Skin
	_ = json.Unmarshal(b, &copy)
	return copy
}
func finite(v float64) bool          { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func bounded(v, lo, hi float64) bool { return finite(v) && v >= lo && v <= hi }

var identifier = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

func (s Skin) Validate() error {
	if s.Version != Version {
		return fmt.Errorf("skin: unsupported version %d", s.Version)
	}
	if !identifier.MatchString(s.ID) || len(s.Name) == 0 || len(s.Name) > 128 || len(s.Description) > 4096 || !utf8.ValidString(s.Name) || !utf8.ValidString(s.Description) {
		return fmt.Errorf("skin: invalid identity")
	}
	if s.Desktop.Chrome != "" && s.Desktop.Chrome != "slate-tabs" && s.Desktop.Chrome != "corner-tools" || s.Desktop.Backdrop != "" && s.Desktop.Backdrop != "sculpted-silver" && s.Desktop.Backdrop != "quiet-gradient" && s.Desktop.Backdrop != "panel-field" {
		return fmt.Errorf("skin: unsupported desktop treatment")
	}
	if len(s.Palette) > 128 || len(s.Controls) > 128 || len(s.Icons) > 128 || len(s.Materials) > 64 || len(s.Assets) > 32 {
		return fmt.Errorf("skin: resource count exceeds package limits")
	}
	assetBytes, assetPixels := 0, 0
	for name, asset := range s.Assets {
		assetBytes += len(asset.Data)
		if !identifier.MatchString(name) || len(asset.Data) == 0 || assetBytes > MaxPackageBytes {
			return fmt.Errorf("skin: invalid or oversized asset %q", name)
		}
		switch asset.Kind {
		case "image":
			if asset.MediaType != "image/png" && asset.MediaType != "image/jpeg" {
				return fmt.Errorf("skin: unsupported image media type")
			}
			config, format, err := image.DecodeConfig(bytes.NewReader(asset.Data))
			if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || asset.MediaType != "image/"+format {
				return fmt.Errorf("skin: invalid image asset %q", name)
			}
			assetPixels += config.Width * config.Height
			if assetPixels > MaxAssetPixels {
				return fmt.Errorf("skin: decoded image assets exceed pixel budget")
			}
		case "font":
			if asset.MediaType != "font/ttf" && asset.MediaType != "font/otf" {
				return fmt.Errorf("skin: unsupported font media type")
			}
			if err := validateFontContainer(asset.Data); err != nil {
				return fmt.Errorf("skin: invalid font asset %q: %w", name, err)
			}
		default:
			return fmt.Errorf("skin: unknown asset kind %q", asset.Kind)
		}
	}
	for key, c := range s.Palette {
		if !identifier.MatchString(key) {
			return fmt.Errorf("skin: invalid palette key %q", key)
		}
		if _, err := parseColor(c); err != nil {
			return err
		}
	}
	for _, key := range []string{"background", "surface", "raised", "hover", "pressed", "accent", "accent-alt", "border", "text", "muted", "selection", "disabled", "success", "warning", "danger"} {
		if _, ok := s.Palette[key]; !ok {
			return fmt.Errorf("skin: missing palette token %q", key)
		}
	}
	t := s.Typography
	if t.Asset != "" {
		a, ok := s.Assets[t.Asset]
		if !ok || a.Kind != "font" {
			return fmt.Errorf("skin: missing font asset %q", t.Asset)
		}
	}
	if len(t.Family) == 0 || len(t.Family) > 128 || !bounded(t.Size, 6, 128) || !bounded(t.LineHeight, t.Size, 256) || t.Weight != 0 && (t.Weight < 100 || t.Weight > 900) {
		return fmt.Errorf("skin: invalid typography")
	}
	m := s.Metrics
	for _, v := range []float64{m.Padding, m.Gap, m.Corner, m.Notch} {
		if !bounded(v, 0, 64) {
			return fmt.Errorf("skin: invalid metric")
		}
	}
	if !bounded(m.ControlHeight, 12, 256) || !bounded(m.Stroke, 1, 16) || !bounded(m.Icon, 6, 128) {
		return fmt.Errorf("skin: invalid control metrics")
	}
	ref := func(token string) error {
		if token == "" {
			return nil
		}
		if _, ok := s.Palette[token]; ok {
			return nil
		}
		_, err := parseColor(Color(token))
		return err
	}
	for name, material := range s.Materials {
		if !identifier.MatchString(name) {
			return fmt.Errorf("skin: invalid material ID")
		}
		switch material.Kind {
		case "flat", "linear-gradient", "glass", "emissive":
		default:
			return fmt.Errorf("skin: unknown material kind %q", material.Kind)
		}
		if err := ref(material.Secondary); err != nil {
			return err
		}
		if !bounded(material.Angle, -360, 360) || !bounded(material.Opacity, 0, 1) || !bounded(material.Glow, 0, 4) || !bounded(material.Blur, 0, 1) || !bounded(material.Refraction, 0, 1) {
			return fmt.Errorf("skin: invalid material %q", name)
		}
	}
	for name, icon := range s.Icons {
		if !identifier.MatchString(name) || len(icon.Paths) == 0 || len(icon.Paths) > 32 || !bounded(icon.StrokeWidth, 0, 16) {
			return fmt.Errorf("skin: invalid icon %q", name)
		}
		for _, path := range icon.Paths {
			if len(path.Points) < 2 || len(path.Points) > 128 {
				return fmt.Errorf("skin: invalid icon path")
			}
			for _, p := range path.Points {
				if !bounded(p.X, 0, 1) || !bounded(p.Y, 0, 1) {
					return fmt.Errorf("skin: icon points must be normalized")
				}
			}
		}
	}
	validateRecipe := func(name string, r Recipe) error {
		if len(r.Layers) == 0 || len(r.Layers) > 32 {
			return fmt.Errorf("skin: recipe %q requires 1..32 layers", name)
		}
		if err := ref(r.TextColor); err != nil {
			return err
		}
		if r.Icon != "" {
			if _, ok := s.Icons[r.Icon]; !ok {
				return fmt.Errorf("skin: missing icon %q", r.Icon)
			}
		}
		for _, v := range []float64{r.ContentInsets.Top, r.ContentInsets.Right, r.ContentInsets.Bottom, r.ContentInsets.Left} {
			if !bounded(v, 0, 256) {
				return fmt.Errorf("skin: invalid recipe insets")
			}
		}
		for _, l := range r.Layers {
			b := l.Bounds
			if b != (Box{}) && (!bounded(b.X, 0, 1) || !bounded(b.Y, 0, 1) || !bounded(b.W, .001, 1) || !bounded(b.H, .001, 1) || b.X+b.W > 1.000001 || b.Y+b.H > 1.000001) {
				return fmt.Errorf("skin: invalid layer bounds in %s", name)
			}
			g := l.Geometry
			switch g.Kind {
			case "rect", "rounded", "chamfered", "bracketed", "notched":
			case "path":
				if len(g.Points) < 3 || len(g.Points) > 128 {
					return fmt.Errorf("skin: invalid geometry path")
				}
			default:
				return fmt.Errorf("skin: unknown geometry %q", g.Kind)
			}
			for _, v := range []float64{g.Radius, g.Corner, g.Notch} {
				if !bounded(v, 0, .5) {
					return fmt.Errorf("skin: geometry sizes must be within 0..0.5")
				}
			}
			for _, p := range g.Points {
				if !bounded(p.X, 0, 1) || !bounded(p.Y, 0, 1) {
					return fmt.Errorf("skin: geometry points must be normalized")
				}
			}
			if err := ref(l.Fill); err != nil {
				return err
			}
			if err := ref(l.Stroke); err != nil {
				return err
			}
			if !bounded(l.StrokeWidth, 0, 16) || !bounded(l.Opacity, 0, 1) {
				return fmt.Errorf("skin: invalid layer paint")
			}
			if l.Material != "" {
				if _, ok := s.Materials[l.Material]; !ok {
					return fmt.Errorf("skin: missing material %q", l.Material)
				}
			}
			if l.Asset != "" {
				a, ok := s.Assets[l.Asset]
				if !ok || a.Kind != "image" {
					return fmt.Errorf("skin: missing image asset %q", l.Asset)
				}
			}
		}
		for state, v := range r.States {
			switch state {
			case "hovered", "pressed", "focused", "selected", "disabled", "invalid":
			default:
				return fmt.Errorf("skin: unknown state %q", state)
			}
			for _, c := range []string{v.Fill, v.Stroke, v.Text} {
				if err := ref(c); err != nil {
					return err
				}
			}
			if !bounded(v.Opacity, 0, 1) || !bounded(v.Glow, 0, 4) || !bounded(v.Offset.X, -32, 32) || !bounded(v.Offset.Y, -32, 32) {
				return fmt.Errorf("skin: invalid state style")
			}
		}
		return nil
	}
	if len(s.Controls) == 0 {
		return fmt.Errorf("skin: no control recipes")
	}
	for name, r := range s.Controls {
		if !identifier.MatchString(name) {
			return fmt.Errorf("skin: invalid control name")
		}
		if err := validateRecipe(name, r); err != nil {
			return err
		}
	}
	for name, r := range map[string]Recipe{"window.frame": s.Window.Frame, "window.titlebar": s.Window.Titlebar, "window.grip": s.Window.Grip, "window.resize": s.Window.Resize} {
		if err := validateRecipe(name, r); err != nil {
			return err
		}
	}
	w := s.Window.Layout
	if w.TitleSide != "" && w.TitleSide != "top" && w.TitleSide != "left" || !bounded(w.TitleWidth, 0, 256) || !bounded(w.BottomHeight, 0, 128) || !bounded(w.GripWidth, 0, 256) || w.ScaleMode != "" && w.ScaleMode != "client-pixels" {
		return fmt.Errorf("skin: invalid title side or rail dimensions")
	}
	for _, v := range []float64{w.TitlebarHeight, w.BorderWidth, w.ButtonWidth, w.ButtonHeight, w.ResizeSize} {
		if !bounded(v, 1, 256) {
			return fmt.Errorf("skin: invalid window metric")
		}
	}
	if !bounded(w.ButtonGap, 0, 64) || (w.ButtonsSide != "left" && w.ButtonsSide != "right") {
		return fmt.Errorf("skin: invalid window layout")
	}
	if len(w.ButtonOrder) != 3 || len(s.Window.Buttons) != 3 {
		return fmt.Errorf("skin: window requires minimize, maximize and close")
	}
	seen := map[string]bool{}
	for _, name := range w.ButtonOrder {
		if name != "minimize" && name != "maximize" && name != "close" || seen[name] {
			return fmt.Errorf("skin: invalid window button order")
		}
		seen[name] = true
		r, ok := s.Window.Buttons[name]
		if !ok {
			return fmt.Errorf("skin: missing window button %q", name)
		}
		if err := validateRecipe(name, r); err != nil {
			return err
		}
	}
	encoded, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if len(encoded)+1 > MaxPackageBytes {
		return fmt.Errorf("skin: package exceeds %d bytes", MaxPackageBytes)
	}
	return nil
}

// Validate the standard sfnt envelope without introducing a font-renderer
// dependency. Backends validate individual outline tables before using a font.
func validateFontContainer(data []byte) error {
	if len(data) < 12 {
		return fmt.Errorf("truncated sfnt header")
	}
	signature := binary.BigEndian.Uint32(data[:4])
	if signature != 0x00010000 && string(data[:4]) != "OTTO" {
		return fmt.Errorf("expected TrueType or OpenType font")
	}
	n := int(binary.BigEndian.Uint16(data[4:6]))
	if n == 0 || n > 256 || len(data) < 12+16*n {
		return fmt.Errorf("invalid font table count")
	}
	seen := map[string]bool{}
	for i := 0; i < n; i++ {
		entry := data[12+i*16 : 12+(i+1)*16]
		name := string(entry[:4])
		if seen[name] {
			return fmt.Errorf("duplicate font table")
		}
		seen[name] = true
		offset, size := uint64(binary.BigEndian.Uint32(entry[8:12])), uint64(binary.BigEndian.Uint32(entry[12:16]))
		if offset > uint64(len(data)) || size > uint64(len(data))-offset {
			return fmt.Errorf("font table outside asset")
		}
	}
	return nil
}

func Decode(reader io.Reader) (Skin, error) {
	var s Skin
	if reader == nil {
		return s, fmt.Errorf("skin: nil reader")
	}
	b, err := io.ReadAll(io.LimitReader(reader, MaxPackageBytes+1))
	if err != nil {
		return s, err
	}
	if len(b) > MaxPackageBytes {
		return s, fmt.Errorf("skin: package exceeds %d bytes", MaxPackageBytes)
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(&s); err != nil {
		return Skin{}, err
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		return Skin{}, fmt.Errorf("skin: package must contain one JSON object")
	}
	if err = s.Validate(); err != nil {
		return Skin{}, err
	}
	return s, nil
}
func Encode(writer io.Writer, s Skin) error {
	if writer == nil {
		return fmt.Errorf("skin: nil writer")
	}
	if err := s.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if len(b) > MaxPackageBytes {
		return fmt.Errorf("skin: package exceeds %d bytes", MaxPackageBytes)
	}
	b = append(b, '\n')
	for len(b) > 0 {
		n, err := writer.Write(b)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

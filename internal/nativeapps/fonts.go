package nativeapps

import (
	"io"
	"os"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

const (
	maxFallbackGlyphs    = 4096
	maxFallbackFaces     = 16
	maxFallbackFileBytes = 32 << 20
	maxFallbackBytes     = 64 << 20
)

type fallbackFontLocation struct {
	Path  string
	Index int // TTC/OTC face index, not a named variable-font instance.
}

type terminalFontMatcher interface {
	Match(character rune, style int) (fallbackFontLocation, bool)
	Close()
}

type fallbackGlyphKey struct {
	character rune
	style     int
}

// This cache belongs to one renderer, as font.Face is not concurrency safe.
// Bounds are lifetime limits: exhaustion keeps the embedded replacement glyph
// instead of repeatedly loading fonts or doing per-frame system lookups.
type terminalFontFallback struct {
	base          [4]font.Face
	matcher       terminalFontMatcher
	matcherOpened bool
	newMatcher    func() terminalFontMatcher
	glyphs        map[fallbackGlyphKey]font.Face
	faces         map[fallbackFontLocation]font.Face
	files         map[string]*opentype.Collection
	bytes         int
	closed        bool
}

func newTerminalFontFallback(base [4]font.Face) *terminalFontFallback {
	return &terminalFontFallback{base: base, newMatcher: newSystemFontMatcher,
		glyphs: make(map[fallbackGlyphKey]font.Face), faces: make(map[fallbackFontLocation]font.Face),
		files: make(map[string]*opentype.Collection)}
}

func (f *terminalFontFallback) face(character rune, style int) font.Face {
	base := f.base[style]
	if f.closed || !utf8.ValidRune(character) {
		return base
	}
	if _, ok := base.GlyphAdvance(character); ok {
		return base
	}
	key := fallbackGlyphKey{character, style}
	if face, cached := f.glyphs[key]; cached {
		if face != nil {
			return face
		}
		return base
	}
	if len(f.glyphs) >= maxFallbackGlyphs {
		return base
	}
	f.glyphs[key] = nil // Cache misses as well as successful glyphs.
	if !f.matcherOpened {
		f.matcherOpened = true
		f.matcher = f.newMatcher()
	}
	if f.matcher == nil {
		return base
	}
	location, ok := f.matcher.Match(character, style)
	if !ok || location.Path == "" || location.Index < 0 || location.Index > 0xffff {
		return base
	}
	face, cached := f.faces[location]
	if !cached && len(f.faces) < maxFallbackFaces {
		face = f.load(location)
		f.faces[location] = face
	}
	if face == nil {
		return base
	}
	// Check the actual parsed face, even if Fontconfig claimed coverage. Bound
	// the raster mask too: clipping a destination alone does not bound the
	// intermediate mask allocated by x/image for malformed/oversized glyphs.
	bounds, _, ok := face.GlyphBounds(character)
	if !ok || bounds.Min.X.Floor() < -4*cellWidth || bounds.Max.X.Ceil() > 4*cellWidth || bounds.Min.Y.Floor() < -4*cellHeight || bounds.Max.Y.Ceil() > 4*cellHeight {
		return base
	}
	f.glyphs[key] = face
	return face
}

func (f *terminalFontFallback) load(location fallbackFontLocation) font.Face {
	collection := f.files[location.Path]
	if collection == nil {
		data, err := readFallbackFont(location.Path, min(maxFallbackFileBytes, maxFallbackBytes-f.bytes))
		if err != nil {
			return nil
		}
		collection, err = opentype.ParseCollection(data)
		if err != nil {
			return nil
		}
		f.files[location.Path] = collection
		f.bytes += len(data)
	}
	parsed, err := collection.Font(location.Index)
	if err != nil {
		return nil
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 16, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil
	}
	return face
}

func readFallbackFont(path string, limit int) ([]byte, error) {
	if limit <= 0 {
		return nil, os.ErrInvalid
	}
	file, err := openFallbackFont(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > int64(limit) {
		return nil, os.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(data) > limit {
		return nil, os.ErrInvalid
	}
	return data, nil
}

func (f *terminalFontFallback) close() {
	if f == nil || f.closed {
		return
	}
	f.closed = true
	for _, face := range f.faces {
		if face != nil {
			_ = face.Close()
		}
	}
	if f.matcher != nil {
		f.matcher.Close()
	}
	f.matcher, f.glyphs, f.faces, f.files = nil, nil, nil, nil
	f.base, f.newMatcher = [4]font.Face{}, nil
	f.bytes = 0
}

package nativeapps

import (
	"bytes"
	"encoding/binary"
	"image"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/math/fixed"
)

type fakeFontMatcher struct {
	calls  []fallbackGlyphKey
	match  func(rune, int) (fallbackFontLocation, bool)
	closed int
}

type countedCloseFace struct {
	font.Face
	closed int
}

func (f *countedCloseFace) Close() error {
	f.closed++
	return f.Face.Close()
}

func (m *fakeFontMatcher) Match(ch rune, style int) (fallbackFontLocation, bool) {
	m.calls = append(m.calls, fallbackGlyphKey{ch, style})
	return m.match(ch, style)
}
func (m *fakeFontMatcher) Close() { m.closed++ }

// Force a controlled coverage gap while retaining the real embedded face's
// metrics and drawing behavior. Tests do not depend on any installed fonts.
type missingGlyphFace struct {
	font.Face
	missing rune
}

func (f missingGlyphFace) GlyphAdvance(ch rune) (fixed.Int26_6, bool) {
	advance, ok := f.Face.GlyphAdvance(ch)
	return advance, ok && ch != f.missing
}

func fallbackRenderer(t *testing.T, missing rune, matcher *fakeFontMatcher) *terminalRenderer {
	t.Helper()
	r, err := newTerminalRenderer(324, 246)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.close)
	for i, face := range r.faces {
		r.fallback.base[i] = missingGlyphFace{face, missing}
	}
	r.fallback.newMatcher = func() terminalFontMatcher { return matcher }
	return r
}

func writeFallbackFont(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "font.ttf")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Build a real TTC from the embedded fonts, relocating the SFNT table offsets.
// This exercises nonzero collection indices without a system fixture download.
func testFontCollection(fonts ...[]byte) []byte {
	header := 12 + len(fonts)*4
	data := make([]byte, header)
	copy(data, "ttcf")
	binary.BigEndian.PutUint32(data[4:], 0x00010000)
	binary.BigEndian.PutUint32(data[8:], uint32(len(fonts)))
	for i, source := range fonts {
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
		offset := len(data)
		binary.BigEndian.PutUint32(data[12+i*4:], uint32(offset))
		data = append(data, source...)
		tables := int(binary.BigEndian.Uint16(data[offset+4:]))
		for table := 0; table < tables; table++ {
			field := offset + 12 + table*16 + 8
			binary.BigEndian.PutUint32(data[field:], binary.BigEndian.Uint32(data[field:])+uint32(offset))
		}
	}
	return data
}

func TestEmbeddedTerminalGlyphNeverInitializesFontconfig(t *testing.T) {
	r, err := newTerminalRenderer(324, 246)
	if err != nil {
		t.Fatal(err)
	}
	defer r.close()
	r.fallback.newMatcher = func() terminalFontMatcher { t.Fatal("embedded glyph requested system lookup"); return nil }
	for style := range r.faces {
		if face := r.fallback.face('A', style); face != r.faces[style] {
			t.Fatal("existing embedded glyph changed fonts")
		}
	}
	if len(r.fallback.glyphs) != 0 || r.fallback.matcherOpened {
		t.Fatal("embedded text allocated system glyph cache")
	}
}

func TestFallbackCachesStyleSpecificCollectionFacesAndMisses(t *testing.T) {
	data := testFontCollection(gomono.TTF, gomonobold.TTF, gomonoitalic.TTF, gomonobolditalic.TTF)
	path := writeFallbackFont(t, data)
	matcher := &fakeFontMatcher{match: func(ch rune, style int) (fallbackFontLocation, bool) {
		return fallbackFontLocation{path, style}, true
	}}
	r := fallbackRenderer(t, 'A', matcher)
	f := r.fallback
	for style := range r.faces {
		face := f.face('A', style)
		if face == f.base[style] || face == nil {
			t.Fatalf("missing glyph did not select collection face %d", style)
		}
		actual, _, ok := face.GlyphBounds('A')
		want, _, _ := r.faces[style].GlyphBounds('A')
		if !ok || actual != want {
			t.Fatalf("collection index %d lost bold/italic outlines: %v vs %v", style, actual, want)
		}
		if f.face('A', style) != face {
			t.Fatal("cached glyph changed its face")
		}
	}
	if len(matcher.calls) != 4 || len(f.faces) != 4 || len(f.files) != 1 || f.bytes != len(data) {
		t.Fatal("fallback duplicated lookups, collection bytes, or style faces")
	}
	for i := 0; i < 3; i++ {
		if f.face('\U0010ffff', 0) != f.base[0] {
			t.Fatal("Fontconfig coverage claim bypassed parsed glyph validation")
		}
	}
	if len(matcher.calls) != 5 {
		t.Fatal("missing glyph was looked up again")
	}
	owned := &countedCloseFace{Face: f.faces[fallbackFontLocation{path, 0}]}
	f.faces[fallbackFontLocation{path, 0}] = owned
	f.close()
	f.close()
	if matcher.closed != 1 || owned.closed != 1 || f.faces != nil || f.files != nil || f.glyphs != nil || f.bytes != 0 || f.base != [4]font.Face{} {
		t.Fatal("fallback did not release its owned cache exactly once")
	}
	if f.face('A', 0) != f.base[0] || len(matcher.calls) != 5 {
		t.Fatal("closed resolver performed another lookup")
	}
}

func TestFallbackRejectsInvalidFilesIndicesAndHonorsBudgets(t *testing.T) {
	path := writeFallbackFont(t, gomono.TTF)
	for _, index := range []int{-1, 1, 65536} {
		matcher := &fakeFontMatcher{match: func(rune, int) (fallbackFontLocation, bool) { return fallbackFontLocation{path, index}, true }}
		r := fallbackRenderer(t, 'A', matcher)
		if r.fallback.face('A', 0) != r.fallback.base[0] || r.fallback.face('A', 0) != r.fallback.base[0] || len(matcher.calls) != 1 {
			t.Fatalf("invalid collection index %d did not retain embedded fallback", index)
		}
	}
	for _, data := range [][]byte{nil, []byte("not a font")} {
		invalid := writeFallbackFont(t, data)
		matcher := &fakeFontMatcher{match: func(rune, int) (fallbackFontLocation, bool) { return fallbackFontLocation{invalid, 0}, true }}
		r := fallbackRenderer(t, 'A', matcher)
		if r.fallback.face('A', 0) != r.fallback.base[0] || r.fallback.bytes != 0 {
			t.Fatal("invalid font data was retained")
		}
	}
	matcher := &fakeFontMatcher{match: func(rune, int) (fallbackFontLocation, bool) { return fallbackFontLocation{path, 0}, true }}
	r := fallbackRenderer(t, 'A', matcher)
	f := r.fallback
	f.bytes = maxFallbackBytes - len(gomono.TTF) + 1
	if f.face('A', 0) != f.base[0] || len(f.files) != 0 {
		t.Fatal("font load exceeded aggregate byte budget")
	}
	f.bytes, f.glyphs = 0, make(map[fallbackGlyphKey]font.Face)
	f.faces = make(map[fallbackFontLocation]font.Face)
	for i := 0; i < maxFallbackFaces; i++ {
		f.faces[fallbackFontLocation{"cached", i}] = nil
	}
	if f.face('A', 1) != f.base[1] || len(f.faces) != maxFallbackFaces || len(f.files) != 0 {
		t.Fatal("font lookup exceeded face budget")
	}
	f.glyphs = make(map[fallbackGlyphKey]font.Face)
	for i := 0; i < maxFallbackGlyphs; i++ {
		f.glyphs[fallbackGlyphKey{rune(0xe000 + i), 0}] = nil
	}
	calls := len(matcher.calls)
	if f.face('A', 2) != f.base[2] || len(f.glyphs) != maxFallbackGlyphs || len(matcher.calls) != calls {
		t.Fatal("full glyph cache performed further system lookup")
	}
}

func TestFallbackFontReadsAreBoundedAndRejectDirectories(t *testing.T) {
	path := writeFallbackFont(t, gomono.TTF)
	if data, err := readFallbackFont(path, len(gomono.TTF)); err != nil || !bytes.Equal(data, gomono.TTF) {
		t.Fatal("bounded normal font read failed", err)
	}
	for _, limit := range []int{0, len(gomono.TTF) - 1} {
		if _, err := readFallbackFont(path, limit); err == nil {
			t.Fatal("oversized font read accepted")
		}
	}
	if _, err := readFallbackFont(t.TempDir(), maxFallbackFileBytes); err == nil {
		t.Fatal("directory accepted as font")
	}
	if err := os.Truncate(path, maxFallbackFileBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := readFallbackFont(path, maxFallbackFileBytes); err == nil {
		t.Fatal("oversized sparse font accepted")
	}
}

func TestFallbackGlyphsKeepTerminalGridAndCellClipping(t *testing.T) {
	path := writeFallbackFont(t, gomonobolditalic.TTF)
	matcher := &fakeFontMatcher{match: func(rune, int) (fallbackFontLocation, bool) { return fallbackFontLocation{path, 0}, true }}
	r := fallbackRenderer(t, 'W', matcher)
	snapshot := testTerminalSnapshot(30, 8)
	if err := r.paint(snapshot, terminalDecoration{}); err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), r.image.Pix...)
	snapshot.Cells = append(snapshot.Cells[:0:0], snapshot.Cells...)
	snapshot.Cells[0].Chars[0], snapshot.Cells[0].Italic = 'W', true
	snapshot.Cells[1].Chars[0], snapshot.Cells[1].Width = 'W', 2
	snapshot.Cells[2].Width = 0
	if err := r.paint(snapshot, terminalDecoration{}); err != nil {
		t.Fatal(err)
	}
	changed := 0
	allowed := image.Rect(contentLeft, contentTop, contentLeft+3*cellWidth, contentTop+cellHeight)
	for y := 0; y < r.image.Rect.Dy(); y++ {
		for x := 0; x < r.image.Rect.Dx(); x++ {
			i := y*r.image.Stride + x*4
			if bytes.Equal(before[i:i+4], r.image.Pix[i:i+4]) {
				continue
			}
			changed++
			if !image.Pt(x, y).In(allowed) {
				t.Fatalf("fallback glyph escaped its fixed cells at (%d,%d)", x, y)
			}
		}
	}
	if changed == 0 || len(matcher.calls) != 2 {
		t.Fatal("fallback glyphs were not painted with independent style lookups")
	}
	if cols, rows := terminalGrid(r.image.Rect.Dx(), r.image.Rect.Dy()); cols != snapshot.Cols || rows != snapshot.Rows {
		t.Fatal("fallback changed terminal cell metrics")
	}
}

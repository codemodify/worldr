package workspace

import (
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	"testing"
)

func TestShapedLabelsKeepTextureIdentityWhileMovingAndRetireCache(t *testing.T) {
	w := study(t)
	c := scene.ColorHex(0x80ddff, 1)
	w.beginShapedLabels()
	w.shapedText(300, 200, 14, 280, "計測 / العربية", c)
	if w.labels == nil || len(w.labels.images) != 1 {
		t.Fatal("Unicode label was not rendered")
	}
	first := w.labels.images[0].Image.Texture
	w.beginShapedLabels()
	w.shapedText(420, 250, 14, 280, "計測 / العربية", c)
	if w.labels.images[0].Image.Texture.ID() != first.ID() || w.labels.images[0].Image.Bounds[0] != 420 {
		t.Fatal("moving annotation recreated its glyph pixels")
	}
	// Simulate a full old cache using separate retained resources; eviction must
	// never retire a label already borrowed by the current frame.
	for i := 0; i < 512; i++ {
		key := shapedKey{text: string(rune(0x1000 + i)), size: 14, width: 280, color: c}
		texture, err := render.NewTexture(1, 1, []byte{0, 0, 0, 0})
		if err != nil {
			t.Fatal(err)
		}
		w.labels.cache[key] = &shapedLabel{texture: texture, width: 1, height: 1, frame: 0}
		w.labels.bytes += 4
	}
	w.shapedText(410, 270, 14, 280, "new note", c)
	if len(w.labels.images) != 2 || w.labels.images[0].Image.Texture != first {
		t.Fatal("cache growth damaged current frame")
	}
	retired := w.RetiredTextures()
	for _, id := range retired {
		if id == first.ID() {
			t.Fatal("retired a resource used by this frame")
		}
	}
	if len(retired) == 0 || len(w.RetiredTextures()) != 0 {
		t.Fatal("retirements not drained once")
	}
}

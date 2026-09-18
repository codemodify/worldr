package render

import (
	"image"
	"testing"
)

type testExternalImage struct {
	w, h   int
	closed bool
}

func (i *testExternalImage) Size() (int, int) { return i.w, i.h }
func (i *testExternalImage) Close() error     { i.closed = true; return nil }
func TestExternalTextureOwnsOnlyOnSuccessAndRejectsCPUChanges(t *testing.T) {
	source := &testExternalImage{w: 0, h: 4}
	if _, err := NewExternalTexture(source); err == nil {
		t.Fatal("accepted invalid external extent")
	}
	if source.closed {
		t.Fatal("rejected source ownership changed")
	}
	source.w = 4
	texture, err := NewExternalTexture(source)
	if err != nil {
		t.Fatal(err)
	}
	if texture.ID() == 0 || texture.ExternalSource() != source {
		t.Fatal("source identity not retained")
	}
	if err := texture.Replace(1, 1, []byte{1, 2, 3, 4}); err == nil {
		t.Fatal("external texture accepted CPU replacement")
	}
	if err := texture.Update(image.Rect(0, 0, 1, 1), []byte{1, 2, 3, 4}); err == nil {
		t.Fatal("external texture accepted CPU update")
	}
	if _, ok := texture.Snapshot(0); ok {
		t.Fatal("external texture invented CPU snapshot")
	}
	if w, h := texture.Size(); w != 4 || h != 4 || texture.Revision() != 1 {
		t.Fatal("rejected mutation changed source state")
	}
	if err := texture.Close(); err != nil || !source.closed {
		t.Fatal("external close not forwarded", err)
	}
}

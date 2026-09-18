package app

import (
	"bytes"
	"io"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/apps"
)

type cursorAdapterServer struct {
	adapterServer
	cursor apps.Cursor
}

func (s *cursorAdapterServer) Cursor() apps.Cursor { return s.cursor }
func (s *cursorAdapterServer) Pointer(id uint64, x, y float32) error {
	s.cursor.SurfaceID, s.cursor.Set = id, false
	return s.adapterServer.Pointer(id, x, y)
}

func TestApplicationCursorRetainsImageAndResetsWithLivePointerTarget(t *testing.T) {
	s := &cursorAdapterServer{adapterServer: adapterServer{current: []apps.Surface{
		{ID: 7, Width: 2, Height: 2, Revision: 1, Pixels: make([]byte, 16)},
		{ID: 8, Width: 2, Height: 2, Revision: 1, Pixels: make([]byte, 16)},
	}}}
	a := &applicationController{server: s, images: make(map[uint64]*applicationImage), output: io.Discard}
	if err := a.poll(); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.ApplicationCursor(7); ok {
		t.Fatal("client without a cursor suppressed workspace fallback")
	}
	s.cursor = apps.Cursor{SurfaceID: 7, Revision: 1, ImageID: 15, ImageRevision: 1, Set: true, Width: 2, Height: 2, Scale: 2, HotspotX: 1, HotspotY: -1, Pixels: []byte{64, 32, 16, 128, 0, 0, 0, 0, 255, 128, 64, 255, 0, 0, 0, 0}}
	first, ok := a.ApplicationCursor(7)
	if !ok || first.Hidden || first.Texture == nil || first.Scale != 2 || first.HotspotX != 1 || first.HotspotY != -1 {
		t.Fatalf("lost cursor metadata: %+v", first)
	}
	image, _ := first.Texture.Snapshot(0)
	if !bytes.Equal(image.Pixels, s.cursor.Pixels) {
		t.Fatal("cursor texture changed premultiplied pixels")
	}
	if _, ok := a.ApplicationCursor(8); ok {
		t.Fatal("cursor was returned for another application")
	}
	s.cursor.HotspotX, s.cursor.Revision = 3, 2
	second, ok := a.ApplicationCursor(7)
	if !ok || second.Texture != first.Texture || second.Texture.Revision() != image.Revision || second.HotspotX != 3 {
		t.Fatal("hotspot-only change recreated or uploaded cursor image")
	}
	s.cursor.Hidden, s.cursor.Revision = true, 3
	if hidden, ok := a.ApplicationCursor(7); !ok || !hidden.Hidden || hidden.Texture != nil {
		t.Fatal("explicit hidden cursor became fallback")
	}
	s.cursor.Hidden, s.cursor.Revision = false, 4
	if visible, ok := a.ApplicationCursor(7); !ok || visible.Texture != first.Texture || visible.Texture.Revision() != image.Revision {
		t.Fatal("unhiding unchanged cursor uploaded image again")
	}
	s.cursor.ImageRevision++
	s.cursor.Width, s.cursor.Height = 4, 2
	s.cursor.Pixels = make([]byte, 32)
	updated, ok := a.ApplicationCursor(7)
	if !ok || updated.Texture != first.Texture || updated.Texture.Revision() == image.Revision {
		t.Fatal("changed cursor image lost retained texture identity")
	}
	if w, h := updated.Texture.Size(); w != 4 || h != 2 {
		t.Fatal("cursor resize was ignored")
	}
	a.Send(7, experience.Event{Kind: experience.PointerCancel})
	if _, ok := a.ApplicationCursor(7); ok {
		t.Fatal("same-frame pointer leave retained a cached client cursor")
	}
	s.current = s.current[1:]
	if err := a.poll(); err != nil {
		t.Fatal(err)
	}
	retired := 0
	for _, id := range a.retired {
		if id == first.Texture.ID() {
			retired++
		}
	}
	if a.cursorTexture != nil || retired != 1 {
		t.Fatal("disconnected cursor owner retained GPU resource or retired twice")
	}
	if _, ok := a.ApplicationCursor(7); ok {
		t.Fatal("disconnected pointer route still returned cursor")
	}
}

func TestApplicationCursorRejectsInvalidDimensions(t *testing.T) {
	for _, value := range []apps.Cursor{
		{Width: 513, Height: 1, Scale: 1}, {Width: 2, Height: 2, Scale: 0}, {Width: 2, Height: 2, Scale: 3}, {Width: 2, Height: 2, Scale: 1, Pixels: []byte{1}},
	} {
		s := &cursorAdapterServer{cursor: value}
		s.cursor.SurfaceID, s.cursor.Set = 1, true
		a := &applicationController{server: s, images: map[uint64]*applicationImage{1: {}}, output: io.Discard}
		if _, ok := a.ApplicationCursor(1); ok || a.err == nil {
			t.Fatal("invalid cursor became a renderable image")
		}
	}
}

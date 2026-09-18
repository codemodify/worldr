package render

import (
	"bytes"
	"image"
	"math"
	"runtime"
	"sync"
	"testing"
)

func TestTextureOwnsInputAndSnapshots(t *testing.T) {
	pixels := []byte{11, 22, 33, 44, 55, 66, 77, 88}
	tex, err := NewTexture(2, 1, pixels)
	if err != nil {
		t.Fatal(err)
	}
	pixels[0] = 99
	first, ok := tex.Snapshot(0)
	if !ok || first.Revision != 1 || first.Rect != image.Rect(0, 0, 2, 1) || first.Pixels[0] != 11 {
		t.Fatalf("bad initial upload: %+v", first)
	}
	first.Pixels[1] = 99
	second, _ := tex.Snapshot(0)
	if second.Pixels[1] != 22 {
		t.Fatal("snapshot exposed retained pixels")
	}
	if _, ok := tex.Snapshot(first.Revision); ok {
		t.Fatal("unchanged resource generated upload")
	}
	other, _ := NewTexture(2, 1, pixels)
	if other.ID() == tex.ID() || tex.ID() == 0 {
		t.Fatal("texture identity is not unique")
	}
}

func TestTextureDamageAndIndependentConsumers(t *testing.T) {
	tex, _ := NewTexture(4, 3, make([]byte, 4*3*4))
	initial, _ := tex.Snapshot(0)
	rect := image.Rect(1, 1, 3, 3)
	patch := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	if err := tex.Update(rect, patch); err != nil {
		t.Fatal(err)
	}
	patch[0] = 99
	upload, ok := tex.Snapshot(initial.Revision)
	if !ok || upload.Rect != rect || upload.Revision != initial.Revision+1 || upload.Pixels[0] != 1 || upload.Width != 4 || upload.Height != 3 {
		t.Fatalf("incorrect partial upload: %+v", upload)
	}
	// Reading a second renderer's damage cannot acknowledge the first one's.
	other, ok := tex.Snapshot(initial.Revision)
	if !ok || !bytes.Equal(other.Pixels, upload.Pixels) || other.Rect != rect {
		t.Fatal("one consumer cleared another consumer's damage")
	}
	if err := tex.Update(image.Rect(3, 0, 4, 1), []byte{21, 22, 23, 24}); err != nil {
		t.Fatal(err)
	}
	fresh, _ := tex.Snapshot(upload.Revision)
	if fresh.Rect != image.Rect(3, 0, 4, 1) || !bytes.Equal(fresh.Pixels, []byte{21, 22, 23, 24}) {
		t.Fatalf("incorrect latest damage: %+v", fresh)
	}
	// A stale consumer must receive both changes, even though only one damage
	// rectangle is retained. A brand-new snapshot renderer has the same need.
	for _, since := range []uint64{0, initial.Revision, math.MaxUint64} {
		full, ok := tex.Snapshot(since)
		if !ok || full.Rect != image.Rect(0, 0, 4, 3) {
			t.Fatalf("revision %d did not get full recovery upload", since)
		}
		want := make([]byte, 4*3*4)
		copy(want[12:16], []byte{21, 22, 23, 24})
		copy(want[20:28], []byte{1, 2, 3, 4, 5, 6, 7, 8})
		copy(want[36:44], []byte{9, 10, 11, 12, 13, 14, 15, 16})
		if !bytes.Equal(full.Pixels, want) {
			t.Fatalf("revision %d lost damage or changed untouched pixels", since)
		}
	}
}

func TestTextureResizeRetainsIdentityAndForcesFullUpload(t *testing.T) {
	tex, _ := NewTexture(2, 2, make([]byte, 16))
	id, previous := tex.ID(), tex.Revision()
	pixels := []byte{1, 2, 3, 255, 4, 5, 6, 255, 7, 8, 9, 255}
	if err := tex.Replace(3, 1, pixels); err != nil {
		t.Fatal(err)
	}
	pixels[0] = 99
	w, h := tex.Size()
	upload, ok := tex.Snapshot(previous)
	if !ok || tex.ID() != id || w != 3 || h != 1 || upload.Rect != image.Rect(0, 0, 3, 1) || upload.Pixels[0] != 1 {
		t.Fatalf("resize failed: %+v", upload)
	}
	// A resize followed by a patch still recovers the complete resized image for
	// a consumer that has the old size, not just the most recent small patch.
	if err := tex.Update(image.Rect(2, 0, 3, 1), []byte{10, 11, 12, 255}); err != nil {
		t.Fatal(err)
	}
	upload, _ = tex.Snapshot(previous)
	if upload.Rect != image.Rect(0, 0, 3, 1) || len(upload.Pixels) != 12 || upload.Pixels[0] != 1 || upload.Pixels[8] != 10 {
		t.Fatal("stale consumer did not recover resized image")
	}
}

func TestTextureRejectsInvalidChangesTransactionally(t *testing.T) {
	for _, size := range [][2]int{{0, 1}, {1, 0}, {-1, 1}, {math.MaxInt, 1}, {8193, 1}, {2, 2}} {
		if _, err := NewTexture(size[0], size[1], []byte{1, 2, 3, 4}); err == nil {
			t.Fatalf("accepted invalid texture %v", size)
		}
	}
	tex, _ := NewTexture(2, 2, bytes.Repeat([]byte{1, 2, 3, 4}, 4))
	before, _ := tex.Snapshot(0)
	for _, rect := range []image.Rectangle{
		{}, image.Rect(-1, 0, 1, 1), image.Rect(0, 0, 3, 1), image.Rect(0, 1, 1, 3), image.Rect(0, 0, 2, 2),
		{Min: image.Pt(math.MinInt, 0), Max: image.Pt(math.MaxInt, 1)},
	} {
		if err := tex.Update(rect, []byte{1, 2, 3, 4}); err == nil {
			t.Fatalf("accepted invalid damage %v", rect)
		}
	}
	if err := tex.Replace(2, 3, []byte{1}); err == nil {
		t.Fatal("accepted invalid replacement")
	}
	after, _ := tex.Snapshot(0)
	if before.Revision != after.Revision || before.Rect != after.Rect || !bytes.Equal(before.Pixels, after.Pixels) {
		t.Fatal("invalid mutation changed texture")
	}
	for _, missing := range []*Texture{nil, {}} {
		if err := missing.Update(image.Rect(0, 0, 1, 1), make([]byte, 4)); err == nil {
			t.Fatal("updated uninitialized texture")
		}
		if err := missing.Replace(1, 1, make([]byte, 4)); err == nil {
			t.Fatal("replaced uninitialized texture")
		}
		if _, ok := missing.Snapshot(0); ok || missing.ID() != 0 || missing.Revision() != 0 {
			t.Fatal("uninitialized texture has content")
		}
	}
}

func TestTextureConcurrentSnapshotsRemainCoherent(t *testing.T) {
	// Encode the full image size and revision into each pixel. An atomic
	// snapshot must never pair one resize's metadata with another's pixels.
	tex, err := NewTexture(2, 3, bytes.Repeat([]byte{2, 3, 1, 255}, 6))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for i := 0; i < 500; i++ {
			w, h := 2+i%3, 3+(i/3)%3
			revision := uint64(2*i + 2)
			if err := tex.Replace(w, h, bytes.Repeat([]byte{byte(w), byte(h), byte(revision), 255}, w*h)); err != nil {
				t.Errorf("concurrent replace: %v", err)
				return
			}
			runtime.Gosched()
			if err := tex.Update(image.Rect(0, 0, 1, 1), []byte{byte(w), byte(h), byte(revision + 1), 255}); err != nil {
				t.Errorf("concurrent update: %v", err)
				return
			}
			runtime.Gosched()
		}
	}()
	for reader := 0; reader < 4; reader++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			var previous uint64
			for i := 0; i < 1000; i++ {
				since := previous
				if i%3 == 0 {
					since = 0 // Exercise fresh/full and incremental consumers.
				}
				snapshot, changed := tex.Snapshot(since)
				if !changed {
					runtime.Gosched()
					continue
				}
				if snapshot.Revision < previous || (since != 0 && snapshot.Revision <= since) {
					t.Errorf("reader revision moved backwards: previous=%d, snapshot=%d", previous, snapshot.Revision)
					return
				}
				previous = snapshot.Revision
				if snapshot.Rect.Empty() || !snapshot.Rect.In(image.Rect(0, 0, snapshot.Width, snapshot.Height)) || len(snapshot.Pixels) != snapshot.Rect.Dx()*snapshot.Rect.Dy()*4 {
					t.Errorf("incoherent snapshot metadata: %+v", snapshot)
					return
				}
				for y := snapshot.Rect.Min.Y; y < snapshot.Rect.Max.Y; y++ {
					for x := snapshot.Rect.Min.X; x < snapshot.Rect.Max.X; x++ {
						offset := ((y-snapshot.Rect.Min.Y)*snapshot.Rect.Dx() + x - snapshot.Rect.Min.X) * 4
						marker := snapshot.Revision
						if marker > 1 && marker%2 == 1 && (x != 0 || y != 0) {
							marker-- // The odd revision only changes the first pixel.
						}
						want := []byte{byte(snapshot.Width), byte(snapshot.Height), byte(marker), 255}
						if !bytes.Equal(snapshot.Pixels[offset:offset+4], want) {
							t.Errorf("torn snapshot revision %d at (%d,%d): got %v, want %v", snapshot.Revision, x, y, snapshot.Pixels[offset:offset+4], want)
							return
						}
					}
				}
			}
		}()
	}
	close(start)
	workers.Wait()
}

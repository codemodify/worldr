package photoapp

import (
	"bytes"
	"context"
	"errors"
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

type decodeCall struct {
	file   *os.File
	answer chan photoResult
}

func controlledPhotoManager(t *testing.T) (*Manager, chan decodeCall) {
	t.Helper()
	calls := make(chan decodeCall, 1)
	m := newManager(func(ctx context.Context, file *os.File) (image.Image, error) {
		call := decodeCall{file: file, answer: make(chan photoResult, 1)}
		calls <- call
		// Model a codec already processing its bounded, buffered input: it
		// unwinds only when allowed, while cancellation must close its file.
		answer := <-call.answer
		return answer.image, answer.err
	})
	t.Cleanup(func() { _ = m.Close() })
	return m, calls
}

func nextDecode(t *testing.T, calls chan decodeCall) decodeCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(2 * time.Second):
		t.Fatal("decoder did not receive request")
		return decodeCall{}
	}
}
func awaitPhoto(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("photo condition timed out")
		}
		time.Sleep(time.Millisecond)
	}
}
func photoSolid(w, h int, red byte) *image.NRGBA {
	p := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(p.Pix); i += 4 {
		p.Pix[i], p.Pix[i+1], p.Pix[i+2], p.Pix[i+3] = red, 40, 80, 255
	}
	return p
}
func finishPhoto(t *testing.T, m *Manager, call decodeCall, photo image.Image, err error) {
	t.Helper()
	call.answer <- photoResult{image: photo, err: err}
	awaitPhoto(t, func() bool { return len(m.results) > 0 })
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
}

func TestPhotoOpenIsAsyncReservesSurfaceAndPreservesPriorImageOnFailure(t *testing.T) {
	m, calls := controlledPhotoManager(t)
	file := photoTestFile(t, []byte("fixture"))
	key, err := m.OpenFile(file, "first.png")
	if err != nil || key != "native:photo-viewer" {
		t.Fatalf("open: %q %v", key, err)
	}
	call := nextDecode(t, calls)
	if len(m.Surfaces()) != 1 || !m.loading || m.photo != nil {
		t.Fatal("OpenFile did not reserve a loading surface immediately")
	}
	if m.Surfaces()[0].AppID != "worldr.photo-viewer" {
		t.Fatal("incorrect native identity")
	}
	photo := photoSolid(20, 10, 180)
	finishPhoto(t, m, call, photo, nil)
	id, texture := m.next, m.renderer.texture
	if m.photo != photo || m.loading || m.message != "" {
		t.Fatal("decoded photo did not replace loading state")
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("decoded descriptor remained open")
	}
	bad := photoTestFile(t, []byte("bad"))
	if _, err := m.OpenFile(bad, "broken.png"); err != nil {
		t.Fatal(err)
	}
	call = nextDecode(t, calls)
	if m.photo != photo || m.next != id || m.renderer.texture != texture {
		t.Fatal("pending replacement discarded working photo")
	}
	finishPhoto(t, m, call, nil, errors.New("invalid header"))
	if m.photo != photo || m.next != id || m.renderer.texture != texture {
		t.Fatal("failed replacement changed the existing image or view")
	}
	if !strings.Contains(m.message, "broken.png") || !strings.Contains(m.message, "invalid header") {
		t.Fatal("decode failure was not visible")
	}
	revision := texture.Revision()
	for i := 0; i < 5; i++ {
		if err := m.Poll(); err != nil {
			t.Fatal(err)
		}
	}
	if texture.Revision() != revision {
		t.Fatal("unchanged decode error caused continual image uploads")
	}
	replacement := photoSolid(12, 20, 75)
	if _, err := m.OpenFile(photoTestFile(t, []byte("second")), "second.jpg"); err != nil {
		t.Fatal(err)
	}
	finishPhoto(t, m, nextDecode(t, calls), replacement, nil)
	if m.next == id || m.renderer.texture == texture || m.photo != replacement || m.message != "" {
		t.Fatal("successful replacement did not replace photo and runtime identity")
	}
	retired := m.RetiredTextures()
	if len(retired) != 1 || retired[0] != texture.ID() || len(m.RetiredTextures()) != 0 {
		t.Fatal("old retained texture was not retired exactly once")
	}
	m.CloseApplication(id)
	if len(m.Surfaces()) != 1 {
		t.Fatal("stale close closed replacement")
	}
	m.CloseApplication(m.next)
	if len(m.Surfaces()) != 0 || m.photo != nil {
		t.Fatal("viewer close retained photo")
	}
}

func TestPhotoCancellationClosesActiveAndSupersededDescriptors(t *testing.T) {
	m, calls := controlledPhotoManager(t)
	first := photoTestFile(t, []byte("first"))
	if _, err := m.OpenFile(first, "first.png"); err != nil {
		t.Fatal(err)
	}
	active := nextDecode(t, calls)
	second, third := photoTestFile(t, []byte("second")), photoTestFile(t, []byte("third"))
	if _, err := m.OpenFile(second, "second.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.OpenFile(third, "third.png"); err != nil {
		t.Fatal(err)
	}
	awaitPhoto(t, func() bool { _, err := first.Stat(); return errors.Is(err, os.ErrClosed) })
	if _, err := second.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("superseded pending descriptor leaked")
	}
	active.answer <- photoResult{image: photoSolid(2, 2, 50)}
	current := nextDecode(t, calls)
	if current.file != third {
		t.Fatal("worker decoded a superseded pending image")
	}
	photo := photoSolid(3, 2, 200)
	finishPhoto(t, m, current, photo, nil)
	if m.photo != photo || m.title != "third.png" {
		t.Fatal("stale canceled result replaced current image")
	}
	pending := photoTestFile(t, []byte("close"))
	if _, err := m.OpenFile(pending, "closing.png"); err != nil {
		t.Fatal(err)
	}
	closing := nextDecode(t, calls)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	awaitPhoto(t, func() bool { _, err := pending.Stat(); return errors.Is(err, os.ErrClosed) })
	closing.answer <- photoResult{image: photoSolid(2, 2, 20)}
	select {
	case <-m.done:
	case <-time.After(2 * time.Second):
		t.Fatal("decoder worker did not stop")
	}
	if len(m.results) != 0 || len(m.Surfaces()) != 0 {
		t.Fatal("closed manager retained worker result or surface")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPhotoFailedEnqueueLeavesCallerOwnership(t *testing.T) {
	m := NewManager()
	t.Cleanup(func() { _ = m.Close() })
	if _, err := m.OpenFile(nil, "nil.png"); err == nil {
		t.Fatal("accepted nil file")
	}
	directory, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if _, err := m.OpenFile(directory, "directory"); err == nil {
		t.Fatal("accepted directory")
	}
	if _, err := directory.Stat(); err != nil {
		t.Fatal("failed enqueue stole caller descriptor")
	}
	if len(m.Surfaces()) != 0 {
		t.Fatal("failed enqueue reserved a surface")
	}
	file := photoTestFile(t, []byte("closed"))
	_ = m.Close()
	if _, err := m.OpenFile(file, "closed.png"); err == nil {
		t.Fatal("closed manager accepted file")
	}
	if _, err := file.Stat(); err != nil {
		t.Fatal("closed manager stole caller file")
	}
}

func TestPhotoIsViewOnlyAndResizeBoxIsIndependentOfTexture(t *testing.T) {
	m, calls := controlledPhotoManager(t)
	if _, err := m.OpenFile(photoTestFile(t, nil), "wide.png"); err != nil {
		t.Fatal(err)
	}
	finishPhoto(t, m, nextDecode(t, calls), photoSolid(2000, 1200, 100), nil)
	if surface := m.Surfaces()[0]; surface.Frameless || !surface.DragContent || surface.Title != "wide.png" {
		t.Fatal("photo did not allow its workspace bracket and content dragging with filename metadata")
	}
	if m.renderer.image.Rect.Size() != image.Pt(1120, 672) {
		t.Fatal("initial photo did not tightly fit its requested display box")
	}
	texture := m.renderer.texture
	original, _ := texture.Snapshot(0)
	m.Focus(m.next)
	for _, event := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: 50, Y: 50},
		{Kind: experience.PointerMove, X: 1000, Y: 1000},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: 1000, Y: 1000},
		{Kind: experience.PointerScroll, X: 100, Y: 100, ScrollY: -10},
		{Kind: experience.KeyInput, Pressed: true, Key: experience.KeyR},
		{Kind: experience.KeyInput, Pressed: true, Key: experience.KeyF},
		{Kind: experience.KeyInput, Pressed: true, Key: experience.Key1},
		{Kind: experience.KeyInput, Pressed: true, Key: experience.KeyLeft},
		{Kind: experience.KeyInput, Pressed: true, Key: "+"},
		{Kind: experience.PointerCancel}, {Kind: experience.KeyboardCancel},
	} {
		m.Send(m.next, event)
		m.Seat(event)
	}
	m.Focus(0)
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	if texture.Revision() != original.Revision {
		t.Fatal("input or focus changed the photograph or triggered an upload")
	}
	m.Resize(m.next, 1440, 900)
	if m.renderer.texture != texture || m.renderer.image.Rect.Size() != image.Pt(1440, 864) || m.renderer.requested != image.Pt(1440, 900) {
		t.Fatal("resize lost stable texture identity, source aspect, or requested box")
	}
	revision := texture.Revision()
	for i := 0; i < 3; i++ {
		m.Resize(m.next, 1440, 900)
	}
	if texture.Revision() != revision {
		t.Fatal("tight texture dimensions caused repeated resize uploads")
	}
	m.Resize(m.next+1, 1920, 1080)
	if texture.Revision() != revision {
		t.Fatal("stale surface ID resized current photo")
	}
	m.Resize(m.next, -1, 200)
	if m.renderer.requested != image.Pt(640, 400) || m.renderer.image.Rect.Size() != image.Pt(640, 384) {
		t.Fatal("requested resize bounds clamped the photo instead of its display box")
	}
	if _, _, _, alpha := m.photo.At(0, 0).RGBA(); alpha != 65535 {
		t.Fatal("presentation modified source pixels")
	}
}

func TestPhotoReplacementKeepsRequestedBoxAndInertInput(t *testing.T) {
	m, calls := controlledPhotoManager(t)
	if _, err := m.OpenFile(photoTestFile(t, nil), "portrait.png"); err != nil {
		t.Fatal(err)
	}
	finishPhoto(t, m, nextDecode(t, calls), photoSolid(10, 40, 50), nil)
	id := m.next
	m.Resize(id, 1440, 900)
	if m.renderer.image.Rect.Size() != image.Pt(225, 900) {
		t.Fatal("portrait did not use a tight texture below the old minimum width")
	}
	original, _ := m.renderer.texture.Snapshot(0)
	if _, err := m.OpenFile(photoTestFile(t, nil), "landscape.png"); err != nil {
		t.Fatal(err)
	}
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	pending, _ := m.renderer.texture.Snapshot(0)
	if !bytes.Equal(original.Pixels, pending.Pixels) {
		t.Fatal("pending replacement painted controls or a loading message over the photo")
	}
	finishPhoto(t, m, nextDecode(t, calls), photoSolid(40, 10, 200), nil)
	if m.renderer.requested != image.Pt(1440, 900) || m.renderer.image.Rect.Size() != image.Pt(1440, 360) {
		t.Fatal("landscape replacement inherited portrait texture dimensions as its display box")
	}
	before := m.renderer.texture.Revision()
	m.Send(id, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary})
	m.Resize(id, 640, 400)
	if m.renderer.texture.Revision() != before {
		t.Fatal("stale photo routes modified replacement")
	}
}

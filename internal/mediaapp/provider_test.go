package mediaapp

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/media"
	"github.com/codemodify/worldr/internal/nativeui"
)

type fakePlayback struct {
	state  media.State
	closed bool
	seeks  []float64
	frames int
}

func (f *fakePlayback) Load(*os.File) error        { return nil }
func (f *fakePlayback) Poll() (media.State, error) { return f.state, nil }
func (f *fakePlayback) Render(p []byte, w, h, stride int) (bool, error) {
	if f.frames > 0 {
		return false, nil
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*stride + x*4
			copy(p[i:i+4], []byte{180, 30, 60, 255})
		}
	}
	f.frames++
	return true, nil
}
func (f *fakePlayback) Pause(v bool) error { f.state.Paused = v; return nil }
func (f *fakePlayback) Seek(v float64) error {
	f.seeks = append(f.seeks, v)
	f.state.Position = v
	f.state.Ended = false
	f.state.SeekRevision++
	return nil
}
func (f *fakePlayback) SetVolume(v float64) error { f.state.Volume = v; return nil }
func (f *fakePlayback) SetMute(v bool) error      { f.state.Muted = v; return nil }
func (f *fakePlayback) Close() error              { f.closed = true; return nil }

func mediaFile(t *testing.T) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
func testManager(t *testing.T) (*Manager, *fakePlayback) {
	t.Helper()
	m := NewManager(media.Options{})
	f := &fakePlayback{state: media.State{Loaded: true, HasVideo: true, Duration: 120, Volume: 70, Width: 1280, Height: 720}}
	m.factory = func(media.Options) (playback, error) { return f, nil }
	t.Cleanup(func() { _ = m.Close() })
	if _, err := m.OpenFile(mediaFile(t), "test.mp4"); err != nil {
		t.Fatal(err)
	}
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	return m, f
}
func clickControl(m *Manager, r image.Rectangle) {
	p := r.Min.Add(image.Pt(r.Dx()/2, r.Dy()/2))
	for _, kind := range []experience.EventKind{experience.PointerDown, experience.PointerUp} {
		m.Send(m.next, experience.Event{Kind: kind, Button: experience.ButtonPrimary, X: float32(p.X), Y: float32(p.Y)})
	}
}

func TestMediaSemanticsTrackPlaybackAndResize(t *testing.T) {
	m, _ := testManager(t)
	tree := m.Semantics(m.next)
	if len(tree.Nodes) != 7 || tree.Nodes[1].ID != "play" || tree.Nodes[1].Label != "Pause" || !tree.Nodes[1].Selected || tree.Nodes[5].Role != nativeui.RoleSlider {
		t.Fatalf("playing semantics: %+v", tree)
	}
	old := tree.Nodes[1].Bounds
	m.Resize(m.next, 640, 360)
	tree = m.Semantics(m.next)
	if tree.Nodes[1].Bounds == old || !tree.Nodes[1].Bounds.In(m.renderer.image.Rect) {
		t.Fatalf("resized semantics: %+v", tree.Nodes[1])
	}
	m.state.Paused, m.state.Muted = true, true
	tree = m.Semantics(m.next)
	if tree.Nodes[1].Label != "Play" || tree.Nodes[1].Selected || tree.Nodes[4].Label != "Unmute" || !tree.Nodes[4].Selected {
		t.Fatalf("paused semantics: %+v", tree)
	}
}

func TestPlayerControlsAndCapturedSeek(t *testing.T) {
	m, p := testManager(t)
	l := m.renderer.layout()
	clickControl(m, l.Play)
	if !p.state.Paused {
		t.Fatal("pause click failed")
	}
	clickControl(m, l.Play)
	if p.state.Paused {
		t.Fatal("resume failed")
	}
	clickControl(m, l.Forward)
	if p.state.Position != 10 {
		t.Fatal("forward seek failed")
	}
	clickControl(m, l.Back)
	if p.state.Position != 0 {
		t.Fatal("back seek failed")
	}
	clickControl(m, l.Mute)
	if !p.state.Muted {
		t.Fatal("mute failed")
	}
	clickControl(m, l.Volume)
	if p.state.Volume < 49 || p.state.Volume > 51 {
		t.Fatal("volume failed")
	}
	m.Send(m.next, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: float32(l.Seek.Min.X), Y: float32(l.Seek.Min.Y + 8)})
	m.Send(m.next, experience.Event{Kind: experience.PointerMove, X: float32(l.Seek.Max.X + 100), Y: 0})
	if m.view().Position != 120 {
		t.Fatal("seek preview did not clamp to duration")
	}
	m.Send(m.next, experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: float32(l.Seek.Max.X + 100), Y: 0})
	if p.state.Position != 120 || m.pressed != "" {
		t.Fatal("captured timeline release failed")
	}
	clickControl(m, l.Stop)
	if !p.state.Paused || p.state.Position != 0 {
		t.Fatal("stop failed")
	}
	m.state.Ended = true
	clickControl(m, l.Play)
	if p.state.Paused || p.state.Position != 0 || m.state.Ended {
		t.Fatal("replay failed")
	}
	m.Focus(m.next)
	m.Send(m.next, experience.Event{Kind: experience.KeyInput, Key: experience.KeySpace, Pressed: true})
	if !p.state.Paused {
		t.Fatal("focused keyboard pause failed")
	}
	m.Focus(0)
	m.Send(m.next, experience.Event{Kind: experience.KeyInput, Key: experience.KeySpace, Pressed: true})
	if !p.state.Paused {
		t.Fatal("unfocused player accepted keys")
	}
}

func TestPlayerReplacementOwnsFileAndRetiresTexture(t *testing.T) {
	m, p := testManager(t)
	oldID, oldTexture, oldFile := m.next, m.renderer.texture.ID(), m.file
	file := mediaFile(t)
	replacement := &fakePlayback{state: p.state}
	m.factory = func(media.Options) (playback, error) { return replacement, nil }
	if key, err := m.OpenFile(file, "next.mp4"); err != nil || key != "native:media-player" {
		t.Fatalf("replace: %s %v", key, err)
	}
	if !p.closed || m.next == oldID {
		t.Fatal("old decoder or runtime identity was retained")
	}
	if _, err := oldFile.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("old descriptor leaked")
	}
	ids := m.RetiredTextures()
	if len(ids) != 1 || ids[0] != oldTexture || len(m.RetiredTextures()) != 0 {
		t.Fatal("retired texture was lost or returned twice")
	}
	m.CloseApplication(oldID)
	if len(m.Surfaces()) != 1 {
		t.Fatal("stale close closed replacement")
	}
	m.CloseApplication(m.next)
	if len(m.Surfaces()) != 0 || !replacement.closed {
		t.Fatal("closing failed")
	}
	if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("descriptor leaked on close")
	}
}

func TestFailedOpenKeepsCurrentPlayerAndCallerFile(t *testing.T) {
	m, p := testManager(t)
	id := m.next
	file := mediaFile(t)
	m.factory = func(media.Options) (playback, error) { return nil, errors.New("unavailable") }
	if _, err := m.OpenFile(file, "bad.mp4"); err == nil {
		t.Fatal("open error missing")
	}
	if p.closed || m.next != id || len(m.Surfaces()) != 1 {
		t.Fatal("failed replacement destroyed current player")
	}
	if _, err := file.Stat(); err != nil {
		t.Fatal("failed open stole caller file")
	}
}

func TestPlayerFramesStayInsideChromeAndControlsFitSizes(t *testing.T) {
	m, p := testManager(t)
	texture, id := m.renderer.texture, m.next
	for _, size := range [][2]int{{640, 360}, {640, 1080}, {1920, 360}, {960, 600}, {1001, 701}, {1440, 900}, {1920, 1080}} {
		m.Resize(m.next, size[0], size[1])
		p.frames = 0
		if err := m.Poll(); err != nil {
			t.Fatal(err)
		}
		l := m.renderer.layout()
		regions := playerControlRegions(l)
		for i, region := range regions {
			rect := region.rect
			if rect.Dx() < 24 || rect.Dy() < 24 || !rect.In(m.renderer.image.Rect) {
				t.Fatalf("%s lacks a usable hit area at %v: %v", region.name, size, rect)
			}
			for _, other := range regions[i+1:] {
				if rect.Overlaps(other.rect) {
					t.Fatalf("%s overlaps %s at %v", region.name, other.name, size)
				}
			}
			for _, point := range []image.Point{rect.Min, rect.Min.Add(image.Pt(rect.Dx()/2, rect.Dy()/2)), rect.Max.Sub(image.Pt(1, 1))} {
				if target := m.target(float32(point.X), float32(point.Y)); target != region.name {
					t.Fatalf("%s hit area routes to %q at %v, size %v", region.name, target, point, size)
				}
			}
		}
		if l.Video.Dx() < 320 || l.Video.Dy() < 96 {
			t.Fatalf("video collapsed at supported size %v: %v", size, l.Video)
		}
		if m.renderer.texture != texture || m.next != id || m.focused {
			t.Fatal("resize replaced the retained window or took keyboard focus")
		}
		inside := m.renderer.image.RGBAAt(l.Video.Min.X+10, l.Video.Min.Y+10)
		if inside.R != 180 || inside.G != 30 || inside.B != 60 {
			t.Fatal("video did not reach retained image")
		}
		outside := m.renderer.image.RGBAAt(10, 10)
		if outside == inside {
			t.Fatal("video overwrote chrome")
		}
	}
}

type playerControlRegion struct {
	name string
	rect image.Rectangle
}

func playerControlRegions(l playerLayout) []playerControlRegion {
	return []playerControlRegion{{"video", l.Video}, {"seek", l.Seek}, {"play", l.Play}, {"back", l.Back}, {"stop", l.Stop}, {"forward", l.Forward}, {"mute", l.Mute}, {"volume", l.Volume}}
}

func TestPlayerControlsRouteAfterExtremeAndFractionalResizes(t *testing.T) {
	for _, size := range [][2]int{{640, 1080}, {1920, 360}, {1001, 701}, {1920, 1080}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m, p := testManager(t)
			m.Resize(m.next, size[0], size[1])
			p.frames, p.state.Position = 0, 20
			if err := m.Poll(); err != nil {
				t.Fatal(err)
			}
			l := m.renderer.layout()
			clickControl(m, l.Play)
			if !p.state.Paused {
				t.Fatal("resized play control missed its target")
			}
			clickControl(m, l.Video)
			if p.state.Paused {
				t.Fatal("resized video click did not resume playback")
			}
			clickControl(m, l.Back)
			if p.state.Position != 10 {
				t.Fatal("resized back control did not seek backward")
			}
			clickControl(m, l.Forward)
			if p.state.Position != 20 {
				t.Fatal("resized forward control did not seek forward")
			}
			clickControl(m, l.Seek)
			if p.state.Position < 59.75 || p.state.Position > 60.25 {
				t.Fatalf("resized seek midpoint sought to %v", p.state.Position)
			}
			clickControl(m, l.Mute)
			if !p.state.Muted {
				t.Fatal("resized mute control did not toggle audio")
			}
			clickControl(m, l.Volume)
			if p.state.Volume < 49 || p.state.Volume > 51 {
				t.Fatalf("resized volume midpoint set %v", p.state.Volume)
			}
			m.Send(m.next, experience.Event{Kind: experience.PointerDown, ButtonCode: 272, X: float32(l.Volume.Min.X), Y: float32(l.Volume.Min.Y + l.Volume.Dy()/2)})
			m.Send(m.next, experience.Event{Kind: experience.PointerMove, X: float32(l.Volume.Max.X + 300), Y: -20})
			m.Send(m.next, experience.Event{Kind: experience.PointerUp, ButtonCode: 272, X: float32(l.Volume.Max.X + 300), Y: -20})
			if p.state.Volume != 100 || m.pressed != "" {
				t.Fatal("volume drag lost capture or failed to clamp outside its resized hit area")
			}
			clickControl(m, l.Stop)
			if !p.state.Paused || p.state.Position != 0 {
				t.Fatal("resized stop control failed")
			}
			m.Send(m.next, experience.Event{Kind: experience.KeyInput, Key: experience.KeySpace, Pressed: true})
			if !p.state.Paused || m.focused {
				t.Fatal("resizing or pointer routing stole keyboard focus")
			}
			m.Focus(m.next)
			m.Send(m.next, experience.Event{Kind: experience.KeyInput, Key: experience.KeySpace, Pressed: true})
			if p.state.Paused {
				t.Fatal("explicit focus did not restore keyboard controls after resizing")
			}
		})
	}
}

func TestPlayerResizeCancelsPendingControlActivation(t *testing.T) {
	m, p := testManager(t)
	l := m.renderer.layout()
	m.Send(m.next, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: float32(l.Seek.Min.X + l.Seek.Dx()/2), Y: float32(l.Seek.Min.Y + l.Seek.Dy()/2)})
	m.Resize(m.next, 1920, 360)
	m.Send(m.next, experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: float32(l.Seek.Max.X), Y: float32(l.Seek.Min.Y)})
	if len(p.seeks) != 0 || m.pressed != "" {
		t.Fatal("release using the old layout sought after resize canceled the gesture")
	}
}

func TestPausedResizeRoundTripRetainsDecodedFrame(t *testing.T) {
	m, p := testManager(t)
	p.state.Paused = true
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	w, h := m.renderer.image.Rect.Dx(), m.renderer.image.Rect.Dy()
	buffer := m.video
	m.Resize(m.next, 1440, 900)
	m.Resize(m.next, w, h)
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	l := m.renderer.layout()
	pixel := m.renderer.image.RGBAAt(l.Video.Min.X+10, l.Video.Min.Y+10)
	if m.video != buffer || pixel.R != 180 || pixel.G != 30 || pixel.B != 60 {
		t.Fatal("paused resize round trip lost the retained video frame")
	}
}

func TestRetainedDecoderErrorDoesNotUploadEveryPoll(t *testing.T) {
	m, p := testManager(t)
	p.state = media.State{Error: "Unsupported video"}
	if err := m.Poll(); err != nil {
		t.Fatal(err)
	}
	revision := m.renderer.texture.Revision()
	for i := 0; i < 3; i++ {
		if err := m.Poll(); err != nil {
			t.Fatal(err)
		}
	}
	if m.renderer.texture.Revision() != revision || m.message != "Unsupported video" {
		t.Fatal("retained decoder error repainted an unchanged frame")
	}
}

package app

import (
	"context"
	"errors"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/platform/linux/host"
	"github.com/codemodify/worldr/internal/platform/linux/native"
)

func TestPNGPreservesPremultipliedTransparency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.png")
	if err := savePNG(path, []byte{0, 0, 128, 128, 255, 0, 0, 255}, 1, 2); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA); got != (color.NRGBA{255, 0, 0, 128}) {
		t.Fatalf("transparent pixel: %+v", got)
	}
	if got := color.NRGBAModel.Convert(img.At(0, 1)).(color.NRGBA); got != (color.NRGBA{0, 0, 255, 255}) {
		t.Fatalf("bottom pixel: %+v", got)
	}
}

func TestNativeEventPreservesTerminalAndCompositionData(t *testing.T) {
	physical := windowEvent(host.Event{Kind: host.Key, Keycode: 47, Code: 'v', Modifiers: 3, Pressed: true, Depressed: 5, Locked: 2, Group: 1})
	if physical.Kind != KeyInput || physical.Key != Key("V") || physical.Keycode != 47 || physical.Modifiers != ModControl|ModShift || physical.Depressed != 5 || physical.Locked != 2 || physical.Group != 1 {
		t.Fatalf("physical key: %+v", physical)
	}
	commit := windowEvent(host.Event{Kind: host.TextCommit, Text: "é", TextContext: "shell", DeleteBefore: 1})
	if commit.Kind != TextCommit || commit.Text != "é" || commit.TextContext != "shell" || commit.DeleteBefore != 1 {
		t.Fatalf("composition: %+v", commit)
	}
	scroll := windowEvent(host.Event{Kind: host.Scroll, ScrollX: 1.25, ScrollY: -3.5, X: 21, Y: 46})
	if scroll.Kind != PointerScroll || scroll.ScrollX != 1.25 || scroll.ScrollY != -3.5 || scroll.X != 21 || scroll.Y != 46 {
		t.Fatalf("scroll: %+v", scroll)
	}
}

type testApplication struct {
	canvas                 *Canvas
	host                   *Host
	scale                  float32
	updates, draws, closes int
	failUpdate             error
}

func (a *testApplication) Atlas() Atlas               { return a.canvas.Atlas() }
func (a *testApplication) Update(time.Duration) error { a.updates++; return a.failUpdate }
func (a *testApplication) Draw(w, h int) Frame {
	a.draws++
	a.canvas.Reset(w, h)
	a.canvas.SetLinearColor(true)
	a.canvas.Rect(8, 8, 16, 16, ColorHex(0x40c8ff, .5))
	return a.canvas.Frame()
}
func (*testApplication) Handle(Event) bool        { return false }
func (*testApplication) NeedsFrame() bool         { return false }
func (a *testApplication) SetHost(host *Host)     { a.host = host }
func (a *testApplication) SetScale(scale float32) { a.scale = scale }
func (a *testApplication) Close() error {
	a.closes++
	if a.canvas != nil {
		return a.canvas.Close()
	}
	return nil
}

func TestRunClosesApplicationWhenOptionsFail(t *testing.T) {
	a := &testApplication{}
	if err := Run(context.Background(), Options{Width: -1}, a); err == nil || a.closes != 1 {
		t.Fatalf("err=%v closes=%d", err, a.closes)
	}
}

func TestStandaloneHeadlessLifecycleGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_GPU=1 to test standalone Vulkan hosting")
	}
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "snapshot", true: "update failure"}[fail], func(t *testing.T) {
			canvas, err := NewCanvas()
			if err != nil {
				t.Fatal(err)
			}
			a := &testApplication{canvas: canvas}
			updateErr := errors.New("application failure")
			if fail {
				a.failUpdate = updateErr
			}
			path := filepath.Join(t.TempDir(), "frame.png")
			err = Run(context.Background(), Options{Width: 64, Height: 48, FPS: 240, Frames: 2, Headless: true, Transparent: true, Snapshot: path}, a)
			if a.closes != 1 || a.host == nil || a.scale != 1 {
				t.Fatalf("lifecycle closes=%d host=%v scale=%v", a.closes, a.host != nil, a.scale)
			}
			if fail {
				if !errors.Is(err, updateErr) {
					t.Fatalf("lost update error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if a.updates != 2 {
				t.Fatalf("frame limit performed %d updates", a.updates)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			img, err := png.Decode(file)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, _, alpha := img.At(0, 0).RGBA(); alpha != 0 {
				t.Fatalf("window exterior alpha=%d", alpha)
			}
			pixel := color.NRGBAModel.Convert(img.At(12, 12)).(color.NRGBA)
			if pixel.A < 126 || pixel.A > 129 || pixel.R < 60 || pixel.R > 67 || pixel.G < 197 || pixel.G > 203 || pixel.B < 252 {
				t.Fatalf("transparent cyan changed: %+v", pixel)
			}
		})
	}
}

func TestStandaloneIdleStillUpdatesWithoutRedrawingGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_GPU=1 to test standalone Vulkan hosting")
	}
	canvas, err := NewCanvas()
	if err != nil {
		t.Fatal(err)
	}
	a := &testApplication{canvas: canvas}
	if err := Run(context.Background(), Options{Width: 64, Height: 48, FPS: 240, Duration: 40 * time.Millisecond, Headless: true}, a); err != nil {
		t.Fatal(err)
	}
	if a.updates < 2 {
		t.Fatalf("idle application received only %d updates", a.updates)
	}
	// One layout preparation and one frame, despite continuing updates.
	if a.draws != 2 {
		t.Fatalf("idle application redrew %d times", a.draws)
	}
}

func TestExplicitTextureLifetimeRetainsInactiveViewGPU(t *testing.T) {
	if os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_GPU=1 to test standalone Vulkan hosting")
	}
	gpu, err := native.OpenVK(false, 32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer gpu.Close()
	if err := gpu.SetSceneAtlas(Atlas{Width: 1, Height: 1, Pixels: []byte{255}}); err != nil {
		t.Fatal(err)
	}
	texture, err := NewTexture(1, 1, []byte{128, 0, 0, 128})
	if err != nil {
		t.Fatal(err)
	}
	frame := Frame{LinearColor: true, Commands: []Command{{Kind: ImageCommand, Image: Image{Texture: texture, Bounds: [4]float32{0, 0, 16, 16}}}}}
	resources := resourceSet{textures: map[uint64]struct{}{}, geometry: map[uint64]struct{}{}}
	if err := gpu.RenderFrame(frame, [4]float32{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := resources.retire(gpu, frame, true, nil); err != nil {
		t.Fatal(err)
	}
	resident := gpu.MemoryStats()
	empty := Frame{LinearColor: true}
	if err := gpu.RenderFrame(empty, [4]float32{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := resources.retire(gpu, empty, true, nil); err != nil {
		t.Fatal(err)
	}
	if stats := gpu.MemoryStats(); stats.Images != resident.Images {
		t.Fatalf("inactive cached view lost textures: %d -> %d", resident.Images, stats.Images)
	}
	if err := resources.retire(gpu, empty, true, []uint64{texture.ID()}); err != nil {
		t.Fatal(err)
	}
	if stats := gpu.MemoryStats(); stats.Images+1 != resident.Images {
		t.Fatalf("explicit retirement did not release texture: %d -> %d", resident.Images, stats.Images)
	}
	if len(resources.textures) != 0 {
		t.Fatal("retired texture identity remains retained")
	}
}

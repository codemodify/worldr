//go:build linux && cgo

package native

import (
	"bytes"
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/render"
)

func glowFixture(t *testing.T) (render.View, render.Draw) {
	t.Helper()
	view, emitter := materialPlane(t)
	emitter.Model[0], emitter.Model[5] = .16, .16
	emitter.Color, emitter.Unlit = [4]float32{.12, .16, .2, 1}, true
	emitter.Glow = [3]float32{.9, .35, .1}
	return view, emitter
}

func glowFrame(t *testing.T, vk *VK, frame render.Frame) []byte {
	t.Helper()
	w, h := vk.Size()
	pixels := make([]byte, int(w*h)*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	return pixels
}

func glowScene(view render.View, draws ...render.Draw) render.Command {
	return render.Command{Kind: render.SceneCommand, View: view, Draws: draws}
}

func glowOff(frame render.Frame) render.Frame {
	frame.Commands = append([]render.Command(nil), frame.Commands...)
	for i := range frame.Commands {
		command := &frame.Commands[i]
		command.Draws = append([]render.Draw(nil), command.Draws...)
		for j := range command.Draws {
			command.Draws[j].Glow = [3]float32{}
		}
	}
	return frame
}

func changedPixelsInRect(before, after []byte, width, x0, y0, x1, y1 int) int {
	count := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			off := (y*width + x) * 4
			if !bytes.Equal(before[off:off+4], after[off:off+4]) {
				count++
			}
		}
	}
	return count
}

func TestAuthoredGlowSpillsAndZeroRestoresExactBaselineGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, emitter := glowFixture(t)
	bright := emitter
	bright.Glow, bright.Color = [3]float32{}, [4]float32{1, 1, 1, 1}
	bright.Model[0], bright.Model[5], bright.Model[12], bright.Model[13] = .08, .08, .75, .6
	frame := render.Frame{Commands: []render.Command{glowScene(view, emitter, bright)}}
	baseline := glowFrame(t, vk, glowOff(frame))
	samples := vk.SampleCount()
	glowing := glowFrame(t, vk, frame)
	// The mesh occupies roughly pixels 27..37. These pixels are outside its
	// rasterized footprint, but inside the bounded blur's surrounding support.
	spill := changedPixelsInRect(baseline, glowing, 64, 23, 29, 27, 35) + changedPixelsInRect(baseline, glowing, 64, 37, 29, 41, 35)
	if spill == 0 {
		t.Fatal("authored glow changed no pixels beyond the emitting mesh")
	}
	if changedPixelsInRect(baseline, glowing, 64, 49, 7, 64, 20) != 0 {
		t.Fatal("bright unmarked mesh was treated as a threshold-based glow source")
	}
	if samples != vk.SampleCount() {
		t.Fatal("enabling glow changed the normal scene's MSAA sample count")
	}
	for i := 0; i < len(glowing); i += 4 {
		if glowing[i+3] != 255 {
			t.Fatal("background halo changed framebuffer alpha")
		}
	}
	for i := 0; i < 12; i++ {
		if next := glowFrame(t, vk, frame); !bytes.Equal(glowing, next) {
			t.Fatalf("static glow changed on frame %d", i)
		}
	}
	if zero := glowFrame(t, vk, glowOff(frame)); !bytes.Equal(baseline, zero) {
		t.Fatal("zero glow did not restore the exact original frame after enabled rendering")
	}
	if len(vk.frame.uploaded) != 1 {
		t.Fatal("changing glow recreated retained mesh geometry")
	}
}

func TestAuthoredGlowOccludedSeedAndNeighboringAppRemainExactGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, emitter := glowFixture(t)
	// Alpha zero deliberately tests the existing opaque application contract.
	pixels := textureSolid(8, 8, [4]byte{32, 64, 96, 0})
	for y := 1; y < 7; y += 2 {
		for x := 1; x < 7; x++ {
			copy(pixels[(y*8+x)*4:][:4], []byte{240, 240, 240, 0})
		}
	}
	texture, err := render.NewTexture(8, 8, pixels)
	if err != nil {
		t.Fatal(err)
	}
	_, model := textureView()
	model[0], model[5], model[14] = 1.4, 1.4, .2
	application := render.Draw{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}}
	for _, order := range [][]render.Draw{{emitter, application}, {application, emitter}} {
		frame := render.Frame{Commands: []render.Command{glowScene(view, order...)}}
		if baseline, glowing := glowFrame(t, vk, glowOff(frame)), glowFrame(t, vk, frame); !bytes.Equal(baseline, glowing) {
			t.Fatal("opaque foreground application failed to occlude the glow seed in both draw orders")
		}
	}
	// The application now sits beside the emitter and would intersect its halo.
	// Its bright terminal-like text must remain exact, even without hiding the
	// emission source itself. The left halo stays visible as a control.
	application.Model[0], application.Model[5], application.Model[12] = .5, .8, .45
	frame := render.Frame{Commands: []render.Command{glowScene(view, emitter, application)}}
	baseline, glowing := glowFrame(t, vk, glowOff(frame)), glowFrame(t, vk, frame)
	if changedPixelsInRect(baseline, glowing, 64, 40, 21, 53, 43) != 0 {
		t.Fatal("background halo washed over neighboring application's opaque text/content")
	}
	if changedPixelsInRect(baseline, glowing, 64, 23, 29, 27, 35) == 0 {
		t.Fatal("neighboring-app comparison did not contain an independently visible halo")
	}
}

func TestAuthoredGlowHonorsAlphaAndDepthReadOnlyMeshesGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, emitter := glowFixture(t)
	emitter.DepthReadOnly = true
	frame := render.Frame{Commands: []render.Command{glowScene(view, emitter)}}
	full := glowFrame(t, vk, frame)
	emitter.Color[3] = .5
	frame.Commands[0].Draws[0] = emitter
	half := glowFrame(t, vk, frame)
	fullLight, halfLight := 0, 0
	for y := 29; y < 35; y++ {
		for x := 23; x < 27; x++ {
			at := (y*64 + x) * 4
			fullLight += int(full[at+2])
			halfLight += int(half[at+2])
		}
	}
	if fullLight == 0 || halfLight == 0 || halfLight >= fullLight {
		t.Fatalf("instance alpha did not attenuate authored emission: full=%d half=%d", fullLight, halfLight)
	}
	clear := glowFrame(t, vk, render.Frame{})
	for _, alpha := range []float32{0, .0005} {
		emitter.Color[3] = alpha
		frame.Commands[0].Draws[0] = emitter
		if got := glowFrame(t, vk, frame); !bytes.Equal(clear, got) {
			t.Fatalf("discarded alpha=%v mesh still emitted glow or color", alpha)
		}
	}
	vertices := emitter.Geometry.Vertices()
	for i := range vertices {
		vertices[i].A = 0
	}
	geometry, err := render.NewGeometry(vertices, emitter.Geometry.Indices())
	if err != nil {
		t.Fatal(err)
	}
	emitter.Geometry, emitter.Color[3] = geometry, 1
	frame.Commands[0].Draws[0] = emitter
	if got := glowFrame(t, vk, frame); !bytes.Equal(clear, got) {
		t.Fatal("zero vertex alpha emitted glow")
	}
}

func TestAuthoredGlowCameraIsolationAndOverlayOrderGPU(t *testing.T) {
	vk := openTextureGPU(t)
	leftView, left := glowFixture(t)
	leftView.Viewport = [4]float32{4, 4, 24, 24}
	left.Glow = [3]float32{1, 0, 0}
	rightView, right := leftView, left
	rightView.Viewport, right.Glow = [4]float32{36, 36, 24, 24}, [3]float32{0, 0, 1}
	frame := render.Frame{Commands: []render.Command{glowScene(leftView, left), glowScene(rightView, right)}}
	baseline, glowing := glowFrame(t, vk, glowOff(frame)), glowFrame(t, vk, frame)
	redSpill, blueSpill := 0, 0
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			at := (y*64 + x) * 4
			insideLeft := x >= 4 && x < 28 && y >= 4 && y < 28
			insideRight := x >= 36 && x < 60 && y >= 36 && y < 60
			if !insideLeft && !insideRight && !bytes.Equal(glowing[at:at+4], baseline[at:at+4]) {
				t.Fatalf("halo escaped its camera viewport at (%d,%d)", x, y)
			}
			if insideLeft && glowing[at+2] > baseline[at+2] {
				redSpill++
				if glowing[at] != baseline[at] {
					t.Fatal("second camera's blue emission contaminated first camera")
				}
			}
			if insideRight && glowing[at] > baseline[at] {
				blueSpill++
				if glowing[at+2] != baseline[at+2] {
					t.Fatal("first camera's red emission contaminated second camera")
				}
			}
		}
	}
	if redSpill == 0 || blueSpill == 0 {
		t.Fatal("independent camera halos were not both visible")
	}
	image, err := render.NewTexture(1, 1, []byte{20, 30, 40, 255})
	if err != nil {
		t.Fatal(err)
	}
	// A full-screen image between cameras must cover the first halo; an opaque
	// text-like overlay after the second camera must cover its halo too.
	var glyph []render.Vertex
	for _, point := range [][2]float32{{39, 42}, {39, 54}, {43, 54}, {39, 42}, {43, 54}, {43, 42}} {
		glyph = append(glyph, render.Vertex{X: point[0], Y: point[1], U: .5, V: .5, R: 1, G: 1, B: 1, A: 1})
	}
	frame.Vertices = glyph
	frame.Commands = []render.Command{
		glowScene(leftView, left),
		{Kind: render.ImageCommand, Image: render.Image{Texture: image, Bounds: [4]float32{0, 0, 64, 64}}},
		glowScene(rightView, right),
		{Kind: render.OverlayCommand, Count: len(glyph)},
	}
	baseline, glowing = glowFrame(t, vk, glowOff(frame)), glowFrame(t, vk, frame)
	if changedPixelsInRect(baseline, glowing, 64, 4, 4, 28, 28) != 0 {
		t.Fatal("earlier camera halo was composited over a later image command")
	}
	if changedPixelsInRect(baseline, glowing, 64, 40, 43, 42, 53) != 0 {
		t.Fatal("camera halo changed later opaque overlay text")
	}
	if changedPixelsInRect(baseline, glowing, 64, 52, 44, 57, 52) == 0 {
		t.Fatal("ordered overlay fixture has no visible second-camera halo")
	}
	// Bright unmarked surfaces/overlay glyphs/images do not become implicit
	// emitters, and removing authored emission cannot retain the earlier seed.
	if next := glowFrame(t, vk, glowOff(frame)); !bytes.Equal(baseline, next) {
		t.Fatal("bright nonemitting image/text seeded glow or retained an old halo")
	}
}

func TestAuthoredGlowResizeAndRetainedResourceReleaseGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, emitter := glowFixture(t)
	texture, err := render.NewTexture(1, 1, []byte{250, 240, 230, 255})
	if err != nil {
		t.Fatal(err)
	}
	_, model := textureView()
	model[0], model[5], model[12], model[13], model[14] = .2, .2, .7, .7, .2
	app := render.Draw{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}}
	frame := render.Frame{Commands: []render.Command{glowScene(view, emitter, app)}}
	for iteration := 0; iteration < 2; iteration++ {
		baseline := glowFrame(t, vk, glowOff(frame))
		samples := vk.SampleCount()
		glowing := glowFrame(t, vk, frame)
		if bytes.Equal(baseline, glowing) {
			t.Fatal("resize/release fixture produced no halo")
		}
		for i := 0; i < 12; i++ {
			if next := glowFrame(t, vk, frame); !bytes.Equal(glowing, next) {
				t.Fatalf("halo resources changed after resize iteration%d frame%d", iteration, i)
			}
		}
		if samples != vk.SampleCount() {
			t.Fatal("glow changed MSAA sample count")
		}
		if err := vk.ReleaseGeometry(emitter.Geometry.ID()); err != nil {
			t.Fatal(err)
		}
		if err := vk.ReleaseTexture(texture.ID()); err != nil {
			t.Fatal(err)
		}
		if len(vk.frame.uploaded) != 0 || len(vk.frame.textures) != 0 {
			t.Fatal("explicit release retained scene resource cache entries")
		}
		if next := glowFrame(t, vk, frame); !bytes.Equal(glowing, next) {
			t.Fatal("resource reupload changed authored halo")
		}
		if len(vk.frame.uploaded) != 1 || len(vk.frame.textures) != 1 {
			t.Fatal("glow allocated duplicate authored geometry or content textures")
		}
		if zero := glowFrame(t, vk, glowOff(frame)); !bytes.Equal(baseline, zero) {
			t.Fatal("disabled halo survived target/resource changes")
		}
		if iteration == 0 {
			if err := vk.Resize(80, 48); err != nil {
				t.Fatal(err)
			}
			frame.Commands[0].View.Viewport = [4]float32{0, 0, 80, 48}
		}
	}
}

func TestAuthoredGlowOddExtentFractionalViewsDiscardPreviousCameraSeedsGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, emitter := glowFixture(t)
	// Fill every retained camera slot before changing the target dimensions.
	var previous render.Frame
	for i := 0; i < 4; i++ {
		draw := emitter
		draw.Model[0], draw.Model[5] = .75, .75
		draw.Glow = [3]float32{1, 1, 1}
		previous.Commands = append(previous.Commands, glowScene(view, draw))
	}
	glowFrame(t, vk, previous)
	if err := vk.Resize(81, 49); err != nil {
		t.Fatal(err)
	}
	// Move large, differently colored seeds through the shared scratch targets.
	// The final frame uses fewer slots and smaller, partly offscreen viewports,
	// so stale pixels outside a new scissor must not enter its blur samples.
	for step := 0; step < 4; step++ {
		for i := range previous.Commands {
			command := &previous.Commands[i]
			command.View.Viewport = [4]float32{float32(i*6-step*3) + .25, float32(step-i) + .5, 75.5, 47.25}
			draw := &command.Draws[0]
			draw.Glow = [3]float32{}
			draw.Glow[(i+step)%3] = 1
			draw.Model[12], draw.Model[13] = float32(step-2)*.15, float32(i-2)*.2
		}
		glowFrame(t, vk, previous)
	}
	viewports := [][4]float32{{-8.5, 2.25, 38.75, 25.5}, {32.25, 5.5, 23.5, 35.25}, {64.25, 20.75, 24.5, 33.25}}
	var final render.Frame
	for i, viewport := range viewports {
		camera, draw := view, emitter
		camera.Viewport = viewport
		draw.Model[0], draw.Model[5] = .3, .3
		draw.Glow = [3]float32{}
		draw.Glow[i] = 1
		final.Commands = append(final.Commands, glowScene(camera, draw))
	}
	got := glowFrame(t, vk, final)
	fresh, err := OpenVK(false, 81, 49)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(fresh.Close)
	want := glowFrame(t, fresh, final)
	if !bytes.Equal(got, want) {
		t.Fatal("camera/resize history changed odd-extent fractional-viewport glow compared with a fresh renderer")
	}
	baseline := glowFrame(t, fresh, glowOff(final))
	for i, region := range [][4]int{{0, 2, 31, 28}, {32, 5, 56, 41}, {64, 20, 81, 49}} {
		if changedPixelsInRect(baseline, want, 81, region[0], region[1], region[2], region[3]) == 0 {
			t.Fatalf("fractional-viewport camera %d produced no visible halo", i)
		}
	}
	for y := 0; y < 49; y++ {
		for x := 0; x < 81; x++ {
			if x == 31 || (x >= 56 && x < 64) || y == 0 {
				at := (y*81 + x) * 4
				if !bytes.Equal(got[at:at+4], baseline[at:at+4]) {
					t.Fatalf("fractional camera halo escaped into the viewport gap at (%d,%d)", x, y)
				}
			}
		}
	}
	if next := glowFrame(t, vk, final); !bytes.Equal(want, next) {
		t.Fatal("odd-extent halo changed when scratch targets were reused for the same cameras")
	}
}

func TestAuthoredGlowRejectsInvalidChannelsTexturesAndCameraOverflowGPU(t *testing.T) {
	vk := openTextureGPU(t)
	view, emitter := glowFixture(t)
	for channel := 0; channel < 3; channel++ {
		for _, bad := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1)), -.01, 1.01} {
			draw := emitter
			draw.Glow[channel] = bad
			if err := vk.RenderFrame(render.Frame{Commands: []render.Command{glowScene(view, draw)}}, [4]float32{}, nil); err == nil {
				t.Fatalf("accepted glow channel%d=%v", channel, bad)
			}
		}
	}
	texture, err := render.NewTexture(1, 1, []byte{255, 255, 255, 255})
	if err != nil {
		t.Fatal(err)
	}
	draw := emitter
	draw.Geometry, draw.Texture = nil, texture
	if err := vk.RenderFrame(render.Frame{Commands: []render.Command{glowScene(view, draw)}}, [4]float32{}, nil); err == nil {
		t.Fatal("application texture accepted authored glow")
	}
	var commands []render.Command
	for i := 0; i < 5; i++ {
		commands = append(commands, glowScene(view, emitter))
	}
	if err := vk.RenderFrame(render.Frame{Commands: commands}, [4]float32{}, nil); err == nil {
		t.Fatal("fifth glowing camera exceeded bounded effect slots silently")
	}
	// The capacity boundary itself is valid, and a rejected frame must not
	// poison the scene or leak an old pool slot into a later camera.
	legal := render.Frame{Commands: commands[:4]}
	first := glowFrame(t, vk, legal)
	if second := glowFrame(t, vk, legal); !bytes.Equal(first, second) {
		t.Fatal("bounded camera pool did not recover after rejected frame")
	}
	if got := glowFrame(t, vk, glowOff(legal)); bytes.Equal(first, got) {
		t.Fatal("capacity test never exercised glow")
	}
}

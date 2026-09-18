//go:build linux

package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/photoapp"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/projectapp"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/workspace"
)

func workspacePhotoFixture(t *testing.T) (string, string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "Specimen λ.PNG")
	im := image.NewRGBA(image.Rect(0, 0, 480, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 480; x++ {
			c := color.RGBA{210, 30, 50, 255}
			if x >= 240 {
				c = color.RGBA{25, 45, 215, 255}
			}
			if y >= 150 {
				c.G = 150
			}
			im.SetRGBA(x, y, c)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, im); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return directory, path
}

func decodedPhotoPixels(texture *render.Texture) bool {
	u, ok := texture.Snapshot(0)
	if !ok {
		return false
	}
	red, blue := 0, 0
	for i := 0; i < len(u.Pixels); i += 4 {
		if u.Pixels[i] > 180 && u.Pixels[i+1] < 60 && u.Pixels[i+2] < 80 {
			red++
		}
		if u.Pixels[i] < 60 && u.Pixels[i+1] < 70 && u.Pixels[i+2] > 180 {
			blue++
		}
	}
	return red > 2000 && blue > 2000
}

func TestFilesPhotoOpenPreservesFocusPlacementAndRetiresReplacement(t *testing.T) {
	directory, path := workspacePhotoFixture(t)
	browser, err := projectapp.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	photos := photoapp.NewManager()
	defer photos.Close()
	texture, err := render.NewTexture(2, 2, bytes.Repeat([]byte{15, 22, 31, 255}, 4))
	if err != nil {
		t.Fatal(err)
	}
	sibling := &hubProvider{surfaces: []experience.ApplicationSurface{{ID: 1, Key: "native:terminal", Texture: texture}}}
	work, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	hub := newApplicationHub(browser, photos, sibling)
	work.SetApplications(hub)
	defer work.SetApplications(nil)
	connectFileBrowser(browser, nil, photos, hub, work)
	const width, height = 1440, 900
	work.Draw(width, height)
	surface := func(key string) experience.ApplicationSurface {
		for _, s := range hub.Surfaces() {
			if s.Key == key {
				return s
			}
		}
		return experience.ApplicationSurface{}
	}
	files, terminal := surface("native:project-browser"), surface("native:terminal")
	stroke := func(code uint32, modifiers experience.Modifiers) {
		t.Helper()
		for _, pressed := range []bool{true, false} {
			e := experience.Event{Kind: experience.KeyInput, Keycode: code, Modifiers: modifiers, Pressed: pressed}
			if code == 19 {
				e.Key = experience.KeyR
			}
			hub.seat(e)
			if quit, err := dispatchEvent(work, e, "", io.Discard); err != nil || quit {
				t.Fatalf("photo input caused exit: %v %v", quit, err)
			}
		}
	}
	poll := func() {
		t.Helper()
		if err := browser.Poll(); err != nil {
			t.Fatal(err)
		}
		if err := photos.Poll(); err != nil {
			t.Fatal(err)
		}
		work.Update(0)
	}
	wait := func(description string, predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			poll()
			if predicate() {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("timed out waiting for " + description)
	}
	focus := func(key string) {
		t.Helper()
		if err := work.ActivateApplication(key); err != nil {
			t.Fatal(err)
		}
		work.Draw(width, height)
	}
	focus(files.Key)
	wait("selected photo", func() bool {
		stroke(46, experience.ModControl|experience.ModShift)
		copied, ok := browser.TakeCopy()
		return ok && copied == path
	})
	if len(photos.Surfaces()) != 0 {
		t.Fatal("selection opened a photo without an explicit open")
	}
	stroke(28, 0)
	// The user leaves Files before the asynchronous open/decode completes.
	// The eventual photo must not steal that newer focus decision.
	focus(terminal.Key)
	before := work.Document().View
	wait("decoded photo", func() bool {
		p := surface("native:photo-viewer")
		return p.ID != 0 && decodedPhotoPixels(p.Texture)
	})
	first := surface("native:photo-viewer")
	if first.Frameless || !first.DragContent {
		t.Fatal("photo did not allow its workspace bracket with dragging on its content")
	}
	after := work.Document().View
	if first.ID == files.ID || first.ID == terminal.ID || hub.focused != terminal.ID || sibling.focused != 1 || !work.OwnsKeyboard() || after.Camera != before.Camera || after.Application.Active != before.Application.Active || after.Application.Selected != before.Application.Selected || after.Application.Reading != before.Application.Reading {
		t.Fatal("asynchronous photo open changed the user's focus, selection or camera")
	}
	focus(first.Key)
	oldWidth, oldHeight := first.Texture.Size()
	if err := work.Dispatch(workspace.Action{Kind: workspace.ToggleApplicationSize}); err != nil {
		t.Fatal(err)
	}
	poll()
	newWidth, newHeight := first.Texture.Size()
	if newWidth <= oldWidth || newHeight <= oldHeight || !decodedPhotoPixels(first.Texture) {
		t.Fatal("photo resize lost image pixels or did not enlarge the retained surface")
	}
	if err := work.Dispatch(workspace.Action{Kind: workspace.MoveApplications, DeltaX: 1.2, DeltaY: -.7}); err != nil {
		t.Fatal(err)
	}
	focus(files.Key)
	before = work.Document().View
	stroke(28, 0)
	wait("photo replacement", func() bool {
		p := surface(first.Key)
		return p.ID != 0 && p.ID != first.ID && decodedPhotoPixels(p.Texture)
	})
	replacement := surface(first.Key)
	if replacement.Texture == first.Texture || hub.focused != files.ID || work.Document().View != before {
		t.Fatal("photo replacement moved its window or changed Files focus/view")
	}
	retired := photos.RetiredTextures()
	if len(retired) != 1 || retired[0] != first.Texture.ID() {
		t.Fatalf("photo replacement did not retire exactly its old texture: %v", retired)
	}
	hub.CloseApplication(first.ID)
	if len(photos.Surfaces()) != 1 {
		t.Fatal("stale photo route closed the replacement")
	}
	// The photo is view-only, even after an explicit user focus action.
	focus(replacement.Key)
	poll()
	original, _ := replacement.Texture.Snapshot(0)
	stroke(19, 0)
	poll()
	stroke(19, experience.ModShift)
	stroke(13, 0) // Former zoom-in shortcut.
	stroke(12, 0) // Former zoom-out shortcut.
	poll()
	unchanged, _ := replacement.Texture.Snapshot(0)
	if !bytes.Equal(original.Pixels, unchanged.Pixels) {
		t.Fatal("removed photo controls changed the view through keyboard input")
	}

	// Drag the actual image through host routing, without a grip or modifier.
	// Untimed events move it without starting a throw, so Undo is deterministic.
	focus(replacement.Key) // Global workspace shortcuts may have left Read.
	beforeDrag := work.Document()
	x, y := applicationTextureCenter(t, work.Draw(width, height), replacement.Texture)
	for _, e := range []experience.Event{
		{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y},
		{Kind: experience.PointerMove, X: x + 40, Y: y - 16},
		{Kind: experience.PointerMove, X: x + 70, Y: y - 28},
		{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x + 70, Y: y - 28},
	} {
		hub.seat(e)
		if quit, err := dispatchEvent(work, e, "", io.Discard); err != nil || quit {
			t.Fatalf("photo drag caused host exit: %t %v", quit, err)
		}
	}
	poll()
	if work.Document().View.Application.Layouts == beforeDrag.View.Application.Layouts || work.Document().View.Application.Reading {
		t.Fatal("dragging a photo in Read did not place it into the workspace")
	}
	afterDrag, _ := replacement.Texture.Snapshot(0)
	if !bytes.Equal(original.Pixels, afterDrag.Pixels) {
		t.Fatal("dragging the photo moved or transformed pixels inside the image")
	}
	if err := work.Dispatch(workspace.Action{Kind: workspace.Undo}); err != nil {
		t.Fatal(err)
	}
	if work.Document().View != beforeDrag.View {
		t.Fatal("photo drag and leaving Read were not undone together")
	}
	if os.Getenv("WORLDR_TEST_GPU") == "1" {
		capturePhotoWorkspace(t, work, replacement.Texture, width, height, "worldr-photo-fixture.png")
	}
	focus(files.Key)
	hub.CloseApplication(replacement.ID)
	poll()
	if len(photos.Surfaces()) != 0 || hub.focused != files.ID || !work.OwnsKeyboard() {
		t.Fatal("closing the unfocused photo viewer affected Files")
	}
	if retired := photos.RetiredTextures(); len(retired) != 1 || retired[0] != replacement.Texture.ID() {
		t.Fatalf("photo close did not retire its final texture once: %v", retired)
	}
	for _, command := range work.Draw(width, height).Commands {
		for _, draw := range command.Draws {
			if draw.Texture == replacement.Texture {
				t.Fatal("closed photo texture remained in frame")
			}
		}
	}
}

func capturePhotoWorkspace(t *testing.T, work *workspace.Workspace, texture *render.Texture, width, height int, name string) {
	t.Helper()
	vk, err := native.OpenVK(false, uint32(width), uint32(height))
	if err != nil {
		t.Fatal(err)
	}
	defer vk.Close()
	if err := vk.SetSceneAtlas(work.Atlas()); err != nil {
		t.Fatal(err)
	}
	frame := work.Draw(width, height)
	found := false
	for _, command := range frame.Commands {
		for _, draw := range command.Draws {
			found = found || draw.Texture == texture
		}
	}
	if !found {
		t.Fatal("photo viewer absent from native GPU frame")
	}
	pixels := make([]byte, width*height*4)
	if err := vk.RenderFrame(frame, [4]float32{.025, .034, .043, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("WORLDR_CAPTURE_DIR"); directory != "" {
		path := filepath.Join(directory, name)
		if err := savePNG(path, pixels, width, height); err != nil {
			t.Fatal(err)
		}
		t.Log("photo workspace capture:", path)
	}
}

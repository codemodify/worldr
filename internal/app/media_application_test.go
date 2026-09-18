//go:build linux && cgo

package app

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/media"
	"github.com/codemodify/worldr/internal/mediaapp"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/projectapp"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/workspace"
)

func generatedWorkspaceVideo(t *testing.T) (directory, path string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal("media integration requires ffmpeg:", err)
	}
	directory = t.TempDir()
	path = filepath.Join(directory, "Mandelbrot - generated demo.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "mandelbrot=size=384x216:rate=15:maxiter=256:start_scale=3:end_scale=0.3:end_pts=359",
		"-f", "lavfi", "-i", "sine=frequency=220:sample_rate=24000:duration=24",
		"-t", "24", "-c:v", "libx264", "-preset", "ultrafast", "-crf", "22", "-pix_fmt", "yuv420p", "-g", "15", "-c:a", "aac", "-threads", "1", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate Mandelbrot video fixture: %v: %s", err, output)
	}
	if captureDirectory := os.Getenv("WORLDR_CAPTURE_DIR"); captureDirectory != "" {
		demoDirectory := filepath.Join(captureDirectory, "media-demo")
		if err := os.MkdirAll(demoDirectory, 0o755); err != nil {
			t.Fatal(err)
		}
		demoPath := filepath.Join(demoDirectory, "Orbital patterns.mp4")
		// Exercise the audio decoder in the silent test player, but keep the
		// exported visual demo silent when opened with normal audio output.
		command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
			"-i", path, "-map", "0:v:0", "-c:v", "copy", "-an", demoPath)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("export silent workspace video demo: %v: %s", err, output)
		}
		t.Log("silent media demo:", demoPath)
	}
	return directory, path
}

func mediaTextureRegion(update render.TextureUpdate, rect image.Rectangle) []byte {
	rect = rect.Intersect(image.Rect(0, 0, update.Width, update.Height))
	pixels := make([]byte, rect.Dx()*rect.Dy()*4)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		start := (y*update.Width + rect.Min.X) * 4
		copy(pixels[(y-rect.Min.Y)*rect.Dx()*4:], update.Pixels[start:start+rect.Dx()*4])
	}
	return pixels
}

func decodedMediaPixels(texture *render.Texture) bool {
	update, ok := texture.Snapshot(0)
	if !ok {
		return false
	}
	// The source has hundreds of distinct colors. The central loading label
	// and uniform dark player background cannot satisfy this decoder check.
	colors := make(map[[3]byte]bool)
	for y := update.Height / 5; y < update.Height*3/5; y += 6 {
		for x := update.Width / 6; x < update.Width*5/6; x += 6 {
			i := (y*update.Width + x) * 4
			colors[[3]byte{update.Pixels[i], update.Pixels[i+1], update.Pixels[i+2]}] = true
		}
	}
	return len(colors) > 400
}

func TestFilesOpensNativeMediaWithHostInputReplacementAndClose(t *testing.T) {
	if os.Getenv("WORLDR_TEST_MEDIA") != "1" {
		t.Skip("set WORLDR_TEST_MEDIA=1 for real Files-to-player playback")
	}
	directory, path := generatedWorkspaceVideo(t)
	browser, err := projectapp.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	player := mediaapp.NewManager(media.Options{AudioOutput: "null"})
	defer player.Close()
	work, err := workspace.NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Close()
	hub := newApplicationHub(browser, player)
	work.SetApplications(hub)
	defer work.SetApplications(nil)
	connectFileBrowser(browser, player, nil, hub, work)
	const width, height = 1440, 900
	work.Draw(width, height)
	browserSurface := hub.Surfaces()[0]
	act := func(action workspace.Action) {
		t.Helper()
		if err := work.Dispatch(action); err != nil {
			t.Fatal(err)
		}
	}
	stroke := func(code uint32, key experience.Key, modifiers experience.Modifiers) {
		t.Helper()
		for _, pressed := range []bool{true, false} {
			event := experience.Event{Kind: experience.KeyInput, Keycode: code, Key: key, Modifiers: modifiers, Pressed: pressed}
			hub.seat(event)
			if quit, err := dispatchEvent(work, event, "", io.Discard); err != nil || quit {
				t.Fatalf("media input caused host exit: quit=%t err=%v", quit, err)
			}
		}
	}
	poll := func() {
		t.Helper()
		if err := browser.Poll(); err != nil {
			t.Fatal(err)
		}
		if err := player.Poll(); err != nil {
			t.Fatal(err)
		}
		work.Update(0)
	}
	wait := func(description string, predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			poll()
			if predicate() {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", description)
	}
	surface := func(key string) experience.ApplicationSurface {
		for _, surface := range hub.Surfaces() {
			if surface.Key == key {
				return surface
			}
		}
		return experience.ApplicationSurface{}
	}
	focusFiles := func() {
		t.Helper()
		act(workspace.Action{Kind: workspace.SelectApplication, ApplicationKey: browserSurface.Key})
		work.Draw(width, height)
		stroke(28, experience.KeyUnknown, 0)
		if !work.OwnsKeyboard() || hub.focused != browserSurface.ID {
			t.Fatal("Files did not acquire exclusive keyboard focus")
		}
	}
	focusFiles()
	wait("Files selecting its video", func() bool {
		stroke(46, experience.KeyUnknown, experience.ModControl|experience.ModShift)
		copied, ok := browser.TakeCopy()
		return ok && copied == path
	})
	if len(player.Surfaces()) != 0 {
		t.Fatal("selecting the video opened it without explicit activation")
	}
	beforeOpen := work.Document().View
	stroke(28, experience.KeyUnknown, 0) // This Enter belongs to the focused Files app.
	wait("decoded video in the new native player", func() bool {
		return len(player.Surfaces()) == 1 && decodedMediaPixels(player.Surfaces()[0].Texture)
	})
	first := surface("native:media-player")
	afterOpen := work.Document().View
	if first.ID == 0 || first.ID == browserSurface.ID || hub.focused != browserSurface.ID || !work.OwnsKeyboard() || afterOpen.Camera != beforeOpen.Camera || afterOpen.Application.Active != beforeOpen.Application.Active || afterOpen.Application.Selected != beforeOpen.Application.Selected || afterOpen.Application.Reading != beforeOpen.Application.Reading {
		t.Fatal("opening a video stole focus, selection or the camera from Files")
	}
	// Playback must advance while Files retains exclusive keyboard ownership.
	initialPlaying, _ := first.Texture.Snapshot(0)
	unfocusedVideo := mediaTextureRegion(initialPlaying, image.Rect(initialPlaying.Width/6, initialPlaying.Height/5, initialPlaying.Width*5/6, initialPlaying.Height*3/5))
	wait("video frames advancing without taking focus", func() bool {
		update, _ := first.Texture.Snapshot(0)
		return !bytes.Equal(unfocusedVideo, mediaTextureRegion(update, image.Rect(update.Width/6, update.Height/5, update.Width*5/6, update.Height*3/5)))
	})
	if hub.focused != browserSurface.ID {
		t.Fatal("asynchronous playback took keyboard focus")
	}
	// The user can still explicitly enter the player to operate its controls.
	if err := work.ActivateApplication(first.Key); err != nil {
		t.Fatal(err)
	}
	playing, _ := first.Texture.Snapshot(0)
	controls := image.Rect(0, playing.Height-80, playing.Width, playing.Height-25)
	playingControls := mediaTextureRegion(playing, controls)
	stroke(57, experience.KeySpace, 0)
	wait("paused player controls", func() bool {
		update, _ := first.Texture.Snapshot(0)
		return !bytes.Equal(playingControls, mediaTextureRegion(update, controls))
	})
	// Pause must stop decoded output and the displayed timeline, not just alter
	// a local button. Allow outstanding decoder/property events to settle.
	stableSince := time.Now()
	stableRevision := first.Texture.Revision()
	wait("paused frame and controls remaining stable", func() bool {
		if revision := first.Texture.Revision(); revision != stableRevision {
			stableRevision, stableSince = revision, time.Now()
		}
		return time.Since(stableSince) >= 250*time.Millisecond
	})
	paused, _ := first.Texture.Snapshot(0)
	videoRect := image.Rect(paused.Width/6, paused.Height/5, paused.Width*5/6, paused.Height*3/5)
	pausedVideo := mediaTextureRegion(paused, videoRect)
	pausedControls := mediaTextureRegion(paused, controls)
	stroke(106, experience.KeyRight, 0)
	wait("seeked paused video and updated timeline", func() bool {
		update, _ := first.Texture.Snapshot(0)
		return !bytes.Equal(pausedVideo, mediaTextureRegion(update, videoRect)) && !bytes.Equal(pausedControls, mediaTextureRegion(update, controls))
	})
	initialWidth, initialHeight := first.Texture.Size()
	act(workspace.Action{Kind: workspace.ToggleApplicationSize})
	wait("resized video image", func() bool { return decodedMediaPixels(first.Texture) })
	newWidth, newHeight := first.Texture.Size()
	if player.Surfaces()[0].Texture != first.Texture || newWidth <= initialWidth || newHeight <= initialHeight {
		t.Fatal("player resize replaced its texture or failed to enlarge it")
	}
	work.Draw(width, height)
	if os.Getenv("WORLDR_TEST_GPU") == "1" {
		captureMediaWorkspace(t, work, first.Texture, width, height)
	}

	// Opening the same file again replaces the playback instance while keeping
	// its document placement key. The withdrawn runtime route must not linger.
	act(workspace.Action{Kind: workspace.MoveApplications, DeltaX: 1.25, DeltaY: -.4})
	focusFiles()
	act(workspace.Action{Kind: workspace.ToggleApplicationReading})
	beforeReplacement := work.Document().View
	stroke(28, experience.KeyUnknown, 0)
	wait("replacement native player", func() bool {
		current := surface(first.Key)
		return current.ID != 0 && current.ID != first.ID && current.Texture != first.Texture && decodedMediaPixels(current.Texture)
	})
	replacement := surface(first.Key)
	afterReplacement := work.Document().View
	if len(hub.Surfaces()) != 2 || replacement.Key != first.Key || hub.focused != browserSurface.ID || !work.OwnsKeyboard() || afterReplacement != beforeReplacement {
		t.Fatal("replacement changed placement, selection, reading mode, camera or Files focus")
	}
	if retired := player.RetiredTextures(); len(retired) != 1 || retired[0] != first.Texture.ID() {
		t.Fatalf("replacement did not retire the old texture once: %v", retired)
	}
	for _, command := range work.Draw(width, height).Commands {
		for _, draw := range command.Draws {
			if draw.Texture == first.Texture {
				t.Fatal("replaced media texture remained in the submitted workspace")
			}
		}
	}
	hub.CloseApplication(first.ID)
	if len(player.Surfaces()) != 1 {
		t.Fatal("stale runtime route closed the replacement player")
	}
	hub.CloseApplication(replacement.ID)
	poll()
	work.Draw(width, height)
	if len(player.Surfaces()) != 0 || len(hub.Surfaces()) != 1 || hub.Surfaces()[0].ID != browserSurface.ID || !work.OwnsKeyboard() || hub.focused != browserSurface.ID {
		t.Fatal("closing an unfocused player removed Files or changed its focus")
	}
	if retired := player.RetiredTextures(); len(retired) != 1 || retired[0] != replacement.Texture.ID() {
		t.Fatalf("closing player did not retire its last texture: %v", retired)
	}
	stroke(46, experience.KeyUnknown, experience.ModControl|experience.ModShift)
	if copied, ok := browser.TakeCopy(); !ok || copied != path {
		t.Fatal("Files stopped working after closing its media player")
	}
}

func captureMediaWorkspace(t *testing.T, work *workspace.Workspace, texture *render.Texture, width, height int) {
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
		t.Fatal("Read-mode frame omitted the decoded media surface")
	}
	pixels := make([]byte, width*height*4)
	if err := vk.RenderFrame(frame, [4]float32{.025, .034, .043, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("WORLDR_CAPTURE_DIR"); directory != "" {
		path := filepath.Join(directory, "worldr-media-player.png")
		if err := savePNG(path, pixels, width, height); err != nil {
			t.Fatal(err)
		}
		t.Log("media workspace capture:", path)
		if update, ok := texture.Snapshot(0); ok {
			path = filepath.Join(directory, "worldr-media-player-panel.png")
			// Retained textures are RGBA; Vulkan framebuffer captures above are BGRA.
			panel := &image.RGBA{Pix: update.Pixels, Stride: update.Width * 4, Rect: image.Rect(0, 0, update.Width, update.Height)}
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, panel); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, encoded.Bytes(), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Log("media panel capture:", path)
		}
	}
}

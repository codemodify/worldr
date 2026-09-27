package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func testStudio(t *testing.T, id string) (*studio, *nativeapp.Validator, nativeapp.Snapshot) {
	t.Helper()
	selected, err := skin.Builtin(id)
	if err != nil {
		t.Fatal(err)
	}
	s := &studio{selected: selected}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := s.Manifest().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(nativeapp.Host{Version: nativeapp.Version, MaxSurfaceWidth: 1920, MaxSurfaceHeight: 1080, MaxSurfaces: 1}); err != nil {
		t.Fatal(err)
	}
	validator := &nativeapp.Validator{}
	return s, validator, studioSnapshot(t, s, validator)
}

func studioSnapshot(t *testing.T, s *studio, validator *nativeapp.Validator) nativeapp.Snapshot {
	t.Helper()
	snapshot := s.Snapshot()
	if err := validator.Validate(snapshot); err != nil {
		t.Fatalf("invalid studio snapshot: %v", err)
	}
	return snapshot
}

func studioNode(t *testing.T, s *studio, id string) nativeapp.SemanticNode {
	t.Helper()
	for _, node := range s.demo.Semantics().Nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("studio is missing control %q", id)
	return nativeapp.SemanticNode{}
}

func clickStudio(t *testing.T, s *studio, validator *nativeapp.Validator, id string) {
	t.Helper()
	node := studioNode(t, s, id)
	x, y := float32(node.Bounds.X+node.Bounds.Width/2), float32(node.Bounds.Y+node.Bounds.Height/2)
	for _, kind := range []nativeapp.EventKind{nativeapp.PointerDown, nativeapp.PointerUp} {
		if err := s.Handle(1, nativeapp.Event{Kind: kind, Button: nativeapp.ButtonPrimary, X: x, Y: y}); err != nil {
			t.Fatal(err)
		}
		studioSnapshot(t, s, validator)
	}
}

func focusStudioQuery(t *testing.T, s *studio, validator *nativeapp.Validator, text string) {
	t.Helper()
	if err := s.Focus(1); err != nil {
		t.Fatal(err)
	}
	studioSnapshot(t, s, validator)
	clickStudio(t, s, validator, "query")
	if err := s.Handle(1, nativeapp.Event{Kind: nativeapp.TextCommit, TextContext: "filter", Text: text}); err != nil {
		t.Fatal(err)
	}
	if s.demo.Values.Query != text {
		t.Fatal("focused field did not receive committed text")
	}
	studioSnapshot(t, s, validator)
}

func TestStudioSkinSwitchPreservesFocusedTextStateAndTextureIdentity(t *testing.T) {
	s, validator, first := testStudio(t, "merrick")
	if !s.Manifest().Skins || !s.Manifest().ControlThemes || len(first.Textures) != 1 || first.Textures[0].Revision != 1 || len(first.Surfaces) != 1 {
		t.Fatal("initial lifecycle/manifest did not publish one supported surface")
	}
	textureID, surfaceID, key := first.Textures[0].ID, first.Surfaces[0].ID, first.Surfaces[0].Key
	// Published pixel storage is owned by the snapshot consumer.
	pixel := s.pixels.Pix[0]
	first.Textures[0].Pixels[0] ^= 255
	if s.pixels.Pix[0] != pixel {
		t.Fatal("snapshot borrowed mutable framebuffer bytes")
	}
	if quiet := studioSnapshot(t, s, validator); len(quiet.Textures) != 0 {
		t.Fatal("unchanged Snapshot repeated a texture update")
	}
	clickStudio(t, s, validator, "locked")
	focusStudioQuery(t, s, validator, "field")
	before := s.demo.Values
	snapshot := studioSnapshot(t, s, validator)
	previous := s.revision
	seen := map[[32]byte]bool{}
	for _, id := range []string{"advanced", "hologram", "plasma", "merrick"} {
		selected, err := skin.Builtin(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetSkin(selected); err != nil {
			t.Fatal(err)
		}
		selected.Palette["accent"] = "#010203"
		if s.selected.Palette["accent"] == "#010203" {
			t.Fatal("SetSkin borrowed mutable caller data")
		}
		snapshot = studioSnapshot(t, s, validator)
		if len(snapshot.Textures) != 1 || snapshot.Textures[0].ID != textureID || snapshot.Textures[0].Revision <= previous {
			t.Fatal("skin update lost texture identity/revision")
		}
		previous = snapshot.Textures[0].Revision
		surface := snapshot.Surfaces[0]
		if surface.ID != surfaceID || surface.Key != key || surface.Texture != textureID || surface.Semantics.FocusedID != "query" || !surface.TextInput.Enabled || surface.TextInput.ContextID != "filter" || surface.TextInput.Surrounding != "field" || surface.TextInput.Cursor != len("field") {
			t.Fatal("skin switch lost surface identity, focused text, or IME context")
		}
		if s.demo.Values != before {
			t.Fatal("skin switch changed control state")
		}
		hash := sha256.Sum256(snapshot.Textures[0].Pixels)
		if seen[hash] {
			t.Fatal("distinct skin did not change published appearance", id)
		}
		seen[hash] = true
		if quiet := studioSnapshot(t, s, validator); len(quiet.Textures) != 0 {
			t.Fatal("skin texture delta was not drained")
		}
	}
	invalid := s.selected.Clone()
	invalid.Version = 999
	if err := s.SetSkin(invalid); err == nil {
		t.Fatal("invalid skin accepted")
	}
	if s.revision != previous || s.demo.Values != before {
		t.Fatal("invalid skin mutated studio state")
	}
	if err := s.SetControlTheme(nativeapp.ControlTheme{Family: "glass", Shape: "slab"}); err != nil {
		t.Fatal(err)
	}
	snapshot = studioSnapshot(t, s, validator)
	if snapshot.Surfaces[0].Semantics.FocusedID != "query" || s.demo.Values != before {
		t.Fatal("legacy theme lost focused application state")
	}
	if err := s.Handle(1, nativeapp.Event{Kind: nativeapp.TextCommit, TextContext: "stale", Text: "wrong"}); err != nil {
		t.Fatal(err)
	}
	if s.demo.Values != before || len(studioSnapshot(t, s, validator).Textures) != 0 {
		t.Fatal("stale text context edited the field")
	}
}

func TestStudioRepeatedResizeKeepsValidSnapshotsAndFocusedControls(t *testing.T) {
	sizes := [][2]int{{640, 320}, {790, 340}, {960, 400}, {960, 500}, {960, 600}, {640, 320}, {960, 600}}
	for _, id := range []string{"merrick", "advanced", "hologram", "plasma"} {
		t.Run(id, func(t *testing.T) {
			s, validator, initial := testStudio(t, id)
			focusStudioQuery(t, s, validator, "a")
			studioSnapshot(t, s, validator)
			previous := s.revision
			for _, size := range sizes {
				t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
					if err := s.Resize(1, size[0], size[1]); err != nil {
						t.Fatal(err)
					}
					snapshot := studioSnapshot(t, s, validator)
					if len(snapshot.Textures) != 1 {
						t.Fatal("resize omitted its texture update")
					}
					update := snapshot.Textures[0]
					if update.ID != initial.Textures[0].ID || update.Width != size[0] || update.Height != size[1] || update.Revision <= previous || update.Rect != (nativeapp.Rect{Width: size[0], Height: size[1]}) {
						t.Fatalf("incorrect resize update: id=%d size=%dx%d revision=%d rect=%v", update.ID, update.Width, update.Height, update.Revision, update.Rect)
					}
					previous = update.Revision
					if snapshot.Surfaces[0].Semantics.FocusedID != "query" || !snapshot.Surfaces[0].TextInput.Enabled || s.demo.Values.Query != "a" {
						t.Fatal("resize lost active editing state")
					}
					if len(studioSnapshot(t, s, validator).Textures) != 0 {
						t.Fatal("resize delta repeated on idle snapshot")
					}
				})
			}
			if err := s.Resize(2, 800, 500); err != nil {
				t.Fatal(err)
			}
			if s.revision != previous {
				t.Fatal("unknown surface resize changed studio")
			}
		})
	}
}

func TestStudioCloseRetiresTextureExactlyOnce(t *testing.T) {
	s, validator, first := testStudio(t, "merrick")
	if err := s.CloseSurface(2); err != nil {
		t.Fatal(err)
	}
	if len(studioSnapshot(t, s, validator).Surfaces) != 1 {
		t.Fatal("unknown close withdrew the studio")
	}
	if err := s.CloseSurface(1); err != nil {
		t.Fatal(err)
	}
	closed := studioSnapshot(t, s, validator)
	if len(closed.Surfaces) != 0 || len(closed.Textures) != 0 || len(closed.RetireTextures) != 1 || closed.RetireTextures[0] != first.Textures[0].ID {
		t.Fatal("closed surface did not retire its unreferenced texture")
	}
	revision := s.revision
	for _, action := range []func() error{
		func() error { return s.CloseSurface(1) },
		func() error { return s.Resize(1, 960, 600) },
		func() error { return s.Handle(1, nativeapp.Event{Kind: nativeapp.PointerMove, X: 10, Y: 10}) },
		func() error { return s.Update(time.Second) },
	} {
		if err := action(); err != nil {
			t.Fatal(err)
		}
	}
	if quiet := studioSnapshot(t, s, validator); len(quiet.Surfaces)+len(quiet.Textures)+len(quiet.RetireTextures) != 0 || s.revision != revision {
		t.Fatal("closed studio republished content or repeated retirement")
	}
}

func TestStudioProtocolNegotiatesSkinResizeAndClose(t *testing.T) {
	selected, err := skin.Builtin("merrick")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := skin.Builtin("plasma")
	if err != nil {
		t.Fatal(err)
	}
	app := &studio{selected: selected}
	requests := []nativeapp.Request{
		{Kind: nativeapp.RequestHello, Host: nativeapp.Host{Version: nativeapp.Version, MaxSurfaceWidth: 960, MaxSurfaceHeight: 600, MaxSurfaces: 1}},
		{Kind: nativeapp.RequestSkin, Skin: &replacement},
		{Kind: nativeapp.RequestResize, Surface: 1, Width: 640, Height: 320},
		{Kind: nativeapp.RequestCloseSurface, Surface: 1},
		{Kind: nativeapp.RequestCloseSurface, Surface: 1},
		{Kind: nativeapp.RequestShutdown},
	}
	var input, output bytes.Buffer
	writer := nativeapp.NewCodec(nil, &input)
	for i := range requests {
		requests[i].Version = nativeapp.Version
		requests[i].Sequence = uint64(i + 1)
		if err := writer.Write(requests[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := nativeapp.Serve(context.Background(), app, &input, &output); err != nil {
		t.Fatal(err)
	}
	reader := nativeapp.NewCodec(&output, nil)
	var validator nativeapp.Validator
	retirements := 0
	for i := range requests {
		var response nativeapp.Response
		if err := reader.Read(&response); err != nil {
			t.Fatal(err)
		}
		if response.Error != "" || response.Sequence != requests[i].Sequence || response.Version != nativeapp.Version {
			t.Fatalf("request %s: %+v", requests[i].Kind, response)
		}
		if i == 0 && (response.Manifest == nil || !response.Manifest.Skins) {
			t.Fatal("handshake omitted skin capability")
		}
		if response.Snapshot != nil {
			if err := validator.Validate(*response.Snapshot); err != nil {
				t.Fatal(err)
			}
			retirements += len(response.Snapshot.RetireTextures)
		}
	}
	if retirements != 1 || app.selected.ID != "plasma" || !app.closed {
		t.Fatal("protocol lifecycle did not apply skin and retire once")
	}
}

func TestStudioBackspaceUsesHostEvdevCode(t *testing.T) {
	s, validator, _ := testStudio(t, "merrick")
	focusStudioQuery(t, s, validator, "field")
	studioSnapshot(t, s, validator)
	if err := s.Handle(1, nativeapp.Event{Kind: nativeapp.KeyInput, Keycode: 14, Pressed: true}); err != nil {
		t.Fatal(err)
	}
	if s.demo.Values.Query != "fiel" {
		t.Fatal("host Backspace did not edit the focused field")
	}
	studioSnapshot(t, s, validator)
}

func TestStudioCompletionPublishesTheFinalAnimationFrame(t *testing.T) {
	s, validator, _ := testStudio(t, "merrick")
	clickStudio(t, s, validator, "run")
	if err := s.Update(4825 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	studioSnapshot(t, s, validator)
	if !s.demo.Values.Running || s.demo.Values.Progress < 99.8 {
		t.Fatal("fixture did not approach completion")
	}
	if err := s.Update(16 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	finished := studioSnapshot(t, s, validator)
	if s.demo.Values.Running || s.demo.Values.Progress != 100 || len(finished.Textures) != 1 || studioNode(t, s, "run").Label != "Run analysis" {
		t.Fatal("completion was not published after a short final update")
	}
	if err := s.Update(time.Second); err != nil {
		t.Fatal(err)
	}
	if len(studioSnapshot(t, s, validator).Textures) != 0 {
		t.Fatal("finished animation kept publishing frames")
	}
}

func TestStudioFocusLossRepaintsAndDisablesTextInput(t *testing.T) {
	s, validator, _ := testStudio(t, "merrick")
	focusStudioQuery(t, s, validator, "a")
	before := append([]byte(nil), s.pixels.Pix...)
	if err := s.Focus(0); err != nil {
		t.Fatal(err)
	}
	after := studioSnapshot(t, s, validator)
	if after.Surfaces[0].TextInput.Enabled || after.Surfaces[0].Semantics.FocusedID != "" {
		t.Fatal("focus loss retained text ownership")
	}
	if len(after.Textures) != 1 || bytes.Equal(before, after.Textures[0].Pixels) {
		t.Fatal("focus loss left stale focused pixels")
	}
}

func TestStudioStationaryPointerDoesNotRepublishPixels(t *testing.T) {
	s, validator, _ := testStudio(t, "merrick")
	node := studioNode(t, s, "reset")
	event := nativeapp.Event{Kind: nativeapp.PointerMove, X: float32(node.Bounds.X + 4), Y: float32(node.Bounds.Y + 4)}
	if err := s.Handle(1, event); err != nil {
		t.Fatal(err)
	}
	studioSnapshot(t, s, validator)
	revision := s.revision
	for i := 0; i < 4; i++ {
		if err := s.Handle(1, event); err != nil {
			t.Fatal(err)
		}
		if quiet := studioSnapshot(t, s, validator); len(quiet.Textures) != 0 || s.revision != revision {
			t.Fatal("unchanged pointer hover republished full framebuffer pixels")
		}
	}
}

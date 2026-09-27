package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
	"time"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func testStudio(t *testing.T) (*studio, *nativeapp.Validator, nativeapp.Snapshot) {
	t.Helper()
	s := &studio{}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := s.Manifest().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(nativeapp.Host{Version: nativeapp.Version, MaxSurfaces: 1, MaxSurfaceWidth: 1920, MaxSurfaceHeight: 1080}); err != nil {
		t.Fatal(err)
	}
	v := &nativeapp.Validator{}
	return s, v, snapshot(t, s, v)
}
func snapshot(t *testing.T, s *studio, v *nativeapp.Validator) nativeapp.Snapshot {
	t.Helper()
	result := s.Snapshot()
	if err := v.Validate(result); err != nil {
		t.Fatalf("invalid snapshot: %v", err)
	}
	return result
}
func node(t *testing.T, s *studio, id string) nativeapp.SemanticNode {
	t.Helper()
	for _, n := range s.controller.Semantics().Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("missing control %q", id)
	return nativeapp.SemanticNode{}
}
func event(t *testing.T, s *studio, v *nativeapp.Validator, e nativeapp.Event) nativeapp.Snapshot {
	t.Helper()
	if err := s.Handle(1, e); err != nil {
		t.Fatal(err)
	}
	return snapshot(t, s, v)
}
func click(t *testing.T, s *studio, v *nativeapp.Validator, id string) {
	t.Helper()
	n := node(t, s, id)
	x, y := float32(n.Bounds.X+n.Bounds.Width/2), float32(n.Bounds.Y+n.Bounds.Height/2)
	for _, kind := range []nativeapp.EventKind{nativeapp.PointerDown, nativeapp.PointerUp} {
		event(t, s, v, nativeapp.Event{Kind: kind, Button: nativeapp.ButtonPrimary, X: x, Y: y})
	}
}
func commit(t *testing.T, s *studio, v *nativeapp.Validator, id, text string) nativeapp.Snapshot {
	t.Helper()
	return event(t, s, v, nativeapp.Event{Kind: nativeapp.TextCommit, TextContext: id, Text: text})
}

func TestStudioIdentitySkinStateAndResize(t *testing.T) {
	s, v, first := testStudio(t)
	if !s.Manifest().Skins || len(first.Surfaces) != 1 || len(first.Textures) != 1 {
		t.Fatal("initial lifecycle")
	}
	surface := first.Surfaces[0]
	if surface.Key != "studio" || surface.FrameStyle != nativeapp.FrameDefault || surface.MinWidth != 720 || surface.MinHeight != 600 || first.Textures[0].Width != 1200 || first.Textures[0].Height != 960 {
		t.Fatal("incorrect public surface metadata")
	}
	beforePixel := s.pixels.Pix[0]
	first.Textures[0].Pixels[0] ^= 255
	if s.pixels.Pix[0] != beforePixel {
		t.Fatal("snapshot borrowed framebuffer")
	}
	if len(snapshot(t, s, v).Textures) != 0 {
		t.Fatal("idle snapshot repainted")
	}
	click(t, s, v, "nav-2")
	click(t, s, v, "portfolio-2")
	click(t, s, v, "email")
	commit(t, s, v, "email", "studio@example.org")
	state := s.state
	hashes := map[[32]byte]bool{}
	for _, id := range []string{"merrick", "hologram", "plasma", "advanced"} {
		selected, err := skin.Builtin(id)
		if err != nil {
			t.Fatal(err)
		}
		// ID-independent presentation: custom packages use the same token contract.
		selected.ID = "test." + id
		if err := s.SetSkin(selected); err != nil {
			t.Fatal(err)
		}
		selected.Palette["accent"] = "#010203"
		if s.selected.Palette["accent"] == "#010203" {
			t.Fatal("skin data aliases caller")
		}
		current := snapshot(t, s, v)
		if s.state != state || current.Surfaces[0].ID != surface.ID || current.Surfaces[0].Texture != surface.Texture || current.Surfaces[0].Key != surface.Key || current.Surfaces[0].Semantics.FocusedID != "email" || !current.Surfaces[0].TextInput.Enabled || current.Surfaces[0].TextInput.Surrounding != "studio@example.org" {
			t.Fatal("skin change lost retained state/focus/identity")
		}
		hash := sha256.Sum256(current.Textures[0].Pixels)
		if hashes[hash] {
			t.Fatalf("skin %s did not change appearance", id)
		}
		hashes[hash] = true
	}
	invalid := s.selected.Clone()
	invalid.Version = 999
	if err := s.SetSkin(invalid); err == nil {
		t.Fatal("accepted invalid skin")
	}
	if s.state != state || len(snapshot(t, s, v).Textures) != 0 {
		t.Fatal("invalid skin mutated app")
	}
	revision := s.revision
	for _, size := range [][2]int{{1000, 800}, {720, 600}, {1200, 960}} {
		if err := s.Resize(1, size[0], size[1]); err != nil {
			t.Fatal(err)
		}
	}
	resized := snapshot(t, s, v)
	if len(resized.Textures) != 1 || resized.Textures[0].Revision != revision+1 {
		t.Fatal("coalesced paints skipped revision")
	}
	if err := s.Resize(1, 720, 600); err != nil {
		t.Fatal(err)
	}
	snapshot(t, s, v)
	click(t, s, v, "nav-1")
	if s.state.Section != 1 {
		t.Fatal("scaled pointer bounds failed")
	}
}

func TestStudioNavigationReelAndArchive(t *testing.T) {
	s, v, _ := testStudio(t)
	click(t, s, v, "nav-4")
	click(t, s, v, "portfolio-1")
	click(t, s, v, "play")
	if s.state.Section != 4 || s.state.Portfolio != 1 || !s.state.Playing {
		t.Fatal("navigation/portfolio/reel controls failed")
	}
	if err := s.Update(time.Second); err != nil {
		t.Fatal(err)
	}
	snapshot(t, s, v)
	if s.state.Reel <= 0 {
		t.Fatal("reel did not advance")
	}
	click(t, s, v, "play")
	stopped := s.state.Reel
	if err := s.Update(time.Second); err != nil {
		t.Fatal(err)
	}
	if s.state.Reel != stopped || len(snapshot(t, s, v).Textures) != 0 {
		t.Fatal("paused reel changed")
	}
	click(t, s, v, "reel-reset")
	if s.state.Reel != 0 {
		t.Fatal("reset failed")
	}
	click(t, s, v, "updates-next")
	if s.state.UpdateOffset != 1 {
		t.Fatal("update navigation failed")
	}
	event(t, s, v, nativeapp.Event{Kind: nativeapp.PointerScroll, X: 1000, Y: 650, ScrollY: 1})
	if s.state.UpdateOffset != 2 {
		t.Fatal("archive pointer scroll failed")
	}
	click(t, s, v, "filter")
	commit(t, s, v, "filter", "motion")
	if len(s.filteredUpdates()) != 1 || s.state.UpdateOffset != 0 {
		t.Fatal("filter did not narrow/reset archive")
	}
	click(t, s, v, "update-2")
	if s.state.SelectedUpdate != 2 || s.state.Status != updates[2].detail {
		t.Fatal("update selection failed")
	}
	click(t, s, v, "transmissions")
	if s.state.Transmissions {
		t.Fatal("channel toggle failed")
	}
	if err := s.Focus(0); err != nil {
		t.Fatal(err)
	}
	snapshot(t, s, v)
	event(t, s, v, nativeapp.Event{Kind: nativeapp.KeyInput, Key: "Tab", Pressed: true})
	if s.controller.FocusedID() != "nav-0" {
		t.Fatal("keyboard traversal failed")
	}
	event(t, s, v, nativeapp.Event{Kind: nativeapp.KeyInput, Key: "Enter", Pressed: true})
	if s.state.Section != 0 {
		t.Fatal("keyboard activation failed")
	}
}

func TestStudioNativeTextSelectionValidationAndCancellation(t *testing.T) {
	s, v, _ := testStudio(t)
	click(t, s, v, "email")
	commit(t, s, v, "stale", "wrong")
	if s.state.Email.Text != "" {
		t.Fatal("stale IME context accepted")
	}
	commit(t, s, v, "email", "invalid")
	click(t, s, v, "subscribe")
	if !s.state.InvalidEmail || s.state.Subscribed {
		t.Fatal("invalid address accepted")
	}
	click(t, s, v, "email")
	event(t, s, v, nativeapp.Event{Kind: nativeapp.KeyInput, Key: "a", Pressed: true, Modifiers: nativeapp.ModControl})
	result := commit(t, s, v, "email", "studio@example.org")
	if result.Surfaces[0].TextInput.Cursor != 18 || s.state.InvalidEmail {
		t.Fatal("selection replacement or text-input state failed")
	}
	event(t, s, v, nativeapp.Event{Kind: nativeapp.KeyInput, Key: "Enter", Pressed: true})
	if !s.state.Subscribed {
		t.Fatal("keyboard subscription failed")
	}
	click(t, s, v, "filter")
	commit(t, s, v, "filter", "café")
	event(t, s, v, nativeapp.Event{Kind: nativeapp.KeyInput, Keycode: 14, Pressed: true})
	if s.state.Filter.Text != "caf" {
		t.Fatal("Backspace split a Unicode code point")
	}
	event(t, s, v, nativeapp.Event{Kind: nativeapp.KeyInput, Key: "Home", Pressed: true})
	commit(t, s, v, "filter", "de")
	if s.state.Filter.Text != "decaf" {
		t.Fatal("cursor insertion failed")
	}
	event(t, s, v, nativeapp.Event{Kind: nativeapp.TextPreedit, TextContext: "filter", Text: "é"})
	if s.state.Filter.Preedit != "é" {
		t.Fatal("preedit not retained")
	}
	event(t, s, v, nativeapp.Event{Kind: nativeapp.TextCommit, TextContext: "filter", DeleteBefore: 2, Text: "mo"})
	if s.state.Filter.Text != "mocaf" || s.state.Filter.Preedit != "" {
		t.Fatal("IME surrounding replacement failed")
	}
	if err := s.Handle(0, nativeapp.Event{Kind: nativeapp.KeyboardCancel}); err != nil {
		t.Fatal(err)
	}
	cancelled := snapshot(t, s, v)
	if cancelled.Surfaces[0].TextInput.Enabled || cancelled.Surfaces[0].Semantics.FocusedID != "" {
		t.Fatal("global cancellation retained text focus")
	}
	commit(t, s, v, "filter", "wrong")
	if s.state.Filter.Text != "mocaf" {
		t.Fatal("unfocused commit edited text")
	}
}

func TestStudioCloseAndProtocol(t *testing.T) {
	s, v, _ := testStudio(t)
	if err := s.CloseSurface(1); err != nil {
		t.Fatal(err)
	}
	closed := snapshot(t, s, v)
	if len(closed.Surfaces) != 0 || len(closed.RetireTextures) != 1 {
		t.Fatal("surface close did not retire its texture")
	}
	if len(snapshot(t, s, v).RetireTextures) != 0 {
		t.Fatal("resource retired twice")
	}
	before := s.state
	event(t, s, v, nativeapp.Event{Kind: nativeapp.KeyInput, Key: "Enter", Pressed: true})
	if s.state != before {
		t.Fatal("closed app accepted input")
	}
	var input, output bytes.Buffer
	codec := nativeapp.NewCodec(nil, &input)
	selected, err := skin.Builtin("plasma")
	if err != nil {
		t.Fatal(err)
	}
	requests := []nativeapp.Request{{Kind: nativeapp.RequestHello, Host: nativeapp.Host{Version: nativeapp.Version, MaxSurfaces: 1, MaxSurfaceWidth: 1920, MaxSurfaceHeight: 1080}}, {Kind: nativeapp.RequestSkin, Skin: &selected}, {Kind: nativeapp.RequestResize, Surface: 1, Width: 720, Height: 600}, {Kind: nativeapp.RequestCloseSurface, Surface: 1}, {Kind: nativeapp.RequestShutdown}}
	for i, request := range requests {
		request.Version = nativeapp.Version
		request.Sequence = uint64(i + 1)
		if err := codec.Write(request); err != nil {
			t.Fatal(err)
		}
	}
	app := &studio{}
	if err := nativeapp.Serve(context.Background(), app, &input, &output); err != nil {
		t.Fatal(err)
	}
	responses := nativeapp.NewCodec(&output, nil)
	for i := range requests {
		var response nativeapp.Response
		if err := responses.Read(&response); err != nil {
			t.Fatal(err)
		}
		if response.Error != "" || response.Sequence != uint64(i+1) {
			t.Fatalf("protocol response %d: %s", i, response.Error)
		}
	}
	if !app.released {
		t.Fatal("protocol shutdown did not release painters")
	}
}

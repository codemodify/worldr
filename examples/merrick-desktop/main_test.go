package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func testDesktop(t *testing.T, limit int) (*desktop, *nativeapp.Validator, nativeapp.Snapshot) {
	t.Helper()
	d := &desktop{}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := d.Manifest().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := d.Start(nativeapp.Host{Version: nativeapp.Version, MaxSurfaces: limit, MaxSurfaceWidth: 1920, MaxSurfaceHeight: 1080}); err != nil {
		t.Fatal(err)
	}
	v := &nativeapp.Validator{}
	return d, v, checkedSnapshot(t, d, v)
}

func checkedSnapshot(t *testing.T, d *desktop, validator *nativeapp.Validator) nativeapp.Snapshot {
	t.Helper()
	s := d.Snapshot()
	if err := validator.Validate(s); err != nil {
		t.Fatalf("invalid desktop snapshot: %v", err)
	}
	return s
}

func controlNode(t *testing.T, d *desktop, surface nativeapp.SurfaceID, id string) nativeapp.SemanticNode {
	t.Helper()
	for _, node := range d.view(surface).controller.Semantics().Nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("surface %d missing control %q", surface, id)
	return nativeapp.SemanticNode{}
}

func clickControl(t *testing.T, d *desktop, validator *nativeapp.Validator, surface nativeapp.SurfaceID, id string) {
	t.Helper()
	node := controlNode(t, d, surface, id)
	x, y := float32(node.Bounds.X+node.Bounds.Width/2), float32(node.Bounds.Y+node.Bounds.Height/2)
	for _, kind := range []nativeapp.EventKind{nativeapp.PointerDown, nativeapp.PointerUp} {
		if err := d.Handle(surface, nativeapp.Event{Kind: kind, Button: nativeapp.ButtonPrimary, X: x, Y: y}); err != nil {
			t.Fatal(err)
		}
		checkedSnapshot(t, d, validator)
	}
}

func TestDesktopSurfaceLifecycleAndReopen(t *testing.T) {
	d, validator, first := testDesktop(t, 8)
	if !d.Manifest().Skins || len(first.Surfaces) != 7 || len(first.Textures) != 7 {
		t.Fatal("initial desktop must publish seven skin-aware surfaces")
	}
	for i, surface := range first.Surfaces {
		spec, update := surfaceSpecs[i], first.Textures[i]
		if surface.ID != spec.id || surface.Key != spec.key || surface.MinWidth != 96 || surface.MinHeight != 64 || update.Width != spec.width || update.Height != spec.height || update.Revision != 1 {
			t.Fatalf("incorrect initial geometry/identity for %s: %+v", spec.key, surface)
		}
		if len(surface.Semantics.Nodes) == 0 {
			t.Fatalf("%s has no accessible controls", spec.key)
		}
	}
	if len(checkedSnapshot(t, d, validator).Textures) != 0 {
		t.Fatal("idle snapshot repainted")
	}
	pixel := d.view(1).pixels.Pix[0]
	first.Textures[0].Pixels[0] ^= 255
	if d.view(1).pixels.Pix[0] != pixel {
		t.Fatal("snapshot pixels alias the renderer")
	}
	clickControl(t, d, validator, 3, "page-next")
	oldTexture := d.view(3).texture
	if err := d.CloseSurface(3); err != nil {
		t.Fatal(err)
	}
	closed := checkedSnapshot(t, d, validator)
	if len(closed.Surfaces) != 6 || len(closed.RetireTextures) != 1 || closed.RetireTextures[0] != oldTexture {
		t.Fatal("closed document did not withdraw and retire")
	}
	if err := d.CloseSurface(3); err != nil {
		t.Fatal(err)
	}
	if len(checkedSnapshot(t, d, validator).RetireTextures) != 0 {
		t.Fatal("duplicate retirement")
	}
	before := d.state
	if err := d.Handle(3, nativeapp.Event{Kind: nativeapp.KeyInput, Key: "Enter", Pressed: true}); err != nil {
		t.Fatal(err)
	}
	if d.state != before {
		t.Fatal("closed surface accepted stale input")
	}
	clickControl(t, d, validator, 6, "open-3")
	if !d.view(3).live || d.view(3).texture == oldTexture || d.view(3).revision != 1 || d.state.Pages[0] != 1 {
		t.Fatal("Programs did not reopen document with stable state and a fresh resource")
	}
	if len(checkedSnapshot(t, d, validator).Surfaces) != 7 {
		t.Fatal("reopened surface absent")
	}
}

func TestDesktopPointerKeyboardAndSkinState(t *testing.T) {
	d, validator, first := testDesktop(t, 8)
	clickControl(t, d, validator, 1, "week-next")
	clickControl(t, d, validator, 1, "day-4")
	clickControl(t, d, validator, 2, "record-tab-1")
	clickControl(t, d, validator, 2, "access")
	clickControl(t, d, validator, 2, "archive-sync")
	clickControl(t, d, validator, 4, "page-next")
	clickControl(t, d, validator, 7, "outgoing")
	clickControl(t, d, validator, 7, "message-next")
	if d.state.Week != 1 || d.state.Day != 4 || d.state.RecordTab != 1 || d.state.Access || d.state.ArchiveSync || d.state.Pages[1] != 1 || !d.state.Outgoing || d.state.Message != 1 {
		t.Fatalf("pointer actions failed: %+v", d.state)
	}
	clickControl(t, d, validator, 2, "nutrient-0")
	old := d.state.Nutrients[0]
	if err := d.Handle(2, nativeapp.Event{Kind: nativeapp.KeyInput, Keycode: 106, Pressed: true}); err != nil {
		t.Fatal(err)
	}
	checkedSnapshot(t, d, validator)
	if d.state.Nutrients[0] != old+1 {
		t.Fatal("focused range did not accept physical right-arrow")
	}
	if err := d.Focus(7); err != nil {
		t.Fatal(err)
	}
	checkedSnapshot(t, d, validator)
	if d.view(2).controller.FocusedID() != "" {
		t.Fatal("surface focus did not blur previous controls")
	}
	if err := d.Handle(7, nativeapp.Event{Kind: nativeapp.KeyInput, Key: "Tab", Pressed: true}); err != nil {
		t.Fatal(err)
	}
	checkedSnapshot(t, d, validator)
	if d.view(7).controller.FocusedID() != "incoming" {
		t.Fatal("Tab traversal did not wrap in the Messages palette")
	}
	if err := d.Handle(7, nativeapp.Event{Kind: nativeapp.KeyInput, Key: "Enter", Pressed: true}); err != nil {
		t.Fatal(err)
	}
	checkedSnapshot(t, d, validator)
	if d.state.Outgoing {
		t.Fatal("keyboard activation failed")
	}
	state, focus := d.state, d.view(7).controller.FocusedID()
	hashes := map[[32]byte]bool{}
	for _, id := range []string{"advanced", "hologram", "plasma", "merrick"} {
		selected, err := skin.Builtin(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.SetSkin(selected); err != nil {
			t.Fatal(err)
		}
		selected.Palette["accent"] = "#010203"
		if d.selected.Palette["accent"] == "#010203" {
			t.Fatal("SetSkin retained caller storage")
		}
		s := checkedSnapshot(t, d, validator)
		if d.state != state || d.view(7).controller.FocusedID() != focus || len(s.Textures) != 7 {
			t.Fatal("skin update lost application state or focus")
		}
		for i, surface := range s.Surfaces {
			if surface.ID != first.Surfaces[i].ID || surface.Key != first.Surfaces[i].Key || surface.Texture != first.Surfaces[i].Texture {
				t.Fatal("skin update changed surface identity")
			}
		}
		hash := sha256.Sum256(s.Textures[1].Pixels)
		if hashes[hash] {
			t.Fatalf("%s did not change control appearance", id)
		}
		hashes[hash] = true
	}
	invalid := d.selected.Clone()
	invalid.Version = 999
	if err := d.SetSkin(invalid); err == nil {
		t.Fatal("invalid skin accepted")
	}
	if d.state != state || len(checkedSnapshot(t, d, validator).Textures) != 0 {
		t.Fatal("invalid skin mutated state")
	}
}

func TestDesktopBoundedHostAndCoalescedResize(t *testing.T) {
	d, validator, first := testDesktop(t, 2)
	if len(first.Surfaces) != 2 || !d.view(6).live || !d.view(2).live {
		t.Fatal("constrained host must retain Programs and Records")
	}
	clickControl(t, d, validator, 6, "open-1")
	if d.view(1).live {
		t.Fatal("exceeded negotiated surface limit")
	}
	if err := d.CloseSurface(2); err != nil {
		t.Fatal(err)
	}
	checkedSnapshot(t, d, validator)
	clickControl(t, d, validator, 6, "open-1")
	if !d.view(1).live {
		t.Fatal("did not reuse the available surface slot")
	}
	before := d.view(1).revision
	for _, size := range [][2]int{{120, 500}, {96, 64}, {190, 720}} {
		if err := d.Resize(1, size[0], size[1]); err != nil {
			t.Fatal(err)
		}
	}
	s := checkedSnapshot(t, d, validator)
	if len(s.Textures) != 1 || s.Textures[0].Revision != before+1 {
		t.Fatal("unpublished repaints skipped resource revisions")
	}
	if err := d.Resize(1, 96, 64); err != nil {
		t.Fatal(err)
	}
	checkedSnapshot(t, d, validator)
	clickControl(t, d, validator, 1, "day-2")
	if d.state.Day != 2 {
		t.Fatal("scaled hit targets failed")
	}
}

func TestDesktopServesPublicProtocol(t *testing.T) {
	var input, output bytes.Buffer
	codec := nativeapp.NewCodec(nil, &input)
	selected, err := skin.Builtin("plasma")
	if err != nil {
		t.Fatal(err)
	}
	requests := []nativeapp.Request{
		{Kind: nativeapp.RequestHello, Host: nativeapp.Host{Version: nativeapp.Version, MaxSurfaces: 8, MaxSurfaceWidth: 1920, MaxSurfaceHeight: 1080}},
		{Kind: nativeapp.RequestSkin, Skin: &selected},
		{Kind: nativeapp.RequestCloseSurface, Surface: 5},
		{Kind: nativeapp.RequestShutdown},
	}
	for i, request := range requests {
		request.Version = nativeapp.Version
		request.Sequence = uint64(i + 1)
		if err := codec.Write(request); err != nil {
			t.Fatal(err)
		}
	}
	d := &desktop{}
	if err := nativeapp.Serve(context.Background(), d, &input, &output); err != nil {
		t.Fatal(err)
	}
	responses := nativeapp.NewCodec(&output, nil)
	for i := range requests {
		var response nativeapp.Response
		if err := responses.Read(&response); err != nil {
			t.Fatal(err)
		}
		if response.Error != "" || response.Sequence != uint64(i+1) {
			t.Fatalf("protocol response: %+v", response)
		}
		if i == 0 && (response.Manifest == nil || !response.Manifest.Skins || len(response.Snapshot.Surfaces) != 7) {
			t.Fatal("hello did not expose complete desktop")
		}
		if i == 2 && len(response.Snapshot.Surfaces) != 6 {
			t.Fatal("protocol close did not remove document")
		}
	}
	if !d.closed {
		t.Fatal("shutdown did not release application")
	}
}

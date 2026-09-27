package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"reflect"
	"testing"
	"time"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

func startDisk(t *testing.T) (*diskDesktop, *nativeapp.Validator, nativeapp.Snapshot) {
	t.Helper()
	d := &diskDesktop{}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := d.Manifest().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := d.Start(nativeapp.Host{Version: nativeapp.Version, MaxSurfaceWidth: 1920, MaxSurfaceHeight: 1080, MaxSurfaces: 1}); err != nil {
		t.Fatal(err)
	}
	v := &nativeapp.Validator{}
	return d, v, diskSnapshot(t, d, v)
}

func diskSnapshot(t *testing.T, d *diskDesktop, validator *nativeapp.Validator) nativeapp.Snapshot {
	t.Helper()
	s := d.Snapshot()
	if err := validator.Validate(s); err != nil {
		t.Fatalf("invalid disk snapshot: %v", err)
	}
	return s
}

func diskNode(t *testing.T, d *diskDesktop, id string) nativeapp.SemanticNode {
	t.Helper()
	for _, node := range d.controller.Semantics().Nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("missing semantic control %q", id)
	return nativeapp.SemanticNode{}
}

func diskEvent(t *testing.T, d *diskDesktop, v *nativeapp.Validator, event nativeapp.Event) nativeapp.Snapshot {
	t.Helper()
	if err := d.Handle(1, event); err != nil {
		t.Fatal(err)
	}
	return diskSnapshot(t, d, v)
}

func clickDisk(t *testing.T, d *diskDesktop, v *nativeapp.Validator, id string) {
	t.Helper()
	node := diskNode(t, d, id)
	x, y := float32(node.Bounds.X+node.Bounds.Width/2), float32(node.Bounds.Y+node.Bounds.Height/2)
	for _, kind := range []nativeapp.EventKind{nativeapp.PointerDown, nativeapp.PointerUp} {
		diskEvent(t, d, v, nativeapp.Event{Kind: kind, Button: nativeapp.ButtonPrimary, X: x, Y: y})
	}
}

func diskKey(t *testing.T, d *diskDesktop, v *nativeapp.Validator, code uint32) {
	t.Helper()
	for _, pressed := range []bool{true, false} {
		diskEvent(t, d, v, nativeapp.Event{Kind: nativeapp.KeyInput, Keycode: code, Pressed: pressed})
	}
}

func requireSelection(t *testing.T, d *diskDesktop, id string) {
	t.Helper()
	if d.state.Selected != id {
		t.Fatalf("selected %q, want %q", d.state.Selected, id)
	}
	for i, volume := range volumes {
		if !d.state.Visible[i] {
			continue
		}
		for _, prefix := range []string{"row-", "block-"} {
			if diskNode(t, d, prefix+volume.id).Selected != (volume.id == id) {
				t.Fatalf("table and partition selection disagree for %s%s", prefix, volume.id)
			}
		}
	}
}

func TestDiskLifecycleOwnsPixelsAndPublishesOnlyChanges(t *testing.T) {
	d, validator, first := startDisk(t)
	if d.Manifest().ID != "dev.worldr.hologram-desktop" || !d.Manifest().Skins || len(first.Textures) != 1 || len(first.Surfaces) != 1 {
		t.Fatal("missing manifest capability or initial resources")
	}
	surface, texture := first.Surfaces[0], first.Textures[0]
	if surface.Key != "volumes" || surface.FrameStyle != nativeapp.FrameDefault || surface.MinWidth != minimumWidth || surface.MinHeight != minimumHeight || texture.Width != initialWidth || texture.Height != initialHeight || texture.Revision != 1 {
		t.Fatal("incorrect stable surface contract or initial size")
	}
	pixel := d.pixels.Pix[0]
	texture.Pixels[0] ^= 255
	if d.pixels.Pix[0] != pixel {
		t.Fatal("published texture shares mutable application pixel storage")
	}
	if err := d.Update(time.Second); err != nil {
		t.Fatal(err)
	}
	if len(diskSnapshot(t, d, validator).Textures) != 0 {
		t.Fatal("idle desktop republished full-frame pixels")
	}
	node := diskNode(t, d, "refresh")
	hover := nativeapp.Event{Kind: nativeapp.PointerMove, X: float32(node.Bounds.X + 5), Y: float32(node.Bounds.Y + 5)}
	if len(diskEvent(t, d, validator, hover).Textures) != 1 {
		t.Fatal("new hover failed to repaint")
	}
	revision := d.revision
	for i := 0; i < 3; i++ {
		if len(diskEvent(t, d, validator, hover).Textures) != 0 || d.revision != revision {
			t.Fatal("stationary hover republished identical pixels")
		}
	}
}

func TestDiskRowsBlocksSortingAndFiltersStaySynchronized(t *testing.T) {
	d, validator, _ := startDisk(t)
	clickDisk(t, d, validator, "row-recovery")
	requireSelection(t, d, "recovery")
	clickDisk(t, d, validator, "block-efi")
	requireSelection(t, d, "efi")
	clickDisk(t, d, validator, "sort-capacity")
	if !reflect.DeepEqual(d.ordered(), []int{0, 2, 1}) || diskNode(t, d, "row-recovery").Bounds.Y >= diskNode(t, d, "row-system").Bounds.Y {
		t.Fatal("ascending capacity sort did not reorder displayed rows")
	}
	clickDisk(t, d, validator, "sort-capacity")
	if !reflect.DeepEqual(d.ordered(), []int{1, 2, 0}) {
		t.Fatal("second header click did not reverse the sort")
	}
	requireSelection(t, d, "efi")
	clickDisk(t, d, validator, "filter-efi")
	requireSelection(t, d, "system")
	for _, node := range d.controller.Semantics().Nodes {
		if node.ID == "row-efi" || node.ID == "block-efi" {
			t.Fatal("filtered volume retained interactive row or block")
		}
	}
	clickDisk(t, d, validator, "filter-system")
	clickDisk(t, d, validator, "filter-recovery")
	if len(d.ordered()) != 0 || d.state.Selected != "" {
		t.Fatal("empty filtered view retained a selected hidden volume")
	}
	clickDisk(t, d, validator, "filter-efi")
	requireSelection(t, d, "efi")
	clickDisk(t, d, validator, "reset")
	requireSelection(t, d, "system")
	if d.state.Sort != "" || len(d.ordered()) != len(volumes) {
		t.Fatal("reset did not restore the sample view")
	}
}

func TestDiskToolbarMenusAndKeyboardOperateFixtureState(t *testing.T) {
	d, validator, _ := startDisk(t)
	clickDisk(t, d, validator, "row-efi")
	diskKey(t, d, validator, 108) // host evdev ArrowDown
	requireSelection(t, d, "system")
	if d.controller.FocusedID() != "row-system" {
		t.Fatal("arrow navigation did not follow the selected row")
	}
	beforeUsed := d.usedMB(1)
	clickDisk(t, d, validator, "menu-action")
	clickDisk(t, d, validator, "popup-refresh")
	if d.state.Menu != "" || d.usedMB(1) == beforeUsed {
		t.Fatal("Action menu did not refresh and dismiss")
	}
	requireSelection(t, d, "system")
	clickDisk(t, d, validator, "view")
	if !d.state.Details {
		t.Fatal("usage/details control had no effect")
	}
	beforeGrid := sha256.Sum256(d.pixels.Pix)
	clickDisk(t, d, validator, "grid")
	if d.state.Grid || sha256.Sum256(d.pixels.Pix) == beforeGrid {
		t.Fatal("grid toggle did not update the canvas")
	}
	clickDisk(t, d, validator, "help")
	if !d.state.Help || !diskNode(t, d, "row-efi").Disabled {
		t.Fatal("help did not disable underlying controls")
	}
	clickDisk(t, d, validator, "row-efi")
	requireSelection(t, d, "system")
	diskKey(t, d, validator, 15) // Tab has exactly one enabled modal target.
	if d.controller.FocusedID() != "help-close" {
		t.Fatal("modal keyboard focus escaped into the desktop")
	}
	diskKey(t, d, validator, 28) // Enter
	if d.state.Help || diskNode(t, d, "row-efi").Disabled {
		t.Fatal("keyboard close did not restore the desktop")
	}
	clickDisk(t, d, validator, "menu-view")
	diskKey(t, d, validator, 1) // Escape
	if d.state.Menu != "" {
		t.Fatal("Escape did not close the popup menu")
	}
	clickDisk(t, d, validator, "row-efi")
	if err := d.Focus(0); err != nil {
		t.Fatal(err)
	}
	snapshot := diskSnapshot(t, d, validator)
	if snapshot.Surfaces[0].Semantics.FocusedID != "" || len(snapshot.Textures) != 1 {
		t.Fatal("focus loss left stale keyboard focus or pixels")
	}
}

func TestDiskLiveSkinAndResizeKeepStateAndValidGeometry(t *testing.T) {
	d, validator, first := startDisk(t)
	clickDisk(t, d, validator, "refresh")
	clickDisk(t, d, validator, "view")
	clickDisk(t, d, validator, "row-recovery")
	before := d.state
	previous := d.revision
	hashes := map[[32]byte]bool{}
	for _, id := range []string{"merrick", "advanced", "plasma", "hologram"} {
		selected, err := skin.Builtin(id)
		if err != nil {
			t.Fatal(err)
		}
		// Renaming proves application behavior is driven by data, never an ID.
		selected.ID = "test-" + id
		if err := d.SetSkin(selected); err != nil {
			t.Fatal(err)
		}
		selected.Palette["accent"] = "#010203"
		if d.selected.Palette["accent"] == "#010203" {
			t.Fatal("live skin borrowed caller-owned palette")
		}
		s := diskSnapshot(t, d, validator)
		if len(s.Textures) != 1 || s.Textures[0].Revision <= previous || s.Textures[0].ID != first.Textures[0].ID || s.Surfaces[0].Key != "volumes" || s.Surfaces[0].Semantics.FocusedID != "row-recovery" || d.state != before {
			t.Fatal("live skin changed state, focus, or surface identity")
		}
		previous = s.Textures[0].Revision
		hash := sha256.Sum256(s.Textures[0].Pixels)
		if hashes[hash] {
			t.Fatal("different shared skins produced identical app appearance")
		}
		hashes[hash] = true
	}
	invalid := d.selected.Clone()
	invalid.Version = 999
	if err := d.SetSkin(invalid); err == nil || d.state != before || len(diskSnapshot(t, d, validator).Textures) != 0 {
		t.Fatal("invalid skin was accepted or changed the desktop")
	}
	for _, size := range [][2]int{{900, 650}, {1100, 760}, {1920, 1080}, {1440, 1000}, {900, 650}} {
		if err := d.Resize(1, size[0], size[1]); err != nil {
			t.Fatal(err)
		}
		s := diskSnapshot(t, d, validator)
		if len(s.Textures) != 1 || s.Textures[0].Width != size[0] || s.Textures[0].Height != size[1] || s.Textures[0].Revision <= previous || d.state != before || s.Surfaces[0].Semantics.FocusedID != "row-recovery" {
			t.Fatalf("resize to %v lost state or valid texture geometry", size)
		}
		previous = s.Textures[0].Revision
		if err := d.Resize(1, size[0], size[1]); err != nil {
			t.Fatal(err)
		}
		if len(diskSnapshot(t, d, validator).Textures) != 0 {
			t.Fatal("unchanged resize republished pixels")
		}
	}
	clickDisk(t, d, validator, "block-system")
	requireSelection(t, d, "system")
	// A held button must not activate after its geometry moves on resize.
	node := diskNode(t, d, "refresh")
	diskEvent(t, d, validator, nativeapp.Event{Kind: nativeapp.PointerDown, Button: nativeapp.ButtonPrimary, X: float32(node.Bounds.X + 4), Y: float32(node.Bounds.Y + 4)})
	sample := d.state.Sample
	if err := d.Resize(1, 1400, 900); err != nil {
		t.Fatal(err)
	}
	diskSnapshot(t, d, validator)
	node = diskNode(t, d, "refresh")
	diskEvent(t, d, validator, nativeapp.Event{Kind: nativeapp.PointerUp, Button: nativeapp.ButtonPrimary, X: float32(node.Bounds.X + 4), Y: float32(node.Bounds.Y + 4)})
	if d.state.Sample != sample {
		t.Fatal("resizing a pressed control accidentally activated it")
	}
}

func TestDiskBoundsAndCloseRetireExactlyOnce(t *testing.T) {
	for _, host := range []nativeapp.Host{{MaxSurfaces: 0, MaxSurfaceWidth: 1440, MaxSurfaceHeight: 1000}, {MaxSurfaces: 1, MaxSurfaceWidth: 899, MaxSurfaceHeight: 1000}, {MaxSurfaces: 1, MaxSurfaceWidth: 1440, MaxSurfaceHeight: 649}} {
		if err := (&diskDesktop{}).Start(host); err == nil {
			t.Fatal("unsupported host limits accepted")
		}
	}
	d, validator, first := startDisk(t)
	for _, bounds := range [][4]int{{100, 100, 900, 650}, {9999, 9999, 1920, 1080}} {
		if err := d.Resize(1, bounds[0], bounds[1]); err != nil {
			t.Fatal(err)
		}
		s := diskSnapshot(t, d, validator)
		if s.Textures[0].Width != bounds[2] || s.Textures[0].Height != bounds[3] {
			t.Fatal("resize did not respect negotiated limits")
		}
	}
	if err := d.CloseSurface(2); err != nil {
		t.Fatal(err)
	}
	if len(diskSnapshot(t, d, validator).Surfaces) != 1 {
		t.Fatal("unknown surface close withdrew the app")
	}
	if err := d.CloseSurface(1); err != nil {
		t.Fatal(err)
	}
	closed := diskSnapshot(t, d, validator)
	if len(closed.Surfaces)+len(closed.Textures) != 0 || !reflect.DeepEqual(closed.RetireTextures, []nativeapp.ResourceID{first.Textures[0].ID}) {
		t.Fatal("close did not retire exactly the published texture")
	}
	for _, action := range []func() error{
		func() error { return d.CloseSurface(1) },
		func() error { return d.Resize(1, 1440, 1000) },
		func() error { return d.Handle(1, nativeapp.Event{Kind: nativeapp.PointerDown}) },
		func() error { return d.Update(time.Second) },
	} {
		if err := action(); err != nil {
			t.Fatal(err)
		}
	}
	quiet := diskSnapshot(t, d, validator)
	if len(quiet.Surfaces)+len(quiet.Textures)+len(quiet.RetireTextures) != 0 {
		t.Fatal("closed app resurrected resources or repeated retirement")
	}
}

func TestDiskProtocolSkinResizeAndClose(t *testing.T) {
	selected, err := skin.Builtin("hologram")
	if err != nil {
		t.Fatal(err)
	}
	requests := []nativeapp.Request{
		{Kind: nativeapp.RequestHello, Host: nativeapp.Host{Version: nativeapp.Version, MaxSurfaceWidth: 1440, MaxSurfaceHeight: 1000, MaxSurfaces: 1}},
		{Kind: nativeapp.RequestSkin, Skin: &selected},
		{Kind: nativeapp.RequestResize, Surface: 1, Width: 900, Height: 650},
		{Kind: nativeapp.RequestCloseSurface, Surface: 1},
		{Kind: nativeapp.RequestCloseSurface, Surface: 1},
		{Kind: nativeapp.RequestShutdown},
	}
	var input, output bytes.Buffer
	writer := nativeapp.NewCodec(nil, &input)
	for i := range requests {
		requests[i].Version, requests[i].Sequence = nativeapp.Version, uint64(i+1)
		if err := writer.Write(requests[i]); err != nil {
			t.Fatal(err)
		}
	}
	d := &diskDesktop{}
	if err := nativeapp.Serve(context.Background(), d, &input, &output); err != nil {
		t.Fatal(err)
	}
	reader, validator := nativeapp.NewCodec(&output, nil), &nativeapp.Validator{}
	retired := 0
	for i := range requests {
		var response nativeapp.Response
		if err := reader.Read(&response); err != nil {
			t.Fatal(err)
		}
		if response.Error != "" || response.Sequence != requests[i].Sequence {
			t.Fatalf("request %s failed: %s", requests[i].Kind, response.Error)
		}
		if i == 0 && (response.Manifest == nil || !response.Manifest.Skins) {
			t.Fatal("protocol did not negotiate the skin capability")
		}
		if response.Snapshot != nil {
			if err := validator.Validate(*response.Snapshot); err != nil {
				t.Fatal(err)
			}
			retired += len(response.Snapshot.RetireTextures)
		}
	}
	if retired != 1 || !d.closed {
		t.Fatal("protocol did not close and retire exactly once")
	}
}

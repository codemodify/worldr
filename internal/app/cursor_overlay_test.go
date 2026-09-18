package app

import (
	"math"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
)

type cursorOwner struct {
	cursor experience.ApplicationCursor
	set    bool
}

func (o cursorOwner) Cursor() (experience.ApplicationCursor, bool) { return o.cursor, o.set }

type cursorHubProvider struct {
	hubProvider
	cursor  experience.ApplicationCursor
	queried uint64
}

func (p *cursorHubProvider) ApplicationCursor(id uint64) (experience.ApplicationCursor, bool) {
	p.queried = id
	return p.cursor, true
}

func TestCursorHubRemapsOnlyTheRequestedLiveProvider(t *testing.T) {
	legacy := &cursorHubProvider{hubProvider: hubProvider{surfaces: []experience.ApplicationSurface{{ID: 9, Key: "legacy"}}}, cursor: experience.ApplicationCursor{Hidden: true}}
	native := &hubProvider{surfaces: []experience.ApplicationSurface{{ID: 9, Key: "native"}}}
	hub := newApplicationHub(native, legacy)
	surfaces := hub.Surfaces()
	nativeID, legacyID := surfaces[0].ID, surfaces[1].ID
	hub.Focus(nativeID)
	cursor, set := hub.ApplicationCursor(legacyID)
	if !set || !cursor.Hidden || legacy.queried != 9 || hub.focused != nativeID {
		t.Fatal("cursor lookup used keyboard focus or failed ID remapping")
	}
	legacy.queried = 0
	if _, set = hub.ApplicationCursor(nativeID); set || legacy.queried != 0 {
		t.Fatal("native provider inherited legacy cursor")
	}
	legacy.surfaces = nil
	hub.Surfaces()
	if _, set = hub.ApplicationCursor(legacyID); set || legacy.queried != 0 {
		t.Fatal("stale ID retained removed client cursor")
	}
}

func TestCursorOverlayUsesRetainedImageHotspotScaleAndBorrowedStorage(t *testing.T) {
	texture, err := render.NewTexture(32, 48, make([]byte, 32*48*4))
	if err != nil {
		t.Fatal(err)
	}
	commands := make([]render.Command, 3)
	commands[0] = render.Command{Kind: render.OverlayCommand, Count: 3}
	commands[1].First, commands[2].First = 71, 72
	vertices := make([]render.Vertex, 9)
	vertices[3].X = 123
	frame := render.Frame{Commands: commands[:1], Vertices: vertices[:3]}
	owner := cursorOwner{cursor: experience.ApplicationCursor{Texture: texture, Scale: 2, HotspotX: 3, HotspotY: 7}, set: true}
	var cursor cursorOverlay
	got := cursor.appendFor(frame, 20, 30, render.Atlas{Width: 1, Height: 1}, owner)
	if len(got.Commands) != 2 || got.Commands[1].Kind != render.ImageCommand || got.Commands[1].Image.Texture != texture || got.Commands[1].Image.Bounds != [4]float32{17, 23, 16, 24} {
		t.Fatalf("cursor placement: %+v", got.Commands)
	}
	if len(got.Vertices) != 3 || commands[1].First != 71 || commands[2].First != 72 || vertices[3].X != 123 {
		t.Fatal("cursor appended vertices or overwrote experience capacity")
	}
	if texture.Revision() != 1 {
		t.Fatal("cursor movement mutated its retained image")
	}
	owner.cursor.Hidden = true
	got = cursor.appendFor(frame, 20, 30, render.Atlas{Width: 1, Height: 1}, owner)
	if len(got.Commands) != 1 || len(got.Vertices) != 3 {
		t.Fatal("explicit hidden cursor added an overlay")
	}
}

func TestCursorOverlayMalformedOrAbsentRequestsFallBack(t *testing.T) {
	texture, err := render.NewTexture(1, 1, []byte{255, 255, 255, 255})
	if err != nil {
		t.Fatal(err)
	}
	owners := []any{nil, cursorOwner{}, cursorOwner{set: true}, cursorOwner{set: true, cursor: experience.ApplicationCursor{Texture: texture, Scale: 0}}, cursorOwner{set: true, cursor: experience.ApplicationCursor{Texture: texture, Scale: float32(math.Inf(1))}}, cursorOwner{set: true, cursor: experience.ApplicationCursor{Texture: texture, Scale: 1, HotspotX: float32(math.NaN())}}}
	for _, owner := range owners {
		var cursor cursorOverlay
		frame := cursor.appendFor(render.Frame{}, 10, 20, render.Atlas{Width: 1, Height: 1}, owner)
		if len(frame.Vertices) != 6 || len(frame.Commands) != 1 || frame.Commands[0].Kind != render.OverlayCommand {
			t.Fatalf("invalid cursor did not fall back: %+v", owner)
		}
	}
}

package app

import (
	"image"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
)

func menuForTest(t *testing.T) *experimentMenu {
	t.Helper()
	m, err := newExperimentMenu("navigator")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	m.layout(1280, 820, 1)
	return m
}

func menuPointer(m *experimentMenu, kind experience.EventKind, rect image.Rectangle) (bool, string) {
	return m.handle(experience.Event{Kind: kind, Button: experience.ButtonPrimary, X: float32(rect.Min.X+rect.Dx()/2) * m.scale, Y: float32(rect.Min.Y+rect.Dy()/2) * m.scale})
}

func menuKey(m *experimentMenu, key experience.Key, mods experience.Modifiers) (bool, string) {
	return m.handle(experience.Event{Kind: experience.KeyInput, Key: key, Pressed: true, Modifiers: mods})
}

func TestExperimentMenuRequiresCompletedClickAndCancelsCapture(t *testing.T) {
	m := menuForTest(t)
	if consumed, _ := menuPointer(m, experience.PointerUp, m.button); consumed {
		t.Fatal("closed button stole the end of an application's drag")
	}
	menuPointer(m, experience.PointerDown, m.button)
	if m.open {
		t.Fatal("button activated on press")
	}
	m.handle(experience.Event{Kind: experience.PointerCancel})
	menuPointer(m, experience.PointerUp, m.button)
	if m.open {
		t.Fatal("cancelled press activated on release")
	}
	menuPointer(m, experience.PointerDown, m.button)
	m.layout(1400, 820, 1)
	menuPointer(m, experience.PointerUp, m.button)
	if m.open {
		t.Fatal("resize preserved pointer capture")
	}
	menuPointer(m, experience.PointerDown, m.button)
	m.layout(1400, 820, 1)
	menuPointer(m, experience.PointerUp, m.button)
	if !m.open {
		t.Fatal("unchanged layout cancelled a valid click")
	}
	row0 := image.Rect(m.panel.Min.X, m.panel.Min.Y+46, m.panel.Max.X, m.panel.Min.Y+92)
	row1 := row0.Add(image.Pt(0, 46))
	menuPointer(m, experience.PointerDown, row0)
	if _, id := menuPointer(m, experience.PointerUp, row1); id != "" {
		t.Fatal("release on different row launched an experiment")
	}
	menuPointer(m, experience.PointerDown, row0)
	if consumed, id := menuPointer(m, experience.PointerUp, row0); !consumed || id != m.items[0].ID || !m.open {
		t.Fatal("completed selection must request launch and retain menu for status")
	}
	outside := image.Rect(1000, 100, 1100, 200)
	if consumed, _ := menuPointer(m, experience.PointerDown, outside); !consumed || m.open {
		t.Fatal("outside click did not dismiss and consume")
	}
	if consumed, _ := menuPointer(m, experience.PointerUp, outside); !consumed {
		t.Fatal("outside release leaked through to application")
	}
}

func TestExperimentMenuKeyboardModalInputAndBusyStatus(t *testing.T) {
	m := menuForTest(t)
	if consumed, _ := menuKey(m, experience.KeyE, experience.ModControl|experience.ModAlt); !consumed || !m.open {
		t.Fatal("global shortcut did not open")
	}
	menuKey(m, experience.KeyTab, 0)
	if m.focus != 0 {
		t.Fatal("tab did not wrap from current last experiment")
	}
	menuKey(m, experience.KeyTab, experience.ModShift)
	if m.focus != len(m.items)-1 {
		t.Fatal("shift-tab did not reverse")
	}
	menuKey(m, experience.KeyUp, 0)
	if _, id := menuKey(m, experience.KeyEnter, 0); id != m.items[len(m.items)-2].ID {
		t.Fatal("keyboard activation requested wrong experiment")
	}
	for _, kind := range []experience.EventKind{experience.KeymapChanged, experience.KeyboardModifiers, experience.KeyboardRepeatInfo} {
		if consumed, _ := m.handle(experience.Event{Kind: kind}); consumed {
			t.Fatal("overlay swallowed keyboard protocol metadata")
		}
	}
	for _, kind := range []experience.EventKind{experience.KeyInput, experience.TextCommit, experience.TextPreedit} {
		if consumed, _ := m.handle(experience.Event{Kind: kind}); !consumed {
			t.Fatal("modal keyboard input leaked to application")
		}
	}
	if consumed, _ := menuKey(m, experience.KeyQ, experience.ModControl|experience.ModAlt); consumed {
		t.Fatal("overlay prevented global quit")
	}
	m.busy = true
	if _, id := menuKey(m, experience.KeyEnter, 0); id != "" {
		t.Fatal("busy menu requested another experiment")
	}
	menuKey(m, experience.KeyEscape, 0)
	if !m.open {
		t.Fatal("busy status was dismissed")
	}
	m.busy = false
	menuKey(m, experience.KeyEscape, 0)
	if m.open {
		t.Fatal("escape did not dismiss")
	}
}

func TestExperimentMenuHiDPIScrollAndCurrentSelection(t *testing.T) {
	m := menuForTest(t)
	m.layout(1120, 840, 1.75)
	menuPointer(m, experience.PointerDown, m.button)
	menuPointer(m, experience.PointerUp, m.button)
	if !m.open || m.visible >= len(m.items) || m.scroll+m.visible != len(m.items) {
		t.Fatal("small viewport did not reveal current last experiment")
	}
	viewport := image.Rect(0, 0, 1120, 840)
	if m.ContentHeight() != 742 || m.bar != image.Rect(0, 742, 1120, 840) {
		t.Fatal("footer did not reserve exactly 56 logical pixels")
	}
	if !m.pixels(m.panel).In(viewport) || !m.pixels(m.button).In(viewport) {
		t.Fatal("scaled menu escaped viewport")
	}
	menuKey(m, experience.KeyDown, 0)
	if m.focus != 0 || m.scroll != 0 {
		t.Fatal("keyboard traversal did not scroll first row into view")
	}
	for i := 0; i < len(m.items); i++ {
		m.handle(experience.Event{Kind: experience.PointerScroll, ScrollY: 10})
	}
	if m.scroll != len(m.items)-m.visible {
		t.Fatal("scroll did not reach last experiment")
	}
	lastY := m.panel.Min.Y + 46 + (m.visible-1)*46
	last := image.Rect(m.panel.Min.X, lastY, m.panel.Max.X, lastY+46)
	menuPointer(m, experience.PointerDown, last)
	if _, id := menuPointer(m, experience.PointerUp, last); id != "" || m.open {
		t.Fatal("current experiment selection should dismiss without restart")
	}
}

func TestExperimentMenuRetainsTexturesAndPreservesBorrowedFrameStorage(t *testing.T) {
	m := menuForTest(t)
	m.open = true
	commands := make([]render.Command, 4)
	commands[1].First, commands[2].First = 71, 72
	vertices := make([]render.Vertex, 9)
	vertices[3].X = 123
	input := render.Frame{Commands: commands[:1], Vertices: vertices[:3], LinearColor: true}
	output := m.append(input)
	if len(output.Commands) != 4 || len(output.Vertices) != 3 || !output.LinearColor || commands[1].First != 71 || commands[2].First != 72 || vertices[3].X != 123 {
		t.Fatal("menu mutated experience-owned slices or frame metadata")
	}
	buttonID, panelID, barID := m.buttonTexture.ID(), m.panelTexture.ID(), m.barTexture.ID()
	buttonRevision, panelRevision, barRevision := m.buttonTexture.Revision(), m.panelTexture.Revision(), m.barTexture.Revision()
	m.append(input)
	if m.buttonTexture.Revision() != buttonRevision || m.panelTexture.Revision() != panelRevision || m.barTexture.Revision() != barRevision {
		t.Fatal("unchanged frame repainted or uploaded menu textures")
	}
	m.status = "Previous layout could not be saved."
	m.append(input)
	if m.panelTexture.Revision() == panelRevision || m.buttonTexture.Revision() != buttonRevision {
		t.Fatal("status change did not update only popup")
	}
	m.Close()
	ids := m.RetiredTextures()
	if len(ids) != 3 || ids[0] != buttonID || ids[1] != panelID || ids[2] != barID || len(m.RetiredTextures()) != 0 {
		t.Fatal("texture retirement was not exact and drainable")
	}
}

func TestExperimentMenuFooterOwnsBackgroundAndKeepsUnownedRelease(t *testing.T) {
	m := menuForTest(t)
	x, y := float32(500), float32(m.height-10)
	for _, kind := range []experience.EventKind{experience.PointerMove, experience.PointerScroll} {
		if consumed, _ := m.handle(experience.Event{Kind: kind, X: x, Y: y}); !consumed {
			t.Fatal("footer background leaked pointer input")
		}
	}
	if !m.Hovering(x, y) {
		t.Fatal("footer did not own cursor")
	}
	if consumed, _ := m.handle(experience.Event{Kind: experience.PointerUp, Button: experience.ButtonPrimary, X: x, Y: y}); consumed {
		t.Fatal("footer lost application drag release")
	}
	for _, button := range []experience.Button{experience.ButtonPrimary, experience.ButtonSecondary} {
		for _, kind := range []experience.EventKind{experience.PointerDown, experience.PointerUp} {
			if consumed, _ := m.handle(experience.Event{Kind: kind, Button: button, X: x, Y: y}); !consumed {
				t.Fatal("footer did not consume its own click")
			}
		}
	}
}

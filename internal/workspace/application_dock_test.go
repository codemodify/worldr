package workspace

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/render"
)

type dockApplications struct {
	*fakeApplications
	catalog  []experience.ApplicationLaunch
	launched []string
	closed   []uint64
	reuse    map[string]string
	texture  *render.Texture
	next     uint64
}

func newDockApplications(t *testing.T, kinds ...string) *dockApplications {
	t.Helper()
	texture, err := render.NewTexture(960, 600, make([]byte, 960*600*4))
	if err != nil {
		t.Fatal(err)
	}
	d := &dockApplications{fakeApplications: &fakeApplications{}, texture: texture, next: 800}
	for _, kind := range kinds {
		d.catalog = append(d.catalog, experience.ApplicationLaunch{Kind: kind, Title: "Open " + kind})
	}
	return d
}

func (d *dockApplications) ApplicationLaunches() []experience.ApplicationLaunch {
	return slices.Clone(d.catalog)
}

func (d *dockApplications) LaunchApplication(kind string) (string, error) {
	available := false
	for _, choice := range d.catalog {
		available = available || choice.Kind == kind
	}
	if !available {
		return "", fmt.Errorf("unsupported launch %q", kind)
	}
	d.launched = append(d.launched, kind)
	if key := d.reuse[kind]; key != "" {
		return key, nil
	}
	d.next++
	appIDs := map[string]string{
		"files":    "worldr.project-browser",
		"terminal": "worldr.native-terminal",
		"photo":    "worldr.photo-viewer",
		"media":    "worldr.media-player",
		"model":    "worldr.model-inspector",
		"research": "worldr.research-workbench",
		"note":     "worldr.native-note",
		"axial":    "worldr.axial",
	}
	key := fmt.Sprintf("native:%s-%d", kind, d.next)
	d.surfaces = append(d.surfaces, experience.ApplicationSurface{
		ID: d.next, Key: key, AppID: appIDs[kind], Title: kind, Texture: d.texture,
	})
	return key, nil
}

func (d *dockApplications) CloseApplication(id uint64) {
	d.closed = append(d.closed, id)
	for i, surface := range d.surfaces {
		if surface.ID == id {
			d.surfaces = append(d.surfaces[:i], d.surfaces[i+1:]...)
			return
		}
	}
}

func desktopDock(t *testing.T, kinds ...string) (*Workspace, *dockApplications) {
	t.Helper()
	w, err := NewDesktop()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	apps := newDockApplications(t, kinds...)
	w.SetApplications(apps)
	return w, apps
}

func dockPoint(w *Workspace, index int) (float32, float32) {
	b := applicationDockButtonBounds(index)
	return w.ox + (b.x+b.w/2)*w.scale, w.oy + (b.y+b.h/2)*w.scale
}

func TestApplicationDockHasStableNativeToolLayout(t *testing.T) {
	w, _ := desktopDock(t, "files", "terminal", "photo", "media", "model", "research", "note", "axial")
	want := []string{"files", "terminal", "photo", "media", "model", "research", "note", "axial"}
	if len(applicationDockEntries) != len(want) || !w.applicationDockVisible() {
		t.Fatalf("dock visibility or entry count is wrong: visible=%v entries=%d", w.applicationDockVisible(), len(applicationDockEntries))
	}
	previousBottom := applicationDockBounds.y
	for i, entry := range applicationDockEntries {
		if entry.kind != want[i] || entry.label == "" || entry.hint == "" {
			t.Fatalf("dock entry %d = %+v, want kind %q with a label and hint", i, entry, want[i])
		}
		b := applicationDockButtonBounds(i)
		if b.x < applicationDockBounds.x || b.y < applicationDockBounds.y || b.x+b.w > applicationDockBounds.x+applicationDockBounds.w || b.y+b.h > applicationDockBounds.y+applicationDockBounds.h || b.y < previousBottom {
			t.Fatalf("dock entry %d has an overlapping or out-of-bounds target: %+v", i, b)
		}
		previousBottom = b.y + b.h
	}
	plain := desktop(t)
	if plain.applicationDockVisible() {
		t.Fatal("dock appeared without an application launcher")
	}
}

func TestApplicationDockLaunchesAllNativeIntentsAtScaledSize(t *testing.T) {
	kinds := make([]string, len(applicationDockEntries))
	for i, entry := range applicationDockEntries {
		kinds[i] = entry.kind
	}
	w, apps := desktopDock(t, kinds...)
	w.Draw(2880, 1800)

	for i, entry := range applicationDockEntries {
		x, y := dockPoint(w, i)
		if !pointer(w, experience.PointerDown, x, y) || len(apps.launched) != i {
			t.Fatalf("%s launched before a completed click: %v", entry.kind, apps.launched)
		}
		if w.pointer.kind != captureApplicationDock || w.pointer.dockIndex != i {
			t.Fatalf("%s did not capture its scaled dock button", entry.kind)
		}
		if !pointer(w, experience.PointerUp, x, y) || len(apps.launched) != i+1 || apps.launched[i] != entry.kind {
			t.Fatalf("%s dock action routed incorrectly: %v", entry.kind, apps.launched)
		}
		if got := w.activeApplicationDockKind(); got != entry.kind {
			t.Fatalf("active dock state = %q after launching %q", got, entry.kind)
		}
	}
	if !slices.Equal(apps.launched, kinds) {
		t.Fatalf("dock launch order = %v, want %v", apps.launched, kinds)
	}
}

func TestApplicationDockHoverOccludesClientAndDrawsTooltip(t *testing.T) {
	w, apps := desktopDock(t, "files", "terminal", "photo", "media", "model", "research", "note")
	// Give the underlying client an existing pointer route. Entering the rail
	// must cancel it so client cursors and hover effects cannot show through.
	_, err := apps.LaunchApplication("files")
	if err != nil {
		t.Fatal(err)
	}
	w.syncApplications()
	w.applicationHoveredID, w.applicationHover = apps.surfaces[0].ID, true
	base := w.Draw(1440, 900)
	x, y := dockPoint(w, 4)
	if !w.Handle(experience.Event{Kind: experience.PointerMove, X: x, Y: y}) {
		t.Fatal("dock hover was not consumed")
	}
	if w.applicationDockHover != 4 || w.applicationHoveredID != 0 || w.applicationHover {
		t.Fatal("dock hover did not replace the underlying client route")
	}
	if len(apps.events) == 0 || apps.events[len(apps.events)-1].event.Kind != experience.PointerCancel {
		t.Fatal("underlying client did not receive pointer cancellation")
	}
	withTooltip := w.Draw(1440, 900)
	if len(withTooltip.Vertices) <= len(base.Vertices) {
		t.Fatal("hover did not draw a label and highlighted icon")
	}
}

func TestApplicationDockCancelDragAndUnavailableDoNotLaunch(t *testing.T) {
	w, apps := desktopDock(t, "terminal")
	w.Draw(1440, 900)
	x, y := dockPoint(w, 1)
	pointer(w, experience.PointerDown, x, y)
	w.Handle(experience.Event{Kind: experience.PointerCancel})
	pointer(w, experience.PointerUp, x, y)
	if len(apps.launched) != 0 || w.pointer.kind != captureNone {
		t.Fatal("cancelled dock press launched an application")
	}

	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerMove, x-200, y)
	pointer(w, experience.PointerUp, x-200, y)
	if len(apps.launched) != 0 {
		t.Fatal("dragged dock press launched an application")
	}

	x, y = dockPoint(w, 2)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if len(apps.launched) != 0 || !strings.Contains(w.applicationNotice, "Photo launcher is unavailable") {
		t.Fatalf("disabled photo intent was not explained: launches=%v notice=%q", apps.launched, w.applicationNotice)
	}
}

func TestApplicationDockCinematicDecorationCalmsWithoutMovingHitTargets(t *testing.T) {
	w, _ := desktopDock(t, "files", "terminal", "photo", "media", "model", "research", "note")
	w.Draw(1440, 900)
	w.presentationBlend = 1
	cinematic := w.Draw(1440, 900)
	w.presentationBlend = 0
	adaptive := w.Draw(1440, 900)
	if len(cinematic.Vertices) <= len(adaptive.Vertices) {
		t.Fatal("cinematic dock did not add its circuit framing")
	}
	for i := range applicationDockEntries {
		b := applicationDockButtonBounds(i)
		if got := applicationDockIndexAt(b.x+b.w/2, b.y+b.h/2); got != i {
			t.Fatalf("presentation-independent dock hit target %d resolved to %d", i, got)
		}
	}
}

func TestApplicationDockLaunchDoesNotStopAnotherWindowThrow(t *testing.T) {
	w, base := windowThrowWorkspace(t, 1)
	apps := newDockApplications(t, "files", "terminal", "photo", "media", "model", "research", "note")
	apps.fakeApplications = base
	apps.texture = base.surfaces[0].Texture
	apps.next = 900
	w.SetApplications(apps)
	w.Draw(1440, 900)
	original := base.surfaces[0]
	startWindowThrow(t, w, original)
	motion := w.windowThrow
	w.Update(50 * time.Millisecond)
	beforeLaunch := w.Document().View.Application.Layouts[w.m.applicationState.index(original.Key)]

	x, y := dockPoint(w, 6)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if w.windowThrow != motion || len(apps.launched) != 1 || apps.launched[0] != "note" {
		t.Fatalf("dock launch interrupted independent motion: throw=%p want=%p launches=%v", w.windowThrow, motion, apps.launched)
	}
	w.Update(50 * time.Millisecond)
	afterLaunch := w.Document().View.Application.Layouts[w.m.applicationState.index(original.Key)]
	if afterLaunch.X == beforeLaunch.X || w.windowThrow == nil {
		t.Fatal("the original window did not keep coasting after a dock launch")
	}
}

func TestApplicationDockRemainsAvailableAtFarZoom(t *testing.T) {
	w, apps := desktopDock(t, "files", "terminal", "photo", "media", "model", "research", "note")
	if _, err := apps.LaunchApplication("files"); err != nil {
		t.Fatal(err)
	}
	w.syncApplications()
	command(t, w, Action{Kind: ZoomCamera, DeltaZoom: -.8})
	w.Draw(1440, 900)
	const dockIndex = 0
	clickX, clickY := dockPoint(w, dockIndex)
	before := len(apps.launched)
	if !pointer(w, experience.PointerDown, clickX, clickY) || w.pointer.kind != captureApplicationDock {
		t.Fatal("far camera zoom prevented the application dock from capturing its button")
	}
	pointer(w, experience.PointerUp, clickX, clickY)
	if len(apps.launched) != before+1 || apps.launched[len(apps.launched)-1] != applicationDockEntries[dockIndex].kind {
		t.Fatalf("far camera zoom dock click did not launch entry %d: %v", dockIndex, apps.launched)
	}
}

func TestApplicationDockActivatesExistingFilesWithoutMovingItsSpace(t *testing.T) {
	w, apps := desktopDock(t, "files")
	key, err := apps.LaunchApplication("files")
	if err != nil {
		t.Fatal(err)
	}
	w.syncApplications()
	command(t, w, Action{Kind: CreateSpace, SpaceName: "Archive"})
	command(t, w, Action{Kind: SelectApplication, ApplicationKey: key})
	command(t, w, Action{Kind: MoveToSpace, Space: 1})
	before := w.Document().View.Application.Layouts[w.m.applicationState.index(key)]
	apps.reuse = map[string]string{"files": key}
	apps.launched = nil

	x, y := dockPoint(w, 0)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	after := w.Document().View.Application.Layouts[w.m.applicationState.index(key)]
	if len(apps.launched) != 1 || apps.launched[0] != "files" || w.m.applicationState.Space != 1 || after != before {
		t.Fatalf("existing Files was moved instead of activated: launches=%v space=%d before=%+v after=%+v", apps.launched, w.m.applicationState.Space, before, after)
	}
}

func TestApplicationDockRollsBackNewSurfaceWhenSavedLayoutIsFull(t *testing.T) {
	w, apps := desktopDock(t, "note")
	d := w.Document()
	for i := range d.View.Application.Layouts {
		d.View.Application.Layouts[i] = ApplicationPlacement{Key: fmt.Sprintf("closed:%02d", i), Space: 0}
	}
	d.View.Application.Active = d.View.Application.Layouts[0].Key
	d.View.Application.Selected = 1
	w.install(d, false)
	before := w.Document()

	x, y := dockPoint(w, 6)
	pointer(w, experience.PointerDown, x, y)
	pointer(w, experience.PointerUp, x, y)
	if len(apps.closed) != 1 || len(apps.surfaces) != 0 || w.Document() != before {
		t.Fatalf("failed dock launch leaked a hidden surface or changed the view: closed=%v surfaces=%d", apps.closed, len(apps.surfaces))
	}
	if !strings.Contains(w.applicationNotice, "32-window limit") && !strings.Contains(w.applicationNotice, "not available") {
		t.Fatalf("capacity failure was not explained: %q", w.applicationNotice)
	}
}

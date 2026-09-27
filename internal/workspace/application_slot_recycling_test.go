package workspace

import (
	"fmt"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
)

func TestClosedApplicationSlotsRecycleAcrossSequentialWindows(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	template := apps.surfaces[0]
	for cycle := 0; cycle < MaxApplicationLayouts*2; cycle++ {
		apps.surfaces = nil
		w.syncApplications()
		key := fmt.Sprintf("sequential/window-%d", cycle)
		if err := w.CheckApplicationPlacement(key); err != nil {
			t.Fatalf("cycle %d dead-ended before launch: %v", cycle, err)
		}
		surface := template
		surface.ID = uint64(1000 + cycle)
		surface.Key = key
		apps.surfaces = []experience.ApplicationSurface{surface}
		w.syncApplications()
		if w.m.applicationState.index(key) < 0 || len(w.applicationSurfaces) != 1 || w.applicationLayoutFull {
			t.Fatalf("cycle %d did not register through a recyclable slot", cycle)
		}
		if err := w.Document().Validate(); err != nil {
			t.Fatalf("cycle %d produced invalid state: %v", cycle, err)
		}
	}
	used := 0
	for _, placement := range w.m.applicationState.Layouts {
		if placement.Key != "" {
			used++
		}
	}
	if used != MaxApplicationLayouts {
		t.Fatalf("sequential launches retained %d slots, want the bounded %d", used, MaxApplicationLayouts)
	}
}

func TestReopenedKeyKeepsPlacementUntilItsSlotIsActuallyRecycled(t *testing.T) {
	w, apps := multipleApplications(t, 2)
	reopened := apps.surfaces[0]
	command(t, w, Action{Kind: MoveApplication, ApplicationKey: reopened.Key, DeltaX: 3.25, DeltaY: -1.5, DeltaDepth: .7})
	saved := w.m.applicationState.Layouts[w.m.applicationState.index(reopened.Key)]

	apps.surfaces = apps.surfaces[1:]
	w.syncApplications()
	newSurface := apps.surfaces[0]
	newSurface.ID, newSurface.Key = 9900, "new/while-capacity-remains"
	apps.surfaces = append(apps.surfaces, newSurface)
	w.syncApplications()
	if got := w.m.applicationState.Layouts[w.m.applicationState.index(reopened.Key)]; got != saved {
		t.Fatalf("an empty slot existed but the retained placement changed: got %+v want %+v", got, saved)
	}

	apps.surfaces = append(apps.surfaces, reopened)
	w.syncApplications()
	if got := w.m.applicationState.Layouts[w.m.applicationState.index(reopened.Key)]; got != saved {
		t.Fatalf("reopening a retained key lost its position: got %+v want %+v", got, saved)
	}
}

func TestRecyclingNeverEvictsAProviderLiveLayout(t *testing.T) {
	w, apps := multipleApplications(t, MaxApplicationLayouts)
	before := w.m.applicationState.Layouts
	template := apps.surfaces[0]
	closed := apps.surfaces[len(apps.surfaces)-1]
	live := append([]experience.ApplicationSurface(nil), apps.surfaces[:len(apps.surfaces)-1]...)
	first, second := template, template
	first.ID, first.Key = 10001, "incoming/first"
	second.ID, second.Key = 10002, "incoming/second"
	// Put both incoming surfaces first. Precomputing provider reservations must
	// protect every later live layout before the recycler chooses the one closed
	// slot; the second incoming surface then remains excluded.
	apps.surfaces = append([]experience.ApplicationSurface{first, second}, live...)
	w.syncApplications()

	for _, surface := range live {
		i := w.m.applicationState.index(surface.Key)
		if i < 0 || w.m.applicationState.Layouts[i] != before[before.indexForTest(surface.Key)] {
			t.Fatalf("recycling evicted or changed live layout %q", surface.Key)
		}
	}
	if w.m.applicationState.index(closed.Key) >= 0 || w.m.applicationState.index(first.Key) < 0 || w.m.applicationState.index(second.Key) >= 0 {
		t.Fatalf("recycler did not replace exactly the one closed slot: closed=%d first=%d second=%d", w.m.applicationState.index(closed.Key), w.m.applicationState.index(first.Key), w.m.applicationState.index(second.Key))
	}
	if len(w.applicationSurfaces) != MaxApplicationLayouts || !w.applicationLayoutFull {
		t.Fatal("the unplaceable extra live surface did not leave a bounded capacity notice")
	}
}

func (layouts ApplicationLayouts) indexForTest(key string) int {
	for i, placement := range layouts {
		if placement.Key == key {
			return i
		}
	}
	return -1
}

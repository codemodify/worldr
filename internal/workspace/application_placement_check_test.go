package workspace

import (
	"strings"
	"testing"
)

func TestApplicationPlacementCheckOffersClosedLayoutsWithoutMutatingThem(t *testing.T) {
	w, apps := multipleApplications(t, MaxApplicationLayouts)
	closedKey := apps.surfaces[len(apps.surfaces)-1].Key
	liveKey := apps.surfaces[0].Key
	apps.surfaces = apps.surfaces[:1]
	w.syncApplications()
	if err := w.ActivateApplication(liveKey); err != nil {
		t.Fatal(err)
	}
	w.Draw(1440, 900)
	before, camera := w.Document(), w.camera
	focusedID, keyboard := w.applicationFocusedID, w.applicationKeyboard
	historyPosition, historyLength := w.historyPosition, len(w.history)
	focusCalls, inputEvents, resizes := len(apps.focus), len(apps.events), len(apps.resizes)
	if err := w.CheckApplicationPlacement("native:photo-viewer"); err != nil {
		t.Fatalf("closed saved placements were not offered to a genuinely new viewer: %v", err)
	}
	for _, key := range []string{liveKey, closedKey} {
		if err := w.CheckApplicationPlacement(key); err != nil {
			t.Fatalf("existing key %q could not reuse its retained slot: %v", key, err)
		}
	}
	if w.Document() != before || w.camera != camera || w.applicationFocusedID != focusedID || w.applicationKeyboard != keyboard || !keyboard || w.historyPosition != historyPosition || len(w.history) != historyLength {
		t.Fatal("capacity checks changed document, Read camera, keyboard ownership or history")
	}
	if len(apps.focus) != focusCalls || len(apps.events) != inputEvents || len(apps.resizes) != resizes {
		t.Fatal("capacity checks invoked application focus, input or resizing")
	}
	if w.m.applicationState.index("native:photo-viewer") >= 0 {
		t.Fatal("preflight reserved a placement before its application existed")
	}
}

func TestApplicationPlacementCheckUsesLastFreeSlotWithoutReservingIt(t *testing.T) {
	w, _ := multipleApplications(t, MaxApplicationLayouts-1)
	before := w.Document()
	for _, key := range []string{"native:photo-viewer", "native:media-player", "native:photo-viewer"} {
		if err := w.CheckApplicationPlacement(key); err != nil {
			t.Fatalf("remaining saved-layout slot was rejected for %q: %v", key, err)
		}
	}
	if w.Document() != before {
		t.Fatal("successful checks consumed the remaining slot")
	}
	for _, key := range []string{"", "bad\nkey", string([]byte{0xff}), strings.Repeat("x", 257)} {
		if err := w.CheckApplicationPlacement(key); err == nil {
			t.Fatalf("invalid application key %q passed placement validation", key)
		}
	}
	if w.Document() != before {
		t.Fatal("invalid key checks changed saved placements")
	}
}

func TestApplicationPlacementCheckAccountsForUnregisteredLiveKeys(t *testing.T) {
	for _, mode := range []string{"ready-surface", "loading-placeholder"} {
		t.Run(mode, func(t *testing.T) {
			w, apps := multipleApplications(t, MaxApplicationLayouts-1)
			retainedKey := apps.surfaces[len(apps.surfaces)-1].Key
			if err := w.ActivateApplication(apps.surfaces[0].Key); err != nil {
				t.Fatal(err)
			}
			w.Draw(1440, 900)
			incoming := apps.surfaces[0]
			incoming.ID, incoming.Key = 9000, "external:new-window"
			if mode == "loading-placeholder" {
				incoming.Texture = nil
			}
			// The host can publish another provider's surface before the next
			// workspace Update has assigned that key its saved layout slot.
			apps.surfaces = append(apps.surfaces, incoming)
			before, camera := w.Document(), w.camera
			focusCalls, focusedID := len(apps.focus), w.applicationFocusedID
			if err := w.CheckApplicationPlacement("native:photo-viewer"); err == nil {
				t.Fatal("incoming live window and photo were admitted into the same final slot")
			}
			for _, key := range []string{apps.surfaces[0].Key, retainedKey, incoming.Key} {
				if err := w.CheckApplicationPlacement(key); err != nil {
					t.Fatalf("reusing stored key or the pending key's own slot was refused: %q: %v", key, err)
				}
			}
			if w.Document() != before || w.camera != camera || len(apps.focus) != focusCalls || w.applicationFocusedID != focusedID || !w.applicationKeyboard {
				t.Fatal("pending-surface preflight changed the existing application's view or focus")
			}
		})
	}
}

func TestApplicationPlacementCheckCountsDistinctValidPendingKeysOnly(t *testing.T) {
	w, apps := multipleApplications(t, MaxApplicationLayouts-2)
	incoming := apps.surfaces[0]
	incoming.ID, incoming.Key = 9000, "external:new-window"
	apps.surfaces = append(apps.surfaces, incoming)
	incoming.ID++
	apps.surfaces = append(apps.surfaces, incoming) // Duplicate stable key.
	incoming.ID, incoming.Key = 9002, "native:photo-viewer"
	apps.surfaces = append(apps.surfaces, incoming) // The requested key's slot.
	incoming.ID, incoming.Key = 0, "invalid-id"
	apps.surfaces = append(apps.surfaces, incoming)
	incoming.ID, incoming.Key = 9003, "invalid\nkey"
	apps.surfaces = append(apps.surfaces, incoming)
	before := w.Document()
	if err := w.CheckApplicationPlacement("native:photo-viewer"); err != nil {
		t.Fatal("duplicates, the requested key, or invalid identities consumed extra future slots", err)
	}
	if err := w.CheckApplicationPlacement("native:media-player"); err == nil {
		t.Fatal("two distinct pending windows did not consume the two remaining slots")
	}
	if w.Document() != before {
		t.Fatal("checking pending identities registered them prematurely")
	}
}

func TestApplicationPlacementCheckReservesRecyclableSlotsForPendingKeys(t *testing.T) {
	w, apps := multipleApplications(t, MaxApplicationLayouts)
	apps.surfaces = apps.surfaces[:MaxApplicationLayouts-2]
	template := apps.surfaces[0]
	first := template
	first.ID, first.Key, first.Texture = 9100, "pending/first", nil
	apps.surfaces = append(apps.surfaces, first)
	before := w.Document()
	if err := w.CheckApplicationPlacement("requested/new-window"); err != nil {
		t.Fatalf("one pending key and one request did not fit two recyclable slots: %v", err)
	}
	second := template
	second.ID, second.Key, second.Texture = 9101, "pending/second", nil
	apps.surfaces = append(apps.surfaces, second)
	if err := w.CheckApplicationPlacement("requested/new-window"); err == nil {
		t.Fatal("two pending keys and a new request overbooked two recyclable slots")
	}
	for _, key := range []string{first.Key, second.Key} {
		if err := w.CheckApplicationPlacement(key); err != nil {
			t.Fatalf("pending key %q could not claim its own reserved recyclable slot: %v", key, err)
		}
	}
	if w.Document() != before {
		t.Fatal("recyclable-slot preflight mutated saved placements")
	}
}

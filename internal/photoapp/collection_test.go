package photoapp

import (
	"context"
	"encoding/json"
	"image"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

type collectionDecode struct {
	file   *os.File
	budget int64
	answer chan image.Image
}

func controlledCollection(t *testing.T) (*Collection, chan collectionDecode) {
	t.Helper()
	calls := make(chan collectionDecode, MaxViewers)
	c := newCollection(func(ctx context.Context, file *os.File, budget int64) (image.Image, error) {
		call := collectionDecode{file: file, budget: budget, answer: make(chan image.Image, 1)}
		calls <- call
		select {
		case photo := <-call.answer:
			return photo, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	t.Cleanup(func() { c.Close() })
	return c, calls
}
func nextCollectionDecode(t *testing.T, calls chan collectionDecode) collectionDecode {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("photo collection did not admit a decode")
		return collectionDecode{}
	}
}
func pollCollection(t *testing.T, c *Collection, predicate func() bool) {
	t.Helper()
	awaitPhoto(t, func() bool {
		if err := c.Poll(); err != nil {
			t.Fatal(err)
		}
		return predicate()
	})
}

func TestPhotoCollectionIndependentWindowsAndReusedSlotsRejectStaleInput(t *testing.T) {
	c, calls := controlledCollection(t)
	first, second := photoTestFile(t, []byte("first")), photoTestFile(t, []byte("second"))
	key, err := c.OpenFile(first, "First.png")
	if err != nil || key != "native:photo-viewer" {
		t.Fatal(key, err)
	}
	one := nextCollectionDecode(t, calls)
	key, err = c.OpenFile(second, "Second.png")
	if err != nil || key != "native:photo-viewer-2" {
		t.Fatal(key, err)
	}
	if len(calls) != 0 {
		t.Fatal("parallel photo decodes bypassed admission")
	}
	initial := append([]uint64{}, c.Surfaces()[0].ID, c.Surfaces()[1].ID)
	textures := []uint64{c.Surfaces()[0].Texture.ID(), c.Surfaces()[1].Texture.ID()}
	one.answer <- photoSolid(5, 4, 80)
	pollCollection(t, c, func() bool { return c.slots[0].manager.photo != nil })
	two := nextCollectionDecode(t, calls)
	if two.budget != maxCollectionPixels-20 {
		t.Fatal("second decode did not receive remaining aggregate pixel budget", two.budget)
	}
	two.answer <- photoSolid(4, 5, 150)
	pollCollection(t, c, func() bool { return c.slots[1].manager.photo != nil })
	if len(c.Surfaces()) != 2 || c.slots[0].manager.photo.Bounds().Dx() != 5 || c.slots[1].manager.photo.Bounds().Dx() != 4 {
		t.Fatal("second photo replaced first")
	}
	c.Resize(initial[1], 800, 500)
	if c.slots[0].manager.renderer.requested == c.slots[1].manager.renderer.requested {
		t.Fatal("resize affected sibling")
	}
	c.CloseApplication(initial[0])
	if len(c.Surfaces()) != 1 || c.Surfaces()[0].ID != initial[1] || c.Surfaces()[0].Texture.ID() != textures[1] {
		t.Fatal("closing first affected second photo")
	}
	if ids := c.RetiredTextures(); !reflect.DeepEqual(ids, []uint64{textures[0]}) || len(c.RetiredTextures()) != 0 {
		t.Fatal("retired wrong texture or repeated retirement", ids)
	}
	third := photoTestFile(t, []byte("third"))
	key, err = c.OpenFile(third, "Third.png")
	if err != nil || key != "native:photo-viewer" {
		t.Fatal(key, err)
	}
	if c.Surfaces()[0].ID <= initial[1] {
		t.Fatal("stable slot reused stale runtime identity")
	}
	requested := c.slots[0].manager.renderer.requested
	c.Resize(initial[0], 640, 360)
	c.CloseApplication(initial[0])
	if c.slots[0] == nil || c.slots[0].manager.renderer.requested != requested {
		t.Fatal("stale routing altered replacement slot")
	}
	c.Close()
	if len(c.Surfaces()) != 0 || c.SessionStates() != nil || len(c.RetiredTextures()) != 2 {
		t.Fatal("collection close did not retire every live texture")
	}
}

func TestPhotoCollectionRestoreKeepsSparseKeysAndCapacityOwnership(t *testing.T) {
	c, _ := controlledCollection(t)
	file := photoTestFile(t, []byte("sixth"))
	state := WindowState{Key: "native:photo-viewer-6", SessionState: SessionState{Path: file.Name()}}
	if key, err := c.RestoreFile(file, "Sixth.png", state); err != nil || key != state.Key {
		t.Fatal(key, err)
	}
	if states := c.SessionStates(); len(states) != 1 || states[0] != state {
		t.Fatal("pending restore lost sparse stable key", states)
	}
	if key, err := c.NextKey(); err != nil || key != "native:photo-viewer" {
		t.Fatal("restore renumbered free slots", key, err)
	}
	duplicate := photoTestFile(t, []byte("caller-owned"))
	if _, err := c.RestoreFile(duplicate, "duplicate", state); err == nil {
		t.Fatal("restore silently replaced occupied slot")
	}
	if _, err := duplicate.Stat(); err != nil {
		t.Fatal("failed restore took descriptor ownership", err)
	}
	for len(c.Surfaces()) < MaxViewers {
		if _, err := c.OpenFile(photoTestFile(t, []byte("pending")), "pending.png"); err != nil {
			t.Fatal(err)
		}
	}
	overflow := photoTestFile(t, []byte("overflow"))
	if _, err := c.OpenFile(overflow, "overflow.png"); err == nil {
		t.Fatal("collection exceeded window capacity")
	}
	if _, err := overflow.Stat(); err != nil {
		t.Fatal("capacity rejection consumed caller descriptor")
	}
	saved := c.SessionStates()
	for i, window := range saved {
		if window.Key != photoSlotKey(i) || window.Validate() != nil {
			t.Fatal("session slots were not canonical", saved)
		}
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "SessionState") {
		t.Fatal("embedded state did not produce flat session JSON")
	}
	var restored []WindowState
	if err := json.Unmarshal(data, &restored); err != nil || !reflect.DeepEqual(saved, restored) {
		t.Fatal("session JSON did not round trip", err)
	}
	managers := make([]*Manager, 0, MaxViewers)
	for _, window := range c.slots {
		managers = append(managers, window.manager)
	}
	c.Close()
	for _, manager := range managers {
		select {
		case <-manager.done:
		case <-time.After(time.Second):
			t.Fatal("collection close leaked a pending decoder")
		}
	}
	if _, err := file.Stat(); err == nil {
		t.Fatal("restored photo descriptor remained open", err)
	}
	for _, key := range []string{"", "native:photo-viewer-1", "native:photo-viewer-01", "native:photo-viewer-9", "native:media-player"} {
		invalid := state
		invalid.Key = key
		if invalid.Validate() == nil {
			t.Fatal("accepted invalid photo slot", key)
		}
	}
}

func TestPhotoCollectionBudgetAndCanceledCodecRemainSerialized(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	c := newCollection(func(ctx context.Context, file *os.File, budget int64) (image.Image, error) {
		started <- file.Name()
		if strings.Contains(file.Name(), "blocking") {
			<-release
			return nil, ctx.Err()
		}
		return photoSolid(5, 5, 80), nil
	})
	defer c.Close()
	c.pixelLimit = 24
	blocking, err := os.CreateTemp(t.TempDir(), "blocking-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.OpenFile(blocking, "blocking"); err != nil {
		t.Fatal(err)
	}
	<-started
	firstID := c.Surfaces()[0].ID
	next := photoTestFile(t, []byte("next"))
	if _, err = c.OpenFile(next, "next.png"); err != nil {
		t.Fatal(err)
	}
	c.CloseApplication(firstID)
	for i := 0; i < 3; i++ {
		if err := c.Poll(); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-started:
		t.Fatal("next decode began before canceled codec completed")
	default:
	}
	close(release)
	pollCollection(t, c, func() bool { return c.active != nil && !c.active.manager.closed })
	<-started
	pollCollection(t, c, func() bool { return !c.slots[1].manager.loading })
	if c.slots[1].manager.photo != nil || !strings.Contains(c.slots[1].manager.message, "combined 64 megapixel") {
		t.Fatal("over-budget result became retained content", c.slots[1].manager.message)
	}
	if len(c.Surfaces()) != 1 {
		t.Fatal("decode error did not remain visible in its own window")
	}
}

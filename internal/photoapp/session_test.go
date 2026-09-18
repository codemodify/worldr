package photoapp

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPhotoSessionPromotesOnlySuccessfullyDecodedDescriptorPath(t *testing.T) {
	m, calls := controlledPhotoManager(t)
	if _, ok := m.SessionState(); ok {
		t.Fatal("empty photo viewer produced a session")
	}
	first := photoTestFile(t, []byte("first"))
	if _, err := m.OpenFile(first, "display name unrelated to the path.png"); err != nil {
		t.Fatal(err)
	}
	call := nextDecode(t, calls)
	if state, ok := m.SessionState(); !ok || state.Path != first.Name() || state.Validate() != nil {
		t.Fatalf("pending first photo lost its real resume path: %+v %v", state, ok)
	}
	renamed := filepath.Join(filepath.Dir(first.Name()), "renamed\nphoto.png")
	if err := os.Rename(first.Name(), renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.Name(), []byte("unrelated replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	finishPhoto(t, m, call, photoSolid(3, 2, 80), nil)
	if state, ok := m.SessionState(); !ok || state.Path != renamed {
		t.Fatalf("successful decode did not capture the descriptor's current path: %+v %v", state, ok)
	}
	bad := photoTestFile(t, []byte("bad"))
	if _, err := m.OpenFile(bad, "bad.png"); err != nil {
		t.Fatal(err)
	}
	call = nextDecode(t, calls)
	if state, _ := m.SessionState(); state.Path != renamed {
		t.Fatal("pending replacement overwrote the last successfully decoded photo path")
	}
	finishPhoto(t, m, call, nil, errors.New("invalid image"))
	if state, ok := m.SessionState(); !ok || state.Path != renamed {
		t.Fatal("failed replacement lost the visible photo's resume path")
	}
	last := photoTestFile(t, []byte("last"))
	if _, err := m.OpenFile(last, "last.png"); err != nil {
		t.Fatal(err)
	}
	finishPhoto(t, m, nextDecode(t, calls), photoSolid(2, 3, 140), nil)
	if state, ok := m.SessionState(); !ok || state.Path != last.Name() {
		t.Fatal("successful replacement did not promote its resume path")
	}
	m.CloseApplication(m.next)
	if _, ok := m.SessionState(); ok {
		t.Fatal("closed photo viewer remained in the session")
	}
}

func TestPhotoSessionExcludesFailedInitialAndUnlinkedImages(t *testing.T) {
	m, calls := controlledPhotoManager(t)
	file := photoTestFile(t, []byte("bad"))
	if _, err := m.OpenFile(file, "bad.png"); err != nil {
		t.Fatal(err)
	}
	finishPhoto(t, m, nextDecode(t, calls), nil, errors.New("bad image"))
	if _, ok := m.SessionState(); ok {
		t.Fatal("failed initial photo remained in session")
	}
	file = photoTestFile(t, []byte("unlinked"))
	if err := os.Remove(file.Name()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.OpenFile(file, "unlinked.png"); err != nil {
		t.Fatal("a non-resumable descriptor prevented ordinary viewing:", err)
	}
	finishPhoto(t, m, nextDecode(t, calls), photoSolid(2, 2, 30), nil)
	if m.photo == nil {
		t.Fatal("unlinked image was not shown")
	}
	if _, ok := m.SessionState(); ok {
		t.Fatal("unlinked photo saved an obsolete filename")
	}
}

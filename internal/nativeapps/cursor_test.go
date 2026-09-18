package nativeapps

import (
	"image"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/terminal"
)

func TestProviderHonorsSteadyAndBlinkingApplicationCursors(t *testing.T) {
	p, backend := testProvider(t)
	now := time.UnixMilli(0)
	p.now = func() time.Time { return now }
	p.Focus(1)
	backend.snapshot.Cursor = terminal.Cursor{Row: 2, Col: 3, Visible: true, Shape: 3}
	backend.snapshot.Revision++
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	x, y := contentLeft+3*cellWidth, contentTop+2*cellHeight
	if got := p.renderer.image.RGBAAt(x, y+1); got != rgb(0x83e6f1) {
		t.Fatal("steady bar cursor was not painted")
	}
	steady := p.renderer.texture.Revision()
	now = now.Add(550 * time.Millisecond)
	if err := p.Poll(); err != nil || p.renderer.texture.Revision() != steady {
		t.Fatal("steady cursor blinked or produced an unnecessary texture upload", err)
	}
	backend.snapshot.Cursor.Blink = true
	backend.snapshot.Revision++
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	if got := p.renderer.image.RGBAAt(x, y+1); got == rgb(0x83e6f1) {
		t.Fatal("application-requested blink did not hide the cursor")
	}
	before := p.renderer.texture.Revision()
	now = now.Add(550 * time.Millisecond)
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	if got := p.renderer.image.RGBAAt(x, y+1); got != rgb(0x83e6f1) {
		t.Fatal("application-requested blink did not show the cursor again")
	}
	update, changed := p.renderer.texture.Snapshot(before)
	if !changed || update.Rect != image.Rect(contentLeft, y, contentLeft+backend.snapshot.Cols*cellWidth, y+cellHeight) {
		t.Fatalf("cursor phase change damaged unrelated content: %v", update.Rect)
	}
	// A steady underline requested while the blink clock is off must replace
	// the previous bar immediately and then stay visible across clock changes.
	now = now.Add(550 * time.Millisecond)
	backend.snapshot.Cursor.Blink = false
	backend.snapshot.Cursor.Shape = 2
	backend.snapshot.Revision++
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	if p.renderer.image.RGBAAt(x, y+1) == rgb(0x83e6f1) || p.renderer.image.RGBAAt(x+cellWidth-1, y+cellHeight-1) != rgb(0x83e6f1) {
		t.Fatal("switching to steady underline retained the blinking bar")
	}
	steady = p.renderer.texture.Revision()
	now = now.Add(550 * time.Millisecond)
	if err := p.Poll(); err != nil || p.renderer.texture.Revision() != steady {
		t.Fatal("steady underline was repainted with the blink clock", err)
	}
}

func TestProviderInactiveAndHiddenCursorsDoNotAnimate(t *testing.T) {
	p, backend := testProvider(t)
	now := time.UnixMilli(550)
	p.now = func() time.Time { return now }
	backend.snapshot.Cursor = terminal.Cursor{Row: 1, Col: 2, Visible: true, Blink: true, Shape: 1}
	backend.snapshot.Revision++
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	x, y := contentLeft+2*cellWidth, contentTop+cellHeight
	if p.renderer.image.RGBAAt(x, y) != rgb(0x83e6f1) || p.renderer.image.RGBAAt(x+2, y+2) == rgb(0x83e6f1) {
		t.Fatal("unfocused blinking cursor did not use a steady outline")
	}
	checkStable := func(why string) {
		t.Helper()
		before := p.renderer.texture.Revision()
		now = now.Add(550 * time.Millisecond)
		if err := p.Poll(); err != nil || p.renderer.texture.Revision() != before {
			t.Fatalf("%s cursor kept animating: %v", why, err)
		}
	}
	checkStable("unfocused")
	p.Focus(1)
	backend.snapshot.Cursor.Visible = false
	backend.snapshot.Revision++
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	checkStable("hidden")
	backend.snapshot.Cursor.Visible = true
	backend.snapshot.Exited = true
	backend.snapshot.Revision++
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	if p.renderer.image.RGBAAt(x, y) == rgb(0x83e6f1) {
		t.Fatal("exited terminal retained its cursor")
	}
	checkStable("exited")
}

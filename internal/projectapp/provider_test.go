package projectapp

import (
	"context"
	"image"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

type readerCall struct {
	request request
	answer  chan result
}

type controlledReader struct {
	calls  chan readerCall
	closed chan struct{}
}

func (r *controlledReader) read(req request) result {
	call := readerCall{request: req, answer: make(chan result, 1)}
	r.calls <- call
	select {
	case res := <-call.answer:
		res.request = req
		return res
	case <-req.ctx.Done():
		return result{request: req, err: req.ctx.Err()}
	}
}

func (r *controlledReader) close() error { close(r.closed); return nil }

func controlledProvider(t *testing.T) (*Provider, *controlledReader) {
	t.Helper()
	r := &controlledReader{calls: make(chan readerCall, 4), closed: make(chan struct{})}
	p, err := newProvider(r, "/project")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = p.Close()
		select {
		case <-r.closed:
		case <-time.After(time.Second):
			t.Error("browser worker did not release its reader")
		}
	})
	return p, r
}

func nextCall(t *testing.T, r *controlledReader) readerCall {
	t.Helper()
	select {
	case call := <-r.calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("browser worker did not receive the request")
		return readerCall{}
	}
}

func await(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatal("browser did not reach the expected state")
		}
		time.Sleep(time.Millisecond)
	}
}

func pollUntil(t *testing.T, p *Provider, predicate func() bool) {
	t.Helper()
	await(t, func() bool {
		if err := p.Poll(); err != nil {
			t.Fatal(err)
		}
		return predicate()
	})
}

func TestProviderRejectsQueuedStalePreviewAndKeepsCurrentSelection(t *testing.T) {
	p, r := controlledProvider(t)
	listing := nextCall(t, r)
	listing.answer <- result{entries: []entry{{name: "first.txt"}, {name: "second.txt"}}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	first := nextCall(t, r)
	first.answer <- result{preview: makePreview("stale first preview")}
	await(t, func() bool { return len(p.results) == 1 })
	p.selectEntry(1) // Supersede a result already buffered by the worker.
	second := nextCall(t, r)
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	if !p.loadingFile || p.preview.text != "" || p.previewPath != "second.txt" {
		t.Fatalf("stale result replaced pending selection: %+v", p.preview)
	}
	generation := p.generation
	p.selectEntry(1)
	p.openSelected()
	if p.generation != generation || second.request.ctx.Err() != nil {
		t.Fatal("reselecting/opening the current file canceled its pending read")
	}
	second.answer <- result{preview: makePreview(strings.Repeat("second line\n", 100))}
	pollUntil(t, p, func() bool { return !p.loadingFile })
	p.scroll(10, false)
	p.openSelected()
	if p.previewTop != 10 || !strings.HasPrefix(p.preview.text, "second line") || p.generation != generation {
		t.Fatal("opening the current preview lost its text or scroll position")
	}
	texture := p.surfaces[0].Texture
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	revision := texture.Revision()
	for i := 0; i < 3; i++ {
		if err := p.Poll(); err != nil {
			t.Fatal(err)
		}
	}
	if texture.Revision() != revision {
		t.Fatal("idle polling uploaded an unchanged browser image")
	}
	p.Focus(1)
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	update, changed := texture.Snapshot(revision)
	if !changed || update.Rect == image.Rect(0, 0, update.Width, update.Height) {
		t.Fatal("a focus-only repaint did not retain partial image damage")
	}
}

type blockedReader struct{ started, release, closed chan struct{} }

func (r *blockedReader) read(req request) result {
	close(r.started)
	<-r.release // Model a filesystem read that cannot be interrupted immediately.
	return result{request: req, entries: []entry{{name: "late.txt"}}}
}
func (r *blockedReader) close() error { close(r.closed); return nil }

func TestProviderCloseDoesNotWaitForFilesystemOrPublishLateResults(t *testing.T) {
	r := &blockedReader{started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	p, err := newProvider(r, "/project")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	<-r.started
	texture := p.Surfaces()[0].Texture
	finished := make(chan struct{})
	go func() { _ = p.Close(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		close(r.release)
		<-finished
		t.Fatal("browser close waited for a blocked filesystem read")
	}
	if len(p.Surfaces()) != 0 {
		t.Fatal("close did not immediately remove the surface")
	}
	retired := p.RetiredTextures()
	if len(retired) != 1 || retired[0] != texture.ID() {
		t.Fatalf("close did not retire the retained texture once: %v", retired)
	}
	close(r.release)
	select {
	case <-r.closed:
	case <-time.After(time.Second):
		t.Fatal("reader anchor was not closed after its read unwound")
	}
	p.Focus(1)
	p.Resize(1, 900, 500)
	p.Send(1, experience.Event{Kind: experience.KeyInput, Pressed: true, Keycode: 63})
	p.CloseApplication(1)
	if err := p.Poll(); err != nil || len(p.Surfaces()) != 0 || len(p.RetiredTextures()) != 0 || len(p.results) != 0 {
		t.Fatal("closed browser accepted late work or retired its texture twice", err)
	}
}

func TestProviderPointerTargetsOnlyPaintedRowsAndConsecutiveClicks(t *testing.T) {
	p, r := controlledProvider(t)
	listing := nextCall(t, r)
	entries := make([]entry, 50)
	for i := range entries {
		entries[i] = entry{name: "folder", kind: directoryEntry}
	}
	listing.answer <- result{entries: entries}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	click := func(x, y float32, stamp uint32) {
		p.Send(1, experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: x, Y: y, Time: stamp})
	}
	generation := p.generation
	click(-.2, contentTop+rowHeight+2, 10)
	click(float32(math.NaN()), contentTop+rowHeight+2, 20)
	click(20, float32(contentTop+p.rows()*rowHeight+1), 30)
	if p.selected != 0 || p.generation != generation {
		t.Fatal("out-of-bounds/unpainted row click changed selection")
	}
	click(20, contentTop+2, 100)
	click(float32(p.renderer.split()+40), contentTop+2, 200)
	click(20, contentTop+2, 300)
	if p.directory != "" {
		t.Fatal("an intervening preview click still counted as a double-click")
	}
	click(90, contentTop+2, 350)
	if p.directory != "" {
		t.Fatal("distant clicks on one row counted as a double-click")
	}
	click(91, contentTop+3, 400)
	if p.directory != "folder" {
		t.Fatal("a consecutive nearby double-click failed to open its folder")
	}
}

func TestProviderResizePublishesCurrentContentBeforeNextPoll(t *testing.T) {
	p, r := controlledProvider(t)
	listing := nextCall(t, r)
	listing.answer <- result{entries: []entry{{name: "fixture.txt"}}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	preview := nextCall(t, r)
	preview.answer <- result{preview: makePreview("Visible project source")}
	pollUntil(t, p, func() bool { return !p.loadingFile })
	texture := p.Surfaces()[0].Texture
	p.Resize(1, 800, 400) // The host can resize after polling, before rendering.
	if p.Surfaces()[0].Texture != texture {
		t.Fatal("resize replaced the retained texture identity")
	}
	update, ok := texture.Snapshot(0)
	if !ok || update.Width != 800 || update.Height != 400 {
		t.Fatal("resize did not publish its new extent immediately")
	}
	visibleText := 0
	for y := contentTop; y < contentTop+rowHeight; y++ {
		for x := p.renderer.split() + 67; x < update.Width-12; x++ {
			i := (y*update.Width + x) * 4
			if update.Pixels[i] > 150 && update.Pixels[i+1] > 170 && update.Pixels[i+2] > 170 {
				visibleText++
			}
		}
	}
	if visibleText < 20 {
		t.Fatal("resized texture lost its text before the next browser poll")
	}
	if err := p.Poll(); err != nil || texture.Revision() != update.Revision {
		t.Fatal("resize left an unnecessary repaint queued", err)
	}
}

func TestProviderCopyRefusesNonUTF8PathWithoutChangingItsBytes(t *testing.T) {
	p, r := controlledProvider(t)
	listing := nextCall(t, r)
	listing.answer <- result{entries: []entry{{name: "valid", kind: directoryEntry}, {name: "invalid-\xff", kind: fileEntry}}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	p.copyPath()
	if !p.copyReady {
		t.Fatal("valid path did not produce an explicit copy request")
	}
	p.selectEntry(1)
	p.copyPath()
	if copied, ready := p.TakeCopy(); ready || copied != "" {
		t.Fatalf("invalid path exported substituted or stale text: %q", copied)
	}
	if !strings.Contains(p.notice, "non-UTF-8") || !p.dirty || p.entries[1].name != "invalid-\xff" {
		t.Fatal("invalid copy failed to report a status or changed the selected filename")
	}
	preview := nextCall(t, r)
	preview.answer <- result{preview: makePreview("Readable source with a non-UTF-8 filename")}
	pollUntil(t, p, func() bool { return !p.loadingFile })
	if !strings.Contains(p.notice, "non-UTF-8") {
		t.Fatal("an asynchronous preview completion erased the failed-copy status")
	}
	if err := p.Poll(); err != nil {
		t.Fatal("invalid filename status could not be rendered", err)
	}
}

func TestWorkerSkipsCanceledPendingRequest(t *testing.T) {
	r := &controlledReader{calls: make(chan readerCall, 1), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests, results, done := make(chan request, 2), make(chan result, 1), make(chan struct{})
	canceled, cancelRequest := context.WithCancel(ctx)
	cancelRequest()
	requests <- request{ctx: canceled, path: "superseded"}
	requests <- request{ctx: ctx, path: "current"}
	go runReader(ctx, r, requests, results, done)
	call := nextCall(t, r)
	if call.request.path != "current" {
		t.Fatal("worker opened a superseded pending request")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop on cancellation")
	}
}

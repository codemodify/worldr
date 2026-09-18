package projectapp

import (
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

func TestFilenameSearchRoutesInputAndPreservesSelectedIdentity(t *testing.T) {
	p, reader := controlledProvider(t)
	listing := nextCall(t, reader)
	listing.answer <- result{entries: []entry{{name: "alpha.png", kind: fileEntry}, {name: "BETA.png", kind: fileEntry}, {name: "docs", kind: directoryEntry}}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	p.Focus(1)
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 33, Pressed: true, Modifiers: experience.ModControl})
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 48, Pressed: true})
	if !p.searchActive || p.query != "b" || len(p.entries) != 1 || p.selectionName() != "BETA.png" || len(p.allEntries) != 3 {
		t.Fatal("Ctrl+F text input did not filter case-insensitive filenames")
	}
	if state, ok := p.SessionState(); !ok || state.Selected != "BETA.png" {
		t.Fatal("filter index was confused with the saved filename", state)
	}
	p.copyPath()
	if path, ready := p.TakeCopy(); !ready || path != "/project/BETA.png" {
		t.Fatal("filtered selection copied the wrong path", path)
	}
	generation := p.generation
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: true})
	if p.searchActive || p.generation != generation || len(reader.calls) != 0 {
		t.Fatal("confirming search launched a file")
	}
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 33, Pressed: true, Modifiers: experience.ModControl})
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 1, Pressed: true})
	if p.query != "" || p.searchActive || len(p.entries) != 3 || p.selected != 1 || p.selectionName() != "BETA.png" {
		t.Fatal("clearing search discarded the selected file")
	}
}

func TestSearchIsBoundedAndDoesNotCancelPendingStartupSelection(t *testing.T) {
	state := SessionState{Root: "/project", Directory: "docs", Selected: "BETA.png"}
	p, reader := sessionProvider(t, state)
	listing := nextCall(t, reader)
	generation := p.generation
	p.setQuery("beta")
	if p.generation != generation || listing.request.ctx.Err() != nil {
		t.Fatal("typing search canceled the initial listing")
	}
	if got, ok := p.SessionState(); !ok || got != state {
		t.Fatal("pending search lost the saved selection", got)
	}
	listing.answer <- result{entries: []entry{{name: "alpha.png", kind: fileEntry}, {name: "BETA.png", kind: fileEntry}}, truncated: true}
	pollUntil(t, p, func() bool { return !p.loadingDirectory })
	if p.selectionName() != "BETA.png" || !p.listTruncated {
		t.Fatal("search lost truncation or pending selection")
	}
	p.setQuery(strings.Repeat("x", maxSearchBytes+1))
	if p.query != "beta" {
		t.Fatal("search accepted unbounded text")
	}
	p.setQuery("λ")
	if len(p.entries) != 0 || p.selected != -1 || !strings.Contains(p.message, "No filenames match") {
		t.Fatal("empty search results kept a stale file selection")
	}
	p.searchActive = true
	p.Focus(1)
	p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: 14, Pressed: true})
	if p.query != "" || len(p.entries) != 2 {
		t.Fatal("backspace did not remove one complete UTF-8 character")
	}
}

func TestAutomaticRefreshPreservesPreviewScrollAndRechecksChangedFile(t *testing.T) {
	p, reader := controlledProvider(t)
	listing := nextCall(t, reader)
	note := entry{name: "notes.txt", kind: fileEntry}
	listing.answer <- result{entries: []entry{note}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	preview := nextCall(t, reader)
	text := strings.Repeat("unchanged long preview text\n", 100)
	preview.answer <- result{preview: makePreview(text)}
	pollUntil(t, p, func() bool { return !p.loadingFile })
	p.previewTop = 10
	now := time.Unix(100, 0)
	p.now = func() time.Time { return now }
	p.nextRefresh = now
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	refresh := nextCall(t, reader)
	if !refresh.request.refresh || refresh.request.selected != "notes.txt" || p.preview.text != text || p.selected != 0 {
		t.Fatal("refresh blanked the live listing/preview")
	}
	generation := p.generation
	for range 5 {
		p.Poll()
	}
	if p.generation != generation || len(reader.calls) != 0 {
		t.Fatal("automatic refresh queued repeated work")
	}
	refresh.answer <- result{entries: []entry{{name: "new.png", kind: fileEntry}, note}}
	pollUntil(t, p, func() bool { return !p.loadingDirectory })
	if p.selected != 1 || p.preview.text != text || p.previewTop != 10 || p.loadingFile || len(reader.calls) != 0 {
		t.Fatal("unchanged selected file lost its preview, scroll or identity")
	}
	now = now.Add(autoRefreshInterval)
	p.Poll()
	changed := nextCall(t, reader)
	note.stamp.size = 42
	changed.answer <- result{entries: []entry{note}}
	pollUntil(t, p, func() bool { return p.loadingFile })
	updated := nextCall(t, reader)
	if updated.request.path != "notes.txt" || updated.request.directory {
		t.Fatal("changed selection did not reload its preview")
	}
	updated.answer <- result{preview: makePreview("new contents")}
	pollUntil(t, p, func() bool { return !p.loadingFile })
	if p.preview.text != "new contents" {
		t.Fatal("changed file kept stale preview text")
	}
}

package projectapp

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func sessionProvider(t *testing.T, state SessionState) (*Provider, *controlledReader) {
	t.Helper()
	reader := &controlledReader{calls: make(chan readerCall, 4), closed: make(chan struct{})}
	p, err := newProviderAt(reader, state.Root, state.Directory, state.Selected, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.Close()
		select {
		case <-reader.closed:
		case <-time.After(time.Second):
			t.Error("session reader did not close")
		}
	})
	return p, reader
}

func TestSessionValidationRejectsUnsafeOrUnrepresentableNavigation(t *testing.T) {
	valid := SessionState{Root: "/project", Directory: "docs/sub", Selected: "photo.png"}
	for _, change := range []func(*SessionState){
		func(s *SessionState) { s.Root = "relative" },
		func(s *SessionState) { s.Root = "" },
		func(s *SessionState) { s.Root = "/project/../other" },
		func(s *SessionState) { s.Root = "/project\x00" },
		func(s *SessionState) { s.Root = "/project\xff" },
		func(s *SessionState) { s.Directory = "/outside" },
		func(s *SessionState) { s.Directory = "../outside" },
		func(s *SessionState) { s.Directory = "docs/../outside" },
		func(s *SessionState) { s.Directory = "docs//sub" },
		func(s *SessionState) { s.Directory = "docs/" },
		func(s *SessionState) { s.Directory = "." },
		func(s *SessionState) { s.Directory = ".." },
		func(s *SessionState) { s.Directory = "docs\x00" },
		func(s *SessionState) { s.Directory = "docs\xff" },
		func(s *SessionState) { s.Selected = "../photo.png" },
		func(s *SessionState) { s.Selected = "/photo.png" },
		func(s *SessionState) { s.Selected = "folder/photo.png" },
		func(s *SessionState) { s.Selected = "folder/" },
		func(s *SessionState) { s.Selected = "." },
		func(s *SessionState) { s.Selected = ".." },
		func(s *SessionState) { s.Selected = "photo\x00.png" },
		func(s *SessionState) { s.Selected = "photo\xff.png" },
		func(s *SessionState) { s.Directory = strings.Repeat("x", 4097) },
	} {
		state := valid
		change(&state)
		if err := state.Validate(); err == nil {
			t.Fatalf("accepted invalid session: %+v", state)
		}
		if p, err := NewSession(state); err == nil || p != nil {
			if p != nil {
				p.Close()
			}
			t.Fatalf("NewSession accepted invalid session: %+v", state)
		}
	}
	for _, state := range []SessionState{
		{Root: "/does-not-need-to-exist"},
		valid,
		{Root: "/project space", Directory: "docs and data/λ", Selected: "photo\nname.png"},
	} {
		if err := state.Validate(); err != nil {
			t.Fatal("shape validation rejected a valid path or touched the filesystem", err)
		}
		data, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		var decoded SessionState
		if err := json.Unmarshal(data, &decoded); err != nil || decoded != state {
			t.Fatal("session JSON did not round-trip filenames", err)
		}
	}
}

func TestSessionPreservesPendingSelectionAndRestoresWithoutActivation(t *testing.T) {
	for _, item := range []entry{{name: "chosen.mp4", kind: fileEntry}, {name: "chosen.png", kind: fileEntry}, {name: "chosen", kind: directoryEntry}} {
		t.Run(item.name, func(t *testing.T) {
			state := SessionState{Root: "/project", Directory: "assets", Selected: item.name}
			p, reader := sessionProvider(t, state)
			p.SetOpenHandler(func(*os.File, string) error { t.Fatal("restore opened media"); return nil })
			p.SetTerminalHandler(func(*os.File, string) error { t.Fatal("restore opened terminal"); return nil })
			call := nextCall(t, reader)
			if call.request.path != "assets" || call.request.selected != item.name || !call.request.directory || !call.request.restoreDirectory {
				t.Fatal("restore did not start directly in the saved folder")
			}
			generation, revision := p.generation, p.surfaces[0].Texture.Revision()
			for range 3 {
				if snapshot, ok := p.SessionState(); !ok || snapshot != state {
					t.Fatalf("pending selection was discarded: %+v, %v", snapshot, ok)
				}
			}
			if p.generation != generation || p.surfaces[0].Texture.Revision() != revision || p.focused {
				t.Fatal("snapshot mutated the browser or took focus")
			}
			call.answer <- result{entries: []entry{{name: "first.png", kind: fileEntry}, item}}
			pollUntil(t, p, func() bool { return !p.loadingDirectory })
			if p.selected != 1 || p.loadingFile || p.focused || p.openingTerminal || len(reader.calls) != 0 {
				t.Fatal("restored selection changed focus, opened the folder, or launched/read media")
			}
			if snapshot, ok := p.SessionState(); !ok || snapshot != state {
				t.Fatalf("ready selection differs from saved state: %+v, %v", snapshot, ok)
			}
		})
	}
}

func TestSessionRefreshKeepsSelectionWhileInitialListingIsPending(t *testing.T) {
	state := SessionState{Root: "/project", Directory: "assets", Selected: "photo.png"}
	p, reader := sessionProvider(t, state)
	first := nextCall(t, reader)
	p.refresh()
	second := nextCall(t, reader)
	if first.request.ctx.Err() == nil || second.request.selected != state.Selected {
		t.Fatal("refresh lost the pending saved selection")
	}
	if got, ok := p.SessionState(); !ok || got != state {
		t.Fatal("snapshot after refresh lost startup selection", got)
	}
	second.answer <- result{entries: []entry{{name: "photo.png", kind: fileEntry}}}
	pollUntil(t, p, func() bool { return !p.loadingDirectory })
}

func TestSessionMissingFolderRecoversToRootWithPersistentNotice(t *testing.T) {
	p, reader := sessionProvider(t, SessionState{Root: "/project", Directory: "gone/nested", Selected: "photo.png"})
	call := nextCall(t, reader)
	call.answer <- result{err: os.ErrNotExist}
	pollUntil(t, p, func() bool { return p.directory == "" && p.loadingDirectory })
	root := nextCall(t, reader)
	if root.request.path != "" || !root.request.directory || root.request.restoreDirectory {
		t.Fatal("missing folder recovery did not request the root once")
	}
	root.answer <- result{entries: []entry{{name: "README", kind: fileEntry}}}
	pollUntil(t, p, func() bool { return p.selected == 0 })
	preview := nextCall(t, reader)
	preview.answer <- result{preview: makePreview("project root")}
	pollUntil(t, p, func() bool { return !p.loadingFile })
	if !strings.Contains(p.notice, "Saved folder could not be restored") || p.preview.text != "project root" || p.focused {
		t.Fatal("recovery notice was lost during root selection or preview", p.notice)
	}
	if got, ok := p.SessionState(); !ok || got.Directory != "" || got.Selected != "README" {
		t.Fatal("snapshot retained the missing folder or stale selection", got)
	}
}

func TestSessionStaleRestoreCannotOverrideNavigationOrReopenClosedBrowser(t *testing.T) {
	for _, action := range []string{"navigate", "close"} {
		t.Run(action, func(t *testing.T) {
			p, reader := sessionProvider(t, SessionState{Root: "/project", Directory: "saved", Selected: "old.png"})
			initial := nextCall(t, reader)
			initial.answer <- result{err: os.ErrNotExist}
			await(t, func() bool { return len(p.results) == 1 })
			if action == "close" {
				p.Close()
				if got, ok := p.SessionState(); ok || got != (SessionState{}) {
					t.Fatal("closed browser was included in the session", got)
				}
			} else {
				p.navigate("current", "new.png")
				current := nextCall(t, reader)
				if err := p.Poll(); err != nil || p.directory != "current" || p.notice != "" {
					t.Fatal("stale restore failure overrode current navigation", err)
				}
				current.answer <- result{entries: []entry{{name: "new.png", kind: fileEntry}}}
				pollUntil(t, p, func() bool { return p.selected == 0 })
				if got, ok := p.SessionState(); !ok || got.Directory != "current" || got.Selected != "new.png" {
					t.Fatal("new navigation did not replace restored state", got)
				}
			}
			if err := p.Poll(); err != nil || p.focused || p.openingTerminal || p.loadingFile {
				t.Fatal("canceled restore activated the browser", err)
			}
		})
	}
}

func TestSessionMissingSelectedItemFallsBackAndNonUTF8SelectionIsOmitted(t *testing.T) {
	p, reader := sessionProvider(t, SessionState{Root: "/project", Directory: "assets", Selected: "gone.png"})
	listing := nextCall(t, reader)
	listing.answer <- result{entries: []entry{{name: "photo\xff.png", kind: fileEntry}}}
	pollUntil(t, p, func() bool { return !p.loadingDirectory })
	if p.selected != 0 || p.loadingFile || p.focused {
		t.Fatal("missing selection did not safely select the first current item")
	}
	if got, ok := p.SessionState(); !ok || got.Selected != "" || got.Directory != "assets" {
		t.Fatal("snapshot changed invalid filename bytes or dropped the valid folder", got)
	}
	p.directory = "assets\xff"
	if _, ok := p.SessionState(); ok {
		t.Fatal("snapshot included a directory that cannot round-trip JSON")
	}
}

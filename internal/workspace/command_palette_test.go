package workspace

import (
	"github.com/codemodify/worldr/internal/experience"
	"strings"
	"testing"
)

type catalogApplications struct{ *launchingApplications }

func (a *catalogApplications) ApplicationLaunches() []experience.ApplicationLaunch {
	return []experience.ApplicationLaunch{{Kind: "terminal", Title: "New terminal"}}
}
func paletteKey(w *Workspace, code uint32, pressed bool) bool {
	return w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: code, Pressed: pressed})
}
func openPalette(t *testing.T, w *Workspace) {
	t.Helper()
	if !w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 57, Modifiers: experience.ModControl | experience.ModAlt, Pressed: true}) {
		t.Fatal("launcher chord not consumed")
	}
	paletteKey(w, 57, false)
	if w.commands == nil || !w.commands.open || !w.OwnsKeyboard() {
		t.Fatal("launcher did not own input")
	}
}
func paletteQuery(w *Workspace, text string) {
	w.commands.field.Set("")
	w.Handle(experience.Event{Kind: experience.TextCommit, Text: text, TextContext: w.TextInput().ContextID})
	w.refreshCommands()
}
func TestPaletteCreatesSpaceAndLaunchesWithoutLeakingKeys(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	w.desktop = true
	launcher := &catalogApplications{terminalLauncher(t, apps)}
	w.SetApplications(launcher)
	openPalette(t, w)
	before := len(apps.events)
	paletteQuery(w, "Research")
	if len(w.commands.entries) != 2 || !strings.HasPrefix(w.commands.entries[0].label, "Create space") {
		t.Fatal("space creation not discoverable", w.commands.entries)
	}
	paletteKey(w, 28, true)
	paletteKey(w, 28, false)
	if w.commands.open || w.m.applicationState.Space != 1 || w.m.applicationState.spaceName(1) != "Research" || w.OwnsKeyboard() {
		t.Fatal("space creation changed focus or failed")
	}
	openPalette(t, w)
	paletteQuery(w, "New terminal")
	paletteKey(w, 28, true)
	paletteKey(w, 28, false)
	if len(launcher.launched) != 1 || w.m.applicationState.Layouts[w.m.applicationState.index(launcher.surface.Key)].Space != 1 {
		t.Fatal("launcher opened outside new space")
	}
	if len(apps.events) != before {
		t.Fatal("palette text or Enter leaked into app")
	}
}
func TestPaletteRejectsStaleIMEAndKeepsCaretRedraw(t *testing.T) {
	w, _ := multipleApplications(t, 1)
	w.desktop = true
	openPalette(t, w)
	old := w.TextInput().ContextID
	paletteKey(w, 1, true)
	paletteKey(w, 1, false)
	openPalette(t, w)
	if old == w.TextInput().ContextID {
		t.Fatal("palette context reused")
	}
	w.Handle(experience.Event{Kind: experience.TextCommit, Text: "stale", TextContext: old})
	if w.commands.field.Text() != "" {
		t.Fatal("stale IME reached reopened query")
	}
	paletteQuery(w, "abc")
	w.Draw(1440, 900)
	paletteKey(w, 105, true)
	if !w.commands.dirty || w.commands.field.Caret() != 2 {
		t.Fatal("caret movement did not redraw")
	}
	paletteKey(w, 105, false)
	// Held key repeats remain local and edit the field while the palette is open.
	w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 14, Pressed: true})
	w.Handle(experience.Event{Kind: experience.KeyInput, Keycode: 14, Pressed: true, Repeat: true})
	if w.commands.field.Text() != "c" {
		t.Fatal("repeat was swallowed", w.commands.field.Text())
	}
}
func TestNewTerminalReusesClosedSlotInCurrentSpace(t *testing.T) {
	w, apps := multipleApplications(t, 1)
	launcher := terminalLauncher(t, apps)
	w.SetApplications(launcher)
	w.launchTerminal(launcher)
	key := launcher.surface.Key
	command(t, w, Action{Kind: CreateSpace, SpaceName: "Build"})
	old := w.m.applicationState.index(key)
	w.m.applicationState.Layouts[old].Group = 5
	apps.surfaces = apps.surfaces[:1]
	w.Update(0)
	command(t, w, Action{Kind: SwitchSpace, Space: 1})
	launcher.surface.ID++
	w.launchTerminal(launcher)
	p := w.m.applicationState.Layouts[w.m.applicationState.index(key)]
	if p.Space != 1 || p.Group != 0 || w.m.applicationState.Active != key || w.applicationNotice != "" {
		t.Fatal("closed launch slot stranded in old space", p, w.applicationNotice)
	}
}

func TestCommandClipboardCannotPasteAcrossPaletteLifetimes(t *testing.T) {
	w := study(t)
	w.desktop = true
	w.openCommands()
	w.commands.field.Set("宇宙")
	w.commands.field.SelectAll()
	copyKey := experience.Event{Kind: experience.KeyInput, Keycode: 46, Pressed: true, Modifiers: experience.ModControl}
	w.handleCommands(copyKey)
	if text, ok := w.TakeCopy(); !ok || text != "宇宙" {
		t.Fatal("palette copy failed", text)
	}
	paste := experience.Event{Kind: experience.KeyInput, Keycode: 47, Pressed: true, Modifiers: experience.ModControl}
	w.handleCommands(paste)
	if !w.TakePasteRequest() {
		t.Fatal("palette paste missing")
	}
	w.commands.open = false
	w.openCommands()
	w.Paste("stale space")
	if w.commands.field.Text() != "" {
		t.Fatal("old transfer reached a new palette")
	}
	paste.Pressed = false
	w.handleCommands(paste)
	paste.Pressed = true
	w.handleCommands(paste)
	if !w.TakePasteRequest() {
		t.Fatal("new lifetime paste blocked")
	}
	w.Paste("Research")
	if w.commands.field.Text() != "Research" {
		t.Fatal("paste not committed")
	}
	found := false
	for _, e := range w.commands.entries {
		found = found || e.label == "Create space / Research"
	}
	if !found {
		t.Fatal("pasted query did not refresh commands")
	}
}

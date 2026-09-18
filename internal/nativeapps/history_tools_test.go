package nativeapps

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/terminal"
)

type historyTerminal struct {
	fakeTerminal
	history terminal.History
	jumps   []uint64
}

func (h *historyTerminal) History() (terminal.History, error) { return h.history, nil }
func (h *historyTerminal) ScrollToLine(line uint64) bool {
	if h.snapshot.AlternateScreen || line < h.history.FirstLine || line-h.history.FirstLine >= uint64(len(h.history.Lines)) {
		return false
	}
	h.jumps = append(h.jumps, line)
	screen := h.snapshot.FirstLine + uint64(h.snapshot.ScrollbackLen)
	h.snapshot.ScrollOffset = 0
	if line < screen {
		h.snapshot.ScrollOffset = int(screen - line)
	}
	h.snapshot.Revision++
	return true
}
func asciiHistoryLine(id uint64, text string) terminal.TextLine {
	columns := make([]uint16, len([]rune(text))+1)
	for i := range columns {
		columns[i] = uint16(i)
	}
	return terminal.TextLine{ID: id, Text: text, Columns: columns}
}
func historyProvider(t *testing.T) (*Provider, *historyTerminal) {
	t.Helper()
	backend := &historyTerminal{fakeTerminal: fakeTerminal{snapshot: testTerminalSnapshot(70, 8)}}
	backend.snapshot.FirstLine = 100
	backend.snapshot.ScrollbackLen = 12
	backend.snapshot.Cursor.Row = 4
	backend.history.FirstLine = 100
	for i := 0; i < 20; i++ {
		backend.history.Lines = append(backend.history.Lines, asciiHistoryLine(uint64(100+i), fmt.Sprintf("line %d", i)))
	}
	backend.history.Lines[5] = asciiHistoryLine(105, "old Needle here")
	backend.history.Lines[14] = asciiHistoryLine(114, "new needle here")
	p, err := newProvider(backend, 70, 8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	p.Focus(1)
	return p, backend
}
func toolKey(t *testing.T, p *Provider, code uint32, mods experience.Modifiers) {
	t.Helper()
	for _, pressed := range []bool{true, false} {
		p.Send(1, experience.Event{Kind: experience.KeyInput, Keycode: code, Modifiers: mods, Pressed: pressed})
	}
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
}

func TestFindOwnsTextAndReleaseWithoutSendingQueryToPTY(t *testing.T) {
	p, backend := historyProvider(t)
	toolKey(t, p, 33, experience.ModControl|experience.ModShift)
	if p.tools.mode != "find" || backend.focused {
		t.Fatal("find did not take keyboard ownership from PTY")
	}
	for _, code := range []uint32{49, 18, 18, 32, 38, 18} {
		toolKey(t, p, code, 0)
	}
	if p.tools.query != "needle" || len(p.tools.matches) != 2 {
		t.Fatalf("query/matches: %q %+v", p.tools.query, p.tools.matches)
	}
	if len(backend.input) != 0 {
		t.Fatalf("find leaked physical keys into shell: %+v", backend.input)
	}
	if got := backend.jumps[len(backend.jumps)-1]; got != 114 {
		t.Fatalf("newest match = %d", got)
	}
	toolKey(t, p, 28, 0)
	if got := backend.jumps[len(backend.jumps)-1]; got != 105 {
		t.Fatalf("next did not wrap: %d", got)
	}
	toolKey(t, p, 28, experience.ModShift)
	if got := backend.jumps[len(backend.jumps)-1]; got != 114 {
		t.Fatalf("previous did not wrap: %d", got)
	}
	toolKey(t, p, 46, experience.ModControl|experience.ModShift)
	if text, ok := p.TakeCopy(); !ok || text != "new needle here" {
		t.Fatalf("match copy = %q %v", text, ok)
	}
	toolKey(t, p, 30, experience.ModControl)
	if err := p.Paste("Needle\n"); err != nil {
		t.Fatal(err)
	}
	if p.tools.query != "Needle" || len(backend.pasted) != 0 {
		t.Fatal("find paste escaped into PTY")
	}
	toolKey(t, p, 1, 0)
	if p.tools.mode != "" || !backend.focused || len(backend.input) != 0 {
		t.Fatal("Escape/release leaked into shell or failed to restore focus")
	}
	toolKey(t, p, 30, 0)
	if len(backend.input) != 2 || backend.input[0].Keycode != 30 {
		t.Fatal("ordinary PTY keyboard did not resume")
	}
}

func TestSearchPreservesUnicodeScreenColumnsAndCapsMatches(t *testing.T) {
	line := terminal.TextLine{ID: 8, Text: "界é界", Columns: []uint16{0, 2, 2, 3, 5}}
	hits := searchLines([]terminal.TextLine{line}, "界")
	if len(hits) != 2 || hits[0] != (textMatch{8, 0, 2}) || hits[1] != (textMatch{8, 3, 5}) {
		t.Fatalf("wide glyph matches: %+v", hits)
	}
	hits = searchLines([]terminal.TextLine{line}, "é")
	if len(hits) != 1 || hits[0] != (textMatch{8, 2, 3}) {
		t.Fatalf("combining match: %+v", hits)
	}
	line = asciiHistoryLine(1, strings.Repeat("x", maxSearchMatches+10))
	if got := len(searchLines([]terminal.TextLine{line}, "x")); got != maxSearchMatches {
		t.Fatalf("unbounded matches: %d", got)
	}
}

func TestClosingFindCancelsOutstandingClipboardDestination(t *testing.T) {
	p, backend := historyProvider(t)
	toolKey(t, p, 33, experience.ModControl|experience.ModShift)
	toolKey(t, p, 47, experience.ModControl|experience.ModShift)
	if !p.TakePasteRequest() {
		t.Fatal("find did not request clipboard text")
	}
	toolKey(t, p, 1, 0)
	if err := p.Paste("must not become shell input"); err != nil {
		t.Fatal(err)
	}
	if len(backend.pasted) != 0 {
		t.Fatal("late find clipboard reply escaped into PTY")
	}
}

func TestTerminalToolbarUsesSharedCaptureAndShapedFieldSemantics(t *testing.T) {
	p, backend := historyProvider(t)
	node := terminalToolbar(p.renderer.image.Rect.Dx())[0]
	press := experience.Event{Kind: experience.PointerDown, Button: experience.ButtonPrimary, X: float32(node.Bounds.Min.X + 3), Y: 35}
	p.Send(1, press)
	if p.tools.mode != "" || backend.focused {
		t.Fatal("toolbar activated on press or retained PTY focus")
	}
	move := press
	move.Kind = experience.PointerMove
	move.Y = contentTop + 10
	p.Send(1, move)
	if len(backend.input) != 0 {
		t.Fatal("captured toolbar drag reached PTY")
	}
	release := press
	release.Kind = experience.PointerUp
	p.Send(1, release)
	if p.tools.mode != "find" {
		t.Fatal("toolbar release did not open find")
	}
	if err := p.Paste("é👩‍💻"); err != nil {
		t.Fatal(err)
	}
	toolKey(t, p, 14, 0)
	if p.tools.query != "é" {
		t.Fatalf("find deletion split grapheme: %q", p.tools.query)
	}
	tree := p.Semantics()
	if len(tree.Nodes) != 1 || tree.Nodes[0].Role != nativeui.RoleTextField || tree.Nodes[0].Value != "é" || tree.FocusedID != "find-field" {
		t.Fatalf("find semantics = %+v", tree)
	}
	p.Focus(0)
	if p.Semantics().FocusedID != "" {
		t.Fatal("unfocused terminal advertised focused control")
	}
}

func TestFindIMECommitPreeditAndFocusEpochNeverReachPTY(t *testing.T) {
	p, backend := historyProvider(t)
	toolKey(t, p, 33, experience.ModControl|experience.ModShift)
	state := p.TextInput(1)
	if !state.Enabled {
		t.Fatal("focused find did not enable IME")
	}
	p.Send(1, experience.Event{Kind: experience.TextPreedit, Text: "かな", PreeditBegin: 3, PreeditEnd: 6, TextContext: state.ContextID})
	if text, _, _ := p.tools.field.Preedit(); text != "かな" || p.tools.query != "" {
		t.Fatal("find lost transient preedit or searched it as committed text")
	}
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	p.Send(1, experience.Event{Kind: experience.TextCommit, Text: "needle", TextContext: state.ContextID})
	if p.tools.query != "needle" || len(p.tools.matches) != 2 || len(backend.input) != 0 || len(backend.pasted) != 0 {
		t.Fatal("IME query was not searchable or leaked into PTY")
	}
	p.Focus(0)
	p.Focus(1)
	if p.TextInput(1).ContextID == state.ContextID {
		t.Fatal("focus lifetime reused IME context")
	}
	p.Send(1, experience.Event{Kind: experience.TextCommit, Text: "stale", TextContext: state.ContextID})
	if p.tools.query != "needle" {
		t.Fatal("old-context IME text reached refocused find")
	}
	toolKey(t, p, 1, 0)
	if p.TextInput(1).Enabled {
		t.Fatal("closed find advertised an editable shell buffer")
	}
}

func TestBookmarksNavigateAndNeverReuseExpiredHistory(t *testing.T) {
	p, backend := historyProvider(t)
	toolKey(t, p, 50, experience.ModControl|experience.ModShift)
	if len(p.tools.bookmarks) != 1 || p.tools.bookmarks[0].line != 116 {
		t.Fatalf("wrong bookmarked row: %+v", p.tools.bookmarks)
	}
	toolKey(t, p, 48, experience.ModControl|experience.ModShift)
	if p.tools.mode != "bookmarks" || len(p.tools.rows) != 1 {
		t.Fatal("bookmarks UI did not open")
	}
	toolKey(t, p, 28, 0)
	if p.tools.mode != "" || len(backend.jumps) != 1 || backend.jumps[0] != 116 {
		t.Fatal("bookmark did not navigate to retained row")
	}
	backend.history = terminal.History{FirstLine: 200, Lines: []terminal.TextLine{asciiHistoryLine(200, "replacement")}}
	backend.snapshot.FirstLine = 200
	backend.snapshot.ScrollbackLen = 0
	backend.snapshot.Revision++
	toolKey(t, p, 48, experience.ModControl|experience.ModShift)
	toolKey(t, p, 28, 0)
	if p.tools.mode != "bookmarks" || !strings.Contains(p.tools.message, "expired") || len(backend.jumps) != 1 {
		t.Fatal("expired bookmark changed history position")
	}
	toolKey(t, p, 111, 0)
	if len(p.tools.bookmarks) != 0 {
		t.Fatal("bookmark deletion failed")
	}
}

func TestCommandBlocksCollapseCopyAndPinIndependentResults(t *testing.T) {
	p, backend := historyProvider(t)
	backend.history.Lines[0] = asciiHistoryLine(100, "$ printf result")
	backend.history.Lines[1] = asciiHistoryLine(101, "result one")
	backend.history.Lines[2] = asciiHistoryLine(102, "two$ NEXT PROMPT")
	backend.history.Commands = []terminal.CommandBlock{{ID: 1, CommandLine: 100, CommandColumn: 2, OutputLine: 101, EndLine: 102, EndColumn: 3, Started: true, Finished: true, Status: 7}}
	toolKey(t, p, 37, experience.ModControl|experience.ModShift)
	if len(p.tools.rows) != 1 || !strings.Contains(p.tools.rows[0].text, "[exit 7] printf result") {
		t.Fatalf("command header: %+v", p.tools.rows)
	}
	toolKey(t, p, 28, 0)
	if len(p.tools.rows) != 3 || p.tools.rows[1].text != "  result one" || p.tools.rows[2].text != "  two" {
		t.Fatalf("expanded output: %+v", p.tools.rows)
	}
	toolKey(t, p, 46, experience.ModControl|experience.ModShift)
	if text, ok := p.TakeCopy(); !ok || text != "result one\ntwo" {
		t.Fatalf("copied prompt or missed result: %q %v", text, ok)
	}
	toolKey(t, p, 25, 0)
	if len(p.tools.pins) != 1 || p.tools.pins[0].text != "result one\ntwo" {
		t.Fatal("command result was not pinned")
	}
	toolKey(t, p, 28, 0)
	if len(p.tools.rows) != 1 {
		t.Fatal("command did not collapse")
	}
	backend.history = terminal.History{FirstLine: 999, Lines: []terminal.TextLine{asciiHistoryLine(999, "replacement")}}
	backend.snapshot.FirstLine = 999
	backend.snapshot.Revision++
	toolKey(t, p, 25, experience.ModControl|experience.ModShift)
	toolKey(t, p, 28, 0)
	if len(p.tools.rows) != 3 || p.tools.rows[1].text != "  result one" {
		t.Fatal("pin changed when backing history expired")
	}
	toolKey(t, p, 46, experience.ModControl|experience.ModShift)
	if text, ok := p.TakeCopy(); !ok || text != "result one\ntwo" {
		t.Fatal("pinned result copy failed")
	}
	toolKey(t, p, 111, 0)
	if len(p.tools.pins) != 0 {
		t.Fatal("pin deletion failed")
	}
}

func TestHistoryToolsPreserveAlternateScreenAndExitedOutput(t *testing.T) {
	p, backend := historyProvider(t)
	backend.snapshot.AlternateScreen = true
	backend.snapshot.Revision++
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	toolKey(t, p, 33, experience.ModControl|experience.ModShift)
	if p.tools.mode != "" || !backend.focused {
		t.Fatal("find took over a full-screen program")
	}
	toolKey(t, p, 30, 0)
	if len(backend.input) != 2 {
		t.Fatal("full-screen app lost ordinary physical input")
	}
	backend.snapshot.AlternateScreen = false
	backend.snapshot.Exited = true
	backend.snapshot.Revision++
	if err := p.Poll(); err != nil {
		t.Fatal(err)
	}
	toolKey(t, p, 33, experience.ModControl|experience.ModShift)
	if err := p.Paste("needle"); err != nil {
		t.Fatal(err)
	}
	if len(p.tools.matches) != 2 || len(backend.pasted) != 0 {
		t.Fatal("exited shell output could not be searched")
	}
}

func TestCommandIntegrationIsOptInAndRetainedDataIsBounded(t *testing.T) {
	p, backend := historyProvider(t)
	toolKey(t, p, 37, experience.ModControl|experience.ModShift)
	toolKey(t, p, 23, 0)
	text, ok := p.TakeCopy()
	if !ok || text != terminal.BashIntegration || len(backend.pasted) != 0 || len(backend.input) != 0 {
		t.Fatal("integration setup executed rather than copied")
	}
	toolKey(t, p, 1, 0)
	for i := 0; i < maxPins+3; i++ {
		p.pinResult()
	}
	if len(p.tools.pins) != maxPins {
		t.Fatal("pins are not bounded")
	}
	for i := 0; i < maxBookmarks+10; i++ {
		backend.snapshot.FirstLine = uint64(1000 + i)
		backend.snapshot.Revision++
		p.snapshot = backend.snapshot
		p.bookmark()
	}
	if len(p.tools.bookmarks) != maxBookmarks {
		t.Fatal("bookmarks are not bounded")
	}
	for _, limit := range []int{3, 4, 5, 6} {
		h := terminal.History{Lines: []terminal.TextLine{asciiHistoryLine(0, "€€")}}
		if text := textRange(h, 0, 0, 0, 9, limit); !utf8.ValidString(text) {
			t.Fatalf("truncation split UTF-8: %q", text)
		}
	}
}

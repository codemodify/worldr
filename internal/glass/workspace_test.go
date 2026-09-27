//go:build linux && cgo

package glass

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/glass/commands"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/terminal"
)

func workspaceBash(t *testing.T) *App {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("Bash unavailable")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".bashrc"), []byte("PS1='worldr-test> '\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	a, err := New(terminal.Options{Command: bash, Directory: file, Env: []string{"HOME=" + dir, "PATH=/usr/bin:/bin", "LC_ALL=C.UTF-8"}, Cols: 100, Rows: 16, Scrollback: 100}, DefaultPreferences())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	a.Draw(1100, 720)
	waitApp(t, a, func() bool { return strings.Contains(paneText(a.pane().snapshot), "worldr-test>") })
	return a
}

func workspaceRun(t *testing.T, a *App, command string, count int) {
	t.Helper()
	if err := a.pane().Paste(command); err != nil {
		t.Fatal(err)
	}
	appKey(a, 28, experience.KeyEnter, 0)
	waitApp(t, a, func() bool {
		h, err := a.pane().term.History()
		if err != nil {
			t.Fatal(err)
		}
		cards := commands.FromHistory(h)
		return len(cards) == count && cards[count-1].Finished
	})
}

func TestWorkspaceCardsComeFromRealCommandsAndLocateReturnsToLive(t *testing.T) {
	a := workspaceBash(t)
	if !a.integrated {
		t.Fatal("default Bash launch was not integrated")
	}
	workspaceRun(t, a, "export WORLDR_TEST=preserved; printf 'ALPHA needle\\nβeta café\\n'; false", 1)
	a.setView(true)
	waitApp(t, a, func() bool { return len(a.cards) == 1 })
	c := a.cards[0]
	if c.Status != 1 || c.Output != "ALPHA needle\nβeta café\n" || c.Running || c.Truncated || !strings.HasPrefix(c.Command, "export WORLDR_TEST=") {
		t.Fatalf("real command card = %+v", c)
	}
	a.Draw(1100, 720)
	clickAppButton(t, a, fmt.Sprintf("block:toggle:%d", c.ID))
	if !a.expanded[c.ID] {
		t.Fatal("Expand did not expand the selected real card")
	}
	a.Draw(1100, 720)
	clickAppButton(t, a, fmt.Sprintf("block:copy:%d", c.ID))
	if a.selectedCard != c.ID || a.cards[0].Output != c.Output || !a.historyView {
		t.Fatal("Copy changed the card or shell mode")
	}
	clickAppButton(t, a, fmt.Sprintf("block:jump:%d", c.ID))
	if a.historyView || a.searchOpen {
		t.Fatal("Locate did not return to Live")
	}
	workspaceRun(t, a, "printf '%s\\n' \"$WORLDR_TEST\"", 2)
	a.setView(true)
	waitApp(t, a, func() bool { return len(a.cards) == 2 })
	if a.cards[1].Output != "preserved\n" {
		t.Fatalf("return to Live lost focus or shell context: %+v", a.cards[1])
	}
	path := a.options.Args[1]
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary integration survived app close: %v", err)
	}
}

func TestWorkspaceSearchAndHistoryKeysNeverReachPTY(t *testing.T) {
	a := testApp(t, "stty raw -echo; printf 'READY ALPHA alpha café\r\n'; dd bs=1 count=1 2>/dev/null | od -An -tx1")
	waitApp(t, a, func() bool { return strings.Contains(paneText(a.pane().snapshot), "READY") })
	appKey(a, 33, experience.Key("F"), experience.ModControl|experience.ModShift)
	if !a.searchOpen {
		t.Fatal("search shortcut did not open Find")
	}
	for _, code := range []uint32{30, 38, 25, 35, 30} { // alpha, via actual XKB translation.
		appKey(a, code, "", 0)
	}
	if a.searchText != "alpha" || len(a.searchResults.Matches) != 2 {
		t.Fatalf("typed case-insensitive search = %q, %+v", a.searchText, a.searchResults)
	}
	appKey(a, 28, experience.KeyEnter, 0)
	appKey(a, 28, experience.KeyEnter, experience.ModShift)
	appKey(a, 30, experience.Key("A"), experience.ModControl)
	a.insertSearch("CAFÉ\n\t") // Same edit path as a clipboard delivery; strip controls.
	if a.searchText != "CAFÉ" || len(a.searchResults.Matches) != 1 {
		t.Fatalf("Unicode search = %q, %+v", a.searchText, a.searchResults)
	}
	appKey(a, 105, experience.KeyLeft, 0)
	appKey(a, 14, experience.Key("Backspace"), 0)
	if a.searchText != "CAÉ" {
		t.Fatalf("Unicode edit used byte offsets: %q", a.searchText)
	}
	appKey(a, 1, experience.KeyEscape, 0)
	appKey(a, 35, experience.Key("H"), experience.ModControl|experience.ModShift)
	if !a.historyView || a.searchOpen {
		t.Fatal("history shortcut did not replace Find")
	}
	appKey(a, 30, experience.Key("A"), 0)
	appKey(a, 28, experience.KeyEnter, 0)
	appKey(a, 1, experience.KeyEscape, 0)
	appKey(a, 30, experience.Key("A"), 0)
	waitApp(t, a, func() bool { return a.pane().snapshot.Exited })
	text := strings.Join(strings.Fields(paneText(a.pane().snapshot)), " ")
	if !strings.HasSuffix(text, "61") {
		t.Fatalf("modal input leaked before the final live a: %q", text)
	}
	if len(a.owned) != 0 {
		t.Fatalf("shortcut releases remained owned: %+v", a.owned)
	}
}

func TestWorkspaceRunningCardFinishesAfterLiveInteractiveInput(t *testing.T) {
	a := workspaceBash(t)
	if err := a.pane().Paste("printf 'WAITING\\n'; IFS= read -r answer; printf 'ANSWER:%s\\n' \"$answer\""); err != nil {
		t.Fatal(err)
	}
	appKey(a, 28, experience.KeyEnter, 0)
	waitApp(t, a, func() bool { return strings.Contains(paneText(a.pane().snapshot), "\nWAITING") })
	a.setView(true)
	waitApp(t, a, func() bool { return len(a.cards) == 1 })
	if !a.cards[0].Running || a.cards[0].Finished || a.cards[0].Status != -1 || a.cards[0].Output != "WAITING" {
		t.Fatalf("active interactive command was misreported: %+v", a.cards[0])
	}
	a.setView(false)
	if err := a.pane().Paste("live input"); err != nil {
		t.Fatal(err)
	}
	appKey(a, 28, experience.KeyEnter, 0)
	a.setView(true)
	waitApp(t, a, func() bool { return len(a.cards) == 1 && a.cards[0].Finished })
	if a.cards[0].Status != 0 || !strings.Contains(a.cards[0].Output, "ANSWER:live input") {
		t.Fatalf("interactive completion lost shell input: %+v", a.cards[0])
	}
}

func TestWorkspaceSwitchingTabsDropsPreviousSearchHighlights(t *testing.T) {
	a := testApp(t, "printf 'OLD needle READY'; IFS= read -r next")
	waitApp(t, a, func() bool { return strings.Contains(paneText(a.pane().snapshot), "READY") })
	a.setSearch(true)
	a.insertSearch("needle")
	if len(a.searchRows) == 0 {
		t.Fatal("fixture did not populate highlights")
	}
	if err := a.addTab(); err != nil {
		t.Fatal(err)
	}
	if len(a.searchRows) != 0 || len(a.searchResults.Matches) != 0 || a.searchRevision != 0 || a.searchPoll != 0 || a.historyPoll != 0 {
		t.Fatalf("old tab search survived switch: rows=%+v results=%+v poll=%v/%v", a.searchRows, a.searchResults, a.searchPoll, a.historyPoll)
	}
	if !a.searchOpen {
		t.Fatal("tab switching unexpectedly closed Find")
	}
	// Closing Find must refocus the new tab immediately for clipboard input.
	a.setSearch(false)
	if err := a.pane().Paste("new tab input\n"); err != nil {
		t.Fatal(err)
	}
	waitApp(t, a, func() bool { return a.pane().snapshot.Exited })
}

func TestWorkspaceDynamicControlsStayWithinContentBounds(t *testing.T) {
	a := testApp(t, "printf READY; IFS= read -r next")
	waitApp(t, a, func() bool { return strings.Contains(paneText(a.pane().snapshot), "READY") })
	for _, size := range [][2]int{{640, 420}, {1100, 720}} {
		for _, mode := range []string{"empty-integrated", "empty-custom", "cards", "scrolled-cards", "search"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], mode), func(t *testing.T) {
				a.historyScroll, a.historyTarget = 0, 0
				a.integrated = mode != "empty-custom"
				a.setView(true)
				a.cards = nil
				if mode == "cards" || mode == "scrolled-cards" {
					a.cards = []commands.Card{{ID: 1, Command: "printf 'one\\n'", Output: "one\n", Finished: true}, {ID: 2, Command: "echo two", Output: "two", Running: true, Status: -1}}
					if mode == "scrolled-cards" {
						a.historyScroll, a.historyTarget = 53, 53
					}
				}
				if mode == "search" {
					a.setSearch(true)
				}
				a.Draw(size[0], size[1])
				for _, b := range a.buttons {
					dynamic := strings.HasPrefix(b.id, "block:") || strings.HasPrefix(b.id, "search:") || b.id == "integration" || b.id == "live" && b.label == "Back to Live"
					if !dynamic {
						continue
					}
					w, r := a.workspace, b.box
					if r.w <= 0 || r.h <= 0 || r.x < w.x || r.y < w.y || r.x+r.w > w.x+w.w+.01 || r.y+r.h > w.y+w.h+.01 {
						t.Errorf("dynamic control %q escapes workspace: %+v, workspace %+v", b.id, r, w)
					}
				}
			})
		}
	}
	if err := a.Update(time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

// Opt-in visual artifacts use only this test's isolated Bash session. They do
// not seed commands or output into a user's interactive application.
func TestCaptureWorkspaceGPU(t *testing.T) {
	directory := os.Getenv("WORLDR_CAPTURE_WORKSPACE")
	if directory == "" {
		t.Skip("set WORLDR_CAPTURE_WORKSPACE to save real-shell GPU screenshots")
	}
	a := workspaceBash(t)
	a.prefs.Motion = false
	workspaceRun(t, a, "printf 'Session directory\\n'; pwd", 1)
	workspaceRun(t, a, "for i in 1 2 3 4 5 6; do printf 'sample %02d\\n' \"$i\"; done; false", 2)
	workspaceRun(t, a, "printf 'Search target: TERMINAL\\nSearch target: terminal\\nUnicode: café\\n'", 3)
	a.setView(true)
	waitApp(t, a, func() bool { return len(a.cards) == 3 })
	capture := func(name string, width, height int) {
		t.Helper()
		a.Draw(width, height)
		for i := 0; i < 30; i++ {
			if err := a.Update(10 * time.Millisecond); err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Millisecond)
		}
		gpu, err := native.OpenVK(false, uint32(width), uint32(height))
		if err != nil {
			t.Fatal(err)
		}
		defer gpu.Close()
		if err := gpu.SetSceneAtlas(a.Atlas()); err != nil {
			t.Fatal(err)
		}
		pixels := make([]byte, width*height*4)
		if err := gpu.RenderFrame(a.Draw(width, height), [4]float32{}, pixels); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		for i := 0; i < len(pixels); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = pixels[i+2], pixels[i+1], pixels[i], pixels[i+3]
		}
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		encodeErr := png.Encode(file, img)
		closeErr := file.Close()
		if encodeErr != nil || closeErr != nil {
			t.Fatalf("save capture: %v, %v", encodeErr, closeErr)
		}
	}
	capture("blocks-wide.png", 1180, 760)
	capture("blocks-compact.png", 640, 420)
	a.setSearch(true)
	a.insertSearch("terminal")
	capture("find-wide.png", 1180, 760)
	capture("find-compact.png", 640, 420)
}

//go:build linux && cgo

package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/terminal"
)

func TestPreparedBashRecordsRealCommandsWithoutReplacingShellContext(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("Bash unavailable")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "work"), 0700); err != nil {
		t.Fatal(err)
	}
	rcfile := filepath.Join(dir, ".bashrc")
	original := "PS1='READY> '\nPROMPT_COMMAND=\"printf 'HOOK\\\\n'\"\nexport FROM_USER_RC=preserved\n"
	if err := os.WriteFile(rcfile, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	options, cleanup, integrated, err := Prepare(terminal.Options{Command: bash, Directory: file, Env: []string{"HOME=" + dir, "PATH=/usr/bin:/bin", "LC_ALL=C.UTF-8"}, Cols: 120, Rows: 24, Scrollback: 100})
	if err != nil || !integrated {
		t.Fatalf("prepare = %v, %v", integrated, err)
	}
	defer cleanup()
	term, err := terminal.Open(options)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	term.Focus(true)
	history := awaitHistory(t, term, func(h terminal.History) bool {
		for _, l := range h.Lines {
			if strings.Contains(l.Text, "READY>") {
				return true
			}
		}
		return false
	})
	if cards := FromHistory(history); len(cards) != 0 {
		t.Fatalf("startup ran an unsolicited command: %+v", cards)
	}
	run := func(command string, want int) []Card {
		t.Helper()
		if err := term.Paste(command); err != nil {
			t.Fatal(err)
		}
		for _, down := range []bool{true, false} {
			if err := term.Input(experience.Event{Kind: experience.KeyInput, Keycode: 28, Pressed: down}); err != nil {
				t.Fatal(err)
			}
		}
		h := awaitHistory(t, term, func(h terminal.History) bool {
			cards := FromHistory(h)
			return len(cards) == want && cards[want-1].Finished
		})
		return FromHistory(h)
	}
	cards := run("cd work; export WORLD_R=held; f(){ printf 'FUNC-ok\\n'; }; printf 'first\\n'; false", 1)
	if cards[0].Status != 1 || cards[0].Output != "first\n" || !strings.Contains(cards[0].Command, "cd work;") {
		h, _ := term.History()
		t.Fatalf("first real command: %+v; history=%+v", cards[0], h)
	}
	cards = run("printf '%s:%s:%s\\n' \"$WORLD_R\" \"${PWD##*/}\" \"$FROM_USER_RC\"; f", 2)
	if cards[1].Status != 0 || cards[1].Output != "held:work:preserved\nFUNC-ok\n" {
		t.Fatalf("shell state was not preserved: %+v", cards[1])
	}
	cards = run("{\nprintf '界é\\n'\nprintf 'second\\n'\n}", 3)
	if cards[2].Status != 0 || cards[2].Output != "界é\nsecond\n" || !strings.Contains(cards[2].Command, "printf 'second") {
		t.Fatalf("multiline command: %+v", cards[2])
	}
	if content, err := os.ReadFile(rcfile); err != nil || string(content) != original {
		t.Fatalf("user rcfile changed: %q, %v", content, err)
	}
}

func awaitHistory(t *testing.T, term *terminal.Terminal, ready func(terminal.History) bool) terminal.History {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var history terminal.History
	for time.Now().Before(deadline) {
		if _, err := term.Poll(); err != nil {
			t.Fatal(err)
		}
		var err error
		history, err = term.History()
		if err != nil {
			t.Fatal(err)
		}
		if ready(history) {
			return history
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("shell did not reach expected state: cards=%+v, history=%+v", FromHistory(history), history)
	return history
}

//go:build linux && cgo

package terminal

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestHistoryRetainsUnicodeAndStableIDsAcrossEviction(t *testing.T) {
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "printf 'first\\r\\n界é tail\\r\\nthird\\r\\nready'; IFS= read -r x; i=0; while [ $i -lt 20 ]; do printf '\\r\\nline-%s' \"$i\"; i=$((i+1)); done; printf '\\r\\ndone'; IFS= read -r x"}, Cols: 30, Rows: 3, Scrollback: 4})
	first := pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "ready") })
	h, err := term.History()
	if err != nil {
		t.Fatal(err)
	}
	var unicodeLine TextLine
	for _, line := range h.Lines {
		if strings.Contains(line.Text, "界") {
			unicodeLine = line
		}
	}
	if unicodeLine.Text != "界é tail" || len(unicodeLine.Columns) != len([]rune(unicodeLine.Text))+1 {
		t.Fatalf("history lost text/columns: %+v", unicodeLine)
	}
	if unicodeLine.Columns[0] != 0 || unicodeLine.Columns[1] != 2 || unicodeLine.Columns[2] != 2 || unicodeLine.Columns[3] != 3 {
		t.Fatalf("wrong wide/combining columns: %v", unicodeLine.Columns)
	}
	if !term.ScrollToLine(h.FirstLine) {
		t.Fatal("retained first line was not scrollable")
	}
	scrolled, err := term.Poll()
	if err != nil || scrolled.ScrollOffset == 0 || !strings.Contains(screenText(scrolled), "first") {
		t.Fatal("did not scroll into history", err)
	}
	old := h.Lines[0].Text
	if err := term.Paste("\n"); err != nil {
		t.Fatal(err)
	}
	last := pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "done") })
	if last.FirstLine <= first.FirstLine || last.ScrollbackLen != 4 {
		t.Fatalf("ring did not advance stable IDs: %+v", last)
	}
	if term.ScrollToLine(unicodeLine.ID) {
		t.Fatal("expired bookmark jumped to reused ring row")
	}
	if h.Lines[0].Text != old {
		t.Fatal("later output mutated retained history snapshot")
	}
	newHistory, _ := term.History()
	for i, line := range newHistory.Lines {
		if line.ID != newHistory.FirstLine+uint64(i) {
			t.Fatal("noncontiguous identities")
		}
	}
}

func TestOSC133BoundariesKeepPromptOutOfCommandOutput(t *testing.T) {
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "printf '\033]133;B\007printf alpha\r\n\033]133;C\007alpha\r\nbeta\033]133;D;7\007PROMPT'; IFS= read -r x"}, Cols: 40, Rows: 8})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "PROMPT") })
	h, err := term.History()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Commands) != 1 {
		t.Fatalf("OSC command not recognized: %+v", h.Commands)
	}
	c := h.Commands[0]
	if !c.Started || !c.Finished || c.Status != 7 || c.CommandLine != 0 || c.CommandColumn != 0 || c.OutputLine != 1 || c.OutputColumn != 0 || c.EndLine != 2 || c.EndColumn != 4 {
		t.Fatalf("wrong command boundaries: %+v", c)
	}
}

func TestHistoryToolsCannotScrollAlternateScreen(t *testing.T) {
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "printf 'main-1\r\nmain-2\r\nmain-3\r\nmain-ready'; IFS= read -r x; printf '\033[?1049h\033[HAPP'; IFS= read -r x; printf '\033[?1049l\r\nrestored'; IFS= read -r x"}, Cols: 30, Rows: 3})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "main-ready") })
	if err := term.Paste("\n"); err != nil {
		t.Fatal(err)
	}
	alt := pollTerminal(t, term, func(s Snapshot) bool { return s.AlternateScreen && strings.Contains(screenText(s), "APP") })
	term.Scroll(100)
	if term.ScrollToLine(alt.FirstLine) {
		t.Fatal("history navigation changed alternate screen")
	}
	s, _ := term.Poll()
	if s.ScrollOffset != 0 || !s.AlternateScreen || !strings.Contains(screenText(s), "APP") {
		t.Fatal("alternate screen was displaced")
	}
	h, _ := term.History()
	for _, line := range h.Lines {
		if strings.Contains(line.Text, "APP") {
			t.Fatal("alternate app leaked into primary history")
		}
	}
	if err := term.Paste("\n"); err != nil {
		t.Fatal(err)
	}
	pollTerminal(t, term, func(s Snapshot) bool { return !s.AlternateScreen && strings.Contains(screenText(s), "restored") })
}

func TestOptionalBashIntegrationPreservesHooksAndCapturesRealCommand(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	term := openTestTerminal(t, Options{Command: bash, Args: []string{"--noprofile", "--norc", "-i"}, Env: append(os.Environ(), "PS1=READY> ", "PROMPT_COMMAND=printf HOOK"), Cols: 100, Rows: 12})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "READY>") })
	if err := term.Paste(BashIntegration + "\n"); err != nil {
		t.Fatal(err)
	}
	key(t, term, 28, 0)
	pollTerminal(t, term, func(s Snapshot) bool { h, _ := term.History(); return len(h.Commands) > 0 })
	if err := term.Paste("printf 'RESULT-value\\n'; false\n"); err != nil {
		t.Fatal(err)
	}
	key(t, term, 28, 0)
	pollTerminal(t, term, func(s Snapshot) bool {
		h, _ := term.History()
		for _, c := range h.Commands {
			if c.Finished && c.Status == 1 {
				return true
			}
		}
		return false
	})
	h, _ := term.History()
	var found bool
	for _, c := range h.Commands {
		if c.Finished && c.Status == 1 {
			found = true
			if c.OutputLine <= c.CommandLine {
				t.Fatalf("command/output were not separated: %+v", c)
			}
			if !strings.Contains(h.Lines[c.OutputLine-h.FirstLine].Text, "RESULT-value") {
				t.Fatalf("missing real command output: %+v", h.Lines[c.OutputLine-h.FirstLine])
			}
		}
	}
	if !found {
		t.Fatal("real command exit status missing")
	}
	s, _ := term.Poll()
	if strings.Count(screenText(s), "HOOK") < 2 {
		t.Fatal("existing PROMPT_COMMAND hook was replaced")
	}
}

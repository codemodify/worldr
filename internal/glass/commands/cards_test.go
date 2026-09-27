package commands

import (
	"reflect"
	"testing"

	"github.com/codemodify/worldr/internal/terminal"
)

func line(id uint64, text string) terminal.TextLine {
	columns := make([]uint16, len([]rune(text))+1)
	for i := range columns {
		columns[i] = uint16(i)
	}
	return terminal.TextLine{ID: id, Text: text, Columns: columns}
}

func TestCardsRespectBoundariesAndPreserveMultilineOutput(t *testing.T) {
	history := terminal.History{Lines: []terminal.TextLine{
		line(0, "$ printf 'one\\n'"), line(1, "> printf 'two\\n'"),
		line(2, "one"), line(3, "two$ next prompt"),
	}, Commands: []terminal.CommandBlock{
		{ID: 7, CommandLine: 0, CommandColumn: 2, OutputLine: 2, EndLine: 3, EndColumn: 3, Started: true, Finished: true, Status: 17},
		{ID: 8, CommandLine: 3, CommandColumn: 5}, // Editable input is not a run.
	}}
	cards := FromHistory(history)
	if len(cards) != 1 {
		t.Fatalf("cards = %+v", cards)
	}
	c := cards[0]
	if c.Command != "printf 'one\\n'\n> printf 'two\\n'" || c.Output != "one\ntwo" || c.Status != 17 || c.Running || !c.Finished || c.Truncated {
		t.Fatalf("wrong boundaries: %+v", c)
	}
	before := cards[0]
	history.Lines[2].Text = "changed"
	history.Commands[0].Status = 99
	if !reflect.DeepEqual(cards[0], before) {
		t.Fatal("immutable card changed with source")
	}
}

func TestColumnSlicesPreserveWideAndCombiningGlyphs(t *testing.T) {
	l := terminal.TextLine{ID: 3, Text: "$ 界e\u0301!PROMPT", Columns: []uint16{0, 1, 2, 4, 4, 5, 6, 7, 8, 9, 10, 11, 12}}
	text, truncated := textRange([]terminal.TextLine{l}, 3, 2, 3, 6)
	if text != "界e\u0301!" || truncated {
		t.Fatalf("unicode slice %q, truncated %v", text, truncated)
	}
	text, _ = textRange([]terminal.TextLine{l}, 3, 2, 3, 3)
	if text != "" {
		t.Fatalf("split wide cell returned %q", text)
	}
}

func TestCardsDropExpiredCommandsAndFlagRetainedRemainders(t *testing.T) {
	h := terminal.History{FirstLine: 5, Lines: []terminal.TextLine{line(5, "remaining"), line(6, "done"), line(7, "$")}, Commands: []terminal.CommandBlock{
		{ID: 1, CommandLine: 0, OutputLine: 1, EndLine: 4, EndColumn: 3, Started: true, Finished: true},
		{ID: 2, CommandLine: 2, OutputLine: 3, EndLine: 5, EndColumn: 0, Started: true, Finished: true},
		{ID: 3, CommandLine: 3, OutputLine: 4, EndLine: 6, EndColumn: 4, Started: true, Finished: true},
	}}
	cards := FromHistory(h)
	if len(cards) != 1 || cards[0].ID != 3 || cards[0].Command != "" || cards[0].Output != "remaining\ndone" || !cards[0].CommandTruncated || !cards[0].OutputTruncated || !cards[0].Truncated {
		t.Fatalf("eviction = %+v", cards)
	}
}

func TestRunningCardContainsOnlyAvailableOutput(t *testing.T) {
	h := terminal.History{Lines: []terminal.TextLine{line(0, "$ sleep 1"), line(1, "working"), line(2, ""), line(3, "")}, Commands: []terminal.CommandBlock{{ID: 1, CommandLine: 0, CommandColumn: 2, OutputLine: 1, Started: true}}}
	cards := FromHistory(h)
	if len(cards) != 1 || cards[0].Output != "working" || !cards[0].Running || cards[0].Finished || cards[0].Status != -1 {
		t.Fatalf("running = %+v", cards)
	}
}

func TestFinishedCardKeepsFinalNewlineAndEmptyOutput(t *testing.T) {
	h := terminal.History{Lines: []terminal.TextLine{line(0, "$ echo ok"), line(1, "ok"), line(2, "$ true"), line(3, "$ ")}, Commands: []terminal.CommandBlock{
		{ID: 1, CommandLine: 0, CommandColumn: 2, OutputLine: 1, EndLine: 2, Started: true, Finished: true},
		{ID: 2, CommandLine: 2, CommandColumn: 2, OutputLine: 3, EndLine: 3, Started: true, Finished: true},
	}}
	cards := FromHistory(h)
	if len(cards) != 2 || cards[0].Output != "ok\n" || cards[1].Output != "" || cards[1].Truncated {
		t.Fatalf("newlines = %+v", cards)
	}
}

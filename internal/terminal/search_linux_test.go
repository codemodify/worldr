//go:build linux && cgo

package terminal

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func terminalSearch(t *testing.T, term *Terminal, query string, sensitive bool) SearchResults {
	t.Helper()
	result, err := term.Search(query, sensitive)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSearchUnicodeColumnsHistoryAndCycling(t *testing.T) {
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "printf 'ALpha alpha\r\n界é Σςσ Kelvin K\r\nrow-two\r\nready'; IFS= read -r x"}, Cols: 30, Rows: 3})
	before := pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "ready") })
	results := terminalSearch(t, term, "alpha", false)
	if len(results.Matches) != 2 || results.Truncated || results.Matches[0].Text != "ALpha" || results.Matches[1].StartColumn != 6 {
		t.Fatalf("unexpected folded history results: %+v", results)
	}
	if sensitive := terminalSearch(t, term, "alpha", true); len(sensitive.Matches) != 1 || sensitive.Matches[0].Text != "alpha" {
		t.Fatalf("sensitive search: %+v", sensitive)
	}
	for _, tc := range []struct {
		query      string
		start, end int
		count      int
	}{
		{"界", 0, 2, 1}, {"e", 2, 3, 1}, {"́", 2, 3, 1}, {"é", 2, 3, 1},
		{"Σ", 4, 5, 3}, {"k", 8, 9, 2},
	} {
		found := terminalSearch(t, term, tc.query, false)
		// ASCII e also appears in other rows, so select the target Unicode row.
		var matches []SearchMatch
		for _, m := range found.Matches {
			if strings.HasPrefix(m.lineText, "界") {
				matches = append(matches, m)
			}
		}
		if tc.query == "e" {
			matches = matches[:min(1, len(matches))]
		}
		if len(matches) != tc.count || matches[0].StartColumn != tc.start || matches[0].EndColumn != tc.end {
			t.Fatalf("query %q lost Unicode cell coordinates: %+v", tc.query, matches)
		}
	}
	unchanged, err := term.Poll()
	if err != nil || unchanged.Revision != before.Revision || unchanged.ScrollOffset != 0 || screenText(unchanged) != screenText(before) {
		t.Fatal("search changed live terminal state", err)
	}
	index := -1
	for _, want := range []int{0, 1, 0, 1} {
		match, next, ok := results.Next(index, false)
		if !ok || next != want || match != results.Matches[want] {
			t.Fatalf("next(%d)=%+v,%d,%v; want %d", index, match, next, ok, want)
		}
		index = next
	}
	_, index, _ = results.Next(-1, true)
	if index != 1 {
		t.Fatal("backwards search did not begin at newest match")
	}
	for _, want := range []int{0, 1, 0} {
		_, index, _ = results.Next(index, true)
		if index != want {
			t.Fatal("backwards cycle failed")
		}
	}
	if !term.RevealMatch(results.Matches[0]) {
		t.Fatal("retained match did not reveal")
	}
	visible, err := term.Poll()
	if err != nil || visible.ScrollOffset == 0 || !strings.Contains(screenText(visible), "ALpha alpha") || visible.Exited {
		t.Fatal("reveal failed or damaged live shell", err)
	}
	altered := results.Matches[0]
	altered.EndColumn++
	if term.RevealMatch(altered) {
		t.Fatal("changed match coordinates were accepted")
	}
}

func TestSearchPhysicalWrapsOverlapsAndEmptyResults(t *testing.T) {
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "printf 'abcdefghij\r\nbanana\r\nREADY'; IFS= read -r x"}, Cols: 6, Rows: 3})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "READY") })
	for _, query := range []string{"", "fg", "absent", "def\nghi"} {
		result := terminalSearch(t, term, query, false)
		if len(result.Matches) != 0 || result.Truncated {
			t.Fatalf("query %q unexpectedly joined physical rows: %+v", query, result)
		}
		if _, index, ok := result.Next(-1, true); ok || index != -1 {
			t.Fatal("empty results were navigable")
		}
	}
	result := terminalSearch(t, term, "def", true)
	if len(result.Matches) != 1 || result.Matches[0].StartColumn != 3 || result.Matches[0].EndColumn != 6 {
		t.Fatalf("physical row ending lost: %+v", result)
	}
	result = terminalSearch(t, term, "ana", true)
	if len(result.Matches) != 2 || result.Matches[0].StartColumn != 1 || result.Matches[1].StartColumn != 3 {
		t.Fatalf("overlapping matches lost: %+v", result)
	}
	for _, invalid := range []string{strings.Repeat("x", 4097), string([]byte{0xff})} {
		if _, err := term.Search(invalid, true); err == nil {
			t.Fatal("invalid/oversized query accepted")
		}
	}
}

func TestSearchAlternateScreenRetainsTUIState(t *testing.T) {
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "printf 'main-match\r\nready'; IFS= read -r x; printf '\033[?1049h\033[HALT-match'; IFS= read -r x; printf '\033[?1049l\r\nrestored'; IFS= read -r x"}, Cols: 30, Rows: 3})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "ready") })
	match := terminalSearch(t, term, "match", true).Matches[0]
	if err := term.Paste("\n"); err != nil {
		t.Fatal(err)
	}
	before := pollTerminal(t, term, func(s Snapshot) bool { return s.AlternateScreen && strings.Contains(screenText(s), "ALT-match") })
	if found := terminalSearch(t, term, "match", false); len(found.Matches) != 0 || term.RevealMatch(match) {
		t.Fatal("primary history search interfered with alternate screen")
	}
	after, _ := term.Poll()
	if after.Revision != before.Revision || after.ScrollOffset != 0 || after.Cursor != before.Cursor || screenText(after) != screenText(before) {
		t.Fatal("alternate screen changed during search")
	}
	if err := term.Paste("\n"); err != nil {
		t.Fatal(err)
	}
	pollTerminal(t, term, func(s Snapshot) bool { return !s.AlternateScreen && strings.Contains(screenText(s), "restored") })
	if !term.RevealMatch(match) {
		t.Fatal("primary match was not valid after leaving alternate screen")
	}
}

func TestSearchRejectsExpiredAndOverwrittenMatches(t *testing.T) {
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "printf 'match-before\r\nready'; IFS= read -r x; printf '\033[Hmatch-after \r\nchanged'; IFS= read -r x; i=0; while [ $i -lt 12 ]; do printf '\r\nline-%s' \"$i\"; i=$((i+1)); done; printf '\r\ndone'; IFS= read -r x"}, Cols: 30, Rows: 3, Scrollback: 3})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "ready") })
	old := terminalSearch(t, term, "match", true).Matches[0]
	if err := term.Paste("\n"); err != nil {
		t.Fatal(err)
	}
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "changed") })
	if term.RevealMatch(old) {
		t.Fatal("repainted row accepted stale match")
	}
	current := terminalSearch(t, term, "match", true).Matches[0]
	if err := term.Paste("\n"); err != nil {
		t.Fatal(err)
	}
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "done") })
	if term.RevealMatch(current) {
		t.Fatal("evicted row accepted stale match")
	}
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := term.Search("x", false); !errors.Is(err, ErrClosed) {
		t.Fatal("closed terminal search did not report ErrClosed", err)
	}
	if term.RevealMatch(current) {
		t.Fatal("closed terminal reveal succeeded")
	}
}

func TestSearchMatchLimit(t *testing.T) {
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "i=0; while [ $i -lt 4100 ]; do printf 'x\r\n'; i=$((i+1)); done; printf ready; IFS= read -r x"}, Cols: 10, Rows: 3, Scrollback: 5000})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "ready") })
	result := terminalSearch(t, term, "x", true)
	if len(result.Matches) != searchMatchLimit || !result.Truncated {
		t.Fatalf("search results were not bounded: %d / %v", len(result.Matches), result.Truncated)
	}
}

func BenchmarkSearchRetainedHistory(b *testing.B) {
	term, err := Open(Options{Command: "/bin/sh", Args: []string{"-c", "i=0; while [ $i -lt 10000 ]; do printf '%05d %s\r\n' \"$i\" 'A representative build log: compiling package services/router with 32 workers'; i=$((i+1)); done; printf ready; IFS= read -r x"}, Cols: 120, Rows: 24, Scrollback: 10000})
	if err != nil {
		b.Fatal(err)
	}
	defer term.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		snapshot, err := term.Poll()
		if err != nil {
			b.Fatal(err)
		}
		if strings.Contains(screenText(snapshot), "ready") {
			break
		}
		if time.Now().After(deadline) {
			b.Fatal("terminal did not finish generating benchmark history")
		}
		time.Sleep(time.Millisecond)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := term.Search("not-in-this-build-output", false)
		if err != nil || len(result.Matches) != 0 {
			b.Fatal("unexpected benchmark search result", err)
		}
	}
}

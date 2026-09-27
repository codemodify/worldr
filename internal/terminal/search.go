package terminal

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

const searchMatchLimit = 4096

// SearchMatch refers to a retained physical row. Columns are terminal-cell
// coordinates, with an exclusive end that covers any wide or combining glyph.
// Text preserves the original case. A match never spans two physical rows.
type SearchMatch struct {
	Line                   uint64
	StartColumn, EndColumn int
	Text                   string
	lineText               string
	startRune, endRune     int
}

// SearchResults is an immutable point-in-time result set in oldest-to-newest,
// left-to-right order. Truncated reports that more than 4096 matches existed.
type SearchResults struct {
	Matches   []SearchMatch
	Truncated bool
}

// Next cycles through results. Use current=-1 for the initial first match (or
// last when backwards). An empty result returns index -1 and ok=false.
func (r SearchResults) Next(current int, backwards bool) (match SearchMatch, index int, ok bool) {
	n := len(r.Matches)
	if n == 0 {
		return SearchMatch{}, -1, false
	}
	if current < 0 || current >= n {
		index = 0
		if backwards {
			index = n - 1
		}
	} else if backwards {
		index = (current + n - 1) % n
	} else {
		index = (current + 1) % n
	}
	return r.Matches[index], index, true
}

func searchPattern(query string, caseSensitive bool) ([]rune, []int, error) {
	if len(query) > 4096 || !utf8.ValidString(query) {
		return nil, nil, fmt.Errorf("terminal search requires valid UTF-8 of at most 4096 bytes")
	}
	pattern := []rune(query)
	if !caseSensitive {
		for i, r := range pattern {
			pattern[i] = searchFold(r)
		}
	}
	// KMP keeps even a repetitive query/output pair linear in retained text,
	// without allocations for a lowercased copy or byte/column assumptions.
	failure := make([]int, len(pattern))
	for i, j := 1, 0; i < len(pattern); i++ {
		for j > 0 && pattern[i] != pattern[j] {
			j = failure[j-1]
		}
		if pattern[i] == pattern[j] {
			j++
		}
		failure[i] = j
	}
	return pattern, failure, nil
}

// SimpleFold handles complete Unicode case cycles, including Greek final sigma
// and Kelvin K, without changing the number of code points or their columns.
// Canonical normalization and multi-code-point folds (ß/ss) are not inferred.
func searchFold(r rune) rune {
	if r < utf8.RuneSelf {
		if r >= 'a' && r <= 'z' {
			return r - ('a' - 'A')
		}
		return r
	}
	minimum := r
	for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
		if next < minimum {
			minimum = next
		}
	}
	return minimum
}

func searchLine(results *SearchResults, line TextLine, pattern []rune, failure []int, caseSensitive bool) bool {
	if len(pattern) == 0 {
		return true
	}
	runes := []rune(line.Text)
	for i, j := 0, 0; i < len(runes); i++ {
		r := runes[i]
		if !caseSensitive {
			r = searchFold(r)
		}
		for j > 0 && r != pattern[j] {
			j = failure[j-1]
		}
		if r == pattern[j] {
			j++
		}
		if j != len(pattern) {
			continue
		}
		if len(results.Matches) == searchMatchLimit {
			results.Truncated = true
			return false
		}
		start, end := i+1-len(pattern), i+1
		// Combining code points occupy the base cell. Include that entire cell
		// even if the query ends before its final combining code point.
		endColumn := end
		for endColumn < len(line.Columns)-1 && line.Columns[endColumn] <= line.Columns[i] {
			endColumn++
		}
		results.Matches = append(results.Matches, SearchMatch{
			Line: line.ID, StartColumn: int(line.Columns[start]), EndColumn: int(line.Columns[endColumn]),
			Text: string(runes[start:end]), lineText: line.Text, startRune: start, endRune: endColumn,
		})
		j = failure[j-1]
	}
	return true
}

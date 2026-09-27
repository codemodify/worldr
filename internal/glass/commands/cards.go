// Package commands gives the terminal's shell metadata a presentation model.
// It neither guesses prompts nor executes, replaces, or replays commands.
package commands

import (
	"strings"

	"github.com/codemodify/worldr/internal/terminal"
)

// Card is an immutable view of one command recorded by an integrated shell.
// Command and Output contain the retained terminal text, not a reconstructed
// shell program. Physical wrapping, secondary prompts, and terminal rewriting
// remain visible. A blank Command with CommandTruncated means its text expired.
type Card struct {
	ID                                           uint64
	Command, Output                              string
	Running, Finished                            bool
	Status                                       int
	Truncated, CommandTruncated, OutputTruncated bool
	CommandLine, OutputLine, EndLine             uint64
}

// FromHistory copies started commands from a terminal history snapshot. It
// excludes the editable prompt and drops completed commands whose entire
// retained range has expired. Running output ends at the last nonempty retained
// row because History deliberately contains no live cursor coordinates.
func FromHistory(history terminal.History) []Card {
	if len(history.Lines) == 0 {
		return nil
	}
	first := history.Lines[0].ID
	last := history.Lines[len(history.Lines)-1]
	var cards []Card
	for _, block := range history.Commands {
		if !block.Started || block.OutputLine < block.CommandLine {
			continue
		}
		if block.Finished && (block.EndLine < first || block.EndLine == first && block.EndColumn == 0) {
			continue
		}
		endLine, endCol := block.EndLine, block.EndColumn
		if !block.Finished {
			endLine, endCol = last.ID, endColumn(last)
			for i := len(history.Lines) - 1; i >= 0; i-- {
				line := history.Lines[i]
				if line.ID < block.OutputLine {
					break
				}
				endLine, endCol = line.ID, endColumn(line)
				if line.Text != "" {
					break
				}
			}
		}
		if endLine < block.OutputLine || endLine == block.OutputLine && endCol < block.OutputColumn {
			continue
		}
		command, commandTruncated := textRange(history.Lines, block.CommandLine, block.CommandColumn, block.OutputLine, block.OutputColumn)
		output, outputTruncated := textRange(history.Lines, block.OutputLine, block.OutputColumn, endLine, endCol)
		// The Enter that submits a command creates the final physical newline;
		// it is a boundary, while preceding multiline command rows stay intact.
		command = strings.TrimSuffix(command, "\n")
		status := block.Status
		if !block.Finished {
			status = -1
		}
		cards = append(cards, Card{
			ID: block.ID, Command: command, Output: output,
			Running: !block.Finished, Finished: block.Finished, Status: status,
			Truncated:        commandTruncated || outputTruncated,
			CommandTruncated: commandTruncated, OutputTruncated: outputTruncated,
			CommandLine: block.CommandLine, OutputLine: block.OutputLine, EndLine: endLine,
		})
	}
	return cards
}

func endColumn(line terminal.TextLine) int {
	if len(line.Columns) == 0 {
		return 0
	}
	return int(line.Columns[len(line.Columns)-1])
}

// textRange uses terminal columns, not bytes or rune offsets. Combining marks
// share their base cell's column; a cut through a wide glyph excludes that glyph
// rather than returning a partial character.
func textRange(lines []terminal.TextLine, first uint64, firstCol int, last uint64, lastCol int) (string, bool) {
	if last < first || last == first && lastCol < firstCol {
		return "", true
	}
	if first == last && firstCol == lastCol {
		return "", false
	}
	retainedFirst, retainedLast := lines[0].ID, lines[len(lines)-1].ID
	truncated := first < retainedFirst || last > retainedLast
	if last < retainedFirst || first > retainedLast {
		return "", true
	}
	var text strings.Builder
	var previous uint64
	wroteLine := false
	for _, line := range lines {
		if line.ID < first || line.ID > last {
			continue
		}
		if wroteLine {
			text.WriteByte('\n')
			if line.ID != previous+1 {
				truncated = true
			}
		}
		start, end := 0, endColumn(line)
		if line.ID == first {
			start = firstCol
		}
		if line.ID == last {
			end = lastCol
		}
		runes := []rune(line.Text)
		if len(line.Columns) != len(runes)+1 {
			truncated = true
		} else {
			for i := 0; i < len(runes); {
				col, next := int(line.Columns[i]), i+1
				for next < len(runes) && line.Columns[next] == line.Columns[i] {
					next++
				}
				if col >= start && col < end && int(line.Columns[next]) <= end {
					text.WriteString(string(runes[i:next]))
				}
				i = next
			}
		}
		previous, wroteLine = line.ID, true
	}
	return text.String(), truncated
}

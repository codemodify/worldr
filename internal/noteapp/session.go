// Package noteapp provides Worldr's bounded native note and document editor.
package noteapp

import (
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/resourcepath"
)

const (
	MaxNotes = 8
	// Eight maximally escaped note buffers still fit inside the workspace's
	// bounded 1 MiB recovery envelope with room for layout and other windows.
	MaxDocumentBytes = 48 << 10
)

// SessionState contains the complete committed editor state. Text is retained
// even for file-backed notes so a recovery checkpoint cannot discard edits
// which have not reached disk yet.
type SessionState struct {
	Key      string `json:"key"`
	Source   string `json:"source,omitempty"`
	Text     string `json:"text"`
	DiskHash string `json:"disk_hash,omitempty"`
	Caret    int    `json:"caret"`
	Anchor   int    `json:"anchor"`
	TopLine  int    `json:"top_line"`
	Left     int    `json:"left"`
	Dirty    bool   `json:"dirty,omitempty"`
}

func slotKey(slot int) string {
	if slot == 0 {
		return "native:note"
	}
	return fmt.Sprintf("native:note-%d", slot+1)
}

func keySlot(key string) (int, error) {
	for slot := 0; slot < MaxNotes; slot++ {
		if key == slotKey(slot) {
			return slot, nil
		}
	}
	return -1, fmt.Errorf("invalid native note key %q", key)
}

// IsNotePath deliberately claims only Worldr note documents. Ordinary Markdown
// and source files remain regular previews in Files.
func IsNotePath(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".worldr-note.md")
}

func validDocumentText(text string) bool {
	return len(text) <= MaxDocumentBytes && validTextFragment(text)
}

func validTextFragment(text string) bool {
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return false
	}
	for _, char := range text {
		if unicode.Is(unicode.Cc, char) && char != '\n' && char != '\r' && char != '\t' {
			return false
		}
	}
	return true
}

func textBoundary(text string, index int) bool {
	return index >= 0 && index <= len(text) && (index == len(text) || utf8.RuneStart(text[index]))
}

func (s SessionState) Validate() error {
	if _, err := keySlot(s.Key); err != nil {
		return err
	}
	if s.Source != "" {
		if err := resourcepath.Validate(s.Source); err != nil {
			return fmt.Errorf("note source: %w", err)
		}
		if !IsNotePath(s.Source) {
			return fmt.Errorf("native notes require a .worldr-note.md source")
		}
		if len(s.DiskHash) != 64 {
			return fmt.Errorf("file-backed note requires a SHA-256 disk identity")
		}
		if _, err := hex.DecodeString(s.DiskHash); err != nil {
			return fmt.Errorf("invalid note disk identity")
		}
	} else if s.DiskHash != "" {
		return fmt.Errorf("untitled note cannot have a disk identity")
	}
	if !validDocumentText(s.Text) {
		return fmt.Errorf("note text must be valid UTF-8, contain no NUL, and fit within %d bytes", MaxDocumentBytes)
	}
	if !textBoundary(s.Text, s.Caret) || !textBoundary(s.Text, s.Anchor) {
		return fmt.Errorf("note selection is not on a UTF-8 boundary")
	}
	stops := runeStops(s.Text)
	for _, position := range []int{s.Caret, s.Anchor} {
		index := sort.SearchInts(stops, position)
		if index >= len(stops) || stops[index] != position {
			return fmt.Errorf("note selection splits a grapheme cluster")
		}
	}
	if s.TopLine < 0 || s.TopLine > MaxDocumentBytes || s.Left < 0 || s.Left > MaxDocumentBytes {
		return fmt.Errorf("note viewport is out of bounds")
	}
	return nil
}

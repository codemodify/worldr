package noteapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/codemodify/worldr/internal/resourcepath"
)

func documentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func readDocument(file *os.File) (string, string, string, error) {
	if file == nil {
		return "", "", "", fmt.Errorf("no note document")
	}
	info, err := file.Stat()
	if err != nil {
		return "", "", "", err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > MaxDocumentBytes {
		return "", "", "", fmt.Errorf("note must be a regular UTF-8 file no larger than %d KiB", MaxDocumentBytes>>10)
	}
	path, err := resourcepath.FromFile(file)
	if err != nil {
		return "", "", "", err
	}
	if !IsNotePath(path) {
		return "", "", "", fmt.Errorf("native notes require a .worldr-note.md filename")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxDocumentBytes+1))
	if err != nil {
		return "", "", "", err
	}
	if len(data) > MaxDocumentBytes || !validDocumentText(string(data)) {
		return "", "", "", fmt.Errorf("note must be valid UTF-8, contain no NUL, and fit within %d KiB", MaxDocumentBytes>>10)
	}
	return path, string(data), documentHash(data), nil
}

type saveSnapshot struct {
	path, text, expected string
	revision             uint64
}

type saveResult struct {
	revision uint64
	hash     string
	err      error
}

func currentDocument(path string) ([]byte, os.FileMode, error) {
	file, err := resourcepath.OpenFile(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, err
	}
	if info.Size() < 0 || info.Size() > MaxDocumentBytes {
		return nil, 0, fmt.Errorf("note on disk exceeds %d KiB", MaxDocumentBytes>>10)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxDocumentBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if len(data) > MaxDocumentBytes {
		return nil, 0, fmt.Errorf("note on disk exceeds %d KiB", MaxDocumentBytes>>10)
	}
	return data, info.Mode().Perm(), nil
}

// writeDocument performs an optimistic, durable atomic replacement. Refusing
// a mismatched hash prevents a background editor from silently replacing work
// written by another process since this note was opened or last saved.
func writeDocument(snapshot saveSnapshot) (string, error) {
	if err := resourcepath.Validate(snapshot.path); err != nil {
		return "", err
	}
	if !IsNotePath(snapshot.path) || !validDocumentText(snapshot.text) {
		return "", fmt.Errorf("invalid native note document")
	}
	current, mode, err := currentDocument(snapshot.path)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(documentHash(current), snapshot.expected) {
		return "", fmt.Errorf("note changed on disk; reopen it before saving")
	}
	directory := filepath.Dir(snapshot.path)
	file, err := os.CreateTemp(directory, ".worldr-note-*")
	if err != nil {
		return "", err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	defer file.Close()
	if err = file.Chmod(mode); err != nil {
		return "", err
	}
	if _, err = io.Copy(file, bytes.NewBufferString(snapshot.text)); err != nil {
		return "", err
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	if err = os.Rename(temporary, snapshot.path); err != nil {
		return "", err
	}
	dir, err := os.Open(directory)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return "", err
	}
	return documentHash([]byte(snapshot.text)), nil
}

func validateClipboardText(text string) error {
	if len(text) > 1<<20 || !validTextFragment(text) {
		return fmt.Errorf("clipboard text must be UTF-8 text without binary control bytes and be at most 1 MiB")
	}
	return nil
}

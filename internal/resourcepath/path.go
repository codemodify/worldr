// Package resourcepath records reopenable local paths without confusing a
// display label or an obsolete file name with the file actually opened.
package resourcepath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Validate accepts canonical absolute local paths that round-trip through JSON.
func Validate(path string) error {
	if path == "" || len(path) > 4096 || !utf8.ValidString(path) || strings.ContainsRune(path, 0) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("expected a clean absolute local path")
	}
	return nil
}

// FromFile borrows file and verifies that the returned path still identifies
// the opened resource. Unlinked/replaced resources cannot safely be reopened.
func FromFile(file *os.File) (string, error) {
	if file == nil {
		return "", fmt.Errorf("no open resource")
	}
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return "", fmt.Errorf("resource must be a regular file or directory")
	}
	path, err := descriptorPath(file)
	if err != nil {
		return "", err
	}
	if err := Validate(path); err != nil {
		return "", err
	}
	current, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, current) {
		return "", fmt.Errorf("resource path no longer identifies the open file")
	}
	return path, nil
}

// OpenFile and OpenDirectory reopen data, never commands. The Linux adapter
// rejects final symlinks and opens nonblocking before checking the descriptor.
func OpenFile(path string) (*os.File, error)      { return openResource(path, false) }
func OpenDirectory(path string) (*os.File, error) { return openResource(path, true) }

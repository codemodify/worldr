package projectapp

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxEntries = 5000
	maxPreview = 1 << 20
)

type entryKind uint8

const (
	fileEntry entryKind = iota
	directoryEntry
	symlinkEntry
	specialEntry
)

type entry struct {
	name  string
	kind  entryKind
	stamp fileStamp
}

type fileStamp struct {
	device, inode  uint64
	size, modified int64
	mode           uint32
}

type request struct {
	ctx              context.Context
	generation       uint64
	path             string // Clean root-relative path; empty names the startup root.
	directory        bool
	openMedia        bool
	openTerminal     bool
	resumePreview    bool
	restoreDirectory bool
	refresh          bool
	operation        *fileOperation
	thumbnail        bool
	thumbnailStamp   fileStamp
	selected         string
}

type result struct {
	request
	entries        []entry
	preview        textPreview
	truncated      bool
	err            error
	file           *os.File
	undo           *fileUndo
	notice         string
	thumbnailImage *image.RGBA
}

func (r *result) close() {
	if r.file != nil {
		_ = r.file.Close()
		r.file = nil
	}
}

func drainResults(results chan result) {
	for {
		select {
		case res := <-results:
			res.close()
		default:
			return
		}
	}
}

func videoPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".mkv", ".webm", ".mov", ".m4v", ".avi", ".ogv", ".ogg", ".mpeg", ".mpg", ".ts", ".m2ts":
		return true
	}
	return false
}

// IsPhotoPath reports whether a filename has a supported native photo extension.
// The photo viewer validates its contents; GIF files display their first frame.
func IsPhotoPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp":
		return true
	}
	return false
}

// IsModelPath identifies native triangle models and model-inspection documents.
// The model application validates their contents after an explicit Open.
func IsModelPath(path string) bool {
	lower := strings.ToLower(path)
	return filepath.Ext(lower) == ".obj" || filepath.Ext(lower) == ".stl" || strings.HasSuffix(lower, ".worldr-model.json")
}

// IsDatasetPath identifies bounded native research documents. Ordinary JSON
// remains available in the text preview; the explicit suffix avoids claiming
// configuration files just because they contain an array.
func IsDatasetPath(path string) bool {
	lower := strings.ToLower(path)
	return filepath.Ext(lower) == ".csv" || filepath.Ext(lower) == ".tsv" || strings.HasSuffix(lower, ".worldr-data.json")
}

// IsNotePath identifies Worldr's editable note documents without claiming
// ordinary Markdown, text, source or configuration files.
func IsNotePath(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".worldr-note.md")
}

func mediaPath(path string) bool {
	return videoPath(path) || IsPhotoPath(path) || IsModelPath(path) || IsDatasetPath(path) || IsNotePath(path)
}

type textPreview struct {
	text       string
	starts     []int32
	maxColumns int
}

func makePreview(text string) textPreview {
	p := textPreview{text: text, starts: []int32{0}}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' && i+1 < len(text) {
			p.starts = append(p.starts, int32(i+1))
		}
	}
	column := 0
	for _, char := range text {
		switch char {
		case '\n':
			p.maxColumns = max(p.maxColumns, column)
			column = 0
		case '\t':
			column += 4 - column%4
		default:
			column++
		}
	}
	p.maxColumns = max(p.maxColumns, column)
	return p
}

func (p textPreview) line(index int) string {
	if index < 0 || index >= len(p.starts) {
		return ""
	}
	start, end := int(p.starts[index]), len(p.text)
	if index+1 < len(p.starts) {
		end = int(p.starts[index+1])
	}
	return strings.TrimSuffix(strings.TrimSuffix(p.text[start:end], "\n"), "\r")
}

// The worker owns directory traversal, file opening, and text reads. Requests
// and results cross channels; Poll consumes metadata and hands checked file or
// directory descriptors to host callbacks with their documented ownership.
type projectReader interface {
	read(request) result
	close() error
}

func runReader(ctx context.Context, reader projectReader, requests <-chan request, results chan result, done chan<- struct{}) {
	defer close(done)
	defer reader.close()
	// Close and a just-completed read can race to drain the result channel.
	// The worker performs the final drain after its last possible send.
	defer drainResults(results)
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-requests:
			if req.ctx.Err() != nil {
				continue
			}
			res := reader.read(req)
			if req.ctx.Err() != nil {
				res.close()
				continue
			}
			select {
			case results <- res:
			case <-req.ctx.Done():
				res.close()
			}
		}
	}
}

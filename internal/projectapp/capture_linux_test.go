//go:build linux

package projectapp

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Captures the actual retained Files texture without opening a GPU or window.
func TestFilesControlsCapture(t *testing.T) {
	output := os.Getenv("WORLDR_CAPTURE_DIR")
	if output == "" {
		t.Skip("set WORLDR_CAPTURE_DIR to capture the native Files UI")
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, name := range []string{"Experiment notes", "Models"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Fixture color study.png", "Research field.jpg"} {
		if err := os.WriteFile(filepath.Join(root, name), thumbnailFixture(t, "png"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README.txt"), []byte("Files supports bounded filename search, thumbnails and recoverable file operations.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.Resize(1, minWidth, 420)
	p.Focus(1)
	pollUntil(t, p, func() bool {
		return !p.loadingDirectory && !p.loadingFile && !p.thumbnailPending && len(p.thumbnails) == 2
	})
	save := func(name string) {
		t.Helper()
		if err := p.Poll(); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(output, name))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, p.renderer.image); err != nil {
			t.Fatal(err)
		}
	}
	save("worldr-files-controls.png")
	p.beginOperation(createFolder)
	p.dialog.field.Set("研究 – Field observations")
	save("worldr-files-operation.png")
}

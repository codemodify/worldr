//go:build linux && cgo

package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/workspace"
)

func TestWorkspaceHelpGPUOverlayAndDismissal(t *testing.T) {
	if os.Getenv("WORLDR_TEST_GPU") != "1" {
		t.Skip("set WORLDR_TEST_GPU=1 to render the shortcut guide")
	}
	w, err := workspace.New()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Dispatch(workspace.Action{Kind: workspace.SetPlayback, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	const width, height = 1440, 900
	vk, err := native.OpenVK(false, width, height)
	if err != nil {
		t.Fatal(err)
	}
	defer vk.Close()
	if err := vk.SetSceneAtlas(w.Atlas()); err != nil {
		t.Fatal(err)
	}
	render := func() []byte {
		pixels := make([]byte, width*height*4)
		if err := vk.RenderFrame(w.Draw(width, height), [4]float32{0, 0, 0, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		return pixels
	}
	before, document := render(), w.Document()
	w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyF1, Keycode: 59, Pressed: true})
	w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyF1, Keycode: 59})
	guide := render()
	// The opaque sheet covers existing camera content, after the camera pass.
	for _, point := range [][2]int{{260, 150}, {700, 400}, {1175, 745}} {
		at := (point[1]*width + point[0]) * 4
		if !bytes.Equal(guide[at:at+4], []byte{0x2d, 0x1e, 0x10, 255}) {
			t.Fatalf("help sheet did not cover scene at %v: %v", point, guide[at:at+4])
		}
	}
	if bytes.Equal(before, guide) || w.Document() != document {
		t.Fatal("guide did not render or mutated the document")
	}
	if directory := os.Getenv("WORLDR_CAPTURE_DIR"); directory != "" {
		if err := savePNG(filepath.Join(directory, "workspace-help.png"), guide, width, height); err != nil {
			t.Fatal(err)
		}
	}
	w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1, Pressed: true})
	w.Handle(experience.Event{Kind: experience.KeyInput, Key: experience.KeyEscape, Keycode: 1})
	if after := render(); !bytes.Equal(before, after) || w.Document() != document {
		t.Fatal("dismissing help failed to restore the exact unchanged workspace")
	}
}

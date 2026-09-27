// Skin Studio demonstrates the public skin, nativeui, and nativeapp SDKs.
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
	"github.com/codemodify/worldr/sdk/nativeui/v1/gallery"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

type studio struct {
	selected                        skin.Skin
	demo                            *gallery.Gallery
	host                            nativeapp.Host
	pixels                          *image.RGBA
	revision                        uint64
	dirty, closed, retired, focused bool
	clock                           time.Duration
}

func (s *studio) Manifest() nativeapp.Manifest {
	return nativeapp.Manifest{ID: "dev.worldr.skin-studio", Name: "Skin Studio", Description: "Interactive shared window and control skin showcase", Skins: true, ControlThemes: true}
}

func (s *studio) Start(host nativeapp.Host) error {
	s.host = host
	t, err := nativeui.ThemeFromSkin(s.selected)
	if err != nil {
		return err
	}
	s.demo, err = gallery.New(t)
	if err != nil {
		return err
	}
	return s.paint(min(960, host.MaxSurfaceWidth), min(600, host.MaxSurfaceHeight))
}

func (s *studio) paint(width, height int) error {
	pixels, err := s.demo.Render(width, height)
	if err != nil {
		return err
	}
	s.pixels = pixels
	s.revision++
	s.dirty = true
	return nil
}

func (s *studio) SetSkin(selected skin.Skin) error {
	theme, err := nativeui.ThemeFromSkin(selected)
	if err != nil {
		return err
	}
	if err = s.demo.SetTheme(theme); err != nil {
		return err
	}
	s.selected = selected.Clone()
	return s.paint(s.pixels.Bounds().Dx(), s.pixels.Bounds().Dy())
}

func (s *studio) SetControlTheme(preference nativeapp.ControlTheme) error {
	theme, err := nativeui.FromControlTheme(preference)
	if err != nil {
		return err
	}
	if err = s.demo.SetTheme(theme); err != nil {
		return err
	}
	s.selected.Name = "Legacy / " + preference.Family
	return s.paint(s.pixels.Bounds().Dx(), s.pixels.Bounds().Dy())
}

func (s *studio) Update(delta time.Duration) error {
	if s.closed {
		return nil
	}
	if s.demo.Update(delta) {
		s.clock += delta
		if s.clock >= 100*time.Millisecond || !s.demo.Values.Running {
			s.clock = 0
			return s.paint(s.pixels.Bounds().Dx(), s.pixels.Bounds().Dy())
		}
	}
	return nil
}

func (s *studio) Snapshot() nativeapp.Snapshot {
	if s.closed {
		if !s.retired {
			s.retired = true
			return nativeapp.Snapshot{RetireTextures: []nativeapp.ResourceID{1}}
		}
		return nativeapp.Snapshot{}
	}
	var updates []nativeapp.TextureUpdate
	w, h := s.pixels.Bounds().Dx(), s.pixels.Bounds().Dy()
	if s.dirty {
		updates = []nativeapp.TextureUpdate{{ID: 1, Revision: s.revision, Width: w, Height: h, Rect: nativeapp.Rect{Width: w, Height: h}, Pixels: append([]byte(nil), s.pixels.Pix...)}}
		s.dirty = false
	}
	semantics := s.demo.Semantics()
	var text nativeapp.TextInputState
	if s.focused && semantics.FocusedID == "query" {
		for _, node := range semantics.Nodes {
			if node.ID == "query" {
				text = nativeapp.TextInputState{Enabled: true, ContextID: "filter", Surrounding: s.demo.Values.Query, Cursor: len(s.demo.Values.Query), Anchor: len(s.demo.Values.Query), CursorRect: node.Bounds}
				break
			}
		}
	}
	return nativeapp.Snapshot{Textures: updates, Surfaces: []nativeapp.Surface{{ID: 1, Key: "studio", Title: "SKIN STUDIO / " + s.selected.Name, Texture: 1, FrameStyle: nativeapp.FrameCinematic, Semantics: semantics, TextInput: text}}}
}

func (s *studio) Handle(id nativeapp.SurfaceID, event nativeapp.Event) error {
	if id != 1 || s.closed {
		return nil
	}
	if event.Kind == nativeapp.TextCommit && event.TextContext != "" && event.TextContext != "filter" {
		return nil
	}
	changed, err := s.demo.Handle(event)
	if err != nil {
		return err
	}
	if changed {
		return s.paint(s.pixels.Bounds().Dx(), s.pixels.Bounds().Dy())
	}
	return nil
}

func (s *studio) Focus(id nativeapp.SurfaceID) error {
	s.focused = id == 1
	if !s.focused && s.demo != nil && !s.closed {
		changed, err := s.demo.Handle(nativeapp.Event{Kind: nativeapp.KeyboardCancel})
		if err != nil {
			return err
		}
		if changed {
			return s.paint(s.pixels.Bounds().Dx(), s.pixels.Bounds().Dy())
		}
	}
	return nil
}
func (s *studio) Resize(id nativeapp.SurfaceID, w, h int) error {
	if id != 1 || s.closed {
		return nil
	}
	return s.paint(max(640, min(s.host.MaxSurfaceWidth, w)), max(320, min(s.host.MaxSurfaceHeight, h)))
}
func (s *studio) CloseSurface(id nativeapp.SurfaceID) error {
	if id == 1 {
		s.closed = true
		s.dirty = false
	}
	return nil
}
func (s *studio) Close() error {
	if s.demo != nil {
		return s.demo.Close()
	}
	return nil
}

func readSkin(value string) (skin.Skin, error) {
	if s, err := skin.Builtin(value); err == nil {
		return s, nil
	}
	f, err := os.Open(value)
	if err != nil {
		return skin.Skin{}, err
	}
	defer f.Close()
	return skin.Decode(f)
}

func run() error {
	selection := flag.String("skin", "merrick", "preset ID or skin JSON path")
	snapshot := flag.String("snapshot", "", "render the control showcase to a PNG and exit")
	export := flag.String("export-skins", "", "write editable preset JSON packages into this directory and exit")
	flag.Parse()
	if *export != "" {
		if err := os.MkdirAll(*export, 0755); err != nil {
			return err
		}
		for _, s := range skin.Builtins() {
			f, err := os.Create(filepath.Join(*export, s.ID+".json"))
			if err != nil {
				return err
			}
			err = skin.Encode(f, s)
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return nil
	}
	selected, err := readSkin(*selection)
	if err != nil {
		return err
	}
	app := &studio{selected: selected}
	if *snapshot != "" {
		theme, err := nativeui.ThemeFromSkin(selected)
		if err != nil {
			return err
		}
		demo, err := gallery.New(theme)
		if err != nil {
			return err
		}
		defer demo.Close()
		pixels, err := demo.Render(960, 600)
		if err != nil {
			return err
		}
		f, err := os.Create(*snapshot)
		if err != nil {
			return err
		}
		err = png.Encode(f, pixels)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	return nativeapp.Serve(context.Background(), app, os.Stdin, os.Stdout)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "skin studio:", err)
		os.Exit(1)
	}
}

// Merrick Desktop is a working, multi-surface reference application built with
// the public nativeapp, nativeui and skin SDKs. Its records are fictional.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

//go:embed assets/personnel-portrait.png
var defaultPortrait []byte

type surfaceSpec struct {
	id            nativeapp.SurfaceID
	key, title    string
	width, height int
}

var surfaceSpecs = []surfaceSpec{
	{1, "calendar", "Calendar", 190, 720},
	{2, "records", "Agnate Profile / Jordan Two Delta", 890, 320},
	{3, "archive", "Internal Paper / Research Methods", 340, 460},
	{4, "report", "Internal Paper / Habitat Study", 340, 460},
	{5, "notes", "Internal Paper / Signal Archive", 340, 460},
	{6, "programs", "Programs", 460, 86},
	{7, "messages", "Messages", 230, 120},
}

type deskState struct {
	Week, Day, RecordTab int
	Access, ArchiveSync  bool
	Nutrients            [6]float64
	Pages                [3]int
	Outgoing             bool
	Message              int
}
type surfaceView struct {
	spec                   surfaceSpec
	live, dirty, published bool
	pixels                 *image.RGBA
	width, height          int
	texture                nativeapp.ResourceID
	revision               uint64
	controller             nativeui.Controller
	controls               []nativeui.Control
}
type desktop struct {
	selected      skin.Skin
	state         deskState
	host          nativeapp.Host
	views         []*surfaceView
	painters      map[int]*nativeui.Painter
	nextTexture   nativeapp.ResourceID
	retire        []nativeapp.ResourceID
	focused       nativeapp.SurfaceID
	portrait      image.Image
	portraitCache *image.RGBA
	notice        string
	closed        bool
}

func (d *desktop) Manifest() nativeapp.Manifest {
	return nativeapp.Manifest{ID: "dev.worldr.merrick-desktop", Name: "Merrick Desktop", Description: "A working fictional calendar, profile, document and communications desktop", Skins: true}
}

func (d *desktop) Start(host nativeapp.Host) error {
	if host.MaxSurfaces < 1 || host.MaxSurfaceWidth < 1 || host.MaxSurfaceHeight < 1 {
		return fmt.Errorf("Merrick Desktop needs valid surface limits")
	}
	d.host = host
	d.state = deskState{Access: true, ArchiveSync: true, Nutrients: [6]float64{74, 62, 48, 82, 55, 68}}
	d.painters = map[int]*nativeui.Painter{}
	if d.portrait == nil {
		var err error
		d.portrait, _, err = image.Decode(bytes.NewReader(defaultPortrait))
		if err != nil {
			return fmt.Errorf("decode personnel portrait: %w", err)
		}
	}
	if d.selected.ID == "" {
		var err error
		d.selected, err = skin.Builtin("merrick")
		if err != nil {
			return err
		}
	}
	if err := d.setPainters(d.selected); err != nil {
		return err
	}
	for _, spec := range surfaceSpecs {
		v := &surfaceView{spec: spec, width: min(spec.width, host.MaxSurfaceWidth), height: min(spec.height, host.MaxSurfaceHeight)}
		d.views = append(d.views, v)
		if err := v.controller.SetTheme(d.painters[10].Theme()); err != nil {
			return err
		}
	}
	// Retain the launcher when a constrained host permits fewer than seven views.
	order := []nativeapp.SurfaceID{6, 2, 1, 3, 4, 5, 7}
	for i, id := range order {
		if i >= host.MaxSurfaces {
			break
		}
		if err := d.open(id); err != nil {
			return err
		}
	}
	d.notice = "MERRICK / PROGRAM INDEX"
	return d.paint(d.view(6))
}
func (d *desktop) view(id nativeapp.SurfaceID) *surfaceView {
	for _, v := range d.views {
		if v.spec.id == id {
			return v
		}
	}
	return nil
}
func (d *desktop) open(id nativeapp.SurfaceID) error {
	v := d.view(id)
	if v == nil {
		return nil
	}
	if v.live {
		d.notice = v.spec.title + " is open"
		return nil
	}
	live := 0
	for _, other := range d.views {
		if other.live {
			live++
		}
	}
	if live >= d.host.MaxSurfaces {
		d.notice = "Surface limit reached"
		return nil
	}
	d.nextTexture++
	v.texture = d.nextTexture
	v.revision = 0
	v.published = false
	v.live = true
	d.notice = "Opened " + v.spec.title
	return d.paint(v)
}
func (d *desktop) SetSkin(selected skin.Skin) error {
	if err := selected.Validate(); err != nil {
		return err
	}
	if d.closed {
		return nil
	}
	if err := d.setPainters(selected); err != nil {
		return err
	}
	d.selected = selected.Clone()
	d.portraitCache = nil
	for _, v := range d.views {
		if err := v.controller.SetTheme(d.painters[10].Theme()); err != nil {
			return err
		}
		if v.live {
			if err := d.paint(v); err != nil {
				return err
			}
		}
	}
	return nil
}
func (d *desktop) Snapshot() nativeapp.Snapshot {
	s := nativeapp.Snapshot{RetireTextures: append([]nativeapp.ResourceID(nil), d.retire...)}
	d.retire = nil
	if d.closed {
		return s
	}
	for _, v := range d.views {
		if !v.live {
			continue
		}
		if v.dirty {
			v.revision++
			s.Textures = append(s.Textures, nativeapp.TextureUpdate{ID: v.texture, Revision: v.revision, Width: v.width, Height: v.height, Rect: nativeapp.Rect{Width: v.width, Height: v.height}, Pixels: append([]byte(nil), v.pixels.Pix...)})
			v.dirty = false
			v.published = true
		}
		semantics := v.controller.Semantics()
		for i := range semantics.Nodes {
			node := &semantics.Nodes[i]
			if strings.HasPrefix(node.ID, "nutrient-") {
				index, _ := strconv.Atoi(strings.TrimPrefix(node.ID, "nutrient-"))
				if index >= 0 && index < len(nutrientNames) {
					node.Label = nutrientNames[index]
				}
			}
			switch node.ID {
			case "page-prev":
				node.Label = "Previous page"
			case "page-next":
				node.Label = "Next page"
			case "week-prev":
				node.Label = "Previous week"
			case "week-next":
				node.Label = "Next week"
			case "message-next":
				node.Label = "Next message"
			}
		}
		s.Surfaces = append(s.Surfaces, nativeapp.Surface{ID: v.spec.id, Key: v.spec.key, Title: v.spec.title, Texture: v.texture, MinWidth: 96, MinHeight: 64, FrameStyle: nativeapp.FrameDefault, Semantics: semantics})
	}
	return s
}
func (d *desktop) Update(time.Duration) error { return nil }
func (d *desktop) Focus(id nativeapp.SurfaceID) error {
	if d.closed {
		return nil
	}
	for _, v := range d.views {
		if v.spec.id != id && v.controller.FocusedID() != "" {
			v.controller.Blur()
			if v.live {
				if err := d.paint(v); err != nil {
					return err
				}
			}
		}
	}
	d.focused = id
	return nil
}
func (d *desktop) Resize(id nativeapp.SurfaceID, width, height int) error {
	v := d.view(id)
	if d.closed || v == nil || !v.live {
		return nil
	}
	width, height = max(1, min(width, d.host.MaxSurfaceWidth)), max(1, min(height, d.host.MaxSurfaceHeight))
	if width == v.width && height == v.height {
		return nil
	}
	v.width, v.height = width, height
	return d.paint(v)
}
func (d *desktop) CloseSurface(id nativeapp.SurfaceID) error {
	v := d.view(id)
	if v == nil || !v.live {
		return nil
	}
	v.live = false
	v.dirty = false
	v.controller.Blur()
	if v.published {
		d.retire = append(d.retire, v.texture)
		v.published = false
	}
	if d.focused == id {
		d.focused = 0
	}
	if p := d.view(6); p != nil && p.live {
		return d.paint(p)
	}
	return nil
}
func (d *desktop) Close() error {
	d.closed = true
	d.portraitCache = nil
	var errs []error
	for _, p := range d.painters {
		errs = append(errs, p.Close())
	}
	for _, v := range d.views {
		errs = append(errs, v.controller.Close())
	}
	return errors.Join(errs...)
}

func (d *desktop) Handle(id nativeapp.SurfaceID, e nativeapp.Event) error {
	if id == 0 && e.Kind == nativeapp.KeyboardCancel {
		for _, v := range d.views {
			if err := d.Handle(v.spec.id, e); err != nil {
				return err
			}
		}
		return nil
	}
	v := d.view(id)
	if d.closed || v == nil || !v.live {
		return nil
	}
	before := make([]nativeui.State, len(v.controls))
	for i, c := range v.controls {
		before[i] = v.controller.Decorate(c).State
	}
	a := v.controller.Handle(e)
	changed := a.Activated || a.Changed || a.ChangedFocus
	for i, c := range v.controls {
		if before[i] != v.controller.Decorate(c).State {
			changed = true
			break
		}
	}
	if a.Changed && strings.HasPrefix(a.ID, "nutrient-") {
		i, _ := strconv.Atoi(strings.TrimPrefix(a.ID, "nutrient-"))
		if i >= 0 && i < len(d.state.Nutrients) {
			d.state.Nutrients[i] = a.Value
		}
	}
	if a.Activated {
		switch {
		case a.ID == "week-prev":
			d.state.Week--
		case a.ID == "week-next":
			d.state.Week++
		case strings.HasPrefix(a.ID, "day-"):
			d.state.Day, _ = strconv.Atoi(strings.TrimPrefix(a.ID, "day-"))
		case strings.HasPrefix(a.ID, "record-tab-"):
			d.state.RecordTab, _ = strconv.Atoi(strings.TrimPrefix(a.ID, "record-tab-"))
		case a.ID == "access":
			d.state.Access = !d.state.Access
		case a.ID == "archive-sync":
			d.state.ArchiveSync = !d.state.ArchiveSync
		case a.ID == "page-prev":
			if id >= 3 && id <= 5 {
				d.state.Pages[int(id)-3] = (d.state.Pages[int(id)-3] + 2) % 3
			}
		case a.ID == "page-next":
			if id >= 3 && id <= 5 {
				d.state.Pages[int(id)-3] = (d.state.Pages[int(id)-3] + 1) % 3
			}
		case a.ID == "incoming":
			d.state.Outgoing = false
			d.state.Message = 0
		case a.ID == "outgoing":
			d.state.Outgoing = true
			d.state.Message = 0
		case a.ID == "message-next":
			d.state.Message = (d.state.Message + 1) % 4
		case strings.HasPrefix(a.ID, "open-"):
			target, _ := strconv.Atoi(strings.TrimPrefix(a.ID, "open-"))
			if err := d.open(nativeapp.SurfaceID(target)); err != nil {
				return err
			}
		}
	}
	if changed {
		return d.paint(v)
	}
	return nil
}

func loadSkin(value string) (skin.Skin, error) {
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
	selection := flag.String("skin", "merrick", "skin ID or JSON package")
	snapshotDir := flag.String("snapshot-dir", "", "write one PNG per surface and exit")
	portraitPath := flag.String("portrait", "", "optional PNG/JPEG portrait for the fictional archive")
	flag.Parse()
	selected, err := loadSkin(*selection)
	if err != nil {
		return err
	}
	d := &desktop{selected: selected}
	if *portraitPath != "" {
		f, err := os.Open(*portraitPath)
		if err != nil {
			return err
		}
		portrait, _, decodeErr := image.Decode(f)
		closeErr := f.Close()
		if decodeErr != nil {
			return decodeErr
		}
		if closeErr != nil {
			return closeErr
		}
		d.portrait = portrait
	}
	if *snapshotDir != "" {
		if err := os.MkdirAll(*snapshotDir, 0755); err != nil {
			return err
		}
		if err := d.Start(nativeapp.Host{Version: nativeapp.Version, MaxSurfaceWidth: 1920, MaxSurfaceHeight: 1080, MaxSurfaces: 8}); err != nil {
			return err
		}
		defer d.Close()
		for _, v := range d.views {
			if !v.live {
				continue
			}
			f, err := os.Create(filepath.Join(*snapshotDir, v.spec.key+".png"))
			if err != nil {
				return err
			}
			writeErr := png.Encode(f, v.pixels)
			closeErr := f.Close()
			if writeErr != nil {
				return writeErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return nil
	}
	return nativeapp.Serve(context.Background(), d, os.Stdin, os.Stdout)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "merrick desktop:", err)
		os.Exit(1)
	}
}

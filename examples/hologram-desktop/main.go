// Hologram Desktop is an interactive, fictional disk-management study.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"sort"
	"strings"
	"time"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

const initialWidth, initialHeight, minimumWidth, minimumHeight = 1440, 1000, 900, 650

type volume struct {
	id, name, filesystem, status string
	capacityMB                   float64
}

var volumes = [...]volume{
	{"efi", "(Disk 0 partition 1)", "FAT32", "Healthy (EFI System Partition)", 200},
	{"system", "(C:)", "NTFS", "Healthy (Boot, Page File, Primary)", 1861.97 * 1024},
	{"recovery", "(Disk 0 partition 4)", "NTFS", "Healthy (Recovery Partition)", 854},
}

type diskState struct {
	Selected, Sort, Menu string
	Descending, Details  bool
	Grid, Help           bool
	Visible              [3]bool
	Sample               int
}

type diskDesktop struct {
	selected               skin.Skin
	state                  diskState
	host                   nativeapp.Host
	theme                  nativeui.Theme
	painters               map[int]*nativeui.Painter
	controller             nativeui.Controller
	controls               []nativeui.Control
	pixels                 *image.RGBA
	background             *image.RGBA
	layers                 [4]diskLayer
	width, height          int
	revision               uint64
	dirty, published       bool
	closed, retired, focus bool
}

func (d *diskDesktop) Manifest() nativeapp.Manifest {
	return nativeapp.Manifest{ID: "dev.worldr.hologram-desktop", Name: "Hologram Disk Management", Description: "Interactive fictional volumes and a luminous partition map", Skins: true}
}

func (d *diskDesktop) Start(host nativeapp.Host) error {
	if host.MaxSurfaces < 1 || host.MaxSurfaceWidth < minimumWidth || host.MaxSurfaceHeight < minimumHeight {
		return fmt.Errorf("disk management needs at least one %dx%d surface", minimumWidth, minimumHeight)
	}
	d.host = host
	d.width, d.height = min(initialWidth, host.MaxSurfaceWidth), min(initialHeight, host.MaxSurfaceHeight)
	d.state = diskState{Selected: "system", Grid: true, Visible: [3]bool{true, true, true}}
	if d.selected.ID == "" {
		var err error
		d.selected, err = skin.Builtin("hologram")
		if err != nil {
			return err
		}
	}
	if err := d.setTheme(d.selected); err != nil {
		return err
	}
	return d.paint()
}

func (d *diskDesktop) setTheme(selected skin.Skin) error {
	theme, err := nativeui.ThemeFromSkin(selected)
	if err != nil {
		return err
	}
	if err := d.controller.SetTheme(theme); err != nil {
		return err
	}
	for _, p := range d.painters {
		_ = p.Close()
	}
	d.painters = make(map[int]*nativeui.Painter)
	d.theme, d.selected = theme, selected.Clone()
	d.clearRenderCache()
	return nil
}

func (d *diskDesktop) SetSkin(selected skin.Skin) error {
	if err := selected.Validate(); err != nil {
		return err
	}
	if d.closed {
		return nil
	}
	if err := d.setTheme(selected); err != nil {
		return err
	}
	if d.pixels == nil {
		return nil
	}
	return d.paint()
}

func (d *diskDesktop) Update(time.Duration) error { return nil }

func (d *diskDesktop) Snapshot() nativeapp.Snapshot {
	if d.closed {
		if d.published && !d.retired {
			d.retired = true
			return nativeapp.Snapshot{RetireTextures: []nativeapp.ResourceID{1}}
		}
		return nativeapp.Snapshot{}
	}
	if d.pixels == nil {
		return nativeapp.Snapshot{}
	}
	s := nativeapp.Snapshot{Surfaces: []nativeapp.Surface{{ID: 1, Key: "volumes", Title: "Disk Management", Texture: 1, MinWidth: minimumWidth, MinHeight: minimumHeight, FrameStyle: nativeapp.FrameDefault, Semantics: d.controller.Semantics()}}}
	if d.dirty {
		d.revision++
		s.Textures = []nativeapp.TextureUpdate{{ID: 1, Revision: d.revision, Width: d.width, Height: d.height, Rect: nativeapp.Rect{Width: d.width, Height: d.height}, Pixels: append([]byte(nil), d.pixels.Pix...)}}
		d.dirty, d.published = false, true
	}
	return s
}

func (d *diskDesktop) Resize(id nativeapp.SurfaceID, width, height int) error {
	if id != 1 || d.closed {
		return nil
	}
	width = max(minimumWidth, min(d.host.MaxSurfaceWidth, width))
	height = max(minimumHeight, min(d.host.MaxSurfaceHeight, height))
	if d.width == width && d.height == height {
		return nil
	}
	// Cancel pressed controls before their hit rectangles move. Keep focus on
	// the same stable control so keyboard navigation survives the resize.
	d.controller.Handle(nativeapp.Event{Kind: nativeapp.PointerCancel})
	d.width, d.height = width, height
	d.clearRenderCache()
	return d.paint()
}

func (d *diskDesktop) Focus(id nativeapp.SurfaceID) error {
	if d.closed {
		return nil
	}
	d.focus = id == 1
	if !d.focus {
		return d.Handle(0, nativeapp.Event{Kind: nativeapp.KeyboardCancel})
	}
	return nil
}

func (d *diskDesktop) CloseSurface(id nativeapp.SurfaceID) error {
	if id == 1 {
		d.closed, d.dirty = true, false
		d.controller.Blur()
	}
	return nil
}

func (d *diskDesktop) Close() error {
	d.closed = true
	errs := []error{d.controller.Close()}
	for _, p := range d.painters {
		errs = append(errs, p.Close())
	}
	d.painters = nil
	d.clearRenderCache()
	return errors.Join(errs...)
}

func (d *diskDesktop) usedMB(index int) float64 {
	if index == 0 {
		return 32
	}
	if index == 2 {
		return 612
	}
	return []float64{648.25, 657.82, 643.70, 669.14}[d.state.Sample%4] * 1024
}

func (d *diskDesktop) ordered() []int {
	var ids []int
	for i, visible := range d.state.Visible {
		if visible {
			ids = append(ids, i)
		}
	}
	if d.state.Sort == "" {
		return ids
	}
	sort.SliceStable(ids, func(a, b int) bool {
		i, j := ids[a], ids[b]
		cmp := 0
		numeric := func(x, y float64) {
			if x < y {
				cmp = -1
			} else if x > y {
				cmp = 1
			}
		}
		switch d.state.Sort {
		case "capacity":
			numeric(volumes[i].capacityMB, volumes[j].capacityMB)
		case "free":
			numeric(volumes[i].capacityMB-d.usedMB(i), volumes[j].capacityMB-d.usedMB(j))
		case "percent":
			numeric(d.usedMB(j)/volumes[j].capacityMB, d.usedMB(i)/volumes[i].capacityMB)
		case "filesystem":
			cmp = strings.Compare(volumes[i].filesystem, volumes[j].filesystem)
		case "status":
			cmp = strings.Compare(volumes[i].status, volumes[j].status)
		case "volume":
			cmp = strings.Compare(volumes[i].name, volumes[j].name)
		}
		if cmp == 0 {
			cmp = i - j
		}
		if d.state.Descending {
			return cmp > 0
		}
		return cmp < 0
	})
	return ids
}

func (d *diskDesktop) chooseVisible() {
	for i, v := range volumes {
		if v.id == d.state.Selected && d.state.Visible[i] {
			return
		}
	}
	d.state.Selected = ""
	if order := d.ordered(); len(order) > 0 {
		d.state.Selected = volumes[order[0]].id
	}
}

func (d *diskDesktop) activate(id string) {
	id = strings.TrimPrefix(id, "popup-")
	switch {
	case strings.HasPrefix(id, "row-") || strings.HasPrefix(id, "block-"):
		d.state.Selected = id[strings.IndexByte(id, '-')+1:]
	case strings.HasPrefix(id, "sort-"):
		column := strings.TrimPrefix(id, "sort-")
		if d.state.Sort == column {
			d.state.Descending = !d.state.Descending
		} else {
			d.state.Sort, d.state.Descending = column, false
		}
	case strings.HasPrefix(id, "filter-"):
		for i, v := range volumes {
			if id == "filter-"+v.id {
				d.state.Visible[i] = !d.state.Visible[i]
			}
		}
		d.chooseVisible()
	case strings.HasPrefix(id, "menu-"):
		menu := strings.TrimPrefix(id, "menu-")
		if d.state.Menu == menu {
			menu = ""
		}
		d.state.Menu = menu
	case id == "refresh":
		d.state.Sample = (d.state.Sample + 1) % 4
	case id == "view":
		d.state.Details = !d.state.Details
	case id == "grid":
		d.state.Grid = !d.state.Grid
	case id == "help":
		d.state.Help = !d.state.Help
	case id == "help-close":
		d.state.Help = false
	case id == "reset":
		d.state.Sort, d.state.Descending = "", false
		d.state.Visible = [3]bool{true, true, true}
		d.state.Selected, d.state.Sample = "system", 0
	case id == "previous" || id == "next":
		order := d.ordered()
		if len(order) > 0 {
			at := 0
			for i, index := range order {
				if volumes[index].id == d.state.Selected {
					at = i
				}
			}
			if id == "previous" {
				at += len(order) - 1
			} else {
				at++
			}
			d.state.Selected = volumes[order[at%len(order)]].id
		}
	}
	if !strings.HasPrefix(id, "menu-") {
		d.state.Menu = ""
	}
}

func (d *diskDesktop) Handle(id nativeapp.SurfaceID, event nativeapp.Event) error {
	if d.closed || id != 1 && !(id == 0 && event.Kind == nativeapp.KeyboardCancel) {
		return nil
	}
	before := make([]nativeui.State, len(d.controls))
	for i, control := range d.controls {
		before[i] = d.controller.Decorate(control).State
	}
	a := d.controller.Handle(event)
	changed := a.Activated || a.Changed || a.ChangedFocus
	for i, control := range d.controls {
		if before[i] != d.controller.Decorate(control).State {
			changed = true
		}
	}
	if a.Activated {
		d.activate(a.ID)
	}
	if event.Kind == nativeapp.KeyInput && event.Pressed {
		if (event.Key == "Escape" || event.Keycode == 1) && (d.state.Menu != "" || d.state.Help) {
			d.state.Menu, d.state.Help, changed = "", false, true
		}
		focused := d.controller.FocusedID()
		if strings.HasPrefix(focused, "row-") || strings.HasPrefix(focused, "block-") {
			switch {
			case event.Key == "ArrowUp" || event.Keycode == 103:
				d.activate("previous")
				changed = true
			case event.Key == "ArrowDown" || event.Keycode == 108:
				d.activate("next")
				changed = true
			}
			if changed && d.state.Selected != "" {
				d.controller.Focus("row-" + d.state.Selected)
			}
		}
	}
	if changed {
		return d.paint()
	}
	return nil
}

func loadSkin(value string) (skin.Skin, error) {
	if selected, err := skin.Builtin(value); err == nil {
		return selected, nil
	}
	f, err := os.Open(value)
	if err != nil {
		return skin.Skin{}, err
	}
	defer f.Close()
	return skin.Decode(f)
}

func run() error {
	selection := flag.String("skin", "hologram", "skin ID or JSON package path")
	snapshot := flag.String("snapshot", "", "render the disk-management study to a PNG and exit")
	flag.Parse()
	selected, err := loadSkin(*selection)
	if err != nil {
		return err
	}
	d := &diskDesktop{selected: selected}
	if *snapshot == "" {
		return nativeapp.Serve(context.Background(), d, os.Stdin, os.Stdout)
	}
	defer d.Close()
	if err := d.Start(nativeapp.Host{Version: nativeapp.Version, MaxSurfaces: 1, MaxSurfaceWidth: initialWidth, MaxSurfaceHeight: initialHeight}); err != nil {
		return err
	}
	f, err := os.Create(*snapshot)
	if err != nil {
		return err
	}
	err = png.Encode(f, d.pixels)
	return errors.Join(err, f.Close())
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hologram desktop:", err)
		os.Exit(1)
	}
}

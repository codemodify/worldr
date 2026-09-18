package projectapp

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	minWidth     = 640
	minHeight    = 360
	maxExtent    = 4096
	contentTop   = 156
	footerHeight = 32
	rowHeight    = 21
	textCell     = 9
)

var toolbarButtons = [...]struct {
	label string
	rect  image.Rectangle
}{
	{"Up", image.Rect(14, 60, 72, 87)},
	{"Refresh", image.Rect(80, 60, 176, 87)},
	{"Open", image.Rect(184, 60, 254, 87)},
	{"Copy path", image.Rect(262, 60, 374, 87)},
	{"Terminal Here", image.Rect(382, 60, 522, 87)},
	{"Search", image.Rect(530, 60, 626, 87)},
}

type browserRenderer struct {
	image       *image.RGBA
	texture     *render.Texture
	face        font.Face
	small       font.Face
	prior       []byte
	ui, uiSmall *nativeui.Painter
	paintErr    error
}

func newBrowserRenderer(width, height int) (*browserRenderer, error) {
	parsed, err := opentype.Parse(gomono.TTF)
	if err != nil {
		return nil, err
	}
	r := &browserRenderer{}
	theme := nativeui.Cinematic()
	r.ui, err = nativeui.NewPainter(theme)
	if err != nil {
		return nil, err
	}
	theme.FontSize = 12
	r.uiSmall, err = nativeui.NewPainter(theme)
	if err != nil {
		r.close()
		return nil, err
	}
	r.face, err = opentype.NewFace(parsed, &opentype.FaceOptions{Size: 15, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		r.close()
		return nil, err
	}
	r.small, err = opentype.NewFace(parsed, &opentype.FaceOptions{Size: 12, DPI: 72, Hinting: font.HintingFull})
	if err == nil {
		err = r.resize(width, height)
	}
	if err != nil {
		r.close()
		return nil, err
	}
	return r, nil
}

func (r *browserRenderer) close() {
	r.ui.Close()
	r.uiSmall.Close()
	if r.face != nil {
		_ = r.face.Close()
		r.face = nil
	}
	if r.small != nil {
		_ = r.small.Close()
		r.small = nil
	}
}

func (r *browserRenderer) resize(width, height int) error {
	if width < minWidth || height < minHeight || width > maxExtent || height > maxExtent {
		return fmt.Errorf("project browser dimensions must be within %dx%d and %dx%d", minWidth, minHeight, maxExtent, maxExtent)
	}
	if r.image != nil && r.image.Rect.Dx() == width && r.image.Rect.Dy() == height {
		return nil
	}
	r.image = image.NewRGBA(image.Rect(0, 0, width, height))
	r.fill(r.image.Rect, 0x0e1b25)
	r.prior = nil
	if r.texture == nil {
		texture, err := render.NewTexture(width, height, r.image.Pix)
		if err != nil {
			return err
		}
		r.texture = texture
		return nil
	}
	return r.texture.Replace(width, height, r.image.Pix)
}

func (r *browserRenderer) split() int { return max(240, min(360, r.image.Rect.Dx()/3)) }

func rgba(c uint32) color.RGBA {
	return color.RGBA{R: byte(c >> 16), G: byte(c >> 8), B: byte(c), A: 255}
}

func (r *browserRenderer) fill(rect image.Rectangle, c uint32) {
	draw.Draw(r.image, rect, image.NewUniform(rgba(c)), image.Point{}, draw.Src)
}

func safeLabel(text string) string {
	var out strings.Builder
	for _, char := range text {
		if unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			fmt.Fprintf(&out, "\\u%04x", char)
		} else {
			out.WriteRune(char)
		}
	}
	return out.String()
}

func (r *browserRenderer) label(rect image.Rectangle, x, baseline int, text string, c uint32, small bool) {
	rect = rect.Intersect(r.image.Rect)
	rect.Min.X = max(rect.Min.X, x)
	if rect.Empty() || r.paintErr != nil {
		return
	}
	painter := r.ui
	if small {
		painter = r.uiSmall
	}
	r.paintErr = painter.DrawLabel(r.image, rect, safeLabel(text), rgba(c))
}

func (r *browserRenderer) previewLabel(rect image.Rectangle, x, baseline int, text string, c uint32, small bool) {
	rect = rect.Intersect(r.image.Rect)
	if rect.Empty() {
		return
	}
	face := r.face
	if small {
		face = r.small
	}
	// Bound shaping even when a filename or error contains an enormous label.
	limit := max(1, rect.Dx()/6+2)
	chars := []rune(safeLabel(text))
	if len(chars) > limit {
		chars = append(chars[:limit-1], '…')
	}
	drawer := font.Drawer{Dst: r.image.SubImage(rect).(*image.RGBA), Src: image.NewUniform(rgba(c)), Face: face,
		Dot: fixed.P(x, baseline)}
	drawer.DrawString(string(chars))
}

// visibleLine expands tabs and replaces controls before clipping to columns.
// It intentionally uses a monospace rune grid; font fallback, grapheme shaping,
// syntax highlighting, and editing are outside this read-only text preview.
func visibleLine(text string, first, columns int) string {
	var out strings.Builder
	column, end := 0, first+columns
	for _, char := range text {
		if column >= end {
			break
		}
		if char == '\t' {
			advance := 4 - column%4
			for i := 0; i < advance; i++ {
				if column >= first && column < end {
					out.WriteByte(' ')
				}
				column++
			}
			continue
		}
		if unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			char = '�'
		}
		if column >= first {
			out.WriteRune(char)
		}
		column++
	}
	return out.String()
}

func (r *browserRenderer) paint(p *Provider) error {
	r.paintErr = nil
	w, h, split := r.image.Rect.Dx(), r.image.Rect.Dy(), r.split()
	r.fill(r.image.Rect, 0x0e1b25)
	r.fill(image.Rect(0, 0, w, contentTop-24), 0x11232f)
	r.fill(image.Rect(0, contentTop-24, split, h-footerHeight), 0x0a1720)
	r.fill(image.Rect(split, contentTop-24, split+1, h-footerHeight), 0x28444e)
	accent := uint32(0x3b6875)
	if p.focused {
		accent = 0x70e6ec
	}
	r.fill(image.Rect(0, 0, w, 2), accent)
	title := "PROJECT  /  " + filepath.Base(p.root)
	if intent := p.openIntent.title(); intent != "" {
		title += "  /  " + intent
	}
	r.label(image.Rect(14, 8, w-14, 34), 14, 28, title, 0xa0eff2, false)
	if p.searchActive || p.query != "" {
		rect := searchRect(w)
		r.label(image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+109, rect.Max.Y), rect.Min.X, 49, "Find filename:", 0x91a9b7, true)
		field := searchFieldRect(w)
		if r.paintErr == nil {
			r.paintErr = r.uiSmall.DrawField(r.image, field, p.searchField, "", p.searchActive && p.focused)
		}
	} else {
		r.label(image.Rect(14, 34, w-14, 55), 14, 49, filepath.Join(p.root, p.directory), 0x91a9b7, true)
	}
	for i, button := range toolbarButtons {
		c := uint32(0xb5dce2)
		if i == 0 && p.directory == "" || i == 2 && p.selected < 0 || i == 4 && (p.loadingDirectory || p.openingTerminal) {
			c = 0x617480
		}
		r.fill(button.rect, 0x1a3542)
		r.label(button.rect, button.rect.Min.X+10, button.rect.Min.Y+18, button.label, c, true)
	}
	for _, button := range operationButtons {
		_, label := p.operationButton(button.action)
		if label == "" {
			label = button.label
		}
		c := uint32(0xb5dce2)
		if p.operationPending || p.loadingDirectory || button.action == undoFileAction && len(p.fileHistory) == 0 || button.action != createFolder && button.action != undoFileAction && p.selected < 0 {
			c = 0x617480
		}
		r.fill(button.rect, 0x1a3542)
		r.label(button.rect, button.rect.Min.X+10, button.rect.Min.Y+18, label, c, true)
	}
	format := "UTF-8"
	if p.selected >= 0 && p.selected < len(p.entries) && p.entries[p.selected].kind == fileEntry {
		if IsPhotoPath(p.entries[p.selected].name) {
			format = "PHOTO"
		} else if IsModelPath(p.entries[p.selected].name) {
			format = "MODEL"
		} else if IsDatasetPath(p.entries[p.selected].name) {
			format = "DATA"
		} else if IsNotePath(p.entries[p.selected].name) {
			format = "NOTE"
		} else if videoPath(p.entries[p.selected].name) {
			format = "VIDEO"
		}
	}
	if w >= 840 {
		format = "PREVIEW  ·  " + format
	}
	if w >= 710 {
		r.label(image.Rect(640, 61, w-10, 85), 640, 79, format, 0x7295a5, true)
	}
	listTitle := fmt.Sprintf("FILES  %d", len(p.entries))
	if intent := p.openIntent.title(); intent != "" {
		listTitle = fmt.Sprintf("%s  /  %d", intent, len(p.entries))
	}
	if p.query != "" {
		listTitle = fmt.Sprintf("MATCHES  %d / %d", len(p.entries), len(p.allEntries))
	}
	if p.loadingDirectory {
		listTitle = "FILES  /  LOADING"
	} else if p.listTruncated {
		listTitle += "+  /  LIMIT 5000"
	}
	listAccent, previewAccent := uint32(0x72a1af), uint32(0x72a1af)
	if p.focused {
		if p.previewActive {
			previewAccent = 0x9af6ee
		} else {
			listAccent = 0x9af6ee
		}
	}
	r.label(image.Rect(14, contentTop-23, split-10, contentTop), 14, contentTop-7, listTitle, listAccent, true)
	previewTitle := "PREVIEW"
	if p.previewPath != "" {
		previewTitle = filepath.Base(p.previewPath)
	}
	if p.loadingFile {
		previewTitle += "  /  LOADING"
	} else if p.previewTruncated {
		previewTitle += "  /  FIRST 1 MiB"
	}
	r.label(image.Rect(split+14, contentTop-23, w-10, contentTop), split+14, contentTop-7, previewTitle, previewAccent, true)
	listBounds := image.Rect(0, contentTop, split, h-footerHeight)
	for row := 0; row < p.rows(); row++ {
		index := p.listTop + row
		if index >= len(p.entries) {
			break
		}
		item, top := p.entries[index], contentTop+row*rowHeight
		if index == p.selected {
			r.fill(image.Rect(0, top, split, top+rowHeight).Intersect(listBounds), 0x173c4a)
			r.fill(image.Rect(0, top, 3, top+rowHeight).Intersect(listBounds), 0x69dce2)
		}
		mark, c := " ", uint32(0xc6d6e0)
		switch item.kind {
		case directoryEntry:
			mark, c = ">", 0x8eeced
		case symlinkEntry:
			mark, c = "@", 0xbba4dc
		case specialEntry:
			mark, c = "!", 0xb8a383
		}
		if thumb := p.thumbnailFor(item); thumb != nil {
			position := image.Pt(12+(thumbnailSide-thumb.Rect.Dx())/2, top+(rowHeight-thumb.Rect.Dy())/2)
			draw.Draw(r.image, thumb.Rect.Add(position).Intersect(listBounds), thumb, image.Point{}, draw.Over)
			r.label(image.Rect(38, top, split-10, top+rowHeight).Intersect(listBounds), 38, top+16, item.name, c, false)
		} else {
			r.label(image.Rect(12, top, split-10, top+rowHeight).Intersect(listBounds), 12, top+16, mark+" "+item.name, c, false)
		}
	}
	previewBounds := image.Rect(split+1, contentTop, w, h-footerHeight)
	gutter := split + 67
	if len(p.preview.starts) > 0 && !p.loadingFile {
		r.fill(image.Rect(split+1, contentTop, gutter-9, h-footerHeight), 0x0c1822)
		for row := 0; row < p.rows(); row++ {
			index := p.previewTop + row
			if index >= len(p.preview.starts) {
				break
			}
			top := contentTop + row*rowHeight
			number := strconv.Itoa(index + 1)
			r.previewLabel(image.Rect(split+4, top, gutter-14, top+rowHeight), gutter-14-len(number)*7, top+16, number, 0x668592, true)
			line := visibleLine(p.preview.line(index), p.previewLeft, max(1, (w-gutter-12)/textCell))
			r.previewLabel(image.Rect(gutter, top, w-10, top+rowHeight).Intersect(previewBounds), gutter, top+16, line, 0xd1dde7, false)
		}
	}
	if p.message != "" || p.loadingDirectory || p.loadingFile {
		message := p.message
		if p.loadingDirectory {
			message = "Loading directory…"
		} else if p.loadingFile {
			message = "Loading text preview…"
			if IsPhotoPath(p.previewPath) {
				message = "Opening photo…"
			} else if IsModelPath(p.previewPath) {
				message = "Opening model…"
			} else if IsDatasetPath(p.previewPath) {
				message = "Opening research dataset…"
			} else if IsNotePath(p.previewPath) {
				message = "Opening native note…"
			} else if videoPath(p.previewPath) {
				message = "Opening video…"
			}
		}
		r.label(image.Rect(split+18, contentTop+20, w-16, contentTop+65).Intersect(previewBounds), split+18, contentTop+44, message, 0x92afbc, true)
	}
	r.fill(image.Rect(0, h-footerHeight, w, h), 0x122832)
	footer := "↑↓ select · Enter open · Ctrl+Shift+Enter terminal · Backspace up · F5 refresh"
	if p.previewTruncated {
		footer = "Preview limited to the first 1 MiB.  ·  Wheel / PgUp / PgDn scroll  ·  ← → pan"
	} else if p.listTruncated {
		footer = "Directory limited to 5000 entries.  ·  No recursive indexing  ·  Refresh to reload"
	} else if p.previewActive && len(p.preview.starts) > 0 {
		footer = fmt.Sprintf("Line %d / %d  ·  Column %d  ·  Wheel / PgUp / PgDn scroll  ·  ← → pan  ·  Tab files", p.previewTop+1, max(1, len(p.preview.starts)), p.previewLeft+1)
	}
	if p.openingTerminal {
		footer = "Opening terminal directory…"
	}
	if instruction := p.openIntent.instruction(); instruction != "" {
		footer = instruction
	}
	if p.searchActive {
		footer = "Search this folder's listed filenames · Enter keeps filter · Esc clears · Ctrl+Backspace clears text"
	} else if p.query != "" {
		footer = "Filename filter active · Ctrl+F edits · Esc in search clears · Auto-refresh every 2s"
	}
	if p.notice != "" {
		footer = p.notice
	}
	r.label(image.Rect(14, h-footerHeight+3, w-14, h-3), 14, h-11, footer, 0x9cbdc8, true)
	if p.dialog != nil {
		r.paintOperationDialog(p)
	}
	if r.paintErr != nil {
		return r.paintErr
	}
	return r.uploadChangedRows()
}

func (r *browserRenderer) paintOperationDialog(p *Provider) {
	d := p.dialog
	rect := operationDialogRect(r.image.Rect.Dx(), r.image.Rect.Dy())
	r.fill(rect, 0x284b58)
	r.fill(rect.Inset(1), 0x0e2531)
	r.fill(image.Rect(rect.Min.X+1, rect.Min.Y+1, rect.Max.X-1, rect.Min.Y+3), 0x70e6ec)
	r.label(image.Rect(rect.Min.X+18, rect.Min.Y+13, rect.Max.X-18, rect.Min.Y+43), rect.Min.X+18, rect.Min.Y+34, d.title, 0xb5edf0, false)
	field := image.Rect(rect.Min.X+18, rect.Min.Y+54, rect.Max.X-18, rect.Min.Y+88)
	if d.operation.action == trashItem || d.operation.action == restoreTrashItem {
		r.label(field, field.Min.X+8, field.Min.Y+23, d.field.Text(), 0xe0eeee, false)
	} else if r.paintErr == nil {
		r.paintErr = r.ui.DrawField(r.image, field, d.field, "", p.focused)
	}
	hint := "Enter confirms · Esc cancels · Ctrl+A selects the name"
	if d.operation.action == trashItem {
		hint = "Recover with Undo file, or open .worldr-trash and choose Restore."
	} else if d.operation.action == restoreTrashItem {
		hint = "Existing destinations are never overwritten. Ctrl+Shift+R also restores."
	}
	if p.notice != "" {
		hint = p.notice
	}
	r.label(image.Rect(rect.Min.X+18, rect.Min.Y+93, rect.Max.X-18, rect.Min.Y+135), rect.Min.X+18, rect.Min.Y+114, hint, 0x9cbdc8, true)
	for i, label := range []string{"Confirm", "Cancel"} {
		button := image.Rect(rect.Min.X+18+i*122, rect.Max.Y-46, rect.Min.X+130+i*122, rect.Max.Y-16)
		r.fill(button, 0x235666)
		r.label(button, button.Min.X+14, button.Min.Y+20, label, 0xb5edf0, true)
	}
}

func (r *browserRenderer) uploadChangedRows() error {
	width, height := r.image.Rect.Dx(), r.image.Rect.Dy()
	first, last := height, -1
	for y := 0; y < height; y++ {
		start, end := y*r.image.Stride, (y+1)*r.image.Stride
		if len(r.prior) != len(r.image.Pix) || !bytes.Equal(r.prior[start:end], r.image.Pix[start:end]) {
			first, last = min(first, y), y
		}
	}
	if last < first {
		return nil
	}
	rect := image.Rect(0, first, width, last+1)
	if err := r.texture.Update(rect, r.image.Pix[first*r.image.Stride:(last+1)*r.image.Stride]); err != nil {
		return err
	}
	r.prior = append(r.prior[:0], r.image.Pix...)
	return nil
}

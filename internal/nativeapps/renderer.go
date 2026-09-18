// Package nativeapps presents engine-native applications without a display
// protocol. The terminal uses real PTY cells and the same retained content
// surfaces as the rest of the spatial workspace.
package nativeapps

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"unicode"

	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/terminal"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	cellWidth    = 10
	cellHeight   = 21
	contentLeft  = 12
	contentTop   = 52
	footerHeight = 26
	minWidth     = 240
	minHeight    = 162
	maxExtent    = 4096
)

type selection struct {
	Anchor, End int
	Active      bool
}

func (s selection) contains(index int) bool {
	if !s.Active {
		return false
	}
	lo, hi := s.Anchor, s.End
	if lo > hi {
		lo, hi = hi, lo
	}
	return index >= lo && index <= hi
}

func (s selection) intersectsRow(row, cols int) bool {
	if !s.Active {
		return false
	}
	lo, hi := s.Anchor, s.End
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo < (row+1)*cols && hi >= row*cols
}

type terminalDecoration struct {
	Focused, CursorOn                bool
	Selection                        selection
	Header, Footer                   string
	Find                             *nativeui.Field
	FindCaret, FindAnchor            int
	ToolFocus                        string
	ToolRows                         []string
	FindPreedit                      string
	FindPreeditBegin, FindPreeditEnd int32
}

type terminalRenderer struct {
	image       *image.RGBA
	texture     *render.Texture
	faces       [4]font.Face
	fallback    *terminalFontFallback
	ui          *nativeui.Painter
	previous    terminal.Snapshot
	decoration  terminalDecoration
	initialized bool
}

func newTerminalRenderer(width, height int) (*terminalRenderer, error) {
	r := &terminalRenderer{}
	theme := nativeui.Cinematic()
	theme.FontSize = 12
	theme.Padding = 4
	theme.CornerCut = 3
	var err error
	r.ui, err = nativeui.NewPainter(theme)
	if err != nil {
		return nil, err
	}
	for i, data := range [][]byte{gomono.TTF, gomonobold.TTF, gomonoitalic.TTF, gomonobolditalic.TTF} {
		parsed, err := opentype.Parse(data)
		if err != nil {
			r.close()
			return nil, err
		}
		face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: 16, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			r.close()
			return nil, err
		}
		r.faces[i] = face
	}
	r.fallback = newTerminalFontFallback(r.faces)
	if err := r.resize(width, height); err != nil {
		r.close()
		return nil, err
	}
	return r, nil
}

func (r *terminalRenderer) close() {
	r.fallback.close()
	for _, face := range r.faces {
		if face != nil {
			_ = face.Close()
		}
	}
	r.ui.Close()
	r.faces = [4]font.Face{}
}

func (r *terminalRenderer) resize(width, height int) error {
	if width < minWidth || height < minHeight || width > maxExtent || height > maxExtent {
		return fmt.Errorf("native terminal image must be within %dx%d and %dx%d", minWidth, minHeight, maxExtent, maxExtent)
	}
	if r.image != nil && r.image.Rect.Dx() == width && r.image.Rect.Dy() == height {
		return nil
	}
	r.image = image.NewRGBA(image.Rect(0, 0, width, height))
	fill(r.image, r.image.Bounds(), rgb(0x101c26))
	if r.texture == nil {
		texture, err := render.NewTexture(width, height, r.image.Pix)
		if err != nil {
			return err
		}
		r.texture = texture
	} else if err := r.texture.Replace(width, height, r.image.Pix); err != nil {
		return err
	}
	r.initialized = false
	return nil
}

func terminalGrid(width, height int) (cols, rows int) {
	return (width - 2*contentLeft) / cellWidth, (height - contentTop - footerHeight) / cellHeight
}

func rgb(value uint32) color.RGBA {
	return color.RGBA{R: byte(value >> 16), G: byte(value >> 8), B: byte(value), A: 255}
}
func terminalColor(c terminal.Color) color.RGBA { return color.RGBA{R: c.R, G: c.G, B: c.B, A: 255} }
func fill(dst *image.RGBA, rect image.Rectangle, c color.RGBA) {
	draw.Draw(dst, rect, image.NewUniform(c), image.Point{}, draw.Src)
}

func (r *terminalRenderer) paint(snapshot terminal.Snapshot, decoration terminalDecoration) error {
	if snapshot.Cols <= 0 || snapshot.Rows <= 0 || len(snapshot.Cells) != snapshot.Cols*snapshot.Rows {
		return fmt.Errorf("invalid terminal cell snapshot")
	}
	cols, rows := terminalGrid(r.image.Rect.Dx(), r.image.Rect.Dy())
	if snapshot.Cols > cols || snapshot.Rows > rows {
		return fmt.Errorf("terminal grid %dx%d exceeds content image", snapshot.Cols, snapshot.Rows)
	}
	full := !r.initialized || snapshot.Cols != r.previous.Cols || snapshot.Rows != r.previous.Rows
	dirty := image.Rectangle{}
	if full {
		fill(r.image, r.image.Bounds(), rgb(0x101c26))
		dirty = r.image.Bounds()
	}
	selectionChanged := decoration.Selection != r.decoration.Selection
	cursorChanged := snapshot.Cursor != r.previous.Cursor || decoration.Focused != r.decoration.Focused || decoration.CursorOn != r.decoration.CursorOn || snapshot.Exited != r.previous.Exited || snapshot.ScrollOffset != r.previous.ScrollOffset
	for row := 0; row < snapshot.Rows; row++ {
		changed := full || selectionChanged && (decoration.Selection.intersectsRow(row, snapshot.Cols) || r.decoration.Selection.intersectsRow(row, snapshot.Cols))
		if (decoration.ToolRows == nil) != (r.decoration.ToolRows == nil) {
			changed = true
		}
		if decoration.ToolRows != nil && (row >= len(r.decoration.ToolRows) || decoration.ToolRows[row] != r.decoration.ToolRows[row]) {
			changed = true
		}
		if !changed && cursorChanged && (row == snapshot.Cursor.Row || row == r.previous.Cursor.Row) {
			changed = true
		}
		if !changed {
			for col := 0; col < snapshot.Cols; col++ {
				i := row*snapshot.Cols + col
				if snapshot.Cells[i] != r.previous.Cells[i] {
					changed = true
					break
				}
			}
		}
		if changed {
			r.drawRow(snapshot, decoration, row)
			dirty = dirty.Union(image.Rect(contentLeft, contentTop+row*cellHeight, contentLeft+snapshot.Cols*cellWidth, contentTop+(row+1)*cellHeight))
		}
	}
	headerChanged := full || snapshot.Title != r.previous.Title || decoration.Focused != r.decoration.Focused || decoration.Header != r.decoration.Header || decoration.Find != r.decoration.Find || decoration.FindCaret != r.decoration.FindCaret || decoration.FindAnchor != r.decoration.FindAnchor || decoration.ToolFocus != r.decoration.ToolFocus || decoration.FindPreedit != r.decoration.FindPreedit || decoration.FindPreeditBegin != r.decoration.FindPreeditBegin || decoration.FindPreeditEnd != r.decoration.FindPreeditEnd
	footerChanged := full || snapshot.Exited != r.previous.Exited || snapshot.ExitError != r.previous.ExitError || snapshot.ScrollOffset != r.previous.ScrollOffset || snapshot.ScrollOffset > 0 && snapshot.ScrollbackLen != r.previous.ScrollbackLen || snapshot.MouseTracking != r.previous.MouseTracking || decoration.Footer != r.decoration.Footer
	if headerChanged || footerChanged {
		r.drawChrome(snapshot, decoration)
		if headerChanged {
			dirty = dirty.Union(image.Rect(0, 0, r.image.Rect.Dx(), contentTop))
		}
		if footerChanged {
			dirty = dirty.Union(image.Rect(0, r.image.Rect.Dy()-footerHeight, r.image.Rect.Dx(), r.image.Rect.Dy()))
		}
	}
	r.previous = snapshot
	r.decoration = decoration
	r.initialized = true
	if dirty.Empty() {
		return nil
	}
	dirty = dirty.Intersect(r.image.Bounds())
	pixels := make([]byte, dirty.Dx()*dirty.Dy()*4)
	for y := dirty.Min.Y; y < dirty.Max.Y; y++ {
		start := (y-r.image.Rect.Min.Y)*r.image.Stride + (dirty.Min.X-r.image.Rect.Min.X)*4
		copy(pixels[(y-dirty.Min.Y)*dirty.Dx()*4:][:dirty.Dx()*4], r.image.Pix[start:][:dirty.Dx()*4])
	}
	return r.texture.Update(dirty, pixels)
}

func cellColors(cell terminal.Cell, selected bool) (foreground, background color.RGBA) {
	foreground, background = terminalColor(cell.Foreground), terminalColor(cell.Background)
	if cell.Reverse {
		foreground, background = background, foreground
	}
	if selected {
		foreground, background = rgb(0xebfcff), rgb(0x285d71)
	}
	return
}

func (r *terminalRenderer) drawRow(snapshot terminal.Snapshot, decoration terminalDecoration, row int) {
	if decoration.ToolRows != nil {
		background := rgb(0x081017)
		if decoration.Selection.intersectsRow(row, snapshot.Cols) {
			background = rgb(0x285d71)
		}
		rect := image.Rect(contentLeft, contentTop+row*cellHeight, contentLeft+snapshot.Cols*cellWidth, contentTop+(row+1)*cellHeight)
		fill(r.image, rect, background)
		_ = r.ui.DrawLabel(r.image, rect, decoration.ToolRows[row], rgb(0xdcebee))
		return
	}
	// Paint the entire row's backgrounds before glyphs. A continuation cell
	// must never erase a glyph that spans two columns.
	for col := 0; col < snapshot.Cols; col++ {
		cell := snapshot.Cells[row*snapshot.Cols+col]
		selected := decoration.Selection.contains(row*snapshot.Cols + col)
		if cell.Width == 0 && col > 0 {
			selected = selected || decoration.Selection.contains(row*snapshot.Cols+col-1)
		}
		_, background := cellColors(cell, selected)
		fill(r.image, image.Rect(contentLeft+col*cellWidth, contentTop+row*cellHeight, contentLeft+(col+1)*cellWidth, contentTop+(row+1)*cellHeight), background)
	}
	for col := 0; col < snapshot.Cols; col++ {
		cell := snapshot.Cells[row*snapshot.Cols+col]
		foreground, _ := cellColors(cell, decoration.Selection.contains(row*snapshot.Cols+col))
		r.drawCell(cell, col, row, foreground)
	}
	cursor := snapshot.Cursor
	if cursor.Row != row || !cursor.Visible || snapshot.ScrollOffset != 0 || cursor.Col < 0 || cursor.Col >= snapshot.Cols || snapshot.Exited || decoration.Focused && !decoration.CursorOn {
		return
	}
	x, y := contentLeft+cursor.Col*cellWidth, contentTop+row*cellHeight
	cursorColor := rgb(0x83e6f1)
	if !decoration.Focused {
		fill(r.image, image.Rect(x, y, x+cellWidth, y+1), cursorColor)
		fill(r.image, image.Rect(x, y+cellHeight-1, x+cellWidth, y+cellHeight), cursorColor)
		fill(r.image, image.Rect(x, y, x+1, y+cellHeight), cursorColor)
		fill(r.image, image.Rect(x+cellWidth-1, y, x+cellWidth, y+cellHeight), cursorColor)
		return
	}
	switch cursor.Shape {
	case 2:
		fill(r.image, image.Rect(x, y+cellHeight-2, x+cellWidth, y+cellHeight), cursorColor)
	case 3:
		fill(r.image, image.Rect(x, y, x+2, y+cellHeight), cursorColor)
	default:
		fill(r.image, image.Rect(x, y, x+cellWidth, y+cellHeight), cursorColor)
		r.drawCell(snapshot.Cells[row*snapshot.Cols+cursor.Col], cursor.Col, row, rgb(0x071b23))
	}
}

func (r *terminalRenderer) drawCell(cell terminal.Cell, col, row int, foreground color.RGBA) {
	if cell.Width <= 0 {
		return
	}
	width := cell.Width
	if width > 2 {
		width = 2
	}
	x, y := contentLeft+col*cellWidth, contentTop+row*cellHeight
	rect := image.Rect(x, y, x+width*cellWidth, y+cellHeight).Intersect(r.image.Bounds())
	if rect.Empty() {
		return
	}
	face := 0
	if cell.Bold {
		face |= 1
	}
	if cell.Italic {
		face |= 2
	}
	dst := r.image.SubImage(rect).(*image.RGBA)
	dot := fixed.P(x, y+17)
	drawer := font.Drawer{Dst: dst, Src: image.NewUniform(foreground), Face: r.faces[face], Dot: dot}
	for _, ch := range cell.Chars {
		if ch == 0 {
			break
		}
		// libvterm stores a base character followed by combining marks. Draw
		// marks at the same cell origin instead of allocating extra columns.
		if unicode.Is(unicode.Mn, ch) || unicode.Is(unicode.Mc, ch) {
			drawer.Dot = dot
		}
		drawer.Face = r.fallback.face(ch, face)
		drawer.DrawString(string(ch))
	}
	if cell.Underline {
		fill(dst, image.Rect(x, y+cellHeight-3, x+width*cellWidth, y+cellHeight-2), foreground)
	}
	if cell.Strike {
		fill(dst, image.Rect(x, y+cellHeight/2, x+width*cellWidth, y+cellHeight/2+1), foreground)
	}
}

func clippedText(value string, columns int) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	text := []rune(value)
	if columns < 1 {
		return ""
	}
	if len(text) > columns {
		if columns > 1 {
			text = append(text[:columns-1], '…')
		} else {
			text = text[:1]
		}
	}
	return string(text)
}

func (r *terminalRenderer) labelText(x, y int, value string, c color.RGBA, maxWidth int) {
	_ = r.ui.DrawLabel(r.image, image.Rect(x, y-13, x+max(0, maxWidth), y+3), value, c)
}

func (r *terminalRenderer) drawChrome(snapshot terminal.Snapshot, decoration terminalDecoration) {
	width, height := r.image.Rect.Dx(), r.image.Rect.Dy()
	fill(r.image, image.Rect(0, 0, width, contentTop), rgb(0x091823))
	accent, rail := rgb(0x5894a3), rgb(0x244d5d)
	if decoration.Focused {
		accent, rail = rgb(0x8bebf3), rgb(0x34788b)
	}
	fill(r.image, image.Rect(18, 1, width-18, 2), rail)
	plate := image.Rect(10, 5, min(width-10, 220), 27)
	terminalChromePlate(r.image, plate, 5, rgb(0x102e3d))
	fill(r.image, image.Rect(plate.Min.X+5, plate.Min.Y, plate.Max.X-5, plate.Min.Y+1), accent)
	terminalChromeLine(r.image, 17, 11, 21, 15, accent)
	terminalChromeLine(r.image, 21, 15, 17, 19, accent)
	fill(r.image, image.Rect(24, 19, 29, 20), accent)
	r.labelText(38, 21, "NATIVE TERMINAL", accent, plate.Max.X-48)
	if width >= 420 {
		// Session identity belongs in the header; live process status stays
		// in the footer. The paired rails are decorative.
		fill(r.image, image.Rect(plate.Max.X+12, 13, width-133, 14), rail)
		fill(r.image, image.Rect(plate.Max.X+12, 17, width-149, 18), rgb(0x183846))
		r.labelText(width-116, 21, "SHELL SESSION", rgb(0x698c9b), 104)
	}
	title := snapshot.Title
	if title == "" {
		title = "Interactive shell"
	}
	if decoration.Find != nil {
		r.labelText(18, 43, "Find", rgb(0xd7e9ef), 40)
		_ = r.ui.DrawField(r.image, findFieldRect(width), decoration.Find, "Search scrollback", decoration.Focused)
	} else if decoration.Header != "" {
		r.labelText(18, 43, decoration.Header, rgb(0xd7e9ef), width-36)
	} else {
		toolbar := terminalToolbar(width)
		titleWidth := width - 36
		if len(toolbar) != 0 {
			titleWidth = toolbar[0].Bounds.Min.X - 26
		}
		r.labelText(18, 43, title, rgb(0xd7e9ef), titleWidth)
		for _, node := range toolbar {
			node.Disabled = snapshot.AlternateScreen
			_ = r.ui.DrawButton(r.image, node, decoration.ToolFocus == node.ID, false)
		}
	}
	fill(r.image, image.Rect(18, contentTop-3, width-18, contentTop-2), rgb(0x183c4a))
	fill(r.image, image.Rect(18, contentTop-2, 74, contentTop-1), rail)

	footer := height - footerHeight
	fill(r.image, image.Rect(0, footer, width, height), rgb(0x091823))
	fill(r.image, image.Rect(15, footer, width-15, footer+1), rgb(0x2a596b))
	terminalChromePlate(r.image, image.Rect(10, footer+4, width-10, height-3), 4, rgb(0x0d2430))
	statusColor, indicator := rgb(0x9cbfcb), rgb(0x62becb)
	status := fmt.Sprintf("%d × %d  /  LIVE", snapshot.Cols, snapshot.Rows)
	if snapshot.MouseTracking {
		status += "  /  APP MOUSE"
	}
	if snapshot.ScrollOffset > 0 {
		status = fmt.Sprintf("HISTORY %d / %d  /  scroll to return", snapshot.ScrollOffset, snapshot.ScrollbackLen)
	}
	if snapshot.Exited {
		statusColor, indicator = rgb(0xc4b5a2), rgb(0xb59770)
		status = "PROCESS EXITED"
		if snapshot.ExitError != "" {
			status += " / " + snapshot.ExitError
		}
	}
	if decoration.Footer != "" {
		status = decoration.Footer
	}
	fill(r.image, image.Rect(16, footer+11, 19, footer+14), indicator)
	r.labelText(27, height-8, status, statusColor, width-57)
	terminalChromeLine(r.image, width-25, height-9, width-20, height-14, rgb(0x3b697a))
	terminalChromeLine(r.image, width-20, height-9, width-15, height-14, rgb(0x3b697a))
}

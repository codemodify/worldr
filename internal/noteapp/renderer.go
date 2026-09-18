package noteapp

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
)

const noteBaseWidth, noteBaseHeight = 980, 640

type renderer struct {
	image          *image.RGBA
	texture        *render.Texture
	painter        *nativeui.Painter
	scaleX, scaleY float32
}

func newRenderer(width, height int) (*renderer, error) {
	theme := nativeui.Cinematic()
	theme.Font = "Monospace"
	theme.FontSize = 15
	painter, err := nativeui.NewPainter(theme)
	if err != nil {
		return nil, err
	}
	r := &renderer{painter: painter}
	if err := r.resize(width, height); err != nil {
		painter.Close()
		return nil, err
	}
	return r, nil
}

func (r *renderer) close() { r.painter.Close() }

func (r *renderer) resize(width, height int) error {
	if width < 640 || height < 420 || width > 4096 || height > 4096 {
		return fmt.Errorf("native note must be between 640x420 and 4096x4096")
	}
	if r.image != nil && r.image.Rect.Dx() == width && r.image.Rect.Dy() == height {
		return nil
	}
	r.image = image.NewRGBA(image.Rect(0, 0, width, height))
	r.scaleX, r.scaleY = float32(width)/noteBaseWidth, float32(height)/noteBaseHeight
	if r.texture == nil {
		texture, err := render.NewTexture(width, height, r.image.Pix)
		r.texture = texture
		return err
	}
	return r.texture.Replace(width, height, r.image.Pix)
}

func noteColor(value uint32) color.RGBA {
	return color.RGBA{R: byte(value >> 16), G: byte(value >> 8), B: byte(value), A: 255}
}

func (r *renderer) bounds(x, y, width, height int) image.Rectangle {
	return image.Rect(int(float32(x)*r.scaleX), int(float32(y)*r.scaleY), int(float32(x+width)*r.scaleX), int(float32(y+height)*r.scaleY))
}

func (r *renderer) fill(rect image.Rectangle, value uint32) {
	draw.Draw(r.image, rect.Intersect(r.image.Bounds()), image.NewUniform(noteColor(value)), image.Point{}, draw.Src)
}

func (r *renderer) text(rect image.Rectangle, value uint32, text string) error {
	return r.painter.DrawLabel(r.image, rect.Intersect(r.image.Bounds()), text, noteColor(value))
}

func (r *renderer) editorRect() image.Rectangle { return r.bounds(26, 94, 928, 474) }

func (r *renderer) textRect() image.Rectangle {
	rect := r.editorRect()
	return image.Rect(rect.Min.X+int(62*r.scaleX), rect.Min.Y+int(10*r.scaleY), rect.Max.X-int(15*r.scaleX), rect.Max.Y-int(10*r.scaleY))
}

func (r *renderer) lineHeight() int { return max(16, int(22*r.scaleY)) }
func (r *renderer) cellWidth() int  { return max(7, int(9*r.scaleX)) }

func (r *renderer) visibleLines() int {
	return max(1, r.textRect().Dy()/r.lineHeight())
}

func (r *renderer) visibleColumns() int {
	return max(1, r.textRect().Dx()/r.cellWidth())
}

func safeLine(text string) string {
	return strings.Map(func(char rune) rune {
		switch char {
		case '\t':
			return '⇥'
		case '\r':
			return '↵'
		}
		if unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			return '·'
		}
		return char
	}, text)
}

func runeSlice(text string, first, count int) string {
	if count <= 0 {
		return ""
	}
	stops := runeStops(text)
	if first < 0 || first >= len(stops)-1 {
		return ""
	}
	start := stops[first]
	stop := stops[min(len(stops)-1, first+count)]
	return text[start:stop]
}

func (r *renderer) pointPosition(view *viewer, x, y float32) (int, int) {
	rect := r.textRect()
	line := view.state.TopLine + max(0, min(r.visibleLines()-1, (int(y)-rect.Min.Y)/r.lineHeight()))
	column := view.state.Left + max(0, (int(x)-rect.Min.X+r.cellWidth()/2)/r.cellWidth())
	return line, column
}

func (r *renderer) caretRect(view *viewer) image.Rectangle {
	line, column := view.editor.lineColumn(view.editor.caret)
	text := r.textRect()
	x := text.Min.X + (column-view.state.Left)*r.cellWidth()
	y := text.Min.Y + (line-view.state.TopLine)*r.lineHeight()
	return image.Rect(x, y, x+max(1, int(r.scaleX)), y+r.lineHeight()).Intersect(text)
}

func noteTitle(view *viewer) string {
	if view.state.Source == "" {
		return "UNTITLED NOTE"
	}
	return strings.ToUpper(filepath.Base(view.state.Source))
}

func (r *renderer) drawFrame(focused bool) {
	r.fill(r.image.Bounds(), 0x06131e)
	r.fill(r.bounds(0, 0, noteBaseWidth, 58), 0x102d3a)
	accent := uint32(0x3d8798)
	if focused {
		accent = 0x76e4ef
	}
	r.fill(r.bounds(20, 16, 6, 24), accent)
	r.fill(r.bounds(26, 16, 154, 2), accent)
	r.fill(r.bounds(26, 38, 92, 2), 0x2a6373)
	rect := r.editorRect()
	r.fill(rect, 0x091b26)
	// An open right edge keeps the document in Worldr's spatial language while
	// retaining unmistakable top/bottom and left-side framing.
	r.fill(image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+2, rect.Min.Y+rect.Dy()/2-20), accent)
	r.fill(image.Rect(rect.Min.X, rect.Min.Y+rect.Dy()/2+20, rect.Min.X+2, rect.Max.Y), accent)
	r.fill(image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+rect.Dx()*3/5, rect.Min.Y+2), accent)
	r.fill(image.Rect(rect.Min.X, rect.Max.Y-2, rect.Min.X+rect.Dx()*3/5, rect.Max.Y), accent)
	gutterX := r.textRect().Min.X - int(12*r.scaleX)
	r.fill(image.Rect(gutterX, rect.Min.Y+int(8*r.scaleY), gutterX+1, rect.Max.Y-int(8*r.scaleY)), 0x245263)
}

func (r *renderer) drawEditor(view *viewer) error {
	textRect := r.textRect()
	lineHeight, cellWidth := r.lineHeight(), r.cellWidth()
	lo, hi := view.editor.Range()
	preedit, preeditBegin, _ := view.editor.Preedit()
	caretLine, caretColumn := view.editor.lineColumn(view.editor.caret)
	for row := 0; row < r.visibleLines(); row++ {
		lineIndex := view.state.TopLine + row
		line, start, end := view.editor.Line(lineIndex)
		if lineIndex >= view.editor.LineCount() {
			break
		}
		y := textRect.Min.Y + row*lineHeight
		lineRect := image.Rect(textRect.Min.X, y, textRect.Max.X, y+lineHeight).Intersect(textRect)
		if hi > start && lo <= end {
			selectionStart := max(start, min(end, lo))
			selectionEnd := max(start, min(end, hi))
			first := graphemeCount(view.editor.text[start:selectionStart]) - view.state.Left
			last := graphemeCount(view.editor.text[start:selectionEnd]) - view.state.Left
			if hi > end && selectionEnd == end {
				last++
			}
			selection := image.Rect(textRect.Min.X+first*cellWidth, y+1, textRect.Min.X+last*cellWidth, y+lineHeight-1).Intersect(lineRect)
			if !selection.Empty() {
				r.fill(selection, 0x285d71)
			}
		}
		display := line
		if preedit != "" && lineIndex == caretLine {
			caretByte := view.editor.caret - start
			if caretByte >= 0 && caretByte <= len(line) {
				display = line[:caretByte] + preedit + line[caretByte:]
				preeditColumn := caretColumn - view.state.Left
				width := max(1, graphemeCount(preedit)) * cellWidth
				underline := image.Rect(textRect.Min.X+preeditColumn*cellWidth, y+lineHeight-2, textRect.Min.X+preeditColumn*cellWidth+width, y+lineHeight-1).Intersect(lineRect)
				r.fill(underline, 0x8bebf3)
			}
		}
		display = runeSlice(safeLine(display), view.state.Left, r.visibleColumns()+2)
		if err := r.text(lineRect, 0xcce5eb, display); err != nil {
			return err
		}
		numberRect := image.Rect(r.editorRect().Min.X+int(8*r.scaleX), y, textRect.Min.X-int(18*r.scaleX), y+lineHeight)
		if err := r.text(numberRect, 0x477482, fmt.Sprintf("%04d", lineIndex+1)); err != nil {
			return err
		}
	}
	if view.focused {
		caret := r.caretRect(view)
		if preedit != "" && caretLine >= view.state.TopLine && caretLine < view.state.TopLine+r.visibleLines() {
			advance := graphemeCount(preedit)
			if preeditBegin >= 0 && int(preeditBegin) <= len(preedit) {
				advance = graphemeCount(preedit[:preeditBegin])
			}
			caret = caret.Add(image.Pt(advance*cellWidth, 0)).Intersect(textRect)
		}
		if !caret.Empty() {
			r.fill(caret, 0x8bebf3)
		}
	}
	return nil
}

func (r *renderer) draw(view *viewer) error {
	r.drawFrame(view.focused)
	if err := r.text(r.bounds(40, 14, 560, 30), 0xc8edf2, "DOCUMENT / "+noteTitle(view)); err != nil {
		return err
	}
	for _, node := range view.controls.Semantics().Nodes {
		if node.Role == nativeui.RoleButton {
			if err := r.painter.DrawButton(r.image, node, view.controls.FocusedID() == node.ID, node.Selected); err != nil {
				return err
			}
		}
	}
	if err := r.drawEditor(view); err != nil {
		return err
	}
	line, column := view.editor.lineColumn(view.editor.caret)
	state := "SAVED"
	if view.state.Dirty {
		state = "MODIFIED"
	}
	if view.saving {
		state = "SAVING"
	}
	status := fmt.Sprintf("%s   LN %d  COL %d   %d / %d BYTES", state, line+1, column+1, len(view.editor.text), MaxDocumentBytes)
	if err := r.text(r.bounds(28, 577, 430, 24), 0x75b9c5, status); err != nil {
		return err
	}
	message := view.message
	if message == "" {
		message = "Ctrl+S save · Ctrl+Z/Y undo/redo · Ctrl+C/X/V clipboard · drag to select"
	}
	if err := r.text(r.bounds(28, 607, 924, 23), 0x7f9fab, message); err != nil {
		return err
	}
	return r.texture.Replace(r.image.Rect.Dx(), r.image.Rect.Dy(), r.image.Pix)
}

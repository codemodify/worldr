package glass

import (
	"strings"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/terminal"
)

type terminalPane struct {
	term                *terminal.Terminal
	snapshot            terminal.Snapshot
	anchor, end         int
	selecting, selected bool
	id                  int
}

func newTerminalPane(options terminal.Options) (*terminalPane, error) {
	t, err := terminal.Open(options)
	if err != nil {
		return nil, err
	}
	p := &terminalPane{term: t}
	t.Focus(true)
	if err := p.Poll(); err != nil {
		t.Close()
		return nil, err
	}
	return p, nil
}

func (p *terminalPane) Poll() error {
	s, err := p.term.Poll()
	if err != nil {
		return err
	}
	if s.Cols != p.snapshot.Cols || s.Rows != p.snapshot.Rows || s.ScrollOffset != p.snapshot.ScrollOffset {
		p.ClearSelection()
	} else if p.selected && s.Revision != p.snapshot.Revision {
		for i, cell := range s.Cells {
			if p.Selected(i) && (i >= len(p.snapshot.Cells) || cell != p.snapshot.Cells[i]) {
				p.ClearSelection()
				break
			}
		}
	}
	p.snapshot = s
	return nil
}
func (p *terminalPane) Close() error { return p.term.Close() }
func (p *terminalPane) Resize(cols, rows int) error {
	cols, rows = min(512, max(2, cols)), min(256, max(2, rows))
	if cols == p.snapshot.Cols && rows == p.snapshot.Rows {
		return nil
	}
	p.ClearSelection()
	if err := p.term.Resize(cols, rows); err != nil {
		return err
	}
	return p.Poll()
}
func (p *terminalPane) ClearSelection() { p.selected, p.selecting = false, false }
func (p *terminalPane) Selected(i int) bool {
	return p.selected && i >= min(p.anchor, p.end) && i <= max(p.anchor, p.end)
}
func (p *terminalPane) Paste(text string) error { p.ClearSelection(); return p.term.Paste(text) }
func (p *terminalPane) index(e experience.Event) int {
	c, r := min(p.snapshot.Cols-1, max(0, int(e.X))), min(p.snapshot.Rows-1, max(0, int(e.Y)))
	i := r*p.snapshot.Cols + c
	if i >= 0 && i < len(p.snapshot.Cells) && c > 0 && p.snapshot.Cells[i].Width == 0 {
		i--
	}
	return i
}
func (p *terminalPane) Handle(e experience.Event) error {
	switch e.Kind {
	case experience.PointerCancel:
		p.selecting = false
	case experience.KeyboardCancel:
		p.selecting = false
	case experience.KeyInput:
		if e.Pressed {
			p.ClearSelection()
			p.term.Focus(true)
		}
	case experience.PointerScroll:
		if !p.snapshot.MouseTracking || e.Modifiers.Has(experience.ModShift) {
			lines := int(-e.ScrollY / 4)
			if lines == 0 && e.ScrollY != 0 {
				if e.ScrollY < 0 {
					lines = 1
				} else {
					lines = -1
				}
			}
			p.term.Scroll(lines)
			p.ClearSelection()
			return nil
		}
	case experience.PointerDown:
		p.term.Focus(true)
		if e.Button == experience.ButtonPrimary && (!p.snapshot.MouseTracking || e.Modifiers.Has(experience.ModShift)) {
			p.anchor = p.index(e)
			p.end = p.anchor
			p.selected = true
			p.selecting = true
			return nil
		}
	case experience.PointerMove:
		if p.selecting {
			p.end = p.index(e)
			return nil
		}
	case experience.PointerUp:
		if p.selecting && e.Button == experience.ButtonPrimary {
			p.end = p.index(e)
			p.selecting = false
			return nil
		}
	}
	return p.term.Input(e)
}
func (p *terminalPane) Copy() (string, bool) {
	if !p.selected || p.snapshot.Cols < 1 {
		return "", false
	}
	lo, hi := max(0, min(p.anchor, p.end)), min(len(p.snapshot.Cells)-1, max(p.anchor, p.end))
	var result strings.Builder
	for row := lo / p.snapshot.Cols; row <= hi/p.snapshot.Cols; row++ {
		var line strings.Builder
		for i := max(lo, row*p.snapshot.Cols); i <= min(hi, (row+1)*p.snapshot.Cols-1); i++ {
			cell := p.snapshot.Cells[i]
			if cell.Width == 0 {
				continue
			}
			if cell.Chars[0] == 0 {
				line.WriteByte(' ')
				continue
			}
			for _, r := range cell.Chars {
				if r == 0 {
					break
				}
				line.WriteRune(r)
			}
		}
		if row > lo/p.snapshot.Cols {
			result.WriteByte('\n')
		}
		result.WriteString(strings.TrimRight(line.String(), " "))
	}
	return result.String(), true
}

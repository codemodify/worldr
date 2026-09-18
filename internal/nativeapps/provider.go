package nativeapps

import (
	"fmt"
	"strings"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/terminal"
	"github.com/codemodify/worldr/internal/textinput"
)

type Options = terminal.Options

type terminalSession interface {
	Poll() (terminal.Snapshot, error)
	Resize(int, int) error
	Input(experience.Event) error
	Focus(bool)
	Paste(string) error
	Scroll(int)
	Close() error
}

// Provider owns one engine-native terminal. It presents actual terminal cells
// as a retained image and forwards keyboard input directly to its PTY backend;
// no Wayland or X11 client exists on this path. All calls use the host goroutine.
type Provider struct {
	terminal                   terminalSession
	renderer                   *terminalRenderer
	surfaces                   []experience.ApplicationSurface
	snapshot                   terminal.Snapshot
	selection                  selection
	selecting, focused, closed bool
	dismissed                  bool
	retired                    []uint64
	err                        error
	copyText                   string
	copyReady, pasteRequested  bool
	pasteTool                  string
	pasteEpoch                 uint64
	pastePending               bool
	now                        func() time.Time
	tools                      terminalTools
}

var _ experience.Applications = (*Provider)(nil)

func New(options Options) (*Provider, error) {
	if options.Cols == 0 {
		options.Cols = 96
	}
	if options.Rows == 0 {
		options.Rows = 24
	}
	maxCols, maxRows := terminalGrid(maxExtent, maxExtent)
	if options.Cols < 2 || options.Rows < 2 || options.Cols > maxCols || options.Rows > maxRows {
		return nil, fmt.Errorf("native terminal grid must be between 2x2 and %dx%d", maxCols, maxRows)
	}
	session, err := terminal.Open(options)
	if err != nil {
		return nil, err
	}
	p, err := newProvider(session, options.Cols, options.Rows)
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	return p, nil
}

func newProvider(session terminalSession, cols, rows int) (*Provider, error) {
	renderer, err := newTerminalRenderer(max(minWidth, cols*cellWidth+2*contentLeft), max(minHeight, rows*cellHeight+contentTop+footerHeight))
	if err != nil {
		return nil, err
	}
	p := &Provider{terminal: session, renderer: renderer, now: time.Now}
	p.tools.text, err = textinput.New()
	if err != nil {
		renderer.close()
		return nil, err
	}
	p.tools.expanded = make(map[uint64]bool)
	p.tools.field = nativeui.NewField(1024)
	p.surfaces = []experience.ApplicationSurface{{ID: 1, Key: "native:terminal", AppID: "worldr.native-terminal", FrameStyle: experience.FrameCinematic, Title: "Native terminal", Texture: renderer.texture}}
	if err := p.Poll(); err != nil {
		p.tools.text.Close()
		renderer.close()
		return nil, err
	}
	return p, nil
}

func (p *Provider) remember(err error) {
	if p.err == nil && err != nil {
		p.err = err
	}
}

func (p *Provider) Poll() error {
	if p.dismissed {
		return p.err
	}
	if p.closed {
		return terminal.ErrClosed
	}
	if p.err != nil {
		return p.err
	}
	snapshot, err := p.terminal.Poll()
	if err != nil {
		return err
	}
	if p.selection.Active {
		if snapshot.Cols != p.snapshot.Cols || snapshot.Rows != p.snapshot.Rows || snapshot.ScrollOffset != p.snapshot.ScrollOffset {
			p.selection = selection{}
			p.selecting = false
		} else if snapshot.Revision != p.snapshot.Revision {
			for i, cell := range snapshot.Cells {
				if p.selection.contains(i) && (i >= len(p.snapshot.Cells) || cell != p.snapshot.Cells[i]) {
					p.selection = selection{}
					p.selecting = false
					break
				}
			}
		}
	}
	p.snapshot = snapshot
	cursorOn := !snapshot.Cursor.Blink || !p.focused || snapshot.Exited || snapshot.ScrollOffset != 0 || !snapshot.Cursor.Visible || p.now().UnixMilli()/550%2 == 0
	display, decoration := p.toolDisplay(snapshot, terminalDecoration{Focused: p.focused, CursorOn: cursorOn, Selection: p.selection})
	if err := p.renderer.paint(display, decoration); err != nil {
		return err
	}
	title := "Native terminal"
	if snapshot.Title != "" {
		title += " / " + snapshot.Title
	}
	if snapshot.Exited {
		title += " (exited)"
	}
	p.surfaces[0].Title = title
	return nil
}

// CloseApplication is an explicit window close; ordinary shell exit keeps its
// final output visible until this request. The host releases the retired image
// after the workspace has removed its scene node.
func (p *Provider) CloseApplication(id uint64) {
	if id != 1 || p.closed {
		return
	}
	p.dismissed = true
	p.retired = append(p.retired, p.renderer.texture.ID())
	p.remember(p.Close())
}

func (p *Provider) Retired() []uint64 {
	retired := p.retired
	p.retired = nil
	return retired
}

func (p *Provider) Surfaces() []experience.ApplicationSurface { return p.surfaces }

// WorkingDirectory is optional for terminal backends. The native PTY backend
// reports its live shell cwd and retains a fallback after the shell exits.
func (p *Provider) WorkingDirectory() (string, error) {
	if p.closed {
		return "", terminal.ErrClosed
	}
	if source, ok := p.terminal.(interface{ WorkingDirectory() (string, error) }); ok {
		return source.WorkingDirectory()
	}
	return "", fmt.Errorf("terminal backend does not expose its working directory")
}

func (p *Provider) Focus(id uint64) {
	if p.closed {
		return
	}
	if p.focused != (id == 1) {
		p.tools.epoch++
		p.tools.field.CancelComposition()
	}
	p.focused = id == 1
	p.terminal.Focus(p.focused && p.tools.mode == "" && !p.tools.toolbarActive)
	if !p.focused {
		p.selecting = false
		p.tools.text.Handle(experience.Event{Kind: experience.KeyboardCancel})
		p.tools.owned = [768]bool{}
		p.tools.controls.Blur()
		p.tools.toolbarActive = false
	}
}

// Seat receives global XKB state while this app is unfocused. Physical key
// presses are sent only through Send, after workspace keyboard ownership.
func (p *Provider) Seat(event experience.Event) {
	if p.closed {
		return
	}
	switch event.Kind {
	case experience.KeyInput:
		event.Kind = experience.KeyboardModifiers
	case experience.KeymapChanged, experience.KeyboardModifiers, experience.KeyboardRepeatInfo:
	case experience.KeyboardCancel:
		p.Focus(0)
	default:
		return
	}
	p.tools.text.Handle(event)
	p.remember(p.terminal.Input(event))
}

func (p *Provider) Resize(id uint64, width, height int) {
	if p.closed || id != 1 {
		return
	}
	width = max(minWidth, min(maxExtent, width))
	height = max(minHeight, min(maxExtent, height))
	cols, rows := terminalGrid(width, height)
	if err := p.terminal.Resize(cols, rows); err != nil {
		p.remember(err)
		return
	}
	if err := p.renderer.resize(width, height); err != nil {
		p.remember(err)
		return
	}
	p.selection = selection{}
	p.selecting = false
	p.remember(p.Poll())
}

func terminalButton(event experience.Event) uint32 {
	if event.ButtonCode != 0 {
		return event.ButtonCode
	}
	switch event.Button {
	case experience.ButtonPrimary:
		return 272
	case experience.ButtonSecondary:
		return 273
	case experience.ButtonMiddle:
		return 274
	}
	return 0
}

func (p *Provider) contentEvent(event experience.Event) (experience.Event, bool) {
	event.X = (event.X - contentLeft) / cellWidth
	event.Y = (event.Y - contentTop) / cellHeight
	inside := event.X >= 0 && event.Y >= 0 && event.X < float32(p.snapshot.Cols) && event.Y < float32(p.snapshot.Rows)
	return event, inside
}

func (p *Provider) selectionIndex(event experience.Event) int {
	col := max(0, min(p.snapshot.Cols-1, int(event.X)))
	row := max(0, min(p.snapshot.Rows-1, int(event.Y)))
	index := row*p.snapshot.Cols + col
	// A wide character's continuation column belongs to its preceding cell.
	if index < len(p.snapshot.Cells) && col > 0 && p.snapshot.Cells[index].Width == 0 {
		index--
	}
	return index
}

func (p *Provider) Send(id uint64, event experience.Event) {
	if p.closed || id != 1 {
		return
	}
	switch event.Kind {
	case experience.TextCommit, experience.TextPreedit:
		state := p.TextInput(1)
		if !state.Enabled || event.TextContext != state.ContextID {
			return
		}
		p.tools.text.Handle(event)
		if p.tools.field.Handle(event, "") {
			p.tools.query = p.tools.field.Text()
			p.searchHistory(true)
		}
		return
	case experience.KeyboardCancel:
		p.Focus(0)
		p.remember(p.terminal.Input(event))
		return
	case experience.KeymapChanged, experience.KeyboardModifiers, experience.KeyboardRepeatInfo:
		p.Seat(event)
		return
	case experience.PointerCancel:
		p.selecting = false
		p.remember(p.terminal.Input(event))
		return
	case experience.KeyInput:
		if !p.focused {
			return
		}
		if p.toolKey(event) {
			return
		}
		if event.Modifiers == experience.ModControl|experience.ModShift && (event.Keycode == 46 || event.Keycode == 47) {
			if event.Pressed && !event.Repeat {
				if event.Keycode == 46 && p.selection.Active {
					p.copyText, p.copyReady = p.Selection(), true
				}
				if event.Keycode == 47 {
					p.pasteRequested = true
				}
			}
			return
		}
		if p.snapshot.Exited {
			return
		}
		if event.Pressed {
			p.selection = selection{}
			p.selecting = false
		}
		p.remember(p.terminal.Input(event))
		return
	case experience.PointerDown, experience.PointerUp, experience.PointerMove, experience.PointerScroll:
		if p.toolPointer(event) {
			return
		}
		mapped, inside := p.contentEvent(event)
		if p.selecting {
			if event.Kind == experience.PointerMove {
				p.selection.End = p.selectionIndex(mapped)
				return
			}
			if event.Kind == experience.PointerUp && terminalButton(event) == 272 {
				p.selection.End = p.selectionIndex(mapped)
				p.selecting = false
				return
			}
		}
		if !inside && event.Kind != experience.PointerUp {
			return
		}
		if event.Kind == experience.PointerScroll && (!p.snapshot.MouseTracking || event.Modifiers.Has(experience.ModShift)) {
			lines := int(-event.ScrollY / 5)
			if lines == 0 && event.ScrollY != 0 {
				if event.ScrollY < 0 {
					lines = 1
				} else {
					lines = -1
				}
			}
			p.terminal.Scroll(lines)
			p.selection = selection{}
			return
		}
		if event.Kind == experience.PointerDown && terminalButton(event) == 272 && (!p.snapshot.MouseTracking || event.Modifiers.Has(experience.ModShift)) {
			index := p.selectionIndex(mapped)
			p.selection = selection{Anchor: index, End: index, Active: true}
			p.selecting = true
			return
		}
		if p.snapshot.MouseTracking && !p.snapshot.Exited {
			p.remember(p.terminal.Input(mapped))
		}
	}
}

// Selection exports plain text from the selected visible cells, with wide
// continuation cells omitted and combining marks retained. Soft-wrap metadata
// is not yet exposed by the backend, so selected rows are separated by LF.
func (p *Provider) Selection() string {
	if !p.selection.Active || p.snapshot.Cols <= 0 {
		return ""
	}
	lo, hi := p.selection.Anchor, p.selection.End
	if lo > hi {
		lo, hi = hi, lo
	}
	lo = max(0, lo)
	hi = min(len(p.snapshot.Cells)-1, hi)
	if hi < lo {
		return ""
	}
	var text strings.Builder
	for row := lo / p.snapshot.Cols; row <= hi/p.snapshot.Cols; row++ {
		start, end := max(lo, row*p.snapshot.Cols), min(hi, (row+1)*p.snapshot.Cols-1)
		var line strings.Builder
		for i := start; i <= end; i++ {
			cell := p.snapshot.Cells[i]
			if cell.Width <= 0 {
				continue
			}
			if cell.Chars[0] == 0 {
				line.WriteByte(' ')
				continue
			}
			for _, ch := range cell.Chars {
				if ch == 0 {
					break
				}
				line.WriteRune(ch)
			}
		}
		if row > lo/p.snapshot.Cols {
			text.WriteByte('\n')
		}
		text.WriteString(strings.TrimRight(line.String(), " "))
	}
	return text.String()
}

func (p *Provider) TakeCopy() (string, bool) {
	text, ok := p.copyText, p.copyReady
	p.copyReady = false
	return text, ok
}
func (p *Provider) TakePasteRequest() bool {
	requested := p.pasteRequested
	p.pasteRequested = false
	if requested {
		p.pasteTool, p.pasteEpoch, p.pastePending = p.tools.mode, p.tools.epoch, true
	}
	return requested
}
func (p *Provider) Paste(text string) error {
	if p.closed {
		return terminal.ErrClosed
	}
	if p.pastePending {
		p.pastePending = false
		if !p.focused || p.pasteTool != p.tools.mode || p.pasteEpoch != p.tools.epoch {
			return nil
		}
	}
	if len(text) > 1024*1024 {
		return fmt.Errorf("native paste exceeds 1 MiB")
	}
	if p.tools.mode == "find" && p.focused {
		p.findText(text)
		return nil
	}
	if p.tools.mode != "" {
		return nil
	}
	if p.snapshot.Exited {
		return nil
	}
	p.selection = selection{}
	p.selecting = false
	return p.terminal.Paste(text)
}

func (p *Provider) Close() error {
	if p.closed {
		return nil
	}
	p.closed = true
	err := p.terminal.Close()
	p.tools.text.Close()
	p.renderer.close()
	p.surfaces = nil
	return err
}

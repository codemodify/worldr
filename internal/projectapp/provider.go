// Package projectapp presents a bounded native project browser.
// Directory and text I/O runs on one worker; the host owns its retained surface,
// input, polling, and shutdown without a Wayland or X11 client.
package projectapp

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/textinput"
)

// Provider belongs to one host goroutine. Poll consumes immutable worker
// results and repaints only after changes. Filesystem errors are visible in the
// browser; only rendering failures are returned to the host.
type Provider struct {
	fieldContext                             uint64
	contextField                             *nativeui.Field
	pasteField                               *nativeui.Field
	pasteContext                             uint64
	pasteRequested, pastePending, pasteValid bool
	searchField                              *nativeui.Field
	root, directory                          string
	pendingSelected                          string
	sessionRoot                              func() string
	entries                                  []entry
	allEntries                               []entry
	query                                    string
	openIntent                               OpenIntent
	searchActive                             bool
	text                                     *textinput.Translator
	now                                      func() time.Time
	nextRefresh                              time.Time
	selected, listTop                        int
	preview                                  textPreview
	previewPath, message, notice             string
	previewTop, previewLeft                  int
	listTruncated, previewTruncated          bool
	loadingDirectory, loadingFile            bool
	focused, previewActive, closed           bool
	dirty                                    bool
	err                                      error
	renderer                                 *browserRenderer
	surfaces                                 []experience.ApplicationSurface
	retired                                  []uint64
	copyText                                 string
	copyReady                                bool
	generation                               uint64
	requests                                 chan request
	results                                  chan result
	done                                     chan struct{}
	context                                  context.Context
	cancel, cancelRequest                    context.CancelFunc
	lastClick                                int
	lastClickTime                            uint32
	lastClickX, lastClickY                   float32
	openHandler                              func(*os.File, string) error
	terminalHandler                          func(*os.File, string) error
	openingTerminal                          bool
	operationPending                         bool
	dialog                                   *fileDialog
	fileHistory                              []fileUndo
	thumbnailsEnabled, thumbnailPending      bool
	thumbnails                               map[string]thumbnailRecord
	thumbnailOrder                           []string
}

var _ experience.Applications = (*Provider)(nil)
var _ experience.ApplicationCloser = (*Provider)(nil)

const (
	terminalListingWait = "Wait for the folder listing before opening a terminal."
	terminalMediaWait   = "Wait for this file to open before opening a terminal."
)

// New validates and anchors the startup root, then returns a loading surface.
// Listings and previews are asynchronous. Paths below the startup root never
// follow symlinks; the root itself may be an explicitly chosen symlink.
func New(root string) (*Provider, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("a project directory is required")
	}
	reader, absolute, err := openReader(root)
	if err != nil {
		return nil, err
	}
	return newProvider(reader, absolute)
}

func newProvider(reader projectReader, root string) (*Provider, error) {
	return newProviderAt(reader, root, "", "", false)
}

func newProviderAt(reader projectReader, root, directory, selected string, restoring bool) (*Provider, error) {
	renderer, err := newBrowserRenderer(1040, 660)
	if err != nil {
		_ = reader.close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	translator, err := textinput.New()
	if err != nil {
		cancel()
		renderer.close()
		_ = reader.close()
		return nil, err
	}
	p := &Provider{root: root, selected: -1, lastClick: -1, renderer: renderer, context: ctx, cancel: cancel,
		requests: make(chan request, 1), results: make(chan result, 1), done: make(chan struct{}), text: translator, now: time.Now, searchField: nativeui.NewField(maxSearchBytes)}
	if rooted, ok := reader.(interface{ sessionRoot() string }); ok {
		p.sessionRoot = rooted.sessionRoot
	}
	if capable, ok := reader.(interface{ supportsThumbnails() bool }); ok {
		p.thumbnailsEnabled = capable.supportsThumbnails()
	}
	p.surfaces = []experience.ApplicationSurface{{ID: 1, Key: "native:project-browser", AppID: "worldr.project-browser",
		Title: "Project / " + safeLabel(filepath.Base(root)), Texture: renderer.texture}}
	go runReader(ctx, reader, p.requests, p.results, p.done)
	p.navigateTo(directory, selected, restoring)
	if err := p.Poll(); err != nil {
		_ = p.Close()
		return nil, err
	}
	return p, nil
}

func (p *Provider) queue(path string, directory bool, selected string) {
	p.submit(request{path: path, directory: directory, selected: selected})
}

func (p *Provider) submit(req request) {
	if p.cancelRequest != nil {
		p.cancelRequest()
	}
	p.generation++
	if !req.thumbnail {
		p.nextRefresh = p.now().Add(autoRefreshInterval)
	}
	p.thumbnailPending = req.thumbnail
	p.openingTerminal = req.openTerminal
	p.operationPending = req.operation != nil
	ctx, cancel := context.WithCancel(p.context)
	p.cancelRequest = cancel
	req.ctx, req.generation = ctx, p.generation
	// Keep only the newest pending request while a canceled read unwinds.
	select {
	case <-p.requests:
	default:
	}
	p.requests <- req
	p.dirty = true
}

func (p *Provider) navigate(directory, selected string) {
	p.navigateTo(directory, selected, false)
}

func (p *Provider) navigateTo(directory, selected string, restoring bool) {
	p.cancelFieldPaste()
	defer p.syncFieldContext()
	p.directory, p.entries, p.selected, p.listTop = directory, nil, -1, 0
	p.allEntries, p.query, p.searchActive = nil, "", false
	p.searchField.Set("")
	p.thumbnails, p.thumbnailOrder = nil, nil
	p.pendingSelected = selected
	p.preview, p.previewPath, p.message = textPreview{}, "", ""
	p.notice = ""
	p.previewTop, p.previewLeft, p.previewActive = 0, 0, false
	p.listTruncated, p.previewTruncated = false, false
	p.loadingDirectory, p.loadingFile = true, false
	p.lastClick = -1
	p.submit(request{path: directory, directory: true, selected: selected, restoreDirectory: restoring})
}

func (p *Provider) Poll() error {
	if p.closed {
		return nil
	}
	// Draining is bounded: the worker and result channel each hold at most one
	// result, and at most one follow-up preview is queued by this call.
	for i := 0; i < 2; i++ {
		select {
		case res := <-p.results:
			if res.generation != p.generation {
				res.close()
				continue
			}
			p.dirty = true
			if res.thumbnail {
				p.thumbnailResult(&res)
				continue
			}
			if res.operation != nil {
				p.operationResult(&res)
				continue
			}
			if res.openTerminal {
				p.openTerminalResult(&res)
				continue
			}
			if p.notice == terminalListingWait || p.notice == terminalMediaWait {
				p.notice = ""
			}
			p.loadingDirectory, p.loadingFile = false, false
			if res.directory {
				p.pendingSelected = ""
			}
			if res.err != nil {
				res.close()
				if res.restoreDirectory && res.directory && res.path != "" {
					p.navigate("", "")
					p.notice = "Saved folder could not be restored; showing the project root. " + safeLabel(res.err.Error())
					continue
				}
				p.message = safeLabel(res.err.Error())
				continue
			}
			if res.openMedia {
				p.openMediaResult(&res)
			} else if res.directory {
				p.listTruncated = res.truncated
				p.installListing(res.entries, res.selected, res.refresh)
			} else {
				p.preview, p.previewPath, p.previewTruncated = res.preview, res.path, res.truncated
				p.message = ""
				if res.preview.text == "" {
					p.message = "This file is empty."
				}
			}
		default:
			i = 2
		}
	}
	if p.closed {
		return nil // An open callback may have closed this browser.
	}
	p.autoRefresh()
	p.queueThumbnail()
	if p.dirty && p.err == nil {
		p.err = p.renderer.paint(p)
		p.dirty = false
	}
	return p.err
}

func (p *Provider) Surfaces() []experience.ApplicationSurface { return p.surfaces }

// SetOpenHandler installs a host-goroutine callback for explicitly opened native
// documents. Poll passes an anchored regular-file descriptor and a display-safe
// basename. A successful handler owns the file; the provider closes it on error.
// Selection alone never invokes the handler or opens a document descriptor.
func (p *Provider) SetOpenHandler(handler func(file *os.File, name string) error) {
	if !p.closed {
		p.openHandler = handler
	}
}

// SetTerminalHandler installs a host-goroutine callback for Terminal Here.
// Poll lends an anchored, open directory descriptor only for the duration of
// the callback, then always closes it. The handler must start its terminal or
// duplicate the descriptor before returning. displayPath is informational and
// display-safe; launch using the descriptor, never this potentially stale path.
func (p *Provider) SetTerminalHandler(handler func(directory *os.File, displayPath string) error) {
	if !p.closed {
		p.terminalHandler = handler
	}
}

func (p *Provider) terminalHere() {
	if p.closed || p.openingTerminal {
		return
	}
	p.dirty = true
	if p.loadingDirectory {
		p.notice = terminalListingWait
		return
	}
	path, resumePreview := p.directory, false
	if p.selected >= 0 && p.selected < len(p.entries) {
		item := p.entries[p.selected]
		if item.kind == symlinkEntry {
			p.notice = "Terminal Here does not follow symbolic links. Select a folder or regular file."
			return
		}
		if item.kind == directoryEntry {
			path = p.selectedPath()
		} else if item.kind == fileEntry {
			if mediaPath(item.name) && p.loadingFile {
				p.notice = terminalMediaWait
				return
			}
			resumePreview = p.loadingFile
		}
	}
	p.notice = ""
	p.submit(request{path: path, directory: true, openTerminal: true, resumePreview: resumePreview})
}

func (p *Provider) openTerminalResult(res *result) {
	defer res.close() // The callback borrows this descriptor, including on success.
	p.openingTerminal = false
	switch {
	case res.err != nil:
		p.notice = "Unable to open terminal: " + safeLabel(res.err.Error())
	case p.terminalHandler == nil:
		p.notice = "Terminal Here is unavailable in this workspace."
	case res.file == nil:
		p.notice = "The terminal directory could not be opened."
	default:
		if err := p.terminalHandler(res.file, safeLabel(filepath.Join(p.root, res.path))); err != nil {
			p.notice = "Unable to open terminal: " + safeLabel(err.Error())
		} else {
			p.notice = "Opened terminal in " + safeLabel(filepath.Join(p.root, res.path)) + "."
		}
	}
	// Terminal Here uses the same bounded worker as previews. Resume an
	// interrupted text read without changing its selection, scroll, or status.
	if res.resumePreview && !p.closed && res.generation == p.generation {
		p.queue(p.previewPath, false, "")
	}
}

func (p *Provider) openMediaResult(res *result) {
	defer res.close()
	kind, viewer, unavailable := "video", "media player", "Video playback"
	if IsPhotoPath(res.path) {
		kind, viewer, unavailable = "photo", "photo viewer", "Photo viewing"
	} else if IsModelPath(res.path) {
		kind, viewer, unavailable = "model", "model inspector", "Model inspection"
	} else if IsDatasetPath(res.path) {
		kind, viewer, unavailable = "dataset", "research workbench", "Dataset analysis"
	} else if IsNotePath(res.path) {
		kind, viewer, unavailable = "note", "native note editor", "Note editing"
	}
	switch {
	case p.openHandler == nil:
		p.message = unavailable + " is unavailable in this workspace."
	case res.file == nil:
		p.message = "The " + kind + " could not be opened."
	default:
		if err := p.openHandler(res.file, safeLabel(filepath.Base(res.path))); err != nil {
			p.message = "Unable to open " + kind + ": " + safeLabel(err.Error())
		} else {
			res.file = nil // Successful launch transfers descriptor ownership.
			p.message = "Opened " + kind + " in the " + viewer + "."
			if !p.closed && p.openIntent != OpenAny {
				p.SetOpenIntent(OpenAny)
				p.message = "Opened " + kind + " in the " + viewer + "."
			}
		}
	}
}

func (p *Provider) Focus(id uint64) {
	if p.closed || p.focused == (id == 1) {
		return
	}
	p.focused, p.dirty = id == 1, true
	p.cancelFieldPaste()
	defer p.syncFieldContext()
	if !p.focused {
		p.lastClick = -1
		p.searchActive = false
		p.text.Handle(experience.Event{Kind: experience.KeyboardCancel})
	}
}

// Seat preserves keymap/compose metadata for native search and filename fields.
func (p *Provider) Seat(event experience.Event) {
	p.text.Handle(event)
	if event.Kind == experience.KeyboardCancel {
		p.Focus(0)
	}
}

func (p *Provider) Resize(id uint64, width, height int) {
	if p.closed || id != 1 {
		return
	}
	width, height = max(minWidth, min(maxExtent, width)), max(minHeight, min(maxExtent, height))
	if p.renderer.image.Rect.Dx() == width && p.renderer.image.Rect.Dy() == height {
		return
	}
	if err := p.renderer.resize(width, height); err != nil {
		p.err = err
		return
	}
	p.clampScroll()
	p.dirty = true
	// Resize can be delivered after this frame's Poll. Publish painted content
	// before returning so the host never submits the newly allocated blank image.
	if p.err == nil {
		p.err = p.renderer.paint(p)
		p.dirty = false
	}
}

func (p *Provider) selectedPath() string {
	if p.selected >= 0 && p.selected < len(p.entries) {
		return filepath.Join(p.directory, p.entries[p.selected].name)
	}
	return p.directory
}

func (p *Provider) selectEntry(index int) {
	if index < 0 || index >= len(p.entries) {
		return
	}
	if index == p.selected {
		// A repeated arrow at the end of the list or opening the current file
		// must not discard its preview and restart an already pending read.
		p.ensureSelectionVisible()
		p.dirty = true
		return
	}
	p.selected = index
	p.notice = ""
	p.preview, p.previewPath, p.previewTop, p.previewLeft = textPreview{}, p.selectedPath(), 0, 0
	p.previewTruncated, p.loadingFile, p.message = false, false, ""
	p.ensureSelectionVisible()
	item := p.entries[index]
	if item.kind == fileEntry && !mediaPath(item.name) {
		p.loadingFile = true
		p.queue(p.selectedPath(), false, "")
	} else {
		if p.cancelRequest != nil {
			p.cancelRequest()
		}
		p.generation++
		p.openingTerminal, p.thumbnailPending = false, false
		switch item.kind {
		case fileEntry:
			if IsPhotoPath(item.name) {
				p.message = "Photo file. Enter, double-click, or Open to view."
			} else if IsModelPath(item.name) {
				p.message = "3D model. Enter, double-click, or Open to inspect."
			} else if IsDatasetPath(item.name) {
				p.message = "Research dataset. Enter, double-click, or Open to analyze."
			} else if IsNotePath(item.name) {
				p.message = "Worldr note. Enter, double-click, or Open to edit."
			} else {
				p.message = "Video file. Enter, double-click, or Open to play."
			}
		case directoryEntry:
			p.message = "Folder selected. Enter, double-click, or Open to browse."
		case symlinkEntry:
			p.message = "Symbolic link. Links are listed without following them."
		default:
			p.message = "Special file. Only regular files have a text preview."
		}
	}
	p.dirty = true
}

func (p *Provider) openSelected() {
	if p.selected < 0 || p.selected >= len(p.entries) {
		return
	}
	item := p.entries[p.selected]
	if item.kind == directoryEntry {
		p.navigate(p.selectedPath(), "")
	} else if item.kind == fileEntry && mediaPath(item.name) {
		if p.loadingFile {
			return
		}
		p.loadingFile, p.message = true, ""
		p.submit(request{path: p.selectedPath(), openMedia: true})
	} else {
		p.selectEntry(p.selected)
		p.previewActive = true
	}
}

func (p *Provider) parent() {
	if p.directory == "" {
		return
	}
	parent := filepath.Dir(p.directory)
	if parent == "." {
		parent = ""
	}
	p.navigate(parent, filepath.Base(p.directory))
}

func (p *Provider) refresh() {
	// An explicit Refresh retries failed thumbnails even if file metadata did
	// not change (for example, after a transient filesystem read failure).
	p.retryFailedThumbnails()
	selected := ""
	if p.loadingDirectory {
		selected = p.pendingSelected
	}
	if p.selected >= 0 && p.selected < len(p.entries) {
		selected = p.entries[p.selected].name
	}
	p.refreshSelection(selected)
}

func (p *Provider) refreshSelection(selected string) {
	if p.loadingFile {
		p.previewPath = "" // An interrupted preview must be read again.
	}
	p.pendingSelected = selected
	p.loadingDirectory, p.loadingFile = true, false
	p.submit(request{path: p.directory, directory: true, selected: selected, refresh: true})
}

func (p *Provider) copyPath() {
	path := filepath.Join(p.root, p.selectedPath())
	if !utf8.ValidString(path) {
		// Linux filenames can contain arbitrary bytes, but our clipboard endpoint
		// offers text. Substituting those bytes would produce a different path.
		p.copyText, p.copyReady = "", false
		p.notice = "This path contains non-UTF-8 bytes and cannot be copied as text."
		p.dirty = true
		return
	}
	p.copyText, p.copyReady = path, true
	if p.notice != "" {
		p.notice, p.dirty = "", true
	}
}

// TakeCopy returns only an explicitly requested path copy; the host owns the
// clipboard transport and can advertise this text without a paste endpoint.
func (p *Provider) TakeCopy() (string, bool) {
	text, ready := p.copyText, p.copyReady
	p.copyText, p.copyReady = "", false
	return text, ready
}

func (p *Provider) rows() int {
	return max(1, (p.renderer.image.Rect.Dy()-footerHeight-contentTop)/rowHeight)
}

func (p *Provider) clampScroll() {
	p.listTop = max(0, min(max(0, len(p.entries)-p.rows()), p.listTop))
	p.previewTop = max(0, min(max(0, len(p.preview.starts)-p.rows()), p.previewTop))
	columns := max(1, (p.renderer.image.Rect.Dx()-p.renderer.split()-79)/textCell)
	p.previewLeft = max(0, min(max(0, p.preview.maxColumns-columns), p.previewLeft))
}

func (p *Provider) ensureSelectionVisible() {
	if p.selected < p.listTop {
		p.listTop = p.selected
	}
	if p.selected >= p.listTop+p.rows() {
		p.listTop = p.selected - p.rows() + 1
	}
	p.clampScroll()
}

func (p *Provider) scroll(lines int, horizontal bool) {
	if p.previewActive {
		if horizontal {
			p.previewLeft += lines
		} else {
			p.previewTop += lines
		}
	} else if !horizontal {
		p.listTop += lines
	}
	p.clampScroll()
	p.dirty = true
}

func (p *Provider) Send(id uint64, event experience.Event) {
	if p.closed || id != 1 {
		return
	}
	defer p.syncFieldContext()
	switch event.Kind {
	case experience.TextCommit, experience.TextPreedit:
		p.fieldTextEvent(event)
	case experience.KeyboardCancel:
		p.Focus(0)
	case experience.PointerCancel:
		p.lastClick = -1
	case experience.KeyInput:
		if p.focused && event.Pressed {
			if p.fieldClipboardKey(event) {
				return
			}
			p.cancelFieldPaste()
			p.lastClick = -1
			if p.operationPending {
				return
			}
			if p.dialog != nil {
				p.dialogKey(event)
				return
			}
			if p.searchKey(event) {
				return
			}
			if p.operationKey(event) {
				return
			}
			p.key(event)
		}
	case experience.PointerDown:
		p.cancelFieldPaste()
		lastClick, lastTime := p.lastClick, p.lastClickTime
		p.lastClick = -1
		if event.ButtonCode != 0 && event.ButtonCode != 272 || event.ButtonCode == 0 && event.Button != experience.ButtonPrimary {
			return
		}
		if !finite(event.X) || !finite(event.Y) {
			return
		}
		if event.X < 0 || event.Y < 0 || event.X >= float32(p.renderer.image.Rect.Dx()) || event.Y >= float32(p.renderer.image.Rect.Dy()) {
			return
		}
		x, y := int(event.X), int(event.Y)
		if p.operationPending {
			return
		}
		if p.dialog != nil {
			p.dialogClick(x, y)
			return
		}
		for _, button := range operationButtons {
			if image.Pt(x, y).In(button.rect) {
				action, _ := p.operationButton(button.action)
				p.beginOperation(action)
				return
			}
		}
		if (p.searchActive || p.query != "") && image.Pt(x, y).In(searchRect(p.renderer.image.Rect.Dx())) {
			p.searchActive, p.dirty = true, true
			if image.Pt(x, y).In(searchFieldRect(p.renderer.image.Rect.Dx())) {
				if err := p.renderer.uiSmall.PlaceCaret(p.searchField, searchFieldRect(p.renderer.image.Rect.Dx()), image.Pt(x, y), event.Modifiers.Has(experience.ModShift)); err != nil {
					p.err = err
				}
			}
			return
		}
		p.searchActive = false
		for i, button := range toolbarButtons {
			if image.Pt(x, y).In(button.rect) {
				switch i {
				case 0:
					p.parent()
				case 1:
					p.refresh()
				case 2:
					p.openSelected()
				case 3:
					p.copyPath()
				case 4:
					p.terminalHere()
				case 5:
					p.searchActive, p.dirty = true, true
				}
				return
			}
		}
		if y < contentTop || y >= p.renderer.image.Rect.Dy()-footerHeight {
			return
		}
		p.previewActive, p.dirty = x >= p.renderer.split(), true
		if !p.previewActive {
			if y >= contentTop+p.rows()*rowHeight {
				return // The final partial row is not painted or selectable.
			}
			index := p.listTop + (y-contentTop)/rowHeight
			if index >= len(p.entries) {
				return
			}
			double := index == lastClick && lastTime != 0 && event.Time != 0 && event.Time-lastTime <= 400 &&
				math.Abs(float64(event.X-p.lastClickX)) <= 5 && math.Abs(float64(event.Y-p.lastClickY)) <= 5
			p.selectEntry(index)
			p.lastClick, p.lastClickTime = index, event.Time
			p.lastClickX, p.lastClickY = event.X, event.Y
			if double {
				p.openSelected()
			}
		}
	case experience.PointerScroll:
		if p.operationPending || p.dialog != nil {
			return
		}
		if !finite(event.X) || !finite(event.Y) || !finite(event.ScrollX) || !finite(event.ScrollY) {
			return
		}
		if event.Y < contentTop || event.Y >= float32(p.renderer.image.Rect.Dy()-footerHeight) || event.X < 0 || event.X >= float32(p.renderer.image.Rect.Dx()) {
			return
		}
		p.previewActive = event.X >= float32(p.renderer.split())
		for axis, delta := range []float32{event.ScrollY, event.ScrollX} {
			if delta == 0 {
				continue
			}
			lines := int(math.Ceil(math.Min(1000, math.Abs(float64(delta))) / 10 * 3))
			if delta < 0 {
				lines = -lines
			}
			p.scroll(lines, axis == 1)
		}
	}
}

func finite(x float32) bool { return !math.IsNaN(float64(x)) && !math.IsInf(float64(x), 0) }

func (p *Provider) key(event experience.Event) {
	code := event.Keycode
	if event.Modifiers.Has(experience.ModAlt) || event.Modifiers.Has(experience.ModSuper) {
		return
	}
	if (code == 28 || code == 96) && event.Modifiers.Has(experience.ModControl|experience.ModShift) {
		if !event.Repeat {
			p.terminalHere()
		}
		return
	}
	if code == 46 && event.Modifiers.Has(experience.ModControl|experience.ModShift) {
		p.copyPath()
		return
	}
	if code == 63 || (code == 19 || event.Key == experience.KeyR) && event.Modifiers.Has(experience.ModControl) {
		p.refresh()
		return
	}
	if event.Modifiers.Has(experience.ModControl) {
		return
	}
	switch code {
	case 15:
		p.previewActive, p.dirty = !p.previewActive, true
	case 14:
		p.parent()
	case 28, 96:
		if !event.Repeat {
			p.openSelected()
		}
	case 1:
		if p.openIntent != OpenAny {
			p.SetOpenIntent(OpenAny)
		} else {
			p.previewActive, p.dirty = false, true
		}
	case 103, 108:
		delta := 1
		if code == 103 {
			delta = -1
		}
		if p.previewActive {
			p.scroll(delta, false)
		} else if len(p.entries) > 0 {
			p.selectEntry(max(0, min(len(p.entries)-1, p.selected+delta)))
		}
	case 105, 106:
		delta := 4
		if code == 105 {
			delta = -4
		}
		p.scroll(delta, true)
	case 104, 109:
		delta := p.rows()
		if code == 104 {
			delta = -delta
		}
		if p.previewActive {
			p.scroll(delta, false)
		} else if len(p.entries) > 0 {
			p.selectEntry(max(0, min(len(p.entries)-1, p.selected+delta)))
		}
	case 102, 107:
		if p.previewActive {
			p.previewTop = 0
			if code == 107 {
				p.previewTop = len(p.preview.starts)
			}
			p.clampScroll()
			p.dirty = true
		} else if len(p.entries) > 0 {
			index := 0
			if code == 107 {
				index = len(p.entries) - 1
			}
			p.selectEntry(index)
		}
	}
}

func (p *Provider) CloseApplication(id uint64) {
	if id == 1 {
		_ = p.Close()
	}
}

// Close removes the surface immediately and cancels work without waiting for a
// filesystem syscall. The worker closes its root anchor when that work unwinds;
// canceled or late results cannot mutate the closed provider.
func (p *Provider) Close() error {
	if p.closed {
		return nil
	}
	p.closed, p.focused = true, false
	p.cancelFieldPaste()
	p.syncFieldContext()
	p.cancel()
	drainResults(p.results)
	p.retired = append(p.retired, p.renderer.texture.ID())
	p.surfaces, p.entries, p.preview = nil, nil, textPreview{}
	p.allEntries = nil
	p.dialog, p.fileHistory = nil, nil
	p.thumbnails, p.thumbnailOrder = nil, nil
	p.text.Close()
	p.copyReady, p.copyText = false, ""
	p.openHandler = nil
	p.terminalHandler = nil
	p.sessionRoot = nil
	p.pendingSelected = ""
	p.openingTerminal = false
	p.renderer.close()
	p.renderer.image, p.renderer.texture, p.renderer.prior = nil, nil, nil
	return nil
}

func (p *Provider) RetiredTextures() []uint64 {
	retired := p.retired
	p.retired = nil
	return retired
}

func (p *Provider) Retired() []uint64 { return p.RetiredTextures() }

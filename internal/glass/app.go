// Package glass implements the standalone WorldR terminal on the kit runtime.
package glass

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/glass/commands"
	"github.com/codemodify/worldr/internal/nativeui"
	"github.com/codemodify/worldr/internal/render"
	"github.com/codemodify/worldr/internal/scene"
	"github.com/codemodify/worldr/internal/terminal"
	"github.com/codemodify/worldr/internal/textinput"
	kit "github.com/codemodify/worldr/sdk/app/v1"
)

type Preferences struct {
	Palette  int     `json:"palette"`
	Opacity  float32 `json:"opacity"`
	Glow     float32 `json:"glow"`
	FontSize float32 `json:"fontSize"`
	Motion   bool    `json:"motion"`
}

func DefaultPreferences() Preferences {
	return Preferences{Opacity: .82, Glow: .7, FontSize: 15, Motion: true}
}
func (p Preferences) Valid() bool {
	return p.Palette >= 0 && p.Palette < 3 && finiteRange(p.Opacity, .35, 1) && finiteRange(p.Glow, 0, 1) && finiteRange(p.FontSize, 11, 24)
}
func finiteRange(v, a, b float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) && v >= a && v <= b
}

type rect struct{ x, y, w, h float32 }

func (r rect) contains(x, y float32) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

type button struct {
	id, label string
	box       rect
}
type glyphKey struct {
	text         string
	width        int
	size         int
	bold, italic bool
	foreground   terminal.Color
}

type App struct {
	canvas                                *scene.Canvas
	host                                  *kit.Host
	tabs                                  []*terminalPane
	active, nextID                        int
	options                               terminal.Options
	prefs                                 Preferences
	appliedPalette                        int
	width, height                         int
	scale, unit                           float32
	frame, body, settingsBox              rect
	buttons                               []button
	font, cellW, cellH                    float32
	settings                              bool
	drawer, tabReveal, entrance, activity float32
	opacity, glow                         float32
	accent                                scene.Color
	elapsed                               time.Duration
	cursorOn, focused, dirty              bool
	hover, pressed, slider                string
	owned                                 map[uint32]bool
	keymap                                experience.Event
	repeat                                experience.Event
	notice                                string
	noticeUntil                           time.Duration
	err                                   error
	closed                                bool
	fallback                              *nativeui.Painter
	glyphs                                map[glyphKey]*render.Texture
	glyphLastUsed                         map[glyphKey]uint64
	glyphFrame                            uint64
	retired                               []uint64
	lastTitle                             string
	workspace, rail                       rect
	historyView, searchOpen               bool
	viewReveal                            float32
	searchReveal                          float32
	cards                                 []commands.Card
	historyScroll, historyTarget          float32
	historyHeight                         float32
	historyTargets                        []blockTarget
	historyRevision                       uint64
	historyPoll                           time.Duration
	selectedCard                          uint64
	expanded                              map[uint64]bool
	searchText                            string
	searchCursor                          int
	searchSelect                          bool
	searchBox                             rect
	searchResults                         terminal.SearchResults
	searchRows                            map[uint64][]terminal.SearchMatch
	searchIndex                           int
	searchRevision                        uint64
	searchPoll                            time.Duration
	translator                            *textinput.Translator
	cleanupIntegration                    func()
	integrated                            bool
	cwd                                   string
	contextPoll                           time.Duration
}

func New(options terminal.Options, prefs Preferences) (*App, error) {
	if !prefs.Valid() {
		return nil, fmt.Errorf("invalid terminal preferences")
	}
	c, err := scene.NewCanvasWithFont(terminalFont)
	if err != nil {
		return nil, err
	}
	theme := nativeui.Cinematic()
	theme.Font = "Monospace"
	painter, err := nativeui.NewPainter(theme)
	if err != nil {
		c.Close()
		return nil, err
	}
	a := &App{canvas: c, options: options, prefs: prefs, appliedPalette: prefs.Palette, scale: 1, unit: 1, focused: true, dirty: true, owned: map[uint32]bool{}, fallback: painter, glyphs: map[glyphKey]*render.Texture{}, opacity: prefs.Opacity, glow: prefs.Glow}
	a.accent = palette(prefs.Palette)
	a.expanded = map[uint64]bool{}
	a.searchIndex = -1
	a.translator, err = textinput.New()
	if err != nil {
		a.Close()
		return nil, err
	}
	a.options, a.cleanupIntegration, a.integrated, err = commands.Prepare(options)
	if err != nil {
		a.Close()
		return nil, err
	}
	if err := a.addTab(); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}
func (a *App) SetHost(h *kit.Host) { a.host = h }
func (a *App) SetScale(scale float32) {
	if scale > 0 && scale != a.scale {
		a.scale = scale
		a.width = 0
		a.dirty = true
		a.clearGlyphs()
	}
}
func (a *App) Atlas() render.Atlas       { return a.canvas.Atlas() }
func (a *App) Preferences() Preferences  { return a.prefs }
func (a *App) NeedsFrame() bool          { return a.dirty }
func (a *App) RetiredTextures() []uint64 { r := a.retired; a.retired = nil; return r }
func (a *App) pane() *terminalPane {
	if len(a.tabs) == 0 {
		return nil
	}
	return a.tabs[a.active]
}
func (a *App) remember(err error) {
	if err != nil && a.err == nil {
		a.err = err
	}
}
func (a *App) say(s string) { a.notice = s; a.noticeUntil = a.elapsed + 3*time.Second; a.dirty = true }
func (a *App) clearGlyphs() {
	for _, t := range a.glyphs {
		a.retired = append(a.retired, t.ID())
	}
	a.glyphs = map[glyphKey]*render.Texture{}
	a.glyphLastUsed = map[glyphKey]uint64{}
}
func (a *App) Close() error {
	if a.closed {
		return nil
	}
	a.closed = true
	var err error
	for _, p := range a.tabs {
		err = errors.Join(err, p.Close())
	}
	a.tabs = nil
	if a.cleanupIntegration != nil {
		a.cleanupIntegration()
	}
	if a.translator != nil {
		a.translator.Close()
	}
	a.clearGlyphs()
	if a.fallback != nil {
		a.fallback.Close()
	}
	if a.canvas != nil {
		err = errors.Join(err, a.canvas.Close())
	}
	return err
}
func (a *App) addTab() error {
	if len(a.tabs) >= 8 {
		a.say("Eight sessions are already open")
		return nil
	}
	opts := a.options
	if p := a.pane(); p != nil {
		opts.Cols, opts.Rows = p.snapshot.Cols, p.snapshot.Rows
		if directory, err := p.term.WorkingDirectory(); err == nil {
			if file, err := os.Open(directory); err == nil {
				defer file.Close()
				opts.Directory = file
			}
		}
	}
	p, err := newTerminalPane(opts)
	if err != nil {
		return err
	}
	if err := p.term.SetPalette(ansiPalette(a.prefs.Palette)); err != nil {
		p.Close()
		return err
	}
	if a.keymap.Keymap != "" {
		if err = p.Handle(a.keymap); err != nil {
			p.Close()
			return err
		}
	}
	if a.repeat.Kind != 0 {
		p.Handle(a.repeat)
	}
	a.nextID++
	p.id = a.nextID
	a.tabs = append(a.tabs, p)
	a.switchTab(len(a.tabs) - 1)
	return nil
}
func (a *App) switchTab(i int) {
	if len(a.tabs) == 0 {
		return
	}
	if a.pane() != nil {
		a.pane().term.Focus(false)
		a.pane().selecting = false
	}
	a.active = (i + len(a.tabs)) % len(a.tabs)
	a.pane().term.Focus(true)
	a.tabReveal = 0
	a.cards = nil
	a.historyRevision, a.searchRevision = 0, 0
	a.historyPoll, a.searchPoll = 0, 0
	a.historyScroll, a.historyTarget = 0, 0
	a.expanded = map[uint64]bool{}
	a.selectedCard = 0
	a.searchResults = terminal.SearchResults{}
	a.searchRows = nil
	a.searchIndex = -1
	a.contextPoll = 0
	if a.historyView || a.searchOpen {
		a.pane().term.Focus(false)
	}
	a.width = 0
	a.dirty = true
}
func (a *App) closeTab() {
	if len(a.tabs) <= 1 {
		if a.host != nil {
			a.host.RequestClose()
		}
		return
	}
	p := a.pane()
	a.remember(p.Close())
	a.tabs = append(a.tabs[:a.active], a.tabs[a.active+1:]...)
	a.active = min(a.active, len(a.tabs)-1)
	a.switchTab(a.active)
}
func (a *App) Update(dt time.Duration) error {
	if a.err != nil {
		return a.err
	}
	a.elapsed += dt
	if a.appliedPalette != a.prefs.Palette {
		colors := ansiPalette(a.prefs.Palette)
		for _, p := range a.tabs {
			if err := p.term.SetPalette(colors); err != nil {
				return err
			}
		}
		a.appliedPalette = a.prefs.Palette
	}
	for _, p := range a.tabs {
		old := p.snapshot.Revision
		if err := p.Poll(); err != nil {
			return err
		}
		if old != p.snapshot.Revision {
			a.dirty = true
			a.activity = 1
		}
	}
	if p := a.pane(); p != nil {
		cursor := !p.snapshot.Cursor.Blink || a.elapsed.Milliseconds()%1100 < 550
		if cursor != a.cursorOn {
			a.cursorOn = cursor
			if p.snapshot.Cursor.Visible && a.focused {
				a.dirty = true
			}
		}
		title := "WorldR Terminal"
		if p.snapshot.Title != "" {
			title += " · " + p.snapshot.Title
		}
		if title != a.lastTitle && a.host != nil {
			a.host.SetTitle(title)
			a.lastTitle = title
		}
	}
	if a.notice != "" && a.elapsed > a.noticeUntil {
		a.notice = ""
		a.dirty = true
	}
	speed := float32(1 - math.Exp(-float64(dt.Seconds())*15))
	if !a.prefs.Motion {
		speed = 1
	}
	approach := func(v *float32, target float32) {
		old := *v
		*v += (target - *v) * speed
		if abs(*v-target) < .002 {
			*v = target
		}
		if *v != old {
			a.dirty = true
		}
	}
	target := float32(0)
	if a.settings {
		target = 1
	}
	approach(&a.drawer, target)
	approach(&a.entrance, 1)
	approach(&a.tabReveal, 1)
	approach(&a.viewReveal, 1)
	searchTarget := float32(0)
	if a.searchOpen {
		searchTarget = 1
	}
	approach(&a.searchReveal, searchTarget)
	approach(&a.historyScroll, a.historyTarget)
	approach(&a.opacity, a.prefs.Opacity)
	approach(&a.glow, a.prefs.Glow)
	approach(&a.activity, 0)
	col := palette(a.prefs.Palette)
	approach(&a.accent.R, col.R)
	approach(&a.accent.G, col.G)
	approach(&a.accent.B, col.B)
	if err := a.updateWorkspace(); err != nil {
		return err
	}
	return nil
}
func palette(i int) scene.Color {
	return scene.ColorHex([]uint32{0x76deec, 0xf3c77b, 0xc0a3f5}[min(2, max(0, i))], 1)
}
func abs(x float32) float32 { return float32(math.Abs(float64(x))) }
func (a *App) shellLabel() string {
	s := a.options.Command
	if s == "" {
		s = "shell"
	}
	return filepath.Base(s)
}

// Advanced Desktop is a dense, working studio interface built with the public SDK.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	nativeapp "github.com/codemodify/worldr/sdk/nativeapp/v1"
	nativeui "github.com/codemodify/worldr/sdk/nativeui/v1"
	skin "github.com/codemodify/worldr/sdk/skin/v1"
)

//go:embed assets/architecture.png
var architecturePNG []byte

type editor struct {
	Text           string
	Cursor, Anchor int
	Preedit        string
}
type studioState struct {
	Section, Portfolio, UpdateOffset, SelectedUpdate int
	Playing, Subscribed, InvalidEmail, Transmissions bool
	Reel                                             float64
	Filter, Email                                    editor
	Status                                           string
}
type studio struct {
	selected                                             skin.Skin
	state                                                studioState
	host                                                 nativeapp.Host
	controller                                           nativeui.Controller
	controls                                             []nativeui.Control
	painters                                             map[int]*nativeui.Painter
	architecture                                         image.Image
	photographs                                          [2]cachedPhotograph
	pixels                                               *image.RGBA
	width, height                                        int
	revision                                             uint64
	dirty, published, retired, closed, released, focused bool
	clock                                                time.Duration
}

func (s *studio) Manifest() nativeapp.Manifest {
	return nativeapp.Manifest{ID: "dev.worldr.advanced-desktop", Name: "Advanced Studio", Description: "A working steel-blue studio interface", Skins: true}
}
func (s *studio) Start(host nativeapp.Host) error {
	if host.MaxSurfaces < 1 || host.MaxSurfaceWidth < 1 || host.MaxSurfaceHeight < 1 {
		return fmt.Errorf("invalid surface limits")
	}
	s.host = host
	s.width, s.height = min(1200, host.MaxSurfaceWidth), min(960, host.MaxSurfaceHeight)
	if s.selected.ID == "" {
		var err error
		s.selected, err = skin.Builtin("advanced")
		if err != nil {
			return err
		}
	}
	var err error
	s.architecture, _, err = image.Decode(bytes.NewReader(architecturePNG))
	if err != nil {
		return fmt.Errorf("decode architecture: %w", err)
	}
	s.state = studioState{Transmissions: true, Status: "SYSTEM ONLINE / ALL CHANNELS NOMINAL"}
	if err := s.setPainters(s.selected); err != nil {
		return err
	}
	if err := s.controller.SetTheme(s.painters[11].Theme()); err != nil {
		return err
	}
	return s.paint()
}
func (s *studio) SetSkin(selected skin.Skin) error {
	if err := selected.Validate(); err != nil {
		return err
	}
	if s.closed {
		return nil
	}
	if err := s.setPainters(selected); err != nil {
		return err
	}
	if err := s.controller.SetTheme(s.painters[11].Theme()); err != nil {
		return err
	}
	s.selected = selected.Clone()
	return s.paint()
}
func (s *studio) Snapshot() nativeapp.Snapshot {
	if s.closed {
		if s.published && !s.retired {
			s.retired = true
			return nativeapp.Snapshot{RetireTextures: []nativeapp.ResourceID{1}}
		}
		return nativeapp.Snapshot{}
	}
	var updates []nativeapp.TextureUpdate
	if s.dirty {
		s.revision++
		updates = []nativeapp.TextureUpdate{{ID: 1, Revision: s.revision, Width: s.width, Height: s.height, Rect: nativeapp.Rect{Width: s.width, Height: s.height}, Pixels: append([]byte(nil), s.pixels.Pix...)}}
		s.dirty, s.published = false, true
	}
	semantics := s.controller.Semantics()
	var text nativeapp.TextInputState
	if e := s.activeEditor(); s.focused && e != nil {
		for _, node := range semantics.Nodes {
			if node.ID == semantics.FocusedID {
				text = nativeapp.TextInputState{Enabled: true, ContextID: node.ID, Surrounding: e.Text, Cursor: e.Cursor, Anchor: e.Anchor, CursorRect: node.Bounds}
				break
			}
		}
	}
	return nativeapp.Snapshot{Textures: updates, Surfaces: []nativeapp.Surface{{ID: 1, Key: "studio", Title: "WORLDR / ADVANCED STUDIOS", Texture: 1, MinWidth: 720, MinHeight: 600, FrameStyle: nativeapp.FrameDefault, Semantics: semantics, TextInput: text}}}
}
func (s *studio) Focus(id nativeapp.SurfaceID) error {
	s.focused = id == 1 && !s.closed
	if !s.focused && !s.closed {
		s.controller.Blur()
		s.state.Email.Preedit, s.state.Filter.Preedit = "", ""
		return s.paint()
	}
	return nil
}
func (s *studio) Resize(id nativeapp.SurfaceID, width, height int) error {
	if id != 1 || s.closed {
		return nil
	}
	width, height = max(1, min(width, s.host.MaxSurfaceWidth)), max(1, min(height, s.host.MaxSurfaceHeight))
	if width == s.width && height == s.height {
		return nil
	}
	s.width, s.height = width, height
	s.photographs = [2]cachedPhotograph{}
	if err := s.setPainters(s.selected); err != nil {
		return err
	}
	if err := s.controller.SetTheme(s.painters[11].Theme()); err != nil {
		return err
	}
	return s.paint()
}
func (s *studio) CloseSurface(id nativeapp.SurfaceID) error {
	if id == 1 {
		s.closed, s.dirty, s.focused = true, false, false
		s.controller.Blur()
	}
	return nil
}
func (s *studio) Close() error {
	if s.released {
		return nil
	}
	s.closed, s.released = true, true
	s.photographs = [2]cachedPhotograph{}
	var errs []error
	for _, p := range s.painters {
		errs = append(errs, p.Close())
	}
	errs = append(errs, s.controller.Close())
	return errors.Join(errs...)
}
func (s *studio) Update(delta time.Duration) error {
	if s.closed || !s.state.Playing || delta <= 0 {
		return nil
	}
	s.state.Reel += delta.Seconds() / 30
	if s.state.Reel >= 1 {
		s.state.Reel = 1
		s.state.Playing = false
	}
	s.clock += delta
	if s.clock < 150*time.Millisecond && s.state.Playing {
		return nil
	}
	s.clock = 0
	return s.paint()
}
func (s *studio) activeEditor() *editor {
	switch s.controller.FocusedID() {
	case "filter":
		return &s.state.Filter
	case "email":
		return &s.state.Email
	}
	return nil
}
func (s *studio) subscribe() {
	value := strings.TrimSpace(s.state.Email.Text)
	address, err := mail.ParseAddress(value)
	valid := err == nil && address.Address == value && strings.Contains(strings.SplitN(value, "@", 2)[1], ".")
	s.state.InvalidEmail, s.state.Subscribed = !valid, valid
	if valid {
		s.state.Status = "SUBSCRIPTION SAVED LOCALLY / " + value
	} else {
		s.state.Status = "ENTER A VALID EMAIL ADDRESS"
	}
}
func (s *studio) filteredUpdates() []int {
	var matches []int
	q := strings.ToLower(strings.TrimSpace(s.state.Filter.Text))
	for i, item := range updates {
		if strings.Contains(strings.ToLower(item.title+" "+item.detail), q) {
			matches = append(matches, i)
		}
	}
	return matches
}
func (s *studio) scrollUpdates(delta int) {
	s.state.UpdateOffset = max(0, min(s.state.UpdateOffset+delta, max(0, len(s.filteredUpdates())-3)))
}
func (s *studio) Handle(id nativeapp.SurfaceID, event nativeapp.Event) error {
	if s.closed || id != 1 && !(id == 0 && event.Kind == nativeapp.KeyboardCancel) {
		return nil
	}
	if event.Kind == nativeapp.PointerDown {
		s.focused = true
	}
	before := make([]nativeui.State, len(s.controls))
	for i, c := range s.controls {
		before[i] = s.controller.Decorate(c).State
	}
	previousFocus := s.controller.FocusedID()
	action := s.controller.Handle(event)
	changed := action.Changed || action.Activated || action.ChangedFocus
	for i, c := range s.controls {
		if before[i] != s.controller.Decorate(c).State {
			changed = true
			break
		}
	}
	if previousFocus != s.controller.FocusedID() {
		s.state.Email.Preedit, s.state.Filter.Preedit = "", ""
	}
	if event.Kind == nativeapp.KeyboardCancel {
		s.focused = false
	}
	if action.Activated {
		switch {
		case strings.HasPrefix(action.ID, "nav-"):
			s.state.Section, _ = strconv.Atoi(strings.TrimPrefix(action.ID, "nav-"))
			s.state.Status = "SECTION / " + strings.ToUpper(sections[s.state.Section].name)
		case strings.HasPrefix(action.ID, "portfolio-"):
			s.state.Portfolio, _ = strconv.Atoi(strings.TrimPrefix(action.ID, "portfolio-"))
			s.state.Status = "PORTFOLIO / " + projects[s.state.Portfolio].title
		case strings.HasPrefix(action.ID, "update-"):
			s.state.SelectedUpdate, _ = strconv.Atoi(strings.TrimPrefix(action.ID, "update-"))
			s.state.Status = updates[s.state.SelectedUpdate].detail
		case action.ID == "play":
			if s.state.Reel >= 1 {
				s.state.Reel = 0
			}
			s.state.Playing = !s.state.Playing
		case action.ID == "reel-reset":
			s.state.Playing, s.state.Reel = false, 0
		case action.ID == "updates-prev":
			s.scrollUpdates(-1)
		case action.ID == "updates-next":
			s.scrollUpdates(1)
		case action.ID == "subscribe":
			s.subscribe()
		case action.ID == "transmissions":
			s.state.Transmissions = !s.state.Transmissions
		}
	}
	if event.Kind == nativeapp.PointerScroll {
		if image.Pt(int(event.X), int(event.Y)).In(s.bounds(744, 542, 420, 182)) && event.ScrollY != 0 {
			if event.ScrollY > 0 {
				s.scrollUpdates(1)
			} else {
				s.scrollUpdates(-1)
			}
			changed = true
		}
	}
	if e := s.activeEditor(); e != nil && s.focused {
		if event.TextContext == "" || event.TextContext == s.controller.FocusedID() {
			if edit(e, event) {
				changed = true
				if s.controller.FocusedID() == "filter" {
					s.state.UpdateOffset = 0
				} else {
					s.state.InvalidEmail, s.state.Subscribed = false, false
				}
			}
		}
		if s.controller.FocusedID() == "email" && event.Kind == nativeapp.KeyInput && event.Pressed && !event.Repeat && (event.Key == "Enter" || event.Keycode == 28) {
			s.subscribe()
			changed = true
		}
	}
	if changed {
		return s.paint()
	}
	return nil
}

func edit(e *editor, event nativeapp.Event) bool {
	lo, hi := min(e.Cursor, e.Anchor), max(e.Cursor, e.Anchor)
	replace := func(text string) bool {
		if !utf8.ValidString(text) || len(e.Text)-(hi-lo)+len(text) > 254 {
			return false
		}
		e.Text = e.Text[:lo] + text + e.Text[hi:]
		e.Cursor, e.Anchor = lo+len(text), lo+len(text)
		e.Preedit = ""
		return true
	}
	switch event.Kind {
	case nativeapp.TextCommit:
		if event.DeleteBefore != 0 || event.DeleteAfter != 0 {
			lo, hi = max(0, e.Cursor-int(event.DeleteBefore)), min(len(e.Text), e.Cursor+int(event.DeleteAfter))
			for lo > 0 && lo < len(e.Text) && !utf8.RuneStart(e.Text[lo]) {
				lo--
			}
			for hi < len(e.Text) && !utf8.RuneStart(e.Text[hi]) {
				hi++
			}
		}
		return replace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, event.Text))
	case nativeapp.TextPreedit:
		if utf8.ValidString(event.Text) && len(event.Text) <= 254 {
			e.Preedit = event.Text
			return true
		}
	case nativeapp.KeyInput:
		if !event.Pressed {
			return false
		}
		if event.Modifiers.Has(nativeapp.ModControl) && (strings.EqualFold(event.Key, "a") || event.Keycode == 30) {
			e.Anchor, e.Cursor = 0, len(e.Text)
			return true
		}
		move := func(position int) bool {
			e.Cursor = position
			if !event.Modifiers.Has(nativeapp.ModShift) {
				e.Anchor = e.Cursor
			}
			return true
		}
		switch {
		case event.Key == "ArrowLeft" || event.Keycode == 105:
			if lo != hi && !event.Modifiers.Has(nativeapp.ModShift) {
				return move(lo)
			}
			_, n := utf8.DecodeLastRuneInString(e.Text[:e.Cursor])
			return move(max(0, e.Cursor-n))
		case event.Key == "ArrowRight" || event.Keycode == 106:
			if lo != hi && !event.Modifiers.Has(nativeapp.ModShift) {
				return move(hi)
			}
			_, n := utf8.DecodeRuneInString(e.Text[e.Cursor:])
			return move(min(len(e.Text), e.Cursor+n))
		case event.Key == "Home" || event.Keycode == 102:
			return move(0)
		case event.Key == "End" || event.Keycode == 107:
			return move(len(e.Text))
		case event.Key == "Backspace" || event.Keycode == 14:
			if lo == hi && lo > 0 {
				_, n := utf8.DecodeLastRuneInString(e.Text[:lo])
				lo -= n
			}
			return replace("")
		case event.Key == "Delete" || event.Keycode == 111:
			if lo == hi && hi < len(e.Text) {
				_, n := utf8.DecodeRuneInString(e.Text[hi:])
				hi += n
			}
			return replace("")
		}
	}
	return false
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
	selection := flag.String("skin", "advanced", "skin ID or JSON package")
	snapshot := flag.String("snapshot", "", "write the client PNG and exit")
	flag.Parse()
	selected, err := readSkin(*selection)
	if err != nil {
		return err
	}
	s := &studio{selected: selected}
	if *snapshot == "" {
		return nativeapp.Serve(context.Background(), s, os.Stdin, os.Stdout)
	}
	if err := s.Start(nativeapp.Host{Version: nativeapp.Version, MaxSurfaces: 1, MaxSurfaceWidth: 1920, MaxSurfaceHeight: 1080}); err != nil {
		return err
	}
	defer s.Close()
	if err := os.MkdirAll(filepath.Dir(*snapshot), 0755); err != nil {
		return err
	}
	f, err := os.Create(*snapshot)
	if err != nil {
		return err
	}
	return errors.Join(png.Encode(f, s.pixels), f.Close())
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "advanced desktop:", err)
		os.Exit(1)
	}
}

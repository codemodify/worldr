package mediaapp

import (
	"fmt"
	"os"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/media"
)

const MaxViewers = 4

type WindowState struct {
	Key string `json:"key"`
	SessionState
}

func mediaSlotKey(slot int) string {
	if slot == 0 {
		return "native:media-player"
	}
	return fmt.Sprintf("native:media-player-%d", slot+1)
}
func mediaKeySlot(key string) (int, error) {
	for slot := 0; slot < MaxViewers; slot++ {
		if mediaSlotKey(slot) == key {
			return slot, nil
		}
	}
	return -1, fmt.Errorf("invalid media window key %q", key)
}
func (s WindowState) Validate() error {
	if _, err := mediaKeySlot(s.Key); err != nil {
		return err
	}
	return s.SessionState.Validate()
}

type mediaWindow struct {
	manager *Manager
	key     string
	id      uint64
}

// Collection retains independent bounded video players and playback settings.
// Closing one player leaves the others running; fresh runtime IDs protect
// reused stable layout slots from stale input. Calls belong to the host thread.
type Collection struct {
	slots    [MaxViewers]*mediaWindow
	next     uint64
	options  media.Options
	factory  func(media.Options) (playback, error)
	surfaces []experience.ApplicationSurface
	retired  []uint64
	closed   bool
}

var _ experience.Applications = (*Collection)(nil)
var _ experience.ApplicationCloser = (*Collection)(nil)

func NewCollection(options media.Options) *Collection {
	if options.InitialState != nil {
		initial := *options.InitialState
		options.InitialState = &initial
	}
	return &Collection{options: options, factory: func(o media.Options) (playback, error) { return media.New(o) }}
}
func (c *Collection) NextKey() (string, error) {
	if c.closed {
		return "", fmt.Errorf("media collection is closed")
	}
	if c.next == ^uint64(0) {
		return "", fmt.Errorf("media window IDs exhausted")
	}
	for slot, window := range c.slots {
		if window == nil {
			return mediaSlotKey(slot), nil
		}
	}
	return "", fmt.Errorf("video window limit reached (%d); close a video first", MaxViewers)
}

// OpenFile creates another player and transfers descriptor ownership only on
// success. It does not steal keyboard focus or alter another player's state.
func (c *Collection) OpenFile(file *os.File, name string) (string, error) {
	key, err := c.NextKey()
	if err != nil {
		return "", err
	}
	return c.openFile(file, name, key, nil)
}
func (c *Collection) RestoreFile(file *os.File, name string, state WindowState) (string, error) {
	if err := state.Validate(); err != nil {
		return "", err
	}
	return c.openFile(file, name, state.Key, &state.SessionState)
}
func (c *Collection) openFile(file *os.File, name, key string, state *SessionState) (string, error) {
	if c.closed {
		return "", fmt.Errorf("media collection is closed")
	}
	slot, err := mediaKeySlot(key)
	if err != nil {
		return "", err
	}
	if c.slots[slot] != nil {
		return "", fmt.Errorf("media window %s is already open", key)
	}
	if c.next == ^uint64(0) {
		return "", fmt.Errorf("media window IDs exhausted")
	}
	manager := NewManager(c.options)
	manager.factory = c.factory
	if _, err := manager.openFile(file, name, state); err != nil {
		manager.Close()
		return "", err
	}
	c.next++
	c.slots[slot] = &mediaWindow{manager: manager, key: key, id: c.next}
	c.rebuild()
	return key, nil
}
func (c *Collection) rebuild() {
	c.surfaces = c.surfaces[:0]
	for _, window := range c.slots {
		if window == nil {
			continue
		}
		for _, surface := range window.manager.Surfaces() {
			surface.ID, surface.Key = window.id, window.key
			c.surfaces = append(c.surfaces, surface)
		}
	}
}
func (c *Collection) Surfaces() []experience.ApplicationSurface { c.rebuild(); return c.surfaces }
func (c *Collection) Poll() error {
	if c.closed {
		return nil
	}
	for _, window := range c.slots {
		if window != nil {
			if err := window.manager.Poll(); err != nil {
				return err
			}
		}
	}
	c.rebuild()
	return nil
}
func (c *Collection) route(id uint64) *mediaWindow {
	if !c.closed {
		for _, window := range c.slots {
			if window != nil && window.id == id {
				return window
			}
		}
	}
	return nil
}
func (c *Collection) Focus(id uint64) {
	for _, window := range c.slots {
		if window != nil {
			local := uint64(0)
			if window.id == id {
				local = window.manager.next
			}
			window.manager.Focus(local)
		}
	}
}
func (c *Collection) Seat(event experience.Event) {
	for _, window := range c.slots {
		if window != nil {
			window.manager.Seat(event)
		}
	}
}
func (c *Collection) Send(id uint64, event experience.Event) {
	if window := c.route(id); window != nil {
		window.manager.Send(window.manager.next, event)
	}
}
func (c *Collection) Resize(id uint64, width, height int) {
	if window := c.route(id); window != nil {
		window.manager.Resize(window.manager.next, width, height)
	}
}
func (c *Collection) CloseApplication(id uint64) {
	if c.closed {
		return
	}
	for slot, window := range c.slots {
		if window == nil || window.id != id {
			continue
		}
		window.manager.Close()
		c.retired = append(c.retired, window.manager.RetiredTextures()...)
		c.slots[slot] = nil
		c.rebuild()
		return
	}
}
func (c *Collection) RetiredTextures() []uint64 {
	for _, window := range c.slots {
		if window != nil {
			c.retired = append(c.retired, window.manager.RetiredTextures()...)
		}
	}
	ids := c.retired
	c.retired = nil
	return ids
}
func (c *Collection) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	for slot, window := range c.slots {
		if window != nil {
			window.manager.Close()
			c.retired = append(c.retired, window.manager.RetiredTextures()...)
			c.slots[slot] = nil
		}
	}
	c.surfaces = nil
	return nil
}
func (c *Collection) SessionStates() []WindowState {
	if c.closed {
		return nil
	}
	var states []WindowState
	for _, window := range c.slots {
		if window != nil {
			if state, ok := window.manager.SessionState(); ok {
				states = append(states, WindowState{Key: window.key, SessionState: state})
			}
		}
	}
	return states
}

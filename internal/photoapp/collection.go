package photoapp

import (
	"context"
	"fmt"
	"image"
	"os"

	"github.com/codemodify/worldr/internal/experience"
)

const MaxViewers = 8
const maxCollectionPixels int64 = 64_000_000

type WindowState struct {
	Key string `json:"key"`
	SessionState
}

func photoSlotKey(slot int) string {
	if slot == 0 {
		return "native:photo-viewer"
	}
	return fmt.Sprintf("native:photo-viewer-%d", slot+1)
}
func photoKeySlot(key string) (int, error) {
	for slot := 0; slot < MaxViewers; slot++ {
		if photoSlotKey(slot) == key {
			return slot, nil
		}
	}
	return -1, fmt.Errorf("invalid photo window key %q", key)
}
func (s WindowState) Validate() error {
	if _, err := photoKeySlot(s.Key); err != nil {
		return err
	}
	return s.SessionState.Validate()
}

type photoWindow struct {
	manager  *Manager
	key      string
	id       uint64
	permit   chan int64
	admitted bool
	budget   int64
}

// Collection owns independent photo windows. One image decode is admitted at
// a time, including its pending result, and retained photos share a 64 MP cap.
// Surface textures and each encoded read retain their existing separate bounds.
// Calls belong to the host goroutine, as with Manager.
type Collection struct {
	slots      [MaxViewers]*photoWindow
	next       uint64
	active     *photoWindow
	decode     func(context.Context, *os.File, int64) (image.Image, error)
	pixelLimit int64
	surfaces   []experience.ApplicationSurface
	retired    []uint64
	closed     bool
}

var _ experience.Applications = (*Collection)(nil)
var _ experience.ApplicationCloser = (*Collection)(nil)

func NewCollection() *Collection { return newCollection(decodePhotoLimited) }
func newCollection(decode func(context.Context, *os.File, int64) (image.Image, error)) *Collection {
	return &Collection{decode: decode, pixelLimit: maxCollectionPixels}
}

func (c *Collection) NextKey() (string, error) {
	if c.closed {
		return "", fmt.Errorf("photo collection is closed")
	}
	if c.next == ^uint64(0) {
		return "", fmt.Errorf("photo window IDs exhausted")
	}
	for slot, window := range c.slots {
		if window == nil {
			return photoSlotKey(slot), nil
		}
	}
	return "", fmt.Errorf("photo window limit reached (%d); close a photo first", MaxViewers)
}

// OpenFile creates a new window and transfers file ownership only on success.
func (c *Collection) OpenFile(file *os.File, name string) (string, error) {
	key, err := c.NextKey()
	if err != nil {
		return "", err
	}
	return c.openFile(file, name, key)
}

// RestoreFile reserves precisely the saved stable slot; it never replaces a
// live viewer. The host opens the validated saved resource without symlinks.
func (c *Collection) RestoreFile(file *os.File, name string, state WindowState) (string, error) {
	if err := state.Validate(); err != nil {
		return "", err
	}
	return c.openFile(file, name, state.Key)
}

func (c *Collection) openFile(file *os.File, name, key string) (string, error) {
	if c.closed {
		return "", fmt.Errorf("photo collection is closed")
	}
	slot, err := photoKeySlot(key)
	if err != nil {
		return "", err
	}
	if c.slots[slot] != nil {
		return "", fmt.Errorf("photo window %s is already open", key)
	}
	if c.next == ^uint64(0) {
		return "", fmt.Errorf("photo window IDs exhausted")
	}
	permit := make(chan int64, 1)
	manager := newManager(func(ctx context.Context, file *os.File) (image.Image, error) {
		select {
		case budget := <-permit:
			return c.decode(ctx, file, budget)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if _, err := manager.OpenFile(file, name); err != nil {
		manager.Close()
		return "", err
	}
	c.next++
	window := &photoWindow{manager: manager, key: key, id: c.next, permit: permit}
	c.slots[slot] = window
	c.rebuild()
	c.admitNext()
	return key, nil
}

func (c *Collection) retainedPixels() int64 {
	var pixels int64
	for _, window := range c.slots {
		if window != nil && window.manager.photo != nil {
			bounds := window.manager.photo.Bounds()
			pixels += int64(bounds.Dx()) * int64(bounds.Dy())
		}
	}
	return pixels
}
func (c *Collection) admitNext() {
	if c.closed || c.active != nil {
		return
	}
	for _, window := range c.slots {
		if window != nil && window.manager.loading && !window.admitted {
			window.budget = max(0, c.pixelLimit-c.retainedPixels())
			window.manager.pixelBudget = &window.budget
			window.admitted = true
			c.active = window
			window.permit <- window.budget
			return
		}
	}
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
	if c.active != nil && c.active.manager.closed {
		select {
		case <-c.active.manager.done:
			c.active = nil
		default:
		}
	}
	for _, window := range c.slots {
		if window == nil {
			continue
		}
		if err := window.manager.Poll(); err != nil {
			return err
		}
		if window == c.active && !window.manager.loading {
			c.active = nil
		}
	}
	c.rebuild()
	c.admitNext()
	return nil
}
func (c *Collection) route(id uint64) *photoWindow {
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
		// A canceled codec may still be unwinding. Poll waits for its worker
		// completion before admitting another decode, without blocking the host.
		c.rebuild()
		c.admitNext()
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
	c.active, c.surfaces = nil, nil
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

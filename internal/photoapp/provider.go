// Package photoapp presents local images as native retained workspace content.
package photoapp

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/resourcepath"
)

// Manager owns one viewer. Host-facing methods belong to the host goroutine;
// one worker owns file reads/decoding. A replacement leaves the current photo
// intact until decoding succeeds, including when the new file is malformed.
type Manager struct {
	renderer               *photoRenderer
	photo                  image.Image
	title, message         string
	path, pendingPath      string
	next, generation       uint64
	surfaces               []experience.ApplicationSurface
	retired                []uint64
	loading, dirty, closed bool
	context                context.Context
	cancel, cancelRequest  context.CancelFunc
	requests               chan photoRequest
	results                chan photoResult
	done                   chan struct{}
	pixelBudget            *int64 // Collection admission; nil preserves the standalone limit.
}

var _ experience.Applications = (*Manager)(nil)
var _ experience.ApplicationCloser = (*Manager)(nil)

func NewManager() *Manager { return newManager(decodePhoto) }

func newManager(decode photoDecoder) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{context: ctx, cancel: cancel,
		requests: make(chan photoRequest, 1), results: make(chan photoResult, 1), done: make(chan struct{})}
	go runPhotoDecoder(ctx, decode, m.requests, m.results, m.done)
	return m
}

// OpenFile takes ownership only after successful enqueue. The worker closes the
// descriptor after decoding or cancellation; it reads that descriptor rather
// than reopening a pathname. The first request reserves a loading surface.
func (m *Manager) OpenFile(file *os.File, name string) (string, error) {
	if m.closed {
		return "", fmt.Errorf("photo viewer is closed")
	}
	if file == nil {
		return "", fmt.Errorf("no image file")
	}
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("only regular image files can be opened")
	}
	if info.Size() > maxEncodedBytes {
		return "", fmt.Errorf("image file exceeds the 64 MiB limit")
	}
	if m.next == ^uint64(0) || m.generation == ^uint64(0) {
		return "", fmt.Errorf("photo viewer IDs exhausted")
	}
	name = photoName(name)
	if m.renderer == nil {
		r, err := newPhotoRenderer(1120, 760)
		if err != nil {
			return "", err
		}
		if err := r.paint("", true, nil); err != nil {
			r.close()
			return "", err
		}
		m.renderer, m.title = r, name
		m.next++
		m.publishSurface()
	}
	if m.cancelRequest != nil {
		m.cancelRequest()
	}
	drainPhotoRequests(m.requests)
	drainPhotoResults(m.results)
	ctx, cancel := context.WithCancel(m.context)
	m.cancelRequest = cancel
	m.generation++
	m.pendingPath, _ = resourcepath.FromFile(file)
	m.requests <- photoRequest{ctx: ctx, generation: m.generation, file: file, name: name}
	m.loading, m.dirty, m.message = true, true, ""
	return "native:photo-viewer", nil
}

func photoName(name string) string {
	name = filepath.Base(name)
	var result strings.Builder
	count := 0
	for _, c := range name {
		if unicode.IsControl(c) || unicode.Is(unicode.Cf, c) {
			continue
		}
		if count >= 256 {
			break
		}
		result.WriteRune(c)
		count++
	}
	return result.String()
}

func (m *Manager) publishSurface() {
	m.surfaces = []experience.ApplicationSurface{{ID: m.next, Key: "native:photo-viewer", AppID: "worldr.photo-viewer", Title: m.title, Texture: m.renderer.texture, DragContent: true}}
}

func (m *Manager) accept(result photoResult) error {
	if result.generation != m.generation || m.renderer == nil {
		return nil
	}
	m.loading, m.dirty = false, true
	m.pendingPath = ""
	if result.err == nil && result.image == nil {
		result.err = fmt.Errorf("image has no pixels")
	}
	if result.err == nil {
		result.err = validImageSize(result.image.Bounds().Dx(), result.image.Bounds().Dy())
	}
	if result.err == nil && m.pixelBudget != nil && int64(result.image.Bounds().Dx())*int64(result.image.Bounds().Dy()) > *m.pixelBudget {
		result.err = fmt.Errorf("photo windows exceed their combined 64 megapixel limit; close a photo and open this file again")
	}
	if result.err != nil {
		m.message = "Unable to open " + result.name + ": " + result.err.Error()
		return nil
	}
	if m.photo != nil {
		// Fresh identity invalidates stale pointer/key routes while the stable
		// document key retains the viewer's existing workspace placement.
		width, height := m.renderer.requested.X, m.renderer.requested.Y
		r, err := newPhotoRenderer(width, height)
		if err != nil {
			return err
		}
		m.retired = append(m.retired, m.renderer.texture.ID())
		m.renderer.close()
		m.renderer = r
		m.next++
	}
	m.photo, m.title, m.message = result.image, result.name, ""
	m.path = result.path
	m.publishSurface()
	return nil
}

// Poll never returns file/codec errors; these remain visible in the viewer.
// It consumes a bounded number of completed results and repaints on changes.
func (m *Manager) Poll() error {
	if m.closed {
		return nil
	}
	for i := 0; i < 2; i++ {
		select {
		case result := <-m.results:
			if err := m.accept(result); err != nil {
				return err
			}
		default:
			i = 2
		}
	}
	if m.dirty {
		return m.paint()
	}
	return nil
}

func (m *Manager) paint() error {
	if m.renderer == nil {
		m.dirty = false
		return nil
	}
	err := m.renderer.paint(m.message, m.loading, m.photo)
	if err == nil {
		m.dirty = false
	}
	return err
}

func (m *Manager) closeCurrent() {
	if m.cancelRequest != nil {
		m.cancelRequest()
		m.cancelRequest = nil
	}
	drainPhotoRequests(m.requests)
	drainPhotoResults(m.results)
	if m.renderer != nil {
		m.retired = append(m.retired, m.renderer.texture.ID())
		m.renderer.close()
	}
	m.renderer, m.photo, m.surfaces = nil, nil, nil
	m.loading, m.dirty = false, false
	m.message, m.title = "", ""
	m.path, m.pendingPath = "", ""
}

func (m *Manager) Close() error {
	if !m.closed {
		m.closed = true
		m.cancel()
		m.closeCurrent()
	}
	return nil
}
func (m *Manager) Surfaces() []experience.ApplicationSurface { return m.surfaces }
func (m *Manager) RetiredTextures() []uint64                 { ids := m.retired; m.retired = nil; return ids }
func (m *Manager) valid(id uint64) bool                      { return !m.closed && m.renderer != nil && id == m.next }
func (m *Manager) CloseApplication(id uint64) {
	if m.valid(id) {
		m.closeCurrent()
	}
}

// The photo has no private input mode. Workspace owns content dragging and
// focus; typing, wheel, and pointer events never transform the photograph.
func (m *Manager) Focus(uint64)                  {}
func (m *Manager) Seat(experience.Event)         {}
func (m *Manager) Send(uint64, experience.Event) {}

func (m *Manager) Resize(id uint64, width, height int) {
	if !m.valid(id) {
		return
	}
	width, height = max(minWidth, min(maxWidth, width)), max(minHeight, min(maxHeight, height))
	if image.Pt(width, height) == m.renderer.requested {
		return
	}
	if err := m.renderer.resize(width, height); err != nil {
		m.message = err.Error()
	}
	m.dirty = true
	if err := m.paint(); err != nil {
		m.message, m.dirty = err.Error(), true
	}
}

package render

import (
	"fmt"
	"image"
	"sync"
	"sync/atomic"
)

// Texture owns a retained image, including the full CPU copy needed by a new
// renderer or a snapshot session. Pixels are tightly packed RGBA8, with rows
// ordered top to bottom. Ordinary surfaces ignore alpha. ImageCommand and
// Translucent surfaces expect premultiplied RGB and honor alpha coverage. RGB
// bytes are authored sRGB; Frame.LinearColor decodes them before filtering and
// lighting, with alpha unassociation/reassociation for premultiplied content.
// The default renderer preserves its original encoded-RGB UNORM behavior.
//
// Updates are copied and versioned. Each renderer tracks its own last uploaded
// revision; a renderer missing more than one change receives the full current
// image. No consumer can clear another consumer's pending damage.
//
// Accessors and updates are safe concurrently, but a frame's textures must not
// be mutated between building and submitting that frame if exact frame-state
// correspondence (including picking) is required.
type Texture struct {
	id       uint64
	mu       sync.RWMutex
	width    int
	height   int
	pixels   []byte
	revision uint64
	damage   image.Rectangle
	external ExternalImage
}

var textureSequence atomic.Uint64

// TextureUpdate is an owned snapshot of one rectangular upload. Width/Height
// describe the entire image; Pixels contains only Rect, with no row padding.
// A full rectangle also permits the backend to create or resize the image.
type TextureUpdate struct {
	Width, Height int
	Revision      uint64
	Rect          image.Rectangle
	Pixels        []byte
}

// NewTexture validates and copies its input. Images are limited to 8192 pixels
// per dimension; a backend may impose a smaller device-specific limit.
func NewTexture(width, height int, pixels []byte) (*Texture, error) {
	if err := validateTexture(width, height, pixels); err != nil {
		return nil, err
	}
	return &Texture{
		id: textureSequence.Add(1), width: width, height: height,
		pixels: append([]byte(nil), pixels...), revision: 1,
		damage: image.Rect(0, 0, width, height),
	}, nil
}

func validateTexture(width, height int, pixels []byte) error {
	if width <= 0 || height <= 0 || width > 8192 || height > 8192 {
		return fmt.Errorf("texture dimensions must be in [1,8192]")
	}
	if uint64(width)*uint64(height)*4 != uint64(len(pixels)) {
		return fmt.Errorf("texture needs %d tightly packed RGBA bytes", uint64(width)*uint64(height)*4)
	}
	return nil
}

func (t *Texture) ID() uint64 {
	if t == nil {
		return 0
	}
	return t.id
}

func (t *Texture) Size() (int, int) {
	if t == nil {
		return 0, 0
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.width, t.height
}

func (t *Texture) Revision() uint64 {
	if t == nil {
		return 0
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.revision
}

// Replace replaces the full image, optionally resizing it without changing its
// identity. Validation failure leaves the image and revision unchanged.
func (t *Texture) Replace(width, height int, pixels []byte) error {
	if t.ID() == 0 {
		return fmt.Errorf("uninitialized texture")
	}
	if err := validateTexture(width, height, pixels); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.external != nil {
		return fmt.Errorf("external textures are immutable")
	}
	if t.revision == ^uint64(0) {
		return fmt.Errorf("texture revision exhausted")
	}
	t.pixels = append(t.pixels[:0], pixels...)
	t.width, t.height = width, height
	t.damage = image.Rect(0, 0, width, height)
	t.revision++
	return nil
}

// Update copies tightly packed rows into a nonempty rectangle within the image.
// Invalid damage or data leaves the resource untouched.
func (t *Texture) Update(rect image.Rectangle, pixels []byte) error {
	if t.ID() == 0 {
		return fmt.Errorf("uninitialized texture")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.external != nil {
		return fmt.Errorf("external textures are immutable")
	}
	if rect.Empty() || !rect.In(image.Rect(0, 0, t.width, t.height)) {
		return fmt.Errorf("texture damage is empty or out of bounds")
	}
	if uint64(rect.Dx())*uint64(rect.Dy())*4 != uint64(len(pixels)) {
		return fmt.Errorf("texture damage needs %d tightly packed RGBA bytes", rect.Dx()*rect.Dy()*4)
	}
	if t.revision == ^uint64(0) {
		return fmt.Errorf("texture revision exhausted")
	}
	stride := rect.Dx() * 4
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		dst := (y*t.width + rect.Min.X) * 4
		src := (y - rect.Min.Y) * stride
		copy(t.pixels[dst:dst+stride], pixels[src:src+stride])
	}
	t.damage = rect
	t.revision++
	return nil
}

// Snapshot returns an owned upload for a consumer's last successful revision.
// Since 0 always obtains a full image; the current revision needs no upload.
// Only the immediately preceding revision can use the most recent damage.
func (t *Texture) Snapshot(since uint64) (TextureUpdate, bool) {
	if t.ID() == 0 {
		return TextureUpdate{}, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.external != nil {
		return TextureUpdate{}, false
	}
	if since == t.revision {
		return TextureUpdate{}, false
	}
	rect := image.Rect(0, 0, t.width, t.height)
	if since != 0 && since == t.revision-1 {
		rect = t.damage
	}
	stride := rect.Dx() * 4
	pixels := make([]byte, stride*rect.Dy())
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		src := (y*t.width + rect.Min.X) * 4
		dst := (y - rect.Min.Y) * stride
		copy(pixels[dst:dst+stride], t.pixels[src:src+stride])
	}
	return TextureUpdate{Width: t.width, Height: t.height, Revision: t.revision, Rect: rect, Pixels: pixels}, true
}

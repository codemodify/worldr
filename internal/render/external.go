package render

import "fmt"

// ExternalImage owns an immutable GPU image snapshot. A backend recognizes its
// concrete type and imports it without a CPU pixel copy. Close must be
// idempotent and synchronized against imports. The owner must Close the
// containing Texture after all scene references have been retired.
type ExternalImage interface {
	Size() (int, int)
	Close() error
}

// NewExternalTexture takes ownership only on success. External textures are
// immutable: Replace and Update reject them. Their backing survives individual
// rendering devices and supplies snapshots and graphics recovery.
func NewExternalTexture(source ExternalImage) (*Texture, error) {
	if source == nil {
		return nil, fmt.Errorf("missing external image")
	}
	w, h := source.Size()
	if w <= 0 || h <= 0 || w > 8192 || h > 8192 {
		return nil, fmt.Errorf("external image dimensions must be in [1,8192]")
	}
	return &Texture{id: textureSequence.Add(1), width: w, height: h, revision: 1, external: source}, nil
}

// ExternalSource is borrowed until Texture.Close. The concrete source must
// synchronize Close against imports; returning it never transfers ownership.
func (t *Texture) ExternalSource() ExternalImage {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.external
}

// Close releases an external image's retained handles. It is idempotent; CPU
// textures need no explicit cleanup. Closing a texture still referenced by a
// future frame causes a visible import error instead of substituting pixels.
func (t *Texture) Close() error {
	if source := t.ExternalSource(); source != nil {
		return source.Close()
	}
	return nil
}

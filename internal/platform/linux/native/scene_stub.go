//go:build !linux || !cgo

package native

import "github.com/codemodify/worldr/internal/render"

func (v *VK) SetSceneAtlas(atlas render.Atlas) error { return ErrUnavailable }
func (v *VK) RenderFrame(frame render.Frame, clear [4]float32, dst []byte) error {
	return ErrUnavailable
}
func (v *VK) ReleaseGeometry(id uint64) error { return ErrUnavailable }
func (v *VK) ReleaseTexture(id uint64) error  { return ErrUnavailable }
func (v *VK) SampleCount() int                { return 0 }

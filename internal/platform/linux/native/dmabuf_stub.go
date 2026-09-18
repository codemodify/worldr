//go:build !linux || !cgo

package native

import (
	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
	"github.com/codemodify/worldr/internal/render"
)

func (v *VK) DMABufFormats() []dmabuf.Format                            { return nil }
func (v *VK) ImportDMABuf(d dmabuf.Descriptor) (*render.Texture, error) { return nil, ErrUnavailable }

//go:build linux && cgo

package native

/*
#include "vk_session.h"
#include <unistd.h>
*/
import "C"

import (
	"fmt"
	"runtime"

	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
	"github.com/codemodify/worldr/internal/render"
)

// DMABufFormats advertises explicit single-plane RGB format/modifier pairs the
// device can import. Every pair must also support copying into an exportable
// LINEAR snapshot and then into a sampled renderer image.
func (v *VK) DMABufFormats() []dmabuf.Format {
	if v == nil || v.ptr == nil {
		return nil
	}
	if v.dmabufFormatsReady {
		return append([]dmabuf.Format(nil), v.dmabufFormats...)
	}
	var formats []dmabuf.Format
	for _, fourCC := range []uint32{
		dmabuf.XRGB8888, dmabuf.ARGB8888, dmabuf.XBGR8888, dmabuf.ABGR8888,
		dmabuf.XRGB2101010, dmabuf.ARGB2101010, dmabuf.XBGR2101010, dmabuf.ABGR2101010,
	} {
		count := int(C.worldr_vk_dmabuf_modifiers(v.ptr, C.uint32_t(fourCC), nil, 0))
		remaining := dmabuf.MaxFormatPairs - len(formats)
		if count <= 0 || remaining <= 0 {
			continue
		}
		if count > remaining {
			count = remaining
		}
		modifiers := make([]C.uint64_t, count)
		C.worldr_vk_dmabuf_modifiers(v.ptr, C.uint32_t(fourCC), &modifiers[0], C.int(count))
		for _, modifier := range modifiers {
			formats = append(formats, dmabuf.Format{FourCC: fourCC, Modifier: uint64(modifier)})
		}
	}
	v.dmabufFormats = append([]dmabuf.Format(nil), formats...)
	v.dmabufFormatsReady = true
	return append([]dmabuf.Format(nil), formats...)
}

func (v *VK) supportsDMABufFormat(fourCC uint32) bool {
	return v.supportsDMABufModifier(fourCC, dmabuf.Linear)
}
func (v *VK) supportsDMABufModifier(fourCC uint32, modifier uint64) bool {
	if v == nil || v.ptr == nil {
		return false
	}
	for _, format := range v.DMABufFormats() {
		if format.FourCC == fourCC && format.Modifier == modifier {
			return true
		}
	}
	return false
}
func (v *VK) queryDMABufModifier(fourCC uint32, modifier uint64) bool {
	return v != nil && v.ptr != nil && C.worldr_vk_dmabuf_modifier_supported(v.ptr, C.uint32_t(fourCC), C.uint64_t(modifier)) != 0
}
func rawDMABuf(d dmabuf.Descriptor) C.worldr_dmabuf {
	p := d.Planes[0]
	return C.worldr_dmabuf{width: C.uint32_t(d.Width), height: C.uint32_t(d.Height), format: C.uint32_t(d.FourCC), modifier: C.uint64_t(d.Modifier), offset: C.uint32_t(p.Offset), stride: C.uint32_t(p.Stride), fd: C.int(p.FD)}
}

// ImportDMABuf borrows all descriptor FDs synchronously. It waits at most two
// seconds for implicit producer fences, then GPU-copies into an owned exported
// snapshot. Successful return means the client buffer may be released. No pixel
// readback occurs. The caller owns Texture.Close and GPU texture retirement.
func (v *VK) ImportDMABuf(d dmabuf.Descriptor) (*render.Texture, error) {
	if v == nil || v.ptr == nil {
		return nil, fmt.Errorf("Vulkan session closed")
	}
	if err := d.ValidateImport(); err != nil {
		return nil, err
	}
	if !v.supportsDMABufModifier(d.FourCC, d.Modifier) {
		return nil, dmabuf.ErrUnsupported
	}
	raw := rawDMABuf(d)
	var result C.worldr_dmabuf
	result.fd = -1
	var errb [errBuf]C.char
	if C.worldr_vk_copy_dmabuf(v.ptr, &raw, &result, &errb[0], C.int(len(errb))) != 0 {
		return nil, cErr(errb[:])
	}
	descriptor := dmabuf.Descriptor{Width: int(result.width), Height: int(result.height), FourCC: uint32(result.format), Modifier: uint64(result.modifier), Planes: []dmabuf.Plane{{FD: int(result.fd), Offset: uint32(result.offset), Stride: uint32(result.stride)}}}
	source, err := dmabuf.OwnSnapshot(descriptor, uint64(result.allocation))
	if err != nil {
		C.close(result.fd)
		return nil, fmt.Errorf("%w: %v", ErrOutOfMemory, err)
	}
	texture, err := render.NewExternalTexture(source)
	if err != nil {
		source.Close()
		return nil, err
	}
	return texture, nil
}
func (v *VK) uploadExternalTexture(texture *render.Texture) error {
	source, ok := texture.ExternalSource().(*dmabuf.Image)
	if !ok {
		return fmt.Errorf("unsupported external image backing")
	}
	err := source.WithDescriptor(func(d dmabuf.Descriptor) error {
		if v.frame.textures[texture.ID()] == texture.Revision() {
			return nil
		}
		if err := d.Validate(); err != nil {
			return err
		}
		if !v.supportsDMABufFormat(d.FourCC) {
			return dmabuf.ErrUnsupported
		}
		raw := rawDMABuf(d)
		var errb [errBuf]C.char
		if C.worldr_vk_upload_dmabuf(v.ptr, C.uint64_t(texture.ID()), &raw, &errb[0], C.int(len(errb))) != 0 {
			return cErr(errb[:])
		}
		return nil
	})
	runtime.KeepAlive(texture)
	if err == nil {
		v.frame.textures[texture.ID()] = texture.Revision()
	}
	return err
}

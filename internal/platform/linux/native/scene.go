//go:build linux && cgo

package native

/*
#include "vk_session.h"
*/
import "C"

import (
	"fmt"
	"math"
	"runtime"
	"unsafe"

	"github.com/codemodify/worldr/internal/render"
)

// SetSceneAtlas uploads coverage once; subsequent frames reuse the GPU image.
// The first texel should be white for solid geometry. Calls are single-threaded.
func (v *VK) SetSceneAtlas(atlas render.Atlas) error {
	if v == nil || v.ptr == nil {
		return fmt.Errorf("vulkan session closed")
	}
	if atlas.Width <= 0 || atlas.Height <= 0 || uint64(atlas.Width) > math.MaxUint32 || uint64(atlas.Height) > math.MaxUint32 || uint64(atlas.Width)*uint64(atlas.Height) != uint64(len(atlas.Pixels)) {
		return fmt.Errorf("invalid tightly packed scene atlas %dx%d (%d bytes)", atlas.Width, atlas.Height, len(atlas.Pixels))
	}
	var errb [errBuf]C.char
	ret := C.worldr_vk_scene_atlas(v.ptr, C.uint32_t(atlas.Width), C.uint32_t(atlas.Height), (*C.uint8_t)(unsafe.Pointer(&atlas.Pixels[0])), &errb[0], C.int(len(errb)))
	runtime.KeepAlive(atlas.Pixels)
	if ret != 0 {
		return cErr(errb[:])
	}
	v.atlas = render.Atlas{Width: atlas.Width, Height: atlas.Height, Pixels: append([]byte(nil), atlas.Pixels...)}
	return nil
}

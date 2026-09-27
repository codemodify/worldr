//go:build linux && cgo

package native

/*
#include "vk_frame.h"
*/
import "C"

import fluid "github.com/codemodify/worldr/sdk/fluid/v1"

func packFluid(field fluid.Field) C.worldr_fluid_field {
	var data C.worldr_fluid_field
	data.bounds = [4]C.float{C.float(field.Bounds.X), C.float(field.Bounds.Y), C.float(field.Bounds.Width), C.float(field.Bounds.Height)}
	data.style = [4]C.float{C.float(field.Style.Blend), C.float(field.Style.Rim), C.float(field.Style.Refraction), C.float(field.Style.Frost)}
	data.finish = [4]C.float{C.float(field.Style.Glow), C.float(field.Style.Opacity), C.float(field.Time), 0}
	data.pointer = [4]C.float{C.float(field.Pointer.X), C.float(field.Pointer.Y), C.float(len(field.Surfaces)), 0}
	if field.PointerActive {
		data.pointer[3] = 1
	}
	for i, background := range field.Style.Background {
		for j, value := range background {
			data.background[i][j] = C.float(value)
		}
	}
	for i, surface := range field.Surfaces {
		data.surfaces[i].bounds = [4]C.float{C.float(surface.Bounds.X), C.float(surface.Bounds.Y), C.float(surface.Bounds.Width), C.float(surface.Bounds.Height)}
		data.surfaces[i].shape[0] = C.float(surface.Radius)
		if surface.Fuse {
			data.surfaces[i].shape[1] = 1
		}
		for j, value := range surface.Tint {
			data.surfaces[i].tint[j] = C.float(value)
		}
	}
	return data
}

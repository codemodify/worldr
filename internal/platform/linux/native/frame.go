//go:build linux && cgo

package native

/*
#include "vk_frame.h"
*/
import "C"

import (
	"fmt"
	"math"
	"runtime"
	"unsafe"

	"github.com/codemodify/worldr/internal/render"
)

type frameState struct {
	draws    []C.worldr_frame_draw
	uploaded map[uint64]bool
	textures map[uint64]uint64
	ordered  []render.Draw
}

// SampleCount reports the active scene target sample count (4 when color/depth
// formats and sample-rate shading support it, otherwise 1). It is 0 before scene
// initialization or after session closure.
func (v *VK) SampleCount() int {
	if v == nil || v.ptr == nil {
		return 0
	}
	return int(C.worldr_vk_scene_samples(v.ptr))
}

func (v *VK) uploadTexture(texture *render.Texture) error {
	if texture.ExternalSource() != nil {
		return v.uploadExternalTexture(texture)
	}
	update, changed := texture.Snapshot(v.frame.textures[texture.ID()])
	if !changed {
		return nil
	}
	r := update.Rect
	if update.Width <= 0 || update.Height <= 0 || uint64(update.Width) > math.MaxUint32 || uint64(update.Height) > math.MaxUint32 || r.Empty() || r.Min.X < 0 || r.Min.Y < 0 || r.Max.X > update.Width || r.Max.Y > update.Height || uint64(r.Dx())*uint64(r.Dy())*4 != uint64(len(update.Pixels)) {
		return fmt.Errorf("invalid texture update")
	}
	var errb [errBuf]C.char
	result := C.worldr_vk_upload_texture(v.ptr, C.uint64_t(texture.ID()), C.uint32_t(update.Width), C.uint32_t(update.Height), C.uint32_t(r.Min.X), C.uint32_t(r.Min.Y), C.uint32_t(r.Dx()), C.uint32_t(r.Dy()), (*C.uint8_t)(unsafe.Pointer(&update.Pixels[0])), &errb[0], C.int(len(errb)))
	runtime.KeepAlive(update.Pixels)
	if result != 0 {
		return cErr(errb[:])
	}
	v.frame.textures[texture.ID()] = update.Revision
	return nil
}

// ReleaseTexture retires a sampled content resource after GPU work completes.
// A later reference uploads its current full image again.
func (v *VK) ReleaseTexture(id uint64) error {
	if v == nil || v.ptr == nil {
		return fmt.Errorf("vulkan session closed")
	}
	var errb [errBuf]C.char
	if C.worldr_vk_release_texture(v.ptr, C.uint64_t(id), &errb[0], C.int(len(errb))) != 0 {
		return cErr(errb[:])
	}
	if v.frame != nil {
		delete(v.frame.textures, id)
	}
	return nil
}

func (v *VK) uploadGeometry(geometry *render.Geometry) error {
	if v.frame.uploaded[geometry.ID()] {
		return nil
	}
	vertices, indices := geometry.Vertices(), geometry.Indices()
	if len(vertices) == 0 || len(indices) == 0 {
		return fmt.Errorf("empty geometry")
	}
	var errb [errBuf]C.char
	result := C.worldr_vk_upload_geometry(v.ptr, C.uint64_t(geometry.ID()), (*C.worldr_mesh_vertex)(unsafe.Pointer(&vertices[0])), C.uint32_t(len(vertices)), (*C.uint32_t)(unsafe.Pointer(&indices[0])), C.uint32_t(len(indices)), &errb[0], C.int(len(errb)))
	runtime.KeepAlive(vertices)
	runtime.KeepAlive(indices)
	if result != 0 {
		return cErr(errb[:])
	}
	v.frame.uploaded[geometry.ID()] = true
	return nil
}

// ReleaseGeometry retires an unused immutable resource after outstanding GPU
// work finishes. A later frame may reference it again and upload it anew.
func (v *VK) ReleaseGeometry(id uint64) error {
	if v == nil || v.ptr == nil {
		return fmt.Errorf("vulkan session closed")
	}
	var errb [errBuf]C.char
	if C.worldr_vk_release_geometry(v.ptr, C.uint64_t(id), &errb[0], C.int(len(errb))) != 0 {
		return cErr(errb[:])
	}
	if v.frame != nil {
		delete(v.frame.uploaded, id)
	}
	return nil
}

// RenderFrame consumes ordered overlay and camera commands. Immutable geometry
// is uploaded only on first use; textures upload only changed image regions.
// Subsequent frames transfer instance constants and dynamic overlay vertices.
// No Go pointer survives the C call.
func (v *VK) RenderFrame(frame render.Frame, clear [4]float32, dst []byte) error {
	if v == nil || v.ptr == nil {
		return fmt.Errorf("vulkan session closed")
	}
	if unsafe.Sizeof(render.Vertex{}) != C.sizeof_worldr_scene_vertex || unsafe.Sizeof(render.MeshVertex{}) != C.sizeof_worldr_mesh_vertex {
		return fmt.Errorf("render vertex ABI mismatch")
	}
	if uint64(len(frame.Vertices)) > math.MaxUint32 {
		return fmt.Errorf("too many overlay vertices")
	}
	for _, value := range clear {
		if !finite(value) {
			return fmt.Errorf("non-finite clear color")
		}
	}
	if err := validateOutputTransform(frame.Output, frame.LinearColor); err != nil {
		return err
	}
	if dst != nil {
		if v.IsDisplay() {
			return fmt.Errorf("swapchain readback unsupported; render an offscreen snapshot")
		}
		w, h := v.Size()
		if uint64(w)*uint64(h)*4 != uint64(len(dst)) {
			return fmt.Errorf("scene readback requires %d bytes", uint64(w)*uint64(h)*4)
		}
	}
	if v.frame == nil {
		v.frame = &frameState{uploaded: make(map[uint64]bool), textures: make(map[uint64]uint64)}
	}
	state := v.frame
	state.draws = state.draws[:0]
	defer func() { clearDraws(state.ordered); state.ordered = state.ordered[:0] }()
	for _, command := range frame.Commands {
		switch command.Kind {
		case render.OverlayCommand:
			if command.First < 0 || command.Count < 0 || command.First > len(frame.Vertices) || command.Count > len(frame.Vertices)-command.First || command.Count%3 != 0 {
				return fmt.Errorf("invalid overlay range")
			}
			state.draws = append(state.draws, C.worldr_frame_draw{kind: 0, first: C.uint32_t(command.First), count: C.uint32_t(command.Count)})
		case render.ImageCommand:
			image := command.Image
			r := image.Bounds
			if image.Texture == nil || image.Texture.ID() == 0 || !finiteValues(r[:]) || r[2] <= 0 || r[3] <= 0 || !finite(r[0]+r[2]) || !finite(r[1]+r[3]) {
				return fmt.Errorf("invalid image overlay")
			}
			if err := v.uploadTexture(image.Texture); err != nil {
				return err
			}
			width, height := v.Size()
			var imageDraw C.worldr_frame_draw
			imageDraw.kind, imageDraw.geometry = 5, C.uint64_t(image.Texture.ID())
			imageDraw.material[2], imageDraw.material[3] = 1, 1
			imageDraw.viewport[2], imageDraw.viewport[3] = C.float(width), C.float(height)
			imageDraw.projection[0], imageDraw.projection[5] = C.float(2/float32(width)), C.float(2/float32(height))
			imageDraw.projection[10], imageDraw.projection[15] = 1, 1
			imageDraw.projection[12], imageDraw.projection[13] = -1, -1
			imageDraw.model[0], imageDraw.model[5] = C.float(r[2]), C.float(-r[3])
			imageDraw.model[10], imageDraw.model[15] = 1, 1
			imageDraw.model[12], imageDraw.model[13] = C.float(r[0]+r[2]/2), C.float(r[1]+r[3]/2)
			state.draws = append(state.draws, imageDraw)
		case render.SceneCommand:
			view := command.View
			if !finiteValues(view.Projection[:]) || !finiteValues(view.Eye[:]) || !finiteValues(view.Light[:]) || !finiteValues(view.Viewport[:]) || !finite(view.EffectPhase) || view.EffectPhase < 0 || view.EffectPhase > 1 || view.Viewport[2] <= 0 || view.Viewport[3] <= 0 {
				return fmt.Errorf("invalid camera view")
			}
			if !finiteValues(view.Shadow.Projection[:]) || !finite(view.Shadow.Strength) || view.Shadow.Strength < 0 || view.Shadow.Strength > 1 || !finite(view.Shadow.Bias) || view.Shadow.Bias < 0 || view.Shadow.Bias > .05 {
				return fmt.Errorf("invalid directional shadow settings")
			}
			if view.Shadow.Strength > 0 && view.Shadow.Projection == ([16]float32{}) {
				return fmt.Errorf("active shadow requires a light projection")
			}
			if view.PointLightCount < 0 || view.PointLightCount > len(view.PointLights) {
				return fmt.Errorf("at most four point lights are supported per camera")
			}
			for _, light := range view.PointLights[:view.PointLightCount] {
				if !validPointLight(light) {
					return fmt.Errorf("point light needs finite position, sRGB color in [0,1], intensity in [0,8], and radius in (0,10000]")
				}
			}
			var begin C.worldr_frame_draw
			if view.TransparencyLayers < 0 || view.TransparencyLayers > 32 {
				return fmt.Errorf("transparency layer budget must be in [0,32]")
			}
			begin.kind = 1
			begin.count = C.uint32_t(view.TransparencyLayers)
			if begin.count == 0 {
				begin.count = 8
			}
			begin.shadow_params[0] = C.float(view.Shadow.Strength)
			for i, value := range view.Shadow.Projection {
				begin.shadow_projection[i] = C.float(value)
			}
			for i, value := range view.Viewport {
				begin.viewport[i] = C.float(value)
			}
			state.draws = append(state.draws, begin)
			// Preserve authored opaque/read-only ordering, then compose per-pixel
			// transparency. No CPU triangle or object-depth sort is needed.
			state.ordered = state.ordered[:0]
			for _, draw := range command.Draws {
				if !draw.Translucent {
					state.ordered = append(state.ordered, draw)
				}
			}
			for _, draw := range command.Draws {
				if draw.Translucent {
					state.ordered = append(state.ordered, draw)
				}
			}
			for _, draw := range state.ordered {
				if (draw.Geometry == nil) == (draw.Texture == nil) || !finiteValues(draw.Model[:]) || !finiteValues(draw.Color[:]) || !finiteValues(draw.WireColor[:]) || !finite(draw.WireWidth) {
					return fmt.Errorf("invalid scene instance")
				}
				if !validMaterial(draw.Material) {
					return fmt.Errorf("material channels must be finite and in [0,1]")
				}
				if draw.Material.Transmission > 0 && (!draw.Translucent || draw.Geometry == nil) {
					return fmt.Errorf("material transmission requires a translucent mesh")
				}
				if (draw.Material.Refraction > 0 || draw.Material.RefractionBlur > 0) && (!draw.Translucent || draw.Geometry == nil || draw.Material.Transmission <= 0 || draw.Unlit) {
					return fmt.Errorf("material refraction requires a lit transmitted translucent mesh")
				}
				if draw.Material.RefractionBlur > 0 && draw.Material.Refraction == 0 {
					return fmt.Errorf("material refraction blur requires refraction")
				}
				if draw.Material.Hologram > 0 && (!draw.Translucent || draw.Geometry == nil) {
					return fmt.Errorf("hologram material requires a translucent mesh")
				}
				hasGlow := false
				for _, channel := range draw.Glow {
					if !finite(channel) || channel < 0 || channel > 1 {
						return fmt.Errorf("glow channels must be finite and in [0,1]")
					}
					hasGlow = hasGlow || channel > 0
				}
				if hasGlow && draw.Texture != nil {
					return fmt.Errorf("opaque content surfaces cannot emit glow")
				}
				if draw.Translucent && draw.CastShadow {
					return fmt.Errorf("translucent shadow casting is not supported")
				}
				if draw.Translucent && draw.DepthReadOnly {
					return fmt.Errorf("translucent and depth-read-only flags are mutually exclusive")
				}
				if draw.Translucent && (draw.Color[3] < 0 || draw.Color[3] > 1) {
					return fmt.Errorf("translucent alpha must be in [0,1]")
				}
				if draw.DepthReadOnly && draw.Texture != nil {
					return fmt.Errorf("opaque content surfaces cannot use depth-read-only rendering")
				}
				var instance C.worldr_frame_draw
				if draw.CastShadow {
					instance.flags = 1
				}
				if draw.Translucent {
					instance.flags |= 2
				}
				for i, value := range view.Shadow.Projection {
					instance.shadow_projection[i] = C.float(value)
				}
				instance.shadow_params[0], instance.shadow_params[1] = C.float(view.Shadow.Strength), C.float(view.Shadow.Bias)
				if draw.ReceiveShadow {
					instance.shadow_params[2] = 1
				}
				if draw.Geometry != nil {
					if draw.Geometry.ID() == 0 {
						return fmt.Errorf("invalid mesh resource")
					}
					if err := v.uploadGeometry(draw.Geometry); err != nil {
						return err
					}
					instance.kind = 2
					if draw.DepthReadOnly || draw.Translucent {
						instance.kind = 4
					}
					instance.geometry = C.uint64_t(draw.Geometry.ID())
				} else {
					if draw.Texture.ID() == 0 {
						return fmt.Errorf("invalid texture resource")
					}
					if err := v.uploadTexture(draw.Texture); err != nil {
						return err
					}
					instance.kind = 3
					if draw.Translucent {
						instance.kind = 6
					}
					instance.geometry = C.uint64_t(draw.Texture.ID())
				}
				for i, value := range view.Viewport {
					instance.viewport[i] = C.float(value)
				}
				for i, value := range view.Projection {
					instance.projection[i] = C.float(value)
				}
				for i, value := range draw.Model {
					instance.model[i] = C.float(value)
				}
				for i, value := range view.Eye {
					instance.eye[i] = C.float(value)
				}
				for i, value := range view.Light {
					instance.light[i] = C.float(value)
				}
				instance.light[3] = C.float(view.PointLightCount)
				for lightIndex, light := range view.PointLights[:view.PointLightCount] {
					for i, value := range light.Position {
						instance.point_position[lightIndex][i] = C.float(value)
					}
					instance.point_position[lightIndex][3] = C.float(light.Radius)
					for i, value := range light.Color {
						instance.point_color[lightIndex][i] = C.float(value)
					}
					instance.point_color[lightIndex][3] = C.float(light.Intensity)
				}
				for i, value := range draw.Color {
					instance.color[i] = C.float(value)
				}
				for i, value := range draw.WireColor {
					instance.wire[i] = C.float(value)
				}
				instance.params[0] = C.float(draw.WireWidth)
				if draw.Unlit {
					instance.params[1] = 1
				}
				instance.params[2] = C.float(view.EffectPhase)
				instance.material[0] = C.float(draw.Material.Specular)
				instance.material[1] = C.float(draw.Material.Roughness)
				instance.material[2] = C.float(draw.Material.Metallic)
				instance.material[3] = C.float(draw.Material.RimStrength)
				instance.optical[0] = C.float(draw.Material.Transmission)
				instance.optical[1] = C.float(draw.Material.Refraction)
				instance.optical[2] = C.float(draw.Material.RefractionBlur)
				instance.optical[3] = C.float(draw.Material.Hologram)
				if draw.Texture != nil {
					uv := draw.UV
					if uv == ([4]float32{}) {
						uv = [4]float32{0, 0, 1, 1}
					}
					if !finiteValues(uv[:]) || uv[0] < 0 || uv[1] < 0 || uv[2] <= 0 || uv[3] <= 0 || uv[0]+uv[2] > 1 || uv[1]+uv[3] > 1 {
						return fmt.Errorf("invalid surface UV crop")
					}
					for i, value := range uv {
						instance.material[i] = C.float(value)
					}
				}
				for i, value := range draw.Material.RimColor {
					instance.rim[i] = C.float(value)
				}
				for i, value := range draw.Glow {
					instance.glow[i] = C.float(value)
				}
				state.draws = append(state.draws, instance)
			}
			clearDraws(state.ordered)
		default:
			return fmt.Errorf("unknown frame command %d", command.Kind)
		}
	}
	if uint64(len(state.draws)) > math.MaxUint32 {
		return fmt.Errorf("too many frame commands")
	}
	var vertices *C.worldr_scene_vertex
	if len(frame.Vertices) > 0 {
		vertices = (*C.worldr_scene_vertex)(unsafe.Pointer(&frame.Vertices[0]))
	}
	var draws *C.worldr_frame_draw
	if len(state.draws) > 0 {
		draws = &state.draws[0]
	}
	var output *C.uint8_t
	if len(dst) > 0 {
		output = (*C.uint8_t)(unsafe.Pointer(&dst[0]))
	}
	var errb [errBuf]C.char
	linear := C.int(0)
	if frame.LinearColor {
		linear = 1
	}
	transform := [8]C.float{
		C.float(frame.Output.Exposure), C.float(frame.Output.Saturation), C.float(frame.Output.Contrast), C.float(frame.Output.ToneMap),
		C.float(frame.Output.BloomStrength), C.float(frame.Output.BloomThreshold), C.float(frame.Output.BloomRadius), 0,
	}
	result := C.worldr_vk_render_frame(v.ptr, linear, &transform[0], vertices, C.uint32_t(len(frame.Vertices)), draws, C.uint32_t(len(state.draws)), (*C.float)(unsafe.Pointer(&clear[0])), output, &errb[0], C.int(len(errb)))
	runtime.KeepAlive(frame)
	runtime.KeepAlive(state.draws)
	runtime.KeepAlive(dst)
	if result == -2 {
		return ErrOutOfDate
	}
	if result == -3 {
		return ErrNotReady
	}
	if result != 0 {
		return cErr(errb[:])
	}
	return nil
}

func finite(value float32) bool { return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0) }

func validMaterial(material render.Material) bool {
	for _, value := range [...]float32{material.Specular, material.Roughness, material.Metallic, material.RimStrength, material.RimColor[0], material.RimColor[1], material.RimColor[2], material.Transmission, material.Refraction, material.RefractionBlur, material.Hologram} {
		if !finite(value) || value < 0 || value > 1 {
			return false
		}
	}
	return true
}

func validPointLight(light render.PointLight) bool {
	if !finiteValues(light.Position[:]) || !finiteValues(light.Color[:]) || !finite(light.Intensity) || !finite(light.Radius) || light.Intensity < 0 || light.Intensity > 8 || light.Radius <= 0 || light.Radius > 10000 {
		return false
	}
	for _, channel := range light.Color {
		if channel < 0 || channel > 1 {
			return false
		}
	}
	return true
}

func validateOutputTransform(output render.OutputTransform, linear bool) error {
	values := [...]float32{output.Exposure, output.Saturation, output.Contrast, output.BloomStrength, output.BloomThreshold, output.BloomRadius}
	if !finiteValues(values[:]) {
		return fmt.Errorf("output transform must be finite")
	}
	if output.Exposure < -4 || output.Exposure > 4 || output.Saturation < -1 || output.Saturation > 1 || output.Contrast < -1 || output.Contrast > 1 {
		return fmt.Errorf("output exposure must be in [-4,4] and saturation/contrast in [-1,1]")
	}
	if output.ToneMap > render.ToneMapFilmic {
		return fmt.Errorf("unknown output tone map %d", output.ToneMap)
	}
	if output.BloomStrength < 0 || output.BloomStrength > 2 || output.BloomThreshold < 0 || output.BloomThreshold > 8 || output.BloomRadius < 0 || output.BloomRadius > 32 {
		return fmt.Errorf("bloom strength must be in [0,2], threshold in [0,8], and radius in [0,32]")
	}
	if !linear && output != (render.OutputTransform{}) {
		return fmt.Errorf("output transform requires linear color")
	}
	return nil
}

func finiteValues(values []float32) bool {
	for _, value := range values {
		if !finite(value) {
			return false
		}
	}
	return true
}

func clearDraws(draws []render.Draw) { clear(draws) }

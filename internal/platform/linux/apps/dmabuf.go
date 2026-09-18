//go:build linux && cgo

package apps

/*
#include "apps.h"
*/
import "C"

import (
	"fmt"
	"path/filepath"
	"runtime/cgo"
	"strings"
	"unsafe"

	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
	"github.com/codemodify/worldr/internal/render"
	"golang.org/x/sys/unix"
)

// SetDMABufImporter enables the optional protocol before launching clients.
// Import borrows the descriptor synchronously and must return an owned immutable
// copy, safe after the client reuses its buffer. Only supplied explicit
// single-plane RGB format/modifier pairs are advertised. The host retires
// returned textures after their last frame.
func (s *Server) SetDMABufImporter(formats []dmabuf.Format, importer func(dmabuf.Descriptor) (*render.Texture, error)) error {
	return s.setDMABufImporter(formats, "", 0, 0, false, importer)
}

// SetDMABufImporterForDevice also records the renderer's DRM render node.
// Xwayland may request glamor only when this exact device is its default node;
// ordinary Wayland clients use the same format advertisement either way.
func (s *Server) SetDMABufImporterForDevice(formats []dmabuf.Format, renderNode string, importer func(dmabuf.Descriptor) (*render.Texture, error)) error {
	if renderNode != "" {
		base := filepath.Base(renderNode)
		digits := strings.TrimPrefix(base, "renderD")
		if !filepath.IsAbs(renderNode) || filepath.Dir(renderNode) != "/dev/dri" ||
			digits == "" || strings.Trim(digits, "0123456789") != "" {
			return fmt.Errorf("invalid DRM render node %q", renderNode)
		}
	}
	var renderMajor, renderMinor uint32
	if renderNode != "" {
		var stat unix.Stat_t
		if err := unix.Stat(renderNode, &stat); err != nil {
			return fmt.Errorf("DRM render node %q: %w", renderNode, err)
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFCHR {
			return fmt.Errorf("DRM render node %q is not a character device", renderNode)
		}
		renderMajor = unix.Major(uint64(stat.Rdev))
		renderMinor = unix.Minor(uint64(stat.Rdev))
		if !drmRenderDevice(renderMajor, renderMinor) {
			return fmt.Errorf("DRM render node %q has device %d:%d, want DRM render minor", renderNode, renderMajor, renderMinor)
		}
	}
	return s.setDMABufImporter(formats, renderNode, renderMajor, renderMinor, renderNode != "", importer)
}

func drmRenderDevice(major, minor uint32) bool {
	return major == 226 && minor >= 128 && minor < 192
}

func (s *Server) setDMABufImporter(formats []dmabuf.Format, renderNode string, renderMajor, renderMinor uint32, advertiseDevice bool, importer func(dmabuf.Descriptor) (*render.Texture, error)) error {
	if err := s.valid(); err != nil {
		return err
	}
	if len(formats) == 0 {
		return nil
	}
	if importer == nil || s.importHandle != 0 {
		return fmt.Errorf("DMA-BUF importer is absent or already configured")
	}
	if len(formats) > dmabuf.MaxFormatPairs {
		return fmt.Errorf("DMA-BUF format/modifier pair capacity exceeded")
	}
	var cHasRenderNode C.int
	if advertiseDevice {
		cHasRenderNode = 1
	}
	var raw []C.worldr_app_dmabuf_format
	for _, f := range formats {
		if f.Modifier == dmabuf.Invalid || !dmabuf.SupportedFourCC(f.FourCC) {
			return dmabuf.ErrUnsupported
		}
		found := false
		for _, v := range raw {
			if uint32(v.fourcc) == f.FourCC && uint64(v.modifier) == f.Modifier {
				found = true
			}
		}
		if !found {
			raw = append(raw, C.worldr_app_dmabuf_format{fourcc: C.uint32_t(f.FourCC), modifier: C.uint64_t(f.Modifier)})
		}
	}
	s.importer = importer
	s.dmabufFormats = make([]dmabuf.Format, len(raw))
	for i, f := range raw {
		s.dmabufFormats[i] = dmabuf.Format{FourCC: uint32(f.fourcc), Modifier: uint64(f.modifier)}
	}
	s.dmabufRenderNode = renderNode
	s.images = make(map[uint64]*render.Texture)
	s.importHandle = cgo.NewHandle(s)
	if C.worldr_apps_dmabuf(s.ptr, C.uintptr_t(s.importHandle), &raw[0], C.int(len(raw)), C.uint32_t(renderMajor), C.uint32_t(renderMinor), cHasRenderNode) != 0 {
		s.importHandle.Delete()
		s.importHandle = 0
		s.importer = nil
		s.dmabufFormats = nil
		s.dmabufRenderNode = ""
		return fmt.Errorf("DMA-BUF protocol registration failed")
	}
	return nil
}

// DMABufCapabilities returns a copy of the advertised pairs and the selected
// renderer node. An empty node deliberately disables device-specific clients.
func (s *Server) DMABufCapabilities() ([]dmabuf.Format, string) {
	if s == nil || s.ptr == nil || s.importHandle == 0 {
		return nil, ""
	}
	return append([]dmabuf.Format(nil), s.dmabufFormats...), s.dmabufRenderNode
}

//export goWorldrAppsImport
func goWorldrAppsImport(handle C.uintptr_t, token C.uint64_t, fd C.int, width C.int, height C.int, format C.uint32_t, modifier C.uint64_t, offset C.uint32_t, stride C.uint32_t) C.int {
	s := cgo.Handle(handle).Value().(*Server)
	d := dmabuf.Descriptor{Width: int(width), Height: int(height), FourCC: uint32(format), Modifier: uint64(modifier), Planes: []dmabuf.Plane{{FD: int(fd), Offset: uint32(offset), Stride: uint32(stride)}}}
	texture, err := s.importer(d)
	if err != nil || texture == nil {
		if texture != nil {
			texture.Close()
		}
		return 0
	}
	if w, h := texture.Size(); w != int(width) || h != int(height) {
		texture.Close()
		return 0
	}
	s.images[uint64(token)] = texture
	return 1
}
func (s *Server) surfaceLayers(id uint64) ([]Layer, error) {
	var raw [32]C.worldr_app_layer
	n := int(C.worldr_apps_layers(s.ptr, C.uint64_t(id), &raw[0], 32))
	if n < 0 {
		return nil, fmt.Errorf("application surface tree exceeds 32 visible layers")
	}
	layers := make([]Layer, 0, n)
	for i := 0; i < n; i++ {
		r := raw[i]
		token := uint64(r.token)
		texture := s.images[token]
		if texture == nil {
			if r.gpu != 0 {
				return nil, fmt.Errorf("missing retained DMA-BUF image")
			}
			pixels := C.GoBytes(unsafe.Pointer(r.pixels), C.int(r.width*r.height*4))
			var err error
			texture, err = render.NewTexture(int(r.width), int(r.height), pixels)
			if err != nil {
				return nil, err
			}
			s.images[token] = texture
		}
		layers = append(layers, Layer{Token: token, Texture: texture, Root: r.root != 0, Opaque: r.opaque != 0, X: float32(r.x), Y: float32(r.y), Width: float32(r.logical_width), Height: float32(r.logical_height), UV: [4]float32{float32(r.u0), float32(r.v0), float32(r.u1), float32(r.v1)}})
	}
	return layers, nil
}
func (s *Server) retireImages() {
	visible := make(map[uint64]bool)
	for _, surface := range s.cache {
		for _, layer := range surface.Layers {
			visible[layer.Token] = true
		}
	}
	for token, texture := range s.images {
		if !visible[token] && C.worldr_apps_image_alive(s.ptr, C.uint64_t(token)) == 0 {
			s.retired = append(s.retired, texture)
			delete(s.images, token)
		}
	}
}

// RetiredTextures transfers snapshots no longer owned by any client surface.
// Release native GPU resources before closing each exported backing texture.
func (s *Server) RetiredTextures() []*render.Texture {
	if s == nil {
		return nil
	}
	out := s.retired
	s.retired = nil
	return out
}

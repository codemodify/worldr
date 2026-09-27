package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/codemodify/worldr/internal/platform/linux/host"
	"github.com/codemodify/worldr/internal/platform/linux/native"
)

// Run owns a standalone Wayland/Vulkan window. It presents directly to the GPU
// swapchain and performs no framebuffer readback unless Snapshot is requested.
// Context cancellation and native close requests return normally.
func Run(ctx context.Context, options Options, application Application) (runErr error) {
	if application == nil {
		return errors.New("application is required")
	}
	defer func() { runErr = errors.Join(runErr, application.Close()) }()
	o, err := normalizeOptions(options)
	if err != nil {
		return err
	}
	if ctx == nil {
		return errors.New("context is required")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	width, height := o.Width, o.Height
	scale := float32(1)
	var window *host.Window
	if !o.Headless {
		window, err = host.OpenWithOptions(o.Title, width, height, host.Options{ClientDecorated: o.ClientDecorated, Transparent: o.Transparent, SystemCursor: true})
		if err != nil {
			return fmt.Errorf("open window: %w", err)
		}
		defer window.Close()
		width, height = window.Size()
		scale = float32(window.Scale())
	}
	h := newHost(window)
	defer close(h.done)
	if target, ok := application.(interface{ SetHost(*Host) }); ok {
		target.SetHost(h)
	}
	if target, ok := application.(interface{ SetScale(float32) }); ok {
		target.SetScale(scale)
	}
	var gpu *native.VK
	if window == nil {
		gpu, err = native.OpenVK(false, uint32(width), uint32(height))
	} else {
		display, surface := window.Handles()
		if o.Transparent {
			gpu, err = native.OpenVKWaylandTransparent(display, surface, uint32(width), uint32(height))
		} else {
			gpu, err = native.OpenVKWayland(display, surface, uint32(width), uint32(height))
		}
	}
	if err != nil {
		return fmt.Errorf("open GPU: %w", err)
	}
	defer gpu.Close()
	if err = gpu.SetSceneAtlas(application.Atlas()); err != nil {
		return fmt.Errorf("upload atlas: %w", err)
	}
	clear := [4]float32{0, 0, 0, 1}
	if o.Transparent {
		clear[3] = 0
	}
	resources := resourceSet{textures: map[uint64]struct{}{}, geometry: map[uint64]struct{}{}}
	// Draw establishes hit-test layout before the first input batch.
	application.Draw(width, height)
	interval := time.Second / time.Duration(o.FPS)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	start, previous := time.Now(), time.Now()
	frames := 0
	presentation := presentationState{}
	var events []host.Event
	for !h.closing {
		select {
		case <-ctx.Done():
			h.closing = true
			continue
		default:
		}
		now := time.Now()
		dt := now.Sub(previous)
		previous = now
		if dt > 100*time.Millisecond {
			dt = 100 * time.Millisecond
		}
		presentationReady := true
		if window != nil {
			events, err = window.Poll(events)
			if err != nil {
				return fmt.Errorf("poll window: %w", err)
			}
			newWidth, newHeight := window.Size()
			newScale := float32(window.Scale())
			if width != newWidth || height != newHeight || scale != newScale || presentation.resizePending {
				presentationReady, err = presentation.resize(gpu, newWidth, newHeight)
				if err != nil {
					return err
				}
				if presentationReady {
					width, height, scale = newWidth, newHeight, newScale
					if target, ok := application.(interface{ SetScale(float32) }); ok {
						target.SetScale(scale)
					}
					application.Handle(Event{Kind: PointerCancel})
					application.Draw(width, height)
					h.Wake()
				}
			}
			for _, event := range events {
				if event.Kind == host.Close {
					h.closing = true
					break
				}
				e := windowEvent(event)
				if e.Kind != 0 {
					application.Handle(e)
					h.Wake()
				}
			}
			h.drainClipboard()
		}
		if h.closing {
			break
		}
		if err = application.Update(dt); err != nil {
			return fmt.Errorf("update application: %w", err)
		}
		if window != nil {
			if input, ok := application.(interface{ TextInput() TextInputState }); ok {
				t := input.TextInput()
				state := host.TextInputState{Enabled: t.Enabled, ContextID: t.ContextID, Surrounding: t.Surrounding, Cursor: t.Cursor, Anchor: t.Anchor, CursorRect: t.CursorRect}
				if err = window.SetTextInput(state); err != nil {
					return fmt.Errorf("set text input: %w", err)
				}
			}
		}
		needsFrame := h.dirty.Swap(false) || o.Frames > 0 || presentation.redrawPending
		if demand, ok := application.(interface{ NeedsFrame() bool }); ok {
			needsFrame = demand.NeedsFrame() || needsFrame
		} else {
			needsFrame = true
		}
		if needsFrame && presentationReady {
			frame := application.Draw(width, height)
			presented, renderErr := presentation.present(gpu, frame, clear)
			if renderErr != nil {
				return renderErr
			}
			if presented {
				var retired []uint64
				owner, ownsTextures := application.(interface{ RetiredTextures() []uint64 })
				if ownsTextures {
					retired = owner.RetiredTextures()
				}
				if err = resources.retire(gpu, frame, ownsTextures, retired); err != nil {
					return err
				}
				frames++
			}
		}
		if o.Frames > 0 && frames >= o.Frames || o.Duration > 0 && time.Since(start) >= o.Duration {
			break
		}
		select {
		case <-ctx.Done():
			h.closing = true
		case <-ticker.C:
		}
	}
	if o.Snapshot != "" {
		capture := gpu
		if window != nil {
			capture, err = native.OpenVK(false, uint32(width), uint32(height))
			if err != nil {
				return fmt.Errorf("open snapshot GPU: %w", err)
			}
			defer capture.Close()
			if err = capture.SetSceneAtlas(application.Atlas()); err != nil {
				return err
			}
		}
		pixels := make([]byte, width*height*4)
		if err = capture.RenderFrame(application.Draw(width, height), clear, pixels); err != nil {
			return fmt.Errorf("render snapshot: %w", err)
		}
		if err = savePNG(o.Snapshot, pixels, width, height); err != nil {
			return fmt.Errorf("write snapshot: %w", err)
		}
	}
	return nil
}

// Transient presentation failures belong to the window lifecycle, not the app
// lifecycle. Keep processing input/PTY updates at the normal ticker cadence and
// retain both the redraw request and resource retirements until a frame succeeds.
type presentationState struct {
	resizePending bool
	redrawPending bool
}

func (s *presentationState) resize(gpu interface{ Resize(uint32, uint32) error }, width, height int) (bool, error) {
	s.resizePending, s.redrawPending = true, true
	if width <= 0 || height <= 0 {
		return false, nil
	}
	if err := gpu.Resize(uint32(width), uint32(height)); err != nil {
		if errors.Is(err, native.ErrOutOfDate) || errors.Is(err, native.ErrNotReady) {
			return false, nil
		}
		return false, fmt.Errorf("resize GPU: %w", err)
	}
	s.resizePending = false
	return true, nil
}

func (s *presentationState) present(gpu interface {
	RenderFrame(Frame, [4]float32, []byte) error
}, frame Frame, clear [4]float32) (bool, error) {
	if err := gpu.RenderFrame(frame, clear, nil); err != nil {
		if errors.Is(err, native.ErrOutOfDate) {
			s.resizePending, s.redrawPending = true, true
			return false, nil
		}
		if errors.Is(err, native.ErrNotReady) {
			s.redrawPending = true
			return false, nil
		}
		return false, fmt.Errorf("render frame: %w", err)
	}
	s.redrawPending = false
	return true, nil
}

func normalizeOptions(o Options) (Options, error) {
	if o.Title == "" {
		o.Title = "WorldR application"
	}
	if o.Width == 0 {
		o.Width = 1100
	}
	if o.Height == 0 {
		o.Height = 720
	}
	if o.FPS == 0 {
		o.FPS = 60
	}
	if o.Width < 1 || o.Height < 1 || o.Width > 8192 || o.Height > 8192 {
		return o, errors.New("window dimensions must be between 1 and 8192")
	}
	if o.FPS < 1 || o.FPS > 240 {
		return o, errors.New("FPS must be between 1 and 240")
	}
	if o.Frames < 0 || o.Duration < 0 {
		return o, errors.New("frame and duration limits cannot be negative")
	}
	if o.Headless && o.Frames == 0 && o.Duration == 0 {
		o.Frames = 1
	}
	return o, nil
}

func savePNG(path string, bgra []byte, width, height int) error {
	if width < 1 || height < 1 || len(bgra) != width*height*4 {
		return errors.New("invalid snapshot buffer")
	}
	// image.RGBA declares premultiplied channels. The PNG encoder unassociates
	// RGB correctly, preserving transparent edges without a dark fringe.
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < len(bgra); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = bgra[i+2], bgra[i+1], bgra[i], bgra[i+3]
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	return errors.Join(png.Encode(file, img), file.Close())
}

type resourceSet struct{ textures, geometry map[uint64]struct{} }

// A resource absent from the newly submitted frame can be retired only after
// submission. The renderer waits for its in-flight uses before freeing it.
func (r *resourceSet) retire(gpu *native.VK, frame Frame, ownsTextures bool, retired []uint64) error {
	next := resourceSet{textures: make(map[uint64]struct{}, len(r.textures)), geometry: make(map[uint64]struct{}, len(r.geometry))}
	for _, command := range frame.Commands {
		if id := command.Image.Texture.ID(); id != 0 {
			next.textures[id] = struct{}{}
		}
		for _, draw := range command.Draws {
			if id := draw.Texture.ID(); id != 0 {
				next.textures[id] = struct{}{}
			}
			if id := draw.Geometry.ID(); id != 0 {
				next.geometry[id] = struct{}{}
			}
		}
	}
	if ownsTextures {
		for _, id := range retired {
			if _, used := next.textures[id]; used {
				return fmt.Errorf("cannot retire texture %d referenced by the submitted frame", id)
			}
			if err := gpu.ReleaseTexture(id); err != nil {
				return fmt.Errorf("retire texture: %w", err)
			}
			delete(r.textures, id)
		}
		for id := range r.textures {
			next.textures[id] = struct{}{}
		}
	} else {
		for id := range r.textures {
			if _, used := next.textures[id]; !used {
				if err := gpu.ReleaseTexture(id); err != nil {
					return fmt.Errorf("retire texture: %w", err)
				}
			}
		}
	}
	for id := range r.geometry {
		if _, used := next.geometry[id]; !used {
			if err := gpu.ReleaseGeometry(id); err != nil {
				return fmt.Errorf("retire geometry: %w", err)
			}
		}
	}
	*r = next
	return nil
}

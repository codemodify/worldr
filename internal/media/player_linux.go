//go:build linux && cgo

package media

/*
#cgo pkg-config: mpv
#include "player.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"unsafe"
)

// Player retains one local media file. Create another Player to open a different
// file; Seek(0) replays the current file. Close never closes the caller's file.
type Player struct {
	mu                    sync.Mutex
	ptr                   *C.worldr_media
	file                  *os.File
	state                 State
	width, height, stride int
	seeking               bool
}

func New(options Options) (*Player, error) {
	if strings.ContainsRune(options.AudioOutput, '\x00') || len(options.AudioOutput) > 128 {
		return nil, fmt.Errorf("invalid media audio output")
	}
	initial := InitialState{Volume: 70}
	if options.InitialState != nil {
		initial = *options.InitialState
	}
	if initial.Volume < 0 || initial.Volume > 100 || math.IsNaN(initial.Volume) || math.IsInf(initial.Volume, 0) {
		return nil, fmt.Errorf("initial media volume must be 0..100")
	}
	audio := C.CString(options.AudioOutput)
	defer C.free(unsafe.Pointer(audio))
	var code C.int
	var stage *C.char
	ptr := C.worldr_media_new(audio, cflag(initial.Paused), C.double(initial.Volume), cflag(initial.Muted), &code, &stage)
	if ptr == nil {
		return nil, fmt.Errorf("media %s: %s", C.GoString(stage), C.GoString(C.mpv_error_string(code)))
	}
	return &Player{ptr: ptr, state: State{Paused: initial.Paused, Volume: initial.Volume, Muted: initial.Muted}}, nil
}

// Load queues playback without waiting for decoding. The caller must keep file
// open until Close returns. Only a regular local file is accepted, and the
// already-open descriptor is used rather than reopening its original pathname.
func (p *Player) Load(file *os.File) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ptr == nil {
		return ErrClosed
	}
	if p.file != nil {
		return fmt.Errorf("media player already has a file")
	}
	if file == nil {
		return fmt.Errorf("media requires an open regular file")
	}
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("media file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("media requires a regular file")
	}
	path := C.CString("/proc/self/fd/" + strconv.FormatUint(uint64(file.Fd()), 10))
	defer C.free(unsafe.Pointer(path))
	if err := mediaError("load", C.worldr_media_load(p.ptr, path)); err != nil {
		return err
	}
	p.file = file
	return nil
}

// Poll drains at most 128 asynchronous events so a decoder cannot monopolize
// the workspace loop. Playback failures are retained in the returned State.
func (p *Player) Poll() (State, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ptr == nil {
		return p.state, ErrClosed
	}
	for i := 0; i < 128; i++ {
		event := C.worldr_media_poll(p.ptr)
		if event.id == C.MPV_EVENT_NONE {
			break
		}
		if event.error < 0 {
			p.state.Error = C.GoString(C.mpv_error_string(event.error))
		}
		switch event.id {
		case C.MPV_EVENT_SEEK:
			p.seeking = true
		case C.MPV_EVENT_PLAYBACK_RESTART:
			if p.seeking {
				p.state.SeekRevision++
				p.seeking = false
			}
		case C.MPV_EVENT_FILE_LOADED:
			p.state.Loaded = true
		case C.MPV_EVENT_END_FILE:
			if event.end_reason == C.MPV_END_FILE_REASON_EOF {
				p.state.Ended = true
			} else if event.end_reason == C.MPV_END_FILE_REASON_ERROR || event.end_reason == C.MPV_END_FILE_REASON_REDIRECT {
				p.state.Loaded = false
				if p.state.Error == "" {
					p.state.Error = "unsupported local video"
				}
			}
		case C.MPV_EVENT_QUEUE_OVERFLOW:
			p.state.Error = "media event queue overflow"
		case C.MPV_EVENT_SHUTDOWN:
			p.state.Error = "media player stopped"
		case C.MPV_EVENT_PROPERTY_CHANGE:
			value := float64(event.value)
			if event.present == 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			switch event.property {
			case 1:
				p.state.Paused = value != 0
			case 2:
				p.state.Position = math.Max(0, value)
			case 3:
				p.state.Duration = math.Max(0, value)
			case 4:
				p.state.Volume = math.Max(0, math.Min(100, value))
			case 5:
				p.state.Muted = value != 0
			case 6:
				p.state.Width = int(value)
			case 7:
				p.state.Height = int(value)
			case 8:
				p.state.Ended = value != 0
			}
		}
	}
	p.state.HasVideo = p.state.Width > 0 && p.state.Height > 0
	return p.state, nil
}

// Render updates pixels only for a new frame or changed dimensions/stride.
// Reuse the same buffer between calls. It must be writable through stride*height,
// four-byte aligned, no larger than 32 MiB, and at most 4096 pixels per dimension.
// libmpv preserves aspect ratio and fills letterboxing; no pointer is retained.
func (p *Player) Render(pixels []byte, width, height, stride int) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ptr == nil {
		return false, ErrClosed
	}
	if width < 1 || height < 1 || width > 4096 || height > 4096 || stride < width*4 || stride%4 != 0 || stride > (32<<20)/height || len(pixels) < stride*height || uintptr(unsafe.Pointer(&pixels[0]))%4 != 0 {
		return false, fmt.Errorf("invalid media RGBA surface")
	}
	force := p.width != width || p.height != height || p.stride != stride
	result := C.worldr_media_render(p.ptr, (*C.uint8_t)(unsafe.Pointer(&pixels[0])), C.int(width), C.int(height), C.size_t(stride), cflag(force))
	runtime.KeepAlive(pixels)
	if err := mediaError("render", result); err != nil {
		return false, err
	}
	p.width, p.height, p.stride = width, height, stride
	return result > 0, nil
}

func (p *Player) Pause(paused bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ptr == nil {
		return ErrClosed
	}
	return mediaError("pause", C.worldr_media_pause(p.ptr, cflag(paused)))
}

func (p *Player) Seek(seconds float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ptr == nil {
		return ErrClosed
	}
	if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return fmt.Errorf("invalid media seek time")
	}
	value := C.CString(strconv.FormatFloat(seconds, 'f', 6, 64))
	defer C.free(unsafe.Pointer(value))
	return mediaError("seek", C.worldr_media_seek(p.ptr, value))
}

func (p *Player) SetVolume(volume float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ptr == nil {
		return ErrClosed
	}
	if volume < 0 || volume > 100 || math.IsNaN(volume) || math.IsInf(volume, 0) {
		return fmt.Errorf("media volume must be 0..100")
	}
	return mediaError("volume", C.worldr_media_volume(p.ptr, C.double(volume)))
}

func (p *Player) SetMute(muted bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ptr == nil {
		return ErrClosed
	}
	return mediaError("mute", C.worldr_media_mute(p.ptr, cflag(muted)))
}

func (p *Player) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ptr == nil {
		return nil
	}
	C.worldr_media_free(p.ptr)
	p.ptr = nil
	runtime.KeepAlive(p.file)
	p.file = nil
	return nil
}

func cflag(value bool) C.int {
	if value {
		return 1
	}
	return 0
}
func mediaError(operation string, code C.int) error {
	if code >= 0 {
		return nil
	}
	return fmt.Errorf("media %s: %s", operation, C.GoString(C.mpv_error_string(code)))
}

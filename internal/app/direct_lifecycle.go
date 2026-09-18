package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/codemodify/worldr/internal/input"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/platform/linux/seat"
	"github.com/codemodify/worldr/internal/render"
)

// Direct display ownership is leased from the active login seat. The retained
// workspace and its application processes outlive each display activation.
type directPresentation struct {
	session                  *seat.Session
	device                   *seat.Device
	sidecars                 []*native.DRM
	options                  Options
	atlas                    render.Atlas
	backend, card, signature string
	nextProbe                time.Time
	failure                  string
	notice                   string
	paused                   bool
}

func (p *presenter) startDirect(o Options, backend string, atlas render.Atlas) error {
	session, err := seat.Open()
	if err != nil {
		return err
	}
	p.direct = &directPresentation{session: session, options: o, atlas: atlas, backend: backend}
	deadline := time.Now().Add(8 * time.Second)
	for !session.Active() {
		events, err := session.Dispatch()
		if err != nil {
			return err
		}
		for _, event := range events {
			if event == seat.Disabled {
				if err := session.AcknowledgeDisable(); err != nil {
					return err
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("direct seat did not become active within 8 seconds")
		}
		if !session.Active() {
			time.Sleep(5 * time.Millisecond)
		}
	}
	return p.openDirect()
}
func (p *presenter) openDirect() error {
	d := p.direct
	cards := []string{d.card}
	if d.card == "" {
		cards = []string{d.options.Card}
		if d.options.Card == "" {
			cards, _ = filepath.Glob("/dev/dri/card[0-9]*")
		}
	}
	var failures error
	for _, card := range cards {
		device, err := d.session.OpenDevice(card)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		d.device = device
		available, err := native.DRMOutputsFromFD(device.FD())
		var planned []directOutput
		if err == nil {
			planned, err = layoutDirectOutputs(available, d.options.Outputs, d.backend == "drm")
		}
		if err == nil && len(planned) == 0 {
			err = fmt.Errorf("%s has no selected connected display", card)
		}
		var outputs []native.Output
		for _, plan := range planned {
			if err != nil {
				break
			}
			var drm *native.DRM
			if d.backend == "drm" {
				drm, err = native.OpenDRMFromFD(device.FD(), plan.connector.ID)
			} else {
				drm, err = native.OpenDRMPlanesFromFD(device.FD(), plan.connector.ID)
			}
			if err != nil {
				break
			}
			d.sidecars = append(d.sidecars, drm)
			var vk *native.VK
			if d.backend == "drm" {
				vk, err = native.OpenVK(false, uint32(plan.bounds.Dx()), uint32(plan.bounds.Dy()))
			} else {
				vk, err = native.OpenVKOnDRM(drm)
			}
			if err != nil {
				break
			}
			outputs = append(outputs, native.Output{ID: plan.connector.ID, Bounds: plan.bounds, VK: vk})
		}
		if err == nil {
			p.outputs, err = native.NewOutputSet(outputs, d.options.GPUMemoryMiB<<20)
			if err == nil {
				p.vk = p.outputs.Primary()
				p.w, p.h = p.outputs.Size()
				err = p.outputs.SetSceneAtlas(d.atlas)
			}
		}
		if err != nil {
			if p.outputs == nil {
				for _, output := range outputs {
					output.VK.Close()
				}
			}
			_ = p.closeDirect()
			failures = errors.Join(failures, fmt.Errorf("%s: %w", card, err))
			continue
		}
		if d.backend == "drm" {
			p.drm = d.sidecars[0]
			p.pixels = make([]byte, p.w*p.h*4)
		}
		p.pointer, err = input.OpenManaged(d.session, p.w, p.h)
		if err != nil {
			_ = p.closeDirect()
			failures = errors.Join(failures, err)
			continue
		}
		d.card, d.signature, d.failure = card, directOutputSignature(planned), ""
		if d.paused {
			d.notice = "Direct session resumed; graphics and input were restored."
			d.paused = false
		}
		d.nextProbe = time.Now().Add(time.Second)
		p.samples = p.vk.SampleCount()
		return nil
	}
	if failures == nil {
		failures = fmt.Errorf("no accessible DRM primary node")
	}
	return failures
}
func (p *presenter) closeDirect() error {
	if p.pointer != nil {
		p.pointer.Close()
		p.pointer = nil
	}
	if p.outputs != nil {
		p.outputs.Close()
		p.outputs = nil
	} else if p.vk != nil {
		p.vk.Close()
	}
	p.vk, p.drm = nil, nil
	p.pixels = nil
	if p.direct == nil {
		return nil
	}
	for _, drm := range p.direct.sidecars {
		drm.Close()
	}
	p.direct.sidecars = nil
	var err error
	if p.direct.device != nil {
		err = p.direct.device.Close()
		p.direct.device = nil
	}
	return err
}

// Disable ACK is never sent before graphics/input leases are released. Keeping
// this ordering explicit lets tests exercise failure paths without a real VT.
func applySeatTransitions(events []seat.Event, release func() error, ack func() error, enable func() error) (bool, error) {
	changed := false
	for _, event := range events {
		switch event {
		case seat.Disabled:
			changed = true
			if err := release(); err != nil {
				return changed, err
			}
			if err := ack(); err != nil {
				return changed, err
			}
		case seat.Enabled:
			changed = true
			if err := enable(); err != nil {
				return changed, err
			}
		}
	}
	return changed, nil
}
func (p *presenter) pollSeat(now time.Time) (bool, error) {
	d := p.direct
	if d == nil {
		return false, nil
	}
	events, err := d.session.Dispatch()
	if err != nil {
		return false, err
	}
	changed, err := applySeatTransitions(events, func() error {
		d.paused = true
		d.notice = "Direct session paused; graphics and input were released."
		return p.closeDirect()
	}, d.session.AcknowledgeDisable, func() error {
		if p.vk != nil {
			return nil
		}
		d.nextProbe = time.Time{}
		return nil
	})
	if err != nil {
		return changed, err
	}
	if !d.session.Active() || now.Before(d.nextProbe) {
		return changed, nil
	}
	d.nextProbe = now.Add(time.Second)
	if p.vk != nil {
		available, err := native.DRMOutputsFromFD(d.device.FD())
		var planned []directOutput
		if err == nil {
			planned, err = layoutDirectOutputs(available, d.options.Outputs, d.backend == "drm")
		}
		if err == nil && directOutputSignature(planned) == d.signature {
			return changed, nil
		}
		if err = p.closeDirect(); err != nil {
			return true, err
		}
		changed = true
		d.notice = "Display topology changed; rebuilding the extended desktop."
	}
	if err := p.openDirect(); err != nil {
		failure := err.Error()
		if failure != d.failure {
			d.notice = "Direct display unavailable; retrying: " + failure
		}
		d.failure = failure
		return changed, nil
	}
	return true, nil
}

func (p *presenter) takeDirectNotice() string {
	if p == nil || p.direct == nil {
		return ""
	}
	notice := p.direct.notice
	p.direct.notice = ""
	return notice
}
func (p *presenter) invalidateDirect(cause error) error {
	if p.direct == nil {
		return cause
	}
	p.direct.failure = cause.Error()
	p.direct.nextProbe = time.Now().Add(time.Second)
	return p.closeDirect()
}
func directVT(e experience.Event) int {
	if e.Kind != experience.KeyInput || !e.Pressed || e.Repeat || e.Modifiers != experience.ModControl|experience.ModAlt {
		return 0
	}
	if e.Keycode >= 59 && e.Keycode <= 68 {
		return int(e.Keycode - 58)
	}
	if e.Keycode == 87 {
		return 11
	}
	if e.Keycode == 88 {
		return 12
	}
	return 0
}
func (p *presenter) renderFrame(frame render.Frame, clear [4]float32) error {
	if p.outputs != nil && p.direct.backend != "drm" {
		return p.outputs.RenderFrame(frame, clear)
	}
	if p.vk == nil {
		return native.ErrNotReady
	}
	return p.vk.RenderFrame(frame, clear, p.pixels)
}
func (p *presenter) releaseTexture(id uint64) error {
	if p.outputs != nil {
		return p.outputs.ReleaseTexture(id)
	}
	if p.vk != nil {
		return p.vk.ReleaseTexture(id)
	}
	return nil
}
func (p *presenter) releaseGeometry(id uint64) error {
	if p.outputs != nil {
		return p.outputs.ReleaseGeometry(id)
	}
	if p.vk != nil {
		return p.vk.ReleaseGeometry(id)
	}
	return nil
}
func (p *presenter) MemoryStats() native.MemoryStats {
	if p.outputs != nil {
		return p.outputs.MemoryStats()
	}
	if p.vk != nil {
		return p.vk.MemoryStats()
	}
	return native.MemoryStats{}
}

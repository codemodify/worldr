//go:build linux && cgo

package native

/*
#include "drm_session.h"
*/
import "C"
import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// OpenDRMFromFD duplicates a managed card FD and modesets a diagnostic buffer.
// The caller's lease must outlive DRM.Close. No master ownership is changed.
func OpenDRMFromFD(fd int, connector ...uint32) (*DRM, error) {
	if len(connector) > 1 {
		return nil, fmt.Errorf("at most one DRM connector")
	}
	var id uint32
	if len(connector) > 0 {
		id = connector[0]
	}
	return OpenDRMFromFDConnector(fd, id)
}
func OpenDRMFromFDConnector(fd int, connector uint32) (*DRM, error) {
	return openManagedDRM(fd, connector, false)
}

// OpenDRMPlanesFromFD selects a connector without modesetting a primary plane.
func OpenDRMPlanesFromFD(fd int, connector ...uint32) (*DRM, error) {
	if len(connector) > 1 {
		return nil, fmt.Errorf("at most one DRM connector")
	}
	var id uint32
	if len(connector) > 0 {
		id = connector[0]
	}
	return OpenDRMPlanesFromFDConnector(fd, id)
}
func OpenDRMPlanesFromFDConnector(fd int, connector uint32) (*DRM, error) {
	return openManagedDRM(fd, connector, true)
}
func openManagedDRM(fd int, connector uint32, planes bool) (*DRM, error) {
	if fd < 0 || int64(fd) > 1<<31-1 {
		return nil, fmt.Errorf("invalid managed DRM descriptor")
	}
	var ptr *C.worldr_drm
	var message [errBuf]C.char
	var only C.int
	if planes {
		only = 1
	}
	if C.worldr_drm_create_fd(C.int(fd), C.uint32_t(connector), only, &ptr, &message[0], errBuf) != 0 {
		return nil, cErr(message[:])
	}
	return &DRM{ptr: ptr, planesOnly: planes}, nil
}
func (d *DRM) ConnectorID() uint32 {
	if d == nil || d.ptr == nil {
		return 0
	}
	return uint32(C.worldr_drm_connector_id(d.ptr))
}

func DRMOutputsFromFD(fd int) ([]DRMOutput, error) {
	if fd < 0 || int64(fd) > 1<<31-1 {
		return nil, fmt.Errorf("invalid DRM inventory descriptor")
	}
	var raw [64]C.worldr_drm_output
	var message [errBuf]C.char
	n := int(C.worldr_drm_outputs(C.int(fd), &raw[0], 64, &message[0], errBuf))
	if n < 0 {
		return nil, cErr(message[:])
	}
	card, _ := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	out := make([]DRMOutput, n)
	for i := range out {
		r := raw[i]
		out[i] = DRMOutput{ID: uint32(r.id), Card: card, Name: C.GoString(&r.name[0]), Connected: r.connected != 0, Width: uint32(r.width), Height: uint32(r.height), RefreshMilliHz: uint32(r.refresh_millihz)}
	}
	return out, nil
}

// ListDRMOutputs opens cards read-only. It does not open a seat, acquire DRM
// master, create a Vulkan display, or alter the active graphical session.
// An empty card path lists every accessible /dev/dri/cardN.
func ListDRMOutputs(card string) ([]DRMOutput, error) {
	paths := []string{card}
	if card == "" {
		var err error
		paths, err = filepath.Glob("/dev/dri/card[0-9]*")
		if err != nil {
			return nil, err
		}
	}
	var out []DRMOutput
	var failures error
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		outputs, err := DRMOutputsFromFD(int(file.Fd()))
		file.Close()
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		out = append(out, outputs...)
	}
	if len(out) == 0 && failures != nil {
		return nil, failures
	}
	return out, nil
}

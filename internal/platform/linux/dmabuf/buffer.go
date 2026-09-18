// Package dmabuf describes bounded, single-plane Linux GPU images. Import
// descriptors borrow handles; Image owns an immutable LINEAR GPU snapshot.
package dmabuf

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
)

const (
	XRGB8888       uint32 = 0x34325258
	ARGB8888       uint32 = 0x34325241
	XBGR8888       uint32 = 0x34324258
	ABGR8888       uint32 = 0x34324241
	XRGB2101010    uint32 = 0x30335258
	ARGB2101010    uint32 = 0x30335241
	XBGR2101010    uint32 = 0x30334258
	ABGR2101010    uint32 = 0x30334241
	Linear         uint64 = 0
	Invalid        uint64 = ^uint64(0)
	MaxFormatPairs        = 256
	SnapshotBudget uint64 = 256 << 20
)

var ErrUnsupported = errors.New("DMA-BUF format or sharing capability unsupported")

type Format struct {
	FourCC   uint32
	Modifier uint64
}
type Plane struct {
	FD             int
	Offset, Stride uint32
}
type Descriptor struct {
	Width, Height int
	FourCC        uint32
	Modifier      uint64
	Planes        []Plane
}

func (d Descriptor) validate(allowModifier bool) error {
	if d.Width <= 0 || d.Height <= 0 || d.Width > 4096 || d.Height > 4096 {
		return fmt.Errorf("DMA-BUF dimensions must be in [1,4096]")
	}
	if !SupportedFourCC(d.FourCC) || len(d.Planes) != 1 || d.Modifier == Invalid || (!allowModifier && d.Modifier != Linear) {
		return ErrUnsupported
	}
	p := d.Planes[0]
	if p.FD < 0 || uint64(p.Stride) < uint64(d.Width)*4 || p.Stride > 1<<20 || p.Offset%4 != 0 || p.Stride%4 != 0 {
		return fmt.Errorf("invalid DMA-BUF plane handle, offset or stride")
	}
	if uint64(p.Offset)+uint64(p.Stride)*uint64(d.Height-1)+uint64(d.Width)*4 > SnapshotBudget {
		return fmt.Errorf("DMA-BUF plane exceeds 256 MiB")
	}
	return nil
}

// Validate checks an owned snapshot descriptor. Snapshots intentionally stay
// LINEAR so another rendering device can import them after recovery.
func (d Descriptor) Validate() error { return d.validate(false) }

// ValidateImport checks the format-independent bounds of a client import.
// The rendering device must separately accept the exact FourCC/modifier pair.
func (d Descriptor) ValidateImport() error { return d.validate(true) }

// SupportedFourCC reports the bounded 32-bit RGB formats understood by the
// retained snapshot path. A rendering device still has to confirm import and
// export support before the compositor advertises a particular format.
func SupportedFourCC(format uint32) bool {
	switch format {
	case XRGB8888, ARGB8888, XBGR8888, ABGR8888,
		XRGB2101010, ARGB2101010, XBGR2101010, ABGR2101010:
		return true
	default:
		return false
	}
}

var snapshotBytes atomic.Uint64

// RetainedBytes reports GPU memory held by exported immutable snapshot FDs,
// independently from each rendering device's live import/allocation accounting.
func RetainedBytes() uint64 { return snapshotBytes.Load() }

type Image struct {
	mu         sync.Mutex
	descriptor Descriptor
	file       *os.File
	allocation uint64
}

// OwnSnapshot takes the exported descriptor FD only on success. allocation is
// the actual VkMemoryRequirements size, not an estimated pixel byte count.
func OwnSnapshot(d Descriptor, allocation uint64) (*Image, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if allocation == 0 || allocation > SnapshotBudget {
		return nil, fmt.Errorf("DMA-BUF snapshot allocation exceeds budget")
	}
	p := d.Planes[0]
	if uint64(p.Offset)+uint64(p.Stride)*uint64(d.Height-1)+uint64(d.Width)*4 > allocation {
		return nil, fmt.Errorf("DMA-BUF snapshot plane exceeds allocation")
	}
	for {
		used := snapshotBytes.Load()
		if used > SnapshotBudget-allocation {
			return nil, fmt.Errorf("DMA-BUF retained snapshot budget exceeded")
		}
		if snapshotBytes.CompareAndSwap(used, used+allocation) {
			break
		}
	}
	d.Planes = append([]Plane(nil), d.Planes...)
	i := &Image{descriptor: d, file: os.NewFile(uintptr(d.Planes[0].FD), "worldr-gpu-snapshot"), allocation: allocation}
	runtime.SetFinalizer(i, func(i *Image) { _ = i.Close() })
	return i, nil
}
func (i *Image) Size() (int, int) {
	if i == nil {
		return 0, 0
	}
	return i.descriptor.Width, i.descriptor.Height
}

// WithDescriptor lends the owned FD only for the synchronous callback.
func (i *Image) WithDescriptor(fn func(Descriptor) error) error {
	if i == nil {
		return fmt.Errorf("nil DMA-BUF snapshot")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.file == nil {
		return os.ErrClosed
	}
	d := i.descriptor
	d.Planes = append([]Plane(nil), d.Planes...)
	return fn(d)
}
func (i *Image) Close() error {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.file == nil {
		return nil
	}
	err := i.file.Close()
	i.file = nil
	snapshotBytes.Add(^(i.allocation - 1))
	i.allocation = 0
	runtime.SetFinalizer(i, nil)
	return err
}

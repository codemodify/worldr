//go:build linux

package dmabuf

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"sync"
	"testing"
)

func TestDescriptorRejectsUnadvertisedAndOverflowingLayouts(t *testing.T) {
	valid := Descriptor{Width: 16, Height: 8, FourCC: ARGB8888, Planes: []Plane{{FD: 3, Stride: 64}}}
	cases := []Descriptor{valid, valid, valid, valid, valid, valid, valid}
	cases[0].Modifier = 1
	cases[1].FourCC = 0
	cases[2].Width = 4097
	cases[3].Planes = nil
	cases[4].Planes = []Plane{{FD: -1, Stride: 64}}
	cases[5].Planes = []Plane{{FD: 3, Stride: 60}}
	cases[6].Planes = []Plane{{FD: 3, Stride: 64, Offset: ^uint32(3)}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, format := range []uint32{
		XRGB8888, ARGB8888, XBGR8888, ABGR8888,
		XRGB2101010, ARGB2101010, XBGR2101010, ABGR2101010,
	} {
		candidate := valid
		candidate.FourCC = format
		if err := candidate.Validate(); err != nil {
			t.Fatalf("supported fourcc %#x rejected: %v", format, err)
		}
	}
	for i, d := range cases {
		if err := d.Validate(); err == nil {
			t.Fatalf("accepted invalid layout %d", i)
		}
	}
}

func TestSupportedFourCCIsClosedSet(t *testing.T) {
	for _, format := range []uint32{
		XRGB8888, ARGB8888, XBGR8888, ABGR8888,
		XRGB2101010, ARGB2101010, XBGR2101010, ABGR2101010,
	} {
		if !SupportedFourCC(format) {
			t.Fatalf("missing supported fourcc %#x", format)
		}
	}
	for _, format := range []uint32{0, 0x34325259, 0x36314752} {
		if SupportedFourCC(format) {
			t.Fatalf("unexpected supported fourcc %#x", format)
		}
	}
}

func TestImportValidationDefersExactModifierCapability(t *testing.T) {
	d := Descriptor{Width: 16, Height: 8, FourCC: ARGB8888, Modifier: 0x0100000000000002, Planes: []Plane{{FD: 3, Stride: 64}}}
	if err := d.ValidateImport(); err != nil {
		t.Fatal("structurally valid explicit modifier rejected", err)
	}
	if !errors.Is(d.Validate(), ErrUnsupported) {
		t.Fatal("non-LINEAR descriptor accepted as retained snapshot")
	}
	d.Modifier = Invalid
	if !errors.Is(d.ValidateImport(), ErrUnsupported) {
		t.Fatal("implicit/invalid modifier accepted for explicit import")
	}
}

func TestSnapshotBudgetOwnershipAndConcurrentClose(t *testing.T) {
	before := RetainedBytes()
	f, err := os.CreateTemp(t.TempDir(), "exported-handle")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fd, err := unix.FcntlInt(f.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	d := Descriptor{Width: 1, Height: 1, FourCC: ARGB8888, Planes: []Plane{{FD: fd, Stride: 4}}}
	if _, err := OwnSnapshot(d, SnapshotBudget+1); err == nil {
		t.Fatal("oversized export accepted")
	}
	if _, err := f.Stat(); err != nil {
		t.Fatal("rejected snapshot consumed descriptor", err)
	}
	source, err := OwnSnapshot(d, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if RetainedBytes() != before+4096 {
		t.Fatal("snapshot allocation not counted")
	}
	var group sync.WaitGroup
	for n := 0; n < 8; n++ {
		group.Add(1)
		go func() { defer group.Done(); _ = source.Close() }()
	}
	group.Wait()
	if RetainedBytes() != before {
		t.Fatal("snapshot allocation released more or less than once")
	}
	if err := source.WithDescriptor(func(Descriptor) error { return nil }); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed snapshot exposed FD", err)
	}
}

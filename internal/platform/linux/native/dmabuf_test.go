//go:build linux && cgo

package native

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
	"github.com/codemodify/worldr/internal/render"
	"golang.org/x/sys/unix"
)

type fixtureDiagnostics struct {
	mu   sync.Mutex
	text bytes.Buffer
}

func (d *fixtureDiagnostics) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.text.Write(p)
}
func (d *fixtureDiagnostics) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.text.String()
}

func fixtureDMABuf(t *testing.T) (dmabuf.Descriptor, func()) {
	t.Helper()
	return fixtureDMABufFormat(t, dmabuf.ARGB8888)
}

func fixtureDMABufFormat(t *testing.T, format uint32) (dmabuf.Descriptor, func()) {
	return fixtureDMABufFormatModifier(t, format, dmabuf.Linear)
}

func fixtureDMABufFormatModifier(t *testing.T, format uint32, modifier uint64) (dmabuf.Descriptor, func()) {
	t.Helper()
	flags, err := exec.Command("pkg-config", "--cflags", "--libs", "gbm").Output()
	if err != nil {
		if os.Getenv("WORLDR_TEST_DMABUF") == "1" {
			t.Fatal(err)
		}
		t.Skip("GBM development files unavailable")
	}
	binaryPath := filepath.Join(t.TempDir(), "producer")
	args := append([]string{"testdata/dmabuf_fixture.c", "-o", binaryPath}, strings.Fields(string(flags))...)
	if out, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("build GBM producer: %v: %s", err, out)
	}
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	parent := os.NewFile(uintptr(pair[0]), "test-producer-parent")
	child := os.NewFile(uintptr(pair[1]), "test-producer-child")
	var stderr fixtureDiagnostics
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, binaryPath)
	command.Env = append(os.Environ(),
		"WORLDR_DMABUF_FIXTURE_FORMAT="+strconv.FormatUint(uint64(format), 10),
		"WORLDR_DMABUF_FIXTURE_MODIFIER="+strconv.FormatUint(modifier, 10),
	)
	command.ExtraFiles = []*os.File{child}
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		parent.Close()
		child.Close()
		t.Fatal(err)
	}
	child.Close()
	t.Cleanup(func() {
		parent.Write([]byte("q"))
		parent.Close()
		if err := command.Wait(); err != nil {
			t.Logf("GBM fixture: %v %s", err, stderr.String())
		}
	})
	data := make([]byte, 20)
	control := make([]byte, unix.CmsgSpace(4))
	n, cn, _, _, err := unix.Recvmsg(pair[0], data, control, unix.MSG_CMSG_CLOEXEC)
	if err != nil || n != len(data) {
		if os.Getenv("WORLDR_TEST_DMABUF") == "1" && (modifier == dmabuf.Linear || os.Getenv("WORLDR_TEST_DMABUF_MODIFIERS") == "1") {
			t.Fatalf("GBM producer failed: n=%d err=%v %s", n, err, stderr.String())
		}
		t.Skipf("GBM producer unavailable for modifier %#x: %s", modifier, stderr.String())
	}
	messages, err := unix.ParseSocketControlMessage(control[:cn])
	if err != nil || len(messages) != 1 {
		t.Fatal("missing descriptor", err)
	}
	fds, err := unix.ParseUnixRights(&messages[0])
	if err != nil || len(fds) != 1 {
		t.Fatal("invalid descriptor", err)
	}
	t.Cleanup(func() { unix.Close(fds[0]) })
	d := dmabuf.Descriptor{Width: int(binary.NativeEndian.Uint32(data)), Height: int(binary.NativeEndian.Uint32(data[4:])), FourCC: binary.NativeEndian.Uint32(data[16:]), Modifier: modifier, Planes: []dmabuf.Plane{{FD: fds[0], Stride: binary.NativeEndian.Uint32(data[8:]), Offset: binary.NativeEndian.Uint32(data[12:])}}}
	mutate := func() {
		t.Helper()
		if _, err := parent.Write([]byte("m")); err != nil {
			t.Fatal(err)
		}
		var reply [1]byte
		if n, err := parent.Read(reply[:]); n != 1 || err != nil || reply[0] != 'm' {
			t.Fatal("producer mutation failed", err)
		}
	}
	return d, mutate
}
func requireDMABuf(t *testing.T, vk *VK) {
	t.Helper()
	if len(vk.DMABufFormats()) == 0 {
		if os.Getenv("WORLDR_TEST_DMABUF") == "1" {
			t.Fatal("device lacks required DMA-BUF sharing")
		}
		t.Skip("device lacks required DMA-BUF sharing")
	}
}

func hasDMABufFormat(vk *VK, fourCC uint32) bool {
	for _, format := range vk.DMABufFormats() {
		if format.FourCC == fourCC && format.Modifier == dmabuf.Linear {
			return true
		}
	}
	return false
}

func TestDMABufModifierAdvertisementIsCapabilityGated(t *testing.T) {
	vk := openTextureGPU(t)
	formats := vk.DMABufFormats()
	if len(formats) == 0 {
		t.Skip("device lacks DMA-BUF sharing")
	}
	if len(formats) > dmabuf.MaxFormatPairs {
		t.Fatalf("advertised %d pairs beyond protocol capacity", len(formats))
	}
	seen := make(map[dmabuf.Format]bool, len(formats))
	for _, format := range formats {
		if seen[format] {
			t.Fatalf("duplicate format/modifier pair: %+v", format)
		}
		seen[format] = true
		if !dmabuf.SupportedFourCC(format.FourCC) || format.Modifier == dmabuf.Invalid || !vk.queryDMABufModifier(format.FourCC, format.Modifier) {
			t.Fatalf("uncapable pair advertised: %+v", format)
		}
	}
	first := formats[0]
	formats[0] = dmabuf.Format{}
	if got := vk.DMABufFormats()[0]; got != first {
		t.Fatalf("caller mutated cached capability list: got %+v want %+v", got, first)
	}
}

func TestNonLinearDMABufCopiesIntoLinearOwnedSnapshot(t *testing.T) {
	vk := openTextureGPU(t)
	var candidate dmabuf.Format
	for _, format := range vk.DMABufFormats() {
		if format.Modifier != dmabuf.Linear {
			candidate = format
			break
		}
	}
	if candidate.Modifier == dmabuf.Linear {
		if os.Getenv("WORLDR_TEST_DMABUF_MODIFIERS") == "1" {
			t.Fatal("device exposes no supported non-LINEAR RGB modifier")
		}
		t.Skip("device exposes no supported non-LINEAR RGB modifier")
	}
	descriptor, _ := fixtureDMABufFormatModifier(t, candidate.FourCC, candidate.Modifier)
	texture, err := vk.ImportDMABuf(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = vk.ReleaseTexture(texture.ID())
		_ = texture.Close()
	}()
	source, ok := texture.ExternalSource().(*dmabuf.Image)
	if !ok {
		t.Fatal("non-LINEAR import did not create an external snapshot")
	}
	if err := source.WithDescriptor(func(snapshot dmabuf.Descriptor) error {
		if snapshot.Modifier != dmabuf.Linear {
			t.Fatalf("retained snapshot modifier=%#x, want LINEAR", snapshot.Modifier)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	view, model := textureView()
	model[14] = .5
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}}}}}}
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 20, 20, [4]byte{255, 0, 0, 255})
}

func TestABGRDMABufPreservesRGBChannelOrder(t *testing.T) {
	vk := openTextureGPU(t)
	if !hasDMABufFormat(vk, dmabuf.ABGR8888) {
		t.Skip("device does not support linear ABGR8888 import/export")
	}
	descriptor, _ := fixtureDMABuf(t)
	// The fixture's four bytes are also a valid ABGR producer payload; changing
	// the descriptor verifies that Vulkan selects R8G8B8A8 rather than silently
	// interpreting every client as B8G8R8A8.
	descriptor.FourCC = dmabuf.ABGR8888
	texture, err := vk.ImportDMABuf(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	defer texture.Close()
	view, model := textureView()
	model[14] = .5
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}}}}}}
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 20, 20, [4]byte{0, 0, 255, 255})
	texturePixel(t, pixels, 64, 44, 20, [4]byte{0, 255, 0, 255})
	texturePixel(t, pixels, 64, 44, 44, [4]byte{0, 255, 255, 255})
}

func TestRGB2101010DMABufChannelOrderAndXAlpha(t *testing.T) {
	vk := openTextureGPU(t)
	candidates := []struct {
		name   string
		format uint32
	}{
		{"ARGB2101010", dmabuf.ARGB2101010},
		{"XRGB2101010", dmabuf.XRGB2101010},
		{"ABGR2101010", dmabuf.ABGR2101010},
		{"XBGR2101010", dmabuf.XBGR2101010},
	}
	available := 0
	for _, candidate := range candidates {
		if !hasDMABufFormat(vk, candidate.format) {
			continue
		}
		available++
		t.Run(candidate.name, func(t *testing.T) {
			descriptor, _ := fixtureDMABufFormat(t, candidate.format)
			texture, err := vk.ImportDMABuf(descriptor)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = vk.ReleaseTexture(texture.ID())
				_ = texture.Close()
			}()
			view, model := textureView()
			model[14] = .5
			frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}, Translucent: true}}}}}
			pixels := make([]byte, 64*64*4)
			if err := vk.RenderFrame(frame, [4]float32{0, 0, 1, 1}, pixels); err != nil {
				t.Fatal(err)
			}
			texturePixel(t, pixels, 64, 20, 20, [4]byte{255, 0, 0, 255})
			texturePixel(t, pixels, 64, 44, 20, [4]byte{0, 255, 0, 255})
			texturePixel(t, pixels, 64, 44, 44, [4]byte{255, 255, 0, 255})
		})
	}
	if available == 0 {
		t.Skip("device does not support LINEAR 2101010 DMA-BUF import/export")
	}
}

func TestDMABufGPUCopyOwnershipRecoveryAndSnapshot(t *testing.T) {
	vk := openTextureGPU(t)
	requireDMABuf(t, vk)
	descriptor, mutate := fixtureDMABuf(t)
	before := dmabuf.RetainedBytes()
	texture, err := vk.ImportDMABuf(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { texture.Close() })
	if texture.ExternalSource() == nil {
		t.Fatal("import fell back to CPU pixels")
	}
	if _, changed := texture.Snapshot(0); changed {
		t.Fatal("import produced CPU pixels")
	}
	if dmabuf.RetainedBytes() <= before {
		t.Fatal("exported snapshot allocation is unaccounted")
	}
	if _, err := unix.FcntlInt(uintptr(descriptor.Planes[0].FD), unix.F_GETFD, 0); err != nil {
		t.Fatal("import consumed borrowed source FD", err)
	}
	mutate() // Client may reuse the original immediately after successful import.
	view, model := textureView()
	model[14] = .5
	draw := render.Draw{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}}
	frame := render.Frame{LinearColor: true, Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{draw}}}}
	pixels := make([]byte, 64*64*4)
	check := func(device *VK) {
		t.Helper()
		if err := device.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
			t.Fatal(err)
		}
		texturePixel(t, pixels, 64, 20, 20, [4]byte{255, 0, 0, 255})
		texturePixel(t, pixels, 64, 44, 20, [4]byte{0, 255, 0, 255})
		texturePixel(t, pixels, 64, 20, 44, [4]byte{0, 0, 128, 255})
	}
	check(vk)
	if err := vk.Recover(); err != nil {
		t.Fatal(err)
	}
	check(vk)
	snapshot := openTextureGPU(t)
	requireDMABuf(t, snapshot)
	check(snapshot)
	// A UV crop selects the right half without modifying geometry bounds.
	frame.Commands[0].Draws[0].UV = [4]float32{.5, 0, .5, 1}
	if err := snapshot.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 20, 20, [4]byte{0, 255, 0, 255})
	// ARGB premultiplication survives both GPU copies; opaque XRGB clients
	// receive alpha one even when the otherwise-unused byte contains garbage.
	frame.LinearColor = false
	frame.Commands[0].Draws[0].UV = [4]float32{}
	frame.Commands[0].Draws[0].Translucent = true
	if err := snapshot.RenderFrame(frame, [4]float32{1, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 20, 44, [4]byte{127, 0, 128, 255})
	if err := vk.ReleaseTexture(texture.ID()); err != nil {
		t.Fatal(err)
	}
	if err := texture.Close(); err != nil {
		t.Fatal(err)
	}
	if got := dmabuf.RetainedBytes(); got != before {
		t.Fatalf("retained FD allocation leak: before=%d after=%d", before, got)
	}
	if err := vk.RenderFrame(frame, [4]float32{}, nil); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed external source did not fail visibly", err)
	}
}

func TestDMABufBudgetFailureRetainsBorrowedFDAndAccounting(t *testing.T) {
	vk := openTextureGPU(t)
	requireDMABuf(t, vk)
	descriptor, _ := fixtureDMABuf(t)
	if err := vk.RenderFrame(render.Frame{}, [4]float32{}, nil); err != nil {
		t.Fatal(err)
	}
	before := vk.MemoryStats()
	if err := vk.SetMemoryBudget(before.AllocatedBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := vk.ImportDMABuf(descriptor); !errors.Is(err, ErrOutOfMemory) {
		t.Fatal("import ignored allocation budget", err)
	}
	if stats := vk.MemoryStats(); stats.AllocatedBytes != before.AllocatedBytes || stats.Images != before.Images || stats.Buffers != before.Buffers {
		t.Fatalf("failed import leaked allocations: before=%+v after=%+v", before, stats)
	}
	if _, err := unix.FcntlInt(uintptr(descriptor.Planes[0].FD), unix.F_GETFD, 0); err != nil {
		t.Fatal("failed import closed caller handle", err)
	}
	if err := vk.SetMemoryBudget(0); err != nil {
		t.Fatal(err)
	}
	descriptor.FourCC = dmabuf.XRGB8888
	texture, err := vk.ImportDMABuf(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	defer texture.Close()
	view, model := textureView()
	model[14] = .5
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: view, Draws: []render.Draw{{Texture: texture, Model: model, Color: [4]float32{1, 1, 1, 1}, Translucent: true}}}}}
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, [4]float32{1, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	texturePixel(t, pixels, 64, 20, 44, [4]byte{0, 0, 128, 255})
}
func TestDMABufRejectsNonBufferAndUnsupportedLayout(t *testing.T) {
	vk := openTextureGPU(t)
	requireDMABuf(t, vk)
	file, err := os.CreateTemp(t.TempDir(), "not-dmabuf")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := file.Truncate(4096); err != nil {
		t.Fatal(err)
	}
	descriptor := dmabuf.Descriptor{Width: 16, Height: 16, FourCC: dmabuf.ARGB8888, Planes: []dmabuf.Plane{{FD: int(file.Fd()), Stride: 64}}}
	if _, err := vk.ImportDMABuf(descriptor); err == nil {
		t.Fatal("regular file accepted as DMA-BUF")
	}
	if _, err := file.Stat(); err != nil {
		t.Fatal("rejected import consumed borrowed FD", err)
	}
	descriptor.Modifier = 1
	for vk.supportsDMABufModifier(descriptor.FourCC, descriptor.Modifier) {
		descriptor.Modifier++
	}
	if _, err := vk.ImportDMABuf(descriptor); !errors.Is(err, dmabuf.ErrUnsupported) {
		t.Fatal("unadvertised modifier accepted", err)
	}
}

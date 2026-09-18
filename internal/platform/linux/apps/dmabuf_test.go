//go:build linux && cgo

package apps

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/platform/linux/dmabuf"
	"github.com/codemodify/worldr/internal/platform/linux/native"
	"github.com/codemodify/worldr/internal/render"
	"golang.org/x/sys/unix"
)

func bindWireGlobal(c *cursorWireClient, kind string, version uint32) uint32 {
	c.t.Helper()
	registry := c.id()
	c.send(1, 1, -1, registry)
	c.roundtrip()
	for _, e := range c.events {
		if e.id != registry || e.opcode != 0 {
			continue
		}
		length := int(binary.LittleEndian.Uint32(e.data[4:]))
		name := string(e.data[8 : 8+length-1])
		if name == kind {
			id := c.id()
			c.send(registry, 0, -1, binary.LittleEndian.Uint32(e.data), name, version, id)
			c.roundtrip()
			return id
		}
	}
	return 0
}

func TestDMABufV4FeedbackAdvertisesExactDeviceAndPairs(t *testing.T) {
	s, err := Open(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	formats := []dmabuf.Format{
		{FourCC: dmabuf.XRGB8888, Modifier: 0},
		{FourCC: dmabuf.ARGB8888, Modifier: 0x0100000000000002},
	}
	if err := s.setDMABufImporter(formats, "/dev/dri/renderD128", 226, 128, true,
		func(dmabuf.Descriptor) (*render.Texture, error) { return nil, dmabuf.ErrUnsupported }); err != nil {
		t.Fatal(err)
	}
	got, node := s.DMABufCapabilities()
	if node != "/dev/dri/renderD128" || len(got) != len(formats) {
		t.Fatalf("capabilities %v %q", got, node)
	}
	got[0].FourCC = 0
	if fresh, _ := s.DMABufCapabilities(); fresh[0] != formats[0] {
		t.Fatal("capability result aliases server storage")
	}
	c := newCursorWireClient(t, s)
	c.preserveFDs = true
	manager := bindWireGlobal(c, "zwp_linux_dmabuf_v1", 4)
	if manager == 0 {
		t.Fatal("version 4 manager missing")
	}
	for _, event := range c.events {
		if event.id == manager && (event.opcode == 0 || event.opcode == 1) {
			t.Fatal("version 4 manager emitted deprecated format event")
		}
	}
	feedback := c.id()
	c.send(manager, 2, -1, feedback)
	c.roundtrip()
	defer func() {
		for _, fd := range c.receivedFDs {
			unix.Close(fd)
		}
	}()
	if len(c.receivedFDs) != 1 {
		t.Fatalf("feedback format-table descriptors: %v", c.receivedFDs)
	}
	seals, err := unix.FcntlInt(uintptr(c.receivedFDs[0]), unix.F_GET_SEALS, 0)
	if err != nil {
		t.Fatalf("read format-table seals: %v", err)
	}
	wantSeals := unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE | unix.F_SEAL_SEAL
	if seals&wantSeals != wantSeals {
		t.Fatalf("format table is mutable: seals=%#x want at least %#x", seals, wantSeals)
	}
	wantOpcodes := []uint16{1, 2, 4, 6, 5, 3, 0}
	var events []cursorWireEvent
	for _, event := range c.events {
		if event.id == feedback {
			events = append(events, event)
		}
	}
	if len(events) != len(wantOpcodes) {
		t.Fatalf("feedback events: %+v", events)
	}
	for i, opcode := range wantOpcodes {
		if events[i].opcode != opcode {
			t.Fatalf("feedback event %d opcode %d, want %d", i, events[i].opcode, opcode)
		}
	}
	if len(events[0].data) != 4 || binary.LittleEndian.Uint32(events[0].data) != uint32(len(formats)*16) {
		t.Fatalf("format table metadata %x", events[0].data)
	}
	table := make([]byte, len(formats)*16)
	if n, err := unix.Pread(c.receivedFDs[0], table, 0); err != nil || n != len(table) {
		t.Fatalf("read format table: %d %v", n, err)
	}
	for i, format := range formats {
		entry := table[i*16:]
		if binary.NativeEndian.Uint32(entry) != format.FourCC ||
			binary.NativeEndian.Uint32(entry[4:]) != 0 ||
			binary.NativeEndian.Uint64(entry[8:]) != format.Modifier {
			t.Fatalf("format table entry %d: %x", i, entry)
		}
	}
	for _, event := range []cursorWireEvent{events[1], events[2]} {
		if len(event.data) != 12 || binary.LittleEndian.Uint32(event.data) != 8 ||
			binary.NativeEndian.Uint64(event.data[4:]) != unix.Mkdev(226, 128) {
			t.Fatalf("feedback device event %x", event.data)
		}
	}
	if data := events[4].data; len(data) != 8 || binary.LittleEndian.Uint32(data) != 4 ||
		binary.NativeEndian.Uint16(data[4:]) != 0 || binary.NativeEndian.Uint16(data[6:]) != 1 {
		t.Fatalf("feedback tranche indices %x", data)
	}
}

func TestDMABufDeviceRegistrationRejectsInvalidNode(t *testing.T) {
	s, err := Open(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	importer := func(dmabuf.Descriptor) (*render.Texture, error) { return nil, nil }
	formats := []dmabuf.Format{{FourCC: dmabuf.ARGB8888}}
	for _, node := range []string{"relative/renderD128", "/dev/dri/card0", "/dev/dri/renderD999999"} {
		if err := s.SetDMABufImporterForDevice(formats, node, importer); err == nil {
			t.Fatalf("accepted invalid render node %q", node)
		}
	}
}

func TestDMABufRenderDeviceIdentity(t *testing.T) {
	if !drmRenderDevice(226, 128) || !drmRenderDevice(226, 191) {
		t.Fatal("DRM render minor range rejected")
	}
	for _, device := range [][2]uint32{{1, 128}, {226, 127}, {226, 192}} {
		if drmRenderDevice(device[0], device[1]) {
			t.Fatalf("accepted non-render device %d:%d", device[0], device[1])
		}
	}
}

func TestDMABufAdvertisementAndMalformedDescriptorIsolation(t *testing.T) {
	s, err := Open(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	healthy := newCursorWireClient(t, s)
	if bindWireGlobal(healthy, "zwp_linux_dmabuf_v1", 3) != 0 {
		t.Fatal("unsupported GPU protocol advertised by default")
	}
	called := false
	if err := s.SetDMABufImporter([]dmabuf.Format{{FourCC: dmabuf.ARGB8888, Modifier: dmabuf.Invalid}}, func(dmabuf.Descriptor) (*render.Texture, error) { return nil, nil }); err == nil {
		t.Fatal("implicit/invalid modifier registration accepted")
	}
	tooMany := make([]dmabuf.Format, dmabuf.MaxFormatPairs+1)
	for i := range tooMany {
		tooMany[i] = dmabuf.Format{FourCC: dmabuf.ARGB8888}
	}
	if err := s.SetDMABufImporter(tooMany, func(dmabuf.Descriptor) (*render.Texture, error) { return nil, nil }); err == nil {
		t.Fatal("unbounded format/modifier registration accepted")
	}
	formats := []dmabuf.Format{
		{FourCC: dmabuf.XRGB8888}, {FourCC: dmabuf.ARGB8888},
		{FourCC: dmabuf.XBGR8888}, {FourCC: dmabuf.ABGR8888},
		{FourCC: dmabuf.XRGB2101010}, {FourCC: dmabuf.ARGB2101010},
		{FourCC: dmabuf.XBGR2101010}, {FourCC: dmabuf.ABGR2101010},
		{FourCC: dmabuf.ARGB8888, Modifier: 0x0100000000000002},
	}
	if err := s.SetDMABufImporter(formats, func(dmabuf.Descriptor) (*render.Texture, error) { called = true; return nil, dmabuf.ErrUnsupported }); err != nil {
		t.Fatal(err)
	}
	c := newCursorWireClient(t, s)
	manager := bindWireGlobal(c, "zwp_linux_dmabuf_v1", 3)
	if manager == 0 {
		t.Fatal("supported importer was not advertised")
	}
	advertisedFormats := make(map[uint32]bool)
	advertisedPairs := make(map[dmabuf.Format]bool)
	for _, event := range c.events {
		if event.id != manager || len(event.data) < 4 {
			continue
		}
		format := binary.LittleEndian.Uint32(event.data)
		switch event.opcode {
		case 0:
			advertisedFormats[format] = true
		case 1:
			if len(event.data) != 12 {
				t.Fatalf("format %#x advertised with malformed modifier event %x", format, event.data)
			}
			modifier := uint64(binary.LittleEndian.Uint32(event.data[4:]))<<32 | uint64(binary.LittleEndian.Uint32(event.data[8:]))
			advertisedPairs[dmabuf.Format{FourCC: format, Modifier: modifier}] = true
		}
	}
	if len(advertisedFormats) != 8 {
		t.Fatalf("advertised %d unique DMA-BUF formats; want 8", len(advertisedFormats))
	}
	for _, format := range formats {
		if !advertisedFormats[format.FourCC] || !advertisedPairs[format] {
			t.Fatalf("format/modifier pair missing: %+v", format)
		}
	}
	f, err := os.CreateTemp(t.TempDir(), "not-a-dmabuf")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(4096); err != nil {
		t.Fatal(err)
	}
	params := c.id()
	c.send(manager, 1, -1, params)
	c.send(params, 1, int(f.Fd()), uint32(0), uint32(0), uint32(128), uint32(0), uint32(0))
	c.send(params, 2, -1, 32, 32, dmabuf.ARGB8888, uint32(0))
	c.roundtrip()
	failed := false
	for _, e := range c.events {
		if e.id == params && e.opcode == 1 {
			failed = true
		}
	}
	if !failed || called {
		t.Fatal("regular file reached GPU importer", failed, called)
	}
	c.send(params, 0, -1)
	c.roundtrip()
	params = c.id()
	c.send(manager, 1, -1, params)
	c.send(params, 1, int(f.Fd()), uint32(1), uint32(0), uint32(128), uint32(0), uint32(0))
	if err := c.sync(); err == nil {
		t.Fatal("unsupported plane index accepted")
	}
	healthy.roundtrip()
	if _, err := s.Poll(); err != nil {
		t.Fatal("bad client poisoned host", err)
	}
}

func appDMABufFixture(t *testing.T) (dmabuf.Descriptor, func()) {
	return appDMABufFixtureFormatModifier(t, dmabuf.ARGB8888, dmabuf.Linear)
}

func appDMABufFixtureFormatModifier(t *testing.T, format uint32, modifier uint64) (dmabuf.Descriptor, func()) {
	t.Helper()
	flags, err := exec.Command("pkg-config", "--cflags", "--libs", "gbm").Output()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "producer")
	args := append([]string{"../native/testdata/dmabuf_fixture.c", "-o", path}, strings.Fields(string(flags))...)
	if out, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	parent, child := os.NewFile(uintptr(pair[0]), "producer-host"), os.NewFile(uintptr(pair[1]), "producer-client")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, path)
	cmd.Env = append(os.Environ(),
		"WORLDR_DMABUF_FIXTURE_FORMAT="+strconv.FormatUint(uint64(format), 10),
		"WORLDR_DMABUF_FIXTURE_MODIFIER="+strconv.FormatUint(modifier, 10),
	)
	cmd.ExtraFiles = []*os.File{child}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		parent.Close()
		child.Close()
		t.Fatal(err)
	}
	child.Close()
	t.Cleanup(func() {
		parent.Write([]byte("q"))
		parent.Close()
		if err := cmd.Wait(); err != nil {
			t.Log(err)
		}
	})
	data, control := make([]byte, 20), make([]byte, unix.CmsgSpace(4))
	n, cn, _, _, err := unix.Recvmsg(pair[0], data, control, unix.MSG_CMSG_CLOEXEC)
	if err != nil || n != len(data) {
		if modifier != dmabuf.Linear && os.Getenv("WORLDR_TEST_DMABUF_MODIFIERS") != "1" {
			t.Skipf("GBM producer unavailable for modifier %#x", modifier)
		}
		t.Fatal("GBM fixture did not provide image", n, err)
	}
	messages, err := unix.ParseSocketControlMessage(control[:cn])
	if err != nil || len(messages) != 1 {
		t.Fatal(err, messages)
	}
	fds, err := unix.ParseUnixRights(&messages[0])
	if err != nil || len(fds) != 1 {
		t.Fatal(err, fds)
	}
	t.Cleanup(func() { unix.Close(fds[0]) })
	d := dmabuf.Descriptor{Width: int(binary.NativeEndian.Uint32(data)), Height: int(binary.NativeEndian.Uint32(data[4:])), FourCC: binary.NativeEndian.Uint32(data[16:]), Modifier: modifier, Planes: []dmabuf.Plane{{FD: fds[0], Stride: binary.NativeEndian.Uint32(data[8:]), Offset: binary.NativeEndian.Uint32(data[12:])}}}
	mutate := func() {
		t.Helper()
		if _, err := parent.Write([]byte("m")); err != nil {
			t.Fatal(err)
		}
		var b [1]byte
		if n, err := parent.Read(b[:]); err != nil || n != 1 || b[0] != 'm' {
			t.Fatal("GBM mutation failed", err)
		}
	}
	return d, mutate
}

func TestDMABufProtocolPassesAdvertisedNonLinearModifier(t *testing.T) {
	if os.Getenv("WORLDR_TEST_DMABUF") != "1" {
		t.Skip("set WORLDR_TEST_DMABUF=1 for real GBM/Vulkan protocol integration")
	}
	vk, err := native.OpenVK(false, 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer vk.Close()
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
	s, err := Open(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		s.Close()
		for _, texture := range s.RetiredTextures() {
			_ = vk.ReleaseTexture(texture.ID())
			_ = texture.Close()
		}
	}()
	var imported dmabuf.Descriptor
	imports := 0
	if err := s.SetDMABufImporter(vk.DMABufFormats(), func(descriptor dmabuf.Descriptor) (*render.Texture, error) {
		imports++
		imported = descriptor
		return vk.ImportDMABuf(descriptor)
	}); err != nil {
		t.Fatal(err)
	}
	c := newCursorWireClient(t, s)
	manager := bindWireGlobal(c, "zwp_linux_dmabuf_v1", 3)
	descriptor, _ := appDMABufFixtureFormatModifier(t, candidate.FourCC, candidate.Modifier)
	plane := descriptor.Planes[0]
	params, buffer := c.id(), c.id()
	c.send(manager, 1, -1, params)
	c.send(params, 1, plane.FD, uint32(0), plane.Offset, plane.Stride, uint32(candidate.Modifier>>32), uint32(candidate.Modifier))
	c.send(params, 3, -1, buffer, descriptor.Width, descriptor.Height, descriptor.FourCC, uint32(0))
	c.send(params, 0, -1)
	c.send(c.surface, 1, -1, buffer, 0, 0)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	surfaces, err := s.Poll()
	if err != nil || len(surfaces) != 1 || surfaces[0].Texture == nil {
		t.Fatal("non-LINEAR protocol import did not publish a texture", surfaces, err)
	}
	if imported.FourCC != candidate.FourCC || imported.Modifier != candidate.Modifier {
		t.Fatalf("importer received %+v, want %+v", dmabuf.Format{FourCC: imported.FourCC, Modifier: imported.Modifier}, candidate)
	}
	if source, ok := surfaces[0].Texture.ExternalSource().(*dmabuf.Image); !ok {
		t.Fatal("protocol import lost external snapshot")
	} else if err := source.WithDescriptor(func(snapshot dmabuf.Descriptor) error {
		if snapshot.Modifier != dmabuf.Linear {
			t.Fatalf("protocol retained modifier %#x, want LINEAR", snapshot.Modifier)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	unsupported := candidate.Modifier + 1
	for {
		found := false
		for _, format := range vk.DMABufFormats() {
			if format.FourCC == candidate.FourCC && format.Modifier == unsupported {
				found = true
				unsupported++
				break
			}
		}
		if !found {
			break
		}
	}
	bad := c.id()
	c.send(manager, 1, -1, bad)
	c.send(bad, 1, plane.FD, uint32(0), plane.Offset, plane.Stride, uint32(unsupported>>32), uint32(unsupported))
	c.send(bad, 2, -1, descriptor.Width, descriptor.Height, descriptor.FourCC, uint32(0))
	c.roundtrip()
	failed := false
	for _, event := range c.events {
		if event.id == bad && event.opcode == 1 {
			failed = true
		}
	}
	if !failed || imports != 1 {
		t.Fatal("valid DMA-BUF with an unadvertised modifier reached importer", failed, imports)
	}
}

func TestDMABufProtocolRetainsGPUCopyAndOrderedClippedLayers(t *testing.T) {
	if os.Getenv("WORLDR_TEST_DMABUF") != "1" {
		t.Skip("set WORLDR_TEST_DMABUF=1 for real GBM/Vulkan protocol integration")
	}
	vk, err := native.OpenVK(false, 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	defer vk.Close()
	s, err := Open(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		s.Close()
		for _, texture := range s.RetiredTextures() {
			vk.ReleaseTexture(texture.ID())
			texture.Close()
		}
	}()
	if len(vk.DMABufFormats()) == 0 {
		t.Fatal("GPU does not support implemented DMA-BUF subset")
	}
	if err := s.SetDMABufImporter(vk.DMABufFormats(), vk.ImportDMABuf); err != nil {
		t.Fatal(err)
	}
	c := newCursorWireClient(t, s)
	manager := bindWireGlobal(c, "zwp_linux_dmabuf_v1", 3)
	d, mutate := appDMABufFixture(t)
	p := d.Planes[0]
	params, buffer := c.id(), c.id()
	c.send(manager, 1, -1, params)
	c.send(params, 1, p.FD, uint32(0), p.Offset, p.Stride, uint32(0), uint32(0))
	c.send(params, 3, -1, buffer, d.Width, d.Height, d.FourCC, uint32(0))
	c.send(params, 0, -1)
	c.send(c.surface, 1, -1, buffer, 0, 0)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	released := false
	for _, e := range c.events {
		if e.id == buffer && e.opcode == 0 {
			released = true
		}
	}
	if !released {
		t.Fatal("client buffer remained busy after synchronous copy")
	}
	surfaces, err := s.Poll()
	if err != nil || len(surfaces) != 1 {
		t.Fatal(surfaces, err)
	}
	root := surfaces[0]
	if root.Texture == nil || root.Texture.ExternalSource() == nil || len(root.Pixels) != 0 || len(root.Layers) != 1 || !root.Layers[0].Root {
		t.Fatal("GPU import did not reach retained root", root)
	}
	if _, cpu := root.Texture.Snapshot(0); cpu {
		t.Fatal("GPU import copied through CPU pixels")
	}
	mutate()
	identity := [16]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	projection := identity
	projection[5] = -1
	model := identity
	model[14] = .5
	frame := render.Frame{Commands: []render.Command{{Kind: render.SceneCommand, View: render.View{Projection: projection, Viewport: [4]float32{0, 0, 64, 64}}, Draws: []render.Draw{{Texture: root.Texture, Model: model, Color: [4]float32{1, 1, 1, 1}}}}}}
	pixels := make([]byte, 64*64*4)
	if err := vk.RenderFrame(frame, [4]float32{0, 0, 0, 1}, pixels); err != nil {
		t.Fatal(err)
	}
	if got := pixels[(20*64+20)*4:][:4]; got[2] < 250 || got[0] > 2 || got[1] > 2 {
		t.Fatal("client reuse changed retained GPU image", got)
	}
	// Put one child behind the root and another partly outside the right edge.
	var front uint32
	for _, item := range []struct {
		x, y  int
		below bool
	}{{0, 0, true}, {24, 5, false}} {
		child := c.cursor()
		sub := c.id()
		c.send(c.subcompositor, 1, -1, sub, child, c.surface)
		c.send(sub, 1, -1, item.x, item.y)
		if item.below {
			c.send(sub, 3, -1, c.surface)
		}
		format := uint32(0)
		if !item.below {
			format = 1
			front = child
		}
		b := c.image(16, 12, 0x80404040, format)
		c.send(child, 1, -1, b, 0, 0)
		c.send(child, 6, -1)
		c.send(c.surface, 6, -1)
	}
	c.roundtrip()
	surfaces, err = s.Poll()
	if err != nil {
		t.Fatal(err)
	}
	layers := surfaces[0].Layers
	if len(layers) != 3 || layers[0].Root || !layers[1].Root || layers[2].Root || layers[2].Width != 8 || layers[2].UV[2] != .5 {
		t.Fatal("child order or root clipping changed", layers)
	}
	if layers[1].Texture != root.Texture {
		t.Fatal("child commit reimported unchanged root")
	}
	if layers[0].Opaque || layers[1].Opaque || !layers[2].Opaque {
		t.Fatal("backing format opacity lost", layers)
	}
	oldChild := layers[2].Texture
	newChild := c.image(16, 12, 0xff102030, uint32(1))
	c.send(front, 1, -1, newChild, 0, 0)
	c.send(front, 6, -1)
	c.roundtrip()
	surfaces, err = s.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if surfaces[0].Layers[2].Texture != oldChild || len(s.RetiredTextures()) != 0 {
		t.Fatal("synchronized child replaced or retired before parent commit")
	}
	c.send(c.surface, 6, -1)
	c.roundtrip()
	surfaces, err = s.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if surfaces[0].Layers[2].Texture == oldChild {
		t.Fatal("parent commit did not publish child")
	}
	childRetired := s.RetiredTextures()
	if len(childRetired) != 1 || childRetired[0] != oldChild {
		t.Fatal("presented child retirement mismatch", childRetired)
	}
	oldChild.Close()
	c.send(c.surface, 1, -1, buffer, 0, 0)
	c.send(c.surface, 6, -1)
	c.roundtrip()
	surfaces, err = s.Poll()
	if err != nil {
		t.Fatal(err)
	}
	if surfaces[0].Texture == root.Texture {
		t.Fatal("new GPU commit reused old snapshot")
	}
	retired := s.RetiredTextures()
	if len(retired) != 1 || retired[0] != root.Texture {
		t.Fatal("old GPU image not retired exactly once", retired)
	}
	if err := vk.ReleaseTexture(retired[0].ID()); err != nil {
		t.Fatal(err)
	}
	retired[0].Close()
	if len(s.RetiredTextures()) != 0 {
		t.Fatal("retirement repeated")
	}
	c.c.Close()
	if _, err := s.Poll(); err != nil {
		t.Fatal(err)
	}
	if retired = s.RetiredTextures(); len(retired) != 3 {
		t.Fatal("disconnect did not retire GPU and SHM layers", len(retired))
	}
	for _, texture := range retired {
		vk.ReleaseTexture(texture.ID())
		texture.Close()
	}
}

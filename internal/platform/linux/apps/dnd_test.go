//go:build linux && cgo

package apps

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestChromiumCrossClientDragDrop(t *testing.T) {
	launch := func(title, html string) func(string) []string {
		return func(dir string) []string {
			page := filepath.Join(dir, "drag.html")
			content := `<!doctype html><meta charset="utf-8"><title>` + title + `</title><style>body{margin:0;background:#19394b;color:white;font:24px sans-serif}div{position:absolute;inset:0;padding:40px}</style>` + html
			if err := os.WriteFile(page, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			return []string{"--ozone-platform=wayland", "--disable-gpu", "--user-data-dir=" + filepath.Join(dir, "profile"), "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-component-update", "--disable-sync", "--disable-default-apps", "--disable-features=MediaRouter", "--app=file://" + page}
		}
	}
	source := startToolkit(t, "chromium", launch("drag source ready", `<div draggable="true" ondragstart="event.dataTransfer.setData('text/plain','worldr drag λ');event.dataTransfer.effectAllowed='copy';document.title='drag source started'" ondragend="document.title='drag source ended:'+event.dataTransfer.dropEffect">Drag research notes</div>`))
	target := startToolkit(t, "chromium", launch("drag target ready", `<div ondragover="event.preventDefault();event.dataTransfer.dropEffect='copy';document.title='drag target over'" ondrop="event.preventDefault();document.title='dropped:'+event.dataTransfer.getData('text/plain')">Drop research notes</div>`), source.server)
	var sourceID, targetID uint64
	source.until("two Chromium clients", func(surfaces []Surface) bool {
		for _, v := range surfaces {
			if v.Title == "drag source ready" {
				sourceID = v.ID
			}
			if v.Title == "drag target ready" {
				targetID = v.ID
			}
		}
		return sourceID != 0 && targetID != 0
	})
	source.server.Focus(sourceID)
	source.settle()
	if err := source.server.Pointer(sourceID, 100, 150); err != nil {
		t.Fatal(err)
	}
	source.server.Button(272, true, 10)
	x := float32(100)
	source.until("native Chromium drag start", func([]Surface) bool {
		if source.server.DragActive() {
			return true
		}
		if x < 400 {
			x += 8
			source.server.Pointer(sourceID, x, 150)
		}
		return false
	})
	if err := source.server.Pointer(targetID, 100, 150); err != nil {
		t.Fatal(err)
	}
	target.until("other client accepts drag", func(surfaces []Surface) bool {
		for _, v := range surfaces {
			if v.ID == targetID && v.Title == "drag target over" {
				return true
			}
		}
		source.server.Pointer(targetID, 101, 150)
		return false
	})
	if err := source.server.Button(272, false, 20); err != nil {
		t.Fatal(err)
	}
	target.until("actual MIME data reaches Chromium drop handler", func(surfaces []Surface) bool {
		for _, v := range surfaces {
			if v.ID == targetID && v.Title == "dropped:worldr drag λ" {
				return true
			}
		}
		return false
	})
	source.until("source receives completion", func(surfaces []Surface) bool {
		for _, v := range surfaces {
			if v.ID == sourceID && strings.HasPrefix(v.Title, "drag source ended:copy") {
				return true
			}
		}
		return false
	})
	if source.server.DragActive() {
		t.Fatal("completed HTML drop retained implicit drag")
	}
}

func TestDragIconRetainedOffsetsAndCancel(t *testing.T) {
	s, err := Open(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := newDragWireClient(t, s, 3)
	surfaces, _ := s.Poll()
	c.createSource(1, 3)
	serial := c.press(surfaces[0].ID)
	icon := c.cursor()
	buffer := c.image(12, 10, 0x80402010)
	c.send(icon, 1, -1, buffer, 4, 5)
	c.send(icon, 6, -1)
	c.send(c.device, 0, -1, c.source, c.surface, icon, serial)
	c.roundtrip()
	cursor := s.Cursor()
	if !cursor.Set || cursor.Hidden || cursor.Width != 12 || cursor.Height != 10 || cursor.HotspotX != -4 || cursor.HotspotY != -5 || cursor.Pixels[3] != 128 {
		t.Fatal("drag image absent or opaque", cursor)
	}
	original := append([]byte(nil), cursor.Pixels...)
	c.send(icon, 1, -1, buffer, 2, -1)
	c.send(icon, 6, -1)
	c.roundtrip()
	moved := s.Cursor()
	if moved.HotspotX != -6 || moved.HotspotY != -4 || moved.Revision == cursor.Revision {
		t.Fatal("drag image attach offset not accumulated", moved)
	}
	if err := s.CancelDrag(); err != nil {
		t.Fatal(err)
	}
	c.roundtrip()
	if after := s.Cursor(); after.Set {
		t.Fatal("cancel kept drag icon", after)
	}
	if string(cursor.Pixels) != string(original) {
		t.Fatal("retained drag image changed")
	}
}

type dragWireClient struct {
	*cursorWireClient
	manager, device, source uint32
}

func newDragWireClient(t *testing.T, s *Server, version uint32) *dragWireClient {
	t.Helper()
	c := newCursorWireClient(t, s)
	client := &dragWireClient{cursorWireClient: c}
	registry := c.id()
	c.send(1, 1, -1, registry)
	c.roundtrip()
	for _, event := range c.events {
		if event.id != registry || event.opcode != 0 {
			continue
		}
		size := int(binary.LittleEndian.Uint32(event.data[4:]))
		kind := string(event.data[8 : 8+size-1])
		if kind == "wl_data_device_manager" {
			client.manager = c.id()
			c.send(registry, 0, -1, binary.LittleEndian.Uint32(event.data), kind, version, client.manager)
		}
	}
	if client.manager == 0 {
		t.Fatal("data device manager missing")
	}
	client.device = c.id()
	c.send(client.manager, 1, -1, client.device, c.seat)
	c.roundtrip()
	t.Cleanup(func() {
		for _, fd := range c.receivedFDs {
			unix.Close(fd)
		}
	})
	return client
}
func (c *dragWireClient) createSource(actions uint32, version uint32) {
	c.source = c.id()
	c.send(c.manager, 0, -1, c.source)
	c.send(c.source, 0, -1, "text/plain;charset=utf-8")
	if version >= 3 {
		c.send(c.source, 2, -1, actions)
	}
	c.roundtrip()
}
func (c *dragWireClient) lastEvent(id uint32, opcode uint16) (cursorWireEvent, bool) {
	for i := len(c.events) - 1; i >= 0; i-- {
		event := c.events[i]
		if event.id == id && event.opcode == opcode {
			return event, true
		}
	}
	return cursorWireEvent{}, false
}
func (c *dragWireClient) press(surfaceID uint64) uint32 {
	c.t.Helper()
	if err := c.s.Pointer(surfaceID, 5, 5); err != nil {
		c.t.Fatal(err)
	}
	if err := c.s.Button(272, true, 51); err != nil {
		c.t.Fatal(err)
	}
	c.roundtrip()
	event, ok := c.lastEvent(c.pointer, 3)
	if !ok || binary.LittleEndian.Uint32(event.data[12:]) != 1 {
		c.t.Fatal("pointer press was not delivered")
	}
	return binary.LittleEndian.Uint32(event.data)
}
func (c *dragWireClient) start(serial uint32) {
	c.send(c.device, 0, -1, c.source, c.surface, uint32(0), serial)
	c.roundtrip()
}
func (c *dragWireClient) accept(version, action uint32) (offer uint32) {
	c.t.Helper()
	c.roundtrip()
	event, ok := c.lastEvent(c.device, 1)
	if !ok {
		c.t.Fatal("drag destination did not receive enter")
	}
	serial := binary.LittleEndian.Uint32(event.data)
	offer = binary.LittleEndian.Uint32(event.data[16:])
	if offer == 0 {
		c.t.Fatal("drag MIME offer missing")
	}
	c.send(offer, 0, -1, serial, "text/plain;charset=utf-8")
	if version >= 3 {
		c.send(offer, 4, -1, uint32(3), action)
	}
	c.roundtrip()
	return offer
}

func TestDragDropTransfersBetweenClientsAndFinishesAfterDrop(t *testing.T) {
	for _, version := range []uint32{1, 3} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			s, err := Open(320, 240)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			source, target := newDragWireClient(t, s, 3), newDragWireClient(t, s, version)
			surfaces, err := s.Poll()
			if err != nil || len(surfaces) != 2 {
				t.Fatal(surfaces, err)
			}
			source.createSource(3, 3)
			serial := source.press(surfaces[0].ID)
			source.start(serial)
			if !s.DragActive() {
				t.Fatal("valid input serial did not start drag")
			}
			if err := s.Pointer(surfaces[1].ID, 7, 8); err != nil {
				t.Fatal(err)
			}
			offer := target.accept(version, 1)
			if err := s.Button(272, false, 52); err != nil {
				t.Fatal(err)
			}
			target.roundtrip()
			if s.DragActive() {
				t.Fatal("release did not end drag gesture")
			}
			if _, ok := target.lastEvent(target.device, 4); !ok {
				t.Fatal("accepted destination did not receive drop")
			}
			read, write, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close()
			source.preserveFDs = true
			target.send(offer, 1, int(write.Fd()), "text/plain;charset=utf-8")
			write.Close()
			target.roundtrip()
			source.roundtrip()
			if len(source.receivedFDs) != 1 {
				t.Fatal("drop did not transfer exactly one writable descriptor", source.receivedFDs)
			}
			fd := source.receivedFDs[0]
			source.receivedFDs = nil
			transferred := os.NewFile(uintptr(fd), "dnd-transfer")
			const contents = "scientific data: λ\n"
			if _, err := transferred.WriteString(contents); err != nil {
				t.Fatal(err)
			}
			transferred.Close()
			read.SetReadDeadline(time.Now().Add(time.Second))
			data, err := io.ReadAll(read)
			if err != nil || string(data) != contents {
				t.Fatal("drag payload changed", string(data), err)
			}
			if version >= 3 {
				target.send(offer, 3, -1)
			} else {
				target.send(offer, 2, -1)
			}
			target.roundtrip()
			source.roundtrip()
			if _, ok := source.lastEvent(source.source, 4); !ok {
				t.Fatal("source did not receive successful completion")
			}
			if _, ok := source.lastEvent(source.source, 2); ok {
				t.Fatal("successful drop was reported as cancellation")
			}
		})
	}
}

func TestDragRejectsStaleSerialAndExplicitCancelNeverDrops(t *testing.T) {
	s, err := Open(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source, target := newDragWireClient(t, s, 3), newDragWireClient(t, s, 3)
	surfaces, _ := s.Poll()
	source.createSource(1, 3)
	serial := source.press(surfaces[0].ID)
	source.start(serial + 1)
	if s.DragActive() {
		t.Fatal("stale serial started drag")
	}
	if _, ok := source.lastEvent(source.source, 2); !ok {
		t.Fatal("rejected source did not receive cancellation")
	}
	source.createSource(1, 3)
	source.start(serial)
	if !s.DragActive() {
		t.Fatal("valid serial failed after rejected request")
	}
	s.Pointer(surfaces[1].ID, 3, 4)
	offer := target.accept(3, 1)
	if err := s.Pointer(0, 0, 0); err != nil || !s.DragActive() {
		t.Fatal("leaving destination canceled the drag", err)
	}
	s.Pointer(surfaces[1].ID, 5, 6)
	_ = target.accept(3, 1)
	if err := s.CancelDrag(); err != nil {
		t.Fatal(err)
	}
	target.roundtrip()
	source.roundtrip()
	if s.DragActive() {
		t.Fatal("host cancellation retained gesture")
	}
	if _, ok := target.lastEvent(target.device, 4); ok {
		t.Fatal("cancel delivered drop")
	}
	if _, ok := source.lastEvent(source.source, 2); !ok {
		t.Fatal("cancel was not sent to source")
	}
	// The old offer cannot acquire data after leave/cancel.
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	target.send(offer, 1, int(write.Fd()), "text/plain;charset=utf-8")
	write.Close()
	target.roundtrip()
	read.SetReadDeadline(time.Now().Add(time.Second))
	data, err := io.ReadAll(read)
	if err != nil || len(data) != 0 {
		t.Fatal("stale offer retained a writable transfer", data, err)
	}
}

func TestDragOriginDisconnectCancelsWithoutAffectingOtherClient(t *testing.T) {
	s, err := Open(320, 240)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source, target := newDragWireClient(t, s, 3), newDragWireClient(t, s, 3)
	surfaces, _ := s.Poll()
	source.createSource(1, 3)
	source.start(source.press(surfaces[0].ID))
	s.Pointer(surfaces[1].ID, 3, 4)
	target.accept(3, 1)
	source.c.Close()
	target.roundtrip()
	if s.DragActive() {
		t.Fatal("disconnected origin retained drag")
	}
	remaining, err := s.Poll()
	if err != nil || len(remaining) != 1 || remaining[0].ID != surfaces[1].ID {
		t.Fatal("origin disconnect damaged destination", remaining, err)
	}
}

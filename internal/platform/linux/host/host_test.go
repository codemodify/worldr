//go:build linux && cgo

package host

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeCompositor(t *testing.T, serve func(net.Conn)) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wayland-test")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	t.Setenv("WAYLAND_DISPLAY", path)
	// The inherited-fd override takes precedence over WAYLAND_DISPLAY.
	t.Setenv("WAYLAND_SOCKET", "")
	if err := os.Unsetenv("WAYLAND_SOCKET"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		connection.SetDeadline(time.Now().Add(2 * time.Second))
		serve(connection)
	}()
	t.Cleanup(func() {
		listener.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("fake compositor did not stop")
		}
	})
}

func TestOpenTimesOutWhenCompositorAcceptsButDoesNotRespond(t *testing.T) {
	fakeCompositor(t, func(conn net.Conn) { io.Copy(io.Discard, conn) })
	start := time.Now()
	window, err := open("timeout test", 640, 480, false, 100)
	if window != nil {
		window.Close()
		t.Fatal("opened without a compositor response")
	}
	if err == nil || !strings.Contains(err.Error(), "registry discovery") || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected discovery timeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("discovery exceeded its bounded timeout: %v", elapsed)
	}
}

func TestOpenRejectsCompositorWithoutBufferScaleVersion(t *testing.T) {
	fakeCompositor(t, func(conn net.Conn) {
		respondToDiscovery(conn, 2)
		io.Copy(io.Discard, conn)
	})
	window, err := open("old compositor", 640, 480, false, 500)
	if window != nil {
		window.Close()
		t.Fatal("accepted unsupported compositor version")
	}
	if err == nil || !strings.Contains(err.Error(), "wl_compositor v3+") {
		t.Fatalf("expected explicit version error, got %v", err)
	}
}

func TestOpenTimesOutWhenCompositorNeverConfiguresSurface(t *testing.T) {
	fakeCompositor(t, func(conn net.Conn) { respondToDiscovery(conn, 4); io.Copy(io.Discard, conn) })
	start := time.Now()
	window, err := open("configure timeout", 640, 480, false, 100)
	if window != nil {
		window.Close()
		t.Fatal("opened without surface configure")
	}
	if err == nil || !strings.Contains(err.Error(), "initial surface configure") || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected configure timeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("configure exceeded startup deadline: %v", elapsed)
	}
}

func respondToDiscovery(conn net.Conn, compositorVersion uint32) {
	var registry, callback uint32
	for callback == 0 {
		var header [8]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return
		}
		object, word := binary.NativeEndian.Uint32(header[:4]), binary.NativeEndian.Uint32(header[4:])
		if word>>16 < 8 {
			return
		}
		payload := make([]byte, int(word>>16)-8)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}
		if object == 1 && len(payload) >= 4 {
			if word&0xffff == 1 {
				registry = binary.NativeEndian.Uint32(payload)
			}
			if word&0xffff == 0 {
				callback = binary.NativeEndian.Uint32(payload)
			}
		}
	}
	writeGlobal(conn, registry, 1, "wl_compositor", compositorVersion)
	writeGlobal(conn, registry, 2, "xdg_wm_base", 1)
	writeMessage(conn, callback, 0, []uint32{1})
}

func writeMessage(conn net.Conn, object, opcode uint32, payload []uint32) {
	data := make([]byte, 8+len(payload)*4)
	binary.NativeEndian.PutUint32(data, object)
	binary.NativeEndian.PutUint32(data[4:], uint32(len(data))<<16|opcode)
	for i, value := range payload {
		binary.NativeEndian.PutUint32(data[8+i*4:], value)
	}
	conn.Write(data)
}

func writeGlobal(conn net.Conn, registry, name uint32, iface string, version uint32) {
	text := append([]byte(iface), 0)
	padded := (len(text) + 3) &^ 3
	payload := make([]uint32, 3+padded/4)
	payload[0], payload[1] = name, uint32(len(text))
	bytes := make([]byte, padded)
	copy(bytes, text)
	for i := 0; i < padded/4; i++ {
		payload[2+i] = binary.NativeEndian.Uint32(bytes[i*4:])
	}
	payload[len(payload)-1] = version
	writeMessage(conn, registry, 0, payload)
}

func TestClosedWindowCallsAreSafe(t *testing.T) {
	for _, window := range []*Window{nil, {}} {
		window.Close()
		window.Close()
		if display, surface := window.Handles(); display != nil || surface != nil {
			t.Fatal("closed window returned handles")
		}
		if w, h := window.Size(); w != 0 || h != 0 {
			t.Fatal("closed window returned nonzero extent")
		}
		if events, err := window.Poll([]Event{{Kind: Move}}); len(events) != 0 || !errors.Is(err, ErrClosed) {
			t.Fatalf("closed poll=%v,%v", events, err)
		}
	}
}

//go:build linux

package app

import (
	"errors"
	"io"
	"os"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

type clipboardFake struct {
	current      clipboardOffer
	published    []clipboardOffer
	queued       []clipboardRequest
	received     []clipboardRequest
	publishError error
	receiveError error
	contents     string
}

func (c *clipboardFake) offer() clipboardOffer { return c.current }
func (c *clipboardFake) publish(id uint64, mimes []string) error {
	if c.publishError != nil {
		return c.publishError
	}
	c.published = append(c.published, clipboardOffer{id: id, mimes: append([]string(nil), mimes...)})
	c.current.revision++
	c.current.id, c.current.external = id, id
	c.current.mimes = append([]string(nil), mimes...)
	return nil
}
func (c *clipboardFake) receive(id uint64, mime string, fd int) error {
	c.received = append(c.received, clipboardRequest{source: id, mime: mime, fd: fd})
	if c.receiveError != nil {
		return c.receiveError
	}
	_, err := unix.Write(fd, []byte(c.contents))
	return err
}
func (c *clipboardFake) requests() []clipboardRequest {
	requests := c.queued
	c.queued = nil
	return requests
}

func TestClipboardBrokerOwnershipAndFocus(t *testing.T) {
	h := &clipboardFake{current: clipboardOffer{revision: 1, id: 10, mimes: []string{"text/plain"}, available: true}}
	a := &clipboardFake{current: clipboardOffer{available: true}}
	b := newClipboardBroker(h, a)
	b.sync()
	b.sync()
	if len(a.published) != 1 || a.published[0].id == 0 || len(h.published) != 0 {
		t.Fatal("desktop offer did not arrive once, or its echo replaced desktop ownership")
	}
	// A new app copy waits for a usable desktop input serial. Metadata is
	// retained, but no content is pulled while the source is merely advertised.
	a.current = clipboardOffer{revision: a.current.revision + 1, id: 20, mimes: []string{"text/plain"}, available: true}
	h.publishError = errors.New("no input serial")
	b.sync()
	if b.pending[0] == nil || len(h.received)+len(a.received) != 0 {
		t.Fatal("deferred offer lost, or contents eagerly requested")
	}
	h.current.available = false
	h.current.revision++
	h.current.id, h.current.external, h.current.mimes = 0, 0, nil
	b.sync()
	if len(a.published) != 1 || b.pending[0] == nil {
		t.Fatal("desktop focus loss cleared the application's clipboard")
	}
	h.publishError = nil
	b.sync()
	b.sync()
	if len(h.published) != 1 || b.owner != 1 || b.offerID != 20 || b.pending[0] != nil || len(a.published) != 1 {
		t.Fatal("application offer was not retried exactly once or echo was republished")
	}
	// A new host selection supersedes a pending application copy.
	a.current = clipboardOffer{revision: a.current.revision + 1, id: 30, mimes: []string{"text/plain"}, available: true}
	h.publishError = errors.New("unfocused")
	b.sync()
	h.current = clipboardOffer{revision: h.current.revision + 1, id: 40, mimes: []string{"text/html", "text/plain"}, available: true}
	b.sync()
	if b.pending[0] != nil || len(a.published) != 2 || b.owner != 0 || b.offerID != 40 || !reflect.DeepEqual(a.published[1].mimes, h.current.mimes) {
		t.Fatal("new desktop copy did not supersede pending application ownership")
	}
}

func TestClipboardBrokerSimultaneousCopiesFavorApplication(t *testing.T) {
	h := &clipboardFake{current: clipboardOffer{revision: 1, id: 10, mimes: []string{"text/plain"}, available: true}}
	a := &clipboardFake{current: clipboardOffer{revision: 1, id: 20, mimes: []string{"text/plain"}, available: true}}
	b := newClipboardBroker(h, a)
	b.sync()
	b.sync()
	if len(a.published) != 0 || len(h.published) != 1 || b.owner != 1 || b.offerID != 20 {
		t.Fatal("simultaneous offers erased the application's new selection")
	}
}

func TestClipboardBrokerRelaysBothDirectionsAndClosesDescriptors(t *testing.T) {
	for _, direction := range []string{"desktop-to-app", "app-to-desktop"} {
		for _, stale := range []bool{false, true} {
			t.Run(direction+map[bool]string{false: "/current", true: "/stale"}[stale], func(t *testing.T) {
				h, a := &clipboardFake{current: clipboardOffer{available: true}}, &clipboardFake{}
				b := newClipboardBroker(h, a)
				source, consumer := h, a
				if direction == "app-to-desktop" {
					source, consumer = a, h
				}
				source.contents = "worldr clipboard canary"
				source.current = clipboardOffer{revision: 1, id: 71, mimes: []string{"text/plain"}, available: true}
				b.sync()
				if stale {
					source.receiveError = errors.New("stale offer")
				}
				r, w, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				// Duplicate for the relay to own; keep os.File ownership separate.
				fd, err := unix.Dup(int(w.Fd()))
				w.Close()
				if err != nil {
					t.Fatal(err)
				}
				consumer.queued = []clipboardRequest{{source: b.token, mime: "text/plain", fd: fd}}
				b.sync()
				if len(source.received) != 1 || source.received[0].source != 71 || source.received[0].mime != "text/plain" {
					t.Fatal("request lost its original source or MIME")
				}
				if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
					t.Fatal("bridge retained the transferred file descriptor")
				}
				data, err := io.ReadAll(r)
				if err != nil {
					t.Fatal(err)
				}
				want := source.contents
				if stale {
					want = ""
				}
				if string(data) != want {
					t.Fatalf("got %q, want %q", data, want)
				}
			})
		}
	}
}

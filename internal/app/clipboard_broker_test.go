//go:build linux

package app

import (
	"errors"
	"io"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func TestClipboardBrokerThreeProvidersRetainOriginalSource(t *testing.T) {
	h := &clipboardFake{current: clipboardOffer{available: true}}
	a := &clipboardFake{current: clipboardOffer{available: true}}
	n := &clipboardFake{current: clipboardOffer{available: true}}
	b := newClipboardBroker(h, a, n)
	providers := []*clipboardFake{h, a, n}
	for i, source := range providers {
		source.contents = "clipboard source canary"
		source.current = clipboardOffer{revision: source.current.revision + 1, id: uint64(100 + i), mimes: []string{"text/plain"}, available: true}
		b.sync()
		b.sync()
		for j, destination := range providers {
			if i == j {
				continue
			}
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			fd, err := unix.Dup(int(w.Fd()))
			w.Close()
			if err != nil {
				r.Close()
				t.Fatal(err)
			}
			destination.queued = []clipboardRequest{{source: destination.current.external, mime: "text/plain", fd: fd}}
			b.sync()
			data, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != source.contents || source.received[len(source.received)-1].source != uint64(100+i) {
				t.Fatal("clipboard transfer lost original provider identity")
			}
		}
	}
}

func TestClipboardBrokerPendingPublicationAndClearDoNotLoop(t *testing.T) {
	h := &clipboardFake{current: clipboardOffer{available: true}, publishError: errors.New("no serial")}
	a := &clipboardFake{current: clipboardOffer{available: true}}
	n := &clipboardFake{current: clipboardOffer{revision: 1, id: 10, mimes: []string{"text/plain"}, available: true}}
	b := newClipboardBroker(h, a, n)
	b.sync()
	if b.pending[0] == nil || len(a.published) != 1 {
		t.Fatal("unavailable desktop blocked other endpoints")
	}
	h.publishError = nil
	b.sync()
	b.sync()
	if len(h.published) != 1 || len(a.published) != 1 || len(n.published) != 0 {
		t.Fatal("retries or echoes looped")
	}
	n.current = clipboardOffer{revision: n.current.revision + 1, available: true}
	b.sync()
	b.sync()
	b.sync()
	if len(h.published) != 2 || len(a.published) != 2 || len(n.published) != 0 || b.token != 0 {
		t.Fatal("cleared source caused an ownership echo loop")
	}
}

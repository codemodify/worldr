//go:build linux

package app

import (
	"io"
	"os"
	"testing"
	"time"
)

type clipboardClient struct {
	copy              string
	copied, requested bool
	pasted            string
}

func (c *clipboardClient) TakeCopy() (string, bool) {
	text, ok := c.copy, c.copied
	c.copied = false
	return text, ok
}
func (c *clipboardClient) TakePasteRequest() bool {
	value := c.requested
	c.requested = false
	return value
}
func (c *clipboardClient) Paste(text string) error { c.pasted = text; return nil }

func TestNativeClipboardExportsAndImportsOnExplicitRequest(t *testing.T) {
	client := &clipboardClient{copy: "native selection λ", copied: true}
	n := newNativeClipboard(client)
	defer n.close()
	external := &clipboardFake{current: clipboardOffer{available: true}}
	b := newClipboardBroker(external, n)
	b.sync()
	if len(external.received) != 0 || client.pasted != "" {
		t.Fatal("selection advertisement read contents")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	fd, err := duplicateClipboardFD(int(w.Fd()))
	w.Close()
	if err != nil {
		t.Fatal(err)
	}
	external.queued = []clipboardRequest{{source: external.current.external, mime: "text/plain", fd: fd}}
	b.sync()
	if err := r.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil || string(data) != client.copy {
		t.Fatalf("native export got %q: %v", data, err)
	}
	external.contents = "external selection α"
	external.current = clipboardOffer{revision: external.current.revision + 1, id: 37, mimes: []string{"text/plain"}, available: true}
	b.sync()
	if client.pasted != "" || len(external.received) != 0 {
		t.Fatal("external offer eagerly pasted")
	}
	client.requested = true
	b.sync()
	deadline := time.Now().Add(time.Second)
	for client.pasted == "" && time.Now().Before(deadline) {
		b.sync()
		time.Sleep(time.Millisecond)
	}
	if client.pasted != external.contents || len(external.received) != 1 || external.received[0].source != 37 {
		t.Fatal("explicit paste lost data or original offer identity")
	}
}

func TestNativeClipboardCloseCancelsStalledPaste(t *testing.T) {
	client := &clipboardClient{requested: true}
	n := newNativeClipboard(client)
	n.publish(31, []string{"text/plain"})
	requests := n.requests()
	if len(requests) != 1 {
		t.Fatal("missing explicit paste request")
	}
	defer closeClipboardFD(requests[0].fd)
	start := time.Now()
	n.close()
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("clipboard shutdown waited for an unresponsive source")
	}
	n.close()
	if client.pasted != "" {
		t.Fatal("incomplete clipboard data entered terminal")
	}
}

package app

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type nativeClipboardClient interface {
	TakeCopy() (string, bool)
	TakePasteRequest() bool
	Paste(string) error
}

type copySource interface{ TakeCopy() (string, bool) }
type copyOnlyClient struct{ copySource }

func (copyOnlyClient) TakePasteRequest() bool { return false }
func (copyOnlyClient) Paste(string) error     { return nil }

// Copy-only native tools share the bounded clipboard transport without
// pretending to accept text input or acquiring a paste target lease.
func newCopyOnlyClipboard(source copySource) *nativeClipboard {
	return newNativeClipboard(copyOnlyClient{source})
}

const maxNativeClipboardBytes = 1 << 20

// nativeClipboard is the content-owning endpoint for explicit native terminal
// copy and paste. Remote data is bounded and read off the render goroutine;
// completed pastes enter the terminal only on the next owner-thread poll.
type nativeClipboard struct {
	client    nativeClipboardClient
	current   clipboardOffer
	text      string
	next      uint64
	pasting   bool
	completed chan string
	done      chan struct{}
	mu        sync.Mutex
	files     map[*os.File]bool
	closed    bool
	wg        sync.WaitGroup
}

func newNativeClipboard(client nativeClipboardClient) *nativeClipboard {
	return &nativeClipboard{client: client, current: clipboardOffer{available: true}, completed: make(chan string, 1), done: make(chan struct{}), files: make(map[*os.File]bool)}
}

func (c *nativeClipboard) offer() clipboardOffer {
	select {
	case text := <-c.completed:
		c.pasting = false
		_ = c.client.Paste(text)
	default:
	}
	if text, ok := c.client.TakeCopy(); ok {
		if len(text) > maxNativeClipboardBytes {
			text = text[:maxNativeClipboardBytes]
			for !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
		}
		c.next++
		c.text = text
		c.current = clipboardOffer{revision: c.current.revision + 1, id: c.next, mimes: []string{"text/plain;charset=utf-8", "text/plain"}, available: true}
	}
	return c.current
}

func (c *nativeClipboard) publish(token uint64, mimes []string) error {
	c.current = clipboardOffer{revision: c.current.revision + 1, id: token, external: token, mimes: append([]string(nil), mimes...), available: true}
	c.text = ""
	return nil
}

func textClipboardMIME(mime string) bool {
	return mime == "text/plain;charset=utf-8" || mime == "text/plain" || mime == "UTF8_STRING" || mime == "STRING"
}

func (c *nativeClipboard) receive(id uint64, mime string, fd int) error {
	if c.current.external != 0 || id != c.current.id || !textClipboardMIME(mime) {
		return fmt.Errorf("native clipboard source is stale")
	}
	owned, err := duplicateClipboardFD(fd)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(owned), "native clipboard send")
	if f == nil {
		closeClipboardFD(owned)
		return fmt.Errorf("invalid clipboard descriptor")
	}
	if err := f.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		f.Close()
		return err
	}
	text := c.text
	if !c.track(f) {
		f.Close()
		return fmt.Errorf("native clipboard is closed")
	}
	go func() { defer c.finish(f); _, _ = io.Copy(f, strings.NewReader(text)) }()
	return nil
}

func (c *nativeClipboard) requests() []clipboardRequest {
	if c.pasting || !c.client.TakePasteRequest() {
		return nil
	}
	// An empty completion releases a native provider's target lease when no
	// supported source can be requested. It never inserts terminal characters.
	requested := false
	defer func() {
		if !requested {
			_ = c.client.Paste("")
		}
	}()
	if c.current.id == 0 {
		return nil
	}
	if c.current.external == 0 {
		_ = c.client.Paste(c.text)
		requested = true
		return nil
	}
	mime := ""
	for _, candidate := range []string{"text/plain;charset=utf-8", "UTF8_STRING", "text/plain", "STRING"} {
		for _, offered := range c.current.mimes {
			if candidate == offered {
				mime = candidate
				break
			}
		}
		if mime != "" {
			break
		}
	}
	if mime == "" {
		return nil
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil
	}
	fd, err := duplicateClipboardFD(int(w.Fd()))
	w.Close()
	if err != nil {
		r.Close()
		return nil
	}
	if err := r.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		r.Close()
		closeClipboardFD(fd)
		return nil
	}
	if !c.track(r) {
		r.Close()
		closeClipboardFD(fd)
		return nil
	}
	c.pasting = true
	requested = true
	go func() {
		defer c.finish(r)
		data, err := io.ReadAll(io.LimitReader(r, maxNativeClipboardBytes+1))
		text := ""
		if err == nil && len(data) <= maxNativeClipboardBytes {
			text = strings.ToValidUTF8(string(data), "�")
		}
		select {
		case c.completed <- text:
		case <-c.done:
		}
	}()
	return []clipboardRequest{{source: c.current.external, mime: mime, fd: fd}}
}

func (c *nativeClipboard) track(f *os.File) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.files[f] = true
	c.wg.Add(1)
	return true
}
func (c *nativeClipboard) finish(f *os.File) {
	f.Close()
	c.mu.Lock()
	delete(c.files, f)
	c.mu.Unlock()
	c.wg.Done()
}
func (c *nativeClipboard) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.done)
	for f := range c.files {
		f.Close()
	}
	c.mu.Unlock()
	c.wg.Wait()
}

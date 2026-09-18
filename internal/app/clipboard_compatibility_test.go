//go:build linux && cgo

package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
)

type clipboardTransferCounter struct {
	clipboardEndpoint
	transfers int
}

func (c *clipboardTransferCounter) receive(id uint64, mime string, fd int) error {
	c.transfers++
	return c.clipboardEndpoint.receive(id, mime, fd)
}

func TestNativeClipboardWithRealFoot(t *testing.T) {
	if os.Getenv("WORLDR_TEST_APPS") != "1" {
		t.Skip("set WORLDR_TEST_APPS=1 for the real clipboard workflow")
	}
	canary := filepath.Join(t.TempDir(), "pasted")
	var output bytes.Buffer
	a, err := launchApplication(Options{Application: "foot", ApplicationArgs: []string{"--config=/dev/null", "/bin/sh", "-c", "IFS= read -r start; printf '\\033]52;c;c3BhdGlhbA==\\007'; IFS= read -r line; printf '%s' \"$line\" > \"$1\"; IFS= read -r done", "worldr-test", canary}}, &output)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	client := &clipboardClient{}
	n := newNativeClipboard(client)
	defer n.close()
	transfers := &clipboardTransferCounter{clipboardEndpoint: n}
	b := newClipboardBroker(applicationClipboard{a.server.(applicationClipboardServer)}, transfers)
	pollUntil := func(reason string, predicate func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if err := a.poll(); err != nil {
				t.Fatal(err)
			}
			b.sync()
			if predicate() {
				return
			}
			time.Sleep(3 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s: %s", reason, output.String())
	}
	pollUntil("foot map", func() bool { return len(a.Surfaces()) == 1 })
	id := a.Surfaces()[0].ID
	a.Focus(id)
	key := func(code, mods uint32, pressed bool) {
		a.Send(id, experience.Event{Kind: experience.KeyInput, Keycode: code, Depressed: mods, Pressed: pressed, Time: 1})
	}
	key(28, 0, true)
	key(28, 0, false)
	pollUntil("foot's native clipboard source", func() bool { return n.current.external != 0 })
	client.requested = true
	pollUntil("foot selection pasted into native app", func() bool { return client.pasted == "spatial" })
	client.copy, client.copied = "worldr native copy", true
	b.sync()
	// Dispatch source metadata before foot handles its paste chord.
	until := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(until) {
		if err := a.poll(); err != nil {
			t.Fatal(err)
		}
		b.sync()
		time.Sleep(time.Millisecond)
	}
	for _, e := range []struct {
		code, mods uint32
		down       bool
	}{{29, 4, true}, {42, 5, true}, {47, 5, true}, {47, 5, false}, {42, 4, false}, {29, 0, false}} {
		key(e.code, e.mods, e.down)
	}
	pollUntil("native copy requested by foot", func() bool { return transfers.transfers > 0 })
	// A consumer may finish within a single scheduling turn; wait through the
	// terminal's next dispatch before Enter completes the shell read.
	until = time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(until) {
		if err := a.poll(); err != nil {
			t.Fatal(err)
		}
		b.sync()
		time.Sleep(time.Millisecond)
	}
	key(28, 0, true)
	key(28, 0, false)
	pollUntil("native text reaches foot shell", func() bool { data, err := os.ReadFile(canary); return err == nil && string(data) == client.copy })
}

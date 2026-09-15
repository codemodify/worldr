package shell

import (
	"os"
	"strings"
	"testing"
)

func TestTryTTYScriptDocumentsF3Flow(t *testing.T) {
	b, err := os.ReadFile("../../scripts/try-tty.sh")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"Ctrl+Alt+F3", "Ctrl+Alt+F1", "DURATION", "--duration", "WAYLAND_DISPLAY", "vk-display", "CARD", "pkill"} {
		if !strings.Contains(s, want) {
			t.Fatalf("scripts/try-tty.sh missing %q", want)
		}
	}
}

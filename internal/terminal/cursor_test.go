//go:build linux && cgo

package terminal

import (
	"fmt"
	"testing"
)

func TestApplicationCursorStyleBlinkVisibilityAndReset(t *testing.T) {
	// A real child emits DECSCUSR and waits for input between changes, so each
	// snapshot observes the application's requested state without timing sleeps.
	script := `stty -echo
for style in 0 1 2 3 4 5 6; do
    printf '\033[%s q\033]2;cursor-%s\007' "$style" "$style"
    IFS= read -r next
done
printf '\033[?12h\033]2;blink-on\007'; IFS= read -r next
printf '\033[?12l\033]2;blink-off\007'; IFS= read -r next
printf '\033[?25l\033]2;hidden\007'; IFS= read -r next
printf '\033[?25h\033]2;shown\007'; IFS= read -r next
printf '\033[4 q\033c\033]2;reset\007'; IFS= read -r next
`
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", script}, Cols: 20, Rows: 4})
	var initial Snapshot
	for style := 0; style <= 6; style++ {
		title := fmt.Sprintf("cursor-%d", style)
		snapshot := pollTerminal(t, term, func(s Snapshot) bool { return s.Title == title })
		shape := 1
		if style >= 3 {
			shape = (style + 1) / 2
		}
		blink := style == 0 || style%2 == 1
		if !snapshot.Cursor.Visible || snapshot.Cursor.Shape != shape || snapshot.Cursor.Blink != blink {
			t.Fatalf("DECSCUSR %d produced %+v; want visible shape=%d blink=%v", style, snapshot.Cursor, shape, blink)
		}
		if style == 0 {
			initial = snapshot
		}
		key(t, term, 28, 0)
	}
	for _, want := range []struct {
		title   string
		shape   int
		blink   bool
		visible bool
	}{
		{"blink-on", 3, true, true},
		{"blink-off", 3, false, true},
		{"hidden", 3, false, false},
		{"shown", 3, false, true},
		{"reset", 1, true, true},
	} {
		snapshot := pollTerminal(t, term, func(s Snapshot) bool { return s.Title == want.title })
		if snapshot.Cursor.Shape != want.shape || snapshot.Cursor.Blink != want.blink || snapshot.Cursor.Visible != want.visible {
			t.Fatalf("%s produced %+v; want shape=%d blink=%v visible=%v", want.title, snapshot.Cursor, want.shape, want.blink, want.visible)
		}
		key(t, term, 28, 0)
	}
	if initial.Cursor.Shape != 1 || !initial.Cursor.Blink || !initial.Cursor.Visible {
		t.Fatal("later cursor changes mutated an earlier snapshot")
	}
	pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
}

//go:build linux && cgo

package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/experience"
	"golang.org/x/sys/unix"
)

func openTestTerminal(t *testing.T, options Options) *Terminal {
	t.Helper()
	term, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := term.Close(); err != nil {
			t.Error(err)
		}
	})
	term.Focus(true)
	return term
}

func pollTerminal(t *testing.T, term *Terminal, ready func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last Snapshot
	for time.Now().Before(deadline) {
		var err error
		last, err = term.Poll()
		if err != nil {
			t.Fatal(err)
		}
		if ready(last) {
			return last
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("terminal did not reach expected state; exited=%v error=%q screen=%q", last.Exited, last.ExitError, screenText(last))
	return Snapshot{}
}

func screenText(snapshot Snapshot) string {
	var out strings.Builder
	for row := 0; row < snapshot.Rows; row++ {
		for col := 0; col < snapshot.Cols; col++ {
			cell := snapshot.Cells[row*snapshot.Cols+col]
			if cell.Width == 0 {
				continue
			}
			if cell.Chars[0] == 0 {
				out.WriteByte(' ')
				continue
			}
			for _, r := range cell.Chars {
				if r != 0 {
					out.WriteRune(r)
				}
			}
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func key(t *testing.T, term *Terminal, code, depressed uint32) {
	t.Helper()
	for _, pressed := range []bool{true, false} {
		if err := term.Input(experience.Event{Kind: experience.KeyInput, Keycode: code, Depressed: depressed, Pressed: pressed}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestShellCellsInputResizeAndExit(t *testing.T) {
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "printf '\033]2;native shell\007\033[1;31mRED\033[0m\r\n'; stty size; IFS= read -r text; printf 'INPUT:%s\r\n' \"$text\"; stty size"}, Cols: 40, Rows: 8})
	first := pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "8 40") })
	if first.Title != "native shell" || !first.Cells[0].Bold || first.Cells[0].Foreground.R <= first.Cells[0].Foreground.G {
		t.Fatalf("lost title or styled cells: %+v %+v", first.Title, first.Cells[0])
	}
	old := screenText(first)
	if err := term.Resize(60, 12); err != nil {
		t.Fatal(err)
	}
	if err := term.Paste("typed"); err != nil {
		t.Fatal(err)
	}
	key(t, term, 28, 0)
	last := pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
	if last.Cols != 60 || last.Rows != 12 || !strings.Contains(screenText(last), "INPUT:typed") || !strings.Contains(screenText(last), "12 60") || last.ExitError != "" {
		t.Fatalf("shell/resize failed: %+v screen=%q", last, screenText(last))
	}
	if screenText(first) != old {
		t.Fatal("later polling mutated an earlier snapshot")
	}
	if err := term.Paste("after exit"); err != nil {
		t.Fatal("normal process exit made input fatal", err)
	}
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
	if err := term.Close(); err != nil {
		t.Fatal("second close failed", err)
	}
}

func TestExplicitChildEnvironmentIsCopiedAndArgumentsStayLiteral(t *testing.T) {
	t.Setenv("DISPLAY", ":outside-test")
	base := []string{"WAYLAND_DISPLAY=/private/worldr-test", "TERM=wrong", "COLORTERM=wrong", "PRIVATE=value"}
	before := append([]string(nil), base...)
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", `printf '<%s>|<%s>|<%s>|<%s>|<%s>|<%s>\n' "$WAYLAND_DISPLAY" "$DISPLAY" "$TERM" "$COLORTERM" "$PRIVATE" "$1"`, "fixture", "$literal"}, Env: base, Cols: 120, Rows: 4})
	last := pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
	if !strings.Contains(screenText(last), "</private/worldr-test>|<>|<xterm-256color>|<truecolor>|<value>|<$literal>") {
		t.Fatalf("child environment or arguments changed: %q", screenText(last))
	}
	if !reflect.DeepEqual(base, before) {
		t.Fatal("terminal mutated caller-owned environment")
	}
	t.Setenv("WORLDR_TERMINAL_ENV_TEST", "inherited")
	inherited := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", `printf '%s\n' "$WORLDR_TERMINAL_ENV_TEST"`}, Cols: 40, Rows: 4})
	last = pollTerminal(t, inherited, func(s Snapshot) bool { return s.Exited })
	if !strings.Contains(screenText(last), "inherited") {
		t.Fatal("nil environment stopped inheriting parent values")
	}
}

func TestUnicodeCellsAndBoundedScrollback(t *testing.T) {
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "printf '界é\r\n'; IFS= read -r text; i=0; while [ $i -lt 20 ]; do printf 'line%02d\r\n' \"$i\"; i=$((i+1)); done; IFS= read -r text"}, Cols: 20, Rows: 4, Scrollback: 5})
	first := pollTerminal(t, term, func(s Snapshot) bool { return len(s.Cells) > 2 && s.Cells[0].Chars[0] == '界' })
	if first.Cells[0].Width != 2 || first.Cells[1].Width != 0 || first.Cells[2].Chars[0] != 'e' || first.Cells[2].Chars[1] != '́' {
		t.Fatalf("wide/combining cells lost: %+v", first.Cells[:4])
	}
	key(t, term, 28, 0)
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "line19") })
	term.Scroll(100)
	scrolled := pollTerminal(t, term, func(s Snapshot) bool { return s.ScrollOffset > 0 })
	if scrolled.ScrollbackLen != 5 || scrolled.ScrollOffset != 5 || scrolled.Cursor.Visible {
		t.Fatalf("scrollback cap or cursor wrong: %+v", scrolled)
	}
	term.Scroll(-100)
	pollTerminal(t, term, func(s Snapshot) bool { return s.ScrollOffset == 0 && strings.Contains(screenText(s), "line19") })
}

func TestBracketedPasteReachesPTYUnmodified(t *testing.T) {
	// Raw input avoids line discipline rewriting the bytes under test. libvterm
	// must emit both delimiters after the child enables bracketed paste.
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "stty raw -echo; printf '\033[?2004hPASTE_READY'; dd bs=1 count=15 2>/dev/null | od -An -tx1"}, Cols: 80, Rows: 8})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "PASTE_READY") })
	if err := term.Paste("abc"); err != nil {
		t.Fatal(err)
	}
	last := pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
	if !strings.Contains(screenText(last), "1b 5b 32 30 30 7e 61 62 63 1b 5b 32 30 31 7e") {
		t.Fatalf("bracketed paste bytes missing: %q", screenText(last))
	}
}

func TestRepeatStopsOnReleaseAndFocusLoss(t *testing.T) {
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "stty raw -echo; printf READY; dd bs=1 count=6 2>/dev/null | od -An -tx1"}, Cols: 80, Rows: 8})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "READY") })
	now := time.Unix(100, 0)
	term.now = func() time.Time { return now }
	if err := term.Input(experience.Event{Kind: experience.KeyboardRepeatInfo, RepeatRate: 20, RepeatDelay: 100}); err != nil {
		t.Fatal(err)
	}
	if err := term.Input(experience.Event{Kind: experience.KeyInput, Keycode: 30, Pressed: true}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(200 * time.Millisecond)
	if _, err := term.Poll(); err != nil {
		t.Fatal(err)
	}
	if term.repeatCode != 30 {
		t.Fatal("repeat was not armed")
	}
	if err := term.Input(experience.Event{Kind: experience.KeyInput, Keycode: 30}); err != nil {
		t.Fatal(err)
	}
	if term.repeatCode != 0 {
		t.Fatal("key release did not stop repeat")
	}
	if err := term.Input(experience.Event{Kind: experience.KeyInput, Keycode: 48, Pressed: true}); err != nil {
		t.Fatal(err)
	}
	term.Focus(false)
	if term.repeatCode != 0 {
		t.Fatal("focus loss did not stop repeat")
	}
	now = now.Add(time.Second)
	if err := term.Input(experience.Event{Kind: experience.KeyInput, Keycode: 46, Pressed: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := term.Poll(); err != nil {
		t.Fatal(err)
	}
	if term.repeatCode != 0 {
		t.Fatal("unfocused terminal accepted keyboard input")
	}
	term.Focus(true)
	key(t, term, 28, 0)
	last := pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
	if !strings.Contains(screenText(last), "61 61 61 61 62 0d") {
		t.Fatalf("repeat/release/focus produced wrong real PTY bytes: %q", screenText(last))
	}
}

func TestXKBComposeAndMouseReports(t *testing.T) {
	t.Run("compose", func(t *testing.T) {
		compose := filepath.Join(t.TempDir(), "Compose")
		if err := os.WriteFile(compose, []byte("<dead_acute> <e> : \"é\" eacute\n"), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("XCOMPOSEFILE", compose)
		term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "stty raw -echo; printf READY; dd bs=1 count=2 2>/dev/null | od -An -tx1"}, Cols: 80, Rows: 8})
		pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "READY") })
		keymap := "xkb_keymap { xkb_keycodes { include \"evdev+aliases(qwerty)\" }; xkb_types { include \"complete\" }; xkb_compatibility { include \"complete\" }; xkb_symbols { include \"pc+us(intl)+inet(evdev)\" }; };"
		if err := term.Input(experience.Event{Kind: experience.KeymapChanged, Keymap: keymap}); err != nil {
			t.Fatal(err)
		}
		key(t, term, 40, 0) // dead acute in us(intl)
		key(t, term, 18, 0) // e
		last := pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
		if !strings.Contains(screenText(last), "c3 a9") {
			t.Fatalf("compose did not produce UTF-8 é: %q", screenText(last))
		}
	})
	t.Run("mouse", func(t *testing.T) {
		term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "stty raw -echo; printf '\033[?1000h\033[?1006hREADY'; dd bs=1 count=18 2>/dev/null | od -An -tx1"}, Cols: 100, Rows: 8})
		pollTerminal(t, term, func(s Snapshot) bool { return s.MouseTracking && strings.Contains(screenText(s), "READY") })
		for _, kind := range []experience.EventKind{experience.PointerDown, experience.PointerUp} {
			if err := term.Input(experience.Event{Kind: kind, Button: experience.ButtonPrimary, X: 2, Y: 1}); err != nil {
				t.Fatal(err)
			}
		}
		last := pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
		text := strings.Join(strings.Fields(strings.TrimPrefix(screenText(last), "READY")), " ")
		if text != "1b 5b 3c 30 3b 33 3b 32 4d 1b 5b 3c 30 3b 33 3b 32 6d" {
			t.Fatalf("SGR mouse report lost cell coordinates or release: %q", text)
		}
	})
}

func TestResizePreservesNonblockingPollForSilentChild(t *testing.T) {
	term := openTestTerminal(t, Options{Command: "/bin/sleep", Args: []string{"20"}, Cols: 40, Rows: 8})
	for _, size := range [][2]int{{60, 12}, {80, 24}, {40, 8}} {
		if err := term.Resize(size[0], size[1]); err != nil {
			t.Fatal(err)
		}
		flags, err := unix.FcntlInt(uintptr(term.fd), unix.F_GETFL, 0)
		if err != nil || flags&unix.O_NONBLOCK == 0 {
			t.Fatalf("resize changed PTY to blocking mode: flags=%x error=%v", flags, err)
		}
		start := time.Now()
		if _, err := term.Poll(); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
			t.Fatalf("silent terminal blocked render polling: %v", elapsed)
		}
	}
}

func TestInteractiveShellForegroundJobReceivesControlC(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	term := openTestTerminal(t, Options{Command: bash, Args: []string{"--noprofile", "--norc", "-i"}, Cols: 80, Rows: 12})
	if err := term.Paste("sleep 30"); err != nil {
		t.Fatal(err)
	}
	key(t, term, 28, 0)
	pollTerminal(t, term, func(s Snapshot) bool {
		group, err := unix.IoctlGetInt(term.fd, unix.TIOCGPGRP)
		return err == nil && group != term.cmd.Process.Pid
	})
	key(t, term, 46, 4) // Ctrl+C, using the US XKB Control mask.
	pollTerminal(t, term, func(s Snapshot) bool {
		group, err := unix.IoctlGetInt(term.fd, unix.TIOCGPGRP)
		return err == nil && group == term.cmd.Process.Pid
	})
	// Readline may enable bracketed paste before or after the foreground-group
	// observation. Send Enter as a key, outside either form of pasted content.
	if err := term.Paste("printf '\\nJOB_OK\\n'"); err != nil {
		t.Fatal(err)
	}
	key(t, term, 28, 0)
	pollTerminal(t, term, func(s Snapshot) bool {
		for _, line := range strings.Split(screenText(s), "\n") {
			if strings.TrimSpace(line) == "JOB_OK" {
				return true
			}
		}
		return false
	})
}

func TestVimEditsFileAndRestoresAlternateScreen(t *testing.T) {
	vim, err := exec.LookPath("vim")
	if err != nil {
		t.Skip("vim unavailable")
	}
	path := filepath.Join(t.TempDir(), "native.txt")
	if err := os.WriteFile(path, []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	term := openTestTerminal(t, Options{Command: vim, Args: []string{"-Nu", "NONE", "-n", "-i", "NONE", "-N", path}, Cols: 80, Rows: 24})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "original") })
	key(t, term, 23, 0) // i
	if err := term.Paste("worldr "); err != nil {
		t.Fatal(err)
	}
	key(t, term, 1, 0)  // Escape
	key(t, term, 39, 1) // :
	key(t, term, 17, 0) // w
	key(t, term, 16, 0) // q
	key(t, term, 28, 0)
	last := pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
	if last.ExitError != "" {
		t.Fatal(last.ExitError)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "worldr original\n" {
		t.Fatalf("Vim did not edit through real PTY: %q", data)
	}
	if strings.Contains(screenText(last), "~") {
		t.Fatal("Vim alternate screen did not restore")
	}
}

func TestTerminalRejectsInvalidGeometryAndBoundsClose(t *testing.T) {
	for _, options := range []Options{{Cols: 1, Rows: 20}, {Cols: 80, Rows: 257}, {Cols: 513, Rows: 20}, {Scrollback: 10001}} {
		if term, err := Open(options); err == nil {
			term.Close()
			t.Fatal("accepted invalid options", options)
		}
	}
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", "trap '' HUP; printf READY; while :; do :; done"}, Cols: 20, Rows: 4})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "READY") })
	before := time.Now()
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(before); elapsed > time.Second {
		t.Fatal("close exceeded bounded escalation", elapsed)
	}
	if term.cmd.ProcessState == nil || !term.exited {
		t.Fatal("terminal process was not reaped")
	}
	if _, err := term.Poll(); err != ErrClosed {
		t.Fatal(fmt.Sprintf("closed poll error: %v", err))
	}
}

func TestExitedShellCleansSameGroupDescendantBeforeReaping(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORLDR_TERMINAL_FIXTURE", "parent")
	t.Setenv("WORLDR_TERMINAL_FIXTURE_DIR", dir)
	term := openTestTerminal(t, Options{Command: os.Args[0], Args: []string{"-test.run=^TestTerminalProcessFixture$"}, Cols: 80, Rows: 8})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "CHILD_READY") })
	data, err := os.ReadFile(filepath.Join(dir, "child"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	pidFD, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.PidfdSendSignal(pidFD, unix.SIGKILL, nil, 0); _ = unix.Close(pidFD) })
	if group, err := syscall.Getpgid(pid); err != nil || group != term.cmd.Process.Pid {
		t.Fatalf("fixture escaped owned process group: %d %v", group, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
	if err := term.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) {
			return
		}
		if err == nil {
			end := strings.LastIndexByte(string(stat), ')')
			if end >= 0 && len(stat) > end+2 && stat[end+2] == 'Z' {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("exited shell left its HUP/TERM-ignoring same-group descendant running")
}

func TestTerminalProcessFixture(t *testing.T) {
	mode := os.Getenv("WORLDR_TERMINAL_FIXTURE")
	if mode == "" {
		return
	}
	dir := os.Getenv("WORLDR_TERMINAL_FIXTURE_DIR")
	signal.Ignore(syscall.SIGHUP, syscall.SIGTERM)
	if mode == "child" {
		if err := os.WriteFile(filepath.Join(dir, "child"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	child := exec.Command(os.Args[0], "-test.run=^TestTerminalProcessFixture$")
	child.Env = append(os.Environ(), "WORLDR_TERMINAL_FIXTURE=child")
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "child")); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	fmt.Println("CHILD_READY")
	for {
		if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

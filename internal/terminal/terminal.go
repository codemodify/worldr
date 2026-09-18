//go:build linux && cgo

package terminal

/*
#cgo pkg-config: vterm xkbcommon
#include "terminal.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/codemodify/worldr/internal/experience"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

const inputLimit = 2 << 20

type Terminal struct {
	ptr                          *C.worldr_term
	master                       *os.File
	fd                           int
	cmd                          *exec.Cmd
	wait                         chan error
	closed, exited, eof, focused bool
	exitError                    string
	pending                      []byte
	cells                        []C.worldr_term_cell
	snapshot                     Snapshot
	revision                     uint64
	exitRevision                 bool
	repeatCode                   uint32
	repeatRate, repeatDelay      int32
	repeatNext                   time.Time
	now                          func() time.Time
	directory                    workingDirectory
}

func Open(options Options) (*Terminal, error) {
	if options.Cols == 0 {
		options.Cols = 100
	}
	if options.Rows == 0 {
		options.Rows = 30
	}
	if options.Scrollback == 0 {
		options.Scrollback = 2000
	}
	if !validSize(options.Cols, options.Rows) || options.Scrollback < 0 || options.Scrollback > 10000 {
		return nil, fmt.Errorf("terminal requires 2..512 columns, 2..256 rows and at most 10000 scrollback lines")
	}
	directory := ""
	if options.Directory != nil {
		info, err := options.Directory.Stat()
		if err != nil {
			return nil, fmt.Errorf("terminal directory: %w", err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("terminal directory must be an open directory")
		}
		// The parent descriptor remains valid through the child's chdir even
		// when the original path was renamed or replaced. It is not inherited
		// or duplicated into the child, and no process-wide chdir is needed.
		directory = fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), options.Directory.Fd())
	}
	if options.Command == "" {
		options.Command = os.Getenv("SHELL")
		if options.Command == "" {
			options.Command = "/bin/sh"
		}
	}
	ptr := C.worldr_term_new(C.int(options.Rows), C.int(options.Cols), C.int(options.Scrollback))
	if ptr == nil {
		return nil, fmt.Errorf("initialize libvterm or XKB")
	}
	cmd := exec.Command(options.Command, options.Args...)
	cmd.Dir = directory
	environment := options.Env
	if environment == nil {
		environment = os.Environ()
	}
	for _, value := range environment {
		key, _, _ := strings.Cut(value, "=")
		if key != "TERM" && key != "COLORTERM" && key != "COLUMNS" && key != "LINES" && (key != "PWD" || options.Directory == nil) {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "TERM=xterm-256color", "COLORTERM=truecolor")
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(options.Rows), Cols: uint16(options.Cols)})
	// Start waits until exec succeeds or reports an error, so the borrowed
	// descriptor is no longer needed once this synchronous call returns.
	runtime.KeepAlive(options.Directory)
	if err != nil {
		C.worldr_term_free(ptr)
		return nil, fmt.Errorf("start terminal: %w", err)
	}
	t := &Terminal{ptr: ptr, master: master, fd: int(master.Fd()), cmd: cmd, wait: make(chan error, 1), repeatRate: 25, repeatDelay: 600, now: time.Now}
	t.directory = openWorkingDirectory(options.Directory, cmd.Process.Pid)
	go func() { t.wait <- waitTerminalProcess(cmd) }()
	if err := unix.SetNonblock(t.fd, true); err != nil {
		t.Close()
		return nil, fmt.Errorf("nonblocking terminal: %w", err)
	}
	return t, nil
}

// Observe death before reaping so the owned process-group number cannot be
// recycled while cleaning up same-group children that retained the PTY. Once
// its shell exits this terminal no longer accepts input, so that owned group
// ends too. Other job-control groups receive the PTY's normal hangup behavior;
// deliberately detached sessions are outside this lifecycle contract.
func waitTerminalProcess(cmd *exec.Cmd) error {
	var info unix.Siginfo
	var observeErr error
	for {
		observeErr = unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if observeErr != unix.EINTR {
			break
		}
	}
	if observeErr == nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		time.Sleep(50 * time.Millisecond)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	err := cmd.Wait()
	if err == nil && observeErr != nil {
		return fmt.Errorf("observe terminal process: %w", observeErr)
	}
	return err
}

func validSize(cols, rows int) bool { return cols >= 2 && cols <= 512 && rows >= 2 && rows <= 256 }

// Poll does bounded nonblocking PTY work and coalesces screen damage. It never
// waits for the shell, and snapshots remain immutable after returning.
func (t *Terminal) Poll() (Snapshot, error) {
	if t == nil || t.closed {
		return Snapshot{}, ErrClosed
	}
	// Retain the last observed shell cwd while it is live. Once the shell has
	// exited, procfs no longer exposes its cwd even if its output stays visible.
	if !t.exited && !t.now().Before(t.directory.nextPoll) {
		_, _ = t.WorkingDirectory()
	}
	t.observeExit()
	var buffer [16384]byte
	for consumed := 0; consumed < 256<<10 && !t.eof; {
		n, err := unix.Read(t.fd, buffer[:])
		if n > 0 {
			consumed += n
			if C.worldr_term_feed(t.ptr, (*C.char)(unsafe.Pointer(&buffer[0])), C.size_t(n)) != 0 {
				return Snapshot{}, fmt.Errorf("libvterm did not consume terminal output")
			}
		}
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			break
		}
		if err == unix.EINTR {
			continue
		}
		if err == unix.EIO || n == 0 && err == nil {
			t.eof = true
			break
		}
		if err != nil {
			return Snapshot{}, fmt.Errorf("read terminal: %w", err)
		}
	}
	if t.focused && t.repeatCode != 0 && !t.exited && t.repeatRate > 0 {
		now := t.now()
		interval := time.Second / time.Duration(t.repeatRate)
		for i := 0; i < 16 && !now.Before(t.repeatNext); i++ {
			C.worldr_term_key(t.ptr, C.uint32_t(t.repeatCode))
			t.repeatNext = t.repeatNext.Add(interval)
		}
		if !now.Before(t.repeatNext) {
			t.repeatNext = now.Add(interval)
		}
	}
	if err := t.flushOutput(); err != nil {
		return Snapshot{}, err
	}
	t.observeExit()
	info := C.worldr_term_snapshot(t.ptr, nil)
	if t.snapshot.Revision != 0 && uint64(info.revision) == t.revision && !t.exitRevision {
		return t.snapshot, nil
	}
	t.revision = uint64(info.revision)
	t.exitRevision = false
	count := int(info.cols) * int(info.rows)
	if cap(t.cells) < count {
		t.cells = make([]C.worldr_term_cell, count)
	} else {
		t.cells = t.cells[:count]
	}
	C.worldr_term_snapshot(t.ptr, &t.cells[0])
	version := t.snapshot.Revision + 1
	snapshot := Snapshot{Cols: int(info.cols), Rows: int(info.rows), Cells: make([]Cell, count), Title: C.GoString(C.worldr_term_title(t.ptr)), Revision: version,
		Cursor: Cursor{Row: int(info.cursor_row), Col: int(info.cursor_col), Visible: info.cursor_visible != 0, Blink: info.cursor_blink != 0, Shape: int(info.cursor_shape)},
		Exited: t.exited, ExitError: t.exitError, ScrollOffset: int(info.scroll_offset), ScrollbackLen: int(info.scrollback_len), MouseTracking: info.mouse != 0, AlternateScreen: info.alternate != 0, FirstLine: uint64(info.first_line)}
	for i, c := range t.cells {
		cell := &snapshot.Cells[i]
		for j := range cell.Chars {
			cell.Chars[j] = rune(c.chars[j])
		}
		cell.Width = int(c.width)
		cell.Foreground = Color{uint8(c.fg[0]), uint8(c.fg[1]), uint8(c.fg[2])}
		cell.Background = Color{uint8(c.bg[0]), uint8(c.bg[1]), uint8(c.bg[2])}
		cell.Bold = c.bold != 0
		cell.Italic = c.italic != 0
		cell.Underline = c.underline != 0
		cell.Reverse = c.reverse != 0
		cell.Strike = c.strike != 0
	}
	t.snapshot = snapshot
	return snapshot, nil
}

func (t *Terminal) observeExit() {
	if t.exited {
		return
	}
	select {
	case err := <-t.wait:
		t.exited = true
		t.directory.close()
		t.repeatCode = 0
		t.exitRevision = true
		if err != nil {
			t.exitError = err.Error()
		}
	default:
	}
}

func (t *Terminal) flushOutput() error {
	if t.exited || t.eof {
		t.pending = t.pending[:0]
	}
	var buffer [32768]byte
	for {
		space := min(len(buffer), inputLimit-len(t.pending))
		if space == 0 {
			break
		}
		n := int(C.worldr_term_output(t.ptr, (*C.char)(unsafe.Pointer(&buffer[0])), C.size_t(space)))
		if n == 0 {
			break
		}
		if !t.exited && !t.eof {
			t.pending = append(t.pending, buffer[:n]...)
		}
	}
	if C.worldr_term_overflow(t.ptr) != 0 {
		return fmt.Errorf("terminal input queue exceeded its bounded capacity")
	}
	for len(t.pending) > 0 {
		n, err := unix.Write(t.fd, t.pending)
		if n > 0 {
			t.pending = t.pending[n:]
		}
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			break
		}
		if err == unix.EINTR {
			continue
		}
		if err == unix.EIO {
			t.pending = t.pending[:0]
			t.eof = true
			return nil
		}
		if err != nil {
			return fmt.Errorf("write terminal: %w", err)
		}
		if n == 0 {
			break
		}
	}
	return nil
}

func (t *Terminal) Resize(cols, rows int) error {
	if t == nil || t.closed {
		return ErrClosed
	}
	if !validSize(cols, rows) {
		return fmt.Errorf("terminal dimensions must be 2..512 columns and 2..256 rows")
	}
	if info := C.worldr_term_snapshot(t.ptr, nil); int(info.cols) == cols && int(info.rows) == rows {
		return nil
	}
	if !t.exited && !t.eof {
		// os.File.Fd switches its descriptor into blocking mode. Keep using the
		// saved raw descriptor: pty.Setsize calls Fd and would make Poll block.
		if err := unix.IoctlSetWinsize(t.fd, unix.TIOCSWINSZ, &unix.Winsize{Row: uint16(rows), Col: uint16(cols)}); err != nil {
			return fmt.Errorf("resize PTY: %w", err)
		}
	}
	C.worldr_term_resize(t.ptr, C.int(rows), C.int(cols))
	return nil
}

func (t *Terminal) Focus(focused bool) {
	if t == nil || t.closed || focused == t.focused {
		return
	}
	t.focused = focused
	t.repeatCode = 0
	if focused {
		C.worldr_term_focus(t.ptr, 1)
	} else {
		C.worldr_term_focus(t.ptr, 0)
	}
}

// Input accepts raw keyboard metadata regardless of focus. Pointer X/Y are
// cell coordinates. Native selection policy belongs to the presentation layer;
// application mouse reporting is encoded only when the terminal enabled it.
func (t *Terminal) Input(event experience.Event) error {
	if t == nil || t.closed {
		return ErrClosed
	}
	t.observeExit()
	if t.exited {
		return nil
	}
	switch event.Kind {
	case experience.KeymapChanged:
		if len(event.Keymap) == 0 || len(event.Keymap) > 1<<20 || strings.IndexByte(event.Keymap, 0) >= 0 {
			return fmt.Errorf("invalid terminal keymap")
		}
		text := C.CString(event.Keymap)
		defer C.free(unsafe.Pointer(text))
		if C.worldr_term_keymap(t.ptr, text) != 0 {
			return fmt.Errorf("cannot parse terminal XKB keymap")
		}
		t.repeatCode = 0
	case experience.KeyboardModifiers:
		C.worldr_term_modifiers(t.ptr, C.uint32_t(event.Depressed), C.uint32_t(event.Latched), C.uint32_t(event.Locked), C.uint32_t(event.Group))
	case experience.KeyboardRepeatInfo:
		t.repeatRate = max(0, min(event.RepeatRate, 1000))
		t.repeatDelay = max(0, event.RepeatDelay)
		if t.repeatRate == 0 {
			t.repeatCode = 0
		}
	case experience.KeyboardCancel:
		t.Focus(false)
		t.repeatCode = 0
		C.worldr_term_modifiers(t.ptr, 0, 0, 0, 0)
	case experience.PointerCancel:
		C.worldr_term_mouse_cancel(t.ptr)
	case experience.KeyInput:
		if !t.focused || event.Keycode == 0 || event.Keycode > 767 || event.Repeat {
			return nil
		}
		C.worldr_term_modifiers(t.ptr, C.uint32_t(event.Depressed), C.uint32_t(event.Latched), C.uint32_t(event.Locked), C.uint32_t(event.Group))
		if event.Pressed {
			C.worldr_term_scroll(t.ptr, -10000)
			C.worldr_term_key(t.ptr, C.uint32_t(event.Keycode))
			if t.repeatRate > 0 && C.worldr_term_repeats(t.ptr, C.uint32_t(event.Keycode)) != 0 {
				t.repeatCode = event.Keycode
				t.repeatNext = t.now().Add(time.Duration(t.repeatDelay) * time.Millisecond)
			}
		} else if t.repeatCode == event.Keycode {
			t.repeatCode = 0
		}
	case experience.PointerMove, experience.PointerDown, experience.PointerUp, experience.PointerScroll:
		info := C.worldr_term_snapshot(t.ptr, nil)
		if info.mouse == 0 {
			if event.Kind == experience.PointerScroll {
				t.Scroll(int(-event.ScrollY/10) * 3)
			}
			return nil
		}
		row, col := min(max(int(event.Y), 0), int(info.rows)-1), min(max(int(event.X), 0), int(info.cols)-1)
		mods := 0
		if event.Modifiers.Has(experience.ModShift) {
			mods |= 1
		}
		if event.Modifiers.Has(experience.ModAlt) {
			mods |= 2
		}
		if event.Modifiers.Has(experience.ModControl) {
			mods |= 4
		}
		button := 0
		switch event.ButtonCode {
		case 0x110:
			button = 1
		case 0x112:
			button = 2
		case 0x111:
			button = 3
		}
		if button == 0 {
			switch event.Button {
			case experience.ButtonPrimary:
				button = 1
			case experience.ButtonMiddle:
				button = 2
			case experience.ButtonSecondary:
				button = 3
			}
		}
		pressed := 0
		if event.Kind == experience.PointerDown {
			pressed = 1
		}
		if event.Kind == experience.PointerMove {
			button = 0
		}
		if event.Kind == experience.PointerScroll {
			button = 4
			if event.ScrollY > 0 {
				button = 5
			}
			steps := min(max(int(abs(event.ScrollY)/10), 1), 10)
			for i := 0; i < steps; i++ {
				C.worldr_term_mouse(t.ptr, C.int(row), C.int(col), C.int(button), 1, C.int(mods))
				C.worldr_term_mouse(t.ptr, C.int(row), C.int(col), C.int(button), 0, C.int(mods))
			}
		} else {
			C.worldr_term_mouse(t.ptr, C.int(row), C.int(col), C.int(button), C.int(pressed), C.int(mods))
		}
	}
	return t.flushOutput()
}

func abs(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}

func (t *Terminal) Paste(text string) error {
	if t == nil || t.closed {
		return ErrClosed
	}
	t.observeExit()
	if t.exited {
		return nil
	}
	if len(text) > 1<<20 {
		return fmt.Errorf("terminal paste exceeds 1 MiB")
	}
	if !t.focused || len(text) == 0 {
		return nil
	}
	// Normalize desktop newlines to terminal Enter without interpreting escapes.
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r")
	data := []byte(text)
	C.worldr_term_scroll(t.ptr, -10000)
	C.worldr_term_paste(t.ptr, (*C.char)(unsafe.Pointer(&data[0])), C.size_t(len(data)))
	return t.flushOutput()
}

// Scroll moves toward older history when positive, toward the live tail when
// negative. It does not inject mouse or keyboard input into the PTY.
func (t *Terminal) Scroll(lines int) {
	if t == nil || t.closed {
		return
	}
	lines = min(max(lines, -10000), 10000)
	C.worldr_term_scroll(t.ptr, C.int(lines))
}

func (t *Terminal) Close() error {
	if t == nil || t.closed {
		return nil
	}
	_, _ = t.WorkingDirectory()
	t.directory.close()
	t.closed = true
	t.repeatCode = 0
	if t.master != nil {
		_ = t.master.Close()
	}
	defer func() {
		if t.ptr != nil {
			C.worldr_term_free(t.ptr)
			t.ptr = nil
		}
	}()
	if t.exited {
		return nil
	}
	wait := func(delay time.Duration) bool {
		select {
		case <-t.wait:
			t.exited = true
			return true
		case <-time.After(delay):
			return false
		}
	}
	if wait(250 * time.Millisecond) {
		return nil
	}
	_ = t.cmd.Process.Signal(syscall.SIGHUP)
	if wait(100 * time.Millisecond) {
		return nil
	}
	err := t.cmd.Process.Kill()
	if wait(500 * time.Millisecond) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("stop terminal process: %w", err)
	}
	return fmt.Errorf("terminal process did not exit within shutdown deadline")
}

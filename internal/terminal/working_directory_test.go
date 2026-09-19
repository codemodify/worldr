//go:build linux && cgo

package terminal

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkingDirectoryTracksShellCDRenameAndExit(t *testing.T) {
	start := t.TempDir()
	next := filepath.Join(start, "next with spaces")
	if err := os.Mkdir(next, 0700); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open(start)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", `printf 'START_READY\n'; IFS= read -r first; cd -- "$1"; printf 'MOVED_READY\n'; IFS= read -r second`, "fixture", next}, Directory: directory, Cols: 80, Rows: 8})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "START_READY") })
	if got, err := term.WorkingDirectory(); err != nil || got != start {
		t.Fatalf("initial cwd = %q, %v; want %q", got, err, start)
	}
	key(t, term, 28, 0)
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "MOVED_READY") })
	if got, err := term.WorkingDirectory(); err != nil || got != next {
		t.Fatalf("shell cd was not captured: %q, %v", got, err)
	}
	renamed := filepath.Join(start, "renamed shell directory")
	if err := os.Rename(next, renamed); err != nil {
		t.Fatal(err)
	}
	if got, err := term.WorkingDirectory(); err != nil || got != renamed {
		t.Fatalf("cwd retained an obsolete display path after rename: %q, %v", got, err)
	}
	processDirectory := term.directory.process
	key(t, term, 28, 0)
	pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
	if got, err := term.WorkingDirectory(); err != nil || got != renamed {
		t.Fatalf("exited shell lost last known cwd: %q, %v", got, err)
	}
	if processDirectory == nil {
		t.Fatal("process identity was not pinned during its lifetime")
	}
	if _, err := processDirectory.Stat(); err == nil {
		t.Fatalf("exit retained the procfs process descriptor: %v", err)
	}
	term.Close()
	if _, err := term.WorkingDirectory(); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed terminal accepted cwd query: %v", err)
	}
}

func TestWorkingDirectoryPollCachesCDBeforeExit(t *testing.T) {
	start, next := t.TempDir(), t.TempDir()
	directory, err := os.Open(start)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	term := openTestTerminal(t, Options{Command: "/bin/sh", Args: []string{"-c", `printf 'START_READY\n'; IFS= read -r first; cd -- "$1"; printf 'MOVED_READY\n'; IFS= read -r second`, "fixture", next}, Directory: directory, Cols: 80, Rows: 8})
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "START_READY") })
	key(t, term, 28, 0)
	pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "MOVED_READY") })
	now := time.Now().Add(time.Second)
	term.now = func() time.Time { return now }
	if _, err := term.Poll(); err != nil {
		t.Fatal(err)
	}
	key(t, term, 28, 0)
	pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
	if got, err := term.WorkingDirectory(); err != nil || got != next {
		t.Fatalf("polling did not preserve the changed cwd through exit: %q, %v", got, err)
	}
}

func TestWorkingDirectoryFastExitAndRemovedDirectoryUseLaunchFallback(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "fast exit", true: "removed cwd"}[remove], func(t *testing.T) {
			start := t.TempDir()
			directory, err := os.Open(start)
			if err != nil {
				t.Fatal(err)
			}
			defer directory.Close()
			args := []string{"-c", "exit 0"}
			if remove {
				args = []string{"-c", "printf 'READY\\n'; IFS= read -r stop"}
			}
			term := openTestTerminal(t, Options{Command: "/bin/sh", Args: args, Directory: directory, Cols: 80, Rows: 4})
			if remove {
				pollTerminal(t, term, func(s Snapshot) bool { return strings.Contains(screenText(s), "READY") })
				if err := os.Remove(start); err != nil {
					t.Fatal(err)
				}
			} else {
				pollTerminal(t, term, func(s Snapshot) bool { return s.Exited })
			}
			if got, err := term.WorkingDirectory(); err != nil || got != start || strings.Contains(got, "/proc/") || strings.HasSuffix(got, " (deleted)") {
				t.Fatalf("unavailable cwd produced an unsafe restore path: %q, %v", got, err)
			}
			processDirectory := term.directory.process
			if err := term.Close(); err != nil {
				t.Fatal(err)
			}
			if processDirectory != nil {
				if _, err := processDirectory.Stat(); err == nil {
					t.Fatalf("close retained process descriptor: %v", err)
				}
			}
		})
	}
}

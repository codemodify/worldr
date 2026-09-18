//go:build linux && cgo

package terminal

import (
	"fmt"
	"os"
	"time"

	"github.com/codemodify/worldr/internal/resourcepath"
	"golang.org/x/sys/unix"
)

type workingDirectory struct {
	process  *os.File
	last     string
	nextPoll time.Time
}

func openWorkingDirectory(directory *os.File, pid int) workingDirectory {
	var state workingDirectory
	if directory == nil {
		if current, err := os.Open("."); err == nil {
			state.last, _ = resourcepath.FromFile(current)
			_ = current.Close()
		}
	} else {
		state.last, _ = resourcepath.FromFile(directory)
	}
	// Open before the wait goroutine can reap this child. Holding the procfs
	// directory pins this process identity; a later PID reuse cannot redirect
	// cwd lookups to another process. os.Open sets close-on-exec.
	state.process, _ = os.Open(fmt.Sprintf("/proc/%d", pid))
	return state
}

// WorkingDirectory reports the shell's physical cwd, including a subsequent
// cd or directory rename. Once that cwd is unavailable, retain the last known
// reopenable path (initially its launch directory). No child command is run.
// Like Poll, this method belongs to the host goroutine.
func (t *Terminal) WorkingDirectory() (string, error) {
	if t == nil || t.closed {
		return "", ErrClosed
	}
	t.directory.nextPoll = t.now().Add(250 * time.Millisecond)
	var err error
	if t.directory.process != nil {
		var fd int
		fd, err = unix.Openat(int(t.directory.process.Fd()), "cwd", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err == nil {
			current := os.NewFile(uintptr(fd), "terminal cwd")
			var path string
			path, err = resourcepath.FromFile(current)
			_ = current.Close()
			if err == nil {
				t.directory.last = path
				return path, nil
			}
		}
	}
	if t.directory.last != "" {
		return t.directory.last, nil
	}
	if err == nil {
		err = fmt.Errorf("shell working directory is unavailable")
	}
	return "", err
}

func (d *workingDirectory) close() {
	if d.process != nil {
		_ = d.process.Close()
		d.process = nil
	}
}

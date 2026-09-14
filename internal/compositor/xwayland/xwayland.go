// Package xwayland launches rootless Xwayland as a Wayland client of worldr
// and runs a tiny XWM so managed X11 windows become compositor actors.
//
// Surfaces use xwayland_shell_v1 (fallback: xdg_toplevel) on the existing
// SSD / focus / shm / dmabuf path.
package xwayland

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Instance is a running Xwayland child plus its tiny XWM.
type Instance struct {
	Cmd        *exec.Cmd
	Display    string // ":2"
	DisplayNum int
	WM         *XWM
	bin        string
	once       sync.Once
}

// LookPath finds the Xwayland binary (PATH or WORLDR_XWAYLAND).
func LookPath() (string, error) {
	if p := os.Getenv("WORLDR_XWAYLAND"); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("WORLDR_XWAYLAND=%s: %w", p, err)
		}
		return p, nil
	}
	p, err := exec.LookPath("Xwayland")
	if err != nil {
		return "", fmt.Errorf("Xwayland not in PATH (Arch: pacman -S xorg-xwayland): %w", err)
	}
	return p, nil
}

// ParseDisplayFD reads the decimal display number Xwayland writes to -displayfd.
func ParseDisplayFD(r io.Reader) (int, error) {
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil && !(err == io.EOF && len(line) > 0) {
		return 0, fmt.Errorf("read Xwayland -displayfd: %w", err)
	}
	line = strings.TrimSpace(line)
	n, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("Xwayland displayfd %q: %w", line, err)
	}
	if n < 0 || n > 255 {
		return 0, fmt.Errorf("Xwayland display number %d out of range", n)
	}
	return n, nil
}

func filterEnv(env []string, drop string) []string {
	prefix := drop + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Start launches `Xwayland -rootless -noreset -displayfd` against waylandDisplay
// and takes the X11 WM role. pump is called while waiting (typically
// compositor Dispatch) so Xwayland can complete its Wayland handshake.
// runtimeDir is XDG_RUNTIME_DIR. Host DISPLAY is stripped so the child
// attaches to worldr, not Plasma's Xwayland.
func Start(waylandDisplay, runtimeDir string, pump func()) (*Instance, error) {
	if waylandDisplay == "" {
		return nil, fmt.Errorf("Xwayland needs the worldr WAYLAND_DISPLAY (not the host session)")
	}
	if runtimeDir == "" {
		return nil, fmt.Errorf("Xwayland needs XDG_RUNTIME_DIR")
	}
	bin, err := LookPath()
	if err != nil {
		return nil, err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	// Test stubs set WORLDR_XWAYLAND to a script that cannot speak X11.
	native := os.Getenv("WORLDR_XWAYLAND") == ""
	var wmSrv, wmCli *os.File
	args := []string{"-rootless", "-noreset", "-ac", "-shm", "-displayfd", "3"}
	if native {
		var err2 error
		wmSrv, wmCli, err2 = socketpairFiles()
		if err2 != nil {
			_ = r.Close()
			_ = w.Close()
			return nil, err2
		}
		args = append(args, "-wm", "4")
	}
	if os.Getenv("WORLDR_XWAYLAND_VERBOSE") != "" {
		args = append(args, "-verbose", "3")
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = filterEnv(os.Environ(), "DISPLAY")
	cmd.Env = filterEnv(cmd.Env, "WAYLAND_DISPLAY")
	cmd.Env = filterEnv(cmd.Env, "XDG_RUNTIME_DIR")
	cmd.Env = append(cmd.Env,
		"WAYLAND_DISPLAY="+waylandDisplay,
		"XDG_RUNTIME_DIR="+runtimeDir,
	)
	if wmSrv != nil {
		cmd.ExtraFiles = []*os.File{w, wmSrv}
	} else {
		cmd.ExtraFiles = []*os.File{w}
	}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = r.Close()
		_ = w.Close()
		if wmSrv != nil {
			_ = wmSrv.Close()
		}
		if wmCli != nil {
			_ = wmCli.Close()
		}
		return nil, fmt.Errorf("start Xwayland: %w", err)
	}
	_ = w.Close()
	if wmSrv != nil {
		_ = wmSrv.Close()
	}

	stopPump := make(chan struct{})
	pumpDone := make(chan struct{})
	if pump != nil {
		go func() {
			defer close(pumpDone)
			tck := time.NewTicker(time.Millisecond)
			defer tck.Stop()
			for {
				select {
				case <-stopPump:
					return
				case <-tck.C:
					pump()
				}
			}
		}()
	} else {
		close(pumpDone)
	}

	_ = r.SetReadDeadline(time.Now().Add(8 * time.Second))
	n, err := ParseDisplayFD(r)
	_ = r.Close()
	if err != nil {
		close(stopPump)
		<-pumpDone
		if wmCli != nil {
			_ = wmCli.Close()
		}
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, err
	}
	in := &Instance{Cmd: cmd, Display: fmt.Sprintf(":%d", n), DisplayNum: n, bin: bin}
	if wmCli != nil {
		if conn, ferr := net.FileConn(wmCli); ferr == nil {
			_ = wmCli.Close()
			if wm, wmErr := StartXWMConn(conn); wmErr != nil {
				fmt.Fprintf(os.Stderr, "xwayland: XWM via -wm fd failed (%v); trying DISPLAY\n", wmErr)
				_ = conn.Close()
				in.WM = startXWMIfSocket(n)
			} else {
				in.WM = wm
			}
		} else {
			_ = wmCli.Close()
			in.WM = startXWMIfSocket(n)
		}
	}
	if in.WM == nil {
		fmt.Fprintf(os.Stderr, "xwayland: XWM not ready (managed X11 windows will not map)\n")
	}
	close(stopPump)
	<-pumpDone
	return in, nil
}

func startXWMIfSocket(displayNum int) *XWM {
	if _, err := os.Stat(fmt.Sprintf("/tmp/.X11-unix/X%d", displayNum)); err != nil {
		return nil
	}
	wm, err := StartXWM(displayNum, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "xwayland: XWM DISPLAY :%d: %v\n", displayNum, err)
		return nil
	}
	return wm
}

func socketpairFiles() (srv, cli *os.File, err error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(fds[0]), "xwm-srv"), os.NewFile(uintptr(fds[1]), "xwm-cli"), nil
}

// Close terminates Xwayland (process group).
func (in *Instance) Close() {
	if in == nil || in.Cmd == nil || in.Cmd.Process == nil {
		return
	}
	in.once.Do(func() {
		in.WM.Close()
		pgid, err := syscall.Getpgid(in.Cmd.Process.Pid)
		if err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
		} else {
			_ = in.Cmd.Process.Signal(syscall.SIGTERM)
		}
		done := make(chan struct{})
		go func() {
			_, _ = in.Cmd.Process.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			if err == nil {
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				_ = in.Cmd.Process.Kill()
			}
			_, _ = in.Cmd.Process.Wait()
		}
	})
}

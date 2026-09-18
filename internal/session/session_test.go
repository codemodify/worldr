//go:build linux

package session

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestShellArgumentsDefaultDirectAndExplicitChoices(t *testing.T) {
	if got := ShellArguments([]string{"--terminal"}); !slices.Equal(got, []string{"--backend=vk-display", "--take-over-display", "--terminal"}) {
		t.Fatal(got)
	}
	withAccessibility := AccessibilityArguments([]string{"--terminal"}, "/run/user/1000")
	if !slices.Equal(withAccessibility, []string{"--terminal", "--accessibility-socket=/run/user/1000/worldr-accessibility.sock"}) {
		t.Fatal(withAccessibility)
	}
	explicit := []string{"--accessibility-socket=/tmp/custom"}
	if got := AccessibilityArguments(explicit, "/run/user/1000"); !slices.Equal(got, explicit) {
		t.Fatal(got)
	}
	for _, input := range [][]string{{"--backend=nested"}, {"--backend", "headless"}, {"--list-outputs"}, {"--version"}} {
		if got := ShellArguments(input); !slices.Equal(got, input) {
			t.Fatalf("explicit choice changed: %v -> %v", input, got)
		}
	}
}

func TestRuntimeDirectoryRejectsBroadPermissionsAndForeignPaths(t *testing.T) {
	private := filepath.Join(t.TempDir(), "runtime")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := RuntimeDirectory(private); err != nil || got != private {
		t.Fatalf("private runtime: %q %v", got, err)
	}
	if err := os.Chmod(private, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RuntimeDirectory(private); err == nil {
		t.Fatal("public runtime directory accepted")
	}
	if _, err := RuntimeDirectory(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing runtime directory accepted")
	}
}

func TestEnvironmentReplacesOnlySessionKeys(t *testing.T) {
	environment := Environment([]string{"PATH=/bin", "XDG_SESSION_TYPE=tty", "DISPLAY=:1"}, "/run/worldr", "worldr")
	joined := strings.Join(environment, "\n")
	for _, want := range []string{"PATH=/bin", "DISPLAY=:1", "XDG_SESSION_TYPE=wayland", "XDG_RUNTIME_DIR=/run/worldr", "XDG_CURRENT_DESKTOP=worldr"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %s", want, joined)
		}
	}
	if strings.Contains(joined, "XDG_SESSION_TYPE=tty") {
		t.Fatal(joined)
	}
}

func TestRunDryRunAndLaunch(t *testing.T) {
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, "runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	shell := filepath.Join(dir, shellName)
	marker := filepath.Join(dir, "marker")
	script := "#!/bin/sh\nprintf '%s\\n' \"$XDG_SESSION_TYPE:$XDG_CURRENT_DESKTOP:$*\" > \"$WORLDR_TEST_MARKER\"\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORLDR_TEST_MARKER", marker)
	var output bytes.Buffer
	options := Options{Shell: shell, RuntimeDir: runtimeDir, Desktop: "worldr", DryRun: true, ShellArgs: []string{"--terminal"}}
	if err := Run(&output, &output, nil, "", options); err != nil {
		t.Fatal(err)
	}
	if text := output.String(); !strings.Contains(text, "--backend=vk-display --take-over-display --terminal --accessibility-socket="+filepath.Join(runtimeDir, "worldr-accessibility.sock")) {
		t.Fatal(text)
	}
	options.DryRun = false
	output.Reset()
	if err := Run(&output, &output, nil, "", options); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "wayland:worldr:--backend=vk-display --take-over-display --terminal --accessibility-socket="+filepath.Join(runtimeDir, "worldr-accessibility.sock") {
		t.Fatal(got)
	}
}

func TestParseSeparatesWrapperAndShellArguments(t *testing.T) {
	options, err := Parse([]string{"--desktop=worldr-lab", "--shutdown-timeout=3s", "--", "--backend=nested", "--terminal"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Desktop != "worldr-lab" || options.ShutdownTimeout != 3*time.Second || !slices.Equal(options.ShellArgs, []string{"--backend=nested", "--terminal"}) {
		t.Fatalf("%+v", options)
	}
	if _, err := Parse([]string{"--shutdown-timeout=0"}); err == nil {
		t.Fatal("unbounded shutdown timeout accepted")
	}
}

func TestSuperviseForwardsSignalAndBoundsUnresponsiveChild(t *testing.T) {
	for _, test := range []struct {
		name, script string
		wantTimeout  bool
	}{
		{"cooperative", `trap 'exit 0' TERM; printf x >&3; while :; do sleep .01; done`, false},
		{"unresponsive", `trap '' TERM HUP INT; printf x >&3; while :; do sleep 1; done`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			readyRead, readyWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", "-c", test.script)
			cmd.ExtraFiles = []*os.File{readyWrite}
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			_ = readyWrite.Close()
			t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
			var ready [1]byte
			if _, err := io.ReadFull(readyRead, ready[:]); err != nil {
				t.Fatal(err)
			}
			_ = readyRead.Close()
			signals := make(chan os.Signal, 2)
			signals <- syscall.SIGTERM
			started := time.Now()
			err = supervise(cmd, signals, 120*time.Millisecond, time.Second)
			var timeout *ShutdownTimeoutError
			if errors.As(err, &timeout) != test.wantTimeout {
				t.Fatalf("timeout=%t error=%v", test.wantTimeout, err)
			}
			if time.Since(started) > 2*time.Second {
				t.Fatalf("supervision exceeded its bound: %s", time.Since(started))
			}
			if cmd.ProcessState == nil {
				t.Fatal("supervision returned without reaping the shell")
			}
		})
	}
}

func TestSuperviseSecondSignalEscalatesImmediately(t *testing.T) {
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", `trap '' TERM HUP INT; printf x >&3; while :; do sleep 1; done`)
	cmd.ExtraFiles = []*os.File{readyWrite}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = readyWrite.Close()
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	var ready [1]byte
	if _, err := io.ReadFull(readyRead, ready[:]); err != nil {
		t.Fatal(err)
	}
	_ = readyRead.Close()
	signals := make(chan os.Signal, 2)
	signals <- syscall.SIGTERM
	signals <- syscall.SIGINT
	started := time.Now()
	err = supervise(cmd, signals, 4*time.Second, time.Second)
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("second signal did not bypass grace period: %s", elapsed)
	}
	var timeout *ShutdownTimeoutError
	if errors.As(err, &timeout) {
		t.Fatalf("operator escalation was reported as timeout: %v", err)
	}
	if cmd.ProcessState == nil {
		t.Fatal("escalation returned without reaping the shell")
	}
}

func TestSupervisePinsGroupUntilStragglersAreKilled(t *testing.T) {
	for _, test := range []struct {
		name, mode string
		signal     bool
	}{
		{name: "leader exits first", mode: "leader-exit"},
		{name: "leader handles termination", mode: "leader-term", signal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "descendant-pid")
			cmd := exec.Command(os.Args[0], "-test.run=^TestSessionProcessFixture$")
			cmd.Env = overlay(os.Environ(), []string{
				"WORLDR_SESSION_PROCESS_FIXTURE=" + test.mode,
				"WORLDR_SESSION_DESCENDANT_PID=" + marker,
			})
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			leaderFD, err := unix.PidfdOpen(cmd.Process.Pid, 0)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = unix.PidfdSendSignal(leaderFD, unix.SIGKILL, nil, 0)
				_ = unix.Close(leaderFD)
			})
			descendantPID := waitForPIDMarker(t, marker)
			if group, err := syscall.Getpgid(descendantPID); err != nil || group != cmd.Process.Pid {
				t.Fatalf("descendant escaped shell group: group=%d err=%v", group, err)
			}
			descendantFD, err := unix.PidfdOpen(descendantPID, 0)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = unix.PidfdSendSignal(descendantFD, unix.SIGKILL, nil, 0)
				_ = unix.Close(descendantFD)
			})
			signals := make(chan os.Signal, 1)
			if test.signal {
				signals <- syscall.SIGTERM
			}
			// Race-instrumented fixture binaries deliberately sleep during normal
			// process exit, so leave that tool overhead outside the product's
			// signal/cleanup deadline being exercised here.
			if err := supervise(cmd, signals, 3*time.Second, time.Second); err != nil {
				t.Fatal(err)
			}
			waitForPIDFDExit(t, descendantFD, time.Second)
			if cmd.ProcessState == nil {
				t.Fatal("shell leader was not reaped")
			}
		})
	}
}

func TestPrepareShellProcessAndKernelParentDeath(t *testing.T) {
	prepared := exec.Command("true")
	prepareShellProcess(prepared)
	if prepared.SysProcAttr == nil || !prepared.SysProcAttr.Setpgid || prepared.SysProcAttr.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("unsafe process attributes: %+v", prepared.SysProcAttr)
	}
	if prepared.WaitDelay <= 0 || prepared.WaitDelay >= finalKillWait {
		t.Fatalf("pipe wait is not bounded inside final kill wait: %s", prepared.WaitDelay)
	}

	marker := filepath.Join(t.TempDir(), "shell-pid")
	supervisor := exec.Command(os.Args[0], "-test.run=^TestSessionParentDeathFixture$")
	supervisor.Env = overlay(os.Environ(), []string{
		"WORLDR_SESSION_PDEATH_FIXTURE=supervisor",
		"WORLDR_SESSION_PDEATH_PID=" + marker,
	})
	if err := supervisor.Start(); err != nil {
		t.Fatal(err)
	}
	supervisorFD, err := unix.PidfdOpen(supervisor.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = unix.PidfdSendSignal(supervisorFD, unix.SIGKILL, nil, 0)
		// If the assertion waiting for the marker fails, still collect and kill
		// any child which raced with supervisor teardown. The pidfd makes this
		// safe against numeric PID reuse.
		deadline := time.Now().Add(250 * time.Millisecond)
		for time.Now().Before(deadline) {
			data, readErr := os.ReadFile(marker)
			if readErr == nil {
				pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
				if parseErr == nil && pid > 1 {
					if fd, openErr := unix.PidfdOpen(pid, 0); openErr == nil {
						_ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, 0)
						_ = unix.Close(fd)
					}
				}
				break
			}
			if !os.IsNotExist(readErr) {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		_ = unix.Close(supervisorFD)
	})
	shellPID := waitForPIDMarker(t, marker)
	shellFD, err := unix.PidfdOpen(shellPID, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = unix.PidfdSendSignal(shellFD, unix.SIGKILL, nil, 0)
		_ = unix.Close(shellFD)
	})
	if err := unix.PidfdSendSignal(supervisorFD, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Wait(); err == nil {
		t.Fatal("SIGKILLed supervisor reported success")
	}
	waitForPIDFDExit(t, shellFD, 2*time.Second)
}

func waitForPIDMarker(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || pid <= 1 {
				t.Fatalf("invalid process marker %q: %v", data, err)
			}
			return pid
		}
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("process marker %s was not written", path)
	return 0
}

func waitForPIDFDExit(t *testing.T, fd int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatal("process remained alive past cleanup deadline")
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		ready, err := unix.Poll(poll, int(remaining.Milliseconds())+1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if ready > 0 && poll[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0 {
			return
		}
	}
}

func TestSessionProcessFixture(t *testing.T) {
	mode := os.Getenv("WORLDR_SESSION_PROCESS_FIXTURE")
	if mode == "" {
		return
	}
	marker := os.Getenv("WORLDR_SESSION_DESCENDANT_PID")
	if mode == "child" {
		signal.Ignore(syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		if err := os.WriteFile(marker, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	var termination chan os.Signal
	if mode == "leader-term" {
		termination = make(chan os.Signal, 1)
		signal.Notify(termination, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		defer signal.Stop(termination)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestSessionProcessFixture$")
	child.Env = overlay(os.Environ(), []string{"WORLDR_SESSION_PROCESS_FIXTURE=child"})
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("descendant failed to become ready")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if mode == "leader-term" {
		<-termination
	}
}

func TestSessionParentDeathFixture(t *testing.T) {
	role := os.Getenv("WORLDR_SESSION_PDEATH_FIXTURE")
	if role == "" {
		return
	}
	if role == "shell" {
		if err := os.WriteFile(os.Getenv("WORLDR_SESSION_PDEATH_PID"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	shell := exec.Command(os.Args[0], "-test.run=^TestSessionParentDeathFixture$")
	shell.Env = overlay(os.Environ(), []string{"WORLDR_SESSION_PDEATH_FIXTURE=shell"})
	prepareShellProcess(shell)
	if err := shell.Start(); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestDesktopEntriesUseInstalledLaunchers(t *testing.T) {
	checks := map[string][]string{
		filepath.Join("..", "..", "contrib", "wayland-sessions", "worldr.desktop"): {"Exec=worldr-session", "TryExec=worldr-session", "DesktopNames=worldr"},
		filepath.Join("..", "..", "contrib", "applications", "worldr.desktop"):     {"Exec=worldr-shell --backend=nested", "TryExec=worldr-shell", "Terminal=false"},
	}
	for path, expected := range checks {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range expected {
			if !strings.Contains(string(data), value) {
				t.Fatalf("%s is missing %q", path, value)
			}
		}
	}
}

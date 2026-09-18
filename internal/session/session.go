// Package session prepares a display-manager session and supervises
// worldr-shell. Authentication and user switching belong to the display
// manager; this package always runs as the already authenticated user.
package session

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	DefaultDesktop         = "worldr"
	DefaultShutdownTimeout = 8 * time.Second
	finalKillWait          = 2 * time.Second
	shellName              = "worldr-shell"
)

type Options struct {
	Shell      string
	Desktop    string
	RuntimeDir string
	PrintEnv   bool
	DryRun     bool
	// ShutdownTimeout bounds graceful shell teardown after the display manager
	// asks the session to stop. Zero selects DefaultShutdownTimeout.
	ShutdownTimeout time.Duration
	ShellArgs       []string
}

// ShutdownTimeoutError reports that the supervised shell ignored the first
// termination request and had to be killed. The wrapper itself still returns
// promptly even if the graphics process was blocked during driver teardown.
type ShutdownTimeoutError struct {
	Timeout time.Duration
	Err     error
}

func (e *ShutdownTimeoutError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("worldr shell did not stop within %s and was killed", e.Timeout)
	}
	return fmt.Sprintf("worldr shell did not stop within %s and was killed: %v", e.Timeout, e.Err)
}

func (e *ShutdownTimeoutError) Unwrap() error { return e.Err }

// FindShell resolves an explicit executable, a sibling installation, then PATH.
func FindShell(explicit, argv0 string) (string, error) {
	if explicit != "" {
		return executable(explicit)
	}
	if dir := filepath.Dir(argv0); argv0 != "" && dir != "" && dir != "." {
		if path, err := executable(filepath.Join(dir, shellName)); err == nil {
			return path, nil
		}
	}
	path, err := exec.LookPath(shellName)
	if err != nil {
		return "", fmt.Errorf("%s not found; install it beside worldr-session or pass --shell", shellName)
	}
	return filepath.Abs(path)
}

func executable(path string) (string, error) {
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", path)
	}
	return filepath.Abs(resolved)
}

// RuntimeDirectory validates the directory supplied by pam_systemd or an
// explicit test/session launcher. Refusing foreign or broadly accessible paths
// prevents another user from replacing the compositor's private sockets.
func RuntimeDirectory(explicit string) (string, error) {
	dir := strings.TrimSpace(explicit)
	if dir == "" {
		dir = strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	}
	if dir == "" {
		dir = filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	}
	dir = filepath.Clean(dir)
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("XDG_RUNTIME_DIR %s: %w (a display manager or pam_systemd must create it)", dir, err)
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("XDG_RUNTIME_DIR %s must be a directory private to its owner", dir)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		return "", fmt.Errorf("XDG_RUNTIME_DIR %s is owned by uid %d, not %d", dir, stat.Uid, os.Getuid())
	}
	return dir, nil
}

// ShellArguments chooses direct display for a login session unless the caller
// made an explicit backend or read-only inventory choice. take-over-display is
// safe here because invoking worldr-session is itself the display-manager grant.
func ShellArguments(args []string) []string {
	result := append([]string(nil), args...)
	for _, arg := range result {
		if arg == "--backend" || strings.HasPrefix(arg, "--backend=") || arg == "--list-outputs" || arg == "--list-devices" || arg == "--version" {
			return result
		}
	}
	return append([]string{"--backend=vk-display", "--take-over-display"}, result...)
}

func AccessibilityArguments(args []string, runtimeDir string) []string {
	for _, argument := range args {
		if argument == "--accessibility-socket" || strings.HasPrefix(argument, "--accessibility-socket=") {
			return args
		}
	}
	return append(args, "--accessibility-socket="+filepath.Join(runtimeDir, "worldr-accessibility.sock"))
}

func overlay(base, values []string) []string {
	replace := make(map[string]bool, len(values))
	for _, entry := range values {
		if index := strings.IndexByte(entry, '='); index > 0 {
			replace[entry[:index]] = true
		}
	}
	result := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		index := strings.IndexByte(entry, '=')
		if index <= 0 || replace[entry[:index]] {
			continue
		}
		result = append(result, entry)
	}
	return append(result, values...)
}

func Environment(base []string, runtimeDir, desktop string) []string {
	if desktop == "" {
		desktop = DefaultDesktop
	}
	return overlay(base, []string{
		"XDG_RUNTIME_DIR=" + runtimeDir,
		"XDG_SESSION_TYPE=wayland",
		"XDG_SESSION_CLASS=user",
		"XDG_CURRENT_DESKTOP=" + desktop,
		"XDG_SESSION_DESKTOP=" + desktop,
		"DESKTOP_SESSION=" + desktop,
	})
}

func Run(stdout, stderr io.Writer, stdin io.Reader, argv0 string, options Options) error {
	runtimeDir, err := RuntimeDirectory(options.RuntimeDir)
	if err != nil {
		return err
	}
	desktop := options.Desktop
	if desktop == "" {
		desktop = DefaultDesktop
	}
	environment := Environment(os.Environ(), runtimeDir, desktop)
	arguments := AccessibilityArguments(ShellArguments(options.ShellArgs), runtimeDir)
	if options.PrintEnv || options.DryRun {
		for _, key := range []string{"XDG_RUNTIME_DIR", "XDG_SESSION_TYPE", "XDG_SESSION_CLASS", "XDG_CURRENT_DESKTOP", "XDG_SESSION_DESKTOP", "DESKTOP_SESSION"} {
			prefix := key + "="
			for _, entry := range environment {
				if strings.HasPrefix(entry, prefix) {
					fmt.Fprintln(stdout, entry)
					break
				}
			}
		}
	}
	if options.PrintEnv && !options.DryRun {
		return nil
	}
	shell, err := FindShell(options.Shell, argv0)
	if err != nil {
		return err
	}
	if options.DryRun {
		fmt.Fprintf(stdout, "shell=%s\nargs=%s\n", shell, strings.Join(arguments, " "))
		return nil
	}
	cmd := exec.Command(shell, arguments...)
	cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = environment, stdin, stdout, stderr
	// A distinct process group lets escalation include compatibility clients and
	// native-app helpers still attached to the session. Providers retain their
	// ordinary graceful-close interval inside worldr-shell.
	// Kernel-enforced parent death cleanup covers an unexpected supervisor
	// crash, where no Go defer or signal-forwarding path can run. Normal display
	// manager shutdown still follows the graceful bounded path below.
	prepareShellProcess(cmd)
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	// Pdeathsig follows the lifetime of the thread that creates the child, not
	// merely the Go process. Keep that thread alive until the child is reaped so
	// the kernel cannot mistake a retired runtime worker for supervisor death.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start worldr shell: %w", err)
	}
	timeout := options.ShutdownTimeout
	if timeout <= 0 {
		timeout = DefaultShutdownTimeout
	}
	err = supervise(cmd, signals, timeout, finalKillWait)
	var timeoutError *ShutdownTimeoutError
	if errors.As(err, &timeoutError) {
		return timeoutError
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return fmt.Errorf("worldr shell exited with status %d", exit.ExitCode())
	}
	return err
}

func supervise(cmd *exec.Cmd, signals <-chan os.Signal, timeout, killWait time.Duration) error {
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("worldr shell process is unavailable")
	}
	if timeout <= 0 {
		timeout = DefaultShutdownTimeout
	}
	if killWait <= 0 {
		killWait = finalKillWait
	}

	// Observe the leader without reaping it. Its retained PID keeps the process
	// group number owned until every remaining member has received the final
	// cleanup signal, closing the PID-reuse race between Wait and kill(-pgid).
	observed := make(chan error, 1)
	go func() { observed <- observeShellProcess(cmd) }()

	for {
		select {
		case observationErr := <-observed:
			return finishObservedShell(cmd, observationErr, killWait)
		case sig, ok := <-signals:
			if !ok {
				signals = nil
				continue
			}
			if sig == nil {
				continue
			}
			if err := signalProcessGroup(cmd, sig); err != nil {
				cleanupErr := killAndReapShell(cmd, observed, killWait)
				return errors.Join(fmt.Errorf("forward %s to worldr shell: %w", sig, err), cleanupErr)
			}
			return awaitGracefulShellShutdown(cmd, observed, signals, timeout, killWait)
		}
	}
}

func prepareShellProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	// A descendant which escaped the process group can otherwise retain an
	// inherited stdout/stderr pipe and strand os/exec's copy goroutines after
	// the leader has died. The process-group kill remains the primary cleanup.
	cmd.WaitDelay = finalKillWait / 2
}

func observeShellProcess(cmd *exec.Cmd) error {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err
	}
}

func awaitGracefulShellShutdown(cmd *exec.Cmd, observed <-chan error, signals <-chan os.Signal, timeout, killWait time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case observationErr := <-observed:
			return finishObservedShell(cmd, observationErr, killWait)
		case <-timer.C:
			err := killAndReapShell(cmd, observed, killWait)
			return &ShutdownTimeoutError{Timeout: timeout, Err: err}
		case sig, ok := <-signals:
			if !ok {
				signals = nil
				continue
			}
			if sig == nil {
				continue
			}
			// A second request means that the operator wants immediate teardown.
			return killAndReapShell(cmd, observed, killWait)
		}
	}
}

func finishObservedShell(cmd *exec.Cmd, observationErr error, killWait time.Duration) error {
	// The shell coordinates graceful provider shutdown before it exits. Any
	// process still in its private group after that point is a straggler and
	// must not survive the login session. The unreaped leader still pins PGID.
	killErr := signalProcessGroup(cmd, syscall.SIGKILL)
	reapErr := reapShell(cmd, killWait)
	return errors.Join(observationErr, killErr, reapErr)
}

func killAndReapShell(cmd *exec.Cmd, observed <-chan error, killWait time.Duration) error {
	deadline := time.Now().Add(killWait)
	killErr := signalProcessGroup(cmd, syscall.SIGKILL)
	observationTimer := time.NewTimer(time.Until(deadline))
	defer observationTimer.Stop()
	var observationErr error
	select {
	case observationErr = <-observed:
	case <-observationTimer.C:
		// Keep ownership of eventual cleanup even though the caller's deadline
		// has expired. This cannot delay the session manager's return.
		go func() {
			<-observed
			_ = cmd.Wait()
		}()
		return errors.Join(killErr, fmt.Errorf("worldr shell leader remained after SIGKILL for %s", killWait))
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		remaining = time.Nanosecond
	}
	return errors.Join(killErr, observationErr, reapShell(cmd, remaining))
}

func reapShell(cmd *exec.Cmd, timeout time.Duration) error {
	reaped := make(chan error, 1)
	go func() { reaped <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-reaped:
		return err
	case <-timer.C:
		return fmt.Errorf("worldr shell could not be reaped within %s", timeout)
	}
}

func signalProcessGroup(cmd *exec.Cmd, signal os.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("shell process is unavailable")
	}
	value, ok := signal.(syscall.Signal)
	if !ok {
		return cmd.Process.Signal(signal)
	}
	err := syscall.Kill(-cmd.Process.Pid, value)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

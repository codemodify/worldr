package app

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func applicationExitPinned() bool { return true }

// WNOWAIT observes death without releasing the leader's PID. Keeping that
// zombie until shutdown prevents its process-group number from being recycled
// while descendants can still connect to the private application server.
func observeApplicationProcess(cmd *exec.Cmd) error {
	var info unix.Siginfo
	for {
		err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err
	}
}

func applicationProcessMatches(cmd *exec.Cmd, pid uint32) bool {
	if cmd == nil || cmd.Process == nil || pid == 0 {
		return false
	}
	if uint32(cmd.Process.Pid) == pid {
		return true
	}
	group, err := syscall.Getpgid(int(pid))
	return err == nil && group == cmd.Process.Pid
}

// Zombie leaders deliberately remain in their groups, so kill(group, 0) cannot
// tell whether live descendants still need the termination grace period. One
// /proc scan checks all owned groups, excluding dead/zombie members.
func applicationProcessesRunning(children []*applicationProcess) bool {
	groups := make(map[int]bool, len(children))
	for _, child := range children {
		if !child.reaped && child.observeErr == nil && child.cmd.Process != nil {
			groups[child.cmd.Process.Pid] = true
		}
	}
	if len(groups) == 0 {
		return false
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) < 3 || fields[0] == "Z" || fields[0] == "X" {
			continue
		}
		group, err := strconv.Atoi(fields[2])
		if err == nil && groups[group] {
			return true
		}
	}
	return false
}

func prepareApplicationProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Inherited pipes held by descendants must not make Wait block forever.
	cmd.WaitDelay = 250_000_000
}
func stopApplicationProcess(cmd *exec.Cmd) {
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

func killApplicationProcess(cmd *exec.Cmd) {
	// The application was started as a private process-group leader. Escalate
	// the whole group so descendants cannot retain stdout or the private display.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

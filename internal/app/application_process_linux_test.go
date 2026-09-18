package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/codemodify/worldr/internal/platform/linux/apps"
	"golang.org/x/sys/unix"
)

type closingApplicationServer struct{ applicationServer }

func (closingApplicationServer) Close() error                  { return nil }
func (closingApplicationServer) RequestClose() error           { return nil }
func (closingApplicationServer) Poll() ([]apps.Surface, error) { return nil, nil }

func processState(pid int) (byte, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 || end+2 >= len(data) {
		return 0, fmt.Errorf("malformed process stat")
	}
	return data[end+2], nil
}

func TestApplicationCloseKillsStubbornProcessGroup(t *testing.T) {
	testApplicationProcessGroup(t, "parent")
}
func TestApplicationCloseKeepsExitedLauncherPinned(t *testing.T) {
	testApplicationProcessGroup(t, "orphan-parent")
}

func testApplicationProcessGroup(t *testing.T, mode string) {
	ready := filepath.Join(t.TempDir(), "child-ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestApplicationProcessFixture$")
	cmd.Env = append(os.Environ(), "WORLDR_PROCESS_FIXTURE="+mode, "WORLDR_PROCESS_READY="+ready)
	var log applicationLog
	cmd.Stdout, cmd.Stderr = &log, &log
	prepareApplicationProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	child := &applicationProcess{cmd: cmd, wait: make(chan error, 1), slot: "fixture"}
	go func() { child.wait <- observeApplicationProcess(cmd) }()
	a := &applicationController{server: closingApplicationServer{}, children: []*applicationProcess{child}, output: io.Discard}
	t.Cleanup(a.close)
	leaderFD, err := unix.PidfdOpen(cmd.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(leaderFD)
	var descendantPID int
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(ready); err == nil {
			descendantPID, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil || descendantPID <= 1 {
				t.Fatalf("invalid descendant PID %q", data)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if descendantPID == 0 {
		t.Fatalf("fixture descendant did not start: %s", log.drain())
	}
	descendantFD, err := unix.PidfdOpen(descendantPID, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.PidfdSendSignal(descendantFD, unix.SIGKILL, nil, 0); unix.Close(descendantFD) })
	if group, err := syscall.Getpgid(descendantPID); err != nil || group != cmd.Process.Pid {
		t.Fatalf("descendant escaped private group: %d %v", group, err)
	}
	if mode == "orphan-parent" {
		deadline = time.Now().Add(3 * time.Second)
		for !child.exited && time.Now().Before(deadline) {
			a.collectExits(false)
			time.Sleep(5 * time.Millisecond)
		}
		if !child.exited || child.reaped || child.observeErr != nil || cmd.ProcessState != nil {
			t.Fatalf("launcher exit was not retained: exited=%v reaped=%v err=%v", child.exited, child.reaped, child.observeErr)
		}
		if state, err := processState(cmd.Process.Pid); err != nil || state != 'Z' {
			t.Fatalf("launcher PID not pinned as unreaped child: %c %v", state, err)
		}
		if !applicationProcessMatches(cmd, uint32(descendantPID)) {
			t.Fatal("descendant identity lost after launcher exit")
		}
		if !applicationProcessesRunning(a.children) {
			t.Fatal("live descendant hidden by exited launcher")
		}
	}
	done := make(chan struct{})
	go func() { a.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		// Exact pidfds remain safe even if a failing close already reaped the
		// leader. Never signal a retained numeric group after ownership ends.
		unix.PidfdSendSignal(descendantFD, unix.SIGKILL, nil, 0)
		unix.PidfdSendSignal(leaderFD, unix.SIGKILL, nil, 0)
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		t.Fatal("application shutdown exceeded termination deadline")
	}
	if !a.exited || !child.exited || !child.reaped || cmd.ProcessState == nil {
		t.Fatal("shutdown did not reap its leader exactly once")
	}
	var info unix.Siginfo
	if err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOHANG, nil); err != unix.ECHILD {
		t.Fatalf("leader remained waitable after close: %v", err)
	}
	start := time.Now()
	a.close()
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("idempotent close waited on consumed process exit")
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state, err := processState(descendantPID)
		if os.IsNotExist(err) || (err == nil && (state == 'Z' || state == 'X')) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("application descendant survived group cleanup")
}

func TestWrappedFootUsesNamedLaunchIdentity(t *testing.T) {
	if os.Getenv("WORLDR_TEST_APPS") != "1" {
		t.Skip("set WORLDR_TEST_APPS=1 for real wrapped foot clients")
	}
	foot, err := exec.LookPath("foot")
	if err != nil {
		t.Fatal(err)
	}
	launches := []ApplicationLaunch{}
	for _, id := range []string{"wrapped-one", "wrapped-two"} {
		launches = append(launches, ApplicationLaunch{ID: id, Command: "sh", Args: []string{"-c", `"$0" --config=/dev/null --app-id=worldr-wrapper-test sh -c 'sleep 10' &`, foot}})
	}
	a, err := launchApplication(Options{Launches: launches}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if err := a.poll(); err != nil {
			t.Fatal(err)
		}
		if len(a.images) == 2 && a.exited {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(a.images) != 2 || !a.exited {
		t.Fatalf("wrapped clients failed to map after launcher exit: images=%d launchersExited=%v", len(a.images), a.exited)
	}
	surfaces, err := a.server.Poll()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, surface := range surfaces {
		entry := a.images[surface.ID]
		if entry == nil {
			t.Fatal("missing renderer surface")
		}
		matched := false
		for _, child := range a.children {
			if applicationProcessMatches(child.cmd, surface.PID) {
				if surface.PID == uint32(child.cmd.Process.Pid) {
					t.Fatal("test did not fork its GUI")
				}
				if entry.surface.Key != child.slot+"/window-1" {
					t.Fatalf("wrapper window key=%q, want named launch %q", entry.surface.Key, child.slot)
				}
				seen[child.slot] = true
				matched = true
				break
			}
		}
		if !matched {
			t.Fatal("GUI lost association with pinned launch group")
		}
	}
	if len(seen) != 2 {
		t.Fatal("independent wrapped clients shared a launch key")
	}
}

// Both descendants ignore TERM and inherit output pipes. The orphan-parent
// mode exits immediately after starting its descendant, recreating the former
// cleanup hole while WNOWAIT must retain safe ownership of the private group.
func TestApplicationProcessFixture(t *testing.T) {
	mode := os.Getenv("WORLDR_PROCESS_FIXTURE")
	if mode == "" {
		return
	}
	signal.Ignore(syscall.SIGTERM)
	if mode == "child" {
		if err := os.WriteFile(os.Getenv("WORLDR_PROCESS_READY"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	child := exec.Command(os.Args[0], "-test.run=^TestApplicationProcessFixture$")
	child.Env = append(os.Environ(), "WORLDR_PROCESS_FIXTURE=child")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	if mode == "orphan-parent" {
		return
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
}

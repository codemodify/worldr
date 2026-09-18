//go:build !linux

package app

import "os/exec"

func applicationExitPinned() bool                   { return false }
func observeApplicationProcess(cmd *exec.Cmd) error { return cmd.Wait() }
func applicationProcessMatches(cmd *exec.Cmd, pid uint32) bool {
	return cmd != nil && cmd.Process != nil && uint32(cmd.Process.Pid) == pid
}
func applicationProcessesRunning(children []*applicationProcess) bool {
	for _, child := range children {
		if !child.exited {
			return true
		}
	}
	return false
}

func prepareApplicationProcess(cmd *exec.Cmd) { cmd.WaitDelay = 250_000_000 }
func stopApplicationProcess(cmd *exec.Cmd)    { _ = cmd.Process.Kill() }
func killApplicationProcess(cmd *exec.Cmd)    { _ = cmd.Process.Kill() }

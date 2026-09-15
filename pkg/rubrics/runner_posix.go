//go:build !windows

package rubrics

import (
	"errors"
	"os/exec"
	"syscall"
)

// setProcAttr puts the spawned process in its own process group so that
// killProcessGroup can later terminate it and any children it forked
// (e.g. a shell script or wrapper that execs further subprocesses) in one
// signal, instead of only the direct child.
func setProcAttr(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup sends SIGKILL to the process group led by cmd's process,
// killing the process and any children it spawned. Go's fork/exec
// synchronizes over a pipe that only closes after execve, and Setpgid runs
// in the child before that -- so by the time Start() returns, the group is
// already set up and there is no race to fall back for here. The fallback
// to a direct kill exists only for genuinely unexpected errors (e.g. the
// group's already gone for some reason other than a normal exit).
func killProcessGroup(cmd *exec.Cmd) error {
	pid := cmd.Process.Pid
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) || isAlreadyExited(err) {
		return nil
	}
	return cmd.Process.Kill()
}

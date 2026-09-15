//go:build !windows

package rubrics

import (
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

// killProcessGroup sends SIGKILL to the process group led by cmd's process
// (best-effort, to also catch any subprocesses it spawned), then
// authoritatively kills the direct child via Process.Kill regardless of how
// the group signal went. Treating a failed/ESRCH'd group signal as
// sufficient on its own would risk leaving the direct child alive with
// nothing left to kill it, hanging the caller's subsequent Wait; always
// falling through to the direct, well-tested Process.Kill avoids that.
func killProcessGroup(cmd *exec.Cmd) error {
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	return cmd.Process.Kill()
}

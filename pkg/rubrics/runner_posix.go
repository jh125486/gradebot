//go:build unix

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
//
// The caller (execCmd.ProcessKill) only reaches this once per Commander and
// only while c.ProcessState is still nil, i.e. before anything has reaped
// this pid -- so it can't yet have been recycled by the OS for an unrelated
// process. That still leaves an unavoidable, narrow TOCTOU between reading
// cmd.Process.Pid here and the kernel processing this signal (the process
// could exit and its pid get reused in between); Process.Kill itself is
// safe against that on platforms where Go's exec package binds the kill to
// a process handle rather than a bare pid, but the raw pid-group signal
// below has no such protection. That residual risk is accepted as the
// unavoidable cost of using pid-based process-group signaling at all.
func killProcessGroup(cmd *exec.Cmd) error {
	pid := cmd.Process.Pid
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	return cmd.Process.Kill()
}

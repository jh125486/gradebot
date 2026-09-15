//go:build windows

package rubrics

import "os/exec"

// setProcAttr is a no-op on Windows: there is no POSIX process-group
// equivalent available here without pulling in job-object plumbing, so
// killProcessGroup falls back to killing the direct process only.
func setProcAttr(_ *exec.Cmd) {}

// killProcessGroup kills the process directly. Windows has no signal-based
// process-group kill; terminating a whole tree requires job objects, which
// this package does not set up, so grandchild processes spawned by a
// student program are not guaranteed to be cleaned up on this platform.
func killProcessGroup(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}

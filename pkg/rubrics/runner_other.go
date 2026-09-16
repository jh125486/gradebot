//go:build !windows && !unix

package rubrics

import "os/exec"

// setProcAttr is a no-op on non-Unix, non-Windows targets (e.g. plan9,
// js/wasm, wasip1): there is no process-group primitive available here, so
// killProcessGroup falls back to killing the direct process only.
func setProcAttr(_ *exec.Cmd) {}

// killProcessGroup kills the process directly. Without a process-group
// mechanism on this platform, grandchild processes spawned by a student
// program are not guaranteed to be cleaned up.
func killProcessGroup(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}

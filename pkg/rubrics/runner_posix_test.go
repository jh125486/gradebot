//go:build !windows

package rubrics_test

import (
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jh125486/gradebot/pkg/rubrics"
)

// TestExecCmd_ProcessKill_KillsGrandchild verifies that ProcessKill takes
// down subprocesses spawned by the child (e.g. a student program run via a
// shell wrapper), not just the direct child, by killing the whole process
// group rather than a single pid.
func TestExecCmd_ProcessKill_KillsGrandchild(t *testing.T) {
	t.Parallel()

	builder := &rubrics.ExecCommandBuilder{}
	cmd := builder.New(t.Context(), "sh", "-c", "sleep 60 & echo $!; wait")

	var stdout rubrics.SafeBuffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(io.Discard)

	require.NoError(t, cmd.Start())

	grandchildPID := waitForGrandchildPID(t, &stdout)

	require.NoError(t, cmd.ProcessKill())

	require.Eventually(t, func() bool {
		return !processAlive(grandchildPID)
	}, time.Second, 10*time.Millisecond, "grandchild process survived ProcessKill")
}

func waitForGrandchildPID(t *testing.T, stdout interface{ String() string }) int {
	t.Helper()

	var pidLine string
	require.Eventually(t, func() bool {
		pidLine = strings.TrimSpace(stdout.String())
		return pidLine != ""
	}, time.Second, 10*time.Millisecond, "sh never printed the sleep pid")

	pid, err := strconv.Atoi(pidLine)
	require.NoError(t, err)
	return pid
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// TestExecCmd_ProcessKill_OpenStdinPipeDoesNotDeadlock reproduces Program's
// real setup: stdin is an *io.PipeReader with nothing writing to it. That
// leaves exec.Cmd's internal stdin-copy goroutine blocked forever in Read,
// which would make a naive Cmd.Wait() (used by Cmd.Run/reaping) hang
// indefinitely -- ProcessKill must not be blocked by it.
func TestExecCmd_ProcessKill_OpenStdinPipeDoesNotDeadlock(t *testing.T) {
	t.Parallel()

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })

	builder := &rubrics.ExecCommandBuilder{}
	cmd := builder.New(t.Context(), "sleep", "60")
	cmd.SetStdin(pr)
	cmd.SetStdout(io.Discard)
	cmd.SetStderr(io.Discard)

	require.NoError(t, cmd.Start())

	done := make(chan error, 1)
	go func() { done <- cmd.ProcessKill() }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("ProcessKill deadlocked on stdin pipe with nothing writing to it")
	}
}

// TestExecCmd_ProcessKill_CalledTwiceDoesNotPanic guards against a
// regression where Wait being called more than once on the same *exec.Cmd
// panics -- a realistic sequence since Program's sendToStdin can already
// invoke cleanup (ProcessKill) once on a wedged write before a caller
// explicitly kills the same Commander again.
func TestExecCmd_ProcessKill_CalledTwiceDoesNotPanic(t *testing.T) {
	t.Parallel()

	builder := &rubrics.ExecCommandBuilder{}
	cmd := builder.New(t.Context(), "sleep", "60")
	cmd.SetStdout(io.Discard)
	cmd.SetStderr(io.Discard)

	require.NoError(t, cmd.Start())

	require.NotPanics(t, func() {
		require.NoError(t, cmd.ProcessKill())
		require.NoError(t, cmd.ProcessKill())
	})
}

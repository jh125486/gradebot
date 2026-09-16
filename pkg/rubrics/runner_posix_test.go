//go:build unix

package rubrics_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jh125486/gradebot/pkg/rubrics"
)

// TestExecCmd_ProcessKill_KillsGrandchild verifies that ProcessKill takes
// down subprocesses spawned by the child (e.g. a student program run via a
// shell wrapper), not just the direct child, by killing the whole process
// group rather than a single pid.
//
// Liveness is checked through a delayed side effect (a marker file written
// after a short sleep) rather than signaling the grandchild's pid directly:
// killProcessGroup only reaps the direct shell, so the killed grandchild can
// briefly remain an unreaped zombie, and a zombie still answers a signal-0
// liveness probe as if it were alive.
func TestExecCmd_ProcessKill_KillsGrandchild(t *testing.T) {
	t.Parallel()

	marker := filepath.Join(t.TempDir(), "grandchild-ran")
	script := fmt.Sprintf(`(sleep 0.3 && : > %q) & echo $!; wait`, marker)

	builder := &rubrics.ExecCommandBuilder{}
	cmd := builder.New(t.Context(), "sh", "-c", script)

	var stdout rubrics.SafeBuffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(io.Discard)

	require.NoError(t, cmd.Start())

	waitForGrandchildStarted(t, &stdout)

	require.NoError(t, cmd.ProcessKill())

	// Wait well past the grandchild's scheduled side effect (0.3s) to prove
	// it never got the chance to run: if ProcessKill only killed the direct
	// shell, the backgrounded grandchild would still write the marker.
	time.Sleep(600 * time.Millisecond)
	_, err := os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist, "grandchild survived ProcessKill and wrote its marker file")
}

// waitForGrandchildStarted blocks until the shell script has echoed its
// backgrounded grandchild's pid, confirming the grandchild actually forked
// before the caller kills the group.
func waitForGrandchildStarted(t *testing.T, stdout interface{ String() string }) {
	t.Helper()

	require.Eventually(t, func() bool {
		return strings.TrimSpace(stdout.String()) != ""
	}, time.Second, 10*time.Millisecond, "sh never echoed the backgrounded grandchild's pid")
}

// TestProgram_Kill_KillsGrandchild is the Program-level counterpart to
// TestExecCmd_ProcessKill_KillsGrandchild: it exercises Program's actual,
// non-test construction path in startCommand (a direct exec.CommandContext
// call with its own setProcAttr wiring), not ExecCommandBuilder.New nor a
// MockCommander. All other Program tests use MockCommander, so a regression
// in that separate wiring -- e.g. someone adding a new Program construction
// path and forgetting setProcAttr, as the original version of this PR did
// -- would otherwise ship with every existing test still green.
//
// Deliberately not t.Parallel(): Program.Run changes the process-wide
// working directory for the duration of starting the command (see
// changeToWorkDir in program.go), which already races with other tests'
// os.Chdir calls (e.g. TestProgram_Run's ChdirFails/PhysicalChdir cases) if
// run concurrently with them -- a pre-existing issue unrelated to this
// PR's process-cleanup fix. Running serially avoids adding to that window.
func TestProgram_Kill_KillsGrandchild(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "grandchild-started")
	marker := filepath.Join(dir, "grandchild-ran")
	script := fmt.Sprintf(`(: > %q; sleep 0.3 && : > %q) & wait`, started, marker)

	prog := rubrics.New(t.TempDir(), "")
	require.NoError(t, prog.Run(t.Context(), "sh", "-c", script))

	// Wait for the grandchild itself (not just the parent shell) to have
	// actually started, rather than a fixed sleep: a fixed delay could
	// elapse before the grandchild forks on a slow/loaded runner, in which
	// case Program.Kill would kill the shell before the grandchild ever
	// existed and this test would pass without proving anything.
	require.Eventually(t, func() bool {
		_, err := os.Stat(started)
		return err == nil
	}, time.Second, 10*time.Millisecond, "grandchild never started")

	require.NoError(t, prog.Kill())

	// Wait well past the grandchild's scheduled side effect (0.3s) to prove
	// it never got the chance to run.
	time.Sleep(600 * time.Millisecond)
	_, err := os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist, "grandchild survived Program.Kill and wrote its marker file")
}

// TestProgram_ContextCancel_KillsGrandchild proves the actual, real-world
// trigger for gradebot not cleaning up a student's program on exit: nothing
// in this codebase calls ProgramRunner.Kill directly. Every real caller
// drives a program's lifetime by canceling spawnCtx (e.g. the root context
// tied to SIGINT/SIGTERM in main.go), and relied on exec.CommandContext's
// default behavior on cancellation -- which only kills the direct child and
// never reaps it. This test cancels the context instead of calling Kill,
// and expects the same group-kill-and-reap guarantee as an explicit Kill.
//
// Not t.Parallel(), for the same os.Chdir reason as TestProgram_Kill_KillsGrandchild.
func TestProgram_ContextCancel_KillsGrandchild(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "grandchild-started")
	marker := filepath.Join(dir, "grandchild-ran")
	script := fmt.Sprintf(`(: > %q; sleep 0.3 && : > %q) & wait`, started, marker)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	prog := rubrics.New(t.TempDir(), "")
	require.NoError(t, prog.Run(ctx, "sh", "-c", script))

	require.Eventually(t, func() bool {
		_, err := os.Stat(started)
		return err == nil
	}, time.Second, 10*time.Millisecond, "grandchild never started")

	cancel()

	// Wait well past the grandchild's scheduled side effect (0.3s) to prove
	// it never got the chance to run.
	time.Sleep(600 * time.Millisecond)
	_, err := os.Stat(marker)
	require.ErrorIs(t, err, os.ErrNotExist, "grandchild survived context cancellation and wrote its marker file")
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

package rubrics

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// processReapTimeout bounds how long ProcessKill waits for the killed
// process to actually exit. SIGKILL is unblockable, so in practice this
// should return almost immediately; the bound exists only to guarantee
// ProcessKill can't hang forever in the pathological case of a process
// stuck in uninterruptible sleep.
const processReapTimeout = 3 * time.Second

// Commander defines an interface that wraps an external command.
// This is a subset of exec.Cmd to allow for mocking.
type (
	Commander interface {
		SetDir(dir string)
		SetEnv(env []string)
		StreamSetter

		// Start begins execution without waiting for completion.
		// Run may block until the process exits.
		Start() error
		Run() error
		ProcessKill() error
	}
	StreamSetter interface {
		SetStdin(stdin io.Reader)
		SetStdout(stdout io.Writer)
		SetStderr(stderr io.Writer)
	}
)

// execCmd is the production implementation of Commander, wrapping exec.Cmd.
type execCmd struct {
	*exec.Cmd

	// killOnce makes ProcessKill idempotent and safe to call more than once
	// on the same Commander (e.g. Program's sendToStdin can already invoke
	// cleanup on a wedged write before a caller explicitly kills the same
	// Commander again).
	killOnce sync.Once
	killErr  error
}

func (c *execCmd) SetDir(dir string) {
	c.Dir = dir
}

func (c *execCmd) SetEnv(env []string) {
	c.Env = env
}

func (c *execCmd) SetStdin(stdin io.Reader) {
	c.Stdin = stdin
}

func (c *execCmd) SetStdout(stdout io.Writer) {
	c.Stdout = stdout
}

func (c *execCmd) SetStderr(stderr io.Writer) {
	c.Stderr = stderr
}

// ProcessKill terminates the child process and reaps it so it doesn't
// linger as a zombie. On POSIX it also kills the process group the child
// leads, catching subprocesses the child may have spawned (e.g. a student
// program run via a shell wrapper); Windows and other non-Unix targets
// (runner_windows.go, runner_other.go) have no
// process-group equivalent here and only kills the direct child.
//
// If the process was already reaped by an earlier Run/Wait (c.ProcessState
// set), ProcessKill is a no-op: the OS may since have recycled that pid for
// an unrelated process, so it's no longer safe to signal it as a group.
//
// Reaping goes through the single real exec.Cmd.Wait -- not a separate
// Process.Wait -- so there's exactly one wait owner and Cmd's own I/O-pipe
// cleanup reliably runs. Cmd.Wait would normally be able to block forever
// here: when Stdin isn't an *os.File (Program's default is an unwritten
// *io.PipeReader), Wait also waits for the stdin-copy goroutine, which can
// block on a Read from Stdin regardless of WaitDelay. So before waiting,
// ProcessKill proactively closes Stdin itself (when it's not an *os.File
// and implements io.Closer) to unblock that goroutine. The wait is further
// bounded by processReapTimeout as a last-resort safety net and, unlike a
// silently-discarded background wait, a timeout is reported as an error
// rather than treated as a successful kill.
func (c *execCmd) ProcessKill() error {
	if c.Process == nil {
		return nil
	}
	c.killOnce.Do(func() {
		if c.ProcessState != nil {
			return
		}

		killErr := killProcessGroup(c.Cmd)
		unblockStdinCopy(c.Cmd)

		done := make(chan struct{})
		go func() {
			_ = c.Wait() // expected to report the process was killed; that's fine
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(processReapTimeout):
			c.killErr = fmt.Errorf("process %d did not exit within %s of being killed", c.Process.Pid, processReapTimeout)
			return
		}

		if killErr != nil && !isAlreadyExited(killErr) {
			c.killErr = killErr
		}
	})
	return c.killErr
}

// unblockStdinCopy closes cmd's Stdin if doing so is necessary and safe:
// necessary because exec.Cmd's internal stdin-copy goroutine can otherwise
// block Wait forever on a Read that nothing will ever satisfy or error out
// (e.g. Program's owned, unwritten io.Pipe); safe because when Stdin is an
// *os.File, exec.Cmd connects it to the child directly with no copy
// goroutine involved, so closing it here would only take away a file the
// caller may still want and buys nothing.
func unblockStdinCopy(cmd *exec.Cmd) {
	if _, isFile := cmd.Stdin.(*os.File); isFile {
		return
	}
	if closer, ok := cmd.Stdin.(io.Closer); ok {
		_ = closer.Close()
	}
}

// Start starts the command without waiting for it to exit. This allows callers
// to interact with the process via stdin/stdout using the provided buffers.
func (c *execCmd) Start() error {
	return c.Cmd.Start()
}

// Run runs the command and waits for it to exit (start + wait). This mirrors
// the behavior of exec.Cmd.Run for callers that expect the command to
// complete before returning.
func (c *execCmd) Run() error {
	return c.Cmd.Run()
}

// ProgramRunner is the interface used by rubrics to run student programs.
// It is declared here so runner-related abstractions live together.
type ProgramRunner interface {
	Path() string
	Run(ctx context.Context, args ...string) error
	Do(in string) (stdout, stderr []string, err error)
	Kill() error
	Cleanup(ctx context.Context) error
}

// ExecCommandBuilder creates Commander instances with environment settings.
// It is used to factory Commander instances for program execution.
type ExecCommandBuilder struct {
	Env map[string]string
}

// New creates a new Commander bound to ctx, with the configured environment.
//
// BREAKING CHANGE: prior to this version, New took no ctx parameter and
// ExecCommandBuilder instead stored it on an exported Context field
// (flagged by SonarCloud S8242 as an anti-pattern -- a context tied to the
// struct's lifetime rather than the call it governs). Callers must now pass
// ctx explicitly and drop any use of the removed Context field.
func (b *ExecCommandBuilder) New(ctx context.Context, name string, args ...string) Commander {
	cmd := exec.CommandContext(ctx, name, args...)
	setProcAttr(cmd)
	execCmd := &execCmd{Cmd: cmd}

	if b.Env != nil {
		env := make([]string, 0, len(b.Env))
		for k, v := range b.Env {
			env = append(env, k+"="+v)
		}
		execCmd.SetEnv(env)
	}

	return execCmd
}

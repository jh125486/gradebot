package rubrics

import (
	"context"
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
// program run via a shell wrapper); Windows (runner_windows.go) has no
// process-group equivalent here and only kills the direct child.
//
// Reaping uses Process.Wait, not Cmd.Wait: when Stdin isn't an *os.File
// (Program's default is an unwritten *io.PipeReader), Cmd.Wait also blocks
// until the stdin-copy goroutine returns, and per the exec package docs
// that can block on a Read from Stdin regardless of WaitDelay -- nothing
// closes our own pipe until later, in Program.Kill's resetPipe. Process.Wait
// reaps the OS-level zombie directly without waiting on that goroutine, so
// ProcessKill can't be blocked by it; it's further bounded by
// processReapTimeout in case the killed process doesn't exit promptly.
// Once Cmd.Wait's usual unblocking condition does occur (immediately, via
// resetPipe, or whenever this Commander is otherwise abandoned), a
// background Cmd.Wait call releases the I/O pipes/copy goroutines Cmd
// itself owns.
func (c *execCmd) ProcessKill() error {
	if c.Process == nil {
		return nil
	}
	c.killOnce.Do(func() {
		err := killProcessGroup(c.Cmd)
		reapWithTimeout(c.Process, processReapTimeout)
		go func() { _ = c.Wait() }() // best-effort I/O pipe cleanup, see doc comment
		if err != nil && !isAlreadyExited(err) {
			c.killErr = err
		}
	})
	return c.killErr
}

// reapWithTimeout waits for proc to exit, giving up after d elapses.
func reapWithTimeout(proc *os.Process, d time.Duration) {
	done := make(chan struct{})
	go func() {
		_, _ = proc.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
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

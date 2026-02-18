package rubrics

import (
	"context"
	"io"
	"os/exec"
)

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

func (c *execCmd) ProcessKill() error {
	if c.Process != nil {
		return c.Process.Kill()
	}
	return nil
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

// ExecCommandBuilder creates Commander instances with context and environment settings.
// It is used to factory Commander instances for program execution.
type ExecCommandBuilder struct {
	Context context.Context
	Env     map[string]string
}

// New creates a new Commander with the configured context and environment.
func (b *ExecCommandBuilder) New(name string, args ...string) Commander {
	cmd := exec.CommandContext(b.Context, name, args...)
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

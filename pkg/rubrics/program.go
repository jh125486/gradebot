package rubrics

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SafeBuffer is a thread-safe bytes.Buffer wrapper that uses a mutex to protect
// concurrent reads and writes. It is safe to use from multiple goroutines.
type SafeBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

// Write implements io.Writer with mutex protection.
func (sb *SafeBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

// Len returns the number of bytes in the buffer.
func (sb *SafeBuffer) Len() int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Len()
}

// String returns the contents of the buffer as a string.
func (sb *SafeBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.String()
}

// CommandBuilder is a function that creates a Commander for executing commands.
// It is used for dependency injection to allow for testable command execution.
type CommandBuilder func(name string, args ...string) Commander

// Program implements the ProgramRunner interface using a CommandBuilder
// to allow for testable command execution.
type Program struct {
	workDir        string
	runCmd         []string
	env            []string
	commandBuilder CommandBuilder

	out    SafeBuffer
	errOut SafeBuffer

	inputWriter io.WriteCloser
	inputReader io.Reader

	cleanup func() error
}

// New creates a new Program instance.
func New(workDir, runCmd string, opts ...func(*Program)) *Program {
	// Convert relative paths to absolute paths to avoid issues with os.Chdir
	if absDir, err := filepath.Abs(workDir); err == nil {
		workDir = absDir
	}

	pr, pw := io.Pipe()

	p := &Program{
		workDir:     workDir,
		runCmd:      strings.Fields(runCmd),
		env:         os.Environ(),
		inputReader: pr,
		inputWriter: pw,
		cleanup:     func() error { return nil }, // Default no-op cleanup
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// NewWithCommander creates a new Program instance with a custom Commander.
func NewWithCommander(workDir, runCmd string, commander Commander) *Program {
	p := New(workDir, runCmd, WithCommandBuilder(func(_ string, _ ...string) Commander {
		// Return the same commander for testing purposes
		return commander
	}))
	return p
}

// WithReaderWriter configures the Program to use the provided reader and writer.
func WithReaderWriter(reader io.Reader, writer io.WriteCloser) func(*Program) {
	return func(p *Program) {
		p.inputReader = reader
		p.inputWriter = writer
	}
}

// WithEnv configures the Program to use the provided environment variables.
func WithEnv(env map[string]string) func(*Program) {
	return func(p *Program) {
		for k, v := range env {
			p.env = append(p.env, fmt.Sprintf("%s=%s", k, v))
		}
	}
}

// WithCommandBuilder configures the Program to use a custom command builder.
func WithCommandBuilder(builder CommandBuilder) func(*Program) {
	return func(p *Program) {
		p.commandBuilder = builder
	}
}

// Path returns the working directory path
func (p *Program) Path() string { return p.workDir }

// Run starts the program with the given arguments
func (p *Program) Run(ctx context.Context, args ...string) (err error) {
	cmdName, cmdArgs := p.resolveCommand(args)
	if cmdName == "" {
		return fmt.Errorf("no run command configured")
	}

	restore, err := p.changeToWorkDir()
	if err != nil {
		return err
	}
	defer func() {
		if restoreErr := restore(); restoreErr != nil {
			err = restoreErr
		}
	}()

	err = p.startCommand(ctx, cmdName, cmdArgs)

	return err
}

func (p *Program) changeToWorkDir() (func() error, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to determine working directory: %w", err)
	}

	if err := os.Chdir(p.workDir); err != nil {
		return nil, err
	}

	return func() error {
		return os.Chdir(currentDir)
	}, nil
}

func (p *Program) resolveCommand(args []string) (cmdName string, cmdArgs []string) {
	switch {
	case len(args) == 0 && len(p.runCmd) == 0:
		return "", nil
	case len(args) == 0:
		return p.runCmd[0], copyArgs(p.runCmd[1:])
	case len(p.runCmd) == 0:
		return args[0], copyArgs(args[1:])
	default:
		return p.runCmd[0], copyArgs(args)
	}
}

func (p *Program) startCommand(ctx context.Context, cmdName string, cmdArgs []string) error {
	var cmd Commander
	if p.commandBuilder != nil {
		cmd = p.commandBuilder(cmdName, cmdArgs...)
	} else {
		cmd = &execCmd{
			Cmd: exec.CommandContext(ctx, cmdName, cmdArgs...),
		}
	}
	cmd.SetDir(p.workDir)
	cmd.SetEnv(p.env)
	cmd.SetStdin(p.inputReader)
	cmd.SetStdout(&p.out)
	cmd.SetStderr(&p.errOut)

	// Save cleanup function to kill process later
	if execCmd, ok := cmd.(*execCmd); ok {
		p.cleanup = execCmd.ProcessKill
	} else {
		// For mocked or custom commanders, store the commander for cleanup
		p.cleanup = cmd.ProcessKill
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	return nil
}

// Do sends input to the running program and returns captured output
func (p *Program) Do(in string) (stdout, stderr []string, err error) {
	if err := p.sendToStdin(in); err != nil {
		return nil, nil, err
	}

	prevOutLen := p.out.Len()
	prevErrLen := p.errOut.Len()

	p.waitForOutput(prevOutLen, prevErrLen)

	outStr, errStr := p.latestOutput(prevOutLen, prevErrLen)
	return splitLines(outStr), splitLines(errStr), nil
}

func (p *Program) sendToStdin(in string) error {
	if p.inputWriter == nil {
		return nil
	}
	_, err := p.inputWriter.Write([]byte(in + "\n"))
	return err
}

func (p *Program) waitForOutput(prevOutLen, prevErrLen int) {
	if p.inputWriter == nil {
		return
	}

	deadline := time.Now().Add(750 * time.Millisecond)
	for time.Now().Before(deadline) {
		if p.out.Len() > prevOutLen || p.errOut.Len() > prevErrLen {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (p *Program) latestOutput(prevOutLen, prevErrLen int) (stdout, stderr string) {
	stdout = p.out.String()
	if prevOutLen < len(stdout) {
		stdout = stdout[prevOutLen:]
	} else {
		stdout = ""
	}

	stderr = p.errOut.String()
	if prevErrLen < len(stderr) {
		stderr = stderr[prevErrLen:]
	} else {
		stderr = ""
	}

	return stdout, stderr
}

func splitLines(s string) []string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

func copyArgs(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

// Kill terminates the running program process
func (p *Program) Kill() error {
	return p.cleanup()
}

// Cleanup prepares the program environment for a fresh run by removing
// persistent data files. This is a no-op for the base Program implementation.
// Course-specific implementations should override this to remove data files.
func (p *Program) Cleanup(_ context.Context) error {
	// Base implementation does nothing - course repos can wrap or extend
	return nil
}

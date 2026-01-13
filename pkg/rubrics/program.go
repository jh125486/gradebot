package rubrics

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
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

// Program implements the ProgramRunner interface using a CommandBuilder
// to allow for testable command execution.
type Program struct {
	WorkDir    string
	RunCmd     []string
	cmdBuilder CommandBuilder

	cmd    Commander
	out    SafeBuffer
	errOut SafeBuffer

	inputWriter io.WriteCloser
	inputReader io.Reader
}

// NewProgram creates a new Program instance.
func NewProgram(workDir, runCmd string, builder CommandBuilder, opts ...func(*Program)) *Program {
	// Convert relative paths to absolute paths to avoid issues with os.Chdir
	if absDir, err := filepath.Abs(workDir); err == nil {
		workDir = absDir
	}

	pr, pw := io.Pipe()

	p := &Program{
		WorkDir:     workDir,
		RunCmd:      strings.Fields(runCmd),
		cmdBuilder:  builder,
		inputReader: pr,
		inputWriter: pw,
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// WithReaderWriter configures the Program to use the provided reader and writer.
func WithReaderWriter(reader io.Reader, writer io.WriteCloser) func(*Program) {
	return func(p *Program) {
		p.inputReader = reader
		p.inputWriter = writer
	}
}

// Path returns the working directory path
func (p *Program) Path() string { return p.WorkDir }

// Run starts the program with the given arguments
func (p *Program) Run(args ...string) (err error) {
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

	err = p.startCommand(cmdName, cmdArgs)

	return err
}

func (p *Program) changeToWorkDir() (func() error, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to determine working directory: %w", err)
	}

	if err := os.Chdir(p.WorkDir); err != nil {
		return nil, err
	}

	return func() error {
		return os.Chdir(currentDir)
	}, nil
}

func (p *Program) resolveCommand(args []string) (cmdName string, cmdArgs []string) {
	switch {
	case len(args) == 0 && len(p.RunCmd) == 0:
		return "", nil
	case len(args) == 0:
		return p.RunCmd[0], copyArgs(p.RunCmd[1:])
	case len(p.RunCmd) == 0:
		return args[0], copyArgs(args[1:])
	default:
		return p.RunCmd[0], copyArgs(args)
	}
}

func (p *Program) startCommand(cmdName string, cmdArgs []string) error {
	if p.cmdBuilder == nil {
		return nil
	}

	p.cmd = p.cmdBuilder.New(cmdName, cmdArgs...)
	p.cmd.SetDir(p.WorkDir)

	p.cmd.SetStdin(p.inputReader)
	p.cmd.SetStdout(&p.out)
	p.cmd.SetStderr(&p.errOut)

	return p.cmd.Start()
}

// Do sends input to the running program and returns captured output
func (p *Program) Do(in string) (stdout, stderr []string, err error) {
	if p.cmd == nil {
		return nil, nil, nil
	}

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
	if p.inputWriter == nil || p.cmd == nil {
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
	if p.cmd != nil {
		return p.cmd.ProcessKill()
	}
	return nil
}

// Cleanup prepares the program environment for a fresh run by removing
// persistent data files. This is a no-op for the base Program implementation.
// Course-specific implementations should override this to remove data files.
func (p *Program) Cleanup(_ context.Context) error {
	// Base implementation does nothing - course repos can wrap or extend
	return nil
}

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
	ownsPipe    bool // true when inputReader/inputWriter is our own io.Pipe(), not caller-supplied via WithReaderWriter

	// spawnCtx is the context used for every exec.CommandContext call, captured
	// once from whichever Run() call spawns the process first. Callers (e.g.
	// ExecuteProject) typically give each rubric item its own short-lived
	// context that's cancelled as soon as that item returns; exec.CommandContext
	// kills the process the moment its context is done, regardless of item
	// boundaries. Reusing the first ctx for every respawn keeps the process's
	// lifetime tied to the overall run instead of whichever evaluator happened
	// to trigger the (re)spawn.
	//
	// NOSONAR(godre:S8242): deliberately stored, not a parameter — see above.
	spawnCtx context.Context

	cleanup func() error
	running bool
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
		ownsPipe:    true,
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
		p.ownsPipe = false
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

// Run starts the program with the given arguments.
// If the program is already running, Run is a no-op and returns nil.
func (p *Program) Run(ctx context.Context, args ...string) (err error) {
	if p.running {
		return nil
	}

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
	if p.spawnCtx == nil {
		p.spawnCtx = ctx
	}

	var cmd Commander
	if p.commandBuilder != nil {
		cmd = p.commandBuilder(cmdName, cmdArgs...)
	} else {
		cmd = &execCmd{
			Cmd: exec.CommandContext(p.spawnCtx, cmdName, cmdArgs...),
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

	p.running = true
	return nil
}

// Do sends input to the running program and returns captured output
func (p *Program) Do(in string) (stdout, stderr []string, err error) {
	prevOutLen := p.out.Len()
	prevErrLen := p.errOut.Len()

	if err := p.sendToStdin(in); err != nil {
		return nil, nil, err
	}

	p.waitForOutput(prevOutLen, prevErrLen)

	outStr, errStr := p.latestOutput(prevOutLen, prevErrLen)
	return splitLines(outStr), splitLines(errStr), nil
}

func (p *Program) sendToStdin(in string) error {
	// Snapshot the writer before launching the goroutine below: if this
	// write times out, resetPipe reassigns p.inputWriter to a new pipe
	// while that goroutine is still blocked on the old one. Reading
	// p.inputWriter from inside the goroutine would race with that
	// reassignment; writing to this local copy instead does not.
	writer := p.inputWriter
	if writer == nil {
		return nil
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte(in + "\n"))
		errCh <- err
	}()

	select {
	case err := <-errCh:
		return err
	case <-time.After(750 * time.Millisecond):
		// The process is presumed wedged/dead. Actually kill it (not just
		// flip p.running) and reset the pipe: otherwise the next Run() spawns
		// a second, still-alive process sharing this pipe with the abandoned
		// one, and the two processes' stdin-bridge goroutines race for every
		// future write -- silently splitting the command stream between them.
		p.running = false
		_ = p.cleanup()
		p.resetPipe()
		return fmt.Errorf("stdin write timed out")
	}
}

// resetPipe replaces the stdin pipe with a fresh one, closing the old writer
// first so any goroutine still reading/writing the old pipe (e.g. the
// abandoned process's stdin-bridge goroutine spawned internally by os/exec)
// is released instead of lingering to contend with the next process. This is
// a no-op when the reader/writer were supplied via WithReaderWriter, since
// callers own that lifecycle themselves.
func (p *Program) resetPipe() {
	if !p.ownsPipe {
		return
	}
	_ = p.inputWriter.Close()
	pr, pw := io.Pipe()
	p.inputReader = pr
	p.inputWriter = pw
}

func (p *Program) waitForOutput(prevOutLen, prevErrLen int) {
	if p.inputWriter == nil {
		return
	}

	const (
		pollInterval = 10 * time.Millisecond
		quietPeriod  = 30 * time.Millisecond
	)
	deadline := time.Now().Add(750 * time.Millisecond)

	for time.Now().Before(deadline) {
		if p.out.Len() > prevOutLen || p.errOut.Len() > prevErrLen {
			break
		}
		time.Sleep(pollInterval)
	}

	// A response can be delivered across more than one Write (e.g. a
	// multi-line print flushed in separate chunks as it crosses the OS
	// pipe). Returning the instant the first byte shows up slices off a
	// partial response and leaks the rest into whatever the next Do()
	// call captures. Instead, keep polling until both buffers have gone
	// quiet (no growth) for a short period, bounded by the same deadline.
	lastOutLen, lastErrLen := p.out.Len(), p.errOut.Len()
	quietUntil := time.Now().Add(quietPeriod)
	for time.Now().Before(deadline) && time.Now().Before(quietUntil) {
		time.Sleep(pollInterval)
		outLen, errLen := p.out.Len(), p.errOut.Len()
		if outLen != lastOutLen || errLen != lastErrLen {
			lastOutLen, lastErrLen = outLen, errLen
			quietUntil = time.Now().Add(quietPeriod)
		}
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
	err := p.cleanup()
	if err == nil || isAlreadyExited(err) {
		p.running = false
	}

	// exec.Cmd spawns a background goroutine that copies from inputReader
	// into the child's real stdin whenever Stdin isn't an *os.File. That
	// goroutine only exits once Wait() sees the process exit and closes its
	// pipe -- but Kill() never waits, so the goroutine is left blocked
	// reading from inputReader indefinitely. On restart, Run() hands the new
	// process the *same* inputReader, so the new process's stdin-bridge
	// goroutine now competes with the still-blocked old one for every future
	// write on the pipe: a write can be silently handed to the dead
	// goroutine and lost instead of reaching the live process, permanently
	// desyncing the command/response stream. resetPipe closes the old writer
	// (releasing that goroutine with EOF) and hands the next Run() a
	// brand-new, uncontended pipe.
	p.resetPipe()

	return err
}

// isAlreadyExited reports whether the error from killing a process indicates
// the process had already exited (and is therefore no longer running).
func isAlreadyExited(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "process already finished") ||
		strings.Contains(msg, "os: process already finished") ||
		strings.Contains(msg, "invalid argument")
}

// Cleanup prepares the program environment for a fresh run by removing
// persistent data files. This is a no-op for the base Program implementation.
// Course-specific implementations should override this to remove data files.
func (p *Program) Cleanup(_ context.Context) error {
	// Base implementation does nothing - course repos can wrap or extend
	return nil
}

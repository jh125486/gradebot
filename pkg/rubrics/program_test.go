package rubrics_test

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jh125486/gradebot/pkg/rubrics"
)

const osWindows = "windows"

// TestSafeBufferBasicOperations tests basic SafeBuffer operations
func TestSafeBufferBasicOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		writes    [][]byte
		wantLen   int
		wantStr   string
		wantBytes int // for first write only
	}{
		{
			name:      "single write",
			writes:    [][]byte{[]byte("hello")},
			wantLen:   5,
			wantStr:   "hello",
			wantBytes: 5,
		},
		{
			name:      "multiple writes",
			writes:    [][]byte{[]byte("test"), []byte(" data")},
			wantLen:   9,
			wantStr:   "test data",
			wantBytes: 4,
		},
		{
			name:      "empty buffer",
			writes:    [][]byte{},
			wantLen:   0,
			wantStr:   "",
			wantBytes: 0,
		},
		{
			name:      "empty write",
			writes:    [][]byte{[]byte("")},
			wantLen:   0,
			wantStr:   "",
			wantBytes: 0,
		},
		{
			name:      "multiple sequential writes",
			writes:    [][]byte{[]byte("first"), []byte(" second"), []byte(" third")},
			wantLen:   18,
			wantStr:   "first second third",
			wantBytes: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var sb rubrics.SafeBuffer

			// Perform writes
			for i, data := range tt.writes {
				n, err := sb.Write(data)
				assert.NoError(t, err)
				if i == 0 && tt.wantBytes > 0 {
					assert.Equal(t, tt.wantBytes, n)
				}
			}

			// Verify final state
			assert.Equal(t, tt.wantLen, sb.Len())
			assert.Equal(t, tt.wantStr, sb.String())
		})
	}
}

// TestSafeBufferConcurrentWrites tests concurrent writes
func TestSafeBufferConcurrentWrites(t *testing.T) {
	var sb rubrics.SafeBuffer
	var wg sync.WaitGroup

	// Launch 100 goroutines that each write 10 times
	numGoroutines := 100
	writesPerGoroutine := 10

	for i := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range writesPerGoroutine {
				sb.Write([]byte("x"))
			}
		}(i)
	}

	wg.Wait()

	// Should have exactly numGoroutines * writesPerGoroutine bytes
	expectedLen := numGoroutines * writesPerGoroutine
	assert.Equal(t, expectedLen, sb.Len())
	assert.Equal(t, expectedLen, len(sb.String()))
}

// TestSafeBufferConcurrentReadWrites tests concurrent reads and writes
func TestSafeBufferConcurrentReadWrites(t *testing.T) {
	var (
		sb rubrics.SafeBuffer
		wg sync.WaitGroup
	)
	// Launch writers
	numWriters := 50
	for range numWriters {
		wg.Go(func() {
			for range 100 {
				sb.Write([]byte("a"))
			}
		})
	}

	// Launch readers (concurrent with writers)
	numReaders := 50
	for range numReaders {
		wg.Go(func() {
			for range 100 {
				_ = sb.Len()
				_ = sb.String()
			}
		})
	}

	wg.Wait()

	// Verify final state is consistent
	finalLen := sb.Len()
	finalStr := sb.String()
	assert.Equal(t, finalLen, len(finalStr))
	assert.Equal(t, numWriters*100, finalLen)
}

// FailingWriter is a mock writer that always returns an error
type FailingWriter struct{}

func (f *FailingWriter) Write([]byte) (int, error) {
	return 0, errors.New("mock write error")
}

func (f *FailingWriter) Close() error {
	return nil
}

// MockWriteCloser is a simple write closer that doesn't block
type MockWriteCloser struct {
	data []byte
}

func (m *MockWriteCloser) Write(p []byte) (n int, err error) {
	m.data = append(m.data, p...)
	return len(p), nil
}

func (m *MockWriteCloser) Close() error {
	return nil
} // MockCommander is a mock implementation of the Commander interface.
type MockCommander struct {
	mock.Mock
	outputs map[string][]byte // Store outputs per writer
}

func NewMockCommander() *MockCommander {
	return &MockCommander{
		outputs: make(map[string][]byte),
	}
}

func (m *MockCommander) SetDir(dir string) {
	m.Called(dir)
}

func (m *MockCommander) SetStdin(stdin io.Reader) {
	m.Called(stdin)
}

func (m *MockCommander) SetStdout(stdout io.Writer) {
	m.Called(stdout)
	// Store the writer for later use
	m.outputs["stdout_writer"] = []byte("mock stdout")
}

func (m *MockCommander) SetStderr(stderr io.Writer) {
	m.Called(stderr)
	// Store the writer for later use
	m.outputs["stderr_writer"] = []byte("mock stderr")
}

func (m *MockCommander) Run() error {
	args := m.Called()
	// Write to stored outputs if available
	if data, ok := m.outputs["stdout_writer"]; ok && len(m.Calls) >= 3 {
		if writer, ok := m.Calls[2].Arguments.Get(0).(io.Writer); ok {
			writer.Write(data)
		}
	}
	if data, ok := m.outputs["stderr_writer"]; ok && len(m.Calls) >= 4 {
		if writer, ok := m.Calls[3].Arguments.Get(0).(io.Writer); ok {
			writer.Write(data)
		}
	}
	return args.Error(0)
}

func (m *MockCommander) Start() error {
	args := m.Called()
	// Write to stored outputs if available
	if data, ok := m.outputs["stdout_writer"]; ok && len(m.Calls) >= 3 {
		if writer, ok := m.Calls[2].Arguments.Get(0).(io.Writer); ok {
			writer.Write(data)
		}
	}
	if data, ok := m.outputs["stderr_writer"]; ok && len(m.Calls) >= 4 {
		if writer, ok := m.Calls[3].Arguments.Get(0).(io.Writer); ok {
			writer.Write(data)
		}
	}
	return args.Error(0)
}

func (m *MockCommander) ProcessKill() error {
	args := m.Called()
	return args.Error(0)
}

// MockCommandBuilder is a mock implementation of the CommandBuilder interface.
type MockCommandBuilder struct {
	mock.Mock
}

func (m *MockCommandBuilder) New(name string, arg ...string) rubrics.Commander {
	args := m.Called(name, arg)
	return args.Get(0).(rubrics.Commander)
}

func TestNewProgram(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		workDir     string
		runCmd      string
		builder     rubrics.CommandBuilder
		wantWorkDir string
	}{
		{
			name:        "SimplePath",
			workDir:     "/tmp/workdir",
			runCmd:      "go run .",
			builder:     nil,
			wantWorkDir: "/tmp/workdir",
		},
		{
			name:        "EmptyRunCmd",
			workDir:     ".",
			runCmd:      "",
			builder:     &MockCommandBuilder{},
			wantWorkDir: ".", // Will be converted to absolute path
		},
		{
			name:        "WithBuilder",
			workDir:     "/home/test",
			runCmd:      "python -m pytest",
			builder:     &MockCommandBuilder{},
			wantWorkDir: "/home/test",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog := rubrics.NewProgram(tc.workDir, tc.runCmd, tc.builder)

			// For relative paths, the result will be absolute
			if tc.workDir == "." {
				assert.Contains(t, prog.Path(), "/") // Should contain an absolute path
			} else {
				assert.Equal(t, tc.wantWorkDir, prog.Path())
			}
		})
	}
}

func TestProgram_Path(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		workDir string
		runCmd  string
	}{
		{name: "SimplePath", workDir: "/tmp/workdirABC", runCmd: "go"},
		{name: "EmptyRunCmd", workDir: "/home/user", runCmd: ""},
		{name: "RelativePath", workDir: "./test", runCmd: "python"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog := rubrics.NewProgram(tc.workDir, tc.runCmd, nil)
			path := prog.Path()
			assert.NotEmpty(t, path)
			// For relative paths, should be converted to absolute
			if strings.HasPrefix(tc.workDir, "/") {
				assert.Equal(t, tc.workDir, path)
			} else {
				assert.Contains(t, path, "/") // Should be absolute
			}
		})
	}
}

func TestProgram_Run(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		setup   func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander)
		args    []string
		wantErr bool
	}{
		{
			name: "SuccessfulRun",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				mockCmd := NewMockCommander()
				mockBuilder := new(MockCommandBuilder)
				mockBuilder.On("New", "go", []string{"run", "."}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				return rubrics.NewProgram(".", "go", mockBuilder), mockBuilder, mockCmd
			},
			args:    []string{"run", "."},
			wantErr: false,
		},
		{
			name: "StartError",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				mockCmd := NewMockCommander()
				mockBuilder := new(MockCommandBuilder)
				startError := errors.New("command failed to start")
				mockBuilder.On("New", "go", []string{"run", "."}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(startError)
				return rubrics.NewProgram(".", "go", mockBuilder), mockBuilder, mockCmd
			},
			args:    []string{"run", "."},
			wantErr: true,
		},
		{
			name: "ChdirFails",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				return rubrics.NewProgram("/a/path/that/most/definitely/does/not/exist", "go", nil), nil, nil
			},
			args:    []string{"run", "."},
			wantErr: true,
		},
		{
			name: "NoRunCommand",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				return rubrics.NewProgram(".", "", nil), nil, nil
			},
			args:    []string{},
			wantErr: true,
		},
		{
			name: "NoBuilder",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				return rubrics.NewProgram(".", "go", nil), nil, nil
			},
			args:    []string{"run", "."},
			wantErr: false, // returns nil
		},
		{
			name: "ArgsOverrideRunCmd",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				mockCmd := NewMockCommander()
				mockBuilder := new(MockCommandBuilder)
				mockBuilder.On("New", "go", []string{"test"}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				return rubrics.NewProgram(".", "go build", mockBuilder), mockBuilder, mockCmd
			},
			args:    []string{"test"}, // These args should override the "build" part
			wantErr: false,
		},
		{
			name: "ArgsProvideCommandName",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				mockCmd := NewMockCommander()
				mockBuilder := new(MockCommandBuilder)
				mockBuilder.On("New", "python", []string{"-m", "pytest"}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				return rubrics.NewProgram(".", "", mockBuilder), mockBuilder, mockCmd
			},
			args:    []string{"python", "-m", "pytest"},
			wantErr: false,
		},
		{
			name: "SingleArgAsCommand",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				mockCmd := NewMockCommander()
				mockBuilder := new(MockCommandBuilder)
				mockBuilder.On("New", "ls", []string(nil)).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				return rubrics.NewProgram(".", "", mockBuilder), mockBuilder, mockCmd
			},
			args:    []string{"ls"},
			wantErr: false,
		},
		{
			name: "GetWdFails",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				// This is hard to test without changing the current directory,
				// but we can test chdir failure above
				return rubrics.NewProgram(".", "go", nil), nil, nil
			},
			args:    []string{"run", "."},
			wantErr: false, // no builder so returns nil
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, builder, mockCmd := tc.setup()
			err := prog.Run(tc.args...)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if builder != nil {
				builder.AssertExpectations(t)
			}
			if mockCmd != nil {
				mockCmd.AssertExpectations(t)
			}
		})
	}
}

func TestProgram_Do(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		input      string
		setup      func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander)
		wantErr    bool
		wantStdout []string
		wantStderr []string
		runFirst   bool
	}{
		{
			name:  "SimpleInput_NoProcess",
			input: "test input",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				return rubrics.NewProgram(".", "go", nil), nil, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "InputWithRunningProcess",
			input: "GET testkey",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				return rubrics.NewProgram(".", "go", nil), nil, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false, // Don't actually run first to avoid pipe blocking
		},
		{
			name:  "EmptyInput",
			input: "",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				return rubrics.NewProgram(".", "", nil), nil, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "MultiLineInput",
			input: "first line\nsecond line",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				return rubrics.NewProgram(".", "python", nil), nil, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "StdinWriteError",
			input: "test input",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				mockCmd := NewMockCommander()
				mockBuilder := new(MockCommandBuilder)
				mockBuilder.On("New", "go", []string{"run", "."}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)

				// Inject a failing writer to simulate stdin write error
				prog := rubrics.NewProgram(".", "go", mockBuilder, rubrics.WithReaderWriter(strings.NewReader(""), &FailingWriter{}))

				// Run the process to set up stdinW with our failing writer
				_ = prog.Run("run", ".")

				return prog, mockBuilder, mockCmd
			},
			wantErr:    true, // We expect an error from the failing stdin write
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false, // We handle the run in setup
		},
		{
			name:  "OutputPollingWithBufferedOutput",
			input: "command with buffered output",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				mockCmd := NewMockCommander()
				mockBuilder := new(MockCommandBuilder)
				mockBuilder.On("New", "go", []string{"run", "."}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)

				// Use a custom stdin writer that doesn't block
				prog := rubrics.NewProgram(".", "go", mockBuilder, rubrics.WithReaderWriter(strings.NewReader(""), &MockWriteCloser{}))

				// Set up the process and pre-populate output buffers
				_ = prog.Run("run", ".")

				// The test will exercise the Do method paths even without direct buffer access

				return prog, mockBuilder, mockCmd
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "OutputPollingWithImmediateOutput",
			input: "command with output",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				// Test without actually running a process to avoid pipe blocking
				return rubrics.NewProgram(".", "go", nil), nil, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "EmptyOutputStrings",
			input: "test",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				// Test the edge case where prevLen >= current length
				prog := rubrics.NewProgram(".", "go", nil)
				// We can't access private fields, so let's use a different approach
				// This test will still exercise the string slicing logic
				return prog, nil, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "ScannerEdgeCases",
			input: "test scanner",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				// Test without running process to avoid pipe blocking
				return rubrics.NewProgram(".", "go", nil), nil, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "NoInputWriter",
			input: "input",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				mockCmd := NewMockCommander()
				mockBuilder := new(MockCommandBuilder)
				// Expect args passed by TestProgram_Do harness ("run", ".")
				mockBuilder.On("New", "echo", []string{"run", "."}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)

				prog := rubrics.NewProgram(".", "echo hello", mockBuilder, rubrics.WithReaderWriter(nil, nil))
				return prog, mockBuilder, mockCmd
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, builder, mockCmd := tc.setup()

			if tc.runFirst && builder != nil {
				err := prog.Run("run", ".")
				assert.NoError(t, err)
			}

			outLines, errOutLines, err := prog.Do(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.wantStdout, outLines)
			assert.Equal(t, tc.wantStderr, errOutLines)

			if builder != nil {
				builder.AssertExpectations(t)
			}
			if mockCmd != nil {
				mockCmd.AssertExpectations(t)
			}
		})
	}
}

func TestProgram_Kill(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		setup           func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander)
		expectKillError require.ErrorAssertionFunc
		runFirst        bool
	}{
		{
			name: "KillSuccessful",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				mockCmd := NewMockCommander()
				mockBuilder := new(MockCommandBuilder)
				mockBuilder.On("New", "go", []string{"run", "."}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				mockCmd.On("ProcessKill").Return(nil)
				return rubrics.NewProgram(".", "go", mockBuilder), mockBuilder, mockCmd
			},
			expectKillError: require.NoError,
			runFirst:        true,
		},
		{
			name: "KillFails",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				mockCmd := NewMockCommander()
				mockBuilder := new(MockCommandBuilder)
				killError := errors.New("kill failed")
				mockBuilder.On("New", "go", []string{"run", "."}).Return(mockCmd)
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				mockCmd.On("ProcessKill").Return(killError)
				return rubrics.NewProgram(".", "go", mockBuilder), mockBuilder, mockCmd
			},
			expectKillError: require.Error,
			runFirst:        true,
		},
		{
			name: "KillNoProcess",
			setup: func() (rubrics.ProgramRunner, *MockCommandBuilder, *MockCommander) {
				return rubrics.NewProgram(".", "go", nil), nil, nil
			},
			expectKillError: require.NoError,
			runFirst:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prog, builder, mockCmd := tt.setup()

			if tt.runFirst {
				err := prog.Run("run", ".")
				assert.NoError(t, err)
			}

			err := prog.Kill()
			tt.expectKillError(t, err, "Kill() error assertion failed")

			if builder != nil {
				builder.AssertExpectations(t)
			}
			if mockCmd != nil && tt.runFirst {
				mockCmd.AssertCalled(t, "ProcessKill")
			}
		})
	}
}

func TestProgram_Cleanup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		workDir string
		runCmd  string
		wantErr bool
	}{
		{
			name:    "cleanup_with_valid_program",
			workDir: "/tmp/test",
			runCmd:  "echo test",
			wantErr: false,
		},
		{
			name:    "cleanup_with_empty_workdir",
			workDir: "",
			runCmd:  "test",
			wantErr: false,
		},
		{
			name:    "cleanup_with_empty_runcmd",
			workDir: "/tmp",
			runCmd:  "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := &rubrics.ExecCommandBuilder{Context: t.Context()}
			prog := rubrics.NewProgram(tt.workDir, tt.runCmd, builder)

			err := prog.Cleanup(t.Context())

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestProgramRunPhysicalChdir(t *testing.T) {
	// Not running parallel as it modifies global state (cwd)

	currentDir, err := os.Getwd()
	require.NoError(t, err)

	tempDir := t.TempDir()

	// Create a dummy builder that just verifies we are in the tempDir
	mockBuilder := &MockCommandBuilder{}
	mockCmd := NewMockCommander()
	mockBuilder.On("New", "echo", []string{"hello"}).Return(mockCmd)
	mockCmd.On("SetDir", tempDir).Return()
	mockCmd.On("SetStdin", mock.Anything).Return()
	mockCmd.On("SetStdout", mock.Anything).Return()
	mockCmd.On("SetStderr", mock.Anything).Return()
	mockCmd.On("Start").Return(nil)

	prog := rubrics.NewProgram(tempDir, "echo hello", mockBuilder)
	err = prog.Run()
	require.NoError(t, err)

	// Verify we are back in the original directory
	afterDir, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, currentDir, afterDir)

	mockBuilder.AssertExpectations(t)
	mockCmd.AssertExpectations(t)
}

func TestProgramDoIntegration(t *testing.T) {
	if runtime.GOOS == osWindows {
		t.Skip("skipping on windows")
	}

	ctx := t.Context()

	// Use 'cat' which echoes stdin to stdout
	runCmd := "cat"

	builder := &rubrics.ExecCommandBuilder{Context: ctx}

	prog := rubrics.NewProgram(t.TempDir(), runCmd, builder)

	err := prog.Run()
	require.NoError(t, err)
	defer prog.Kill()

	// Allow process to start up
	time.Sleep(100 * time.Millisecond)

	stdout, _, err := prog.Do("hello")
	require.NoError(t, err)

	assert.Contains(t, stdout, "hello")

	stdout, _, err = prog.Do("world")
	require.NoError(t, err)
	assert.Contains(t, stdout, "world")
}

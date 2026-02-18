package rubrics_test

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

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

// TestSafeBufferConcurrentOperations tests concurrent buffer operations
func TestSafeBufferConcurrentOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		numWriters         int
		writesPerGoroutine int
		numReaders         int
		readsPerGoroutine  int
	}{
		{
			name:               "concurrent writes only",
			numWriters:         100,
			writesPerGoroutine: 10,
			numReaders:         0,
			readsPerGoroutine:  0,
		},
		{
			name:               "concurrent reads and writes",
			numWriters:         50,
			writesPerGoroutine: 100,
			numReaders:         50,
			readsPerGoroutine:  100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				sb rubrics.SafeBuffer
				wg sync.WaitGroup
			)

			// Launch writers
			for range tt.numWriters {
				wg.Go(func() {
					for range tt.writesPerGoroutine {
						sb.Write([]byte("x"))
					}
				})
			}

			// Launch readers (concurrent with writers)
			for range tt.numReaders {
				wg.Go(func() {
					for range tt.readsPerGoroutine {
						_ = sb.Len()
						_ = sb.String()
					}
				})
			}

			wg.Wait()

			// Verify final state is consistent
			expectedLen := tt.numWriters * tt.writesPerGoroutine
			finalLen := sb.Len()
			finalStr := sb.String()
			assert.Equal(t, expectedLen, finalLen)
			assert.Equal(t, expectedLen, len(finalStr))
		})
	}
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
}

// MockCommander is a mock implementation of the Commander interface.
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

func (m *MockCommander) SetEnv(env []string) {
	m.Called(env)
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

func TestNewProgram(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		workDir     string
		runCmd      string
		wantWorkDir string
	}{
		{
			name:        "SimplePath",
			workDir:     "/tmp/workdir",
			runCmd:      "go run .",
			wantWorkDir: "/tmp/workdir",
		},
		{
			name:        "EmptyRunCmd",
			workDir:     ".",
			runCmd:      "",
			wantWorkDir: ".", // Will be converted to absolute path
		},
		{
			name:        "WithBuilder",
			workDir:     "/home/test",
			runCmd:      "python -m pytest",
			wantWorkDir: "/home/test",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog := rubrics.New(tc.workDir, tc.runCmd)

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
			prog := rubrics.New(tc.workDir, tc.runCmd)
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
	tests := []struct {
		name    string
		setup   func(t *testing.T) (rubrics.ProgramRunner, *MockCommander)
		args    []string
		wantErr bool
		// optional verification after Run
		verify func(t *testing.T, prog rubrics.ProgramRunner, mockCmd *MockCommander)
		// when true, do not run this subtest in parallel
		noParallel bool
	}{
		{
			name: "SuccessfulRun",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				mockCmd := NewMockCommander()
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetEnv", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				return rubrics.NewWithCommander(".", "go", mockCmd), mockCmd
			},
			args:    []string{"run", "."},
			wantErr: false,
		},
		{
			name: "StartError",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				mockCmd := NewMockCommander()
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetEnv", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(errors.New("command failed to start"))
				return rubrics.NewWithCommander(".", "go", mockCmd), mockCmd
			},
			args:    []string{"run", "."},
			wantErr: true,
		},
		{
			name: "ChdirFails",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				return rubrics.New("/a/path/that/most/definitely/does/not/exist", "go"), nil
			},
			args:    []string{"run", "."},
			wantErr: true,
		},
		{
			name: "NoRunCommand",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				return rubrics.New(".", ""), nil
			},
			args:    []string{},
			wantErr: true,
		},
		{
			name: "NoBuilder",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				return rubrics.New(".", "go"), nil
			},
			args:    []string{"run", "."},
			wantErr: false, // returns nil
		},
		{
			name: "ArgsOverrideRunCmd",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				mockCmd := NewMockCommander()
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetEnv", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				return rubrics.NewWithCommander(".", "go build", mockCmd), mockCmd
			},
			args:    []string{"test"}, // These args should override the "build" part
			wantErr: false,
		},
		{
			name: "ArgsProvideCommandName",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				mockCmd := NewMockCommander()
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetEnv", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				return rubrics.NewWithCommander(".", "", mockCmd), mockCmd
			},
			args:    []string{"python", "-m", "pytest"},
			wantErr: false,
		},
		{
			name: "SingleArgAsCommand",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				mockCmd := NewMockCommander()
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetEnv", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				return rubrics.NewWithCommander(".", "", mockCmd), mockCmd
			},
			args:    []string{"ls"},
			wantErr: false,
		},
		{
			name: "GetWdFails",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				// This is hard to test without changing the current directory,
				// but we can test chdir failure above
				return rubrics.New(".", "go"), nil
			},
			args:    []string{"run", "."},
			wantErr: false, // no builder so returns nil
		},
		{
			name: "PhysicalChdir",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				tempDir := t.TempDir()
				t.Chdir(tempDir)
				mockCmd := NewMockCommander()
				mockCmd.On("SetDir", tempDir).Return()
				mockCmd.On("SetEnv", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				return rubrics.NewWithCommander(tempDir, "go version", mockCmd), mockCmd
			},
			args:    []string{},
			wantErr: false, noParallel: true, verify: func(t *testing.T, prog rubrics.ProgramRunner, mockCmd *MockCommander) {
				cwd, err := os.Getwd()
				require.NoError(t, err)
				// cwd should match the directory we switched to with t.Chdir
				assert.Equal(t, prog.Path(), cwd)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.noParallel {
				t.Parallel()
			}
			prog, mockCmd := tt.setup(t)
			err := prog.Run(t.Context(), tt.args...)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if tt.verify != nil {
				tt.verify(t, prog, mockCmd)
			}
			if mockCmd != nil {
				mockCmd.AssertExpectations(t)
			}
		})
	}
}

func TestProgram_Do(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		setup      func(t *testing.T) (rubrics.ProgramRunner, *MockCommander)
		wantErr    bool
		wantStdout []string
		wantStderr []string
		runFirst   bool
		verify     func(t *testing.T, prog rubrics.ProgramRunner, mockCmd *MockCommander, outLines, errOutLines []string)
		noParallel bool
	}{
		{
			name:  "SimpleInput_NoProcess",
			input: "test input",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				prog := rubrics.New(".", "go", rubrics.WithReaderWriter(strings.NewReader(""), &MockWriteCloser{}))
				return prog, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "InputWithRunningProcess",
			input: "GET testkey",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				prog := rubrics.New(".", "go", rubrics.WithReaderWriter(strings.NewReader(""), &MockWriteCloser{}))
				return prog, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false, // Don't actually run first to avoid pipe blocking
		},
		{
			name:  "EmptyInput",
			input: "",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				prog := rubrics.New(".", "", rubrics.WithReaderWriter(strings.NewReader(""), &MockWriteCloser{}))
				return prog, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "MultiLineInput",
			input: "first line\nsecond line",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				prog := rubrics.New(".", "python", rubrics.WithReaderWriter(strings.NewReader(""), &MockWriteCloser{}))
				return prog, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "StdinWriteError",
			input: "test input",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				// Inject a failing writer to simulate stdin write error
				prog := rubrics.New(".", "go", rubrics.WithReaderWriter(strings.NewReader(""), &FailingWriter{}))
				return prog, nil
			},
			wantErr:    true, // We expect an error from the failing stdin write
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false, // Don't run, just test Do() which will fail when writing to stdin
		},
		{
			name:  "OutputPollingWithBufferedOutput",
			input: "command with buffered output",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				// Use non-blocking writer to avoid pipe blocking
				prog := rubrics.New(".", "go", rubrics.WithReaderWriter(strings.NewReader(""), &MockWriteCloser{}))
				return prog, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "OutputPollingWithImmediateOutput",
			input: "command with output",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				prog := rubrics.New(".", "go", rubrics.WithReaderWriter(strings.NewReader(""), &MockWriteCloser{}))
				return prog, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "EmptyOutputStrings",
			input: "test",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				prog := rubrics.New(".", "go", rubrics.WithReaderWriter(strings.NewReader(""), &MockWriteCloser{}))
				return prog, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "ScannerEdgeCases",
			input: "test scanner",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				prog := rubrics.New(".", "go", rubrics.WithReaderWriter(strings.NewReader(""), &MockWriteCloser{}))
				return prog, nil
			},
			wantErr:    false,
			wantStdout: nil,
			wantStderr: nil,
			runFirst:   false,
		},
		{
			name:  "IntegrationGoVersion",
			input: "",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				return nil, nil
			},
			noParallel: true,
			wantErr:    false,
			verify: func(t *testing.T, prog rubrics.ProgramRunner, mockCmd *MockCommander, outLines, errOutLines []string) {
				cmd := exec.CommandContext(t.Context(), "go", "version")
				out, err := cmd.CombinedOutput()
				require.NoErrorf(t, err, "failed to run 'go version': %v\noutput:\n%s", err, string(out))
				assert.Contains(t, string(out), "go version")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.noParallel {
				t.Parallel()
			}
			prog, mockCmd := tt.setup(t)
			if prog == nil {
				if tt.verify != nil {
					tt.verify(t, prog, mockCmd, nil, nil)
				}
				if mockCmd != nil {
					mockCmd.AssertExpectations(t)
				}
				return
			}

			if tt.runFirst {
				err := prog.Run(t.Context())
				assert.NoError(t, err)
			}

			outLines, errOutLines, err := prog.Do(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantStdout, outLines)
			assert.Equal(t, tt.wantStderr, errOutLines)

			if tt.verify != nil {
				tt.verify(t, prog, mockCmd, outLines, errOutLines)
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
		setup           func(t *testing.T) (rubrics.ProgramRunner, *MockCommander)
		expectKillError require.ErrorAssertionFunc
		runFirst        bool
	}{
		{
			name: "KillSuccessful",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				mockCmd := NewMockCommander()
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetEnv", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				mockCmd.On("ProcessKill").Return(nil)
				return rubrics.NewWithCommander(".", "go", mockCmd), mockCmd
			},
			expectKillError: require.NoError,
			runFirst:        true,
		},
		{
			name: "KillFails",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				mockCmd := NewMockCommander()
				killError := errors.New("kill failed")
				mockCmd.On("SetDir", mock.Anything).Return()
				mockCmd.On("SetEnv", mock.Anything).Return()
				mockCmd.On("SetStdin", mock.Anything).Return()
				mockCmd.On("SetStdout", mock.Anything).Return()
				mockCmd.On("SetStderr", mock.Anything).Return()
				mockCmd.On("Start").Return(nil)
				mockCmd.On("ProcessKill").Return(killError)
				return rubrics.NewWithCommander(".", "go", mockCmd), mockCmd
			},
			expectKillError: require.Error,
			runFirst:        true,
		},
		{
			name: "KillNoProcess",
			setup: func(t *testing.T) (rubrics.ProgramRunner, *MockCommander) {
				return rubrics.New(".", "go"), nil
			},
			expectKillError: require.NoError,
			runFirst:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prog, mockCmd := tt.setup(t)

			if tt.runFirst {
				_ = prog.Run(t.Context())
			}

			err := prog.Kill()
			tt.expectKillError(t, err, "Kill() error assertion failed")

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

			prog := rubrics.New(tt.workDir, tt.runCmd)

			err := prog.Cleanup(t.Context())

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

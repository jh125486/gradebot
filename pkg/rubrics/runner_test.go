package rubrics_test

import (
	"bytes"
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jh125486/gradebot/pkg/rubrics"
)

func TestExecCommandBuilder_New(t *testing.T) {
	t.Parallel()

	type args struct {
		ctx  context.Context
		name string
		arg  []string
	}
	tests := []struct {
		name           string
		args           args
		env            map[string]string
		expectContains string
		skip           func() bool
	}{
		{
			name: "creates_commander_with_single_arg",
			args: args{
				ctx:  t.Context(),
				name: "echo",
				arg:  []string{"hello"},
			},
		},
		{
			name: "creates_commander_with_multiple_args",
			args: args{
				ctx:  t.Context(),
				name: "echo",
				arg:  []string{"hello", "world"},
			},
		},
		{
			name: "creates_commander_with_no_args",
			args: args{
				ctx:  t.Context(),
				name: "pwd",
				arg:  []string{},
			},
		},
		{
			name: "creates_commander_with_context",
			args: args{
				ctx:  t.Context(),
				name: "echo",
				arg:  []string{"test"},
			},
		},
		{
			name: "sets_env_on_command",
			args: args{
				ctx:  t.Context(),
				name: "env",
				arg:  []string{},
			},
			env: map[string]string{
				"GRADEBOT_ENV_TEST": "42",
			},
			expectContains: "GRADEBOT_ENV_TEST=42",
			skip:           func() bool { return runtime.GOOS == osWindows },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.skip != nil && tt.skip() {
				t.Skip("platform-specific")
			}

			builder := &rubrics.ExecCommandBuilder{Context: tt.args.ctx, Env: tt.env}
			cmd := builder.New(tt.args.name, tt.args.arg...)

			if tt.expectContains != "" {
				var stdout bytes.Buffer
				cmd.SetStdout(&stdout)

				err := cmd.Run()
				require.NoError(t, err)

				assert.Contains(t, stdout.String(), tt.expectContains)
				return
			}

			require.NotNil(t, cmd)
		})
	}
}

func TestExecCmd_SetDir(t *testing.T) {
	t.Parallel()

	type args struct {
		dir string
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "sets_directory_path",
			args: args{dir: "/tmp"},
		},
		{
			name: "sets_empty_directory",
			args: args{dir: ""},
		},
		{
			name: "sets_relative_path",
			args: args{dir: "./testdata"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := &rubrics.ExecCommandBuilder{Context: t.Context()}
			cmd := builder.New("echo", "test")
			cmd.SetDir(tt.args.dir)
			// Just verify it doesn't panic - actual dir check would require reflection
		})
	}
}

func TestExecCmd_SetStdin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "sets_stdin_with_string_reader",
			input: "hello world",
		},
		{
			name:  "sets_stdin_with_empty_reader",
			input: "",
		},
		{
			name:  "sets_stdin_with_multiline_input",
			input: "line1\nline2\nline3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := &rubrics.ExecCommandBuilder{Context: t.Context()}
			cmd := builder.New("cat")
			cmd.SetStdin(strings.NewReader(tt.input))
			// Just verify it doesn't panic
		})
	}
}

func TestExecCmd_SetStdout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{
			name: "sets_stdout_buffer",
		},
		{
			name: "sets_stdout_to_bytes_buffer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := &rubrics.ExecCommandBuilder{Context: t.Context()}
			cmd := builder.New("echo", "test")

			var stdout bytes.Buffer
			cmd.SetStdout(&stdout)
			// Just verify it doesn't panic
		})
	}
}

func TestExecCmd_SetStderr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{
			name: "sets_stderr_buffer",
		},
		{
			name: "sets_stderr_to_bytes_buffer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := &rubrics.ExecCommandBuilder{Context: t.Context()}
			cmd := builder.New("echo", "test")

			var stderr bytes.Buffer
			cmd.SetStderr(&stderr)
			// Just verify it doesn't panic
		})
	}
}

func TestExecCmd_ProcessKill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) rubrics.Commander
		wantErr bool
	}{
		{
			name: "kills_nil_process_without_error",
			setup: func(t *testing.T) rubrics.Commander {
				builder := &rubrics.ExecCommandBuilder{Context: t.Context()}
				return builder.New("echo", "hello")
			},
			wantErr: false,
		},
		{
			name: "kills_started_process",
			setup: func(t *testing.T) rubrics.Commander {
				builder := &rubrics.ExecCommandBuilder{Context: t.Context()}
				cmd := builder.New("sleep", "60")
				err := cmd.Start()
				require.NoError(t, err)
				return cmd
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := tt.setup(t)
			err := cmd.ProcessKill()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExecCmd_Start(t *testing.T) {
	t.Parallel()

	type args struct {
		name string
		arg  []string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "starts_valid_command",
			args: args{
				name: "echo",
				arg:  []string{"hello"},
			},
			wantErr: false,
		},
		{
			name: "starts_command_without_args",
			args: args{
				name: "pwd",
				arg:  []string{},
			},
			wantErr: false,
		},
		{
			name: "returns_error_for_nonexistent_command",
			args: args{
				name: "nonexistent_command_12345",
				arg:  []string{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := &rubrics.ExecCommandBuilder{Context: t.Context()}
			cmd := builder.New(tt.args.name, tt.args.arg...)

			err := cmd.Start()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Clean up started process
				_ = cmd.ProcessKill()
			}
		})
	}
}

func TestExecCmd_Run(t *testing.T) {
	t.Parallel()

	type args struct {
		name string
		arg  []string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "runs_and_completes_valid_command",
			args: args{
				name: "echo",
				arg:  []string{"hello"},
			},
			wantErr: false,
		},
		{
			name: "runs_command_without_args",
			args: args{
				name: "pwd",
				arg:  []string{},
			},
			wantErr: false,
		},
		{
			name: "returns_error_for_nonexistent_command",
			args: args{
				name: "nonexistent_command_67890",
				arg:  []string{},
			},
			wantErr: true,
		},
		{
			name: "runs_command_with_multiple_args",
			args: args{
				name: "echo",
				arg:  []string{"hello", "world"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := &rubrics.ExecCommandBuilder{Context: t.Context()}
			cmd := builder.New(tt.args.name, tt.args.arg...)

			err := cmd.Run()

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestExecCommandBuilder_Integration(t *testing.T) {
	t.Parallel()

	builder := &rubrics.ExecCommandBuilder{Context: t.Context()}
	var cmd rubrics.Commander
	if runtime.GOOS == osWindows {
		cmd = builder.New("cmd", "/C", "echo", "hello")
	} else {
		cmd = builder.New("echo", "hello")
	}

	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)

	err := cmd.Run()

	assert.NoError(t, err)
	assert.Empty(t, stderr.String())
	assert.Equal(t, "hello\n", strings.ReplaceAll(stdout.String(), "\r\n", "\n"))
}

func TestExecCmd_ProcessKill_WithRunningProcess(t *testing.T) {
	t.Parallel()

	// Start a long-running process we can kill
	cmd := exec.CommandContext(t.Context(), "sleep", "60")
	require.NoError(t, cmd.Start())

	err := cmd.Process.Kill()
	assert.NoError(t, err)

	// Wait to reap the process
	_ = cmd.Wait()
}

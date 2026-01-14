package cli_test

import (
	"context"
	"io"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/jh125486/gradebot/pkg/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

func TestNewKongContext(t *testing.T) {
	t.Parallel()

	type args struct {
		ctx     context.Context
		name    string
		buildID string
		cli     any
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "creates_context_with_name",
			args: args{
				ctx:     t.Context(),
				name:    "test-app",
				buildID: "some-build-id",
				cli: &struct {
					Help bool `help:"Show help"`
				}{},
			},
		},
		{
			name: "binds_context_and_buildid",
			args: args{
				ctx:     context.WithValue(t.Context(), contextKey("test-key"), "test-value"),
				name:    "gradebot",
				buildID: "v1.2.3",
				cli: &struct {
					Version bool `help:"Show version"`
				}{},
			},
		},
		{
			name: "empty_name",
			args: args{
				ctx:     t.Context(),
				name:    "",
				buildID: "",
				cli:     &struct{}{},
			},
		},
		{
			name: "hashes_build_id_internally",
			args: args{
				ctx:     t.Context(),
				name:    "test",
				buildID: "my-build-id",
				cli:     &struct{}{},
			},
		},
		{
			name: "empty_build_id",
			args: args{
				ctx:     t.Context(),
				name:    "test",
				buildID: "",
				cli:     &struct{}{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Pass empty args to avoid parsing test flags from os.Args
			kctx := cli.NewKongContext(tt.args.ctx, tt.args.name, tt.args.buildID, tt.args.cli, []string{},
				kong.Exit(func(int) {}),
				kong.Writers(io.Discard, io.Discard),
			)

			require.NotNil(t, kctx, "Kong context should be created")
			assert.NotNil(t, kctx.Model, "Model should be set")
		})
	}
}

func TestNewKongContext_ErrorPaths(t *testing.T) {
	t.Parallel()

	type cliWithRequired struct {
		Required string `help:"Required argument" required:""`
	}

	type args struct {
		cli  any
		args []string
	}
	tests := []struct {
		name      string
		args      args
		wantPanic bool
	}{
		{
			name:      "invalid_cli_type_causes_panic",
			args:      args{cli: "not a struct pointer", args: []string{}},
			wantPanic: true,
		},
		{
			name:      "nil_cli_causes_panic",
			args:      args{cli: nil, args: []string{}},
			wantPanic: true,
		},
		{
			name:      "valid_cli_no_panic",
			args:      args{cli: &struct{}{}, args: []string{}},
			wantPanic: false,
		},
		{
			name:      "missing_required_arg",
			args:      args{cli: &cliWithRequired{}, args: []string{}},
			wantPanic: true,
		},
		{
			name:      "unknown_flag",
			args:      args{cli: &struct{}{}, args: []string{"--unknown-flag"}},
			wantPanic: true,
		},
		{
			name:      "valid_args_no_panic",
			args:      args{cli: &cliWithRequired{}, args: []string{"--required", "value"}},
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.wantPanic {
				assert.Panics(t, func() {
					cli.NewKongContext(t.Context(), "test", "", tt.args.cli, tt.args.args,
						kong.Exit(func(int) {}),
						kong.Writers(io.Discard, io.Discard),
					)
				})
			} else {
				assert.NotPanics(t, func() {
					kctx := cli.NewKongContext(t.Context(), "test", "", tt.args.cli, tt.args.args,
						kong.Exit(func(int) {}),
						kong.Writers(io.Discard, io.Discard),
					)
					assert.NotNil(t, kctx)
				})
			}
		})
	}
}

func TestContext_Wrapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ctx      context.Context
		validate func(*testing.T, cli.Context)
	}{
		{
			name: "wraps_todo_context",
			ctx:  t.Context(),
			validate: func(t *testing.T, wrapped cli.Context) {
				assert.NotNil(t, wrapped.Context)
			},
		},
		{
			name: "preserves_context_values",
			ctx:  context.WithValue(t.Context(), contextKey("key"), "value"),
			validate: func(t *testing.T, wrapped cli.Context) {
				assert.Equal(t, "value", wrapped.Value(contextKey("key")))
			},
		},
		{
			name: "preserves_cancellation",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			}(),
			validate: func(t *testing.T, wrapped cli.Context) {
				assert.Error(t, wrapped.Err())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wrapped := cli.Context{Context: tt.ctx}
			tt.validate(t, wrapped)
		})
	}
}

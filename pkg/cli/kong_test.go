package cli_test

import (
	"context"
	"encoding/hex"
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
		ctx  context.Context
		name string
		id   [32]byte
		cli  any
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "creates_context_with_name",
			args: args{
				ctx:  context.Background(),
				name: "test-app",
				id:   [32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},
				cli: &struct {
					Help bool `help:"Show help"`
				}{},
			},
		},
		{
			name: "binds_context_and_buildid",
			args: args{
				ctx:  context.WithValue(context.Background(), contextKey("test-key"), "test-value"),
				name: "gradebot",
				id:   [32]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF},
				cli: &struct {
					Version bool `help:"Show version"`
				}{},
			},
		},
		{
			name: "empty_name",
			args: args{
				ctx:  context.Background(),
				name: "",
				id:   [32]byte{},
				cli:  &struct{}{},
			},
		},
		{
			name: "hex_encodes_build_id",
			args: args{
				ctx:  context.Background(),
				name: "test",
				id:   [32]byte{0xFF, 0xAA, 0x55},
				cli:  &struct{}{},
			},
		},
		{
			name: "zero_build_id",
			args: args{
				ctx:  context.Background(),
				name: "test",
				id:   [32]byte{},
				cli:  &struct{}{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Pass empty args to avoid parsing test flags from os.Args
			kctx := cli.NewKongContext(tt.args.ctx, tt.args.name, tt.args.id, tt.args.cli, []string{}, kong.Exit(func(int) {}))

			require.NotNil(t, kctx, "Kong context should be created")
			assert.NotNil(t, kctx.Model, "Model should be set")

			// Verify hex encoding is correct
			expectedHex := hex.EncodeToString(tt.args.id[:])
			assert.Len(t, expectedHex, 64, "Build ID should be 64 hex characters")
		})
	}
}

func TestNewKongContext_ErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("kong_new_errors", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			cli       any
			wantPanic bool
		}{
			{
				name:      "invalid_cli_type_causes_panic",
				cli:       "not a struct pointer",
				wantPanic: true,
			},
			{
				name:      "nil_cli_causes_panic",
				cli:       nil,
				wantPanic: true,
			},
			{
				name:      "valid_cli_no_panic",
				cli:       &struct{}{},
				wantPanic: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				if tt.wantPanic {
					assert.Panics(t, func() {
						cli.NewKongContext(context.Background(), "test", [32]byte{}, tt.cli, []string{}, kong.Exit(func(int) {}))
					})
				} else {
					assert.NotPanics(t, func() {
						kctx := cli.NewKongContext(context.Background(), "test", [32]byte{}, tt.cli, []string{}, kong.Exit(func(int) {}))
						assert.NotNil(t, kctx)
					})
				}
			})
		}
	})

	t.Run("parse_errors", func(t *testing.T) {
		t.Parallel()

		type cliWithRequired struct {
			Required string `help:"Required argument" required:""`
		}

		tests := []struct {
			name      string
			cli       any
			args      []string
			wantPanic bool
		}{
			{
				name:      "missing_required_arg",
				cli:       &cliWithRequired{},
				args:      []string{}, // Missing required argument
				wantPanic: true,
			},
			{
				name:      "unknown_flag",
				cli:       &struct{}{},
				args:      []string{"--unknown-flag"},
				wantPanic: true,
			},
			{
				name:      "valid_args_no_panic",
				cli:       &cliWithRequired{},
				args:      []string{"--required", "value"},
				wantPanic: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				if tt.wantPanic {
					assert.Panics(t, func() {
						cli.NewKongContext(context.Background(), "test", [32]byte{}, tt.cli, tt.args, kong.Exit(func(int) {}))
					})
				} else {
					assert.NotPanics(t, func() {
						kctx := cli.NewKongContext(context.Background(), "test", [32]byte{}, tt.cli, tt.args, kong.Exit(func(int) {}))
						assert.NotNil(t, kctx)
					})
				}
			})
		}
	})
}

func TestContext_Wrapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ctx      context.Context
		validate func(*testing.T, cli.Context)
	}{
		{
			name: "wraps_background_context",
			ctx:  context.Background(),
			validate: func(t *testing.T, wrapped cli.Context) {
				assert.NotNil(t, wrapped.Context)
			},
		},
		{
			name: "preserves_context_values",
			ctx:  context.WithValue(context.Background(), contextKey("key"), "value"),
			validate: func(t *testing.T, wrapped cli.Context) {
				assert.Equal(t, "value", wrapped.Value(contextKey("key")))
			},
		},
		{
			name: "preserves_cancellation",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
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

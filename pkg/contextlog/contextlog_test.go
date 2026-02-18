package contextlog_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/jh125486/gradebot/pkg/contextlog"
	"github.com/stretchr/testify/assert"
)

func TestWith(t *testing.T) {
	t.Parallel()

	type args struct {
		logger *slog.Logger
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "with discard handler",
			args: args{logger: slog.New(slog.DiscardHandler)},
		},
		{
			name: "with default logger",
			args: args{logger: slog.Default()},
		},
		{
			name: "with text handler",
			args: args{logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			newCtx := contextlog.With(ctx, tt.args.logger)

			retrieved := contextlog.From(newCtx)
			assert.Equal(t, tt.args.logger, retrieved)
		})
	}
}

func TestFromNoLogger(t *testing.T) {
	t.Parallel()

	type args struct {
		setupCtx func() context.Context
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "background context has no logger",
			args: args{setupCtx: func() context.Context {
				return t.Context()
			}},
		},
		{
			name: "context with timeout has no logger",
			args: args{setupCtx: func() context.Context {
				ctx, cancel := context.WithTimeout(t.Context(), 0)
				defer cancel()
				return ctx
			}},
		},
		{
			name: "test context has no logger",
			args: args{setupCtx: func() context.Context {
				return t.Context()
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := tt.args.setupCtx()
			logger := contextlog.From(ctx)
			assert.Equal(t, slog.Default(), logger)
		})
	}
}

func TestDiscardLogger(t *testing.T) {
	t.Parallel()

	type args struct {
		logFunc func(*slog.Logger, context.Context, string)
		message string
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "discard info log",
			args: args{
				logFunc: func(l *slog.Logger, ctx context.Context, msg string) {
					l.InfoContext(ctx, msg)
				},
				message: "test info",
			},
		},
		{
			name: "discard warn log",
			args: args{
				logFunc: func(l *slog.Logger, ctx context.Context, msg string) {
					l.WarnContext(ctx, msg)
				},
				message: "test warning",
			},
		},
		{
			name: "discard error log",
			args: args{
				logFunc: func(l *slog.Logger, ctx context.Context, msg string) {
					l.ErrorContext(ctx, msg)
				},
				message: "test error",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := contextlog.DiscardLogger()
			assert.NotNil(t, logger)

			// Should not panic when logging
			ctx := t.Context()
			tt.args.logFunc(logger, ctx, tt.args.message)
		})
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	type args struct {
		rawLevel string
		attrs    []slog.Attr
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "debug level",
			args: args{rawLevel: "debug"},
		},
		{
			name: "info level",
			args: args{rawLevel: "info"},
		},
		{
			name: "warn level",
			args: args{rawLevel: "warn"},
		},
		{
			name: "error level",
			args: args{rawLevel: "error"},
		},
		{
			name: "invalid level defaults to info",
			args: args{rawLevel: "invalid"},
		},
		{
			name: "empty string defaults to info",
			args: args{rawLevel: ""},
		},
		{
			name: "with attributes",
			args: args{
				rawLevel: "info",
				attrs: []slog.Attr{
					slog.String("version", "1.0.0"),
					slog.String("commit", "abc123"),
					slog.String("built", "2024-01-01"),
				},
			},
		},
		{
			name: "with single attribute",
			args: args{
				rawLevel: "info",
				attrs: []slog.Attr{
					slog.String("service", "gradebot"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			newCtx := contextlog.New(ctx, tt.args.rawLevel, tt.args.attrs...)

			// Verify a context is returned
			assert.NotNil(t, newCtx)

			// Verify the logger is stored in context and can be retrieved
			logger := contextlog.From(newCtx)
			assert.NotNil(t, logger)

			// Verify it's a real logger (not the default empty one)
			assert.NotEqual(t, slog.New(slog.DiscardHandler), logger)
		})
	}
}

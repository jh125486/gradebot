// Package contextlog provides utilities for managing loggers through context.Context.
package contextlog

import (
	"context"
	"log/slog"
	"os"
)

type loggerKey struct{}

const DefaultLevel = slog.LevelInfo

func New(ctx context.Context, rawLevel string, attrs ...slog.Attr) context.Context {
	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(rawLevel)); err != nil {
		slog.Default().WarnContext(ctx, "Invalid level, falling back to default",
			slog.String("rawLevel", rawLevel),
			slog.String("default", DefaultLevel.String()),
			slog.Any("error", err),
		)
		logLevel = DefaultLevel
	}

	l := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

	if len(attrs) > 0 {
		anyAttrs := make([]any, len(attrs))
		for i := range attrs {
			anyAttrs[i] = attrs[i]
		}
		l = l.With(anyAttrs...)
	}

	slog.SetDefault(l)

	return With(ctx, l)
}

// With returns a new context with the given logger attached.
func With(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// From retrieves the logger from the context.
// If no logger is found, it returns the default logger.
func From(ctx context.Context) *slog.Logger {
	logger, ok := ctx.Value(loggerKey{}).(*slog.Logger)
	if !ok {
		return slog.Default()
	}
	return logger
}

// DiscardLogger returns a logger that discards all output.
// Useful for tests to suppress logging.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

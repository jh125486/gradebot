package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/jh125486/gradebot/pkg/app"
	"github.com/jh125486/gradebot/pkg/cli"
	"github.com/jh125486/gradebot/pkg/contextlog"
)

func main() {
	// Load .env file if it exists
	_ = godotenv.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ctx = newLogger(ctx)

	buildID := os.Getenv("BUILD_ID")
	contextlog.From(ctx).InfoContext(ctx, "Starting gradebot application", slog.String("buildID", buildID))
	var cliApp app.CLI
	if err := cli.NewKongContext(ctx, "gradebot", buildID, &cliApp, os.Args[1:]).
		Run(ctx); err != nil {
		contextlog.From(ctx).ErrorContext(ctx, "Failed to execute command", slog.Any("error", err))
		os.Exit(1)
	}

	// tiny grace period for logs to flush
	time.Sleep(10 * time.Millisecond)
}

func newLogger(ctx context.Context) context.Context {
	var logLevel slog.Level
	if lvl := os.Getenv("LOG_LEVEL"); lvl != "" {
		_ = logLevel.UnmarshalText([]byte(lvl))
	}
	l := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(l)

	return contextlog.With(ctx, l)
}

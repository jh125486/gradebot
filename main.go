package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/jh125486/gradebot/cli"
	basecli "github.com/jh125486/gradebot/pkg/cli"
	"github.com/jh125486/gradebot/pkg/contextlog"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Load .env file if it exists
	_ = godotenv.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ctx = contextlog.New(ctx, os.Getenv("LOG_LEVEL"),
		slog.String("version", version),
		slog.String("commit", commit),
		slog.String("built", date))

	buildID := os.Getenv("BUILD_ID")
	var app cli.CLI
	if err := basecli.NewKongContext(ctx, "gradebot", buildID, version, &app, os.Args[1:]).
		Run(ctx); err != nil {
		contextlog.From(ctx).ErrorContext(ctx, "Failed to execute command", slog.Any("error", err))
		os.Exit(1)
	}

	// tiny grace period for logs to flush
	time.Sleep(10 * time.Millisecond)
}

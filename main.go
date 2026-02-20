package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/jh125486/gradebot/cli"
	basecli "github.com/jh125486/gradebot/pkg/cli"
)

func main() {
	// Load .env file if it exists
	_ = godotenv.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var grammar cli.CLI
	kctx := basecli.NewKongContext(ctx, "gradebot", os.Getenv("BUILD_ID"), "server", &grammar, os.Args[1:])
	if err := kctx.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// tiny grace period for logs to flush
	time.Sleep(10 * time.Millisecond)
}

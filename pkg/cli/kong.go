package cli

import (
	"context"
	"encoding/hex"

	"github.com/alecthomas/kong"
)

// Context wraps context.Context to work around reflection issues in Kong's Bind().
// Use this as the parameter type for Kong command Run methods.
type Context struct {
	context.Context
}

// NewKongContext creates and configures a Kong parser context for a CLI application.
// It binds the provided context and build ID for use in command execution.
// Pass nil for args to use os.Args (typical for production), or provide custom args for testing.
func NewKongContext(ctx context.Context, name string, id [32]byte, cli any, args []string) *kong.Context {
	buildID := hex.EncodeToString(id[:])
	parser, err := kong.New(cli,
		kong.Name(name),
		kong.UsageOnError(),
		kong.Bind(Context{ctx}, buildID),
	)
	if err != nil {
		panic(err)
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		parser.Errorf("%s", err)
		panic(err)
	}
	return kctx
}

// NewServerContext creates a Kong context specifically for the server application.
// This is in the client package because it's a CLI adapter concern, not application logic.
func NewServerContext(ctx context.Context, name string, id [32]byte, cli any) *kong.Context {
	buildID := hex.EncodeToString(id[:])
	return kong.Parse(cli,
		kong.Name(name),
		kong.UsageOnError(),
		kong.Exit(func(int) {}), // Prevent os.Exit() in tests
		kong.Bind(Context{Context: ctx}, buildID),
	)
}

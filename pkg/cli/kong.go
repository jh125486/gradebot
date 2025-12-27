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

// NewKongContext creates a Kong context with required params.
func NewKongContext(ctx context.Context, name string, id [32]byte, cli any, args []string, opts ...kong.Option) *kong.Context {
	buildID := hex.EncodeToString(id[:])
	opts = append(opts,
		kong.Name(name),
		kong.UsageOnError(),
		kong.Bind(Context{Context: ctx}, buildID),
	)
	parser, err := kong.New(cli, opts...)
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

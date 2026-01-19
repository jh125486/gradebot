package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/alecthomas/kong"
)

// Context wraps context.Context to work around reflection issues in Kong's Bind().
// Use this as the parameter type for Kong command Run methods.
type Context struct {
	context.Context
}

// BuildID is a distinct type for the hashed build identifier.
// Used for Kong binding to avoid conflicts with plain strings.
type BuildID string

// NewKongContext creates a Kong context with required params.
func NewKongContext(ctx context.Context, name, id string, cli any, args []string, opts ...kong.Option) *kong.Context {
	hashedID := sha256.Sum256([]byte(id))
	buildID := hex.EncodeToString(hashedID[:])
	svc := New(buildID)

	opts = append(opts,
		kong.Name(name),
		kong.UsageOnError(),
		kong.Bind(Context{Context: ctx}, svc, BuildID(buildID)),
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

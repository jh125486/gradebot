package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"

	"github.com/alecthomas/kong"
	"github.com/jh125486/gradebot/pkg/contextlog"
)

// Context wraps context.Context to work around reflection issues in Kong's Bind().
// Use this as the parameter type for Kong command Run methods.
type Context struct {
	context.Context
}

type (
	// BuildID is a distinct type for the hashed build identifier.
	// Used for Kong binding to avoid conflicts with plain strings.
	BuildID string

	// Version is a distinct type for the application version.
	// Used for Kong binding to track server/client version mismatches.
	Version string
)

// NewKongContext creates a Kong context with required params.
func NewKongContext(
	ctx context.Context,
	name, buildID, version string,
	cli any,
	args []string,
	opts ...kong.Option,
) *kong.Context {
	ctx = contextlog.New(ctx, os.Getenv("LOG_LEVEL"),
		slog.String("buildID", buildID),
		slog.String("version", version),
	)

	hashedID := sha256.Sum256([]byte(buildID))
	buildID = hex.EncodeToString(hashedID[:])
	svc := New(buildID, version)

	opts = append(opts,
		kong.Name(name),
		kong.UsageOnError(),
		kong.Bind(Context{Context: ctx}),
		kong.Bind(svc),
		kong.Bind(BuildID(buildID)),
		kong.Bind(Version(version)),
		kong.Vars{"version": version},
	)
	parser, err := kong.New(cli, opts...)
	if err != nil {
		panic(err)
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		parser.Errorf("%s", err)
		parser.Exit(1)
	}

	return kctx
}

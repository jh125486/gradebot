package cli

import (
	"github.com/alecthomas/kong"
)

// BaseCLI defines the core fields for all CLIs using our framework.
type BaseCLI struct {
	Version kong.VersionFlag `help:"Show version and exit" name:"version"`
}

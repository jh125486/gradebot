package cli

import (
	"fmt"
	"io"
	"reflect"
)

// VersionFlag adds a --version option to a Kong command struct.
type VersionFlag struct {
	Version bool `help:"Show version and exit" name:"version"`
}

// PrintVersion writes a simple name/version string to the provided writer.
func PrintVersion(w io.Writer, name string, version Version) {
	if w == nil {
		return
	}
	if reflect.ValueOf(w).Kind() == reflect.Pointer && reflect.ValueOf(w).IsNil() {
		return
	}

	fmt.Fprintf(w, "%s %s\n", name, version)
}

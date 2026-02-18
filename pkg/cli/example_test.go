package cli_test

import (
	"context"
	"fmt"

	"github.com/jh125486/gradebot/pkg/cli"
)

// exampleCommand demonstrates a simple Kong command that embeds CommonArgs and
// receives a bound *cli.Service in its Run method. This mirrors how course-specific
// commands should be structured when building a grader CLI.
type exampleCommand struct {
	Args cli.CommonArgs `embed:""`
}

// Run executes the command using the injected Service. In a real grader this
// is where you'd construct a client.Config from the parsed args and call
// client.ExecuteProject with the appropriate evaluators.
func (c *exampleCommand) Run(svc *cli.Service) error {
	// Print a small, deterministic value so the example can be tested and shown
	// on pkg.go.dev. Real implementations should use svc and c.Args to run the
	// grading workflow.
	fmt.Fprintf(svc.Stdout, "svc_ok: %v\n", svc.Client != nil)
	return nil
}

// ExampleKongBindAndRun shows how to wire up a command and run it using
// NewKongContext. It simulates invoking the CLI as:
//
//	grade --dir . --run true
func ExampleNewKongContext() {
	var CLI struct {
		Grade exampleCommand `cmd:"" help:"Grade project"`
	}

	// Create a Kong context (binding is performed inside NewKongContext). In
	// some environments calling kctx.Run(...) can exercise Kong's runtime
	// binding paths; here we only assert the context was created successfully.
	kctx := cli.NewKongContext(context.Background(), "gradebot", "v1.0.0", "commit-hash", "2023-01-01", &CLI, []string{"grade", "--dir", ".", "--run", "true"})
	fmt.Println("kctx_created:", kctx != nil)

	// Manually construct a service and run the command to demonstrate how the
	// Run method receives an injected *cli.Service. This keeps the example
	// deterministic and safe to run in package tests.
	svc := cli.New("build-id-123", "v1.0.0")
	cmd := &exampleCommand{Args: cli.CommonArgs{WorkDir: cli.CommonArgs{}.WorkDir}} // leave defaults
	_ = cmd.Run(svc)

	// Output:
	// kctx_created: true
	// svc_ok: true
}

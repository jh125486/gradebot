package cli

import (
	"io"
	"net/http"
	"time"

	"github.com/jh125486/gradebot/pkg/client"
	"github.com/jh125486/gradebot/pkg/rubrics"
)

// CommonArgs contains arguments shared across project grading commands.
//
//nolint:lll // Long struct tags
type CommonArgs struct {
	ServerURL string            `default:"https://gradebot-unt-fab5dc5c.koyeb.app" help:"URL of the grading server"                                     name:"server-url"`
	Dir       client.WorkDir    `default:"."                                       help:"Path to your project directory (must exist and be accessible)" name:"dir"        required:""`
	RunCmd    string            `help:"Command to run your program"                name:"run"                                                           required:""`
	Env       map[string]string `help:"Environment variables (key=value)"          name:"env"                                                           short:"e"`

	Client *http.Client `kong:"-"`
	Stdin  io.Reader    `kong:"-"` // For testing - can inject stdin for prompts
	Stdout io.Writer    `kong:"-"` // For testing - can capture output

	// CommandBuilder creates commands for execution. Can be injected in tests.
	CommandBuilder rubrics.CommandBuilder `kong:"-"`
}

// AfterApply is a Kong hook that initializes the HTTP client with the build ID.
// This method allows CommonArgs to be used directly in Kong command structs.
func (a *CommonArgs) AfterApply(buildID string) error {
	a.Client = &http.Client{
		Timeout:   30 * time.Second,
		Transport: client.NewAuthTransport(buildID, http.DefaultTransport),
	}
	return nil
}

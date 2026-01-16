package cli

import (
	"github.com/jh125486/gradebot/pkg/client"
)

// CommonArgs contains arguments shared across project grading commands.
//
//nolint:lll // Long struct tags
type CommonArgs struct {
	ServerURL string            `default:"https://gradebot-unt-fab5dc5c.koyeb.app" help:"URL of the grading server"                                     name:"server-url"`
	WorkDir   client.WorkDir    `default:"."                                       help:"Path to your project directory (must exist and be accessible)" name:"dir"        required:"" type:"existingdir"`
	RunCmd    string            `help:"Command to run your program"                name:"run"                                                           required:""`
	Env       map[string]string `help:"Environment variables (key=value)"          name:"env"                                                           short:"e"`
}

func (c *CommonArgs) Validate() error {
	if err := c.WorkDir.Validate(); err != nil {
		return err
	}

	return nil
}

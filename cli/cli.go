package cli

import (
	"github.com/jh125486/gradebot/pkg/cli"
	"github.com/jh125486/gradebot/pkg/openai"
	"github.com/jh125486/gradebot/pkg/server"
	"github.com/jh125486/gradebot/pkg/storage"
)

type (
	// CLI defines the command-line interface structure for the gradebot server application.
	CLI struct {
		cli.BaseCLI    `embed:""`
		Port           string `default:"8080"              env:"PORT"                                 help:"Port to listen on" name:"port"`
		DatabaseURL    string `env:"DATABASE_URL"          help:"PostgreSQL database URL"             name:"database-url"`
		R2Endpoint     string `env:"R2_ENDPOINT"           help:"R2/S3 endpoint URL"                  name:"r2-endpoint"`
		R2Region       string `default:"auto"              env:"AWS_REGION"                           help:"AWS region"        name:"r2-region"`
		R2Bucket       string `env:"R2_BUCKET"             help:"R2/S3 bucket name"                   name:"r2-bucket"`
		R2AccessKey    string `env:"AWS_ACCESS_KEY_ID"     help:"AWS access key ID"                   name:"r2-access-key"`
		R2SecretKey    string `env:"AWS_SECRET_ACCESS_KEY" help:"AWS secret access key"               name:"r2-secret-key"`
		R2UsePathStyle bool   `env:"USE_PATH_STYLE"        help:"Use path-style S3 URLs"              name:"r2-path-style"`
		OpenAIAPIKey   string `env:"OPENAI_API_KEY"        help:"OpenAI API key for quality analysis" name:"openai-api-key"`
	}
	Service struct {
		Storage      storage.Storage
		OpenAIClient *openai.Client
	}
)

// New initializes storage and OpenAI client for the CLI Service.
func New(ctx cli.Context, cmd *CLI) (*Service, error) {
	var (
		svc Service
		err error
	)

	// Initialize OpenAI client if API key is provided
	if cmd.OpenAIAPIKey != "" {
		svc.OpenAIClient = openai.NewClient(cmd.OpenAIAPIKey, nil)
	}

	// Initialize storage
	if cmd.DatabaseURL != "" {
		svc.Storage, err = storage.NewSQLStorage(ctx, cmd.DatabaseURL)
	} else {
		svc.Storage, err = storage.NewR2Storage(ctx, &storage.R2Config{
			Endpoint:        cmd.R2Endpoint,
			Region:          cmd.R2Region,
			Bucket:          cmd.R2Bucket,
			AccessKeyID:     cmd.R2AccessKey,
			SecretAccessKey: cmd.R2SecretKey,
			UsePathStyle:    cmd.R2UsePathStyle,
		})
	}
	if err != nil {
		return nil, err
	}

	return &svc, nil
}

// Run executes the server command.
func (cmd *CLI) Run(ctx cli.Context, buildID cli.BuildID, version cli.Version) error {
	svc, err := New(ctx, cmd)
	if err != nil {
		return err
	}

	return server.Start(ctx, &server.Config{
		ID:           string(buildID),
		Version:      string(version),
		Port:         cmd.Port,
		OpenAIClient: svc.OpenAIClient,
		Storage:      svc.Storage,
	})
}

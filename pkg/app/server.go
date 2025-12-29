package app

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/jh125486/gradebot/pkg/cli"
	"github.com/jh125486/gradebot/pkg/openai"
	"github.com/jh125486/gradebot/pkg/server"
	"github.com/jh125486/gradebot/pkg/storage"
)

type (
	// CLI defines the command-line interface structure for the gradebot server application.
	CLI struct {
		Port           string `default:"8080"              env:"PORT"                                 help:"Port to listen on" name:"port"`
		DatabaseURL    string `env:"DATABASE_URL"          help:"PostgreSQL database URL"             name:"database-url"`
		R2Endpoint     string `env:"R2_ENDPOINT"           help:"R2/S3 endpoint URL"                  name:"r2-endpoint"`
		R2Region       string `default:"auto"              env:"AWS_REGION"                           help:"AWS region"        name:"r2-region"`
		R2Bucket       string `env:"R2_BUCKET"             help:"R2/S3 bucket name"                   name:"r2-bucket"`
		R2AccessKey    string `env:"AWS_ACCESS_KEY_ID"     help:"AWS access key ID"                   name:"r2-access-key"`
		R2SecretKey    string `env:"AWS_SECRET_ACCESS_KEY" help:"AWS secret access key"               name:"r2-secret-key"`
		R2UsePathStyle bool   `env:"USE_PATH_STYLE"        help:"Use path-style S3 URLs"              name:"r2-path-style"`
		OpenAIAPIKey   string `env:"OPENAI_API_KEY"        help:"OpenAI API key for quality analysis" name:"openai-api-key"`

		Storage      storage.Storage `kong:"-"`
		OpenAIClient *openai.Client  `kong:"-"`
	}
)

// AfterApply is a Kong hook that initializes storage and OpenAI client.
func (cmd *CLI) AfterApply(ctx cli.Context) error {
	var err error

	// Initialize storage
	if cmd.DatabaseURL != "" {
		cmd.Storage, err = storage.NewSQLStorage(ctx, cmd.DatabaseURL)
	} else {
		cmd.Storage, err = storage.NewR2Storage(ctx, &storage.R2Config{
			Endpoint:        cmd.R2Endpoint,
			Region:          cmd.R2Region,
			Bucket:          cmd.R2Bucket,
			AccessKeyID:     cmd.R2AccessKey,
			SecretAccessKey: cmd.R2SecretKey,
			UsePathStyle:    cmd.R2UsePathStyle,
		})
	}
	if err != nil {
		return err
	}

	// Initialize OpenAI client if API key is provided
	if cmd.OpenAIAPIKey != "" {
		cmd.OpenAIClient = openai.NewClient(cmd.OpenAIAPIKey, nil)
	}

	return nil
}

// Run executes the server command.
func (cmd *CLI) Run(ctx cli.Context, buildID string) error {
	port := cmd.Port
	if port == "" {
		port = "8080"
	}

	hashBytes := sha256.Sum256([]byte(buildID))
	cfg := server.Config{
		ID:           hex.EncodeToString(hashBytes[:]),
		Port:         port,
		OpenAIClient: cmd.OpenAIClient,
		Storage:      cmd.Storage,
	}
	return server.Start(ctx, cfg)
}

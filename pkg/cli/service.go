package cli

import (
	"io"
	"net/http"
	"os"
	"time"

	"github.com/jh125486/gradebot/pkg/client"
	"github.com/jh125486/gradebot/pkg/rubrics"
)

// Service holds global dependencies that can be injected into commands.
// It separates runtime dependencies from configuration (args).
type Service struct {
	Client         *http.Client
	Stdin          io.Reader
	Stdout         io.Writer
	CommandBuilder rubrics.CommandBuilder
}

// NewService creates a new Service with default implementations.
// buildID is used for the Authorization header in the HTTP client.
func NewService(id string) *Service {
	return &Service{
		Client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: client.NewAuthTransport(id, http.DefaultTransport),
		},
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
	}
}

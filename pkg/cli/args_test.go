package cli_test

import (
	"testing"
	"time"

	"github.com/jh125486/gradebot/pkg/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewService(t *testing.T) {
	t.Parallel()

	buildID := "test-build-123"
	svc := cli.NewService(buildID)

	require.NotNil(t, svc)
	require.NotNil(t, svc.Client, "Client should be initialized")
	assert.Equal(t, 30*time.Second, svc.Client.Timeout, "Timeout should be 30 seconds")
	assert.NotNil(t, svc.Client.Transport, "Transport should be set")

	assert.NotNil(t, svc.Stdin, "Stdin should be initialized")
	assert.NotNil(t, svc.Stdout, "Stdout should be initialized")
}

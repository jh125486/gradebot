package cli_test

import (
	"testing"
	"time"

	"github.com/jh125486/gradebot/pkg/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	type args struct {
		buildID string
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "with_build_id",
			args: args{buildID: "test-build-123"},
		},
		{
			name: "with_different_build_id",
			args: args{buildID: "prod-build-456"},
		},
		{
			name: "with_empty_build_id",
			args: args{buildID: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := cli.New(tt.args.buildID)

			require.NotNil(t, svc)
			require.NotNil(t, svc.Client, "Client should be initialized")
			assert.Equal(t, 30*time.Second, svc.Client.Timeout, "Timeout should be 30 seconds")
			assert.NotNil(t, svc.Client.Transport, "Transport should be set")

			assert.NotNil(t, svc.Stdin, "Stdin should be initialized")
			assert.NotNil(t, svc.Stdout, "Stdout should be initialized")
		})
	}
}

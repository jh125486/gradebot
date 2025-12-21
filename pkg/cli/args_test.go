package cli_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/jh125486/gradebot/pkg/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommonArgs_AfterApply(t *testing.T) {
	t.Parallel()

	type args struct {
		buildID string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
		verify  func(t *testing.T, ca *cli.CommonArgs)
	}{
		{
			name: "initializes_http_client",
			args: args{buildID: "test-build-123"},
			verify: func(t *testing.T, ca *cli.CommonArgs) {
				require.NotNil(t, ca.Client, "Client should be initialized")
				assert.Equal(t, 30*time.Second, ca.Client.Timeout, "Timeout should be 30 seconds")
				assert.NotNil(t, ca.Client.Transport, "Transport should be set")
			},
			wantErr: false,
		},
		{
			name: "empty_build_id",
			args: args{buildID: ""},
			verify: func(t *testing.T, ca *cli.CommonArgs) {
				require.NotNil(t, ca.Client, "Client should be initialized even with empty buildID")
				assert.Equal(t, 30*time.Second, ca.Client.Timeout)
			},
			wantErr: false,
		},
		{
			name: "replaces_existing_client",
			args: args{buildID: "new-build-456"},
			verify: func(t *testing.T, ca *cli.CommonArgs) {
				require.NotNil(t, ca.Client, "Client should be replaced")
				assert.Equal(t, 30*time.Second, ca.Client.Timeout)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ca := &cli.CommonArgs{}

			// For the "replaces_existing_client" test, set an initial client
			if tt.name == "replaces_existing_client" {
				ca.Client = &http.Client{Timeout: 10 * time.Second}
			}

			err := ca.AfterApply(tt.args.buildID)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.verify != nil {
				tt.verify(t, ca)
			}
		})
	}
}

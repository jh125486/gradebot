package app_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jh125486/gradebot/pkg/app"
	"github.com/jh125486/gradebot/pkg/cli"
	pb "github.com/jh125486/gradebot/pkg/proto"
	"github.com/jh125486/gradebot/pkg/storage"
)

func TestServerCmd_AfterApply(t *testing.T) {
	// Cannot run subtests in parallel - they share the same PostgreSQL database
	// and create the same schema, causing "duplicate key value violates unique constraint" errors

	databaseURL := os.Getenv("DATABASE_URL")

	type args struct {
		databaseURL    string
		r2Endpoint     string
		r2Region       string
		r2Bucket       string
		r2AccessKey    string
		r2SecretKey    string
		r2UsePathStyle bool
		openAIAPIKey   string
	}
	tests := []struct {
		name       string
		args       args
		wantErr    bool
		setup      func(t *testing.T)
		verify     func(t *testing.T, cmd *app.CLI)
		requiresDB bool
	}{
		{
			name: "sql_storage_with_valid_connection",
			args: args{
				databaseURL: databaseURL,
			},
			wantErr:    false,
			requiresDB: true,
			verify: func(t *testing.T, cmd *app.CLI) {
				assert.NotNil(t, cmd.Storage)
				assert.Nil(t, cmd.OpenAIClient)
			},
		},
		{
			name: "sql_storage_with_openai_key",
			args: args{
				databaseURL:  databaseURL,
				openAIAPIKey: "sk-test-key-12345",
			},
			wantErr:    false,
			requiresDB: true,
			verify: func(t *testing.T, cmd *app.CLI) {
				assert.NotNil(t, cmd.Storage)
				assert.NotNil(t, cmd.OpenAIClient)
			},
		},
		{
			name: "sql_storage_missing_database_url",
			args: args{
				databaseURL: "",
			},
			wantErr: true,
		},
		{
			name: "r2_storage_missing_endpoint",
			args: args{
				r2Endpoint: "",
				r2Bucket:   "test-bucket",
			},
			wantErr: true,
		},
		{
			name: "r2_storage_missing_bucket",
			args: args{
				r2Endpoint: "https://test.r2.cloudflarestorage.com",
				r2Region:   "auto",
				r2Bucket:   "",
			},
			wantErr: true,
		},
		{
			name: "sql_storage_invalid_url",
			args: args{
				databaseURL: "invalid://url",
			},
			wantErr: true,
		},
		{
			name: "sql_storage_malformed_url",
			args: args{
				databaseURL: "postgres://invalid",
			},
			wantErr: true,
		},
		{
			name: "sql_storage_connection_refused",
			args: args{
				databaseURL: "postgresql://user:pass@localhost:54321/nonexistent",
			},
			wantErr: true,
		},
		{
			name: "r2_missing_credentials",
			args: args{
				r2Endpoint: "https://test.r2.cloudflarestorage.com",
				r2Region:   "auto",
				r2Bucket:   "test-bucket",
			},
			wantErr: true,
		},
		{
			name: "empty_openai_key_does_not_initialize_client",
			args: args{
				databaseURL:  databaseURL,
				openAIAPIKey: "",
			},
			wantErr:    false,
			requiresDB: true,
			verify: func(t *testing.T, cmd *app.CLI) {
				assert.NotNil(t, cmd.Storage)
				assert.Nil(t, cmd.OpenAIClient)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Cannot run in parallel - tests share same database
			if tt.requiresDB && databaseURL == "" {
				t.Skip("DATABASE_URL not set, skipping test that requires database")
			}

			if tt.setup != nil {
				tt.setup(t)
			}

			cmd := &app.CLI{
				DatabaseURL:    tt.args.databaseURL,
				R2Endpoint:     tt.args.r2Endpoint,
				R2Region:       tt.args.r2Region,
				R2Bucket:       tt.args.r2Bucket,
				R2AccessKey:    tt.args.r2AccessKey,
				R2SecretKey:    tt.args.r2SecretKey,
				R2UsePathStyle: tt.args.r2UsePathStyle,
				OpenAIAPIKey:   tt.args.openAIAPIKey,
			}

			ctx := cli.Context{Context: context.Background()}
			err := cmd.AfterApply(ctx)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, cmd.Storage)

			if tt.verify != nil {
				tt.verify(t, cmd)
			}

			// Cleanup
			if cmd.Storage != nil {
				_ = cmd.Storage.Close()
			}
		})
	}
}

func TestServerCmd_Run(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) (*app.CLI, context.Context, context.CancelFunc)
		wantErr bool
	}{
		{
			name: "nil_storage_returns_error",
			setup: func(t *testing.T) (*app.CLI, context.Context, context.CancelFunc) {
				cmd := &app.CLI{
					Port:    "0",
					Storage: nil,
				}
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // Cancel immediately
				return cmd, ctx, cancel
			},
			wantErr: true,
		},
		{
			name: "context_cancelled_returns_error",
			setup: func(t *testing.T) (*app.CLI, context.Context, context.CancelFunc) {
				cmd := &app.CLI{
					Port:    "0",
					Storage: &mockStorage{},
				}
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // Cancel before starting
				return cmd, ctx, cancel
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd, ctx, cancel := tt.setup(t)
			defer cancel()

			buildID := cli.BuildID("test-build-id-12345")
			appCtx := cli.Context{Context: ctx}

			err := cmd.Run(appCtx, buildID)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// mockStorage implements storage.Storage for testing
type mockStorage struct{}

func (m *mockStorage) SaveResult(_ context.Context, _ *pb.Result) error {
	return nil
}

func (m *mockStorage) LoadResult(_ context.Context, _ string) (*pb.Result, error) {
	return &pb.Result{}, nil
}

func (m *mockStorage) ListResultsPaginated(_ context.Context, _ storage.ListResultsParams) (results map[string]*pb.Result, total int, err error) {
	return make(map[string]*pb.Result), 0, nil
}

func (m *mockStorage) ListProjects(_ context.Context) ([]string, error) {
	return []string{}, nil
}

func (m *mockStorage) Close() error {
	return nil
}

// Ensure mockStorage implements storage.Storage
var _ storage.Storage = (*mockStorage)(nil)

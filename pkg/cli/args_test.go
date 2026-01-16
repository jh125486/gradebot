package cli_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jh125486/gradebot/pkg/cli"
	"github.com/jh125486/gradebot/pkg/client"
	"github.com/stretchr/testify/assert"
)

func TestCommonArgsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setup         func(t *testing.T) (string, func())
		wantErr       bool
		errContains   string
		skipOnWindows bool
	}{
		{
			name: "empty_path",
			setup: func(t *testing.T) (string, func()) {
				return "", nil
			},
			wantErr:     true,
			errContains: "work directory not specified",
		},
		{
			name: "valid_directory",
			setup: func(t *testing.T) (string, func()) {
				return t.TempDir(), nil
			},
			wantErr: false,
		},
		{
			name: "nonexistent_directory",
			setup: func(t *testing.T) (string, func()) {
				return "/path/that/does/not/exist", nil
			},
			wantErr:     true,
			errContains: "no such file or directory",
		},
		{
			name: "path_is_file_not_directory",
			setup: func(t *testing.T) (string, func()) {
				dir := t.TempDir()
				f := filepath.Join(dir, "file.txt")
				if err := os.WriteFile(f, []byte("test"), 0o644); err != nil {
					t.Fatalf("failed to create test file: %v", err)
				}
				return f, nil
			},
			wantErr:     true,
			errContains: "is not a directory",
		},
		{
			name: "unreadable_directory",
			setup: func(t *testing.T) (string, func()) {
				if runtime.GOOS == "windows" {
					t.Skip("Skip on Windows - permission handling differs")
				}
				dir := t.TempDir()
				if err := os.Chmod(dir, 0o000); err != nil {
					t.Fatalf("failed to change permissions: %v", err)
				}
				return dir, func() { _ = os.Chmod(dir, 0o755) }
			},
			wantErr:       true,
			errContains:   "permission denied",
			skipOnWindows: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.skipOnWindows && runtime.GOOS == "windows" {
				t.Skip("directory permission semantics differ on Windows")
			}

			path, cleanup := tt.setup(t)
			if cleanup != nil {
				t.Cleanup(cleanup)
			}

			args := cli.CommonArgs{WorkDir: client.WorkDir(path)}
			err := args.Validate()

			if (err != nil) != tt.wantErr {
				t.Fatalf("CommonArgs.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.errContains != "" && err != nil {
				assert.Contains(t, err.Error(), tt.errContains)
			}
		})
	}
}

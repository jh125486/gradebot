package client

import (
	"bytes"
	"context"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/jh125486/gradebot/pkg/contextlog"
	"github.com/stretchr/testify/assert"
)

func TestDirectoryError_getPermissionHelp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		wantText string
	}{
		{
			name: "returns_help_text",
			// We can't easily test all OS branches without build tags,
			// but we can verify it returns non-empty help text
			wantText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := &DirectoryError{Err: io.ErrUnexpectedEOF}
			help := err.getPermissionHelp()
			assert.NotEmpty(t, help, "getPermissionHelp should return non-empty help text")

			// Verify help text contains expected keywords based on OS
			switch runtime.GOOS {
			case "darwin":
				assert.Contains(t, help, "macOS")
			case "windows":
				assert.Contains(t, help, "Windows")
			case "linux":
				assert.Contains(t, help, "Linux")
			default:
				assert.Contains(t, help, "permissions")
			}
		})
	}
}

func TestPromptForSubmission(t *testing.T) {
	t.Parallel()

	type args struct {
		ctx context.Context
		w   io.Writer
		r   io.Reader
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "user_answers_yes",
			args: args{
				r: strings.NewReader("y\n"),
			},
			want: true,
		},
		{
			name: "user_answers_no",
			args: args{
				r: strings.NewReader("n\n"),
			},
			want: false,
		},
		{
			name: "user_answers_yes_uppercase",
			args: args{
				r: strings.NewReader("Y\n"),
			},
			want: true,
		},
		{
			name: "user_answers_invalid",
			args: args{
				r: strings.NewReader("maybe\n"),
			},
			want: false,
		},
		{
			name: "read_error_eof",
			args: args{
				r: strings.NewReader(""),
			},
			want: false,
		},
		{
			name: "whitespace_before_yes",
			args: args{
				r: strings.NewReader("  y  \n"),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.args.ctx == nil {
				tt.args.ctx = t.Context()
			}
			tt.args.ctx = contextlog.With(tt.args.ctx, contextlog.DiscardLogger())
			// Use a bytes.Buffer to capture output
			output := new(bytes.Buffer)
			if tt.args.w == nil {
				tt.args.w = output
			}
			got := PromptForSubmission(tt.args.ctx, tt.args.w, tt.args.r)

			if got != tt.want {
				t.Errorf("PromptForSubmission() = %v, want %v", got, tt.want)
			}
		})
	}
}

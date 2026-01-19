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
	"github.com/stretchr/testify/require"
)

func TestDirectoryError_ErrorIncludesPermissionHelp(t *testing.T) {
	t.Parallel()

	// Exercise exported behavior: WorkDir.Validate returns an error
	// when the path doesn't exist. The returned error's message should
	// include permission help to assist users.
	nonexistent := WorkDir("/this-path-should-not-exist-please-remove-if-it-does")
	err := nonexistent.Validate()
	require.Error(t, err)

	// The error string should include a human-helpful hint based on OS
	s := err.Error()
	switch runtime.GOOS {
	case "darwin":
		assert.Contains(t, s, "macOS")
	case "windows":
		assert.Contains(t, s, "Windows")
	case "linux":
		assert.Contains(t, s, "Linux")
	default:
		assert.Contains(t, s, "permissions")
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
			// Use a bytes.Buffer to capture output for normal cases
			output := new(bytes.Buffer)
			if tt.args.w == nil {
				// For the table-driven cases we want a non-nil writer so the prompt can be written
				tt.args.w = output
			}
			got := PromptForSubmission(tt.args.ctx, tt.args.w, tt.args.r)

			if got != tt.want {
				t.Errorf("PromptForSubmission() = %v, want %v", got, tt.want)
			}
		})
	}

	// Additional cases to validate nil writer/reader behavior
	t.Run("nil_writer_returns_false", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		ctx = contextlog.With(ctx, contextlog.DiscardLogger())
		got := PromptForSubmission(ctx, nil, strings.NewReader("y\n"))
		if got != false {
			t.Errorf("PromptForSubmission() with nil writer = %v, want %v", got, false)
		}
	})

	t.Run("nil_reader_returns_false", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		ctx = contextlog.With(ctx, contextlog.DiscardLogger())
		output := new(bytes.Buffer)
		got := PromptForSubmission(ctx, output, nil)
		if got != false {
			t.Errorf("PromptForSubmission() with nil reader = %v, want %v", got, false)
		}
	})
}

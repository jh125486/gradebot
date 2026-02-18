package cli_test

import (
	"bytes"
	"testing"

	"github.com/jh125486/gradebot/pkg/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		writer  *bytes.Buffer
		appName string
		version cli.Version
		want    string
	}{
		{
			name:    "writes_version",
			writer:  &bytes.Buffer{},
			appName: "gradebot",
			version: "v1.2.3",
			want:    "gradebot v1.2.3\n",
		},
		{
			name:    "writes_empty_version",
			writer:  &bytes.Buffer{},
			appName: "gradebot",
			version: "",
			want:    "gradebot \n",
		},
		{
			name:    "nil_writer_no_output",
			writer:  nil,
			appName: "gradebot",
			version: "v1.0.0",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.NotPanics(t, func() {
				cli.PrintVersion(tt.writer, tt.appName, tt.version)
			})

			if tt.writer != nil {
				assert.Equal(t, tt.want, tt.writer.String())
			} else {
				assert.Equal(t, tt.want, "")
			}
		})
	}
}

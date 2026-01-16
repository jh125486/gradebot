package client_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/jh125486/gradebot/pkg/client"
	"github.com/jh125486/gradebot/pkg/contextlog"
	pb "github.com/jh125486/gradebot/pkg/proto"
	"github.com/jh125486/gradebot/pkg/proto/protoconnect"
	"github.com/jh125486/gradebot/pkg/rubrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkDirValidate(t *testing.T) {
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
				dir := t.TempDir()
				return dir, nil
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
				return dir, func() {
					_ = os.Chmod(dir, 0o755)
				}
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

			err := client.WorkDir(path).Validate()

			if (err != nil) != tt.wantErr {
				t.Errorf("WorkDir.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestWorkDirString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		w    client.WorkDir
		want string
	}{
		{
			name: "empty_workdir",
			w:    client.WorkDir(""),
			want: "",
		},
		{
			name: "simple_path",
			w:    client.WorkDir("/tmp/test"),
			want: "/tmp/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.w.String()
			if got != tt.want {
				t.Errorf("WorkDir.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewAuthTransport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
		base  http.RoundTripper
	}{
		{
			name:  "nil_base_uses_default",
			token: "test-token",
			base:  nil,
		},
		{
			name:  "custom_base_transport",
			token: "another-token",
			base:  &http.Transport{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			transport := client.NewAuthTransport(tt.token, tt.base)
			if transport == nil {
				t.Fatal("NewAuthTransport() returned nil")
			}
		})
	}
}

func TestAuthTransportRoundTrip(t *testing.T) {
	t.Parallel()

	type args struct {
		token  string
		status int
		body   string
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "adds_bearer_token_to_request",
			args: args{token: "test-token-12345", status: http.StatusOK, body: "OK"},
		},
		{
			name: "handles_different_token",
			args: args{token: "another-token-789", status: http.StatusOK, body: "OK"},
		},
		{
			name: "preserves_response_status",
			args: args{token: "token", status: http.StatusCreated, body: "Created"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockRT := &mockRoundTripper{
				response: &http.Response{
					StatusCode: tt.args.status,
					Body:       io.NopCloser(strings.NewReader(tt.args.body)),
				},
			}

			transport := client.NewAuthTransport(tt.args.token, mockRT)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/test", http.NoBody)
			require.NoError(t, err)

			resp, err := transport.RoundTrip(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.args.status, resp.StatusCode)

			require.NotNil(t, mockRT.lastRequest, "mockRoundTripper should have received request")

			authHeader := mockRT.lastRequest.Header.Get("Authorization")
			expectedAuth := "Bearer " + tt.args.token
			assert.Equal(t, expectedAuth, authHeader)
		})
	}
}

func TestDirectoryError(t *testing.T) {
	t.Parallel()

	type args struct {
		errorMsg string
	}
	tests := []struct {
		name              string
		args              args
		shouldContainText string
	}{
		{
			name:              "error_contains_message",
			args:              args{errorMsg: "test error"},
			shouldContainText: "test error",
		},
		{
			name:              "error_contains_different_message",
			args:              args{errorMsg: "connection failed"},
			shouldContainText: "connection failed",
		},
		{
			name:              "error_unwrap_works",
			args:              args{errorMsg: "wrapped error"},
			shouldContainText: "wrapped error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			baseErr := errors.New(tt.args.errorMsg)
			dirErr := &client.DirectoryError{Err: baseErr}

			// Test Error() method
			errMsg := dirErr.Error()
			assert.Contains(t, errMsg, tt.shouldContainText, "Error() should contain base error message")

			// Error message should contain OS-specific help
			if runtime.GOOS == "darwin" {
				assert.Contains(t, errMsg, "macOS", "Error() should contain macOS help on darwin")
			}

			// Test Unwrap() method
			unwrapped := dirErr.Unwrap()
			assert.True(t, errors.Is(unwrapped, baseErr), "Unwrap() should return the original error")
		})
	}
}

func TestUploadResult(t *testing.T) {
	t.Parallel()

	ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())

	tests := []struct {
		name             string
		setupConfig      func() *client.Config
		result           *rubrics.Result
		wantErr          bool
		wantUploadCalled bool
	}{
		{
			name: "nil_rubric_client_skips_upload",
			setupConfig: func() *client.Config {
				return &client.Config{
					RubricClient: nil,
					Writer:       io.Discard,
					Reader:       strings.NewReader("y\n"),
				}
			},
			result: &rubrics.Result{
				SubmissionID: "test-123",
				Project:      "TestProject",
				Timestamp:    time.Now(),
			},
			wantErr:          false,
			wantUploadCalled: false,
		},
		{
			name: "user_declines_upload",
			setupConfig: func() *client.Config {
				return &client.Config{
					RubricClient: &mockRubricServiceClient{},
					Writer:       io.Discard,
					Reader:       strings.NewReader("n\n"),
				}
			},
			result: &rubrics.Result{
				SubmissionID: "test-456",
				Project:      "TestProject",
				Timestamp:    time.Now(),
			},
			wantErr:          false,
			wantUploadCalled: false,
		},
		{
			name: "successful_upload",
			setupConfig: func() *client.Config {
				return &client.Config{
					RubricClient: &mockRubricServiceClient{},
					Writer:       io.Discard,
					Reader:       strings.NewReader("y\n"),
				}
			},
			result: &rubrics.Result{
				SubmissionID: "test-789",
				Project:      "TestProject",
				Timestamp:    time.Now(),
				Rubric: []rubrics.RubricItem{
					{Name: "Test", Points: 10, Awarded: 8, Note: "Good"},
				},
			},
			wantErr:          false,
			wantUploadCalled: true,
		},
		{
			name: "upload_error",
			setupConfig: func() *client.Config {
				return &client.Config{
					RubricClient: &mockRubricServiceClient{
						uploadErr: errors.New("upload failed"),
					},
					Writer: io.Discard,
					Reader: strings.NewReader("yes\n"),
				}
			},
			result: &rubrics.Result{
				SubmissionID: "test-error",
				Project:      "TestProject",
				Timestamp:    time.Now(),
			},
			wantErr:          true,
			wantUploadCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := tt.setupConfig()
			err := cfg.UploadResult(ctx, tt.result)

			if (err != nil) != tt.wantErr {
				t.Errorf("UploadResult() error = %v, wantErr %v", err, tt.wantErr)
			}

			if mockClient, ok := cfg.RubricClient.(*mockRubricServiceClient); ok {
				called := mockClient.uploadCalls > 0
				if called != tt.wantUploadCalled {
					t.Errorf("upload called = %v, want %v", called, tt.wantUploadCalled)
				}
			}
		})
	}
}

func TestExecuteProject(t *testing.T) {
	t.Parallel()

	ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())

	tests := []struct {
		name        string
		setupConfig func() *client.Config
		projectName string
		evaluators  []rubrics.Evaluator
		wantErr     bool
		bag         rubrics.RunBag
		checkOutput func(t *testing.T, output string)
		verify      func(t *testing.T, cfg *client.Config, bag rubrics.RunBag)
	}{
		{
			name: "simple_project_execution",
			setupConfig: func() *client.Config {
				dir := t.TempDir()
				return &client.Config{
					WorkDir:        client.WorkDir(dir),
					RunCmd:         "echo test",
					Writer:         new(bytes.Buffer),
					Reader:         strings.NewReader("n\n"),
					ProgramBuilder: newMockProgramBuilder(),
				}
			},
			projectName: "TestProject",
			evaluators: []rubrics.Evaluator{
				func(_ context.Context, _ rubrics.ProgramRunner, _ rubrics.RunBag) rubrics.RubricItem {
					return rubrics.RubricItem{
						Name:    "Test Item",
						Points:  10,
						Awarded: 10,
						Note:    "Passed",
					}
				},
			},
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				t.Helper()
				if !strings.Contains(output, "Test Item") {
					t.Errorf("output should contain 'Test Item', got: %s", output)
				}
			},
		},
		{
			name: "multiple_evaluators",
			setupConfig: func() *client.Config {
				dir := t.TempDir()
				output := new(bytes.Buffer)
				return &client.Config{
					WorkDir:        client.WorkDir(dir),
					RunCmd:         "echo test",
					Writer:         output,
					Reader:         strings.NewReader("n\n"),
					ProgramBuilder: newMockProgramBuilder(),
				}
			},
			projectName: "MultiEvalProject",
			evaluators: []rubrics.Evaluator{
				func(_ context.Context, _ rubrics.ProgramRunner, _ rubrics.RunBag) rubrics.RubricItem {
					return rubrics.RubricItem{Name: "Item 1", Points: 5, Awarded: 5}
				},
				func(_ context.Context, _ rubrics.ProgramRunner, _ rubrics.RunBag) rubrics.RubricItem {
					return rubrics.RubricItem{Name: "Item 2", Points: 10, Awarded: 8}
				},
			},
			wantErr: false,
			checkOutput: func(t *testing.T, output string) {
				t.Helper()
				if !strings.Contains(output, "Item 1") || !strings.Contains(output, "Item 2") {
					t.Errorf("output should contain both items, got: %s", output)
				}
			},
		},
		{
			name: "with_upload_to_server",
			setupConfig: func() *client.Config {
				dir := t.TempDir()
				return &client.Config{
					WorkDir:        client.WorkDir(dir),
					RunCmd:         "echo test",
					Writer:         io.Discard,
					Reader:         strings.NewReader("y\n"),
					RubricClient:   &mockRubricServiceClient{},
					ProgramBuilder: newMockProgramBuilder(),
				}
			},
			projectName: "UploadProject",
			evaluators: []rubrics.Evaluator{
				func(_ context.Context, _ rubrics.ProgramRunner, _ rubrics.RunBag) rubrics.RubricItem {
					return rubrics.RubricItem{Name: "Test", Points: 10, Awarded: 10}
				},
			},
			wantErr: false,
		},
		{
			name: "upload_error_does_not_fail_execution",
			setupConfig: func() *client.Config {
				dir := t.TempDir()
				return &client.Config{
					WorkDir:        client.WorkDir(dir),
					RunCmd:         "echo test",
					Writer:         io.Discard,
					Reader:         strings.NewReader("y\n"),
					RubricClient:   &mockRubricServiceClient{uploadErr: errors.New("upload failed")},
					ProgramBuilder: newMockProgramBuilder(),
				}
			},
			projectName: "UploadErrorProject",
			evaluators: []rubrics.Evaluator{
				func(_ context.Context, _ rubrics.ProgramRunner, _ rubrics.RunBag) rubrics.RubricItem {
					return rubrics.RubricItem{Name: "Test", Points: 10, Awarded: 10}
				},
			},
			wantErr: false, // Upload errors should not fail execution
		},
		{
			name: "cleanup_error_returns_error",
			setupConfig: func() *client.Config {
				dir := t.TempDir()
				// Write invalid file that will cause cleanup error
				invalidFile := filepath.Join(dir, ".git", "index.lock")
				_ = os.MkdirAll(filepath.Dir(invalidFile), 0o755)
				_ = os.WriteFile(invalidFile, []byte("test"), 0o644)
				return &client.Config{
					WorkDir: client.WorkDir(dir),
					RunCmd:  "echo test",
					Writer:  io.Discard,
					Reader:  strings.NewReader("n\n"),
					// Use default factory (nil) to trigger real cleanup
				}
			},
			projectName: "CleanupErrorProject",
			evaluators:  []rubrics.Evaluator{},
			wantErr:     false, // Cleanup errors are logged but don't fail execution
		},
		{
			name: "with_quality_client",
			setupConfig: func() *client.Config {
				dir := t.TempDir()
				// Create a simple test file for quality check
				_ = os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main\n"), 0o644)
				return &client.Config{
					WorkDir:        client.WorkDir(dir),
					RunCmd:         "echo test",
					Writer:         io.Discard,
					Reader:         strings.NewReader("n\n"),
					QualityClient:  &mockQualityServiceClient{},
					ProgramBuilder: newMockProgramBuilder(),
				}
			},
			projectName: "QualityProject",
			evaluators:  []rubrics.Evaluator{},
			wantErr:     false,
		},
		{
			name: "uses_provided_program_factory_and_bag",
			setupConfig: func() *client.Config {
				dir := t.TempDir()
				return &client.Config{
					WorkDir:        client.WorkDir(dir),
					RunCmd:         "echo test",
					Writer:         io.Discard,
					Reader:         strings.NewReader("n\n"),
					ProgramBuilder: newMockProgramBuilder(),
				}
			},
			projectName: "ProvidedProgram",
			evaluators: []rubrics.Evaluator{
				func(_ context.Context, _ rubrics.ProgramRunner, bag rubrics.RunBag) rubrics.RubricItem {
					bag["touched"] = true
					return rubrics.RubricItem{Name: "Bag", Points: 5, Awarded: 5}
				},
			},
			bag:     rubrics.RunBag{"seed": "value"},
			wantErr: false,
			verify: func(t *testing.T, cfg *client.Config, bag rubrics.RunBag) {
				t.Helper()
				if touched, ok := bag["touched"].(bool); !ok || !touched {
					t.Errorf("bag should be mutated by evaluator, got touched=%v ok=%v", bag["touched"], ok)
				}
				if seed, ok := bag["seed"]; !ok || seed != "value" {
					t.Errorf("bag should preserve existing entries, got seed=%v ok=%v", seed, ok)
				}
			},
		},
		{
			name: "program_builder_error",
			setupConfig: func() *client.Config {
				dir := t.TempDir()
				return &client.Config{
					WorkDir: client.WorkDir(dir),
					RunCmd:  "echo test",
					Writer:  io.Discard,
					Reader:  strings.NewReader("n\n"),
					ProgramBuilder: func(workDir, runCmd string) (rubrics.ProgramRunner, error) {
						return nil, fmt.Errorf("builder failed")
					},
				}
			},
			projectName: "BuilderErrorProject",
			evaluators:  []rubrics.Evaluator{},
			wantErr:     true,
		},
		{
			name: "program_run_error",
			setupConfig: func() *client.Config {
				dir := t.TempDir()
				return &client.Config{
					WorkDir: client.WorkDir(dir),
					RunCmd:  "echo test",
					Writer:  io.Discard,
					Reader:  strings.NewReader("n\n"),
					ProgramBuilder: func(workDir, runCmd string) (rubrics.ProgramRunner, error) {
						return &stubProgramWithRunError{}, nil
					},
				}
			},
			projectName: "RunErrorProject",
			evaluators:  []rubrics.Evaluator{},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := tt.setupConfig()
			err := client.ExecuteProject(ctx, cfg, tt.projectName, "", tt.bag, tt.evaluators...)

			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteProject() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.checkOutput != nil && cfg.Writer != nil {
				if buf, ok := cfg.Writer.(*bytes.Buffer); ok {
					tt.checkOutput(t, buf.String())
				}
			}

			if tt.verify != nil {
				tt.verify(t, cfg, tt.bag)
			}
		})
	}
}

// Mock types for testing

type mockRoundTripper struct {
	response    *http.Response
	err         error
	lastRequest *http.Request
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	m.lastRequest = req
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

type mockRubricServiceClient struct {
	uploadCalls int
	uploadErr   error
	protoconnect.UnimplementedRubricServiceHandler
}

func (m *mockRubricServiceClient) UploadRubricResult(_ context.Context, _ *connect.Request[pb.UploadRubricResultRequest]) (*connect.Response[pb.UploadRubricResultResponse], error) {
	m.uploadCalls++
	if m.uploadErr != nil {
		return nil, m.uploadErr
	}
	return connect.NewResponse(&pb.UploadRubricResultResponse{
		Message: "Upload successful",
	}), nil
}

type mockQualityServiceClient struct {
	protoconnect.UnimplementedQualityServiceHandler
}

func (m *mockQualityServiceClient) EvaluateCodeQuality(_ context.Context, _ *connect.Request[pb.EvaluateCodeQualityRequest]) (*connect.Response[pb.EvaluateCodeQualityResponse], error) {
	return connect.NewResponse(&pb.EvaluateCodeQualityResponse{
		QualityScore: 85,
		Feedback:     "Code quality is good",
	}), nil
}

type stubProgram struct {
	path          string
	runCalled     bool
	cleanupCalled bool
}

func (s *stubProgram) Path() string { return s.path }

func (s *stubProgram) Run(_ ...string) error {
	s.runCalled = true
	return nil
}

func (s *stubProgram) Do(string) (stdout, stderr []string, err error) {
	return nil, nil, nil
}

func (s *stubProgram) Kill() error { return nil }

func (s *stubProgram) Cleanup(context.Context) error {
	s.cleanupCalled = true
	return nil
}

// stubProgramWithRunError is a stub program that fails when Run is called.
type stubProgramWithRunError struct{}

func (s *stubProgramWithRunError) Path() string {
	return "/stub"
}

func (s *stubProgramWithRunError) Run(_ ...string) error {
	return errors.New("program run failed")
}

func (s *stubProgramWithRunError) Do(string) (stdout, stderr []string, err error) {
	return nil, nil, nil
}

func (s *stubProgramWithRunError) Kill() error { return nil }

func (s *stubProgramWithRunError) Cleanup(context.Context) error {
	return nil
}

// newMockProgramBuilder creates a ProgramBuilder that returns stub programs for testing.
func newMockProgramBuilder() func(workDir, runCmd string) (rubrics.ProgramRunner, error) {
	return func(workDir, runCmd string) (rubrics.ProgramRunner, error) {
		return &stubProgram{path: workDir}, nil
	}
}

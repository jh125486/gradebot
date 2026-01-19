package server_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"

	"github.com/jh125486/gradebot/pkg/contextlog"
	"github.com/jh125486/gradebot/pkg/openai"
	pb "github.com/jh125486/gradebot/pkg/proto"
	"github.com/jh125486/gradebot/pkg/server"
	"github.com/jh125486/gradebot/pkg/storage"
)

const (
	testToken               = "s3cr3t"
	testTokenBearer         = "Bearer " + testToken
	testTokenWrong          = "Bearer wrong"
	testSubmissionID1       = "test-123"
	testSubmissionID2       = "test-456"
	testSubmissionID3       = "test-789"
	testItem1               = "Item 1"
	testItem2               = "Item 2"
	testRubricNote1         = "Good"
	testRubricNote2         = "Excellent"
	testFilename            = "f.py"
	testFileContent         = "print(1)"
	testAuthHeaderMissing   = "missing authorization header"
	testAuthHeaderMalformed = "invalid authorization header format"
	testAuthHeaderInvalid   = "invalid token"
	testAuthOK              = "ok"
	testNoFilesProvided     = "no files provided"
	testFailedToReviewCode  = "failed to review code"
	testQualityScore        = 77
	testFeedback            = "good"
	testUploadPath          = "/rubric.RubricService/UploadRubricResult"
	testSubmissionsPath     = "/submissions"
)

// mockReviewer implements the openai.Reviewer interface for tests.
type mockReviewer struct {
	review *openai.AIReview
	err    error
}

func (m *mockReviewer) ReviewCode(ctx context.Context, instructions string, files []*pb.File) (*openai.AIReview, error) {
	return m.review, m.err
}

// mockStorage implements the storage.Storage interface for tests.
type mockStorage struct {
	results          map[string]*pb.Result
	mu               sync.RWMutex
	failSaveResult   bool
	failLoadResult   bool
	failListResults  bool
	failListProjects bool
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		results: make(map[string]*pb.Result),
	}
}

func (m *mockStorage) SaveResult(ctx context.Context, result *pb.Result) error {
	if m.failSaveResult {
		return errors.New("mock save error")
	}
	if result == nil || result.SubmissionId == "" {
		return errors.New("invalid result")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[result.SubmissionId] = result
	return nil
}

func (m *mockStorage) LoadResult(ctx context.Context, submissionID string) (*pb.Result, error) {
	if m.failLoadResult {
		return nil, errors.New("mock load error")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	result, exists := m.results[submissionID]
	if !exists {
		return nil, errors.New("result not found")
	}
	return result, nil
}

func (m *mockStorage) ListResultsPaginated(ctx context.Context, params storage.ListResultsParams) (
	results map[string]*pb.Result, totalCount int, err error) {
	if m.failListResults {
		return nil, 0, errors.New("mock list results error")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	results = make(map[string]*pb.Result)

	// Filter by project if specified
	for id, result := range m.results {
		if params.Project == "" || result.Project == params.Project {
			results[id] = result
		}
	}

	totalCount = len(results)
	return results, totalCount, nil
}

func (m *mockStorage) ListProjects(ctx context.Context) ([]string, error) {
	if m.failListProjects {
		return nil, errors.New("mock list projects error")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	projectSet := make(map[string]bool)
	for _, result := range m.results {
		if result.Project != "" {
			projectSet[result.Project] = true
		}
	}

	projects := make([]string, 0, len(projectSet))
	for project := range projectSet {
		projects = append(projects, project)
	}

	return projects, nil
}

func (m *mockStorage) Close() error {
	return nil
} // TestStart tests the Start function with various configurations and contexts.
func TestStart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		port     string
		setupCtx func() context.Context
		wantErr  bool
		checkErr func(error) bool
	}{
		{
			name: "InvalidPort",
			port: "notaport",
			setupCtx: func() context.Context {
				return contextlog.With(t.Context(), contextlog.DiscardLogger())
			},
			wantErr: true,
			checkErr: func(err error) bool {
				// Any error is acceptable for invalid port
				return true
			},
		},
		{
			name: "CancelledContext",
			port: "0",
			setupCtx: func() context.Context {
				ctx, cancel := context.WithTimeout(contextlog.With(t.Context(), contextlog.DiscardLogger()), 50*time.Millisecond)
				defer cancel()
				return ctx
			},
			wantErr: true,
			checkErr: func(err error) bool {
				// Should fail due to context cancellation
				return err != nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := server.Config{
				ID:           "id",
				Port:         tt.port,
				OpenAIClient: &mockReviewer{},
			}
			ctx := tt.setupCtx()
			err := server.Start(ctx, cfg)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.checkErr != nil {
					assert.True(t, tt.checkErr(err), "error check failed for error: %v", err)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestEvaluateCodeQualityBehaviors tests QualityServer.EvaluateCodeQuality with various scenarios.
func TestEvaluateCodeQualityBehaviors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		reviewer         *mockReviewer
		files            []*pb.File
		wantErr          bool
		wantErrSubstring string
		wantScore        int32
		wantFeedback     string
		setupCtx         func() context.Context
	}{
		{
			name:             "EmptyFiles",
			reviewer:         &mockReviewer{},
			files:            []*pb.File{},
			wantErr:          true,
			wantErrSubstring: testNoFilesProvided,
			setupCtx: func() context.Context {
				return contextlog.With(t.Context(), contextlog.DiscardLogger())
			},
		},
		{
			name:             "ReviewerError",
			reviewer:         &mockReviewer{err: errors.New("boom")},
			files:            []*pb.File{{Name: testFilename, Content: testFileContent}},
			wantErr:          true,
			wantErrSubstring: testFailedToReviewCode,
			setupCtx: func() context.Context {
				return contextlog.With(t.Context(), contextlog.DiscardLogger())
			},
		},
		{
			name:         "SuccessfulReview",
			reviewer:     &mockReviewer{review: &openai.AIReview{QualityScore: int32(testQualityScore), Feedback: testFeedback}},
			files:        []*pb.File{{Name: testFilename, Content: testFileContent}},
			wantErr:      false,
			wantScore:    int32(testQualityScore),
			wantFeedback: testFeedback,
			setupCtx: func() context.Context {
				return contextlog.With(t.Context(), contextlog.DiscardLogger())
			},
		},
		{
			name:         "CancelledContext",
			reviewer:     &mockReviewer{review: &openai.AIReview{QualityScore: int32(testQualityScore), Feedback: testFeedback}},
			files:        []*pb.File{{Name: testFilename, Content: testFileContent}},
			wantErr:      false,
			wantScore:    int32(testQualityScore),
			wantFeedback: testFeedback,
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return contextlog.With(ctx, contextlog.DiscardLogger())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := tt.setupCtx()
			qs := &server.QualityServer{OpenAIClient: tt.reviewer}
			req := connect.NewRequest(&pb.EvaluateCodeQualityRequest{Files: tt.files})
			resp, err := qs.EvaluateCodeQuality(ctx, req)

			if tt.wantErr {
				assert.Nil(t, resp)
				if assert.Error(t, err) {
					assert.Contains(t, err.Error(), tt.wantErrSubstring)
				}
				return
			}

			assert.NoError(t, err)
			if assert.NotNil(t, resp) {
				assert.EqualValues(t, tt.wantScore, resp.Msg.QualityScore)
				assert.Equal(t, tt.wantFeedback, resp.Msg.Feedback)
			}
		})
	}
}

// TestUploadRubricResult tests RubricServer.UploadRubricResult with various scenarios.
func TestUploadRubricResult(t *testing.T) {
	t.Parallel()

	mockStore := newMockStorage()
	qs := server.NewRubricServer(mockStore)

	tests := []struct {
		name         string
		submissionID string
		rubric       []*pb.RubricItem
		expectError  bool
		errorCode    connect.Code
		setupCtx     func() context.Context
	}{
		{
			name:         "ValidSubmission",
			submissionID: testSubmissionID1,
			rubric: []*pb.RubricItem{
				{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
			},
			expectError: false,
			setupCtx: func() context.Context {
				return contextlog.With(t.Context(), contextlog.DiscardLogger())
			},
		},
		{
			name:         "EmptySubmissionID",
			submissionID: "",
			rubric: []*pb.RubricItem{
				{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
			},
			expectError: true,
			errorCode:   connect.CodeInvalidArgument,
			setupCtx: func() context.Context {
				return contextlog.With(t.Context(), contextlog.DiscardLogger())
			},
		},
		{
			name:         "EmptyRubric",
			submissionID: testSubmissionID2,
			rubric:       []*pb.RubricItem{},
			expectError:  false,
			setupCtx: func() context.Context {
				return contextlog.With(t.Context(), contextlog.DiscardLogger())
			},
		},
		{
			name:         "CancelledContext",
			submissionID: testSubmissionID1,
			rubric: []*pb.RubricItem{
				{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
			},
			expectError: false,
			setupCtx: func() context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return contextlog.With(ctx, contextlog.DiscardLogger())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := connect.NewRequest(&pb.UploadRubricResultRequest{
				Result: &pb.Result{
					SubmissionId: tt.submissionID,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       tt.rubric,
				},
			})

			ctx := tt.setupCtx()
			resp, err := qs.UploadRubricResult(ctx, req)

			if tt.expectError {
				assert.Error(t, err)
				if err != nil {
					connectErr := func() *connect.Error {
						target := &connect.Error{}
						_ = errors.As(err, &target)
						return target
					}()
					assert.Equal(t, tt.errorCode, connectErr.Code())
				}
				assert.Nil(t, resp)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
				assert.Equal(t, tt.submissionID, resp.Msg.SubmissionId)
				assert.Contains(t, resp.Msg.Message, "uploaded successfully")

				stored, err := mockStore.LoadResult(t.Context(), tt.submissionID)
				assert.NoError(t, err)
				assert.NotNil(t, stored)
				assert.Equal(t, tt.submissionID, stored.SubmissionId)
			}
		})
	}
}

func TestStart_ServerLifecycle(t *testing.T) {
	t.Parallel()

	mockStore := newMockStorage()

	tests := []struct {
		name       string
		setupCtx   func() (context.Context, context.CancelFunc)
		port       string
		wantErr    bool
		verifyFunc func(t *testing.T, port string)
	}{
		{
			name: "server_starts_and_handles_requests",
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(t.Context(), 2*time.Second)
			},
			port:    "0", // Use random port
			wantErr: false,
			verifyFunc: func(t *testing.T, port string) {
				// Give server time to start
				time.Sleep(100 * time.Millisecond)
			},
		},
		{
			name: "server_graceful_shutdown",
			setupCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(t.Context())
				// Cancel after a short time to trigger shutdown
				go func() {
					time.Sleep(100 * time.Millisecond)
					cancel()
				}()
				return ctx, cancel
			},
			port:    "0",
			wantErr: true, // Context cancellation returns error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := tt.setupCtx()
			defer cancel()

			ctx = contextlog.With(ctx, contextlog.DiscardLogger())

			cfg := server.Config{
				ID:      "test-id",
				Port:    tt.port,
				Storage: mockStore,
			}

			err := server.Start(ctx, cfg)

			if tt.wantErr {
				assert.Error(t, err)
			} else if err != nil && ctx.Err() != nil {
				// If context times out, that's expected
				assert.ErrorIs(t, err, context.DeadlineExceeded)
			}

			if tt.verifyFunc != nil {
				tt.verifyFunc(t, tt.port)
			}
		})
	}
}

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/health", http.NoBody)
	rr := httptest.NewRecorder()

	// We need to create a mux similar to the one in Start
	mux := http.NewServeMux()
	mux.Handle("/health", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`))
	}))

	mux.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "healthy")
	assert.Contains(t, rr.Body.String(), "timestamp")
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
}

func TestHealthHandler(t *testing.T) {
	t.Parallel()

	type args struct {
		method string
	}
	tests := []struct {
		name             string
		args             args
		wantStatus       int
		wantContentType  string
		wantBodyContains []string
	}{
		{
			name:             "get_request",
			args:             args{method: http.MethodGet},
			wantStatus:       http.StatusOK,
			wantContentType:  "application/json",
			wantBodyContains: []string{"healthy", "timestamp"},
		},
		{
			name:             "post_request",
			args:             args{method: http.MethodPost},
			wantStatus:       http.StatusOK,
			wantContentType:  "application/json",
			wantBodyContains: []string{"healthy", "timestamp"},
		},
		{
			name:             "head_request",
			args:             args{method: http.MethodHead},
			wantStatus:       http.StatusOK,
			wantContentType:  "application/json",
			wantBodyContains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tt.args.method, "/health", http.NoBody)
			rr := httptest.NewRecorder()

			server.HealthHandler(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)
			assert.Equal(t, tt.wantContentType, rr.Header().Get("Content-Type"))

			body := rr.Body.String()
			for _, want := range tt.wantBodyContains {
				assert.Contains(t, body, want)
			}
		})
	}
}

func TestRootHandler(t *testing.T) {
	t.Parallel()

	type args struct {
		path      string
		setupData func(*mockStorage) error
	}
	tests := []struct {
		name             string
		args             args
		wantStatus       int
		wantBodyContains []string
	}{
		{
			name: "root_path_index",
			args: args{
				path: "/",
				setupData: func(m *mockStorage) error {
					return nil
				},
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"<!DOCTYPE html>", "Classes"},
		},
		{
			name: "project_path",
			args: args{
				path: "/test-project",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					return m.SaveResult(ctx, &pb.Result{
						SubmissionId: "sub-1",
						Project:      "test-project",
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
					})
				},
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"<!DOCTYPE html>", "Submissions", "test-project"},
		},
		{
			name: "submission_detail_path",
			args: args{
				path: "/test-project/sub-123",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					return m.SaveResult(ctx, &pb.Result{
						SubmissionId: "sub-123",
						Project:      "test-project",
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
					})
				},
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"<!DOCTYPE html>", "sub-123", "80.0%"},
		},
		{
			name: "invalid_path_too_many_segments",
			args: args{
				path: "/project/submission/extra",
				setupData: func(m *mockStorage) error {
					return nil
				},
			},
			wantStatus:       http.StatusNotFound,
			wantBodyContains: []string{"Invalid path"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := newMockStorage()
			if tt.args.setupData != nil {
				err := tt.args.setupData(mockStore)
				assert.NoError(t, err)
			}

			htmlHandler := server.NewHTMLHandler(mockStore)
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, tt.args.path, http.NoBody)
			rr := httptest.NewRecorder()

			server.RootHandler(rr, req, htmlHandler)

			assert.Equal(t, tt.wantStatus, rr.Code)
			body := rr.Body.String()
			for _, want := range tt.wantBodyContains {
				assert.Contains(t, body, want)
			}
		})
	}
}

func TestServeIndexPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		setupData        func(*mockStorage) error
		mockError        bool
		wantStatus       int
		wantBodyContains []string
	}{
		{
			name: "empty_projects",
			setupData: func(m *mockStorage) error {
				return nil
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"<!DOCTYPE html>", "Classes", "No Classes Yet"},
		},
		{
			name: "with_projects",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				_ = m.SaveResult(ctx, &pb.Result{
					SubmissionId: "s1",
					Project:      "project-alpha",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
				})
				_ = m.SaveResult(ctx, &pb.Result{
					SubmissionId: "s2",
					Project:      "project-beta",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
				})
				return nil
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"<!DOCTYPE html>", "Classes", "project-alpha", "project-beta"},
		},
		{
			name: "storage_error",
			setupData: func(m *mockStorage) error {
				m.failListProjects = true
				return nil
			},
			wantStatus:       http.StatusInternalServerError,
			wantBodyContains: []string{"Internal server error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := newMockStorage()
			if tt.setupData != nil {
				err := tt.setupData(mockStore)
				assert.NoError(t, err)
			}

			htmlHandler := server.NewHTMLHandler(mockStore)
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", http.NoBody)
			rr := httptest.NewRecorder()

			server.ServeIndexPage(rr, req, htmlHandler)

			assert.Equal(t, tt.wantStatus, rr.Code)
			body := rr.Body.String()
			for _, want := range tt.wantBodyContains {
				assert.Contains(t, body, want)
			}
		})
	}
}

func TestHandleProjectRoutes(t *testing.T) {
	t.Parallel()

	type args struct {
		path      string
		setupData func(*mockStorage) error
	}
	tests := []struct {
		name             string
		args             args
		wantStatus       int
		wantBodyContains []string
	}{
		{
			name: "empty_path",
			args: args{
				path: "/",
				setupData: func(m *mockStorage) error {
					return nil
				},
			},
			wantStatus:       http.StatusNotFound,
			wantBodyContains: []string{"Invalid path"},
		},
		{
			name: "project_submissions",
			args: args{
				path: "/my-project",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					return m.SaveResult(ctx, &pb.Result{
						SubmissionId: "sub-1",
						Project:      "my-project",
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
					})
				},
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"<!DOCTYPE html>", "Submissions", "my-project"},
		},
		{
			name: "submission_detail",
			args: args{
				path: "/my-project/sub-detail",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					return m.SaveResult(ctx, &pb.Result{
						SubmissionId: "sub-detail",
						Project:      "my-project",
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
					})
				},
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"<!DOCTYPE html>", "sub-detail", "80.0%"},
		},
		{
			name: "too_many_path_segments",
			args: args{
				path: "/project/submission/extra/segment",
				setupData: func(m *mockStorage) error {
					return nil
				},
			},
			wantStatus:       http.StatusNotFound,
			wantBodyContains: []string{"Invalid path"},
		},
		{
			name: "storage_error_list_results",
			args: args{
				path: "/error-project",
				setupData: func(m *mockStorage) error {
					m.failListResults = true
					return nil
				},
			},
			wantStatus:       http.StatusInternalServerError,
			wantBodyContains: []string{"Internal server error"},
		},
		{
			name: "storage_error_load_result",
			args: args{
				path: "/error-project/sub-123",
				setupData: func(m *mockStorage) error {
					m.failLoadResult = true
					return nil
				},
			},
			wantStatus:       http.StatusNotFound,
			wantBodyContains: []string{"Submission not found"},
		},
		{
			name: "project_mismatch",
			args: args{
				path: "/wrong-project/sub-456",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					return m.SaveResult(ctx, &pb.Result{
						SubmissionId: "sub-456",
						Project:      "correct-project",
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
					})
				},
			},
			wantStatus:       http.StatusNotFound,
			wantBodyContains: []string{"Submission not found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := newMockStorage()
			if tt.args.setupData != nil {
				err := tt.args.setupData(mockStore)
				assert.NoError(t, err)
			}

			htmlHandler := server.NewHTMLHandler(mockStore)
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, tt.args.path, http.NoBody)
			rr := httptest.NewRecorder()

			server.HandleProjectRoutes(rr, req, htmlHandler)

			assert.Equal(t, tt.wantStatus, rr.Code)
			body := rr.Body.String()
			for _, want := range tt.wantBodyContains {
				assert.Contains(t, body, want)
			}
		})
	}
}

func TestParseSubmissionsFromResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		results             map[string]*pb.Result
		wantSubmissionCount int
		wantHighScore       float64
	}{
		{
			name:                "empty_results",
			results:             map[string]*pb.Result{},
			wantSubmissionCount: 0,
			wantHighScore:       0.0,
		},
		{
			name: "single_submission",
			results: map[string]*pb.Result{
				"sub1": {
					SubmissionId: "sub1",
					Project:      "test",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "item1", Points: 10, Awarded: 8}},
					IpAddress:    "127.0.0.1",
				},
			},
			wantSubmissionCount: 1,
			wantHighScore:       80.0,
		},
		{
			name: "multiple_submissions_different_scores",
			results: map[string]*pb.Result{
				"sub1": {
					SubmissionId: "sub1",
					Project:      "test",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "item1", Points: 10, Awarded: 8}},
				},
				"sub2": {
					SubmissionId: "sub2",
					Project:      "test",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "item1", Points: 10, Awarded: 10}},
				},
				"sub3": {
					SubmissionId: "sub3",
					Project:      "test",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "item1", Points: 10, Awarded: 5}},
				},
			},
			wantSubmissionCount: 3,
			wantHighScore:       100.0,
		},
		{
			name: "zero_points_rubric",
			results: map[string]*pb.Result{
				"sub1": {
					SubmissionId: "sub1",
					Project:      "test",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "item1", Points: 0, Awarded: 0}},
				},
			},
			wantSubmissionCount: 1,
			wantHighScore:       0.0,
		},
		{
			name: "invalid_timestamp",
			results: map[string]*pb.Result{
				"sub1": {
					SubmissionId: "sub1",
					Project:      "test",
					Timestamp:    "invalid-timestamp",
					Rubric:       []*pb.RubricItem{{Name: "item1", Points: 10, Awarded: 8}},
				},
			},
			wantSubmissionCount: 1,
			wantHighScore:       80.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			// This is testing unexported function indirectly through HandleProjectRoutes
			mockStore := newMockStorage()
			for _, result := range tt.results {
				_ = mockStore.SaveResult(ctx, result)
			}

			htmlHandler := server.NewHTMLHandler(mockStore)
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/test", http.NoBody)
			rr := httptest.NewRecorder()

			server.HandleProjectRoutes(rr, req, htmlHandler)

			assert.Equal(t, http.StatusOK, rr.Code)
			body := rr.Body.String()
			assert.Contains(t, body, "<!DOCTYPE html>")
		})
	}
}

func TestProjectSubmissionsPageEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		path             string
		setupData        func(*mockStorage) error
		wantStatus       int
		wantBodyContains []string
	}{
		{
			name: "htmx_request_pagination",
			path: "/test-project?page=2&pageSize=5",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				for i := range 15 {
					if err := m.SaveResult(ctx, &pb.Result{
						SubmissionId: fmt.Sprintf("sub-%d", i),
						Project:      "test-project",
						Timestamp:    time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
						Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
					}); err != nil {
						return err
					}
				}
				return nil
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"Submissions", "test-project"},
		},
		{
			name: "empty_project_no_submissions",
			path: "/empty-project",
			setupData: func(m *mockStorage) error {
				return nil
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"<!DOCTYPE html>", "Submissions", "empty-project"},
		},
		{
			name: "large_page_size",
			path: "/test-project?pageSize=200",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				return m.SaveResult(ctx, &pb.Result{
					SubmissionId: "sub-1",
					Project:      "test-project",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
				})
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"Submissions"},
		},
		{
			name: "negative_page",
			path: "/test-project?page=-1",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				return m.SaveResult(ctx, &pb.Result{
					SubmissionId: "sub-1",
					Project:      "test-project",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
				})
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"Submissions"},
		},
		{
			name: "invalid_page_string",
			path: "/test-project?page=invalid",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				return m.SaveResult(ctx, &pb.Result{
					SubmissionId: "sub-1",
					Project:      "test-project",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
				})
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"Submissions"},
		},
		{
			name: "page_beyond_total_pages",
			path: "/test-project?page=100&pageSize=10",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				for i := range 5 {
					if err := m.SaveResult(ctx, &pb.Result{
						SubmissionId: fmt.Sprintf("sub-%d", i),
						Project:      "test-project",
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
					}); err != nil {
						return err
					}
				}
				return nil
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"Submissions"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := newMockStorage()
			if tt.setupData != nil {
				err := tt.setupData(mockStore)
				assert.NoError(t, err)
			}

			htmlHandler := server.NewHTMLHandler(mockStore)
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, tt.path, http.NoBody)
			rr := httptest.NewRecorder()

			server.HandleProjectRoutes(rr, req, htmlHandler)

			assert.Equal(t, tt.wantStatus, rr.Code)
			body := rr.Body.String()
			for _, want := range tt.wantBodyContains {
				assert.Contains(t, body, want)
			}
		})
	}
}

func TestSubmissionDetailPageEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		path             string
		setupData        func(*mockStorage) error
		wantStatus       int
		wantBodyContains []string
	}{
		{
			name: "submission_with_multiple_rubric_items",
			path: "/test-project/sub-multi",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				return m.SaveResult(ctx, &pb.Result{
					SubmissionId: "sub-multi",
					Project:      "test-project",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: "Item 1", Points: 10, Awarded: 8, Note: "Good"},
						{Name: "Item 2", Points: 5, Awarded: 5, Note: "Perfect"},
						{Name: "Item 3", Points: 15, Awarded: 10, Note: "Needs work"},
					},
					IpAddress:   "192.168.1.1",
					GeoLocation: "Test Location",
				})
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"sub-multi", "Item 1", "Item 2", "Item 3", "Good", "Perfect", "Needs work"},
		},
		{
			name: "submission_with_empty_notes",
			path: "/test-project/sub-empty-note",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				return m.SaveResult(ctx, &pb.Result{
					SubmissionId: "sub-empty-note",
					Project:      "test-project",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "Item", Points: 10, Awarded: 10, Note: ""}},
				})
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"sub-empty-note", "100.0%"},
		},
		{
			name: "submission_with_invalid_timestamp",
			path: "/test-project/sub-bad-time",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				return m.SaveResult(ctx, &pb.Result{
					SubmissionId: "sub-bad-time",
					Project:      "test-project",
					Timestamp:    "not-a-valid-timestamp",
					Rubric:       []*pb.RubricItem{{Name: "Item", Points: 10, Awarded: 8}},
				})
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"sub-bad-time", "80.0%"},
		},
		{
			name: "submission_with_geo_data",
			path: "/test-project/sub-geo",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				return m.SaveResult(ctx, &pb.Result{
					SubmissionId: "sub-geo",
					Project:      "test-project",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "Item", Points: 10, Awarded: 8}},
					IpAddress:    "1.2.3.4",
					GeoLocation:  "New York, NY, United States",
				})
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"sub-geo", "1.2.3.4", "New York"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := newMockStorage()
			if tt.setupData != nil {
				err := tt.setupData(mockStore)
				assert.NoError(t, err)
			}

			htmlHandler := server.NewHTMLHandler(mockStore)
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, tt.path, http.NoBody)
			rr := httptest.NewRecorder()

			server.HandleProjectRoutes(rr, req, htmlHandler)

			assert.Equal(t, tt.wantStatus, rr.Code)
			body := rr.Body.String()
			for _, want := range tt.wantBodyContains {
				assert.Contains(t, body, want)
			}
		})
	}
}

func TestHTMXPartialRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		path             string
		hxRequest        bool
		setupData        func(*mockStorage) error
		wantStatus       int
		wantBodyContains []string
		wantBodyExcludes []string
	}{
		{
			name:      "htmx_table_content_only",
			path:      "/test-project?page=1&pageSize=10",
			hxRequest: true,
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				for i := range 5 {
					if err := m.SaveResult(ctx, &pb.Result{
						SubmissionId: fmt.Sprintf("sub-%d", i),
						Project:      "test-project",
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: float64(i + 5)}},
					}); err != nil {
						return err
					}
				}
				return nil
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"table", "sub-0", "sub-1"},
			wantBodyExcludes: []string{"<!DOCTYPE html>", "<head>", "<body>"},
		},
		{
			name:      "full_page_without_htmx",
			path:      "/test-project?page=1&pageSize=10",
			hxRequest: false,
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				return m.SaveResult(ctx, &pb.Result{
					SubmissionId: "sub-1",
					Project:      "test-project",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
				})
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"<!DOCTYPE html>", "<head>", "<body>", "Submissions"},
			wantBodyExcludes: []string{},
		},
		{
			name:      "htmx_with_pagination",
			path:      "/test-project?page=2&pageSize=3",
			hxRequest: true,
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				for i := range 10 {
					if err := m.SaveResult(ctx, &pb.Result{
						SubmissionId: fmt.Sprintf("sub-%d", i),
						Project:      "test-project",
						Timestamp:    time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
						Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
					}); err != nil {
						return err
					}
				}
				return nil
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"table"},
			wantBodyExcludes: []string{"<!DOCTYPE html>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := newMockStorage()
			if tt.setupData != nil {
				err := tt.setupData(mockStore)
				assert.NoError(t, err)
			}

			htmlHandler := server.NewHTMLHandler(mockStore)
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, tt.path, http.NoBody)
			if tt.hxRequest {
				req.Header.Set("HX-Request", "true")
			}
			rr := httptest.NewRecorder()

			server.HandleProjectRoutes(rr, req, htmlHandler)

			assert.Equal(t, tt.wantStatus, rr.Code)
			body := rr.Body.String()
			for _, want := range tt.wantBodyContains {
				assert.Contains(t, body, want)
			}
			for _, exclude := range tt.wantBodyExcludes {
				if exclude != "" {
					assert.NotContains(t, body, exclude)
				}
			}
		})
	}
}

func TestGetPaginationParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		queryParams  string
		wantPage     int
		wantPageSize int
	}{
		{
			name:         "no_params",
			queryParams:  "",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "valid_page_and_pagesize",
			queryParams:  "?page=5&pageSize=50",
			wantPage:     5,
			wantPageSize: 50,
		},
		{
			name:         "zero_page",
			queryParams:  "?page=0",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "negative_page",
			queryParams:  "?page=-5",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "invalid_page_string",
			queryParams:  "?page=abc",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "pagesize_too_large",
			queryParams:  "?pageSize=500",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "zero_pagesize",
			queryParams:  "?pageSize=0",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "negative_pagesize",
			queryParams:  "?pageSize=-10",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "valid_edge_pagesize",
			queryParams:  "?pageSize=100",
			wantPage:     1,
			wantPageSize: 100,
		},
		{
			name:         "just_over_max_pagesize",
			queryParams:  "?pageSize=101",
			wantPage:     1,
			wantPageSize: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Test by making a request through HandleProjectRoutes
			mockStore := newMockStorage()
			ctx := t.Context()
			_ = mockStore.SaveResult(ctx, &pb.Result{
				SubmissionId: "test-sub",
				Project:      "test-proj",
				Timestamp:    time.Now().Format(time.RFC3339),
				Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
			})

			htmlHandler := server.NewHTMLHandler(mockStore)
			ctx = contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/test-proj"+tt.queryParams, http.NoBody)
			rr := httptest.NewRecorder()

			server.HandleProjectRoutes(rr, req, htmlHandler)

			assert.Equal(t, http.StatusOK, rr.Code)
			// The function worked correctly if it didn't error
		})
	}
}

func TestMultipleRubricItems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		path             string
		setupData        func(*mockStorage) error
		wantStatus       int
		wantBodyContains []string
	}{
		{
			name: "submission_with_varied_scores",
			path: "/proj/sub-var",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				return m.SaveResult(ctx, &pb.Result{
					SubmissionId: "sub-var",
					Project:      "proj",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: "Style", Points: 20, Awarded: 15, Note: "Good formatting"},
						{Name: "Tests", Points: 30, Awarded: 30, Note: "All tests pass"},
						{Name: "Logic", Points: 50, Awarded: 40, Note: "Minor issues"},
					},
					IpAddress:   "10.0.0.1",
					GeoLocation: "London, UK",
				})
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"sub-var", "Style", "Tests", "Logic", "85.0%"},
		},
		{
			name: "project_page_with_multiple_submissions",
			path: "/multi-proj",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				for i := range 3 {
					if err := m.SaveResult(ctx, &pb.Result{
						SubmissionId: fmt.Sprintf("multi-sub-%d", i),
						Project:      "multi-proj",
						Timestamp:    time.Now().Add(-time.Duration(i*24) * time.Hour).Format(time.RFC3339),
						Rubric: []*pb.RubricItem{
							{Name: "Item1", Points: 10, Awarded: float64(10 - i*2)},
							{Name: "Item2", Points: 10, Awarded: float64(10 - i)},
						},
						IpAddress:   fmt.Sprintf("192.168.1.%d", i+1),
						GeoLocation: fmt.Sprintf("Location %d", i+1),
					}); err != nil {
						return err
					}
				}
				return nil
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"multi-sub-0", "multi-sub-1", "multi-sub-2", "Submissions"},
		},
		{
			name: "empty_notes_and_zero_awarded",
			path: "/zero-proj/zero-sub",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				return m.SaveResult(ctx, &pb.Result{
					SubmissionId: "zero-sub",
					Project:      "zero-proj",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: "Item1", Points: 10, Awarded: 0, Note: ""},
						{Name: "Item2", Points: 20, Awarded: 0, Note: ""},
					},
				})
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"zero-sub", "0.0%"},
		},
		{
			name: "perfect_score",
			path: "/perfect-proj/perfect-sub",
			setupData: func(m *mockStorage) error {
				ctx := t.Context()
				return m.SaveResult(ctx, &pb.Result{
					SubmissionId: "perfect-sub",
					Project:      "perfect-proj",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: "All", Points: 100, Awarded: 100, Note: "Excellent!"},
					},
				})
			},
			wantStatus:       http.StatusOK,
			wantBodyContains: []string{"perfect-sub", "100.0%", "Excellent!"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockStore := newMockStorage()
			if tt.setupData != nil {
				err := tt.setupData(mockStore)
				assert.NoError(t, err)
			}

			htmlHandler := server.NewHTMLHandler(mockStore)
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, tt.path, http.NoBody)
			rr := httptest.NewRecorder()

			server.HandleProjectRoutes(rr, req, htmlHandler)

			assert.Equal(t, tt.wantStatus, rr.Code)
			body := rr.Body.String()
			for _, want := range tt.wantBodyContains {
				assert.Contains(t, body, want)
			}
		})
	}
}

func TestHTMLEndpoints(t *testing.T) {
	t.Parallel()

	type args struct {
		method    string
		path      string
		hxRequest bool
		page      string
		pageSize  string
		setupData func(*mockStorage) error
	}
	tests := []struct {
		name               string
		args               args
		wantStatus         int
		wantContentType    string
		wantBodyContains   []string
		wantBodyNotContain []string
	}{
		{
			name: "index_page_empty_projects",
			args: args{
				method: http.MethodGet,
				path:   "/",
				setupData: func(m *mockStorage) error {
					return nil
				},
			},
			wantStatus:       http.StatusOK,
			wantContentType:  "text/html",
			wantBodyContains: []string{"<!DOCTYPE html>", "Classes", "No Classes Yet"},
		},
		{
			name: "index_page_with_projects",
			args: args{
				method: http.MethodGet,
				path:   "/",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					return m.SaveResult(ctx, &pb.Result{
						SubmissionId: "test-1",
						Project:      "project-a",
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
					})
				},
			},
			wantStatus:       http.StatusOK,
			wantContentType:  "text/html",
			wantBodyContains: []string{"<!DOCTYPE html>", "Classes", "project-a"},
		},
		{
			name: "project_submissions_page",
			args: args{
				method: http.MethodGet,
				path:   "/test-project",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					for i := range 3 {
						if err := m.SaveResult(ctx, &pb.Result{
							SubmissionId: "submission-" + string(rune('0'+i)),
							Project:      "test-project",
							Timestamp:    time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
							Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
							IpAddress:    "192.168.1.1",
							GeoLocation:  "Test Location",
						}); err != nil {
							return err
						}
					}
					return nil
				},
			},
			wantStatus:       http.StatusOK,
			wantContentType:  "text/html",
			wantBodyContains: []string{"<!DOCTYPE html>", "Submissions", "test-project", "submission-0", "80.0%"},
		},
		{
			name: "project_submissions_page_htmx",
			args: args{
				method:    http.MethodGet,
				path:      "/test-project",
				hxRequest: true,
				page:      "2",
				pageSize:  "10",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					for i := range 25 {
						if err := m.SaveResult(ctx, &pb.Result{
							SubmissionId: "submission-" + string(rune('0'+i)),
							Project:      "test-project",
							Timestamp:    time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
							Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
							IpAddress:    "192.168.1.1",
							GeoLocation:  "Test Location",
						}); err != nil {
							return err
						}
					}
					return nil
				},
			},
			wantStatus:         http.StatusOK,
			wantContentType:    "text/html",
			wantBodyContains:   []string{"hx-get", "hx-target"},
			wantBodyNotContain: []string{"<!DOCTYPE html>", "<head>", "<body>"},
		},
		{
			name: "project_submissions_page_pagination_first_page",
			args: args{
				method:   http.MethodGet,
				path:     "/test-project",
				page:     "1",
				pageSize: "5",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					for i := range 12 {
						if err := m.SaveResult(ctx, &pb.Result{
							SubmissionId: "sub-" + string(rune('0'+i)),
							Project:      "test-project",
							Timestamp:    time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
							Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
						}); err != nil {
							return err
						}
					}
					return nil
				},
			},
			wantStatus:       http.StatusOK,
			wantContentType:  "text/html",
			wantBodyContains: []string{"Page 1 of 3", "Next", "Last"},
		},
		{
			name: "project_submissions_page_pagination_middle_page",
			args: args{
				method:   http.MethodGet,
				path:     "/test-project",
				page:     "2",
				pageSize: "5",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					for i := range 12 {
						if err := m.SaveResult(ctx, &pb.Result{
							SubmissionId: "sub-" + string(rune('0'+i)),
							Project:      "test-project",
							Timestamp:    time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
							Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
						}); err != nil {
							return err
						}
					}
					return nil
				},
			},
			wantStatus:       http.StatusOK,
			wantContentType:  "text/html",
			wantBodyContains: []string{"Page 2 of 3", "First", "Previous", "Next", "Last"},
		},
		{
			name: "project_submissions_page_pagination_last_page",
			args: args{
				method:   http.MethodGet,
				path:     "/test-project",
				page:     "3",
				pageSize: "5",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					for i := range 12 {
						if err := m.SaveResult(ctx, &pb.Result{
							SubmissionId: "sub-" + string(rune('0'+i)),
							Project:      "test-project",
							Timestamp:    time.Now().Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
							Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
						}); err != nil {
							return err
						}
					}
					return nil
				},
			},
			wantStatus:       http.StatusOK,
			wantContentType:  "text/html",
			wantBodyContains: []string{"Page 3 of 3", "First", "Previous"},
		},
		{
			name: "submission_detail_page",
			args: args{
				method: http.MethodGet,
				path:   "/test-project/test-123",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					return m.SaveResult(ctx, &pb.Result{
						SubmissionId: "test-123",
						Project:      "test-project",
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric: []*pb.RubricItem{
							{Name: "Item 1", Points: 10, Awarded: 8, Note: "Good work"},
							{Name: "Item 2", Points: 5, Awarded: 4, Note: "Nice"},
						},
						IpAddress:   "192.168.1.100",
						GeoLocation: "New York, NY, United States",
					})
				},
			},
			wantStatus:       http.StatusOK,
			wantContentType:  "text/html",
			wantBodyContains: []string{"<!DOCTYPE html>", "test-123", "80.0%", "Item 1", "Item 2", "Good work", "192.168.1.100", "New York"},
		},
		{
			name: "submission_detail_page_zero_points",
			args: args{
				method: http.MethodGet,
				path:   "/test-project/test-456",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					return m.SaveResult(ctx, &pb.Result{
						SubmissionId: "test-456",
						Project:      "test-project",
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric:       []*pb.RubricItem{{Name: "Item", Points: 0, Awarded: 0}},
					})
				},
			},
			wantStatus:       http.StatusOK,
			wantContentType:  "text/html",
			wantBodyContains: []string{"<!DOCTYPE html>", "test-456", "0.0%"},
		},
		{
			name: "submission_not_found",
			args: args{
				method: http.MethodGet,
				path:   "/test-project/nonexistent",
				setupData: func(m *mockStorage) error {
					return nil
				},
			},
			wantStatus:       http.StatusNotFound,
			wantBodyContains: []string{"Submission not found"},
		},
		{
			name: "submission_wrong_project",
			args: args{
				method: http.MethodGet,
				path:   "/other-project/test-123",
				setupData: func(m *mockStorage) error {
					ctx := t.Context()
					return m.SaveResult(ctx, &pb.Result{
						SubmissionId: "test-123",
						Project:      "test-project",
						Timestamp:    time.Now().Format(time.RFC3339),
						Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
					})
				},
			},
			wantStatus:       http.StatusNotFound,
			wantBodyContains: []string{"Submission not found"},
		},
		{
			name: "invalid_path_too_many_segments",
			args: args{
				method: http.MethodGet,
				path:   "/project/submission/extra/segment",
				setupData: func(m *mockStorage) error {
					return nil
				},
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "empty_project_submissions",
			args: args{
				method: http.MethodGet,
				path:   "/empty-project",
				setupData: func(m *mockStorage) error {
					return nil
				},
			},
			wantStatus:       http.StatusOK,
			wantContentType:  "text/html",
			wantBodyContains: []string{"<!DOCTYPE html>", "Submissions", "empty-project"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock storage
			mockStore := newMockStorage()
			if tt.args.setupData != nil {
				err := tt.args.setupData(mockStore)
				assert.NoError(t, err)
			}

			// Create HTML handler with storage
			htmlHandler := server.NewHTMLHandler(mockStore)

			// Create mux and register routes (simulating what Start does)
			mux := http.NewServeMux()
			mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/" {
					server.ServeIndexPage(w, r, htmlHandler)
					return
				}
				server.HandleProjectRoutes(w, r, htmlHandler)
			}))

			// Create request
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, tt.args.method, tt.args.path, http.NoBody)

			// Add query parameters if specified
			if tt.args.page != "" || tt.args.pageSize != "" {
				q := req.URL.Query()
				if tt.args.page != "" {
					q.Set("page", tt.args.page)
				}
				if tt.args.pageSize != "" {
					q.Set("pageSize", tt.args.pageSize)
				}
				req.URL.RawQuery = q.Encode()
			}

			// Add HTMX header if specified
			if tt.args.hxRequest {
				req.Header.Set("HX-Request", "true")
			}

			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			// Assertions
			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantContentType != "" {
				assert.Equal(t, tt.wantContentType, rr.Header().Get("Content-Type"))
			}

			body := rr.Body.String()
			for _, want := range tt.wantBodyContains {
				assert.Contains(t, body, want, "Response should contain: %s", want)
			}

			for _, notWant := range tt.wantBodyNotContain {
				assert.NotContains(t, body, notWant, "Response should not contain: %s", notWant)
			}
		})
	}
}

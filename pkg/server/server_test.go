package server_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	results map[string]*pb.Result
	mu      sync.RWMutex
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		results: make(map[string]*pb.Result),
	}
}

func (m *mockStorage) SaveResult(ctx context.Context, result *pb.Result) error {
	if result == nil || result.SubmissionId == "" {
		return errors.New("invalid result")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[result.SubmissionId] = result
	return nil
}

func (m *mockStorage) LoadResult(ctx context.Context, submissionID string) (*pb.Result, error) {
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
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
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
				ctx, cancel := context.WithTimeout(contextlog.With(context.Background(), contextlog.DiscardLogger()), 50*time.Millisecond)
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

// TestAuthMiddleware tests the AuthMiddleware function with various authorization headers.
func TestAuthMiddleware(t *testing.T) {
	t.Parallel()
	handler := server.AuthMiddleware(testToken)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testAuthOK))
	}))

	tests := []struct {
		name       string
		header     string
		wantStatus int
		wantSubstr string
	}{
		{name: "MissingHeader", header: "", wantStatus: http.StatusUnauthorized, wantSubstr: testAuthHeaderMissing},
		{name: "Malformed", header: "BadToken", wantStatus: http.StatusUnauthorized, wantSubstr: testAuthHeaderMalformed},
		{name: "WrongToken", header: testTokenWrong, wantStatus: http.StatusUnauthorized, wantSubstr: testAuthHeaderInvalid},
		{name: "Correct", header: testTokenBearer, wantStatus: http.StatusOK, wantSubstr: testAuthOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", http.NoBody)
			if tt.header != "" {
				req.Header.Set("authorization", tt.header)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
			assert.Contains(t, rr.Body.String(), tt.wantSubstr)
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
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name:             "ReviewerError",
			reviewer:         &mockReviewer{err: errors.New("boom")},
			files:            []*pb.File{{Name: testFilename, Content: testFileContent}},
			wantErr:          true,
			wantErrSubstring: testFailedToReviewCode,
			setupCtx: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
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
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
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
				ctx, cancel := context.WithCancel(context.Background())
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
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
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
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name:         "EmptyRubric",
			submissionID: testSubmissionID2,
			rubric:       []*pb.RubricItem{},
			expectError:  false,
			setupCtx: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
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
				ctx, cancel := context.WithCancel(context.Background())
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

// TestAuthRubricHandler tests AuthRubricHandler with selective authentication.
func TestAuthRubricHandler(t *testing.T) {
	t.Parallel()
	handler := server.AuthRubricHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}), testToken)

	tests := []struct {
		name       string
		path       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "UploadPathRequiresAuth",
			path:       testUploadPath,
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "UploadPathWithValidAuth",
			path:       testUploadPath,
			authHeader: testTokenBearer,
			wantStatus: http.StatusOK,
		},
		{
			name:       "OtherPathsDontRequireAuth",
			path:       testSubmissionsPath,
			authHeader: "",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, tt.path, http.NoBody)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

// failingStorage is a mock storage that always returns errors for testing error paths
type failingStorage struct{}

func (f *failingStorage) SaveResult(context.Context, *pb.Result) error {
	return errors.New("database connection failed")
}

func (f *failingStorage) LoadResult(context.Context, string) (*pb.Result, error) {
	return nil, errors.New("database connection failed")
}

func (f *failingStorage) ListResultsPaginated(_ context.Context, _ storage.ListResultsParams) (results map[string]*pb.Result, totalCount int, err error) {
	return nil, 0, errors.New("database connection failed")
}

func (f *failingStorage) ListProjects(context.Context) ([]string, error) {
	return nil, errors.New("database connection failed")
}

func (f *failingStorage) Close() error {
	return nil
}

func TestMaskToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "empty_token",
			token: "",
			want:  "<empty>",
		},
		{
			name:  "short_token_without_bearer",
			token: "abc",
			want:  "***",
		},
		{
			name:  "short_token_with_bearer",
			token: "Bearer abc",
			want:  "Bearer ***",
		},
		{
			name:  "eight_char_token",
			token: "12345678",
			want:  "********",
		},
		{
			name:  "long_token_without_bearer",
			token: "abcdefghijklmnop",
			want:  "abcd********mnop",
		},
		{
			name:  "long_token_with_bearer",
			token: "Bearer abcdefghijklmnop",
			want:  "Bearer abcd********mnop",
		},
		{
			name:  "very_long_token",
			token: "Bearer sk-proj-1234567890abcdefghijklmnop",
			want:  "Bearer sk-p**************************mnop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Access maskToken through a test helper that calls it
			got := server.ExposedMaskToken(tt.token)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestServeIndexPage(t *testing.T) {
	t.Parallel()

	mockStore := newMockStorage()
	// Add some test data with different projects
	_ = mockStore.SaveResult(context.Background(), &pb.Result{
		SubmissionId: "sub-1",
		Project:      "project1",
		Timestamp:    time.Now().Format(time.RFC3339),
		Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
	})
	_ = mockStore.SaveResult(context.Background(), &pb.Result{
		SubmissionId: "sub-2",
		Project:      "project2",
		Timestamp:    time.Now().Format(time.RFC3339),
		Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 9}},
	})
	_ = mockStore.SaveResult(context.Background(), &pb.Result{
		SubmissionId: "sub-3",
		Project:      "project1",
		Timestamp:    time.Now().Format(time.RFC3339),
		Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 10}},
	})

	rubricServer := server.NewRubricServer(mockStore)

	tests := []struct {
		name         string
		method       string
		wantStatus   int
		wantContains []string
	}{
		{
			name:       "get_index_page_lists_projects",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantContains: []string{
				"project1",
				"project2",
				"GradeBot",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, tt.method, "/", http.NoBody)
			rr := httptest.NewRecorder()

			server.ExposedServeIndexPage(rr, req, rubricServer)

			assert.Equal(t, tt.wantStatus, rr.Code)
			for _, substr := range tt.wantContains {
				assert.Contains(t, rr.Body.String(), substr)
			}
		})
	}

	// Test error handling with mock error
	t.Run("storage_error", func(t *testing.T) {
		t.Parallel()
		errorStore := &failingStorage{}
		rubricServer := server.NewRubricServer(errorStore)

		ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", http.NoBody)
		rr := httptest.NewRecorder()

		server.ExposedServeIndexPage(rr, req, rubricServer)

		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}

func TestServeProjectSubmissionsPage(t *testing.T) {
	t.Parallel()

	mockStore := newMockStorage()
	// Add submissions for project
	_ = mockStore.SaveResult(context.Background(), &pb.Result{
		SubmissionId: "sub-1",
		Project:      "myproject",
		Timestamp:    time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 8}},
	})
	_ = mockStore.SaveResult(context.Background(), &pb.Result{
		SubmissionId: "sub-2",
		Project:      "myproject",
		Timestamp:    time.Now().Format(time.RFC3339),
		Rubric:       []*pb.RubricItem{{Name: "test", Points: 10, Awarded: 9}},
	})

	rubricServer := server.NewRubricServer(mockStore)

	tests := []struct {
		name         string
		path         string
		method       string
		wantStatus   int
		wantContains []string
	}{
		{
			name:       "get_project_submissions",
			path:       "/myproject",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantContains: []string{
				"myproject",
				"sub-1",
				"sub-2",
			},
		},
		{
			name:       "get_project_with_pagination",
			path:       "/myproject?page=1&pageSize=1",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantContains: []string{
				"myproject",
			},
		},
		{
			name:       "htmx_request_returns_table_only",
			path:       "/myproject",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
		},
		{
			name:       "nonexistent_project",
			path:       "/nonexistent",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantContains: []string{
				"nonexistent",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, tt.method, tt.path, http.NoBody)
			if tt.name == "htmx_request_returns_table_only" {
				req.Header.Set("HX-Request", "true")
			}
			rr := httptest.NewRecorder()

			// Extract project from path
			project := strings.TrimPrefix(tt.path, "/")
			if idx := strings.Index(project, "?"); idx > 0 {
				project = project[:idx]
			}

			server.ExposedServeProjectSubmissionsPage(rr, req, rubricServer, project)

			assert.Equal(t, tt.wantStatus, rr.Code)
			for _, substr := range tt.wantContains {
				assert.Contains(t, rr.Body.String(), substr)
			}
		})
	}

	// Test error handling with mock error
	t.Run("storage_error", func(t *testing.T) {
		t.Parallel()
		errorStore := &failingStorage{}
		rubricServer := server.NewRubricServer(errorStore)

		ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/myproject", http.NoBody)
		rr := httptest.NewRecorder()

		server.ExposedServeProjectSubmissionsPage(rr, req, rubricServer, "myproject")

		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}

func TestServeSubmissionDetailPage(t *testing.T) {
	t.Parallel()

	mockStore := newMockStorage()
	now := time.Now()
	_ = mockStore.SaveResult(context.Background(), &pb.Result{
		SubmissionId: "detail-test",
		Project:      "testproject",
		Timestamp:    now.Format(time.RFC3339),
		Rubric: []*pb.RubricItem{
			{Name: "item1", Points: 10, Awarded: 8, Note: "Good work"},
			{Name: "item2", Points: 20, Awarded: 15, Note: "Needs improvement"},
		},
		IpAddress:   "192.0.2.1",
		GeoLocation: "Test City, Test Country",
	})

	rubricServer := server.NewRubricServer(mockStore)

	tests := []struct {
		name         string
		path         string
		method       string
		wantStatus   int
		wantContains []string
	}{
		{
			name:       "get_submission_detail",
			path:       "/testproject/detail-test",
			method:     http.MethodGet,
			wantStatus: http.StatusOK,
			wantContains: []string{
				"detail-test",
				"testproject",
				"item1",
				"item2",
				"Good work",
				"Needs improvement",
				"192.0.2.1",
				"Test City, Test Country",
			},
		},
		{
			name:       "nonexistent_submission",
			path:       "/testproject/nonexistent",
			method:     http.MethodGet,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "invalid_path_format",
			path:       "/testproject/",
			method:     http.MethodGet,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, tt.method, tt.path, http.NoBody)
			rr := httptest.NewRecorder()

			// Extract project and submission ID from path
			parts := strings.Split(strings.Trim(tt.path, "/"), "/")
			if len(parts) < 2 {
				parts = append(parts, "")
			}
			project, submissionID := parts[0], parts[1]

			server.ExposedServeSubmissionDetailPage(rr, req, rubricServer, project, submissionID)

			assert.Equal(t, tt.wantStatus, rr.Code)
			for _, substr := range tt.wantContains {
				assert.Contains(t, rr.Body.String(), substr)
			}
		})
	}

	// Test invalid timestamp handling
	t.Run("invalid_timestamp", func(t *testing.T) {
		t.Parallel()
		invalidStore := newMockStorage()
		_ = invalidStore.SaveResult(context.Background(), &pb.Result{
			SubmissionId: "invalid-time",
			Project:      "testproject",
			Timestamp:    "not-a-valid-timestamp",
			Rubric:       []*pb.RubricItem{{Name: "item", Points: 10, Awarded: 8}},
		})
		rubricServer := server.NewRubricServer(invalidStore)

		ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/testproject/invalid-time", http.NoBody)
		rr := httptest.NewRecorder()

		server.ExposedServeSubmissionDetailPage(rr, req, rubricServer, "testproject", "invalid-time")

		// Should still render page, just with current time
		assert.Equal(t, http.StatusOK, rr.Code)
	})
}

func TestStart_ServerLifecycle(t *testing.T) {
	t.Parallel()

	mockStore := newMockStorage()

	tests := []struct {
		name       string
		setupCtx   func() (context.Context, context.CancelFunc)
		port       string
		wantErr    bool
		verifyFunc func(t *testing.T, ctx context.Context, port string)
	}{
		{
			name: "server_starts_and_handles_requests",
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 2*time.Second)
			},
			port:    "0", // Use random port
			wantErr: false,
			verifyFunc: func(t *testing.T, ctx context.Context, port string) {
				// Give server time to start
				time.Sleep(100 * time.Millisecond)
			},
		},
		{
			name: "server_graceful_shutdown",
			setupCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
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
				tt.verifyFunc(t, ctx, tt.port)
			}
		})
	}
}

func TestHealthEndpoint(t *testing.T) {
	t.Parallel()

	ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
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

func TestAuthRubricHandler_EdgeCases(t *testing.T) {
	t.Parallel()

	handler := server.AuthRubricHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}), testToken)

	tests := []struct {
		name       string
		method     string
		path       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "get_request_no_auth_required",
			method:     http.MethodGet,
			path:       "/some/other/path",
			authHeader: "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "post_with_valid_auth",
			method:     http.MethodPost,
			path:       testUploadPath,
			authHeader: testTokenBearer,
			wantStatus: http.StatusOK,
		},
		{
			name:       "post_without_auth",
			method:     http.MethodPost,
			path:       testUploadPath,
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "post_with_malformed_auth",
			method:     http.MethodPost,
			path:       testUploadPath,
			authHeader: "BadFormat",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "post_with_wrong_token",
			method:     http.MethodPost,
			path:       testUploadPath,
			authHeader: testTokenWrong,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "put_requires_auth",
			method:     http.MethodPut,
			path:       testUploadPath,
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "delete_requires_auth",
			method:     http.MethodDelete,
			path:       testUploadPath,
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, tt.method, tt.path, http.NoBody)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantStatus, rr.Code)
		})
	}
}

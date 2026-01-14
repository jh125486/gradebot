package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"

	"github.com/jh125486/gradebot/pkg/contextlog"
	pb "github.com/jh125486/gradebot/pkg/proto"
	"github.com/jh125486/gradebot/pkg/storage"
)

// Test constants for reducing duplication.
const (
	testToken                 = "s3cr3t"
	testTokenBearer           = "Bearer " + testToken
	testIP1                   = "127.0.0.1:12345"
	testIP2                   = "192.168.1.100"
	testIP3                   = "192.168.1.101"
	testIP4                   = "192.168.1.102"
	testIP5                   = "10.0.0.1"
	testIP6                   = "192.168.1.103"
	testIP7                   = "192.168.1.104"
	testIP8                   = "203.0.113.4:8080"
	testIP9                   = "203.0.113.4"
	testSubmissionID1         = "test-123"
	testSubmissionID2         = "test-456"
	testSubmissionID3         = "test-789"
	testProject               = "test-project"
	testItem1                 = "Item 1"
	testItem2                 = "Item 2"
	testRubricNote1           = "Good"
	testRubricNote2           = "Excellent"
	testRubricNote3           = "Very Good"
	testRubricNote4           = "No points"
	testRubricNote5           = "Good work"
	testLocation1             = "New York, NY, United States"
	testLocation2             = "Los Angeles, CA, United States"
	testUploadPath            = "/rubric.RubricService/UploadRubricResult"
	testSubmissionsPath       = "/submissions"
	testDoctype               = "<!DOCTYPE html>"
	testResponseShouldContain = "Response should contain: %s"
	testHTMXContentType       = "text/html"
	testHTMXAttrGet           = "hx-get"
	testHTMXAttrTarget        = "hx-target"
	testSubmission0           = "submission-0"
	testSubmission17          = "submission-17"
	testSubmission18          = "submission-18"
	testSubmission4           = "submission-4"
	testSubmission5           = "submission-5"
	testSubmission9           = "submission-9"
	testTotalSubmissions      = "Total Submissions"
	testHighestScore          = "Highest Score"
	testSubmissionNotFound    = "Submission not found"
	testInternalServerError   = "Internal server error"
	testStorageConnFailed     = "storage connection failed"
	testInvalidTimestamp      = "invalid-timestamp"
	testRealIP                = "127.0.0.1"
	testLocation              = "Test Location"
	testPageItemDisabled      = "page-item disabled"
	testTotalPaginationItems  = "525 total"
)

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
		return fmt.Errorf("invalid result")
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
		return nil, fmt.Errorf("result not found")
	}
	return result, nil
}

func (m *mockStorage) ListResultsPaginated(ctx context.Context, params storage.ListResultsParams) (
	results map[string]*pb.Result, totalCount int, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create sorted list of all submissions, filtering by project if specified
	keys := make([]string, 0, len(m.results))
	for k, result := range m.results {
		if params.Project == "" || result.Project == params.Project {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	totalCount = len(keys)

	// Calculate pagination
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 {
		params.PageSize = 20
	}

	startIdx := (params.Page - 1) * params.PageSize
	endIdx := startIdx + params.PageSize
	if startIdx >= totalCount && totalCount > 0 {
		startIdx = max(totalCount-params.PageSize, 0)
		endIdx = totalCount
	}
	if endIdx > totalCount {
		endIdx = totalCount
	}

	// Get the requested page
	results = make(map[string]*pb.Result)
	for i := startIdx; i < endIdx; i++ {
		key := keys[i]
		results[key] = m.results[key]
	}

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
	sort.Strings(projects)

	return projects, nil
}

func (m *mockStorage) Close() error {
	return nil
}

// createTestRubricServer creates a RubricServer for testing
func createTestRubricServer(t *testing.T, mockStore *mockStorage) *RubricServer {
	t.Helper()
	return &RubricServer{
		storage:   mockStore,
		templates: NewTemplateManager(),
		geoClient: &GeoLocationClient{
			Client: &http.Client{},
		},
	}
}

// mockConnectRequest wraps a connect request with mock peer info for testing
type mockConnectRequest struct {
	*connect.Request[pb.UploadRubricResultRequest]
	peerAddr string
}

func (m *mockConnectRequest) Peer() connect.Peer {
	return connect.Peer{Addr: m.peerAddr}
}

func (m *mockConnectRequest) Header() http.Header {
	return m.Request.Header()
}

// getClientIPFromMock is a test version of getClientIP that works with our mock
func getClientIPFromMock(ctx context.Context, req *mockConnectRequest) string {
	return extractClientIP(ctx, req)
}

func TestGetClientIP(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		headers  map[string]string
		peerAddr string
		expected string
	}{
		{
			name:     "RealIPFromContext",
			headers:  map[string]string{},
			peerAddr: testIP1,
			expected: testIP2,
		},
		{
			name:     "XForwardedFor",
			headers:  map[string]string{"X-Forwarded-For": testIP3},
			peerAddr: testIP1,
			expected: testIP3,
		},
		{
			name:     "XForwardedForMultiple",
			headers:  map[string]string{"X-Forwarded-For": testIP4 + ", " + testIP5},
			peerAddr: testIP1,
			expected: testIP4,
		},
		{
			name:     "XRealIP",
			headers:  map[string]string{"X-Real-IP": testIP6},
			peerAddr: testIP1,
			expected: testIP6,
		},
		{
			name:     "CFConnectingIP",
			headers:  map[string]string{"CF-Connecting-IP": testIP7},
			peerAddr: testIP1,
			expected: testIP7,
		},
		{
			name:     "PeerAddrFallback",
			headers:  map[string]string{},
			peerAddr: testIP8,
			expected: testIP9,
		},
		{
			name:     "UnknownFallback",
			headers:  map[string]string{},
			peerAddr: "",
			expected: unknownIP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create base context - use t.Context() or context.Background() for test setup
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			if tt.name == "RealIPFromContext" {
				ctx = context.WithValue(ctx, RealIPKey, "192.168.1.100")
			}

			// Create a mock request
			req := connect.NewRequest(&pb.UploadRubricResultRequest{
				Result: &pb.Result{SubmissionId: "test"},
			})

			// Set headers
			for k, v := range tt.headers {
				req.Header().Set(k, v)
			}

			// For peer testing, we need to create a request with peer info
			if tt.name == "PeerAddrFallback" {
				// Create a custom request structure to simulate peer info
				mockReq := &mockConnectRequest{
					Request:  req,
					peerAddr: tt.peerAddr,
				}
				result := getClientIPFromMock(ctx, mockReq)
				assert.Equal(t, tt.expected, result)
				return
			}

			if tt.name == "UnknownFallback" {
				result := getClientIP(ctx, req)
				assert.Equal(t, tt.expected, result)
				return
			}

			result := getClientIP(ctx, req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// mockRoundTripper returns a pre-configured HTTP response without any real HTTP calls
type mockRoundTripper struct {
	response *http.Response
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// If no response is configured, return a default successful geo location response
	if m.response == nil {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     http.StatusText(http.StatusOK),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"city": "Test City", "region": "Test Region", "country_name": "Test Country"}`)),
			Request:    req,
		}, nil
	}

	// Set the request on the response and return it
	m.response.Request = req
	return m.response, nil
}

func TestRealIPMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		requestIP      string
		expectedRealIP string
	}{
		{
			name:           "ValidRemoteAddr",
			requestIP:      testIP2 + ":8080",
			expectedRealIP: testIP2,
		},
		{
			name:           "Localhost",
			requestIP:      "127.0.0.1:8080",
			expectedRealIP: testRealIP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a test handler that checks the context
			handler := RealIPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				realIP := r.Context().Value(RealIPKey)
				assert.Equal(t, tt.expectedRealIP, realIP)
				w.WriteHeader(http.StatusOK)
			}))
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/", http.NoBody)
			req.RemoteAddr = tt.requestIP
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, http.StatusOK, rr.Code)
		})
	}
}

func TestServeSubmissionsPage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		setupResults       map[string]*pb.Result
		expectedStatusCode int
		expectedContent    []string
	}{
		{
			name:               "EmptyResults",
			setupResults:       map[string]*pb.Result{},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testTotalSubmissions, testHighestScore},
		},
		{
			name: "SingleSubmission",
			setupResults: map[string]*pb.Result{
				testSubmissionID1: {
					SubmissionId: testSubmissionID1,
					Project:      testProject,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
						{Name: testItem2, Points: 5.0, Awarded: 4.0, Note: testRubricNote2},
					},
					IpAddress:   testIP2,
					GeoLocation: testLocation1,
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testTotalSubmissions, testSubmissionID1, "80.0%"},
		},
		{
			name: "MultipleSubmissions",
			setupResults: map[string]*pb.Result{
				testSubmissionID1: {
					SubmissionId: testSubmissionID1,
					Project:      testProject,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
					},
					IpAddress:   testIP2,
					GeoLocation: testLocation1,
				},
				testSubmissionID2: {
					SubmissionId: testSubmissionID2,
					Project:      testProject,
					Timestamp:    time.Now().Add(-time.Hour).Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 20.0, Awarded: 15.0, Note: testRubricNote3},
					},
					IpAddress:   testIP5,
					GeoLocation: testLocation2,
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testTotalSubmissions, testHighestScore, testSubmissionID1, testSubmissionID2},
		},
		{
			name: "ZeroTotalPoints",
			setupResults: map[string]*pb.Result{
				testSubmissionID3: {
					SubmissionId: testSubmissionID3,
					Project:      testProject,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 0.0, Awarded: 0.0, Note: testRubricNote4},
					},
					IpAddress:   testRealIP,
					GeoLocation: localUnknown,
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testTotalSubmissions, testHighestScore},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())

			// Create a test server with mock storage
			mockStore := newMockStorage()
			for _, result := range tt.setupResults {
				err := mockStore.SaveResult(ctx, result)
				assert.NoError(t, err)
			}
			server := NewRubricServer(mockStore)

			// Create a test request
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, testSubmissionsPath, http.NoBody)
			rr := httptest.NewRecorder()

			// Call the handler
			serveProjectSubmissionsPage(rr, req, server, testProject)

			// Check status code
			assert.Equal(t, tt.expectedStatusCode, rr.Code)

			// Check content type
			assert.Equal(t, testHTMXContentType, rr.Header().Get("Content-Type"))

			// Check response body contains expected content
			body := rr.Body.String()
			for _, expected := range tt.expectedContent {
				assert.Contains(t, body, expected, testResponseShouldContain, expected)
			}

			// Verify it's valid HTML (contains basic HTML structure)
			assert.Contains(t, body, testDoctype)
		})
	}
}

func TestServeSubmissionsPageHTMX(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		totalSubmissions   int
		page               int
		pageSize           int
		expectedStatusCode int
		expectedContent    []string
		notExpectedContent []string
	}{
		{
			name:               "HTMXFirstPage",
			totalSubmissions:   25,
			page:               1,
			pageSize:           10,
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmission0, testSubmission17, testHTMXAttrGet, testHTMXAttrTarget},
			notExpectedContent: []string{testDoctype, "<head>", "<body>", testSubmission18, testSubmission5},
		},
		{
			name:               "HTMXSecondPage",
			totalSubmissions:   25,
			page:               2,
			pageSize:           10,
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmission18, testSubmission4, testHTMXAttrGet},
			notExpectedContent: []string{testDoctype, testSubmission0, testSubmission17, testSubmission5},
		},
		{
			name:               "HTMXLastPage",
			totalSubmissions:   25,
			page:               3,
			pageSize:           10,
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmission5, testSubmission9},
			notExpectedContent: []string{testDoctype, testSubmission0, testSubmission4},
		},
		{
			name:               "HTMXEmptyResults",
			totalSubmissions:   0,
			page:               1,
			pageSize:           10,
			expectedStatusCode: http.StatusOK,
			notExpectedContent: []string{testDoctype, "<head>", "<body>"},
		},
		{
			name:               "HTMXSingleResult",
			totalSubmissions:   1,
			page:               1,
			pageSize:           10,
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmission0},
			notExpectedContent: []string{testDoctype},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			// Create test results
			mockStore := newMockStorage()
			for i := 0; i < tt.totalSubmissions; i++ {
				submissionID := fmt.Sprintf("submission-%d", i)
				result := &pb.Result{
					SubmissionId: submissionID,
					Project:      testProject,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 100.0, Awarded: float64(50 + i%50), Note: "Test"},
					},
					IpAddress:   fmt.Sprintf("192.168.1.%d", i%256),
					GeoLocation: "Test City",
				}
				err := mockStore.SaveResult(ctx, result)
				assert.NoError(t, err)
			}
			server := NewRubricServer(mockStore)

			// Create a test request with HX-Request header
			url := fmt.Sprintf("/submissions?page=%d&pageSize=%d", tt.page, tt.pageSize)
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
			req.Header.Set("HX-Request", "true")
			rr := httptest.NewRecorder()

			// Call the handler
			serveProjectSubmissionsPage(rr, req, server, testProject)

			// Check status code
			assert.Equal(t, tt.expectedStatusCode, rr.Code)

			// Check response body contains expected content
			body := rr.Body.String()
			for _, expected := range tt.expectedContent {
				assert.Contains(t, body, expected, "HTMX response should contain: %s", expected)
			}

			// Check response body does NOT contain full page HTML
			for _, notExpected := range tt.notExpectedContent {
				assert.NotContains(t, body, notExpected, "HTMX response should NOT contain: %s", notExpected)
			}
		})
	}
}

func TestServeSubmissionsPageErrorCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		storageError       error
		expectedStatusCode int
		expectedContent    string
	}{
		{
			name:               "StorageListError",
			storageError:       errors.New(testStorageConnFailed),
			expectedStatusCode: http.StatusInternalServerError,
			expectedContent:    testInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())
			// Create a custom mock storage that returns an error for ListResultsPaginated
			mockStore := &errorMockStorage{
				listResultsPagErr: tt.storageError,
			}
			server := newMockRubricServer(mockStore)

			// Create a test request
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, testSubmissionsPath, http.NoBody)
			rr := httptest.NewRecorder()

			// Call the handler
			serveProjectSubmissionsPage(rr, req, server, testProject)

			// Check status code
			assert.Equal(t, tt.expectedStatusCode, rr.Code)

			// Check response body contains error message
			body := rr.Body.String()
			assert.Contains(t, body, tt.expectedContent)
		})
	}
}

// errorMockStorage is a mock storage that can be configured to return errors
type errorMockStorage struct {
	mockStorage
	listResultsPagErr error
	loadResultErr     error
}

func (m *errorMockStorage) ListResultsPaginated(ctx context.Context, params storage.ListResultsParams) (
	results map[string]*pb.Result, totalCount int, err error) {
	if m.listResultsPagErr != nil {
		return nil, 0, m.listResultsPagErr
	}
	return m.mockStorage.ListResultsPaginated(ctx, params)
}

func (m *errorMockStorage) LoadResult(ctx context.Context, submissionID string) (*pb.Result, error) {
	if m.loadResultErr != nil {
		return nil, m.loadResultErr
	}
	return m.mockStorage.LoadResult(ctx, submissionID)
}

// newMockRubricServer creates a RubricServer with mock storage and geo client for testing
func newMockRubricServer(stor storage.Storage) *RubricServer {
	server := NewRubricServer(stor)
	// Replace with mock geo client that never makes real HTTP calls
	server.geoClient = &GeoLocationClient{
		Client: &http.Client{
			Transport: &mockRoundTripper{},
		},
	}
	return server
}

func TestServeSubmissionsPageTimestampParseError(t *testing.T) {
	t.Parallel()
	// Test case where timestamp parsing fails
	mockStore := newMockStorage()
	result := &pb.Result{
		SubmissionId: "test-invalid-timestamp",
		Project:      testProject,
		Timestamp:    testInvalidTimestamp, // This will fail to parse
		Rubric: []*pb.RubricItem{
			{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
		},
		IpAddress:   testIP2,
		GeoLocation: testLocation,
	}
	ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
	err := mockStore.SaveResult(ctx, result)
	assert.NoError(t, err)

	server := newMockRubricServer(mockStore)

	// Create a test request
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, testSubmissionsPath, http.NoBody)
	rr := httptest.NewRecorder()

	// Call the handler
	serveProjectSubmissionsPage(rr, req, server, testProject)

	// Should still return OK status (handler uses fallback timestamp)
	assert.Equal(t, http.StatusOK, rr.Code)

	// Should contain the submission data
	body := rr.Body.String()
	assert.Contains(t, body, "test-invalid-timestamp")
}

func TestServeSubmissionDetailPage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		urlPath            string
		setupResults       map[string]*pb.Result
		expectedStatusCode int
		expectedContent    []string
	}{
		{
			name:               "InvalidSubmissionID_Empty",
			urlPath:            testSubmissionsPath + "/",
			setupResults:       map[string]*pb.Result{},
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    []string{},
		},
		{
			name:               "InvalidSubmissionID_Root",
			urlPath:            testSubmissionsPath,
			setupResults:       map[string]*pb.Result{},
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    []string{},
		},
		{
			name:               "SubmissionNotFound",
			urlPath:            testSubmissionsPath + "/nonexistent",
			setupResults:       map[string]*pb.Result{},
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    []string{testSubmissionNotFound},
		},
		{
			name:    "ValidSubmission",
			urlPath: testSubmissionsPath + "/" + testSubmissionID1,
			setupResults: map[string]*pb.Result{
				testSubmissionID1: {
					SubmissionId: testSubmissionID1,
					Project:      testProject,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote5},
						{Name: testItem2, Points: 5.0, Awarded: 4.0, Note: testRubricNote2},
					},
					IpAddress:   testIP2,
					GeoLocation: testLocation1,
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmissionID1, "80.0%", "15.0", "12.0", testItem1, testItem2, testIP2, testLocation1},
		},
		{
			name:    "ZeroTotalPoints",
			urlPath: testSubmissionsPath + "/" + testSubmissionID2,
			setupResults: map[string]*pb.Result{
				testSubmissionID2: {
					SubmissionId: testSubmissionID2,
					Project:      testProject,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 0.0, Awarded: 0.0, Note: testRubricNote4},
					},
					IpAddress:   testRealIP,
					GeoLocation: localUnknown,
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmissionID2, "0.0%", "0.0", "0.0", testItem1, testRubricNote4},
		},
		{
			name:    "EmptyRubric",
			urlPath: testSubmissionsPath + "/" + testSubmissionID3,
			setupResults: map[string]*pb.Result{
				testSubmissionID3: {
					SubmissionId: testSubmissionID3,
					Project:      testProject,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric:       []*pb.RubricItem{},
					IpAddress:    testIP5,
					GeoLocation:  testLocation,
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{testSubmissionID3, "0.0%", "0.0", "0.0", testIP5, testLocation},
		},
		{
			name:    "ProjectMismatch",
			urlPath: testSubmissionsPath + "/" + testSubmissionID1,
			setupResults: map[string]*pb.Result{
				testSubmissionID1: {
					SubmissionId: testSubmissionID1,
					Project:      "different-project",
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 10.0, Awarded: 8.0, Note: testRubricNote1},
					},
					IpAddress:   testIP2,
					GeoLocation: testLocation1,
				},
			},
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    []string{testSubmissionNotFound},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testServeSubmissionDetailPageCase(t, tt.urlPath, tt.setupResults, tt.expectedStatusCode, tt.expectedContent)
		})
	}
}

func testServeSubmissionDetailPageCase(t *testing.T, urlPath string, setupResults map[string]*pb.Result, expectedStatus int, expectedContent []string) {
	t.Helper()
	t.Parallel()
	ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())

	// Create a test server with mock storage
	mockStore := newMockStorage()
	for _, result := range setupResults {
		err := mockStore.SaveResult(ctx, result)
		assert.NoError(t, err)
	}
	server := NewRubricServer(mockStore)

	// Create a test request
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, urlPath, http.NoBody)
	rr := httptest.NewRecorder()

	// Extract submission ID from URL path (format: /submissions/ID or /$project/ID)
	pathParts := strings.Split(strings.TrimPrefix(urlPath, "/"), "/")
	var submissionID string
	if len(pathParts) >= 2 {
		submissionID = pathParts[len(pathParts)-1]
	} else {
		submissionID = testSubmissionID1 // Fallback
	}

	// Call the handler
	serveSubmissionDetailPage(rr, req, server, testProject, submissionID)

	// Check status code
	assert.Equal(t, expectedStatus, rr.Code)

	body := rr.Body.String()
	if expectedStatus == http.StatusOK {
		// Check content type
		assert.Equal(t, testHTMXContentType, rr.Header().Get("Content-Type"))

		// Check response body contains expected content
		for _, expected := range expectedContent {
			assert.Contains(t, body, expected, testResponseShouldContain, expected)
		}

		// Verify it's valid HTML
		assert.Contains(t, body, testDoctype)
	} else {
		// For error cases, check the error message
		for _, expected := range expectedContent {
			assert.Contains(t, body, expected, "Error response should contain: %s", expected)
		}
	}
}

// TestServeSubmissionsPagePagination verifies that pagination correctly shows all submissions across pages
func TestServeSubmissionsPagePagination(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		totalSubmissions   int
		pageSize           int
		requestPage        int
		expectedPageCount  int
		expectedItemCount  int
		expectedHasPrev    bool
		expectedHasNext    bool
		expectedContent    []string
		notExpectedContent []string
	}{
		{
			name:               "Page1Of3_15ItemsPerPage",
			totalSubmissions:   35,
			pageSize:           15,
			requestPage:        1,
			expectedPageCount:  3,
			expectedItemCount:  15,
			expectedHasPrev:    false,
			expectedHasNext:    true,
			expectedContent:    []string{"Page 1 of 3", "1 / 3", "Next", "Last", testPageItemDisabled, "« First"},
			notExpectedContent: []string{},
		},
		{
			name:               "Page2Of3_15ItemsPerPage",
			totalSubmissions:   35,
			pageSize:           15,
			requestPage:        2,
			expectedPageCount:  3,
			expectedItemCount:  15,
			expectedHasPrev:    true,
			expectedHasNext:    true,
			expectedContent:    []string{"Page 2 of 3", "2 / 3", "First", "Previous", "Next", "Last", "hx-get", "hx-target"},
			notExpectedContent: []string{},
		},
		{
			name:               "Page3Of3_15ItemsPerPage",
			totalSubmissions:   35,
			pageSize:           15,
			requestPage:        3,
			expectedPageCount:  3,
			expectedItemCount:  5,
			expectedHasPrev:    true,
			expectedHasNext:    false,
			expectedContent:    []string{"Page 3 of 3", "3 / 3", "First", "Previous", testPageItemDisabled, "Next ›", "Last »"},
			notExpectedContent: []string{},
		},
		{
			name:               "525SubmissionsWithPageSize15",
			totalSubmissions:   525,
			pageSize:           15,
			requestPage:        1,
			expectedPageCount:  35,
			expectedItemCount:  15,
			expectedHasPrev:    false,
			expectedHasNext:    true,
			expectedContent:    []string{"Page 1 of 35", "1 / 35", testTotalPaginationItems, "Next", "Last", "« First"},
			notExpectedContent: []string{},
		},
		{
			name:               "525SubmissionsMiddlePage",
			totalSubmissions:   525,
			pageSize:           15,
			requestPage:        18,
			expectedPageCount:  35,
			expectedItemCount:  15,
			expectedHasPrev:    true,
			expectedHasNext:    true,
			expectedContent:    []string{"Page 18 of 35", "18 / 35", testTotalPaginationItems, "First", "Previous", "Next", "Last"},
			notExpectedContent: []string{},
		},
		{
			name:               "525SubmissionsLastPage",
			totalSubmissions:   525,
			pageSize:           15,
			requestPage:        35,
			expectedPageCount:  35,
			expectedItemCount:  15,
			expectedHasPrev:    true,
			expectedHasNext:    false,
			expectedContent:    []string{"Page 35 of 35", "35 / 35", testTotalPaginationItems, "First", "Previous", testPageItemDisabled, "Next ›", "Last »"},
			notExpectedContent: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := contextlog.With(t.Context(), contextlog.DiscardLogger())

			// Create test results
			mockStore := newMockStorage()
			for i := 0; i < tt.totalSubmissions; i++ {
				submissionID := fmt.Sprintf("submission-%d", i)
				result := &pb.Result{
					SubmissionId: submissionID,
					Project:      testProject,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 100.0, Awarded: float64(50 + i%50), Note: "Test"},
					},
					IpAddress:   fmt.Sprintf("192.168.1.%d", i%256),
					GeoLocation: testLocation,
				}
				err := mockStore.SaveResult(ctx, result)
				assert.NoError(t, err)
			}
			server := NewRubricServer(mockStore)

			// Create request for specific page and page size
			url := fmt.Sprintf("/submissions?page=%d&pageSize=%d", tt.requestPage, tt.pageSize)
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
			rr := httptest.NewRecorder()

			// Call handler
			serveProjectSubmissionsPage(rr, req, server, testProject)

			// Verify status
			assert.Equal(t, http.StatusOK, rr.Code)

			body := rr.Body.String()

			// Verify page information is in response
			for _, expected := range tt.expectedContent {
				assert.Contains(t, body, expected, testResponseShouldContain, expected)
			} // Verify unwanted content is NOT in response
			for _, notExpected := range tt.notExpectedContent {
				assert.NotContains(t, body, notExpected, "Response should NOT contain: %s", notExpected)
			}

			// Count table rows: Extract tbody and count <tr> tags
			// Find tbody section
			tbodyStart := strings.Index(body, "<tbody>")
			tbodyEnd := strings.Index(body, "</tbody>")
			if tbodyStart > 0 && tbodyEnd > tbodyStart {
				tbody := body[tbodyStart:tbodyEnd]
				// Count <tr> tags in tbody (each submission row has one)
				bodyRows := strings.Count(tbody, "<tr>")
				assert.Equal(t, tt.expectedItemCount, bodyRows, "Page should have %d submissions", tt.expectedItemCount)
			} else {
				t.Fatal("Could not find tbody in response")
			}

			// Verify pagination controls exist
			assert.Contains(t, body, "<nav aria-label=\"Page navigation\"", "Should have pagination nav")
			assert.Contains(t, body, "<ul class=\"pagination", "Should have pagination list")

			// Verify HTMX attributes on pagination links
			assert.Contains(t, body, "hx-get=", "Pagination should use HTMX")
			assert.Contains(t, body, "hx-target=\"#page-container\"", "Should target page-container")
			assert.Contains(t, body, "hx-push-url=\"true\"", "Should update URL")

			// Verify total count
			assert.Contains(t, body, fmt.Sprintf("%d total", tt.totalSubmissions), "Should show total count")
		})
	}
}

func TestServeIndexPage(t *testing.T) {
	tests := []struct {
		name             string
		setupProjects    []string
		expectedProjects []string
		expectError      bool
	}{
		{
			name:             "empty project list",
			setupProjects:    []string{},
			expectedProjects: []string{},
			expectError:      false,
		},
		{
			name:             "single project",
			setupProjects:    []string{"project-a"},
			expectedProjects: []string{"project-a"},
			expectError:      false,
		},
		{
			name:             "multiple projects",
			setupProjects:    []string{"project-a", "project-b", "project-c"},
			expectedProjects: []string{"project-a", "project-b", "project-c"},
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock storage with results for the projects
			mockStore := newMockStorage()
			for _, proj := range tt.setupProjects {
				result := &pb.Result{
					SubmissionId: proj + "-001",
					Project:      proj,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: "Test", Points: 10, Awarded: 5},
					},
				}
				_ = mockStore.SaveResult(context.Background(), result)
			}

			// Create rubric server
			rubricServer := createTestRubricServer(t, mockStore)

			// Create test request
			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			w := httptest.NewRecorder()

			// Call function
			serveIndexPage(w, req, rubricServer)

			// Check response
			resp := w.Result()
			defer resp.Body.Close()

			if tt.expectError {
				assert.NotEqual(t, http.StatusOK, resp.StatusCode)
			} else {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				body, _ := io.ReadAll(resp.Body)
				bodyStr := string(body)

				// Verify page structure
				assert.Contains(t, bodyStr, "<title>GradeBot - Classes</title>")
				assert.Contains(t, bodyStr, "📚 Classes")

				// Verify projects are listed
				for _, proj := range tt.expectedProjects {
					assert.Contains(t, bodyStr, proj)
				}

				// Verify empty state if no projects
				if len(tt.expectedProjects) == 0 {
					assert.Contains(t, bodyStr, "No Classes Yet")
				}
			}
		})
	}
}

func TestHandleProjectRoutes(t *testing.T) {
	tests := []struct {
		name               string
		path               string
		setupResults       map[string]*pb.Result
		expectedStatusCode int
		expectedContent    []string
	}{
		{
			name: "project submissions page",
			path: "/test-project",
			setupResults: map[string]*pb.Result{
				testSubmissionID1: {
					SubmissionId: testSubmissionID1,
					Project:      testProject,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 10, Awarded: 8, Note: testRubricNote1},
					},
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{"test-project", "Submissions", testSubmissionID1},
		},
		{
			name: "submission detail page",
			path: "/test-project/test-123",
			setupResults: map[string]*pb.Result{
				testSubmissionID1: {
					SubmissionId: testSubmissionID1,
					Project:      testProject,
					Timestamp:    time.Now().Format(time.RFC3339),
					Rubric: []*pb.RubricItem{
						{Name: testItem1, Points: 10, Awarded: 8, Note: testRubricNote1},
					},
				},
			},
			expectedStatusCode: http.StatusOK,
			expectedContent:    []string{"test-123", testItem1, testRubricNote1},
		},
		{
			name:               "too many path segments",
			path:               "/project/submission/extra/segment",
			setupResults:       map[string]*pb.Result{},
			expectedStatusCode: http.StatusNotFound,
			expectedContent:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock storage with test results
			mockStore := newMockStorage()
			maps.Copy(mockStore.results, tt.setupResults)

			// Create rubric server
			rubricServer := createTestRubricServer(t, mockStore)

			// Create test request
			req := httptest.NewRequest(http.MethodGet, tt.path, http.NoBody)
			w := httptest.NewRecorder()

			// Call function
			handleProjectRoutes(w, req, rubricServer)

			// Check response
			resp := w.Result()
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatusCode, resp.StatusCode)

			if tt.expectedStatusCode == http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				bodyStr := string(body)

				for _, content := range tt.expectedContent {
					assert.Contains(t, bodyStr, content)
				}
			}
		})
	}
}

func TestServeIndexPageErrorCases(t *testing.T) {
	tests := []struct {
		name               string
		mockError          error
		expectedStatusCode int
		expectedContent    string
	}{
		{
			name:               "storage error",
			mockError:          fmt.Errorf("database connection failed"),
			expectedStatusCode: http.StatusInternalServerError,
			expectedContent:    "Internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock storage that returns error
			mockStore := &mockStorageWithError{
				listProjectsError: tt.mockError,
			}

			rubricServer := &RubricServer{
				storage:   mockStore,
				templates: NewTemplateManager(),
			}

			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			w := httptest.NewRecorder()

			serveIndexPage(w, req, rubricServer)

			resp := w.Result()
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatusCode, resp.StatusCode)
			body, _ := io.ReadAll(resp.Body)
			assert.Contains(t, string(body), tt.expectedContent)
		})
	}
}

func TestExtractFromPeerErrorCase(t *testing.T) {
	tests := []struct {
		name     string
		peerAddr string
		expected string
	}{
		{
			name:     "invalid address format",
			peerAddr: "invalid-address-no-port",
			expected: "invalid-address-no-port",
		},
		{
			name:     "empty address",
			peerAddr: "",
			expected: unknownIP,
		},
		{
			name:     "valid address with port",
			peerAddr: "192.168.1.1:8080",
			expected: "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockReq := &mockConnectRequest{
				Request:  &connect.Request[pb.UploadRubricResultRequest]{},
				peerAddr: tt.peerAddr,
			}

			result := extractFromPeer(mockReq)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHandleProjectRoutesEmptyPath(t *testing.T) {
	mockStore := newMockStorage()
	rubricServer := createTestRubricServer(t, mockStore)

	tests := []struct {
		name               string
		path               string
		expectedStatusCode int
	}{
		{
			name:               "empty path after trim",
			path:               "/",
			expectedStatusCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, http.NoBody)
			w := httptest.NewRecorder()

			// Manually construct the scenario where parts[0] is empty
			// This happens when path is just "/"
			path := strings.TrimPrefix(tt.path, "/")
			parts := strings.Split(path, "/")

			// Verify this creates the empty parts scenario
			assert.True(t, len(parts) == 0 || parts[0] == "")

			handleProjectRoutes(w, req, rubricServer)

			resp := w.Result()
			defer resp.Body.Close()
			assert.Equal(t, tt.expectedStatusCode, resp.StatusCode)
		})
	}
}

// mockStorageWithError is a mock storage that can return errors
type mockStorageWithError struct {
	listProjectsError error
	loadResultError   error
	listResultsError  error
}

func (m *mockStorageWithError) SaveResult(ctx context.Context, result *pb.Result) error {
	return nil
}

func (m *mockStorageWithError) LoadResult(ctx context.Context, submissionID string) (*pb.Result, error) {
	if m.loadResultError != nil {
		return nil, m.loadResultError
	}
	return nil, fmt.Errorf("result not found")
}

func (m *mockStorageWithError) ListResultsPaginated(ctx context.Context, params storage.ListResultsParams) (
	results map[string]*pb.Result, totalCount int, err error) {
	if m.listResultsError != nil {
		return nil, 0, m.listResultsError
	}
	return make(map[string]*pb.Result), 0, nil
}

func (m *mockStorageWithError) ListProjects(ctx context.Context) ([]string, error) {
	if m.listProjectsError != nil {
		return nil, m.listProjectsError
	}
	return []string{}, nil
}

func (m *mockStorageWithError) Close() error {
	return nil
}

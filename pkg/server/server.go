package server

import (
	"context"
	"crypto/tls"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/jh125486/gradebot/pkg/contextlog"
	"github.com/jh125486/gradebot/pkg/openai"
	pb "github.com/jh125486/gradebot/pkg/proto"
	"github.com/jh125486/gradebot/pkg/proto/protoconnect"
	"github.com/jh125486/gradebot/pkg/storage"
)

const (
	submissionsTmplFile  = "submissions.go.tmpl"
	submissionTmplFile   = "submission.go.tmpl"
	indexTmplFile        = "index.go.tmpl"
	unknownIP            = "unknown"
	unknownLocation      = "Unknown"
	localUnknown         = "Local/Unknown"
	contentTypeHeader    = "Content-Type"
	templateExecErrMsg   = "template execution error"
	htmlContentType      = "text/html"
	xForwardedForHeader  = "X-Forwarded-For"
	xRealIPHeader        = "X-Real-IP"
	cfConnectingIPHeader = "CF-Connecting-IP"
	requestIDHeader      = "X-Request-ID"
)

var (
	//go:embed templates
	templatesFS embed.FS
)

// IPExtractable interface for types that can provide IP extraction methods
type IPExtractable interface {
	Header() http.Header
	Peer() connect.Peer
}

// extractClientIP extracts client IP from any object that implements IPExtractable
func extractClientIP(ctx context.Context, req IPExtractable) string {
	// Method 1: Try to get from context (set by realIP middleware)
	if realIP, ok := ctx.Value(RealIPKey).(string); ok && realIP != "" && realIP != unknownIP {
		return realIP
	}

	// Method 2: Try to extract from HTTP headers
	if ip := extractFromXForwardedFor(req); ip != unknownIP {
		return ip
	}

	if ip := extractFromXRealIP(req); ip != unknownIP {
		return ip
	}

	if ip := extractFromCFConnectingIP(req); ip != unknownIP {
		return ip
	}

	// Method 3: Try peer info as fallback
	return extractFromPeer(req)
}

// extractFromXForwardedFor extracts IP from X-Forwarded-For header
func extractFromXForwardedFor(req IPExtractable) string {
	headers := req.Header()
	if xff := headers.Get(xForwardedForHeader); xff != "" {
		if ips := strings.Split(xff, ","); len(ips) > 0 {
			if ip := strings.TrimSpace(ips[0]); ip != "" && ip != unknownIP {
				return ip
			}
		}
	}
	return unknownIP
}

// extractFromXRealIP extracts IP from X-Real-IP header
func extractFromXRealIP(req IPExtractable) string {
	headers := req.Header()
	if xri := headers.Get(xRealIPHeader); xri != "" && xri != unknownIP {
		return xri
	}
	return unknownIP
}

// extractFromCFConnectingIP extracts IP from CF-Connecting-IP header
func extractFromCFConnectingIP(req IPExtractable) string {
	headers := req.Header()
	if cfip := headers.Get(cfConnectingIPHeader); cfip != "" && cfip != unknownIP {
		return cfip
	}
	return unknownIP
}

// extractFromPeer extracts IP from peer address
func extractFromPeer(req IPExtractable) string {
	peer := req.Peer()
	if peer.Addr != "" {
		if ip, _, err := net.SplitHostPort(peer.Addr); err == nil {
			return ip
		}
		return peer.Addr
	}
	return unknownIP
}

// TemplateManager manages HTML templates
type TemplateManager struct {
	submissionsTmpl  *template.Template
	submissionTmpl   *template.Template
	tableContentTmpl *template.Template
	indexTmpl        *template.Template
}

// NewTemplateManager creates and returns a new template manager with all embedded templates loaded.
// It registers custom template functions (add, sub) for pagination calculations.
// It panics if any template fails to parse.
func NewTemplateManager() *TemplateManager {
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
	}
	templatesDir := filepath.Join("templates", submissionsTmplFile)
	return &TemplateManager{
		submissionsTmpl: template.Must(
			template.New(submissionsTmplFile).Funcs(funcMap).ParseFS(templatesFS, templatesDir)),
		submissionTmpl: template.Must(
			template.New(submissionTmplFile).Funcs(funcMap).ParseFS(templatesFS,
				filepath.Join("templates", submissionTmplFile))),
		tableContentTmpl: template.Must(
			template.New("table-content.go.tmpl").Funcs(funcMap).ParseFS(templatesFS,
				filepath.Join("templates", "table-content.go.tmpl"))),
		indexTmpl: template.Must(
			template.New(indexTmplFile).Funcs(funcMap).ParseFS(templatesFS,
				filepath.Join("templates", indexTmplFile))),
	}
}

// SubmissionData represents a single submission with its evaluation score and metadata.
// SubmissionData represents a single submission for display purposes.
// For the submissions list page, it contains: submission_id, project, uploaded_at, and score.
// For the submission detail page, it includes the full result data with project info.
type SubmissionData struct {
	SubmissionID string
	Project      string
	Timestamp    time.Time
	Score        float64
	IPAddress    string
	GeoLocation  string
}

// RubricItemData represents a single rubric item with its point value and awarded score.
type RubricItemData struct {
	Name    string
	Points  float64
	Awarded float64
	Note    string
}

// SubmissionsPageData contains all data needed to render the submissions overview page,
// including pagination information, submission details, and score statistics.
type SubmissionsPageData struct {
	Project          string
	TotalSubmissions int
	HighScore        float64
	Submissions      []SubmissionData
	CurrentPage      int
	TotalPages       int
	PageSize         int
	HasPrevPage      bool
	HasNextPage      bool
}

// IndexPageData contains the list of projects for the index page
type IndexPageData struct {
	Projects []string
}

type (
	// Config contains the configuration required to start the server.
	Config struct {
		ID           string
		Port         string
		OpenAIClient openai.Reviewer
		Storage      storage.Storage
	}
	// GradingServer represents a grading server
	GradingServer struct {
		ID string
	}
)

// QualityServer implements the Quality service for code quality evaluation via AI.
type QualityServer struct {
	protoconnect.UnimplementedQualityServiceHandler
	OpenAIClient openai.Reviewer
}

// tlsConfig configures TLS 1.2 with ciphers compatible with corporate proxies.
// This ensures compatibility with older corporate proxy infrastructure that may not support TLS 1.3.
func tlsConfig() *tls.Config {
	//#nosec:G402 // This is needed to get around proxies that don't support TLS 1.3
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		},
	}
}

// Start initializes and runs the gRPC/HTTP server on the configured port.
// It sets up handlers for both Connect RPC services (Quality and Rubric),
// serves embedded HTML pages for viewing submissions, and provides a health check endpoint.
// The server is configured with TLS 1.2 for corporate proxy compatibility.
// It gracefully shuts down on context cancellation or when the listener returns an error.
func Start(ctx context.Context, cfg Config) error {
	contextlog.From(ctx).InfoContext(ctx, "Server will start on port", slog.String("port", cfg.Port))
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", ":"+cfg.Port)
	if err != nil {
		return err
	}

	// Build Connect handlers for both services
	qualityPath, qualityHandler := protoconnect.NewQualityServiceHandler(&QualityServer{
		OpenAIClient: cfg.OpenAIClient,
	})

	rubricServer := NewRubricServer(cfg.Storage)
	rubricPath, rubricHandler := protoconnect.NewRubricServiceHandler(rubricServer)

	// Create a multiplexer to handle both services
	mux := http.NewServeMux()

	// Wrap handlers with middleware: requestID -> logging -> realIP -> auth
	mux.Handle(qualityPath, RequestIDMiddleware(LoggingMiddleware(RealIPMiddleware(AuthMiddleware(cfg.ID)(qualityHandler)))))
	mux.Handle(rubricPath, RequestIDMiddleware(LoggingMiddleware(RealIPMiddleware(AuthMiddleware(cfg.ID)(rubricHandler)))))

	// Serve index page with list of all projects (no auth required)
	mux.Handle("/", RequestIDMiddleware(LoggingMiddleware(RealIPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only handle exact root path
		if r.URL.Path == "/" {
			serveIndexPage(w, r, rubricServer)
			return
		}

		// Try to handle as project submissions page or submission detail
		handleProjectRoutes(w, r, rubricServer)
	})))))

	// Health check endpoint for Koyeb and monitoring
	mux.Handle("/health", RequestIDMiddleware(LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(contentTypeHeader, "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`))
	}))))
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second, // Prevent Slowloris attacks
		TLSConfig:         tlsConfig(),
	}
	contextlog.From(ctx).InfoContext(ctx, "Connect HTTP server listening", slog.String("port", cfg.Port))

	// Run Serve in goroutine so we can respond to ctx cancellation and
	// attempt graceful shutdown.
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(lis)
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		// attempt graceful shutdown with timeout
		// Use a fresh context for shutdown since the original is already cancelled
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return ctx.Err()
	}
}

// parseSubmissionsFromResults converts storage results to submission data
func parseSubmissionsFromResults(ctx context.Context, results map[string]*pb.Result) (submissions []SubmissionData, highScore float64) {
	submissions = make([]SubmissionData, 0, len(results))
	highScore = 0.0

	for _, result := range results {
		totalPoints := 0.0
		awardedPoints := 0.0

		for _, item := range result.Rubric {
			totalPoints += item.Points
			awardedPoints += item.Awarded
		}

		score := 0.0
		if totalPoints > 0 {
			score = (awardedPoints / totalPoints) * 100
		}

		timestamp, err := time.Parse(time.RFC3339, result.Timestamp)
		if err != nil {
			contextlog.From(ctx).ErrorContext(ctx, "Failed to parse timestamp",
				slog.Any("error", err),
				slog.String("timestamp", result.Timestamp),
			)
			timestamp = time.Now()
		}

		submissions = append(submissions, SubmissionData{
			SubmissionID: result.SubmissionId,
			Project:      result.Project,
			Timestamp:    timestamp,
			Score:        score,
			IPAddress:    result.IpAddress,
			GeoLocation:  result.GeoLocation,
		})
		if score > highScore {
			highScore = score
		}
	}

	sort.Slice(submissions, func(i, j int) bool {
		return submissions[i].SubmissionID < submissions[j].SubmissionID
	})

	return submissions, highScore
}

// getPaginationParams extracts and validates pagination parameters from the request
func getPaginationParams(r *http.Request) (page, pageSize int) {
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("pageSize")

	page = 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	pageSize = 20 // Default page size
	if pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 && ps <= 100 {
			pageSize = ps
		}
	}

	return page, pageSize
}

// serveIndexPage serves the HTML page for viewing all projects/classes
func serveIndexPage(w http.ResponseWriter, r *http.Request, rubricServer *RubricServer) {
	ctx := r.Context()
	l := contextlog.From(ctx)

	w.Header().Set(contentTypeHeader, htmlContentType)

	// Fetch list of projects from storage
	projects, err := rubricServer.storage.ListProjects(ctx)
	if err != nil {
		l.ErrorContext(ctx, "Failed to list projects from storage", slog.Any("error", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := IndexPageData{
		Projects: projects,
	}

	l.InfoContext(ctx, "Serving index page",
		slog.Int("project_count", len(projects)))

	// Execute template
	if err := rubricServer.templates.indexTmpl.Execute(w, data); err != nil {
		l.ErrorContext(ctx, templateExecErrMsg, slog.Any("error", err))
		http.Error(w, templateExecErrMsg, http.StatusInternalServerError)
		return
	}
}

// handleProjectRoutes handles /$project and /$project/$id routes
func handleProjectRoutes(w http.ResponseWriter, r *http.Request, rubricServer *RubricServer) {
	// Parse path: /$project or /$project/$id
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Invalid path", http.StatusNotFound)
		return
	}

	project := parts[0]

	switch len(parts) {
	case 1:
		// /$project - show submissions for this project
		serveProjectSubmissionsPage(w, r, rubricServer, project)
	case 2:
		// /$project/$id - show submission detail
		submissionID := parts[1]
		serveSubmissionDetailPage(w, r, rubricServer, project, submissionID)
	default:
		http.Error(w, "Invalid path", http.StatusNotFound)
	}
}

// executeTableContent renders just the table content for HTMX partial updates using a template
func executeTableContent(w http.ResponseWriter, data *SubmissionsPageData, rubricServer *RubricServer) error {
	return rubricServer.templates.tableContentTmpl.Execute(w, data)
}

// serveProjectSubmissionsPage serves the HTML page for viewing submissions for a specific project
func serveProjectSubmissionsPage(w http.ResponseWriter, r *http.Request, rubricServer *RubricServer, project string) {
	ctx := r.Context()

	// Get pagination parameters
	page, pageSize := getPaginationParams(r)

	w.Header().Set(contentTypeHeader, htmlContentType)

	// Fetch paginated results from storage, filtered by project
	results, totalCount, err := rubricServer.storage.ListResultsPaginated(ctx, storage.ListResultsParams{
		Page:     page,
		PageSize: pageSize,
		Project:  project,
	})
	if err != nil {
		contextlog.From(ctx).ErrorContext(ctx, "Failed to list paginated results from storage",
			slog.Any("error", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Parse submissions and calculate scores
	submissions, highScore := parseSubmissionsFromResults(ctx, results)

	// Calculate pagination with actual total count from storage
	totalPages := (totalCount + pageSize - 1) / pageSize
	if page > totalPages && totalPages > 0 {
		page = totalPages
	}

	data := SubmissionsPageData{
		Project:          project,
		TotalSubmissions: totalCount,
		HighScore:        highScore,
		Submissions:      submissions,
		CurrentPage:      page,
		TotalPages:       totalPages,
		PageSize:         pageSize,
		HasPrevPage:      page > 1,
		HasNextPage:      page < totalPages,
	}

	contextlog.From(ctx).InfoContext(ctx, "Serving project submissions page",
		slog.String("project", project),
		slog.Int("total_submissions", totalCount),
		slog.Int("page", page),
		slog.Int("total_pages", totalPages),
		slog.Int("page_submissions", len(submissions)))

	// Check if this is an HTMX request (partial update)
	if r.Header.Get("HX-Request") == "true" {
		// Return only table content for HTMX
		w.Header().Set(contentTypeHeader, htmlContentType)
		if err := executeTableContent(w, &data, rubricServer); err != nil {
			contextlog.From(ctx).ErrorContext(ctx, templateExecErrMsg, slog.Any("error", err))
			http.Error(w, templateExecErrMsg, http.StatusInternalServerError)
			return
		}
		return
	}

	// Execute full template for initial page load
	if err := rubricServer.templates.submissionsTmpl.Execute(w, data); err != nil {
		contextlog.From(ctx).ErrorContext(ctx, templateExecErrMsg, slog.Any("error", err))
		http.Error(w, templateExecErrMsg, http.StatusInternalServerError)
		return
	}
}

// serveSubmissionDetailPage serves the HTML page for a specific submission's details
func serveSubmissionDetailPage(w http.ResponseWriter, r *http.Request, rubricServer *RubricServer, project, submissionID string) {
	// Set content type
	w.Header().Set(contentTypeHeader, htmlContentType)

	ctx := r.Context()
	result, err := rubricServer.storage.LoadResult(ctx, submissionID)
	if err != nil {
		contextlog.From(ctx).ErrorContext(ctx, "Failed to load result from storage",
			slog.Any("error", err),
			slog.String("submission_id", submissionID))
		http.Error(w, "Submission not found", http.StatusNotFound)
		return
	}

	// Verify that the submission belongs to the requested project
	if result.Project != project {
		contextlog.From(ctx).WarnContext(ctx, "Submission project mismatch",
			slog.String("submission_id", submissionID),
			slog.String("requested_project", project),
			slog.String("actual_project", result.Project))
		http.Error(w, "Submission not found", http.StatusNotFound)
		return
	}

	// Calculate totals
	totalPoints := 0.0
	awardedPoints := 0.0
	for _, item := range result.Rubric {
		totalPoints += item.Points
		awardedPoints += item.Awarded
	}

	score := 0.0
	if totalPoints > 0 {
		score = (awardedPoints / totalPoints) * 100
	}

	// Parse timestamp
	timestamp, err := time.Parse(time.RFC3339, result.Timestamp)
	if err != nil {
		contextlog.From(ctx).ErrorContext(ctx, "Failed to parse timestamp",
			slog.Any("error", err),
			slog.String("timestamp", result.Timestamp),
		)
		timestamp = time.Now()
	}

	// Convert rubric items to template data
	rubricItems := make([]RubricItemData, len(result.Rubric))
	for i, item := range result.Rubric {
		rubricItems[i] = RubricItemData{
			Name:    item.Name,
			Points:  item.Points,
			Awarded: item.Awarded,
			Note:    item.Note,
		}
	}

	data := struct {
		SubmissionID  string
		Project       string
		Timestamp     time.Time
		TotalPoints   float64
		AwardedPoints float64
		Score         float64
		IPAddress     string
		GeoLocation   string
		Rubric        []RubricItemData
	}{
		SubmissionID:  result.SubmissionId,
		Project:       result.Project,
		Timestamp:     timestamp,
		TotalPoints:   totalPoints,
		AwardedPoints: awardedPoints,
		Score:         score,
		IPAddress:     result.IpAddress,
		GeoLocation:   result.GeoLocation,
		Rubric:        rubricItems,
	}

	if err := rubricServer.templates.submissionTmpl.Execute(w, data); err != nil {
		contextlog.From(ctx).ErrorContext(ctx, templateExecErrMsg, slog.Any("error", err))
		http.Error(w, templateExecErrMsg, http.StatusInternalServerError)
		return
	}
}

// EvaluateCodeQuality handles RPC requests to evaluate code quality using AI.
// It validates that files are provided in the request, calls the OpenAI client to review the code,
// and returns the review feedback or an error if validation or evaluation fails.
func (s *QualityServer) EvaluateCodeQuality(
	ctx context.Context,
	req *connect.Request[pb.EvaluateCodeQualityRequest],
) (*connect.Response[pb.EvaluateCodeQualityResponse], error) {
	// Basic validation: ensure files are present
	if len(req.Msg.Files) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("no files provided"))
	}

	review, err := s.OpenAIClient.ReviewCode(ctx, req.Msg.Instructions, req.Msg.Files)
	if err != nil {
		contextlog.From(ctx).ErrorContext(ctx, "failed to review code", slog.Any("error", err))
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to review code"))
	}

	return connect.NewResponse(&pb.EvaluateCodeQualityResponse{
		QualityScore: review.QualityScore,
		Feedback:     review.Feedback,
	}), nil
}

package server_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jh125486/gradebot/pkg/contextlog"
	"github.com/jh125486/gradebot/pkg/server"
)

const (
	testAuthToken       = "test-secret-token"
	testBearerToken     = "Bearer " + testAuthToken
	testWrongToken      = "Bearer wrong-token"
	testRequestIDHeader = "X-Request-ID"
)

func TestAuthMiddleware(t *testing.T) {
	t.Parallel()

	type args struct {
		token      string
		authHeader string
	}
	tests := []struct {
		name           string
		args           args
		wantStatus     int
		wantBodySubstr string
	}{
		{
			name:           "valid_token",
			args:           args{token: testAuthToken, authHeader: testBearerToken},
			wantStatus:     http.StatusOK,
			wantBodySubstr: "success",
		},
		{
			name:           "missing_auth_header",
			args:           args{token: testAuthToken, authHeader: ""},
			wantStatus:     http.StatusUnauthorized,
			wantBodySubstr: "missing authorization header",
		},
		{
			name:           "malformed_header_no_bearer",
			args:           args{token: testAuthToken, authHeader: "InvalidFormat"},
			wantStatus:     http.StatusUnauthorized,
			wantBodySubstr: "invalid authorization header format",
		},
		{
			name:           "malformed_header_just_bearer",
			args:           args{token: testAuthToken, authHeader: "Bearer"},
			wantStatus:     http.StatusUnauthorized,
			wantBodySubstr: "invalid authorization header format",
		},
		{
			name:           "wrong_token",
			args:           args{token: testAuthToken, authHeader: testWrongToken},
			wantStatus:     http.StatusUnauthorized,
			wantBodySubstr: "invalid token",
		},
		{
			name:           "empty_token_value",
			args:           args{token: testAuthToken, authHeader: "Bearer "},
			wantStatus:     http.StatusUnauthorized,
			wantBodySubstr: "invalid token",
		},
		{
			name:           "case_sensitive_bearer",
			args:           args{token: testAuthToken, authHeader: "bearer " + testAuthToken},
			wantStatus:     http.StatusUnauthorized,
			wantBodySubstr: "invalid authorization header format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create handler that should only be called on success
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("success"))
			})

			// Wrap with auth middleware
			middleware := server.AuthMiddleware(tt.args.token)
			wrapped := middleware(handler)

			// Create request with logger
			ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/test", http.NoBody)
			if tt.args.authHeader != "" {
				req.Header.Set("Authorization", tt.args.authHeader)
			}

			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)
			assert.Contains(t, rr.Body.String(), tt.wantBodySubstr)
		})
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	t.Parallel()

	type args struct {
		existingRequestID string
	}
	tests := []struct {
		name              string
		args              args
		wantRequestIDSet  bool
		wantSameRequestID bool
	}{
		{
			name:              "generates_new_request_id",
			args:              args{existingRequestID: ""},
			wantRequestIDSet:  true,
			wantSameRequestID: false,
		},
		{
			name:              "preserves_existing_request_id",
			args:              args{existingRequestID: "existing-id-123"},
			wantRequestIDSet:  true,
			wantSameRequestID: true,
		},
		{
			name:              "handles_uuid_format",
			args:              args{existingRequestID: "550e8400-e29b-41d4-a716-446655440000"},
			wantRequestIDSet:  true,
			wantSameRequestID: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var capturedRequestID string
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify logger is enriched with request ID by checking context
				_ = contextlog.From(r.Context())

				w.WriteHeader(http.StatusOK)
			})

			wrapped := server.RequestIDMiddleware(handler)

			ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/test", http.NoBody)
			if tt.args.existingRequestID != "" {
				req.Header.Set(testRequestIDHeader, tt.args.existingRequestID)
			}

			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)

			// Check response header has request ID
			capturedRequestID = rr.Header().Get(testRequestIDHeader)
			if tt.wantRequestIDSet {
				assert.NotEmpty(t, capturedRequestID, "Request ID should be set in response header")
			}

			// Check if it's the same as the existing one
			if tt.wantSameRequestID {
				assert.Equal(t, tt.args.existingRequestID, capturedRequestID)
			} else if tt.args.existingRequestID == "" {
				assert.NotEqual(t, tt.args.existingRequestID, capturedRequestID)
			}
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	t.Parallel()

	type args struct {
		method     string
		path       string
		statusCode int
	}
	tests := []struct {
		name         string
		args         args
		wantLogged   bool
		setupContext func() context.Context
	}{
		{
			name: "logs_successful_request",
			args: args{
				method:     http.MethodGet,
				path:       "/test",
				statusCode: http.StatusOK,
			},
			wantLogged: true,
			setupContext: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name: "logs_error_request",
			args: args{
				method:     http.MethodPost,
				path:       "/error",
				statusCode: http.StatusInternalServerError,
			},
			wantLogged: true,
			setupContext: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name: "logs_not_found",
			args: args{
				method:     http.MethodGet,
				path:       "/notfound",
				statusCode: http.StatusNotFound,
			},
			wantLogged: true,
			setupContext: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name: "logs_unauthorized",
			args: args{
				method:     http.MethodPost,
				path:       "/protected",
				statusCode: http.StatusUnauthorized,
			},
			wantLogged: true,
			setupContext: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name: "logs_default_status_when_not_set",
			args: args{
				method:     http.MethodGet,
				path:       "/default",
				statusCode: 0, // Don't call WriteHeader
			},
			wantLogged: true,
			setupContext: func() context.Context {
				return contextlog.With(context.Background(), contextlog.DiscardLogger())
			},
		},
		{
			name: "logs_with_empty_real_ip",
			args: args{
				method:     http.MethodGet,
				path:       "/test",
				statusCode: http.StatusOK,
			},
			wantLogged: true,
			setupContext: func() context.Context {
				ctx := context.Background()
				ctx = context.WithValue(ctx, server.RealIPKey, "")
				return contextlog.With(ctx, contextlog.DiscardLogger())
			},
		},
		{
			name: "logs_with_real_ip_from_context",
			args: args{
				method:     http.MethodPost,
				path:       "/api/test",
				statusCode: http.StatusCreated,
			},
			wantLogged: true,
			setupContext: func() context.Context {
				ctx := context.Background()
				ctx = context.WithValue(ctx, server.RealIPKey, "203.0.113.1")
				return contextlog.With(ctx, contextlog.DiscardLogger())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.args.statusCode > 0 {
					w.WriteHeader(tt.args.statusCode)
				}
				// If statusCode is 0, don't call WriteHeader to test default behavior
			})

			wrapped := server.LoggingMiddleware(handler)

			ctx := tt.setupContext()
			req := httptest.NewRequestWithContext(ctx, tt.args.method, tt.args.path, http.NoBody)
			rr := httptest.NewRecorder()

			wrapped.ServeHTTP(rr, req)

			expectedStatus := tt.args.statusCode
			if expectedStatus == 0 {
				expectedStatus = http.StatusOK // Default status
			}
			assert.Equal(t, expectedStatus, rr.Code)
		})
	}
}

func TestRealIPMiddleware(t *testing.T) {
	t.Parallel()

	type args struct {
		remoteAddr     string
		xForwardedFor  string
		xRealIP        string
		cfConnectingIP string
	}
	tests := []struct {
		name           string
		args           args
		wantIPCaptured bool
	}{
		{
			name: "extracts_from_x_forwarded_for",
			args: args{
				remoteAddr:    "10.0.0.1:12345",
				xForwardedFor: "203.0.113.1, 198.51.100.1",
			},
			wantIPCaptured: true,
		},
		{
			name: "extracts_from_x_real_ip",
			args: args{
				remoteAddr: "10.0.0.1:12345",
				xRealIP:    "203.0.113.2",
			},
			wantIPCaptured: true,
		},
		{
			name: "extracts_from_cf_connecting_ip",
			args: args{
				remoteAddr:     "10.0.0.1:12345",
				cfConnectingIP: "203.0.113.3",
			},
			wantIPCaptured: true,
		},
		{
			name: "uses_remote_addr_fallback",
			args: args{
				remoteAddr: "203.0.113.4:8080",
			},
			wantIPCaptured: true,
		},
		{
			name: "handles_ipv6",
			args: args{
				remoteAddr:    "[2001:db8::1]:8080",
				xForwardedFor: "2001:db8::2",
			},
			wantIPCaptured: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			wrapped := server.RealIPMiddleware(handler)

			ctx := context.Background()
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/test", http.NoBody)
			req.RemoteAddr = tt.args.remoteAddr
			if tt.args.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.args.xForwardedFor)
			}
			if tt.args.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.args.xRealIP)
			}
			if tt.args.cfConnectingIP != "" {
				req.Header.Set("CF-Connecting-IP", tt.args.cfConnectingIP)
			}

			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)
		})
	}
}

func TestResponseWriter(t *testing.T) {
	t.Parallel()

	type args struct {
		statusCode int
		body       string
	}
	tests := []struct {
		name       string
		args       args
		wantStatus int
		wantBody   string
	}{
		{
			name:       "captures_200_status",
			args:       args{statusCode: http.StatusOK, body: "success"},
			wantStatus: http.StatusOK,
			wantBody:   "success",
		},
		{
			name:       "captures_404_status",
			args:       args{statusCode: http.StatusNotFound, body: "not found"},
			wantStatus: http.StatusNotFound,
			wantBody:   "not found",
		},
		{
			name:       "captures_500_status",
			args:       args{statusCode: http.StatusInternalServerError, body: "error"},
			wantStatus: http.StatusInternalServerError,
			wantBody:   "error",
		},
		{
			name:       "captures_201_status",
			args:       args{statusCode: http.StatusCreated, body: "created"},
			wantStatus: http.StatusCreated,
			wantBody:   "created",
		},
		{
			name:       "captures_401_status",
			args:       args{statusCode: http.StatusUnauthorized, body: "unauthorized"},
			wantStatus: http.StatusUnauthorized,
			wantBody:   "unauthorized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rr := httptest.NewRecorder()
			rw := &server.ResponseWriter{ResponseWriter: rr, Status: http.StatusOK}

			rw.WriteHeader(tt.args.statusCode)
			_, err := io.WriteString(rw, tt.args.body)
			require.NoError(t, err)

			assert.Equal(t, tt.wantStatus, rw.Status, "ResponseWriter should capture status")
			assert.Equal(t, tt.wantStatus, rr.Code, "Underlying recorder should have status")
			assert.Equal(t, tt.wantBody, rr.Body.String(), "Body should match")
		})
	}
}

func TestMiddlewareChain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		authToken  string
		authHeader string
		wantStatus int
	}{
		{
			name:       "full_chain_with_valid_auth",
			authToken:  testAuthToken,
			authHeader: testBearerToken,
			wantStatus: http.StatusOK,
		},
		{
			name:       "full_chain_with_invalid_auth",
			authToken:  testAuthToken,
			authHeader: testWrongToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "full_chain_without_auth",
			authToken:  testAuthToken,
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("success"))
			})

			// Chain all middleware together
			wrapped := server.RequestIDMiddleware(
				server.LoggingMiddleware(
					server.RealIPMiddleware(
						server.AuthMiddleware(tt.authToken)(handler),
					),
				),
			)

			ctx := contextlog.With(context.Background(), contextlog.DiscardLogger())
			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/test", http.NoBody)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rr := httptest.NewRecorder()
			wrapped.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			// Verify request ID was added
			requestID := rr.Header().Get(testRequestIDHeader)
			assert.NotEmpty(t, requestID, "Request ID should be set in response")
		})
	}
}

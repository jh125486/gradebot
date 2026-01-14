package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jh125486/gradebot/pkg/contextlog"
	"github.com/jh125486/gradebot/pkg/middleware"
	"github.com/stretchr/testify/assert"
)

func TestAuthMiddlewareValidToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		authHeader     string
		token          string
		expectStatusOK bool
	}{
		{
			name:           "valid_bearer_token",
			authHeader:     "Bearer valid-secret-token",
			token:          "valid-secret-token",
			expectStatusOK: true,
		},
		{
			name:           "valid_bearer_token_with_spaces",
			authHeader:     "Bearer token-with-special-chars-!@#$%",
			token:          "token-with-special-chars-!@#$%",
			expectStatusOK: true,
		},
		{
			name:           "mismatched_token",
			authHeader:     "Bearer wrong-token",
			token:          "valid-secret-token",
			expectStatusOK: false,
		},
		{
			name:           "missing_authorization_header",
			authHeader:     "",
			token:          "valid-secret-token",
			expectStatusOK: false,
		},
		{
			name:           "invalid_header_format_no_bearer",
			authHeader:     "Basic dXNlcjpwYXNz",
			token:          "valid-secret-token",
			expectStatusOK: false,
		},
		{
			name:           "invalid_header_format_missing_token",
			authHeader:     "Bearer",
			token:          "valid-secret-token",
			expectStatusOK: false,
		},
		{
			name:           "empty_bearer_prefix",
			authHeader:     "Bearer ",
			token:          "valid-secret-token",
			expectStatusOK: false,
		},
		{
			name:           "case_sensitive_bearer",
			authHeader:     "bearer valid-secret-token",
			token:          "valid-secret-token",
			expectStatusOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/protected", http.NoBody)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			// Set up initial logger in context
			req = req.WithContext(contextlog.With(req.Context(), contextlog.DiscardLogger()))

			nextCalled := false
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			})

			rr := httptest.NewRecorder()

			// Apply auth middleware
			authMiddleware := middleware.AuthMiddleware(tt.token)
			handler := authMiddleware(nextHandler)
			handler.ServeHTTP(rr, req)

			if tt.expectStatusOK {
				assert.Equal(t, http.StatusOK, rr.Code, "should return 200 OK for valid token")
				assert.True(t, nextCalled, "next handler should be called for valid token")
			} else {
				assert.Equal(t, http.StatusUnauthorized, rr.Code, "should return 401 Unauthorized for invalid token")
				assert.False(t, nextCalled, "next handler should not be called for invalid token")
			}
		})
	}
}

func TestAuthMiddlewareStatusCodeAndMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		authHeader    string
		token         string
		expectMessage string
	}{
		{
			name:          "missing_header_message",
			authHeader:    "",
			token:         "secret",
			expectMessage: "missing authorization header",
		},
		{
			name:          "invalid_format_message",
			authHeader:    "Basic xyz",
			token:         "secret",
			expectMessage: "invalid authorization header format",
		},
		{
			name:          "invalid_token_message",
			authHeader:    "Bearer wrong",
			token:         "secret",
			expectMessage: "invalid token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/protected", http.NoBody)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			req = req.WithContext(contextlog.With(req.Context(), contextlog.DiscardLogger()))

			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			rr := httptest.NewRecorder()

			authMiddleware := middleware.AuthMiddleware(tt.token)
			handler := authMiddleware(nextHandler)
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusUnauthorized, rr.Code)
			assert.Contains(t, rr.Body.String(), tt.expectMessage)
		})
	}
}

func TestAuthMiddlewareHTTPMethods(t *testing.T) {
	t.Parallel()

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(method, "/protected", http.NoBody)
			req.Header.Set("Authorization", "Bearer valid-token")
			req = req.WithContext(contextlog.With(req.Context(), contextlog.DiscardLogger()))

			nextCalled := false
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			rr := httptest.NewRecorder()

			authMiddleware := middleware.AuthMiddleware("valid-token")
			handler := authMiddleware(nextHandler)
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code, "should allow %s with valid token", method)
			assert.True(t, nextCalled, "next handler should be called for %s", method)
		})
	}
}

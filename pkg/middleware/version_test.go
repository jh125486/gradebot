package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jh125486/gradebot/pkg/middleware"
	"github.com/stretchr/testify/assert"
)

func TestVersionMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		version        string
		expectVersion  string
		expectStatusOK bool
	}{
		{
			name:           "adds_version_header",
			version:        "v1.2.3",
			expectVersion:  "v1.2.3",
			expectStatusOK: true,
		},
		{
			name:           "adds_develop_version_header",
			version:        "develop",
			expectVersion:  "develop",
			expectStatusOK: true,
		},
		{
			name:           "empty_version",
			version:        "",
			expectVersion:  "",
			expectStatusOK: true,
		},
		{
			name:           "version_with_build_info",
			version:        "v1.0.0-beta.1+build.123",
			expectVersion:  "v1.0.0-beta.1+build.123",
			expectStatusOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			nextCalled := false
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			versionMiddleware := middleware.VersionMiddleware(tt.version)
			handler := versionMiddleware(nextHandler)

			rr := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/test", http.NoBody)

			handler.ServeHTTP(rr, req)

			assert.True(t, nextCalled, "next handler should be called")
			if tt.expectStatusOK {
				assert.Equal(t, http.StatusOK, rr.Code)
			}
			assert.Equal(t, tt.expectVersion, rr.Header().Get("X-Version"),
				"X-Version header should match")
		})
	}
}

func TestVersionMiddlewarePreservesExistingHeaders(t *testing.T) {
	t.Parallel()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "custom-value")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	})

	versionMiddleware := middleware.VersionMiddleware("v1.0.0")
	handler := versionMiddleware(nextHandler)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", http.NoBody)

	handler.ServeHTTP(rr, req)

	assert.Equal(t, "v1.0.0", rr.Header().Get("X-Version"))
	assert.Equal(t, "custom-value", rr.Header().Get("X-Custom-Header"))
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
}

package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jh125486/gradebot/pkg/middleware"
	"github.com/stretchr/testify/require"
)

func TestRequestIDMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		headerSet bool
		headerVal string
	}{
		{name: "uses_existing_header", headerSet: true, headerVal: "test-id-123"},
		{name: "no_header_generates", headerSet: false, headerVal: ""},
		{name: "empty_header_generates", headerSet: true, headerVal: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			if tt.headerSet {
				req.Header.Set(middleware.RequestIDHeader, tt.headerVal)
			}

			var capturedID string
			h := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if id, ok := r.Context().Value(middleware.RequestIDKey).(string); ok {
					capturedID = id
				}
			}))

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			respID := w.Header().Get(middleware.RequestIDHeader)

			if tt.headerSet && tt.headerVal != "" {
				require.Equal(t, tt.headerVal, respID)
				require.Equal(t, tt.headerVal, capturedID)
			} else {
				// Expect a generated UUID
				require.NotEmpty(t, respID)
				_, err := uuid.Parse(respID)
				require.NoError(t, err)
				require.Equal(t, respID, capturedID)
			}
		})
	}
}

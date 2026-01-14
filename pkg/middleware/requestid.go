package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jh125486/gradebot/pkg/contextlog"
)

const RequestIDHeader = "X-Request-ID"

// RequestIDKey is the context key for storing the request ID
const RequestIDKey contextKey = "request-id"

// RequestID generates a unique request ID and adds it to context and logger.
// It checks for an existing X-Request-ID header from upstream proxies, or generates a new one.
// The request ID is added to the response headers and enriched in the logger for log correlation.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Check if request already has an ID from upstream proxy
		requestID := r.Header.Get(RequestIDHeader)
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Store request ID in context
		ctx = context.WithValue(ctx, RequestIDKey, requestID)

		// Enrich logger with request ID
		logger := contextlog.From(ctx).With(slog.String("request_id", requestID))
		ctx = contextlog.With(ctx, logger)

		// Add request ID to response headers for client correlation
		w.Header().Set(RequestIDHeader, requestID)

		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}

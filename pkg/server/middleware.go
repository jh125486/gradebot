package server

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jh125486/gradebot/pkg/contextlog"
	"github.com/tomasen/realip"
)

// Context keys for storing values in request context
type contextKey string

const (
	// RealIPKey is the context key for storing the real client IP address
	RealIPKey contextKey = "real-ip"
	// RequestIDKey is the context key for storing the request ID
	RequestIDKey contextKey = "request-id"
)

// ResponseWriter wraps http.ResponseWriter to capture the status code
type ResponseWriter struct {
	http.ResponseWriter
	Status int
}

// WriteHeader captures the status code and writes it to the underlying ResponseWriter
func (rw *ResponseWriter) WriteHeader(code int) {
	rw.Status = code
	rw.ResponseWriter.WriteHeader(code)
}

// AuthMiddleware returns a middleware function that validates Bearer token authentication.
// It verifies the Authorization header contains a valid "Bearer {token}" before allowing the request through.
// Returns 401 Unauthorized if the token is missing or invalid.
func AuthMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			logger := contextlog.From(ctx)

			authHeader := r.Header.Get("authorization")
			if authHeader == "" {
				logger.WarnContext(ctx, "Authentication failed: missing authorization header")
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}

			const bearerPrefix = "Bearer "
			if !strings.HasPrefix(authHeader, bearerPrefix) {
				logger.WarnContext(ctx, "Authentication failed: invalid header format")
				http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
				return
			}

			bearer := authHeader[len(bearerPrefix):]
			if subtle.ConstantTimeCompare([]byte(bearer), []byte(token)) != 1 {
				logger.WarnContext(ctx, "Authentication failed: invalid token")
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestIDMiddleware generates a unique request ID and adds it to context and logger.
// It checks for an existing X-Request-ID header from upstream proxies, or generates a new one.
// The request ID is added to the response headers and enriched in the logger for log correlation.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Check if request already has an ID from upstream proxy
		requestID := r.Header.Get(requestIDHeader)
		if requestID == "" {
			requestID = uuid.New().String()
		}

		// Store request ID in context
		ctx = context.WithValue(ctx, RequestIDKey, requestID)

		// Enrich logger with request ID
		logger := contextlog.From(ctx).With(slog.String("request_id", requestID))
		ctx = contextlog.With(ctx, logger)

		// Add request ID to response headers for client correlation
		w.Header().Set(requestIDHeader, requestID)

		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}

// LoggingMiddleware logs HTTP requests with method, path, status, duration, and client IP.
// It wraps the ResponseWriter to capture status codes and logs after the request completes.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ctx := r.Context()
		logger := contextlog.From(ctx)

		// Wrap response writer to capture status
		rw := &ResponseWriter{ResponseWriter: w, Status: http.StatusOK}

		// Call next handler
		next.ServeHTTP(rw, r)

		// Log request details
		duration := time.Since(start)
		clientIP := r.RemoteAddr
		if realIP, ok := ctx.Value(RealIPKey).(string); ok && realIP != "" {
			clientIP = realIP
		}

		logger.InfoContext(ctx, "HTTP request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rw.Status),
			slog.Duration("duration", duration),
			slog.String("client_ip", clientIP),
		)
	})
}

// RealIPMiddleware extracts the real client IP and stores it in request context.
// It uses the tomasen/realip library to handle X-Forwarded-For and other proxy headers.
func RealIPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract real IP using tomasen/realip
		realIP := realip.RealIP(r)

		// Store the real IP in the request context
		ctx := context.WithValue(r.Context(), RealIPKey, realIP)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

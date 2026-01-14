package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jh125486/gradebot/pkg/contextlog"
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

// Logging logs HTTP requests with method, path, status, duration, and client IP.
// It wraps the ResponseWriter to capture status codes and logs after the request completes.
func Logging(next http.Handler) http.Handler {
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

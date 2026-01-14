package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/jh125486/gradebot/pkg/contextlog"
)

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

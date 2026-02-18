package middleware

import "net/http"

// VersionMiddleware returns a middleware that adds the X-Version header to all responses.
// This allows clients to detect the server version and warn if there's a mismatch.
func VersionMiddleware(version string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Version", version)
			next.ServeHTTP(w, r)
		})
	}
}

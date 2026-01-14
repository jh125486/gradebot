package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"github.com/tomasen/realip"
)

type contextKey string

// RealIPKey is the context key for storing the real client IP address
const RealIPKey contextKey = "real-ip"

// StoreRealIP extracts the real client IP and stores it in request context.
// It uses the tomasen/realip library to handle X-Forwarded-For and other proxy headers.
func StoreRealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract real IP using tomasen/realip (handles X-Forwarded-For, X-Real-IP, RemoteAddr)
		realIP := realip.RealIP(r)

		// Store the IP in the request context (realip never returns empty)
		ctx := context.WithValue(r.Context(), RealIPKey, realIP)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)
	})
}

// IPExtractable interface for types that can provide IP extraction methods
type IPExtractable interface {
	Header() http.Header
	Peer() connect.Peer
}

const (
	UnknownIP            = "unknown"
	XFFHeader            = "X-Forwarded-For"
	XRealIPHeader        = "X-Real-IP"
	CFConnectingIPHeader = "CF-Connecting-IP"
)

// ClientIP extracts client IP from any object that implements IPExtractable
func ClientIP(ctx context.Context, req IPExtractable) string {
	// Method 1: Try to get from context (set by StoreRealIP middleware)
	if realIP, ok := ctx.Value(RealIPKey).(string); ok && realIP != "" && realIP != UnknownIP {
		return realIP
	}

	// Method 2: Try to extract from HTTP headers
	headers := req.Header()
	if xff := headers.Get(XFFHeader); xff != "" {
		if ips := strings.Split(xff, ","); len(ips) > 0 {
			if ip := strings.TrimSpace(ips[0]); ip != "" && ip != UnknownIP {
				return ip
			}
		}
	}
	if ip := headers.Get(XRealIPHeader); ip != "" && ip != UnknownIP {
		return ip
	}
	if ip := headers.Get(CFConnectingIPHeader); ip != "" && ip != UnknownIP {
		return ip
	}

	// Method 3: Try peer info as fallback
	peer := req.Peer()
	if peer.Addr != "" {
		if ip, _, err := net.SplitHostPort(peer.Addr); err == nil {
			return ip
		}
		return peer.Addr
	}

	// No methods worked.
	return UnknownIP
}

package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/jh125486/gradebot/pkg/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockExtractable struct {
	headers http.Header
	peer    connect.Peer
}

func (m *mockExtractable) Header() http.Header {
	return m.headers
}

func (m *mockExtractable) Peer() connect.Peer {
	return m.peer
}

// headers helper returns an http.Header with canonicalized keys (via Set).
func headers(kv map[string]string) http.Header {
	h := make(http.Header)
	for k, v := range kv {
		h.Set(k, v)
	}
	return h
}

func TestStoreRealIPMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
	}{
		{name: "xff_header", headers: map[string]string{"X-Forwarded-For": "203.0.113.195"}, remoteAddr: "172.16.0.1:8080"},
		{name: "x_real_ip_header", headers: map[string]string{"X-Real-IP": "203.0.113.200"}, remoteAddr: "172.16.0.1:8080"},
		{name: "remote_addr_fallback", headers: map[string]string{}, remoteAddr: "203.0.113.1:1234"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			req.RemoteAddr = tt.remoteAddr

			ipExtracted := false
			h := middleware.StoreRealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify that StoreRealIP middleware stores some IP (non-empty) in context
				if ip, ok := r.Context().Value(middleware.RealIPKey).(string); ok && ip != "" {
					ipExtracted = true
				}
			}))

			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			require.True(t, ipExtracted, "StoreRealIP middleware should store an IP address in context")
		})
	}
}

func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		contextValue string // empty => no context value, otherwise used as RealIPKey
		headers      http.Header
		peer         connect.Peer
		want         string
	}{
		// X-Forwarded-For variations
		{name: "xff_single", headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "192.168.1.1"},
		{name: "xff_first_of_multiple", headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1, 10.0.0.1, 172.16.0.1"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "192.168.1.1"},
		{name: "xff_with_whitespace", headers: headers(map[string]string{"X-Forwarded-For": "  192.168.1.1  , 10.0.0.1"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "192.168.1.1"},
		{name: "xff_empty_falls_back_xreal", headers: headers(map[string]string{"X-Forwarded-For": "", "X-Real-IP": "10.0.0.1"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "10.0.0.1"},

		// X-Real-IP variations
		{name: "xreal_valid", headers: headers(map[string]string{"X-Real-IP": "192.168.1.1"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "192.168.1.1"},
		{name: "xreal_ipv6", headers: headers(map[string]string{"X-Real-IP": "2001:db8::1"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "2001:db8::1"},
		{name: "xreal_empty_falls_back_cf", headers: headers(map[string]string{"X-Real-IP": "", "CF-Connecting-IP": "203.0.113.1"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "203.0.113.1"},
		{name: "xreal_unknown_falls_back_peer", headers: headers(map[string]string{"X-Real-IP": "unknown"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "172.16.0.1"},

		// CF-Connecting-IP variations
		{name: "cf_valid", headers: headers(map[string]string{"CF-Connecting-IP": "203.0.113.1"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "203.0.113.1"},
		{name: "cf_ipv6", headers: headers(map[string]string{"CF-Connecting-IP": "2001:db8::2"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "2001:db8::2"},
		{name: "cf_empty_falls_back_peer", headers: headers(map[string]string{"CF-Connecting-IP": ""}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "172.16.0.1"},
		{name: "cf_unknown_falls_back_peer", headers: headers(map[string]string{"CF-Connecting-IP": "unknown"}), peer: connect.Peer{Addr: "192.168.1.1:9000"}, want: "192.168.1.1"},

		// Peer/fallback behavior
		{name: "peer_with_port", headers: headers(map[string]string{}), peer: connect.Peer{Addr: "192.168.1.1:8080"}, want: "192.168.1.1"},
		{name: "peer_ipv6_with_port", headers: headers(map[string]string{}), peer: connect.Peer{Addr: "[2001:db8::1]:8080"}, want: "2001:db8::1"},
		{name: "peer_without_port", headers: headers(map[string]string{}), peer: connect.Peer{Addr: "192.168.1.1"}, want: "192.168.1.1"},
		{name: "peer_empty_returns_unknown", headers: headers(map[string]string{}), peer: connect.Peer{Addr: ""}, want: middleware.UnknownIP},

		// Context priority and fallbacks
		{name: "context_priority_over_headers", contextValue: "10.0.0.1", headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1", "X-Real-IP": "172.16.0.1"}), peer: connect.Peer{Addr: "203.0.113.1:8080"}, want: "10.0.0.1"},
		{name: "xff_priority_over_xrealip", headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1", "X-Real-IP": "10.0.0.1"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "192.168.1.1"},
		{name: "context_empty_string_falls_back_to_headers", headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "192.168.1.1"},
		{name: "context_unknown_falls_back_to_headers", contextValue: middleware.UnknownIP, headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "192.168.1.1"},
		{name: "xrealip_priority_over_cf", headers: headers(map[string]string{"X-Real-IP": "10.0.0.1", "CF-Connecting-IP": "203.0.113.1"}), peer: connect.Peer{Addr: "172.16.0.1:8080"}, want: "10.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			if tt.contextValue != "" {
				ctx = context.WithValue(ctx, middleware.RealIPKey, tt.contextValue)
			}

			req := &mockExtractable{headers: tt.headers, peer: tt.peer}
			got := middleware.ClientIP(ctx, req)
			assert.Equal(t, tt.want, got)
		})
	}
}

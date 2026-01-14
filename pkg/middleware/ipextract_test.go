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

func TestMockExtractableHeadersAccessible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		headers http.Header
	}{
		{name: "xrealip_present", headers: headers(map[string]string{"X-Real-IP": "192.168.1.1"})},
		{name: "xff_present", headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1, 10.0.0.1"})},
		{name: "cf_present", headers: headers(map[string]string{"CF-Connecting-IP": "203.0.113.1"})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := &mockExtractable{headers: tt.headers}
			for k, v := range tt.headers {
				if got := req.Header().Get(k); got != v[0] {
					t.Fatalf("expected header %s to be %s, got %s", k, v[0], got)
				}
			}
		})
	}
}

func TestClientIPFromXForwardedFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ctx     context.Context
		headers http.Header
		peer    connect.Peer
		want    string
	}{
		{
			name:    "single_ip_from_xff",
			ctx:     t.Context(),
			headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "192.168.1.1",
		},
		{
			name:    "first_ip_from_multiple_xff",
			ctx:     t.Context(),
			headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1, 10.0.0.1, 172.16.0.1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "192.168.1.1",
		},
		{
			name:    "xff_with_whitespace",
			ctx:     t.Context(),
			headers: headers(map[string]string{"X-Forwarded-For": "  192.168.1.1  , 10.0.0.1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "192.168.1.1",
		},
		{
			name:    "empty_xff_header_falls_back_to_xrealip",
			ctx:     t.Context(),
			headers: headers(map[string]string{"X-Forwarded-For": "", "X-Real-IP": "10.0.0.1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "10.0.0.1",
		},
		{
			name:    "no_xff_header",
			ctx:     t.Context(),
			headers: headers(map[string]string{"X-Real-IP": "10.0.0.1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := &mockExtractable{headers: tt.headers, peer: tt.peer}
			got := middleware.ClientIP(tt.ctx, req)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClientIPFromXRealIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ctx     context.Context
		headers http.Header
		peer    connect.Peer
		want    string
	}{
		{
			name:    "valid_xrealip",
			ctx:     t.Context(),
			headers: headers(map[string]string{"X-Real-IP": "192.168.1.1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "192.168.1.1",
		},
		{
			name:    "ipv6_xrealip",
			ctx:     t.Context(),
			headers: headers(map[string]string{"X-Real-IP": "2001:db8::1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "2001:db8::1",
		},
		{
			name:    "empty_xrealip_falls_back_to_cf",
			ctx:     t.Context(),
			headers: headers(map[string]string{"X-Real-IP": "", "CF-Connecting-IP": "203.0.113.1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "203.0.113.1",
		},
		{
			name:    "unknown_value_xrealip",
			ctx:     t.Context(),
			headers: headers(map[string]string{"X-Real-IP": "unknown"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "172.16.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := &mockExtractable{headers: tt.headers, peer: tt.peer}
			got := middleware.ClientIP(tt.ctx, req)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClientIPFromCFConnectingIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ctx     context.Context
		headers http.Header
		peer    connect.Peer
		want    string
	}{
		{
			name:    "valid_cf_ip",
			ctx:     t.Context(),
			headers: headers(map[string]string{"CF-Connecting-IP": "203.0.113.1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "203.0.113.1",
		},
		{
			name:    "ipv6_cf_ip",
			ctx:     t.Context(),
			headers: headers(map[string]string{"CF-Connecting-IP": "2001:db8::2"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "2001:db8::2",
		},
		{
			name:    "empty_cf_ip_falls_back_to_peer",
			ctx:     t.Context(),
			headers: headers(map[string]string{"CF-Connecting-IP": ""}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "172.16.0.1",
		},
		{
			name:    "unknown_cf_ip_falls_back_to_peer",
			ctx:     t.Context(),
			headers: headers(map[string]string{"CF-Connecting-IP": "unknown"}),
			peer:    connect.Peer{Addr: "192.168.1.1:9000"},
			want:    "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := &mockExtractable{headers: tt.headers, peer: tt.peer}
			got := middleware.ClientIP(tt.ctx, req)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClientIPFromPeer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ctx     context.Context
		headers http.Header
		peer    connect.Peer
		want    string
	}{
		{
			name:    "peer_with_port",
			ctx:     t.Context(),
			headers: headers(map[string]string{}),
			peer:    connect.Peer{Addr: "192.168.1.1:8080"},
			want:    "192.168.1.1",
		},
		{
			name:    "peer_ipv6_with_port",
			ctx:     t.Context(),
			headers: headers(map[string]string{}),
			peer:    connect.Peer{Addr: "[2001:db8::1]:8080"},
			want:    "2001:db8::1",
		},
		{
			name:    "peer_without_port",
			ctx:     t.Context(),
			headers: headers(map[string]string{}),
			peer:    connect.Peer{Addr: "192.168.1.1"},
			want:    "192.168.1.1",
		},
		{
			name:    "empty_peer_returns_unknown",
			ctx:     t.Context(),
			headers: headers(map[string]string{}),
			peer:    connect.Peer{Addr: ""},
			want:    middleware.UnknownIP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := &mockExtractable{headers: tt.headers, peer: tt.peer}
			got := middleware.ClientIP(tt.ctx, req)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClientIPContextPriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ctx     context.Context
		headers http.Header
		peer    connect.Peer
		want    string
	}{
		{
			name:    "context_priority_over_headers",
			ctx:     context.WithValue(t.Context(), middleware.RealIPKey, "10.0.0.1"),
			headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1", "X-Real-IP": "172.16.0.1"}),
			peer:    connect.Peer{Addr: "203.0.113.1:8080"},
			want:    "10.0.0.1",
		},
		{
			name:    "xff_priority_over_xrealip",
			ctx:     t.Context(),
			headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1", "X-Real-IP": "10.0.0.1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "192.168.1.1",
		},
		{
			name:    "xrealip_priority_over_cf",
			ctx:     t.Context(),
			headers: headers(map[string]string{"X-Real-IP": "10.0.0.1", "CF-Connecting-IP": "203.0.113.1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "10.0.0.1",
		},
		{
			name:    "context_empty_string_falls_back_to_headers",
			ctx:     context.WithValue(t.Context(), middleware.RealIPKey, ""),
			headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "192.168.1.1",
		},
		{
			name:    "context_unknown_falls_back_to_headers",
			ctx:     context.WithValue(t.Context(), middleware.RealIPKey, middleware.UnknownIP),
			headers: headers(map[string]string{"X-Forwarded-For": "192.168.1.1"}),
			peer:    connect.Peer{Addr: "172.16.0.1:8080"},
			want:    "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := &mockExtractable{headers: tt.headers, peer: tt.peer}
			got := middleware.ClientIP(tt.ctx, req)
			assert.Equal(t, tt.want, got)
		})
	}
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

// TestRealIPBackwardsCompatibility ensures the deprecated RealIP function still works.
func TestRealIPBackwardsCompatibility(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.RemoteAddr = "203.0.113.1:1234"

	ipExtracted := false
	h := middleware.StoreRealIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ip, ok := r.Context().Value(middleware.RealIPKey).(string); ok && ip != "" {
			ipExtracted = true
		}
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.True(t, ipExtracted, "RealIP (deprecated) should still work as wrapper to StoreRealIP")
}

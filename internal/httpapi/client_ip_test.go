package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"retrom/internal/testassert"
)

func TestCanonicalClientIPUsesSingleProxyForwardedAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		remote     string
		forwarded  []string
		want       string
		diagnostic string
	}{
		{
			name: "proxy forwarded address", remote: "10.0.0.1:4200",
			forwarded: []string{"203.0.113.9"}, want: "203.0.113.9",
		},
		{
			name: "forwarded address without peer allowlist", remote: "192.0.2.8:4200",
			forwarded: []string{"203.0.113.9"}, want: "203.0.113.9",
		},
		{
			name: "direct request uses peer", remote: "192.0.2.8:4200", want: "192.0.2.8",
		},
		{
			name: "invalid forwarded address falls back", remote: "10.0.0.1:4200",
			forwarded: []string{"not-an-ip"}, want: "10.0.0.1",
			diagnostic: "CLIENT_IP_XFF_ADDRESS_INVALID",
		},
		{
			name: "multiple header fields fall back", remote: "10.0.0.1:4200",
			forwarded: []string{"203.0.113.8", "203.0.113.9"}, want: "10.0.0.1",
			diagnostic: "CLIENT_IP_XFF_MULTIPLE_OR_EMPTY",
		},
		{
			name: "forwarded chain falls back", remote: "10.0.0.1:4200",
			forwarded: []string{"203.0.113.8, 10.0.0.2"}, want: "10.0.0.1",
			diagnostic: "CLIENT_IP_XFF_ADDRESS_INVALID",
		},
		{
			name: "mapped peer is canonical", remote: "[::ffff:192.0.2.10]:4200",
			want: "192.0.2.10",
		},
		{
			name: "mapped forwarded address is canonical", remote: "10.0.0.1:4200",
			forwarded: []string{"::ffff:192.0.2.10"}, want: "192.0.2.10",
		},
		{
			name: "invalid peer falls back to unknown", remote: "invalid-peer",
			forwarded: []string{"203.0.113.9"}, want: unknownClientIP,
			diagnostic: "CLIENT_IP_PEER_INVALID",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(context.Background(), "POST", "http://retrom.example/api/v1/auth/login", nil)
			request.RemoteAddr = test.remote
			for _, value := range test.forwarded {
				request.Header.Add("X-Forwarded-For", value)
			}
			request.Header.Set("X-Real-IP", "198.51.100.200")
			address, diagnostic := canonicalClientIP(request)
			testassert.Falsef(t, testassert.Any(func() bool { return address != test.want }, func() bool { return diagnostic != test.diagnostic }), "canonicalClientIP() = %q/%q, want %q/%q", address, diagnostic, test.want, test.diagnostic)
		})
	}
}

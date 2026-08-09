package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestCanonicalClientIPTrustsOnlyConfiguredProxyChain(t *testing.T) {
	t.Parallel()
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	tests := []struct {
		name       string
		remote     string
		forwarded  []string
		proxies    []netip.Prefix
		want       string
		diagnostic string
	}{
		{
			name: "untrusted peer ignores spoof", remote: "192.0.2.8:4200",
			forwarded: []string{"203.0.113.9"}, proxies: trusted, want: "192.0.2.8",
		},
		{
			name: "rightmost untrusted client", remote: "10.0.0.1:4200",
			forwarded: []string{"203.0.113.9, 10.0.0.2"}, proxies: trusted, want: "203.0.113.9",
		},
		{
			name: "invalid forwarded address falls back", remote: "10.0.0.1:4200",
			forwarded: []string{"not-an-ip"}, proxies: trusted, want: "10.0.0.1",
			diagnostic: "CLIENT_IP_XFF_ADDRESS_INVALID",
		},
		{
			name: "multiple header fields fall back", remote: "10.0.0.1:4200",
			forwarded: []string{"203.0.113.8", "203.0.113.9"}, proxies: trusted, want: "10.0.0.1",
			diagnostic: "CLIENT_IP_XFF_MISSING_OR_MULTIPLE",
		},
		{
			name: "mapped peer is canonical", remote: "[::ffff:192.0.2.10]:4200",
			want: "192.0.2.10",
		},
		{
			name: "oversized chain falls back", remote: "10.0.0.1:4200",
			forwarded: []string{strings.Repeat("192.0.2.1,", 16) + "192.0.2.1"},
			proxies:   trusted, want: "10.0.0.1", diagnostic: "CLIENT_IP_XFF_LENGTH_INVALID",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest("POST", "http://retrom.example/api/v1/auth/login", nil)
			request.RemoteAddr = test.remote
			for _, value := range test.forwarded {
				request.Header.Add("X-Forwarded-For", value)
			}
			request.Header.Set("X-Real-IP", "198.51.100.200")
			address, diagnostic := canonicalClientIP(request, test.proxies)
			if address != test.want || diagnostic != test.diagnostic {
				t.Fatalf("canonicalClientIP() = %q/%q, want %q/%q", address, diagnostic, test.want, test.diagnostic)
			}
		})
	}
}

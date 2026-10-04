package config

import (
	"net/url"
	"testing"
)

func TestNetworkEnvironmentUsesEffectiveRuntimeOrigin(t *testing.T) {
	t.Setenv("RETROM_HTTP_ADDR", "127.0.0.1:8080")
	t.Setenv("RETROM_PUBLIC_ORIGIN", "https://example.com")
	t.Setenv("RETROM_ALLOW_INSECURE_PUBLIC_ORIGIN", "")
	for _, tc := range []struct{ value, expected string }{
		{"", "https://{launchId}.example.com"},
		{"https://{launchId}.games.example.com", "https://{launchId}.games.example.com"},
	} {
		t.Setenv("RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE", tc.value)
		network, err := loadNetworkConfig(ModeRelease)
		if err != nil || network.rpgRuntimeOriginTemplate != tc.expected {
			t.Fatalf("effective template = %q, error = %v; want %q", network.rpgRuntimeOriginTemplate, err, tc.expected)
		}
	}
}

func TestRuntimeOriginDefaultUsesCompleteApplicationHostAndPort(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ origin, template, expected string }{
		{"https://example.com", "", "https://{launchId}.example.com"},
		{"https://play.example.com:8443", "", "https://{launchId}.play.example.com:8443"},
		{"https://example.com:443", "", "https://{launchId}.example.com:443"},
		{"https://play.example.com", "https://{launchId}.games.example.com", "https://{launchId}.games.example.com"},
		{"http://localhost:4000", "http://{launchId}.rpg.localhost:8080", "http://{launchId}.rpg.localhost:8080"},
	} {
		t.Run(tc.origin+tc.template, func(t *testing.T) {
			origin, err := url.Parse(tc.origin)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := parseRPGRuntimeOriginTemplate(tc.template, origin, true)
			if err != nil || actual != tc.expected {
				t.Fatalf("template = %q, error = %v; want %q", actual, err, tc.expected)
			}
		})
	}
}

func TestRuntimeOriginDefaultDoesNotBypassValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ origin, template string }{
		{"https://127.0.0.1", ""},
		{"https://[::1]:8443", ""},
		{"https://com", ""},
		{"https://github.io", ""},
		{"https://example.com", " "},
		{"https://example.com", "https://{launchId}.unrelated.com"},
		{"https://example.com", "http://{launchId}.example.com"},
		{"https://example.com", "https://example.com/{launchId}"},
		{"http://localhost:4000", ""},
	} {
		t.Run(tc.origin+tc.template, func(t *testing.T) {
			origin, err := url.Parse(tc.origin)
			if err != nil {
				t.Fatal(err)
			}
			if value, err := parseRPGRuntimeOriginTemplate(tc.template, origin, true); err == nil {
				t.Fatalf("invalid configuration accepted: %q", value)
			}
		})
	}
	if _, err := parseRPGRuntimeOriginTemplate("", nil, false); err == nil {
		t.Fatal("missing application origin accepted")
	}
}

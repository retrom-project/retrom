package config

import (
	"net/url"
	"testing"
)

func TestRuntimeCookieDomain(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ application, template, domain string }{
		{"https://example.com", "https://{launchId}.example.com", "example.com"},
		{"https://play.example.com:8443", "https://{launchId}.play.example.com:8443", "play.example.com"},
		{"https://play.example.com", "https://{launchId}.games.example.com", "example.com"},
		{"https://play.example.com", "https://{launchId}.rpg.play.example.com", "play.example.com"},
		{"http://pfb.localhost:3000", "http://{launchId}.rpg.pfb.localhost:3000", "pfb.localhost"},
		{"http://localhost:4000", "http://{launchId}.rpg.localhost:8080", "localhost"},
		{"https://example.co.uk", "https://{launchId}.unrelated.co.uk", ""},
		{"https://one.github.io", "https://{launchId}.two.github.io", ""},
		{"https://example.com", "http://{launchId}.example.com", ""},
		{"http://127.0.0.1:3000", "http://{launchId}.localhost:3000", ""},
	} {
		t.Run(tc.application+tc.template, func(t *testing.T) {
			application, err := url.Parse(tc.application)
			if err != nil {
				t.Fatal(err)
			}
			domain, err := RuntimeCookieDomain(application, tc.template)
			if domain != tc.domain || (err != nil) != (tc.domain == "") {
				t.Fatalf("domain=%q error=%v", domain, err)
			}
		})
	}
}

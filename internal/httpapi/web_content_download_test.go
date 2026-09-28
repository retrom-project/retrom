package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebContentBytesCannotExecuteOnApplicationOrigin(t *testing.T) {
	writer := httptest.NewRecorder()
	setWebContentDownloadHeaders(writer)
	for key, expected := range map[string]string{
		"Content-Type":                 "application/octet-stream",
		"Content-Disposition":          `attachment; filename="content"`,
		"Content-Security-Policy":      "sandbox; default-src 'none'",
		"X-Content-Type-Options":       "nosniff",
		"Cross-Origin-Resource-Policy": "same-origin",
	} {
		if writer.Header().Get(key) != expected {
			t.Fatalf("%s: %q", key, writer.Header().Get(key))
		}
	}
}

func TestManagedWebDocumentsPreserveEngineIsolationPolicy(t *testing.T) {
	for _, format := range []string{"RPG_MAKER_PROJECT", "TYRANOSCRIPT_PROJECT"} {
		config := nativeContentConfiguration(format, "https://app.example.test")
		if !strings.Contains(config.CSP, "frame-ancestors https://app.example.test") || !strings.Contains(config.CSP, "frame-src 'none'") {
			t.Fatal(config.CSP)
		}
		if strings.Contains(config.CSP, "script-src 'self' 'unsafe-inline'") != (format == "TYRANOSCRIPT_PROJECT") {
			t.Fatal(config.CSP)
		}
		if config.Bootstrap != webContentBootstrapPath || config.Permissions != rpgRuntimePermissionsPolicy {
			t.Fatal(config)
		}
	}
}

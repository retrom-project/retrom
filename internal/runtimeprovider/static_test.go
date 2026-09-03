package runtimeprovider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/runtimebundle"
)

func TestStaticHandlerServesOnlyVerifiedActivePublicFiles(t *testing.T) {
	handler := fixtureStaticHandler(t)
	bundle := strings.Repeat("a", 64)
	base := "/runtime/providers/fixture/" + bundle + "/"

	assertClientResponse(t, request(t, handler, http.MethodGet, base+"client.mjs", ""))
	assertHeadResponse(t, request(t, handler, http.MethodHead, base+"client.mjs", ""))
	assertRangeResponse(t, request(t, handler, http.MethodGet, base+"assets/core.wasm", "bytes=1-2"))
	unsupportedRange := request(t, handler, http.MethodGet, base+"client.mjs", "bytes=0-1")
	if unsupportedRange.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("unsupported range = %d %q", unsupportedRange.Code, unsupportedRange.Body.String())
	}
	assertNotFoundPaths(t, handler, base, bundle)
}

func assertClientResponse(t *testing.T, client *httptest.ResponseRecorder) {
	t.Helper()
	if client.Code != http.StatusOK || client.Body.String() != "export{}" ||
		client.Header().Get("Content-Type") != "text/javascript; charset=utf-8" ||
		client.Header().Get("X-Content-Type-Options") != "nosniff" ||
		client.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		client.Header().Get("ETag") != `"`+digestBytes([]byte("export{}"))+`"` {
		t.Fatalf("client response = %d headers=%v body=%q", client.Code, client.Header(), client.Body.String())
	}
}

func assertHeadResponse(t *testing.T, head *httptest.ResponseRecorder) {
	t.Helper()
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "8" {
		t.Fatalf("HEAD response = %d headers=%v body=%q", head.Code, head.Header(), head.Body.String())
	}
}

func assertRangeResponse(t *testing.T, partial *httptest.ResponseRecorder) {
	t.Helper()
	if partial.Code != http.StatusPartialContent || partial.Body.String() != "as" ||
		partial.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("range response = %d headers=%v body=%q", partial.Code, partial.Header(), partial.Body.String())
	}
}

func assertNotFoundPaths(t *testing.T, handler http.Handler, base, bundle string) {
	t.Helper()
	for _, path := range []string{
		"/runtime/providers/fixture/" + strings.Repeat("b", 64) + "/client.mjs",
		base + "provider.json", base + "assets/", base + "%2e%2e/client.mjs",
		"/runtime/providers/unknown/" + bundle + "/client.mjs",
	} {
		response := request(t, handler, http.MethodGet, path, "")
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s = %d %q", path, response.Code, response.Body.String())
		}
	}
}

func TestStaticHandlerRejectsIntegrityDriftBeforeServing(t *testing.T) {
	root := t.TempDir()
	bundle := strings.Repeat("a", 64)
	directory := filepath.Join(root, "fixture", bundle)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "client.mjs"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	active := fixtureActive(bundle)
	_, err := NewStaticHandler(root, active, map[string][]runtimebundle.IntegrityFile{
		"fixture": {{
			Path: "client.mjs", SizeBytes: 8, SHA256: digestBytes([]byte("export{}")),
			MediaType: "text/javascript; charset=utf-8",
		}},
	})
	if !errors.Is(err, ErrInstallationInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func TestStaticHandlerRequiresActiveClientModuleIdentity(t *testing.T) {
	root := t.TempDir()
	bundle := strings.Repeat("a", 64)
	directory := filepath.Join(root, "fixture", bundle)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := []byte("export{}")
	if err := os.WriteFile(filepath.Join(directory, "client.mjs"), contents, 0o644); err != nil {
		t.Fatal(err)
	}
	active := fixtureActive(bundle)
	active.Providers[0].ModuleSHA256 = strings.Repeat("f", 64)
	_, err := NewStaticHandler(root, active, map[string][]runtimebundle.IntegrityFile{
		"fixture": {{
			Path: "client.mjs", SizeBytes: int64(len(contents)), SHA256: digestBytes(contents),
			MediaType: "text/javascript; charset=utf-8",
		}},
	})
	if !errors.Is(err, ErrInstallationInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func fixtureStaticHandler(t *testing.T) http.Handler {
	t.Helper()
	root := t.TempDir()
	bundle := strings.Repeat("a", 64)
	directory := filepath.Join(root, "fixture", bundle)
	if err := os.MkdirAll(filepath.Join(directory, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]struct {
		contents  []byte
		mediaType string
	}{
		"client.mjs":       {[]byte("export{}"), "text/javascript; charset=utf-8"},
		"assets/core.wasm": {[]byte("wasm"), "application/wasm"},
		"provider.json":    {[]byte("{}"), "application/json; charset=utf-8"},
	}
	integrity := make([]runtimebundle.IntegrityFile, 0, len(files))
	for path, file := range files {
		if err := os.WriteFile(filepath.Join(directory, filepath.FromSlash(path)), file.contents, 0o644); err != nil {
			t.Fatal(err)
		}
		integrity = append(integrity, runtimebundle.IntegrityFile{
			Path: path, SizeBytes: int64(len(file.contents)),
			SHA256: digestBytes(file.contents), MediaType: file.mediaType,
		})
	}
	handler, err := NewStaticHandler(root, fixtureActive(bundle), map[string][]runtimebundle.IntegrityFile{"fixture": integrity})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func fixtureActive(bundle string) runtimebundle.ActiveDescriptor {
	return runtimebundle.ActiveDescriptor{SchemaVersion: 1, Providers: []runtimebundle.ActiveProvider{{
		ProviderID: "fixture", BundleSHA256: bundle, InstallationPath: "fixture/" + bundle,
		ClientModulePath: "client.mjs", ModuleSHA256: digestBytes([]byte("export{}")),
	}}}
}

func request(t *testing.T, handler http.Handler, method, path, byteRange string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func digestBytes(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}

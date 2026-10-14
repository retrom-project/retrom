package httpapi

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
)

func providerCacheTestServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	root := t.TempDir()
	for name, data := range map[string]string{"client.mjs": "formal", "core.wasm": "binary", "style.css": "static"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	devRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(devRoot, "module"), []byte("development"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devRoot, "wasm"), []byte("new-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	bundle, revision := strings.Repeat("a", 64), strings.Repeat("b", 64)
	s := &Server{Runtime: &runtimeclient.Client{
		Providers: map[string]runtimeclient.Provider{"test": {
			ProviderID: "test", BundleSHA256: bundle, ModuleSHA256: revision, Root: root,
		}},
		Assets: map[string]map[string]bool{"test": {"client.mjs": true, "core.wasm": true, "style.css": true}},
		Overrides: map[string]map[string]runtimeclient.DevelopmentAsset{"test": {
			"client.mjs": {Root: devRoot, Name: "module"}, "core.wasm": {Root: devRoot, Name: "wasm"},
		}},
	}}
	return s, bundle, revision
}

func TestProviderAssetsSeparateImmutableAndDevelopmentBytes(t *testing.T) {
	s, bundle, revision := providerCacheTestServer(t)
	for _, test := range []struct{ asset, body, cache string }{
		{"client.mjs", "formal", "private, max-age=31536000, immutable, no-transform"},
		{"dev/" + revision + "/client.mjs", "development", "private, no-store, no-transform"},
		{"dev/" + revision + "/core.wasm", "new-binary", "private, no-store, no-transform"},
		{"core.wasm", "binary", "private, no-store, no-transform"},
		{"style.css", "static", "private, max-age=31536000, immutable, no-transform"},
		{"dev/" + revision + "/style.css", "static", "private, no-store, no-transform"},
	} {
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/runtime/providers/test/"+bundle+"/"+test.asset, nil)
		r.SetPathValue("providerId", "test")
		r.SetPathValue("bundle", bundle)
		r.SetPathValue("asset", test.asset)
		w := httptest.NewRecorder()
		w.Header().Set("Cache-Control", "private, no-store, no-transform")
		if err := s.providerAsset(w, r, model.Principal{}); err != nil {
			t.Fatalf("%s: %v", test.asset, err)
		}
		if w.Body.String() != test.body || w.Header().Get("Cache-Control") != test.cache {
			t.Errorf("%s: body=%q cache=%q", test.asset, w.Body.String(), w.Header().Get("Cache-Control"))
		}
	}
	r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
	r.SetPathValue("providerId", "test")
	r.SetPathValue("bundle", bundle)
	r.SetPathValue("asset", "dev/"+strings.Repeat("c", 64)+"/client.mjs")
	if err := s.providerAsset(httptest.NewRecorder(), r, model.Principal{}); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("stale development revision: %v", err)
	}
}

func TestProviderLargeScriptsDoNotDuplicatePersistentAssets(t *testing.T) {
	if got := providerAssetCacheControl("large.js", 8*1024*1024+1, false); got != "private, no-store, no-transform" {
		t.Fatalf("large script cache: %s", got)
	}
}

func TestDevelopmentProviderStreamsHeadAndRange(t *testing.T) {
	s, bundle, revision := providerCacheTestServer(t)
	for _, test := range []struct {
		method, rangeHeader, body, length string
		status                            int
	}{
		{"HEAD", "", "", "10", 200},
		{"GET", "bytes=4-9", "binary", "6", 206},
	} {
		r := httptest.NewRequestWithContext(t.Context(), test.method, "/", nil)
		r.SetPathValue("providerId", "test")
		r.SetPathValue("bundle", bundle)
		r.SetPathValue("asset", "dev/"+revision+"/core.wasm")
		r.Header.Set("Range", test.rangeHeader)
		w := httptest.NewRecorder()
		if err := s.providerAsset(w, r, model.Principal{}); err != nil {
			t.Fatal(err)
		}
		if w.Code != test.status || w.Body.String() != test.body || w.Header().Get("Content-Length") != test.length ||
			w.Header().Get("Content-Type") != "application/wasm" || w.Header().Get("Cache-Control") != "private, no-store, no-transform" {
			t.Fatalf("development %s %s: %d %v %q", test.method, test.rangeHeader, w.Code, w.Header(), w.Body.String())
		}
	}
}

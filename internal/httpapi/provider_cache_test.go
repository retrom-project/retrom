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

func TestProviderAssetsSeparateImmutableAndDevelopmentBytes(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{"client.mjs": "formal", "core.wasm": "binary", "style.css": "static"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bundle, revision := strings.Repeat("a", 64), strings.Repeat("b", 64)
	s := &Server{Runtime: &runtimeclient.Client{
		Providers: map[string]runtimeclient.Provider{"test": {
			ProviderID: "test", BundleSHA256: bundle, ModuleSHA256: revision, Root: root,
		}},
		Assets:    map[string]map[string]bool{"test": {"client.mjs": true, "core.wasm": true, "style.css": true}},
		Overrides: map[string]map[string][]byte{"test": {"client.mjs": []byte("development")}},
	}}
	for _, test := range []struct{ asset, body, cache string }{
		{"client.mjs", "formal", "private, max-age=31536000, immutable"},
		{"dev/" + revision + "/client.mjs", "development", "private, no-store"},
		{"core.wasm", "binary", "private, no-store"},
		{"style.css", "static", "private, max-age=31536000, immutable"},
		{"dev/" + revision + "/style.css", "static", "private, no-store"},
	} {
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/runtime/providers/test/"+bundle+"/"+test.asset, nil)
		r.SetPathValue("providerId", "test")
		r.SetPathValue("bundle", bundle)
		r.SetPathValue("asset", test.asset)
		w := httptest.NewRecorder()
		w.Header().Set("Cache-Control", "private, no-store")
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
	if got := providerAssetCacheControl("large.js", 8*1024*1024+1, false); got != "private, no-store" {
		t.Fatalf("large script cache: %s", got)
	}
}

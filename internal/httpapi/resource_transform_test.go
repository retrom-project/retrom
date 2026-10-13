package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"
)

func TestManagedRangePreservesBytesThroughTransformingProxies(t *testing.T) {
	t.Parallel()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	payload := bytes.Repeat([]byte("<p>immutable game entry</p>"), 128)
	file, err := store.Write(t.Context(), "games", uuid.NewString(), bytes.NewReader(payload), 8192)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Storage: store}
	checkImmutableRange(t, payload, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := server.serveManaged(w, r, file.Key); err != nil {
			t.Error(err)
		}
	})
}

func TestProviderRangesPreserveBytesThroughTransformingProxies(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte("console.log('immutable core');"), 128)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "core.js"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "installed", true: "development"}[override], func(t *testing.T) {
			client := &runtimeclient.Client{
				Providers: map[string]runtimeclient.Provider{"test": {ProviderID: "test", BundleSHA256: "bundle", Root: root}},
				Assets:    map[string]map[string]bool{"test": {"core.js": true}},
				Overrides: map[string]map[string][]byte{},
			}
			if override {
				client.Overrides["test"] = map[string][]byte{"core.js": payload}
			}
			server := &Server{Runtime: client}
			checkImmutableRange(t, payload, func(w http.ResponseWriter, r *http.Request) {
				r.SetPathValue("providerId", "test")
				r.SetPathValue("bundle", "bundle")
				r.SetPathValue("asset", "core.js")
				if err := server.providerAsset(w, r, model.Principal{}); err != nil {
					t.Error(err)
				}
			})
		})
	}
}

func checkImmutableRange(t *testing.T, payload []byte, handler http.HandlerFunc) {
	t.Helper()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r := httptest.NewRequestWithContext(t.Context(), method, "/resource", http.NoBody)
		r.Header.Set("Range", "bytes=1-2048")
		r.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		securityHeaders(handler).ServeHTTP(w, r)
		if w.Code != http.StatusPartialContent || w.Header().Get("Content-Length") != "2048" {
			t.Fatalf("status=%d length=%q", w.Code, w.Header().Get("Content-Length"))
		}
		if !strings.Contains(w.Header().Get("Cache-Control"), "no-transform") {
			t.Errorf("%s allows a proxy to compress a byte-addressed response: %q", method, w.Header().Get("Cache-Control"))
		}
		if method == http.MethodGet && !bytes.Equal(w.Body.Bytes(), payload[1:2049]) {
			t.Fatal("range bytes changed")
		}
		if method == http.MethodHead && w.Body.Len() != 0 {
			t.Fatal("HEAD returned a body")
		}
	}
}

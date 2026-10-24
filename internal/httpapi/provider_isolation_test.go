package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/model"
)

func TestProviderWorkerResponsesPreserveCrossOriginIsolation(t *testing.T) {
	s, bundle, revision := providerCacheTestServer(t)
	const worker = "self.postMessage('ready');"
	name := "runtime.worker.js"
	file := filepath.Join(s.Runtime.Providers["test"].Root, name)
	modified := time.Unix(1735689600, 0)
	if err := os.WriteFile(file, []byte(worker), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, modified, modified); err != nil {
		t.Fatal(err)
	}
	s.Runtime.Assets["test"][name] = true
	for _, asset := range []string{name, "dev/" + revision + "/" + name} {
		for _, test := range []struct {
			name, method, header, value, body string
			status                            int
		}{
			{"get", "GET", "", "", worker, http.StatusOK},
			{"head", "HEAD", "", "", "", http.StatusOK},
			{"range", "GET", "Range", "bytes=0-3", worker[:4], http.StatusPartialContent},
			{"not-modified", "GET", "If-Modified-Since", modified.UTC().Format(http.TimeFormat), "", http.StatusNotModified},
		} {
			t.Run(asset+"/"+test.name, func(t *testing.T) {
				r := httptest.NewRequestWithContext(t.Context(), test.method, "/runtime/providers/test/"+bundle+"/"+asset, nil)
				r.SetPathValue("providerId", "test")
				r.SetPathValue("bundle", bundle)
				r.SetPathValue("asset", asset)
				if test.header != "" {
					r.Header.Set(test.header, test.value)
				}
				w := httptest.NewRecorder()
				if err := s.providerAsset(w, r, model.Principal{}); err != nil {
					t.Fatal(err)
				}
				if w.Code != test.status || w.Body.String() != test.body {
					t.Fatalf("response: status=%d body=%q", w.Code, w.Body.String())
				}
				coep, corp := w.Header().Get("Cross-Origin-Embedder-Policy"), w.Header().Get("Cross-Origin-Resource-Policy")
				if coep != "require-corp" || corp != "same-origin" {
					t.Errorf("Worker isolation policies: COEP=%q CORP=%q", coep, corp)
				}
			})
		}
	}
}

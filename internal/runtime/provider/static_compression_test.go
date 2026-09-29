package runtimeprovider

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestStaticCompressionNegotiation(t *testing.T) {
	key := "fixture\x00" + strings.Repeat("a", 64) + "\x00assets/core.wasm"
	raw := staticFile{contents: []byte("raw wasm"), sizeBytes: 8, sha256: strings.Repeat("1", 64), mediaType: "application/wasm"}
	br := staticFile{contents: []byte("compressed"), sizeBytes: 10, sha256: strings.Repeat("2", 64), mediaType: "application/octet-stream"}
	handler := &staticHandler{files: map[string]staticFile{}, devFiles: map[string]staticFile{key: raw, key + ".br": br}}
	for _, tc := range []struct {
		name, accept, method, rangeHeader, encoding, body string
		status                                            int
	}{
		{"br", "gzip, br", http.MethodGet, "", "br", "compressed", 200},
		{"quality zero", "br;q=0, *;q=1", http.MethodGet, "", "", "raw wasm", 200},
		{"wildcard", "*;q=0.5", http.MethodGet, "", "br", "compressed", 200},
		{"head", "br", http.MethodHead, "", "br", "", 200},
		{"range stays identity", "br", http.MethodGet, "bytes=0-2", "", "raw", 206},
		{"identity", "gzip", http.MethodGet, "", "", "raw wasm", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tc.method, "/runtime/providers/fixture/"+strings.Repeat("a", 64)+"/assets/core.wasm", nil)
			req.Header.Set("Accept-Encoding", tc.accept)
			req.Header.Set("Range", tc.rangeHeader)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tc.status || res.Header().Get("Content-Encoding") != tc.encoding || res.Body.String() != tc.body || res.Header().Get("Vary") != "Accept-Encoding" {
				t.Fatalf("response %d %v %q", res.Code, res.Header(), res.Body.String())
			}
			if tc.encoding != "" && (res.Header().Get("Content-Length") != "10" || res.Header().Get("ETag") != `"`+br.sha256+`"` || res.Header().Get("Content-Type") != "application/wasm") {
				t.Fatalf("encoded headers: %v", res.Header())
			}
		})
	}
}

func TestStaticCompressionNeverMixesDevelopmentAndBase(t *testing.T) {
	key := "fixture\x00" + strings.Repeat("a", 64) + "\x00assets/core.wasm"
	handler := &staticHandler{files: map[string]staticFile{key + ".br": {}}, devFiles: map[string]staticFile{key: {contents: []byte("new"), sizeBytes: 3, mediaType: "application/wasm"}}}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/runtime/providers/fixture/"+strings.Repeat("a", 64)+"/assets/core.wasm", nil)
	req.Header.Set("Accept-Encoding", "br")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != 200 || res.Body.String() != "new" || res.Header().Get("Content-Encoding") != "" {
		t.Fatalf("mixed generations: %d %v %q", res.Code, res.Header(), res.Body.String())
	}
}

func TestMameCompressionUsesOnlyOriginalProviderAsset(t *testing.T) {
	key := "retrom-runtime\x00" + strings.Repeat("a", 64) + "\x00assets/mame/mame-common.wasm"
	raw := []byte(strings.Repeat("WASM payload", 1024))
	handler := &staticHandler{files: map[string]staticFile{}, devFiles: map[string]staticFile{
		key: {contents: raw, sizeBytes: int64(len(raw)), sha256: strings.Repeat("b", 64), mediaType: "application/wasm"},
	}}
	url := "/runtime/providers/retrom-runtime/" + strings.Repeat("a", 64) + "/assets/mame/mame-common.wasm"
	for _, tc := range []struct{ accept, encoding string }{{"br", "br"}, {"gzip", "gzip"}, {"br;q=0, gzip", "gzip"}, {"identity", ""}} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		req.Header.Set("Accept-Encoding", tc.accept)
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != http.StatusOK || res.Header().Get("Content-Encoding") != tc.encoding {
			t.Fatalf("%s response: %d %v", tc.accept, res.Code, res.Header())
		}
		var decoded []byte
		var err error
		switch tc.encoding {
		case "br":
			decoded, err = io.ReadAll(brotli.NewReader(res.Body))
		case "gzip":
			reader, openErr := gzip.NewReader(res.Body)
			if openErr != nil {
				t.Fatal(openErr)
			}
			decoded, err = io.ReadAll(reader)
		default:
			decoded = res.Body.Bytes()
		}
		if err != nil || !bytes.Equal(decoded, raw) {
			t.Fatalf("%s decoded body: %v", tc.accept, err)
		}
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	req.Header.Set("Accept-Encoding", "br")
	req.Header.Set("Range", "bytes=0-3")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusPartialContent || res.Header().Get("Content-Encoding") != "" || res.Body.String() != "WASM" {
		t.Fatalf("range: %d %v %q", res.Code, res.Header(), res.Body.String())
	}
}

func TestMameInstalledAssetCompressesWithoutSiblingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "mame-common.wasm")
	raw := []byte(strings.Repeat("WASM", 4096))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	key := "retrom-runtime\x00" + strings.Repeat("a", 64) + "\x00assets/mame/mame-common.wasm"
	handler := &staticHandler{files: map[string]staticFile{key: {
		path: path, sizeBytes: int64(len(raw)), sha256: strings.Repeat("b", 64), mediaType: "application/wasm",
	}}, devFiles: map[string]staticFile{}}
	url := "/runtime/providers/retrom-runtime/" + strings.Repeat("a", 64) + "/assets/mame/mame-common.wasm"
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	req.Header.Set("Accept-Encoding", "br")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	decoded, err := io.ReadAll(brotli.NewReader(res.Body))
	if err != nil || res.Code != http.StatusOK || !bytes.Equal(decoded, raw) || res.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("installed Brotli response: %d %v %v", res.Code, res.Header(), err)
	}
	etag := res.Header().Get("ETag")
	req = httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	req.Header.Set("Accept-Encoding", "br")
	req.Header.Set("If-None-Match", etag)
	res = httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNotModified || res.Body.Len() != 0 {
		t.Fatalf("conditional response: %d", res.Code)
	}
}

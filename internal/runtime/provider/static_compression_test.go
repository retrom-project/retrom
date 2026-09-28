package runtimeprovider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

//go:build integration

package httpapi

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"testing"
)

// Exercise the authorized CAS route, including its actual conditional handling.
func assertContentIOProtocol(t *testing.T, contentURL string, requestContent runtimeContentRequester) {
	t.Helper()
	full := requestContent(http.MethodGet, contentURL, nil)
	if full.Code != http.StatusOK || full.Body.Len() < 4 {
		t.Fatalf("content fixture status=%d bytes=%d", full.Code, full.Body.Len())
	}
	size := full.Body.Len()
	etag := fmt.Sprintf(`"sha256-%x"`, sha256.Sum256(full.Body.Bytes()))
	if full.Header().Get("ETag") != etag || full.Header().Get("Content-Length") != strconv.Itoa(size) {
		t.Fatalf("representation identity or length mismatch: %v", full.Header())
	}
	cases := []struct {
		name, method, interval, match string
		status                        int
		length, contentRange          string
	}{
		{name: "range", method: http.MethodGet, interval: "bytes=0-3", match: etag, status: 206, length: "4", contentRange: fmt.Sprintf("bytes 0-3/%d", size)},
		{name: "head", method: http.MethodHead, match: etag, status: 200, length: strconv.Itoa(size)},
		{name: "stale identity", method: http.MethodGet, interval: "bytes=0-3", match: `"different"`, status: 412},
		{name: "unsatisfiable", method: http.MethodGet, interval: fmt.Sprintf("bytes=%d-", size), match: etag, status: 416, contentRange: fmt.Sprintf("bytes */%d", size)},
		{name: "multiple ranges", method: http.MethodGet, interval: "bytes=0-1,3-3", match: etag, status: 416},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			got := requestContent(item.method, contentURL, func(request *http.Request) {
				request.Header.Set("Range", item.interval)
				request.Header.Set("If-Match", item.match)
				request.Header.Set("Accept-Encoding", "gzip")
			})
			if got.Code != item.status {
				t.Fatalf("status=%d want=%d", got.Code, item.status)
			}
			if got.Header().Get("Content-Encoding") != "" && got.Header().Get("Content-Encoding") != "identity" {
				t.Fatal("encoded immutable body")
			}
			if item.length != "" && got.Header().Get("Content-Length") != item.length {
				t.Fatalf("length=%s want=%s", got.Header().Get("Content-Length"), item.length)
			}
			if item.contentRange != "" && got.Header().Get("Content-Range") != item.contentRange {
				t.Fatalf("range=%s want=%s", got.Header().Get("Content-Range"), item.contentRange)
			}
			if item.method == http.MethodHead && got.Body.Len() != 0 {
				t.Fatal("HEAD body")
			}
			if item.status == 206 && got.Body.String() != full.Body.String()[:4] {
				t.Fatal("range body")
			}
		})
	}
}

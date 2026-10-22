package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"retrom/internal/service/runs"
	"retrom/internal/storage"

	"github.com/google/uuid"
)

func TestMemberHTTPProtocolAndConcurrentRanges(t *testing.T) {
	t.Parallel()
	server, payload, blob := memberHTTPFixture(t)
	t.Run("protocol", func(t *testing.T) { assertMemberProtocol(t, server, payload, blob) })
	t.Run("unsatisfied", func(t *testing.T) { assertMemberUnsatisfied(t, server, payload) })
	t.Run("multipart", func(t *testing.T) { assertMemberMultipart(t, server, payload) })
	t.Run("concurrent", func(t *testing.T) { assertMemberConcurrent(t, server, payload) })
}

func memberHTTPFixture(t *testing.T) (*httptest.Server, []byte, runs.Blob) {
	t.Helper()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	payload := bytes.Repeat([]byte("logical-member-"), 160000)
	object := append([]byte("adjacent-before"), payload...)
	object = append(object, []byte("adjacent-after")...)
	file, err := store.Write(t.Context(), "games", uuid.NewString(), bytes.NewReader(object), int64(len(object)))
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	blob := runs.Blob{Key: file.Key, Offset: int64(len("adjacent-before")), SizeBytes: int64(len(payload)), SHA256: hex.EncodeToString(hash[:]), MediaType: "application/octet-stream"}
	api := Server{Storage: store}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveErr := api.serveRunBlob(w, r, blob); serveErr != nil {
			t.Error(serveErr)
		}
	}))
	t.Cleanup(server.Close)
	return server, payload, blob
}

func assertMemberProtocol(t *testing.T, server *httptest.Server, payload []byte, blob runs.Blob) {
	t.Helper()
	etag := "\"sha256-" + blob.SHA256 + "\""
	cases := []struct {
		method, span, condition, value string
		status                         int
		expected                       []byte
	}{
		{"GET", "", "", "", 200, payload},
		{"HEAD", "", "", "", 200, nil},
		{"GET", "bytes=0-0", "", "", 206, payload[:1]},
		{"GET", "bytes=-7", "", "", 206, payload[len(payload)-7:]},
		{"GET", fmt.Sprintf("bytes=%d-", len(payload)-1), "", "", 206, payload[len(payload)-1:]},
		{"GET", fmt.Sprintf("bytes=%d-99999999", len(payload)-2), "", "", 206, payload[len(payload)-2:]},
		{"HEAD", "bytes=0-3", "If-Match", etag, 206, nil},
		{"GET", "bytes=0-3", "If-Match", "\"wrong\"", 412, nil},
		{"GET", "bytes=0-3", "If-None-Match", etag, 304, nil},
		{"GET", "bytes=0-3", "If-Range", etag, 206, payload[:4]},
		{"GET", "bytes=0-3", "If-Range", "\"wrong\"", 200, payload},
	}
	for _, c := range cases {
		t.Run(c.method+c.span+c.condition+c.value, func(t *testing.T) {
			request, requestErr := http.NewRequestWithContext(t.Context(), c.method, server.URL, http.NoBody)
			if requestErr != nil {
				t.Fatal(requestErr)
			}
			request.Header.Set("Range", c.span)
			if c.condition != "" {
				request.Header.Set(c.condition, c.value)
			}
			response, requestErr := server.Client().Do(request)
			if requestErr != nil {
				t.Fatal(requestErr)
			}
			defer closeFile(response.Body)
			body, readErr := io.ReadAll(response.Body)
			if readErr != nil || response.StatusCode != c.status || !bytes.Equal(body, c.expected) {
				t.Fatalf("status=%d size=%d err=%v", response.StatusCode, len(body), readErr)
			}
			if response.Header.Get("ETag") != etag || !strings.Contains(response.Header.Get("Cache-Control"), "no-transform") {
				t.Fatal("member representation headers changed")
			}
		})
	}
}

func assertMemberUnsatisfied(t *testing.T, server *httptest.Server, payload []byte) {
	t.Helper()
	request, _ := http.NewRequestWithContext(t.Context(), "GET", server.URL, http.NoBody)
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-", len(payload)))
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 416 || response.Header.Get("Content-Range") != fmt.Sprintf("bytes */%d", len(payload)) {
		t.Fatal("wrong logical unsatisfied range")
	}
	defer closeFile(response.Body)
}

func assertMemberMultipart(t *testing.T, server *httptest.Server, payload []byte) {
	t.Helper()
	request, _ := http.NewRequestWithContext(t.Context(), "GET", server.URL, http.NoBody)
	request.Header.Set("Range", "bytes=0-1,100-103")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFile(response.Body)
	_, parameters, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(response.Body, parameters["boundary"])
	for _, expected := range [][]byte{payload[:2], payload[100:104]} {
		part, partErr := reader.NextPart()
		if partErr != nil {
			t.Fatal(partErr)
		}
		body, readErr := io.ReadAll(part)
		if readErr != nil || !bytes.Equal(body, expected) {
			t.Fatal("multipart leaked adjacent bytes")
		}
	}
}

func assertMemberConcurrent(t *testing.T, server *httptest.Server, payload []byte) {
	t.Helper()
	var workers sync.WaitGroup
	for i := range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := range 16 {
				start := (i*16511 + j*7919) % (len(payload) - 8192)
				r, requestErr := http.NewRequestWithContext(t.Context(), "GET", server.URL, http.NoBody)
				if requestErr != nil {
					t.Error(requestErr)
					return
				}
				r.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, start+8191))
				res, requestErr := server.Client().Do(r)
				if requestErr != nil {
					t.Error(requestErr)
					return
				}
				body, readErr := io.ReadAll(res.Body)
				if closeErr := res.Body.Close(); closeErr != nil {
					t.Error(closeErr)
				}
				if readErr != nil || !bytes.Equal(body, payload[start:start+8192]) {
					t.Error("concurrent cursor crossed member")
					return
				}
			}
		}()
	}
	workers.Wait()
}

//go:build integration

package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"retrom/internal/libraryimport"
	"retrom/internal/testsupport"
)

func TestOrdinaryRGSSReviewServesDeclaredArchiveThroughAuthenticatedHTTP(t *testing.T) {
	t.Parallel()
	for _, generation := range []string{"rpgxp", "rpgvx", "rpgvxace"} {
		t.Run(generation, func(t *testing.T) {
			t.Parallel()
			server, itemID := newProjectArchiveReviewHTTPFixture(t, generation)
			preview, launchCookie := createCheckpointPreviewHTTP(t, server, itemID, nil)
			configuration := requestReviewCheckpointHTTP(t, server, preview.PreviewID, launchCookie, "GET", "config", nil, "")
			var envelope map[string]any
			decoder := json.NewDecoder(bytes.NewReader(configuration.Body.Bytes()))
			decoder.UseNumber()
			if configuration.Code != http.StatusOK || decoder.Decode(&envelope) != nil {
				t.Fatalf("archive preview config = %d %s", configuration.Code, configuration.Body.String())
			}
			resource := testsupport.RuntimeEnvelopeResource(t, envelope, "game")
			archiveURL, ok := resource["url"].(string)
			if !ok || resource["kind"] != "SEEKABLE_BLOB" || !strings.HasSuffix(archiveURL, "/game.mkxpz") {
				t.Fatalf("declared archive = %#v", resource)
			}
			var contentCookie *http.Cookie
			for _, cookie := range configuration.Result().Cookies() {
				if cookie.Name == runtimeContentGrantPrefix+preview.PreviewID {
					contentCookie = cookie
				}
			}
			if contentCookie == nil {
				t.Fatal("configuration did not grant project content access")
			}
			assertReviewArchiveHTTP(t, server, archiveURL, contentCookie, resource)
			wrong := *contentCookie
			wrong.Value = "invalid-capability"
			for _, supplied := range []*http.Cookie{nil, &wrong} {
				for _, method := range []string{"GET", "HEAD"} {
					response := requestReviewArchiveHTTP(t, server, archiveURL, method, supplied, "")
					if response.Code != http.StatusUnauthorized {
						t.Fatalf("unowned archive %s = %d", method, response.Code)
					}
				}
			}
			finished := requestReviewCheckpointHTTP(t, server, preview.PreviewID, launchCookie, "POST", "finish",
				strings.NewReader(`{"clientSequence":0,"clientObservedAtMs":1,"previousInterval":null}`), "application/json")
			if finished.Code != http.StatusOK {
				t.Fatalf("finish preview = %d %s", finished.Code, finished.Body.String())
			}
			if response := requestReviewArchiveHTTP(t, server, archiveURL, "HEAD", contentCookie, ""); response.Code != http.StatusUnauthorized {
				t.Fatalf("closed preview archive = %d", response.Code)
			}
		})
	}
}

func newProjectArchiveReviewHTTPFixture(t *testing.T, generation string) (*Server, string) {
	t.Helper()
	server := newTestServer(t)
	if err := server.dependencies.Bootstrap(t.Context(), server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	uploadID := completeRPGMakerHTTPUpload(t, t.Context(), server, rpgMakerHTTPFixture(t, generation))
	created, err := server.importer.Create(t.Context(), libraryimport.CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, server.database, "rpgmaker/rpgmaker"),
		MetadataProvider: "NONE", ContentMode: "RPG_MAKER_PROJECT",
	})
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := server.database.QueryRowContext(t.Context(), `SELECT id FROM import_items WHERE import_job_id=?`,
		created.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	return server, itemID
}

func assertReviewArchiveHTTP(t *testing.T, server *Server, archiveURL string, cookie *http.Cookie, resource map[string]any) {
	t.Helper()
	head := requestReviewArchiveHTTP(t, server, archiveURL, "HEAD", cookie, "")
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != fmt.Sprint(resource["sizeBytes"]) ||
		head.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("declared archive HEAD = %d %s headers=%v", head.Code, head.Body.String(), head.Header())
	}
	partial := requestReviewArchiveHTTP(t, server, archiveURL, "GET", cookie, "bytes=0-3")
	if partial.Code != http.StatusPartialContent || partial.Body.String() != "PK\x03\x04" ||
		partial.Header().Get("Content-Range") != fmt.Sprintf("bytes 0-3/%v", resource["sizeBytes"]) {
		t.Fatalf("declared archive Range = %d %q headers=%v", partial.Code, partial.Body.String(), partial.Header())
	}
	whole := requestReviewArchiveHTTP(t, server, archiveURL, "GET", cookie, "")
	digest := sha256.Sum256(whole.Body.Bytes())
	if whole.Code != http.StatusOK || hex.EncodeToString(digest[:]) != resource["sha256"] {
		t.Fatalf("declared archive bytes mismatch: status=%d digest=%x", whole.Code, digest)
	}
}

func requestReviewArchiveHTTP(t *testing.T, server *Server, archiveURL, method string, cookie *http.Cookie, byteRange string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, archiveURL, nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

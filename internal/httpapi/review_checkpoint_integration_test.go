//go:build integration

package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/launch"
	"retrom/internal/libraryimport"
	"retrom/internal/testsupport"
)

func TestOrdinaryReviewCheckpointHTTPUsesPreviewCookieThroughSaveAccess(t *testing.T) {
	t.Parallel()
	server, itemID := newCheckpointReviewHTTPFixture(t)
	preview, cookie := createCheckpointPreviewHTTP(t, server, itemID, nil)
	status := requestReviewCheckpointHTTP(t, server, preview.PreviewID, cookie, "GET", "checkpoint-status", nil, "")
	if status.Code != http.StatusOK {
		t.Fatalf("preview checkpoint status = %d %s", status.Code, status.Body.String())
	}
	var declaration struct {
		CheckpointFormat string `json:"checkpointFormat"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &declaration); err != nil || declaration.CheckpointFormat == "" {
		t.Fatalf("checkpoint declaration: %v", err)
	}
	for _, value := range []string{"point-A", "point-B"} {
		body, contentType := checkpointHTTPForm(t, declaration.CheckpointFormat, value)
		saved := requestReviewCheckpointHTTP(t, server, preview.PreviewID, cookie, "POST", "save-states", body, contentType)
		var receipt map[string]any
		if saved.Code != http.StatusCreated || json.Unmarshal(saved.Body.Bytes(), &receipt) != nil ||
			len(receipt) != 4 || receipt["resourceKind"] != "REVIEW_PREVIEW_CHECKPOINT" || receipt["previewId"] != preview.PreviewID {
			t.Fatalf("ordinary preview save = %d %s", saved.Code, saved.Body.String())
		}
	}
	restored, restoreCookie := createCheckpointPreviewHTTP(t, server, itemID, &preview.PreviewID)
	state := requestReviewCheckpointHTTP(t, server, restored.PreviewID, restoreCookie, "GET", "state", nil, "")
	if state.Code != http.StatusOK || state.Body.String() != "point-B" {
		t.Fatalf("ordinary preview restore = %d %s", state.Code, state.Body.String())
	}
	assertReviewCheckpointHTTPAuthorization(t, server, preview, cookie, restoreCookie)
	var products int
	if err := server.database.QueryRowContext(t.Context(), `
SELECT (SELECT count(*) FROM games)+(SELECT count(*) FROM launch_sessions)+
 (SELECT count(*) FROM play_sessions)+(SELECT count(*) FROM save_states)`).Scan(&products); err != nil || products != 0 {
		t.Fatalf("ordinary preview created product records: %d %v", products, err)
	}
}

func newCheckpointReviewHTTPFixture(t *testing.T) (*Server, string) {
	t.Helper()
	server := newTestServer(t)
	if err := server.dependencies.Bootstrap(t.Context(), server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	uploadID := completeMultiDiscHTTPUpload(t, server, "FILES", []multiDiscHTTPFile{
		{path: "review.gba", contents: []byte("deterministic review HTTP fixture")},
	})
	created, err := server.importer.Create(t.Context(), libraryimport.CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, server.database, "gba/mgba"),
		MetadataProvider: "NONE",
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

func createCheckpointPreviewHTTP(t *testing.T, server *Server, itemID string, restoreFrom *string) (launch.ReviewPreviewCreated, *http.Cookie) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"clientCapabilities": launch.Capabilities{}, "restoreFromPreviewId": restoreFrom})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v1/admin/reviews/"+itemID+"/previews", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", uuid.NewString())
	cookie, csrf := testSessionCredentials()
	setCSRFCredentials(request, cookie, csrf)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	var preview launch.ReviewPreviewCreated
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &preview) != nil || preview.PreviewID == "" {
		t.Fatalf("create ordinary HTTP preview = %d %s", response.Code, response.Body.String())
	}
	for _, launchCookie := range (&http.Response{Header: response.Header()}).Cookies() {
		if launchCookie.Name != "retrom_launch_"+preview.PreviewID {
			continue
		}
		configuration := requestReviewCheckpointHTTP(t, server, preview.PreviewID, launchCookie, "GET", "config", nil, "")
		if configuration.Code != http.StatusOK {
			t.Fatalf("preview config = %d %s", configuration.Code, configuration.Body.String())
		}
		return preview, launchCookie
	}
	t.Fatal("ordinary preview did not issue its scoped launch cookie")
	return launch.ReviewPreviewCreated{}, nil
}

func checkpointHTTPForm(t *testing.T, format, payload string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadata, err := json.Marshal(map[string]string{"checkpointFormat": format, "name": "Review checkpoint"})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("metadata", string(metadata)); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("payload", "payload.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, strings.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}

func requestReviewCheckpointHTTP(t *testing.T, server *Server, previewID string, cookie *http.Cookie, method, endpoint string,
	body io.Reader, contentType string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), method, "/runtime/launches/"+previewID+"/"+endpoint, body)
	request.Header.Set("Origin", "http://localhost:3000")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Idempotency-Key", uuid.NewString())
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func assertReviewCheckpointHTTPAuthorization(t *testing.T, server *Server, preview launch.ReviewPreviewCreated,
	cookie, otherCookie *http.Cookie,
) {
	t.Helper()
	wrong := *otherCookie
	wrong.Name = cookie.Name
	for _, endpoint := range []string{"checkpoint-status", "save-states", "state"} {
		method := "GET"
		if endpoint == "save-states" {
			method = "POST"
		}
		for _, supplied := range []*http.Cookie{nil, &wrong} {
			response := requestReviewCheckpointHTTP(t, server, preview.PreviewID, supplied, method, endpoint, nil, "")
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("unowned %s = %d %s", endpoint, response.Code, response.Body.String())
			}
		}
	}
	finished := requestReviewCheckpointHTTP(t, server, preview.PreviewID, cookie, "POST", "finish",
		strings.NewReader(`{"clientSequence":0,"clientObservedAtMs":1,"previousInterval":null}`), "application/json")
	if finished.Code != http.StatusOK {
		t.Fatalf("finish preview = %d %s", finished.Code, finished.Body.String())
	}
	response := requestReviewCheckpointHTTP(t, server, preview.PreviewID, cookie, "GET", "checkpoint-status", nil, "")
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("closed preview = %d %s", response.Code, response.Body.String())
	}
}

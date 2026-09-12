//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"retrom/internal/composition"
	"retrom/internal/persistence/blobcatalog"
	"retrom/internal/testsupport"
)

func TestReviewCoverSQLFailuresRemainServerErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, query string }{
		{"source read", "FROM upload_files"}, {"draft authority read", "FROM review_drafts"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := newTestServer(t)
			itemID := createReviewSnapshotItem(t, server)
			fileID := createReviewCoverUpload(t, server)
			cause := errors.New("review cover database unavailable")
			hits := 0
			faultDB := testsupport.OpenSQLFaultDatabase(t, server.database, testsupport.SQLFaultHooks{
				BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
					if strings.Contains(query, test.query) {
						hits++
						return cause
					}
					return nil
				},
			})
			server.reviewCoverUploads = composition.NewLibraryReviewCoverUploads(faultDB, server.blobs, server.now)
			response := requestReviewCover(t, server, itemID, fileID, `"v1"`)
			if response.Code != http.StatusInternalServerError || hits != 1 {
				t.Fatalf("SQL failure mapped as domain rejection: status=%d hits=%d body=%s", response.Code, hits, response.Body.String())
			}
		})
	}
}

func requestReviewCover(t *testing.T, server *Server, itemID, fileID, version string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/reviews/"+itemID+"/assets",
		strings.NewReader(`{"uploadFileId":"`+fileID+`","kind":"COVER"}`))
	request.SetPathValue("importItemId", itemID)
	request.Header.Set("If-Match", version)
	request.Header.Set("Idempotency-Key", "01980000-0000-7000-8000-000000008601")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.createReviewAsset(response, request)
	return response
}

func createReviewCoverUpload(t *testing.T, server *Server) string {
	t.Helper()
	var contents bytes.Buffer
	if err := png.Encode(&contents, image.NewNRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	blob, err := server.blobs.Put(bytes.NewReader(contents.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := blobcatalog.EnsureRecord(t.Context(), server.database, blob, "image/png", 0)
	if err != nil {
		t.Fatal(err)
	}
	const uploadID = "01980000-0000-7000-8000-000000008602"
	const fileID = "01980000-0000-7000-8000-000000008603"
	if _, err := server.database.ExecContext(t.Context(), `
INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'COMPLETE','FILES',1,?,?,9999999999999,0,0)`, uploadID, blob.Size, blob.SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := server.database.ExecContext(t.Context(), `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms)
VALUES(?,?,'review-cover.png',?,?,?,'COMPLETE',0,0)`, fileID, uploadID, blob.Size, blob.Size, blobID); err != nil {
		t.Fatal(err)
	}
	return fileID
}

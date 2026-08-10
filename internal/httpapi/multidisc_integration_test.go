//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/libraryimport"
	"retrom/internal/uploads"
)

type multiDiscHTTPFile struct {
	path     string
	contents []byte
}

func completeMultiDiscHTTPUpload(
	t *testing.T,
	server *Server,
	sourceType string,
	files []multiDiscHTTPFile,
) string {
	t.Helper()
	declarations := make([]uploads.FileDeclaration, 0, len(files))
	for index, file := range files {
		declarations = append(declarations, uploads.FileDeclaration{
			ClientFileID: fmt.Sprintf("disc-%d", index), RelativePath: file.path,
			SizeBytes: int64(len(file.contents)),
		})
	}
	ctx := context.Background()
	upload, err := server.uploads.Create(ctx, uploads.CreateRequest{SourceType: sourceType, Files: declarations})
	if err != nil {
		t.Fatal(err)
	}
	for index, file := range files {
		digest := sha256.Sum256(file.contents)
		if err := server.uploads.PutPart(
			ctx, upload.ID, upload.Files[index].ID, 0,
			fmt.Sprintf("bytes 0-%d/%d", len(file.contents)-1, len(file.contents)),
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
			bytes.NewReader(file.contents),
		); err != nil {
			t.Fatal(err)
		}
	}
	current, err := server.uploads.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := server.uploads.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitForHTTPJob(t, server.database, jobID, "SUCCEEDED")
	return upload.ID
}

func seedMultiDiscHTTPBIOS(t *testing.T, server *Server) {
	t.Helper()
	ctx := context.Background()
	metadata, err := server.blobs.Put(bytes.NewReader([]byte("deterministic HTTP Saturn BIOS fixture")))
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := blobstore.EnsureRecord(ctx, server.database, metadata, "application/octet-stream", time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	var requirementID string
	var requirementVersion int64
	if err := server.database.QueryRowContext(ctx, `
SELECT id,version FROM bios_requirements
WHERE core_id='yabause' AND logical_name='saturn_bios.bin' AND enabled=1
`).Scan(&requirementID, &requirementVersion); err != nil {
		t.Fatal(err)
	}
	installationID := uuid.NewString()
	if _, err := server.database.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,'HASH_WARNING','{}',1,1,?,?)
`, installationID, requirementID, blobID, "saturn_bios.bin", metadata.Size, metadata.MD5, metadata.SHA1,
		metadata.SHA256, requirementVersion, time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

func multiDiscHTTPCHD(value string) []byte {
	return append([]byte("MComprHD"), []byte(value)...)
}

func TestMultiDiscAttachmentHTTPContractAndReviewProjection(t *testing.T) {
	server := newTestServer(t)
	ctx := context.Background()
	if err := server.dependencies.Bootstrap(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := server.dependencies.BootstrapCatalogs(ctx, server.database, time.Now()); err != nil {
		t.Fatal(err)
	}
	server.importer.WithMultiDiscImportEnabled(true)
	seedMultiDiscHTTPBIOS(t, server)
	baseUploadID := completeMultiDiscHTTPUpload(t, server, "DIRECTORY", []multiDiscHTTPFile{
		{path: "game/game.m3u", contents: []byte("one.chd\ntwo.chd\nthree.chd\n")},
		{path: "game/one.chd", contents: multiDiscHTTPCHD("one")},
		{path: "game/two.chd", contents: multiDiscHTTPCHD("two")},
	})
	createdImport, err := server.importer.Create(ctx, libraryimport.CreateRequest{
		UploadID: baseUploadID, TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000020",
		MetadataProvider: "NONE", ContentMode: "MULTI_DISC_M3U_V1",
	})
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := server.database.QueryRowContext(
		ctx, `SELECT id FROM import_items WHERE import_job_id=?`, createdImport.ImportJobID,
	).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	attachmentUploadID := completeMultiDiscHTTPUpload(t, server, "FILES", []multiDiscHTTPFile{
		{path: "three.chd", contents: multiDiscHTTPCHD("three")},
	})
	handler, cookie, csrf := httpSession(t, server)
	key := uuid.NewString()
	send := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(
			http.MethodPost, "/api/v1/admin/reviews/"+itemID+"/multi-disc-attachments",
			strings.NewReader(fmt.Sprintf(`{"uploadId":%q}`, attachmentUploadID)),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("If-Match", `"v1"`)
		request.Header.Set("Idempotency-Key", key)
		setCSRFCredentials(request, cookie, csrf)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	first := send()
	var attachment struct {
		AttachmentID  string `json:"attachmentId"`
		JobID         string `json:"jobId"`
		State         string `json:"state"`
		ReviewVersion int64  `json:"reviewVersion"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &attachment); err != nil || first.Code != http.StatusAccepted ||
		attachment.AttachmentID == "" || attachment.JobID == "" || attachment.State != "QUEUED" ||
		attachment.ReviewVersion != 2 || first.Header().Get("ETag") != `"v2"` ||
		first.Header().Get("Location") != "/api/v1/admin/jobs/"+attachment.JobID {
		t.Fatalf("attachment create = %d %s, headers=%v, error=%v", first.Code, first.Body.String(), first.Header(), err)
	}
	replay := send()
	if replay.Code != http.StatusAccepted || replay.Body.String() != first.Body.String() ||
		replay.Header().Get("X-Retrom-Idempotent-Replay") != "true" || replay.Header().Get("ETag") != `"v2"` {
		t.Fatalf("attachment replay = %d %s, headers=%v", replay.Code, replay.Body.String(), replay.Header())
	}
	waitForHTTPJob(t, server.database, attachment.JobID, "SUCCEEDED")
	reviewRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	reviewRequest.AddCookie(cookie)
	review := httptest.NewRecorder()
	handler.ServeHTTP(review, reviewRequest)
	var reviewProjection struct {
		CanApprove bool            `json:"canApprove"`
		MultiDisc  json.RawMessage `json:"multiDisc"`
	}
	if err := json.Unmarshal(review.Body.Bytes(), &reviewProjection); err != nil || review.Code != http.StatusOK ||
		!reviewProjection.CanApprove || !bytes.Contains(reviewProjection.MultiDisc, []byte(`"missingDiscCount":0`)) ||
		bytes.Contains(reviewProjection.MultiDisc, []byte(`"blobId"`)) {
		t.Fatalf("accepted review = %d %s", review.Code, review.Body.String())
	}
	if _, err := server.database.ExecContext(ctx, `
UPDATE core_artifacts
SET compatibility_config_json=json_set(compatibility_config_json,'$.multiDisc.maxDiscs',7)
WHERE core_id='yabause' AND enabled=1
`); err != nil {
		t.Fatal(err)
	}
	staleRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	staleRequest.AddCookie(cookie)
	stale := httptest.NewRecorder()
	handler.ServeHTTP(stale, staleRequest)
	if stale.Code != http.StatusOK || !strings.Contains(stale.Body.String(), `"validationStale":true`) ||
		!strings.Contains(stale.Body.String(), `"canApprove":false`) {
		t.Fatalf("compatibility-stale review = %d %s", stale.Code, stale.Body.String())
	}
}

//go:build integration

package libraryimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestCreateRPGMakerMVArchiveReachesReviewPending(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	archive := rpgMakerMVArchiveWithMToolSidecar(t)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{
		Purpose: "RPG_MAKER_PROJECT", SourceType: "FILES",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "rpg-mv", RelativePath: "fixture.zip", SizeBytes: int64(len(archive)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	if err := uploadService.PutPart(
		ctx, upload.ID, upload.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(archive)-1, len(archive)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
		bytes.NewReader(archive),
	); err != nil {
		t.Fatal(err)
	}
	current, err := uploadService.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := uploadService.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitForRPGUploadFinalization(t, ctx, database.SQL, jobID)

	created, err := New(database.SQL, time.Now).WithBlobStore(blobs).Create(ctx, CreateRequest{
		UploadID: upload.ID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(
			t, database.SQL, "rpgmaker/rpgmaker",
		),
		MetadataProvider: "HASHEOUS", ContentMode: "RPG_MAKER_PROJECT_V1", TagIDs: []string{},
	})
	if err != nil {
		t.Fatalf("Create(RPG Maker MV) error = %v", err)
	}
	if created.ItemCount != 1 || created.State != "REVIEW_PENDING" {
		t.Fatalf("Create(RPG Maker MV) = %#v", created)
	}
	var state, code, title, metadataProvider string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT item.state,validation.compatibility_code,json_extract(draft.metadata_json,'$.title'),job.metadata_provider
FROM import_items item
JOIN import_jobs job ON job.id=item.import_job_id
JOIN import_item_core_validations validation ON validation.import_item_id=item.id
JOIN review_drafts draft ON draft.import_item_id=item.id
WHERE item.import_job_id=?
`, created.ImportJobID).Scan(&state, &code, &title, &metadataProvider); err != nil {
		t.Fatal(err)
	}
	if state != "REVIEW_PENDING" || code != "RPG_RUNTIME_VALIDATION_REQUIRED" || title != "fixture" ||
		metadataProvider != "NONE" {
		t.Fatalf("RPG review state/code/title/provider = %s/%s/%q/%s", state, code, title, metadataProvider)
	}
	var defaultCoreID, selectedCoreID, generation, routeKey string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT instance.default_core_id,profile.selected_core_id,profile.generation,profile.route_key
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
JOIN rpgmaker_review_profiles profile ON profile.review_draft_id=draft.id
WHERE item.import_job_id=?
`, created.ImportJobID).Scan(&defaultCoreID, &selectedCoreID, &generation, &routeKey); err != nil {
		t.Fatal(err)
	}
	if defaultCoreID != "rpgmaker" || selectedCoreID != "rpgmaker_mv" || generation != "RPGMV" ||
		routeKey != "RPGMV_NATIVE" {
		t.Fatalf("virtual binding = %s/%s/%s/%s", defaultCoreID, selectedCoreID, generation, routeKey)
	}
	var role, nestedSHA, nestedBlobID string
	var nestedOrdinal int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT file.role,blob.sha256,file.blob_id,file.source_archive_entry_ordinal
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN import_item_source_snapshot_files file ON file.source_snapshot_id=draft.effective_source_snapshot_id
JOIN blobs blob ON blob.id=file.blob_id
WHERE item.import_job_id=? AND file.logical_name='audio/bgm/config'
`, created.ImportJobID).Scan(&role, &nestedSHA, &nestedBlobID, &nestedOrdinal); err != nil {
		t.Fatal(err)
	}
	nestedBody := []byte("7z\xbc\xaf\x27\x1c encrypted MTool sidecar")
	wantNestedSHA := sha256.Sum256(nestedBody)
	var recursivelyIndexed int
	if err := database.SQL.QueryRowContext(
		ctx, "SELECT COUNT(*) FROM archive_entries WHERE archive_blob_id=?", nestedBlobID,
	).Scan(&recursivelyIndexed); err != nil {
		t.Fatal(err)
	}
	if role != "PROJECT_FILE" || nestedOrdinal < 0 || nestedSHA != fmt.Sprintf("%x", wantNestedSHA[:]) ||
		recursivelyIndexed != 0 {
		t.Fatalf(
			"opaque nested file role=%s ordinal=%d sha=%s recursivelyIndexed=%d",
			role, nestedOrdinal, nestedSHA, recursivelyIndexed,
		)
	}
}

func waitForRPGUploadFinalization(t *testing.T, ctx context.Context, database *sql.DB, jobID string) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		if err := database.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("upload finalization state = %s", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

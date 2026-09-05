//go:build integration

package libraryimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
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
		Purpose: "PROJECT", SourceType: "FILES",
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
		MetadataProvider: "HASHEOUS", ContentMode: "RPG_MAKER_PROJECT", TagIDs: []string{},
	})
	if err != nil {
		t.Fatalf("Create(RPG Maker MV) error = %v", err)
	}
	if created.ItemCount != 1 || created.State != "REVIEW_PENDING" {
		t.Fatalf("Create(RPG Maker MV) = %#v", created)
	}
	noiseDigest := sha256.Sum256([]byte("packaging noise"))
	if _, err := os.Stat(blobs.Path(fmt.Sprintf("%x", noiseDigest))); !os.IsNotExist(err) {
		t.Fatalf("excluded packaging noise reached CAS: %v", err)
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
	if state != "REVIEW_PENDING" || code != "READY" || title != "fixture" ||
		metadataProvider != "NONE" {
		t.Fatalf("RPG review state/code/title/provider = %s/%s/%q/%s", state, code, title, metadataProvider)
	}
	var defaultCoreID, providerID, targetID, generation string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT instance.default_core_id,profile.provider_id,profile.target_id,profile.generation
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN platform_instances instance ON instance.id=draft.target_platform_instance_id
JOIN rpgmaker_review_profiles profile ON profile.review_draft_id=draft.id
WHERE item.import_job_id=?
`, created.ImportJobID).Scan(&defaultCoreID, &providerID, &targetID, &generation); err != nil {
		t.Fatal(err)
	}
	if defaultCoreID != "rpgmaker" || providerID != "retrom-runtime" || targetID != "rpgmaker-mv" ||
		generation != "RPGMV" {
		t.Fatalf("virtual binding = %s/%s/%s/%s", defaultCoreID, providerID, targetID, generation)
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

	var itemID, validationID string
	var draftVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT item.id,draft.version,profile.provider_id,profile.target_id,
 (SELECT validation.id FROM import_item_core_validations validation
  WHERE validation.import_item_id=item.id ORDER BY validation.created_at_ms DESC,validation.id DESC LIMIT 1)
FROM import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
JOIN rpgmaker_review_profiles profile ON profile.review_draft_id=draft.id
WHERE item.import_job_id=?
`, created.ImportJobID).Scan(&itemID, &draftVersion, &providerID, &targetID, &validationID); err != nil {
		t.Fatal(err)
	}
	replacementBundle := fmt.Sprintf("%064x", 42)
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE runtime_providers SET provider_version='1.1.0',bundle_sha256=?,activated_at_ms=activated_at_ms+1
WHERE provider_id='retrom-runtime'
`, replacementBundle); err != nil {
		t.Fatal(err)
	}
	validationCurrent, err := New(database.SQL, time.Now).ReviewValidationCurrent(ctx, validationID)
	if err != nil || !validationCurrent {
		t.Fatalf("review validation after provider bundle upgrade = %t, error=%v", validationCurrent, err)
	}
	var reboundProvider, reboundTarget string
	var reboundVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT draft.version,profile.provider_id,profile.target_id
FROM review_drafts draft
JOIN rpgmaker_review_profiles profile ON profile.review_draft_id=draft.id
WHERE draft.import_item_id=?
`, itemID).Scan(&reboundVersion, &reboundProvider, &reboundTarget); err != nil {
		t.Fatal(err)
	}
	if reboundVersion != draftVersion || reboundProvider != providerID || reboundTarget != targetID {
		t.Fatalf(
			"stable runtime route version=%d route=%s/%s want=%d %s/%s",
			reboundVersion, reboundProvider, reboundTarget, draftVersion, providerID, targetID,
		)
	}
	var validationCount int
	if err := database.SQL.QueryRowContext(ctx, `
	SELECT COUNT(*) FROM import_item_core_validations WHERE import_item_id=?
`, itemID).Scan(&validationCount); err != nil {
		t.Fatal(err)
	}
	if validationCount != 1 {
		t.Fatalf("provider bundle upgrade created redundant review validations: %d", validationCount)
	}
	approved, err := New(database.SQL, time.Now).Approve(ctx, itemID, draftVersion)
	if err != nil || approved.GameID == "" {
		t.Fatalf("READY RPG review must approve without a runtime proof session: %+v %v", approved, err)
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

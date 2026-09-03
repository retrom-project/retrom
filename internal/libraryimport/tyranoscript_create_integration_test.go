//go:build integration

package libraryimport

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
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

func TestCreateTyranoScriptArchiveReachesTrialRequiredReview(t *testing.T) {
	testCreateTyranoScriptInputReachesTrialRequiredReview(t, "fixture.zip", tyranoScriptProjectArchive(t))
}

func TestCreateTyranoScriptNWJSExecutableReachesTrialRequiredReview(t *testing.T) {
	testCreateTyranoScriptInputReachesTrialRequiredReview(t, "fixture.exe", tyranoScriptNWJSExecutable(t))
}

func TestCreateTyranoScriptElectronArchiveReachesTrialRequiredReview(t *testing.T) {
	testCreateTyranoScriptInputReachesTrialRequiredReview(t, "fixture.zip", tyranoScriptElectronArchive(t))
}

func testCreateTyranoScriptInputReachesTrialRequiredReview(t *testing.T, inputName string, input []byte) {
	t.Helper()
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
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{
		Purpose: "TYRANOSCRIPT_PROJECT", SourceType: "FILES",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "tyranoscript", RelativePath: inputName, SizeBytes: int64(len(input)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(input)
	if err := uploadService.PutPart(
		ctx, upload.ID, upload.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(input)-1, len(input)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(input),
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
			t, database.SQL, "tyranoscript/tyranoscript",
		),
		MetadataProvider: "HASHEOUS", ContentMode: "TYRANOSCRIPT_PROJECT_V1", TagIDs: []string{},
	})
	if err != nil {
		t.Fatalf("Create(TyranoScript) error = %v", err)
	}
	if created.ItemCount != 1 || created.State != "REVIEW_PENDING" {
		t.Fatalf("Create(TyranoScript) = %#v", created)
	}
	var state, code, contentKind, metadataProvider, providerID, targetID string
	var selectedValidation any
	if err := database.SQL.QueryRowContext(ctx, `
SELECT item.state,validation.compatibility_code,snapshot.content_kind,job.metadata_provider,
	   validation.provider_id,validation.target_id,draft.selected_validation_id
FROM import_items item
JOIN import_jobs job ON job.id=item.import_job_id
JOIN import_item_core_validations validation ON validation.import_item_id=item.id
JOIN import_item_source_snapshots snapshot ON snapshot.id=validation.source_snapshot_id
JOIN review_drafts draft ON draft.import_item_id=item.id
WHERE item.import_job_id=?
`, created.ImportJobID).Scan(
		&state, &code, &contentKind, &metadataProvider, &providerID, &targetID, &selectedValidation,
	); err != nil {
		t.Fatal(err)
	}
	if state != "REVIEW_PENDING" || code != "TYRANOSCRIPT_RUNTIME_TRIAL_REQUIRED" ||
		contentKind != "TYRANOSCRIPT_PROJECT_V1" || metadataProvider != "NONE" ||
		providerID != "retrom-runtime" || targetID != "tyranoscript" || selectedValidation != nil {
		t.Fatalf("TyranoScript review = %s/%s/%s/%s/%s/%s selected=%v",
			state, code, contentKind, metadataProvider, providerID, targetID, selectedValidation)
	}
}

func tyranoScriptProjectArchive(t *testing.T) []byte {
	t.Helper()
	files := map[string]string{
		"Fixture/index.html":                "<!doctype html><html><head></head><body></body></html>",
		"Fixture/data/scenario/first.ks":    "*start\n[cm]",
		"Fixture/data/system/Config.tjs":    "; TyranoScript config",
		"Fixture/tyrano/plugins/kag/kag.js": "window.tyrano = window.tyrano || {};",
		"Fixture/tyrano/tyrano.js":          "window.tyrano = window.tyrano || {};",
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, contents := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

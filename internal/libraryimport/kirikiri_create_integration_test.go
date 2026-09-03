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

func TestCreateKiriKiriArchiveReachesTrialRequiredReview(t *testing.T) {
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
	archive := kirikiriProjectArchive(t)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{
		Purpose: "KIRIKIRI_PROJECT", SourceType: "FILES",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "kirikiri", RelativePath: "fixture.zip", SizeBytes: int64(len(archive)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	if err := uploadService.PutPart(
		ctx, upload.ID, upload.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(archive)-1, len(archive)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(archive),
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
			t, database.SQL, "kirikiri/kirikiri2",
		),
		MetadataProvider: "HASHEOUS", ContentMode: "KIRIKIRI_PROJECT_V1", TagIDs: []string{},
	})
	if err != nil {
		t.Fatalf("Create(KiriKiri) error = %v", err)
	}
	if created.ItemCount != 1 || created.State != "REVIEW_PENDING" {
		t.Fatalf("Create(KiriKiri) = %#v", created)
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
	if state != "REVIEW_PENDING" || code != "KIRIKIRI_RUNTIME_TRIAL_REQUIRED" ||
		contentKind != "KIRIKIRI_PROJECT_V1" || metadataProvider != "NONE" || providerID != "retrom-runtime" ||
		targetID != "kirikiri2-kag" || selectedValidation != nil {
		t.Fatalf(
			"KiriKiri review = %s/%s/%s/%s/%s/%s selected=%v",
			state, code, contentKind, metadataProvider, providerID, targetID, selectedValidation,
		)
	}
}

func kirikiriProjectArchive(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, body := range map[string][]byte{
		"Fixture/startup.tjs":       []byte(`System.execStorage("scenario/first.ks");`),
		"Fixture/scenario/first.ks": []byte("[cm]\nKAG fixture\n"),
	} {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

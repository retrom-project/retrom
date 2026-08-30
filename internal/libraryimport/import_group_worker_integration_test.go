//go:build integration

package libraryimport

import (
	"archive/zip"
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
	"retrom/internal/store"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestQueuedImportGroupReturnsBeforePreparationAndPublishesProgress(t *testing.T) {
	ctx := context.Background()
	database, blobs, dataDir := openImportGroupFixture(t, ctx)
	uploadID := completeImportGroupUpload(t, ctx, database.SQL, blobs, dataDir, onsProjectArchive(t))
	service := New(database.SQL, time.Now).WithBlobStore(blobs)
	service.importGroupSlots <- struct{}{}
	released := false
	defer func() {
		if !released {
			<-service.importGroupSlots
		}
	}()

	created, err := service.QueueCreate(ctx, CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(
			t, database.SQL, "ons/onscripter_yuri",
		),
		MetadataProvider: "NONE", ContentMode: "ONS_PROJECT_V1", TagIDs: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.State != "QUEUED" || created.ItemCount != 0 {
		t.Fatalf("QueueCreate() = %#v", created)
	}
	var importState, jobState, bindingState, inputScopeType string
	var itemCount int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT import.state,job.state,json_extract(import.config_snapshot_json,'$.bindingState'),
 (SELECT count(*) FROM import_items WHERE import_job_id=import.id),
 json_extract(input.input_json,'$.scope.type')
FROM import_jobs import JOIN jobs job ON job.scope_id=import.id AND job.kind='IMPORT_GROUP'
JOIN job_input_snapshots input ON input.job_id=job.id AND input.execution_no=job.execution_no
WHERE import.id=?
`, created.ImportJobID).Scan(
		&importState, &jobState, &bindingState, &itemCount, &inputScopeType,
	); err != nil {
		t.Fatal(err)
	}
	if importState != "QUEUED" || jobState != "QUEUED" || bindingState != "PENDING" ||
		itemCount != 0 || inputScopeType != "IMPORT_GROUP" {
		t.Fatalf(
			"queued projection = %s/%s/%s items=%d inputScope=%s",
			importState, jobState, bindingState, itemCount, inputScopeType,
		)
	}

	<-service.importGroupSlots
	released = true
	waitForImportGroupTerminal(t, ctx, database.SQL, created.JobID, "SUCCEEDED")
	if err := database.SQL.QueryRowContext(ctx, `
SELECT state,total_item_count FROM import_jobs WHERE id=?
`, created.ImportJobID).Scan(&importState, &itemCount); err != nil {
		t.Fatal(err)
	}
	if importState != "REVIEW_PENDING" || itemCount != 1 {
		t.Fatalf("completed import = %s items=%d", importState, itemCount)
	}
	rows, err := database.SQL.QueryContext(ctx, `
SELECT event_type FROM job_events WHERE job_id=? ORDER BY id
`, created.JobID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	events := make([]string, 0, 4)
	for rows.Next() {
		var event string
		if err := rows.Scan(&event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	want := []string{"QUEUED", "STARTED", "PROGRESS", "SUCCEEDED"}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestQueuedImportGroupReportsInvalidProjectAsTerminalFailure(t *testing.T) {
	ctx := context.Background()
	database, blobs, dataDir := openImportGroupFixture(t, ctx)
	uploadID := completeImportGroupUpload(t, ctx, database.SQL, blobs, dataDir, invalidONSArchive(t))
	service := New(database.SQL, time.Now).WithBlobStore(blobs)
	created, err := service.QueueCreate(ctx, CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(
			t, database.SQL, "ons/onscripter_yuri",
		),
		MetadataProvider: "NONE", ContentMode: "ONS_PROJECT_V1", TagIDs: []string{},
	})
	if err != nil {
		t.Fatalf("QueueCreate() error = %v", err)
	}
	waitForImportGroupTerminal(t, ctx, database.SQL, created.JobID, "FAILED")
	var importState, errorCode string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT state,last_error_code FROM import_jobs WHERE id=?
`, created.ImportJobID).Scan(&importState, &errorCode); err != nil {
		t.Fatal(err)
	}
	if importState != "FAILED" || errorCode != "RPG_PROJECT_NOT_FOUND" {
		t.Fatalf("failed import = %s/%s", importState, errorCode)
	}
}

func TestQueuedImportGroupCanBeCancelledBeforePreparation(t *testing.T) {
	ctx := context.Background()
	database, blobs, dataDir := openImportGroupFixture(t, ctx)
	uploadID := completeImportGroupUpload(t, ctx, database.SQL, blobs, dataDir, onsProjectArchive(t))
	service := New(database.SQL, time.Now).WithBlobStore(blobs)
	service.importGroupSlots <- struct{}{}
	released := false
	defer func() {
		if !released {
			<-service.importGroupSlots
		}
	}()
	created, err := service.QueueCreate(ctx, onsImportGroupRequest(t, database.SQL, uploadID))
	if err != nil {
		t.Fatal(err)
	}
	cancelled, pending, err := service.Cancel(ctx, created.ImportJobID, 1, "operator cancelled")
	if err != nil || pending || cancelled.State != "CANCELLED" {
		t.Fatalf("Cancel() = %#v pending=%v error=%v", cancelled, pending, err)
	}
	var importState, jobState string
	var itemCount int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT import.state,job.state,(SELECT count(*) FROM import_items WHERE import_job_id=import.id)
FROM import_jobs import JOIN jobs job ON job.scope_id=import.id AND job.kind='IMPORT_GROUP'
WHERE import.id=?
`, created.ImportJobID).Scan(&importState, &jobState, &itemCount); err != nil {
		t.Fatal(err)
	}
	if importState != "CANCELLED" || jobState != "CANCELLED" || itemCount != 0 {
		t.Fatalf("cancelled projection = %s/%s items=%d", importState, jobState, itemCount)
	}
	<-service.importGroupSlots
	released = true
}

func TestRunningImportGroupIsRecoveredAfterProcessRestart(t *testing.T) {
	ctx := context.Background()
	database, blobs, dataDir := openImportGroupFixture(t, ctx)
	uploadID := completeImportGroupUpload(t, ctx, database.SQL, blobs, dataDir, onsProjectArchive(t))
	original := New(database.SQL, time.Now).WithBlobStore(blobs)
	original.importGroupSlots <- struct{}{}
	released := false
	defer func() {
		if !released {
			<-original.importGroupSlots
		}
	}()
	created, err := original.QueueCreate(ctx, onsImportGroupRequest(t, database.SQL, uploadID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := original.claimImportGroup(ctx, created.JobID); err != nil {
		t.Fatal(err)
	}
	recovered := New(database.SQL, time.Now).WithBlobStore(blobs)
	recovered.RecoverImportGroupJobs(ctx)
	waitForImportGroupTerminal(t, ctx, database.SQL, created.JobID, "SUCCEEDED")
	var importState string
	var attempts int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT import.state,job.attempt_count
FROM import_jobs import JOIN jobs job ON job.scope_id=import.id AND job.kind='IMPORT_GROUP'
WHERE import.id=?
`, created.ImportJobID).Scan(&importState, &attempts); err != nil {
		t.Fatal(err)
	}
	if importState != "REVIEW_PENDING" || attempts != 2 {
		t.Fatalf("recovered projection = %s attempts=%d", importState, attempts)
	}
	<-original.importGroupSlots
	released = true
}

func TestQueuedKiriKiriAndRPGMakerProjectsResolveInBackground(t *testing.T) {
	for _, test := range []struct {
		name, purpose, catalogKey, contentMode, expectedState string
		archive                                               func(*testing.T) []byte
	}{
		{
			name: "KiriKiri", purpose: "KIRIKIRI_PROJECT", catalogKey: "kirikiri/kirikiri2",
			contentMode: "KIRIKIRI_PROJECT_V1", expectedState: "REVIEW_PENDING", archive: kirikiriProjectArchive,
		},
		{
			name: "RPGMaker", purpose: "RPG_MAKER_PROJECT", catalogKey: "rpgmaker/rpgmaker",
			contentMode: "RPG_MAKER_PROJECT_V1", expectedState: "REVIEW_PENDING",
			archive: rpgMakerMVArchiveWithMToolSidecar,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			database, blobs, dataDir := openImportGroupFixture(t, ctx)
			uploadID := completeProjectUpload(
				t, ctx, database.SQL, blobs, dataDir, test.purpose, test.archive(t),
			)
			created, err := New(database.SQL, time.Now).WithBlobStore(blobs).QueueCreate(ctx, CreateRequest{
				UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(
					t, database.SQL, test.catalogKey,
				),
				MetadataProvider: "NONE", ContentMode: test.contentMode, TagIDs: []string{},
			})
			if err != nil {
				t.Fatal(err)
			}
			waitForImportGroupTerminal(t, ctx, database.SQL, created.JobID, "SUCCEEDED")
			var state string
			if err := database.SQL.QueryRowContext(ctx, `
SELECT state FROM import_jobs WHERE id=?
`, created.ImportJobID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != test.expectedState {
				t.Fatalf("background import state = %s, want %s", state, test.expectedState)
			}
		})
	}
}

func onsImportGroupRequest(t *testing.T, database *sql.DB, uploadID string) CreateRequest {
	t.Helper()
	return CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(
			t, database, "ons/onscripter_yuri",
		),
		MetadataProvider: "NONE", ContentMode: "ONS_PROJECT_V1", TagIDs: []string{},
	}
}

func openImportGroupFixture(
	t *testing.T,
	ctx context.Context,
) (*store.DB, *blobstore.Store, string) {
	t.Helper()
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
	return database, blobs, dataDir
}

func completeImportGroupUpload(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	blobs *blobstore.Store,
	dataDir string,
	archive []byte,
) string {
	return completeProjectUpload(t, ctx, database, blobs, dataDir, "ONS_PROJECT", archive)
}

func completeProjectUpload(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	blobs *blobstore.Store,
	dataDir, purpose string,
	archive []byte,
) string {
	t.Helper()
	uploadService := uploads.New(database, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(ctx, uploads.CreateRequest{
		Purpose: purpose, SourceType: "FILES",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "ons", RelativePath: "fixture.zip", SizeBytes: int64(len(archive)),
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
	waitForRPGUploadFinalization(t, ctx, database, jobID)
	return upload.ID
}

func invalidONSArchive(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	writer, err := archive.Create("not-a-project/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("not an ONS project")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func waitForImportGroupTerminal(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	jobID, wanted string,
) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; {
		var state string
		if err := database.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == wanted {
			return
		}
		if state == "FAILED" || state == "CANCELLED" || time.Now().After(deadline) {
			t.Fatalf("job state = %s, want %s", state, wanted)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

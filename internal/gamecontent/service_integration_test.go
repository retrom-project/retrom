//go:build integration

package gamecontent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	"retrom/internal/store"
	"retrom/internal/uploads"
)

func TestReplacementPublishesAtomicallyAndFailureKeepsCurrent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
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
	initialUpload := completeUpload(t, ctx, database.SQL, uploadService, "original.gba", []byte("original"))
	createdImport, err := libraryimport.New(database.SQL, time.Now).
		Create(ctx, libraryimport.CreateRequest{UploadID: initialUpload, TargetPlatformInstanceID: "01980000-0000-7000-8000-000000000005", MetadataProvider: "NONE"})
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id
FROM import_items
WHERE import_job_id=?
`, createdImport.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	published, err := libraryimport.New(database.SQL, time.Now).Approve(ctx, itemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var originalContent string
	var initialVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT current_content_revision_id,
version
FROM games
WHERE id=?
`, published.GameID).Scan(&originalContent, &initialVersion); err != nil {
		t.Fatal(err)
	}

	replacementUpload := completeUpload(t, ctx, database.SQL, uploadService, "replacement.gba", []byte("replacement"))
	service := New(database.SQL, time.Now)
	idempotencyKey := "01980000-0000-7000-8000-000000000099"
	requestDigest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	scheduled, replayed, err := service.ScheduleIdempotent(
		ctx,
		published.GameID,
		replacementUpload,
		initialVersion,
		idempotencyKey,
		requestDigest,
	)
	if err != nil || replayed || scheduled.Version != initialVersion {
		t.Fatalf("schedule = %#v, error=%v", scheduled, err)
	}
	replayedSchedule, replayed, err := service.ScheduleIdempotent(
		ctx,
		published.GameID,
		replacementUpload,
		initialVersion,
		idempotencyKey,
		requestDigest,
	)
	if err != nil || !replayed || replayedSchedule.JobID != scheduled.JobID {
		t.Fatalf("idempotent replay = %#v, replayed=%v, error=%v", replayedSchedule, replayed, err)
	}
	if _, _, err := service.ScheduleIdempotent(ctx, published.GameID, replacementUpload, initialVersion, idempotencyKey, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"); !errors.Is(
		err,
		ErrIdempotencyKeyReused,
	) {
		t.Fatalf("idempotency conflict error = %v", err)
	}
	waitForJob(t, ctx, database.SQL, scheduled.JobID, "SUCCEEDED")
	var replacementContent string
	var replacedVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT current_content_revision_id,
version
FROM games
WHERE id=?
`, published.GameID).Scan(&replacementContent, &replacedVersion); err != nil {
		t.Fatal(err)
	}
	if replacementContent == originalContent || replacedVersion != initialVersion+1 {
		t.Fatalf("content/version = %s/%d, wanted new/%d", replacementContent, replacedVersion, initialVersion+1)
	}
	var sourceKind, sourceRef, variantContent string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT c.source_kind,
c.source_ref_id,
r.game_content_revision_id
FROM game_content_revisions c
JOIN game_variant_revisions r ON r.game_content_revision_id=c.id
JOIN game_variants v ON v.current_revision_id=r.id
WHERE c.id=?
`, replacementContent).Scan(&sourceKind, &sourceRef, &variantContent); err != nil {
		t.Fatal(err)
	}
	if sourceKind != "ADMIN_REPLACE" || sourceRef != scheduled.JobID || variantContent != replacementContent {
		t.Fatalf("published revision = %s/%s/%s", sourceKind, sourceRef, variantContent)
	}

	if _, err := database.SQL.ExecContext(ctx, `
UPDATE games
SET platform_instance_id='01980000-0000-7000-8000-000000000006',
version=version+1,
updated_at_ms=?
WHERE id=?
`, time.Now().UnixMilli(), published.GameID); err != nil {
		t.Fatal(err)
	}
	badUpload := completeUpload(t, ctx, database.SQL, uploadService, "not-an-arcade-set.bin", []byte("invalid"))
	failed, err := service.Schedule(ctx, published.GameID, badUpload, replacedVersion+1)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, ctx, database.SQL, failed.JobID, "FAILED")
	var afterFailure string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT current_content_revision_id
FROM games
WHERE id=?
`, published.GameID).Scan(&afterFailure); err != nil {
		t.Fatal(err)
	}
	if afterFailure != replacementContent {
		t.Fatalf("failed replacement changed current content: %s != %s", afterFailure, replacementContent)
	}
	var failedRevisionCount int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT count(*)
FROM game_content_revisions
WHERE source_ref_id=?
`, failed.JobID).Scan(&failedRevisionCount); err != nil ||
		failedRevisionCount != 0 {
		t.Fatalf("failed revision count = %d, error=%v", failedRevisionCount, err)
	}
}

func completeUpload(t *testing.T, ctx context.Context, database interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, service *uploads.Service, name string, contents []byte,
) string {
	t.Helper()
	session, err := service.Create(
		ctx,
		uploads.CreateRequest{
			SourceType: "FILES",
			Files: []uploads.FileDeclaration{
				{ClientFileID: "file", RelativePath: name, SizeBytes: int64(len(contents))},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	contentRange := "bytes 0-" + strconv.FormatInt(
		int64(len(contents)-1),
		10,
	) + "/" + strconv.FormatInt(
		int64(len(contents)),
		10,
	)
	if err := service.PutPart(ctx, session.ID, session.Files[0].ID, 0, contentRange, "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := service.Complete(ctx, session.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitForJob(t, ctx, database, jobID, "SUCCEEDED")
	return session.ID
}

func waitForJob(t *testing.T, ctx context.Context, database interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, jobID, expected string,
) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); ; {
		var state string
		if err := database.QueryRowContext(ctx, `
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == expected {
			return
		}
		if state == "FAILED" || state == "CANCELLED" || time.Now().After(deadline) {
			t.Fatalf("job %s state = %s, wanted %s", jobID, state, expected)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

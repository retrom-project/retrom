//go:build integration

package gamecontent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func installSaturnBIOS(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	blobs *blobstore.Store,
) {
	t.Helper()
	metadata, err := blobs.Put(bytes.NewReader([]byte("replacement Saturn BIOS fixture")))
	testassert.False(t, err != nil, err)
	blobID, err := blobstore.EnsureRecord(ctx, database, metadata, "application/octet-stream", time.Now().UnixMilli())
	testassert.False(t, err != nil, err)
	var requirementID string
	var requirementVersion int64
	if err := database.QueryRowContext(ctx, `
SELECT id,version FROM bios_requirements
WHERE core_id='yabause' AND logical_name='saturn_bios.bin' AND enabled=1
`).Scan(&requirementID, &requirementVersion); err != nil {
		t.Fatal(err)
	}
	installationID, _ := uuid.NewV7()
	if _, err := database.ExecContext(ctx, `
INSERT INTO bios_installations(id,requirement_id,blob_id,original_filename,size_bytes,md5,sha1,sha256,
validated_requirement_version,status,validation_details_json,is_active,version,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,'HASH_WARNING','{}',1,1,?,?)
`, installationID.String(), requirementID, blobID, "saturn_bios.bin", metadata.Size,
		metadata.MD5, metadata.SHA1, metadata.SHA256, requirementVersion,
		time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

func TestReplacementPublishesAtomicallyAndFailureKeepsCurrent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	initialUpload := completeUpload(t, ctx, database.SQL, uploadService, "original.gba", []byte("original"))
	gbaID := testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba")
	createdImport, err := libraryimport.New(database.SQL, time.Now).
		Create(ctx, libraryimport.CreateRequest{UploadID: initialUpload, TargetPlatformInstanceID: gbaID, MetadataProvider: "NONE"})
	testassert.False(t, err != nil, err)
	var itemID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id
FROM import_items
WHERE import_job_id=?
`, createdImport.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	published, err := libraryimport.New(database.SQL, time.Now).Approve(ctx, itemID, 1)
	testassert.False(t, err != nil, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return replayed }, func() bool { return scheduled.Version != initialVersion }), "schedule = %#v, error=%v", scheduled, err)
	replayedSchedule, replayed, err := service.ScheduleIdempotent(
		ctx,
		published.GameID,
		replacementUpload,
		initialVersion,
		idempotencyKey,
		requestDigest,
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !replayed }, func() bool { return replayedSchedule.JobID != scheduled.JobID }), "idempotent replay = %#v, replayed=%v, error=%v", replayedSchedule, replayed, err)
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
	testassert.Falsef(t, testassert.Any(func() bool { return replacementContent == originalContent }, func() bool { return replacedVersion != initialVersion+1 }), "content/version = %s/%d, wanted new/%d", replacementContent, replacedVersion, initialVersion+1)
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
	testassert.Falsef(t, testassert.Any(func() bool { return sourceKind != "ADMIN_REPLACE" }, func() bool { return sourceRef != scheduled.JobID }, func() bool { return variantContent != replacementContent }), "published revision = %s/%s/%s", sourceKind, sourceRef, variantContent)

	if _, err := database.SQL.ExecContext(ctx, `
UPDATE games
SET platform_instance_id=(SELECT id FROM platform_instances WHERE catalog_template_key='arcade/fbneo'),
version=version+1,
updated_at_ms=?
WHERE id=?
`, time.Now().UnixMilli(), published.GameID); err != nil {
		t.Fatal(err)
	}
	badUpload := completeUpload(t, ctx, database.SQL, uploadService, "not-an-arcade-set.bin", []byte("invalid"))
	failed, err := service.Schedule(ctx, published.GameID, badUpload, replacedVersion+1)
	testassert.False(t, err != nil, err)
	waitForJob(t, ctx, database.SQL, failed.JobID, "FAILED")
	var afterFailure string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT current_content_revision_id
FROM games
WHERE id=?
`, published.GameID).Scan(&afterFailure); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, afterFailure != replacementContent, "failed replacement changed current content: %s != %s", afterFailure, replacementContent)
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

func TestMultiDiscReplacementPublishesCompleteRevisionAndRejectsMissingDisc(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	installSaturnBIOS(t, ctx, database.SQL, blobs)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	initialUpload := completeUpload(t, ctx, database.SQL, uploadService, "original.chd", fakeReplacementCHD("original"))
	importer := libraryimport.New(database.SQL, time.Now).WithBlobStore(blobs)
	saturnID := testsupport.MustPlatformInstanceID(t, database.SQL, "saturn/yabause")
	createdImport, err := importer.Create(ctx, libraryimport.CreateRequest{
		UploadID: initialUpload, TargetPlatformInstanceID: saturnID,
		MetadataProvider: "NONE",
	})
	testassert.False(t, err != nil, err)
	var itemID string
	if err := database.SQL.QueryRowContext(ctx, `SELECT id FROM import_items WHERE import_job_id=?`,
		createdImport.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	published, err := importer.Approve(ctx, itemID, 1)
	testassert.False(t, err != nil, err)
	var originalContentID string
	var gameVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT current_content_revision_id,version FROM games WHERE id=?
`, published.GameID).Scan(&originalContentID, &gameVersion); err != nil {
		t.Fatal(err)
	}
	replacementUpload := completeDirectoryUpload(t, ctx, database.SQL, uploadService, map[string][]byte{
		"replacement/game.m3u":   []byte("one.chd\ntwo.chd\n"),
		"replacement/one.chd":    fakeReplacementCHD("one"),
		"replacement/two.chd":    fakeReplacementCHD("two"),
		"replacement/readme.txt": []byte("ignored"),
	})
	service := New(database.SQL, time.Now).WithBlobStore(blobs).WithMultiDiscImportEnabled(true)
	scheduled, err := service.ScheduleMode(
		ctx, published.GameID, replacementUpload, "MULTI_DISC_M3U_V1", gameVersion,
	)
	testassert.False(t, err != nil, err)
	waitForJob(t, ctx, database.SQL, scheduled.JobID, "SUCCEEDED")
	var currentContentID, contentKind string
	var replacedVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT game.current_content_revision_id,game.version,content.content_kind
FROM games game JOIN game_content_revisions content ON content.id=game.current_content_revision_id
WHERE game.id=?
`, published.GameID).Scan(&currentContentID, &replacedVersion, &contentKind); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return currentContentID == originalContentID }, func() bool { return replacedVersion != gameVersion+1 }, func() bool { return contentKind != "MULTI_DISC_M3U_V1" }), "replacement = %s/%d/%s", currentContentID, replacedVersion, contentKind)
	var discCount, playlistCount int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM game_content_files WHERE game_content_revision_id=? AND role='DISC'),
       (SELECT count(*) FROM game_variants variant
        JOIN variant_files file ON file.game_variant_revision_id=variant.current_revision_id
        WHERE variant.game_id=? AND file.role='MULTI_DISC_PLAYLIST')
`, currentContentID, published.GameID).Scan(&discCount, &playlistCount); err != nil ||
		discCount != 2 || playlistCount != 1 {
		t.Fatalf("published multi evidence = discs=%d playlists=%d error=%v", discCount, playlistCount, err)
	}
	missingUpload := completeDirectoryUpload(t, ctx, database.SQL, uploadService, map[string][]byte{
		"broken/game.m3u": []byte("one.chd\ntwo.chd\n"),
		"broken/one.chd":  fakeReplacementCHD("one-new"),
	})
	failed, err := service.ScheduleMode(
		ctx, published.GameID, missingUpload, "MULTI_DISC_M3U_V1", replacedVersion,
	)
	testassert.False(t, err != nil, err)
	waitForJob(t, ctx, database.SQL, failed.JobID, "FAILED")
	var errorCode, afterFailure string
	if err := database.SQL.QueryRowContext(ctx, `SELECT error_code FROM jobs WHERE id=?`, failed.JobID).
		Scan(&errorCode); err != nil {
		t.Fatal(err)
	}
	if err := database.SQL.QueryRowContext(ctx, `SELECT current_content_revision_id FROM games WHERE id=?`,
		published.GameID).Scan(&afterFailure); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return errorCode != "MULTI_DISC_FILE_MISSING" }, func() bool { return afterFailure != currentContentID }), "failed replacement = code=%s content=%s", errorCode, afterFailure)
}

func fakeReplacementCHD(payload string) []byte {
	return append([]byte("MComprHD"), []byte(payload)...)
}

func completeDirectoryUpload(
	t *testing.T,
	ctx context.Context,
	database interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	service *uploads.Service,
	contents map[string][]byte,
) string {
	t.Helper()
	paths := make([]string, 0, len(contents))
	for name := range contents {
		paths = append(paths, name)
	}
	slices.Sort(paths)
	declarations := make([]uploads.FileDeclaration, 0, len(paths))
	for index, name := range paths {
		declarations = append(declarations, uploads.FileDeclaration{
			ClientFileID: fmt.Sprintf("file-%d", index), RelativePath: name,
			SizeBytes: int64(len(contents[name])),
		})
	}
	session, err := service.Create(ctx, uploads.CreateRequest{SourceType: "DIRECTORY", Files: declarations})
	testassert.False(t, err != nil, err)
	for index, name := range paths {
		value := contents[name]
		digest := sha256.Sum256(value)
		contentRange := fmt.Sprintf("bytes 0-%d/%d", len(value)-1, len(value))
		if err := service.PutPart(
			ctx, session.ID, session.Files[index].ID, 0, contentRange,
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(value),
		); err != nil {
			t.Fatal(err)
		}
	}
	current, err := service.Get(ctx, session.ID)
	testassert.False(t, err != nil, err)
	jobID, _, err := service.Complete(ctx, session.ID, current.Version)
	testassert.False(t, err != nil, err)
	waitForJob(t, ctx, database, jobID, "SUCCEEDED")
	return session.ID
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
	testassert.False(t, err != nil, err)
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
	testassert.False(t, err != nil, err)
	jobID, _, err := service.Complete(ctx, session.ID, current.Version)
	testassert.False(t, err != nil, err)
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
		testassert.Falsef(t, testassert.Any(func() bool { return state == "FAILED" }, func() bool { return state == "CANCELLED" }, func() bool { return time.Now().After(deadline) }), "job %s state = %s, wanted %s", jobID, state, expected)
		time.Sleep(10 * time.Millisecond)
	}
}

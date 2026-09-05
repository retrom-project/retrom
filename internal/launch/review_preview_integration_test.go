//go:build integration

package launch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestReviewPreviewStoresFiveSecondScreenshotAndAllowsBlockedRuntimeOverride(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	const actorID = "01980000-0000-7000-8000-000000009995"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('review-preview-profile','Review Preview Admin',0);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'review-preview-profile','review-preview-admin','Review Preview Admin','ADMIN','ENABLED',0,0);
`, actorID); err != nil {
		t.Fatal(err)
	}
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(
		filepath.Join(repositoryRoot, "data"), []string{"4.2.3", "4.3.0-pre"}, "4.2.3",
	)
	testassert.False(t, err != nil, err)
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	importService := libraryimport.New(database.SQL, time.Now)
	createReview := func(name string, contents []byte, targetID string) string {
		t.Helper()
		upload, createErr := uploadService.Create(ctx, uploads.CreateRequest{
			SourceType: "FILES", Files: []uploads.FileDeclaration{{
				ClientFileID: name, RelativePath: name, SizeBytes: int64(len(contents)),
			}},
		})
		testassert.False(t, createErr != nil, createErr)
		digest := sha256.Sum256(contents)
		if putErr := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0,
			fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)),
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); putErr != nil {
			t.Fatal(putErr)
		}
		current, getErr := uploadService.Get(ctx, upload.ID)
		testassert.False(t, getErr != nil, getErr)
		jobID, _, completeErr := uploadService.Complete(ctx, upload.ID, current.Version)
		testassert.False(t, completeErr != nil, completeErr)
		for deadline := time.Now().Add(3 * time.Second); ; {
			var state string
			if queryErr := database.SQL.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state); queryErr != nil {
				t.Fatal(queryErr)
			}
			if state == "SUCCEEDED" {
				break
			}
			testassert.Falsef(t, testassert.Any(func() bool { return state == "FAILED" }, func() bool { return time.Now().After(deadline) }), "review preview upload finalization = %s", state)
			time.Sleep(10 * time.Millisecond)
		}
		created, importErr := importService.Create(ctx, libraryimport.CreateRequest{
			UploadID: upload.ID, TargetPlatformInstanceID: targetID, MetadataProvider: "NONE",
		})
		testassert.False(t, importErr != nil, importErr)
		var itemID string
		if queryErr := database.SQL.QueryRowContext(ctx, `
SELECT id FROM import_items WHERE import_job_id=?
`, created.ImportJobID).Scan(&itemID); queryErr != nil {
			t.Fatal(queryErr)
		}
		return itemID
	}

	readyItemID := createReview("ready.gba", []byte("review-preview-ready"), testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba"))
	blockedItemID := createReview("blocked.fds", []byte("review-preview-blocked"), testsupport.MustPlatformInstanceID(t, database.SQL, "nes/fceumm"))
	parentMetadata, err := blobs.Put(bytes.NewReader([]byte("review-preview-parent")))
	testassert.False(t, err != nil, err)
	parentBlobID, err := blobstore.EnsureRecord(ctx, database.SQL, parentMetadata, "application/zip", time.Now().UnixMilli())
	testassert.False(t, err != nil, err)
	var baseValidationID, sourceSnapshotID, datVersionID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT draft.selected_validation_id,draft.effective_source_snapshot_id,(SELECT id FROM dat_versions ORDER BY id LIMIT 1)
FROM review_drafts draft WHERE draft.import_item_id=?
`, readyItemID).Scan(&baseValidationID, &sourceSnapshotID, &datVersionID); err != nil {
		t.Fatal(err)
	}
	arcadeValidationID := newUUID()
	arcadeSnapshot := fmt.Sprintf(`{"schemaVersion":1,"kind":"ARCADE","machine":"review-child","datVersionId":%q,"closure":[],"dependencies":[{"kind":"PARENT","machine":"review-parent","state":"SATISFIED_EXTERNAL","requiredEntries":[]}],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`, datVersionID)
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,
platform_instance_version,core_id,provider_id,target_id,
dat_version_id,default_dos_entry,source_manifest_digest,source_snapshot_id,prepublish_input_digest,
status,compatibility_code,dependency_snapshot_json,created_at_ms)
SELECT ?,import_item_id,target_platform_instance_id,platform_instance_version,core_id,provider_id,target_id,
?,default_dos_entry,source_manifest_digest,source_snapshot_id,
?,status,compatibility_code,?,created_at_ms+1
FROM import_item_core_validations WHERE id=?
`, arcadeValidationID, datVersionID, strings.Repeat("a", 64), arcadeSnapshot, baseValidationID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO import_item_validation_files(import_item_core_validation_id,role,logical_name,blob_id,sort_order,created_at_ms)
VALUES(?,'PARENT','review-parent.zip',?,0,0)
`, arcadeValidationID, parentBlobID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE review_drafts SET selected_validation_id=?,version=version+1,updated_at_ms=updated_at_ms+1
WHERE import_item_id=? AND effective_source_snapshot_id=?
`, arcadeValidationID, readyItemID, sourceSnapshotID); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	runtimeBuilder, err := testsupport.NewRuntimeBuilder(ctx, database.SQL)
	testassert.False(t, err != nil, err)
	service := New(database.SQL, dependencySet, credentials, time.Now).WithBlobStore(blobs).
		WithRuntimeProvider(dependencySet.RuntimeCatalog, runtimeBuilder)
	capabilities := Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}
	ready, err := service.CreateReviewPreview(ctx, ReviewPreviewRequest{
		ImportItemID: readyItemID, ActorUserID: actorID, IdempotencyKey: "ready-preview-1",
		ClientCapabilities: capabilities,
	})
	testassert.Falsef(t, err != nil, "ready review preview = %#v, error=%v", ready, err)
	replayed, err := service.CreateReviewPreview(ctx, ReviewPreviewRequest{
		ImportItemID: readyItemID, ActorUserID: actorID, IdempotencyKey: "ready-preview-1",
		ClientCapabilities: capabilities,
	})
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return replayed.PreviewID != ready.PreviewID }, func() bool { return replayed.Capability != ready.Capability }), "replayed review preview = %#v, error=%v", replayed, err)
	configuration, err := service.ReviewPreviewConfig(ctx, ready.PreviewID, ready.Capability)
	testassert.False(t, err != nil, err)
	readyEnvelope := testsupport.RuntimeEnvelope(t, configuration)
	readySession := testsupport.RuntimeEnvelopeObject(t, readyEnvelope, "session")
	parentResource := testsupport.RuntimeEnvelopeResource(t, readyEnvelope, "parent")
	testassert.Falsef(t, testassert.Any(
		func() bool { return readySession["purpose"] != "REVIEW_PREVIEW" },
		func() bool { return parentResource["url"] == "" },
	), "ready review envelope = %#v", readyEnvelope)
	content, err := service.ReviewPreviewContent(ctx, ready.PreviewID, ready.Capability, "ready.gba")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return content.Digest == "" }, func() bool { return content.Format != "SOURCE_V1" }), "ready review content = %#v, error=%v", content, err)
	pngBody, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	testassert.False(t, err != nil, err)
	screenshot, err := service.StoreReviewScreenshot(ctx, ready.PreviewID, ready.Capability, bytes.NewReader(pngBody))
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return screenshot.ImportItemID != readyItemID }, func() bool { return screenshot.WidthPX != 1 }, func() bool { return screenshot.HeightPX != 1 }), "stored review screenshot = %#v, error=%v", screenshot, err)

	blocked, err := service.CreateReviewPreview(ctx, ReviewPreviewRequest{
		ImportItemID: blockedItemID, ActorUserID: actorID, IdempotencyKey: "blocked-preview-1",
		ClientCapabilities: capabilities,
	})
	testassert.Falsef(t, err != nil, "blocked best-effort preview = %#v, error=%v", blocked, err)
	blockedConfig, err := service.ReviewPreviewConfig(ctx, blocked.PreviewID, blocked.Capability)
	testassert.False(t, err != nil, err)
	blockedEnvelope := testsupport.RuntimeEnvelope(t, blockedConfig)
	blockedGame := testsupport.RuntimeEnvelopeResource(t, blockedEnvelope, "game")
	testassert.Falsef(t, testassert.Any(
		func() bool { return blockedGame["url"] == "" },
		func() bool { return len(testsupport.RuntimeEnvelopeResources(t, blockedEnvelope, "bios")) != 0 },
	), "blocked best-effort envelope = %#v", blockedEnvelope)
	blockedScreenshot, err := service.StoreReviewScreenshot(
		ctx, blocked.PreviewID, blocked.Capability, bytes.NewReader(pngBody),
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return blockedScreenshot.ImportItemID != blockedItemID }), "blocked screenshot = %#v, error=%v", blockedScreenshot, err)
	approved, err := importService.Approve(ctx, blockedItemID, 1)
	testassert.Falsef(t, err != nil, "approve blocked screenshot override: %v", err)
	var compatibilityCode string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT variant.compatibility_code
FROM games game
JOIN game_variants variant ON variant.game_id=game.id
WHERE game.id=?
`, approved.GameID).Scan(&compatibilityCode); err != nil || compatibilityCode != "REVIEW_SCREENSHOT_OVERRIDE" {
		t.Fatalf("screenshot override compatibility = %q, error=%v", compatibilityCode, err)
	}
	var arcadeOverrideVariantID string
	arcadeOverrideSnapshot := fmt.Sprintf(
		`{"schemaVersion":1,"kind":"ARCADE","machine":"review-blocked","datVersionId":%q,"closure":[],"dependencies":[{"kind":"BIOS_OR_BASE","machine":"review-bios","state":"SATISFIED_EXTERNAL","requiredEntries":[]}],"missingEntries":[],"mismatchedEntries":[],"warnings":[]}`,
		datVersionID,
	)
	if err := database.SQL.QueryRowContext(ctx, `
UPDATE game_variants SET dat_version_id=?,status='READY',compatibility_code='REVIEW_SCREENSHOT_OVERRIDE',
dependency_snapshot_json=?,version=version+1,updated_at_ms=updated_at_ms+1
WHERE game_id=? RETURNING id
`, datVersionID, arcadeOverrideSnapshot, approved.GameID).Scan(&arcadeOverrideVariantID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO variant_files(game_variant_id,role,logical_name,blob_id,sort_order)
VALUES(?,'BIOS_BUNDLE','review-bios.zip',?,0)
`, arcadeOverrideVariantID, parentBlobID); err != nil {
		t.Fatal(err)
	}
	createdLaunch, err := service.Create(ctx, "review-preview-profile", CreateRequest{
		GameID: approved.GameID, ReturnTo: "/games/" + approved.GameID, ClientCapabilities: capabilities,
	})
	testassert.Falsef(t, err != nil, "launch screenshot-approved game: %v", err)
	publishedConfig, err := service.Config(ctx, createdLaunch.LaunchID, createdLaunch.Capability)
	testassert.False(t, err != nil, err)
	publishedEnvelope := testsupport.RuntimeEnvelope(t, publishedConfig)
	publishedSession := testsupport.RuntimeEnvelopeObject(t, publishedEnvelope, "session")
	warnings, _ := publishedSession["warnings"].([]any)
	testassert.Falsef(t, testassert.Any(
		func() bool { return !slices.Contains(warnings, any("REVIEW_SCREENSHOT_OVERRIDE")) },
		func() bool { return len(testsupport.RuntimeEnvelopeResources(t, publishedEnvelope, "bios")) != 1 },
	), "screenshot-approved envelope = %#v", publishedEnvelope)
	publishedBIOS, err := service.BundleFiles(
		ctx, createdLaunch.LaunchID, createdLaunch.Capability, "BIOS_BUNDLE",
	)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(publishedBIOS) != 1 }, func() bool { return publishedBIOS[0].LogicalName != "review-bios.zip" }, func() bool { return publishedBIOS[0].SHA256 != parentMetadata.SHA256 }), "screenshot-approved Arcade BIOS = %#v, error=%v", publishedBIOS, err)
}

func ptr(value string) *string { return &value }

type melondsRequirement struct {
	id          string
	logicalName string
	virtualPath string
	version     int64
	oldDigest   string
	newDigest   string
}

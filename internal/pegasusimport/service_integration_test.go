package pegasusimport

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/dependencies"
	"retrom/internal/libraryimport"
	"retrom/internal/payloadrelease"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/tagging"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

type pegasusTestSQLExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func mustExecPegasusTest(
	ctx context.Context,
	t *testing.T,
	execer pegasusTestSQLExecer,
	query string,
	arguments ...any,
) {
	t.Helper()
	_, err := execer.ExecContext(ctx, query, arguments...)
	testassert.False(t, err != nil, err)
}

type pegasusTestScanner interface {
	Scan(...any) error
}

func mustScanPegasusTest(t *testing.T, scanner pegasusTestScanner, destinations ...any) {
	t.Helper()
	testassert.False(t, scanner.Scan(destinations...) != nil, "scan Pegasus fixture")
}

func assertNoPegasusGames(ctx context.Context, t *testing.T, database *store.DB) {
	t.Helper()
	var gameCount int
	mustScanPegasusTest(t, database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM games`), &gameCount)
	testassert.Falsef(t, gameCount != 0, "games before review = %d", gameCount)
}

func TestScanMapImportCreatesReviewBeforePublishingGameAndMedia(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	err = testsupport.SeedPlatformInstances(ctx, database.SQL)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	err = dependencySet.Bootstrap(ctx, database.SQL, time.Now())
	testassert.False(t, err != nil, err)
	mustExecPegasusTest(ctx, t, database.SQL, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('pegasus-profile','Pegasus Test',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-000000000800','pegasus-profile','pegasus-test','Pegasus Test','ADMIN','ENABLED',1,1)`)
	ctx = authn.WithPrincipal(ctx, authn.Principal{
		UserID: "01980000-0000-7000-8000-000000000800", ProfileID: "pegasus-profile", Role: "ADMIN",
	})
	tagService := tagging.New(database.SQL, time.Now)
	mappedTag, err := tagService.Create(ctx, "01980000-0000-7000-8000-000000000800", "扫描选择")
	testassert.False(t, err != nil, err)
	externalTag, err := tagService.Create(ctx, "01980000-0000-7000-8000-000000000800", "External")
	testassert.False(t, err != nil, err)
	driftTag, err := tagService.Create(ctx, "01980000-0000-7000-8000-000000000800", "映射后删除")
	testassert.False(t, err != nil, err)
	root := createPegasusIntegrationSource(t, dataDir)
	blobs, err := blobstore.Open(dataDir)
	testassert.False(t, err != nil, err)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	importer := libraryimport.New(database.SQL, time.Now).WithBlobStore(blobs)
	service := New(
		database.SQL,
		blobs,
		importer,
		credentials,
		[]config.ServerImportRoot{{ID: "games", Label: "Games", Path: root, CanonicalPath: root}},
		time.Now,
	)
	created, err := service.Create(
		ctx,
		CreateRequest{RootID: "games", SourceRelativePath: ""},
		"01980000-0000-7000-8000-000000000800",
	)
	testassert.False(t, err != nil, err)
	scanWork, ok := service.claim(ctx)
	testassert.True(t, ok, "scan job was not claimable")
	service.execute(ctx, scanWork)
	scanned, err := service.Get(ctx, created.ID)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return scanned.State != "AWAITING_MAPPING" }, func() bool { return scanned.Counts.Covers != 1 }, func() bool { return scanned.Counts.Videos != 1 }), "scan = %#v, error=%v", scanned, err)
	collections, err := service.Collections(ctx, created.ID, "", 0, "", 10)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(collections) != 1 }), "collections = %#v, error=%v", collections, err)
	var targetID string
	mustScanPegasusTest(t, database.SQL.QueryRowContext(context.Background(),
		`SELECT id FROM platform_instances WHERE platform_id='nes' AND enabled=1 ORDER BY sort_order,id LIMIT 1`),
		&targetID)
	mapped, err := service.UpdateMappings(
		ctx,
		created.ID,
		scanned.Version,
		[]Mapping{{CollectionID: collections[0].ID, Action: "IMPORT", PlatformInstanceID: targetID, TagIDs: []string{mappedTag.TagID, driftTag.TagID}}},
	)
	testassert.False(t, err != nil, err)
	mappedCollections, err := service.Collections(ctx, created.ID, "", 0, "", 10)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(mappedCollections) != 1 }, func() bool { return len(mappedCollections[0].TagSnapshot) != 2 }), "mapped tag snapshot = %#v, error=%v", mappedCollections, err)
	currentDriftTag, err := tagService.Get(ctx, driftTag.TagID)
	testassert.False(t, err != nil, err)
	_, _, err = tagService.Delete(
		ctx, "01980000-0000-7000-8000-000000000800", driftTag.TagID, driftTag.Name, currentDriftTag.Version,
	)
	testassert.False(t, err != nil, err)
	driftedPlan, err := service.Get(ctx, created.ID)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return driftedPlan.Version != mapped.Version+1 }), "deleted tag plan version = %#v, error=%v", driftedPlan, err)
	refreshedCollections, err := service.Collections(ctx, created.ID, "", 0, "", 10)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(refreshedCollections) != 1 }, func() bool { return len(refreshedCollections[0].TagSnapshot) != 1 }, func() bool { return refreshedCollections[0].TagSnapshot[0].TagID != mappedTag.TagID }), "refreshed active mapping tags = %#v, error=%v", refreshedCollections, err)
	if _, err := service.StartImport(ctx, created.ID, mapped.Version); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale mapping start error = %v", err)
	}
	remapped, err := service.UpdateMappings(
		ctx, created.ID, driftedPlan.Version,
		[]Mapping{{CollectionID: collections[0].ID, Action: "IMPORT", PlatformInstanceID: targetID, TagIDs: []string{mappedTag.TagID}}},
	)
	testassert.False(t, err != nil, err)
	_, err = service.StartImport(ctx, created.ID, remapped.Version)
	testassert.False(t, err != nil, err)
	importWork, ok := service.claim(ctx)
	testassert.True(t, ok, "import job was not claimable")
	service.execute(ctx, importWork)
	finished, err := service.Get(ctx, created.ID)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return finished.State != "COMPLETED" }, func() bool { return finished.Counts.ReviewPending != 2 }, func() bool { return finished.Counts.Published != 0 }, func() bool { return finished.Counts.Failed != 0 }), "finished = %#v, error=%v", finished, err)
	projectedItems, err := service.Items(ctx, created.ID, "", "", "", "", "", "", 10)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return len(projectedItems) != 2 }), "projected items = %#v, error=%v", projectedItems, err)
	for _, item := range projectedItems {
		testassert.Falsef(t, testassert.Any(func() bool { return len(item.Tags) != 1 }, func() bool { return item.Tags[0].TagID != mappedTag.TagID }), "projected item tags = %#v", item.Tags)
	}
	var mappedDrafts, externalDrafts int
	if err := database.SQL.QueryRowContext(context.Background(), `
SELECT
  (SELECT count(*) FROM review_draft_tags WHERE tag_id=?),
  (SELECT count(*) FROM review_draft_tags WHERE tag_id=?)
`, mappedTag.TagID, externalTag.TagID).Scan(&mappedDrafts, &externalDrafts); err != nil ||
		mappedDrafts != 2 || externalDrafts != 0 {
		t.Fatalf("review tag inheritance = mapped:%d external:%d error=%v", mappedDrafts, externalDrafts, err)
	}
	var reviewItemID string
	var reviewVersion int64
	var reviewTitle, reviewDescription, reviewDeveloper, reviewWarnings string
	mustScanPegasusTest(t, database.SQL.QueryRowContext(context.Background(), `
SELECT item.library_import_item_id,draft.version,json_extract(draft.metadata_json,'$.title'),
json_extract(draft.metadata_json,'$.description'),json_extract(draft.metadata_json,'$.developer'),item.warnings_json
FROM pegasus_import_items item
JOIN review_drafts draft ON draft.import_item_id=item.library_import_item_id
WHERE item.import_id=? AND item.execution_state='REVIEW_PENDING' AND item.title='Published Fixture'
`, created.ID),
		&reviewItemID, &reviewVersion, &reviewTitle, &reviewDescription, &reviewDeveloper, &reviewWarnings,
	)
	testassert.Falsef(t, testassert.Any(func() bool { return reviewTitle != "Published Fixture" }, func() bool { return len([]rune(reviewDescription)) != 10_000 }, func() bool { return len([]rune(reviewDeveloper)) != 200 }, func() bool {
		return !strings.Contains(reviewWarnings, `"code":"FIELD_TRUNCATED","field":"description"`)
	}, func() bool { return !strings.Contains(reviewWarnings, `"code":"FIELD_TRUNCATED","field":"developer"`) }), "review metadata = title:%q description:%d developer:%d warnings:%s", reviewTitle, len([]rune(reviewDescription)), len([]rune(reviewDeveloper)), reviewWarnings)
	assertNoPegasusGames(ctx, t, database)
	assertResumedPegasusReview(ctx, t, database, service, created.ID, importWork, mappedTag.TagID, mappedDrafts)
	_, err = importer.Approve(ctx, reviewItemID, reviewVersion)
	testassert.False(t, err != nil, err)
	var discardedItemID string
	var discardedVersion int64
	mustScanPegasusTest(t, database.SQL.QueryRowContext(context.Background(), `
SELECT item.library_import_item_id,draft.version
FROM pegasus_import_items item
JOIN review_drafts draft ON draft.import_item_id=item.library_import_item_id
WHERE item.import_id=? AND item.execution_state='REVIEW_PENDING' AND item.title='Discarded Fixture'
`, created.ID), &discardedItemID, &discardedVersion)
	_, err = importer.Discard(ctx, discardedItemID, discardedVersion, "not suitable")
	testassert.False(t, err != nil, err)
	var gameID, title string
	var assetCount, gameMappedTags, gameExternalTags int
	mustScanPegasusTest(t, database.SQL.QueryRowContext(context.Background(), `SELECT game.id,metadata.title,
  (SELECT count(*) FROM game_assets asset WHERE asset.game_id=game.id AND asset.metadata_revision_id=game.current_metadata_revision_id),
  (SELECT count(*) FROM game_tags relation WHERE relation.game_id=game.id AND relation.tag_id=?),
  (SELECT count(*) FROM game_tags relation WHERE relation.game_id=game.id AND relation.tag_id=?)
FROM games game JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
WHERE metadata.source_kind='SERVER_PEGASUS_IMPORT'`, mappedTag.TagID, externalTag.TagID),
		&gameID, &title, &assetCount, &gameMappedTags, &gameExternalTags)
	testassert.Falsef(t, testassert.Any(func() bool { return gameID == "" }, func() bool { return title != "Published Fixture" }, func() bool { return assetCount != 2 }, func() bool { return gameMappedTags != 1 }, func() bool { return gameExternalTags != 0 }), "published game = %q/%q assets=%d tags=%d/%d", gameID, title, assetCount, gameMappedTags, gameExternalTags)
	decided, err := service.Get(ctx, created.ID)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return decided.Counts.ReviewPending != 0 }, func() bool { return decided.Counts.Published != 1 }, func() bool { return decided.Counts.ReviewDiscarded != 1 }), "review decisions = %#v, error=%v", decided, err)
	assertPegasusPayloadReleased(t, database.SQL, blobs, created.ID, gameID)
}

func assertPegasusPayloadReleased(
	t *testing.T,
	database *sql.DB,
	blobs *blobstore.Store,
	importID, gameID string,
) {
	t.Helper()
	ctx := t.Context()
	releases, err := payloadrelease.New(database, blobs, time.Now, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 12; attempt++ {
		worked, runErr := releases.RunOnce(ctx)
		if runErr != nil {
			t.Fatal(runErr)
		}
		if !worked {
			break
		}
	}
	var releasedPegasus, releasedImports, pegasusBlobRefs, gamePayloadRows int64
	mustScanPegasusTest(t, database.QueryRowContext(ctx, `
SELECT
 (SELECT count(*) FROM pegasus_import_items WHERE import_id=? AND payload_state='RELEASED'),
 (SELECT count(*) FROM import_items WHERE id IN (SELECT library_import_item_id FROM pegasus_import_items WHERE import_id=?) AND payload_state='RELEASED'),
 (SELECT count(*) FROM pegasus_import_item_files file JOIN pegasus_import_items item ON item.id=file.item_id WHERE item.import_id=? AND (file.blob_id IS NOT NULL OR file.source_archive_blob_id IS NOT NULL))+
 (SELECT count(*) FROM pegasus_import_item_assets asset JOIN pegasus_import_items item ON item.id=asset.item_id WHERE item.import_id=? AND asset.blob_id IS NOT NULL),
 (SELECT count(*) FROM game_content_files file JOIN game_content_revisions revision ON revision.id=file.game_content_revision_id WHERE revision.game_id=?)+
 (SELECT count(*) FROM game_assets WHERE game_id=?)
`, importID, importID, importID, importID, gameID, gameID),
		&releasedPegasus, &releasedImports, &pegasusBlobRefs, &gamePayloadRows)
	testassert.Falsef(t, testassert.Any(
		func() bool { return releasedPegasus != 2 }, func() bool { return releasedImports != 2 },
		func() bool { return pegasusBlobRefs != 0 }, func() bool { return gamePayloadRows != 3 },
	), "released Pegasus = items:%d imports:%d refs:%d game:%d",
		releasedPegasus, releasedImports, pegasusBlobRefs, gamePayloadRows)
}

func createPegasusIntegrationSource(t *testing.T, dataDir string) string {
	t.Helper()
	root := filepath.Join(dataDir, "source")
	testassert.False(t, os.MkdirAll(filepath.Join(root, "media", "Published Fixture"), 0o700) != nil,
		"create Pegasus source directories")
	writeFixture(t, filepath.Join(root, "metadata.pegasus.txt"), []byte(
		"collection: NES\ngame: Published Fixture\ndescription: "+strings.Repeat("界", 10_001)+
			"\ndeveloper: "+strings.Repeat("开", 201)+
			"\ntags: External\ngenre: Action\nfile: fixture.nes\n\ngame: Discarded Fixture"+
			"\ndescription: Must not publish\ntags: External\nfile: discard.nes\n",
	))
	writeFixture(t, filepath.Join(root, "fixture.nes"), []byte("deterministic NES fixture"))
	writeFixture(t, filepath.Join(root, "discard.nes"), []byte("deterministic discarded NES fixture"))
	pngBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	testassert.False(t, err != nil, err)
	writeFixture(t, filepath.Join(root, "media", "Published Fixture", "boxFront.png"), pngBytes)
	writeFixture(t, filepath.Join(root, "media", "Published Fixture", "video.mp4"),
		[]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 'i', 's', 'o', 'm', 'm', 'p', '4', '2'})
	return root
}

func TestRecoverWorkClosesExhaustedLeaseAsFailed(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	if err := testsupport.SeedPlatformInstances(ctx, database.SQL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	const importID = "01980000-0000-7000-8000-000000000820"
	const scanJobID = "01980000-0000-7000-8000-000000000821"
	const workJobID = "01980000-0000-7000-8000-000000000822"
	const userID = "01980000-0000-7000-8000-000000000823"
	now := time.UnixMilli(1_786_000_000_000)
	mustExecPegasusTest(ctx, t, database.SQL, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('pegasus-recovery-profile','Recovery',1)`)
	mustExecPegasusTest(ctx, t, database.SQL, `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'pegasus-recovery-profile','pegasus-recovery','Recovery','ADMIN','ENABLED',1,1)
`, userID)
	mustExecPegasusTest(ctx, t, database.SQL, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,version,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_SCAN',?,1,'{}',1,'SUCCEEDED',1,4,1,1,1,1,1)
`, scanJobID, importID, strings.Repeat("1", 64))
	mustExecPegasusTest(ctx, t, database.SQL, `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,version,available_at_ms,execution_started_at_ms,execution_deadline_at_ms,leased_until_ms,heartbeat_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_IMPORT',?,1,'{}',1,'RUNNING',4,4,1,1,1,?,?,?,1,1)
`, workJobID, importID, strings.Repeat("2", 64), now.Add(time.Hour).UnixMilli(), now.Add(-time.Second).UnixMilli(), now.Add(-time.Second).UnixMilli())
	mustExecPegasusTest(ctx, t, database.SQL, `
INSERT INTO pegasus_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,phase,scan_job_id,import_job_id,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)
VALUES(?,'games','Games','',?,'RUNNING','COPYING_CONTENT',?,?,?,1,1,?)
`, importID, strings.Repeat("3", 64), scanJobID, workJobID, userID, now.Add(time.Hour).UnixMilli())
	service := &Service{database: database.SQL, now: func() time.Time { return now }}
	if err := service.recoverWork(ctx); err != nil {
		t.Fatal(err)
	}
	var aggregateState, jobState, aggregateCode, jobCode string
	var completedAt int64
	if err := database.SQL.QueryRowContext(context.Background(), `SELECT import.state,job.state,import.last_error_code,job.error_code,import.completed_at_ms FROM pegasus_imports import JOIN jobs job ON job.id=import.import_job_id WHERE import.id=?`, importID).Scan(&aggregateState, &jobState, &aggregateCode, &jobCode, &completedAt); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return aggregateState != "FAILED" }, func() bool { return jobState != "FAILED" }, func() bool { return aggregateCode != "PEGASUS_WORKER_ATTEMPTS_EXHAUSTED" }, func() bool { return jobCode != aggregateCode }, func() bool { return completedAt != now.UnixMilli() }), "recovered state = aggregate:%s job:%s codes:%s/%s completed:%d", aggregateState, jobState, aggregateCode, jobCode, completedAt)
}

func assertResumedPegasusReview(
	ctx context.Context, t *testing.T, database *store.DB, service *Service, importID string,
	claimedWork work, tagID string, mappedDrafts int,
) {
	var resumedPegasusItemID, resumedImportJobID, resumedReviewItemID string
	var importJobCount, draftEventCount int
	mustScanPegasusTest(t, database.SQL.QueryRowContext(ctx, `
SELECT item.id,item.library_import_job_id,item.library_import_item_id,
 (SELECT count(*) FROM import_jobs),
 (SELECT count(*) FROM review_events event
  WHERE event.import_item_id=item.library_import_item_id AND event.event_type='DRAFT_SAVED')
FROM pegasus_import_items item
WHERE item.import_id=? AND item.title='Discarded Fixture'
`, importID),
		&resumedPegasusItemID,
		&resumedImportJobID,
		&resumedReviewItemID,
		&importJobCount,
		&draftEventCount,
	)
	mustExecPegasusTest(ctx, t, database.SQL, `
UPDATE pegasus_imports
SET state='RUNNING',phase='VALIDATING',completed_at_ms=NULL
WHERE id=?
`, importID)
	mustExecPegasusTest(ctx, t, database.SQL, `
UPDATE pegasus_import_items
SET execution_state='PENDING',completed_at_ms=NULL
WHERE id=? AND execution_state='REVIEW_PENDING'
`, resumedPegasusItemID)
	resumed, found, err := service.nextItem(ctx, importID)
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !found }, func() bool { return resumed.LibraryImportJobID != resumedImportJobID }, func() bool { return resumed.LibraryImportItemID != resumedReviewItemID }), "resumed review handoff = %#v, found=%v, error=%v", resumed, found, err)
	service.processItem(ctx, claimedWork, service.roots["games"], resumed)
	err = service.finishImport(ctx, claimedWork)
	testassert.False(t, err != nil, err)
	var resumedState string
	var resumedImportJobCount, resumedDraftEventCount int
	mustScanPegasusTest(t, database.SQL.QueryRowContext(ctx, `
SELECT item.execution_state,
 (SELECT count(*) FROM import_jobs),
 (SELECT count(*) FROM review_events event
  WHERE event.import_item_id=item.library_import_item_id AND event.event_type='DRAFT_SAVED')
FROM pegasus_import_items item WHERE item.id=?
`, resumedPegasusItemID), &resumedState, &resumedImportJobCount, &resumedDraftEventCount)
	testassert.Falsef(t, testassert.Any(func() bool { return resumedState != "REVIEW_PENDING" }, func() bool { return resumedImportJobCount != importJobCount }, func() bool { return resumedDraftEventCount != draftEventCount }), "resumed review = state:%s imports:%d/%d draft events:%d/%d", resumedState, resumedImportJobCount, importJobCount, resumedDraftEventCount, draftEventCount)
	if err := database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM review_draft_tags WHERE tag_id=?`, tagID).
		Scan(&mappedDrafts); err != nil || mappedDrafts != 2 {
		t.Fatalf("resumed tag inheritance = %d, %v", mappedDrafts, err)
	}
}

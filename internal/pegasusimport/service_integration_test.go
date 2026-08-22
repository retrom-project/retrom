package pegasusimport

import (
	"context"
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
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/tagging"
	"retrom/internal/testsupport"
)

func TestScanMapImportCreatesReviewBeforePublishingGameAndMedia(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := testsupport.SeedPlatformInstances(ctx, database.SQL); err != nil {
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
	if _, err := database.SQL.Exec(`
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('pegasus-profile','Pegasus Test',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-000000000800','pegasus-profile','pegasus-test','Pegasus Test','ADMIN','ENABLED',1,1)`); err != nil {
		t.Fatal(err)
	}
	ctx = authn.WithPrincipal(ctx, authn.Principal{
		UserID: "01980000-0000-7000-8000-000000000800", ProfileID: "pegasus-profile", Role: "ADMIN",
	})
	tagService := tagging.New(database.SQL, time.Now)
	mappedTag, err := tagService.Create(ctx, "01980000-0000-7000-8000-000000000800", "扫描选择")
	if err != nil {
		t.Fatal(err)
	}
	externalTag, err := tagService.Create(ctx, "01980000-0000-7000-8000-000000000800", "External")
	if err != nil {
		t.Fatal(err)
	}
	driftTag, err := tagService.Create(ctx, "01980000-0000-7000-8000-000000000800", "映射后删除")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dataDir, "source")
	if err := os.MkdirAll(filepath.Join(root, "media", "Published Fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(
		t,
		filepath.Join(root, "metadata.pegasus.txt"),
		[]byte("collection: NES\ngame: Published Fixture\ndescription: "+strings.Repeat("界", 10_001)+"\ndeveloper: "+strings.Repeat("开", 201)+"\ntags: External\ngenre: Action\nfile: fixture.nes\n\ngame: Discarded Fixture\ndescription: Must not publish\ntags: External\nfile: discard.nes\n"),
	)
	writeFixture(t, filepath.Join(root, "fixture.nes"), []byte("deterministic NES fixture"))
	writeFixture(t, filepath.Join(root, "discard.nes"), []byte("deterministic discarded NES fixture"))
	pngBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(root, "media", "Published Fixture", "boxFront.png"), pngBytes)
	writeFixture(
		t,
		filepath.Join(root, "media", "Published Fixture", "video.mp4"),
		[]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 'i', 's', 'o', 'm', 'm', 'p', '4', '2'},
	)
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	scanWork, ok := service.claim(ctx)
	if !ok {
		t.Fatal("scan job was not claimable")
	}
	service.execute(ctx, scanWork)
	scanned, err := service.Get(ctx, created.ID)
	if err != nil || scanned.State != "AWAITING_MAPPING" || scanned.Counts.Covers != 1 || scanned.Counts.Videos != 1 {
		t.Fatalf("scan = %#v, error=%v", scanned, err)
	}
	collections, err := service.Collections(ctx, created.ID, "", 0, "", 10)
	if err != nil || len(collections) != 1 {
		t.Fatalf("collections = %#v, error=%v", collections, err)
	}
	var targetID string
	if err := database.SQL.QueryRow(`SELECT id FROM platform_instances WHERE platform_id='nes' AND enabled=1 ORDER BY sort_order,id LIMIT 1`).Scan(&targetID); err != nil {
		t.Fatal(err)
	}
	mapped, err := service.UpdateMappings(
		ctx,
		created.ID,
		scanned.Version,
		[]Mapping{{CollectionID: collections[0].ID, Action: "IMPORT", PlatformInstanceID: targetID, TagIDs: []string{mappedTag.TagID, driftTag.TagID}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	mappedCollections, err := service.Collections(ctx, created.ID, "", 0, "", 10)
	if err != nil || len(mappedCollections) != 1 || len(mappedCollections[0].TagSnapshot) != 2 {
		t.Fatalf("mapped tag snapshot = %#v, error=%v", mappedCollections, err)
	}
	currentDriftTag, err := tagService.Get(ctx, driftTag.TagID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tagService.Delete(
		ctx, "01980000-0000-7000-8000-000000000800", driftTag.TagID, driftTag.Name, currentDriftTag.Version,
	); err != nil {
		t.Fatal(err)
	}
	driftedPlan, err := service.Get(ctx, created.ID)
	if err != nil || driftedPlan.Version != mapped.Version+1 {
		t.Fatalf("deleted tag plan version = %#v, error=%v", driftedPlan, err)
	}
	refreshedCollections, err := service.Collections(ctx, created.ID, "", 0, "", 10)
	if err != nil || len(refreshedCollections) != 1 || len(refreshedCollections[0].TagSnapshot) != 1 ||
		refreshedCollections[0].TagSnapshot[0].TagID != mappedTag.TagID {
		t.Fatalf("refreshed active mapping tags = %#v, error=%v", refreshedCollections, err)
	}
	if _, err := service.StartImport(ctx, created.ID, mapped.Version); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale mapping start error = %v", err)
	}
	remapped, err := service.UpdateMappings(
		ctx, created.ID, driftedPlan.Version,
		[]Mapping{{CollectionID: collections[0].ID, Action: "IMPORT", PlatformInstanceID: targetID, TagIDs: []string{mappedTag.TagID}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartImport(ctx, created.ID, remapped.Version); err != nil {
		t.Fatal(err)
	}
	importWork, ok := service.claim(ctx)
	if !ok {
		t.Fatal("import job was not claimable")
	}
	service.execute(ctx, importWork)
	finished, err := service.Get(ctx, created.ID)
	if err != nil || finished.State != "COMPLETED" || finished.Counts.ReviewPending != 2 ||
		finished.Counts.Published != 0 || finished.Counts.Failed != 0 {
		t.Fatalf("finished = %#v, error=%v", finished, err)
	}
	projectedItems, err := service.Items(ctx, created.ID, "", "", "", "", "", "", 10)
	if err != nil || len(projectedItems) != 2 {
		t.Fatalf("projected items = %#v, error=%v", projectedItems, err)
	}
	for _, item := range projectedItems {
		if len(item.Tags) != 1 || item.Tags[0].TagID != mappedTag.TagID {
			t.Fatalf("projected item tags = %#v", item.Tags)
		}
	}
	var mappedDrafts, externalDrafts int
	if err := database.SQL.QueryRow(`
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
	if err := database.SQL.QueryRow(`
SELECT item.library_import_item_id,draft.version,json_extract(draft.metadata_json,'$.title'),
json_extract(draft.metadata_json,'$.description'),json_extract(draft.metadata_json,'$.developer'),item.warnings_json
FROM pegasus_import_items item
JOIN review_drafts draft ON draft.import_item_id=item.library_import_item_id
WHERE item.import_id=? AND item.execution_state='REVIEW_PENDING' AND item.title='Published Fixture'
`, created.ID).Scan(
		&reviewItemID, &reviewVersion, &reviewTitle, &reviewDescription, &reviewDeveloper, &reviewWarnings,
	); err != nil {
		t.Fatal(err)
	}
	if reviewTitle != "Published Fixture" || len([]rune(reviewDescription)) != 10_000 ||
		len([]rune(reviewDeveloper)) != 200 ||
		!strings.Contains(reviewWarnings, `"code":"FIELD_TRUNCATED","field":"description"`) ||
		!strings.Contains(reviewWarnings, `"code":"FIELD_TRUNCATED","field":"developer"`) {
		t.Fatalf(
			"review metadata = title:%q description:%d developer:%d warnings:%s",
			reviewTitle, len([]rune(reviewDescription)), len([]rune(reviewDeveloper)), reviewWarnings,
		)
	}
	var gameCount int
	if err := database.SQL.QueryRow(`SELECT count(*) FROM games`).Scan(&gameCount); err != nil {
		t.Fatal(err)
	}
	if gameCount != 0 {
		t.Fatalf("games before review = %d", gameCount)
	}
	var resumedPegasusItemID, resumedImportJobID, resumedReviewItemID string
	var importJobCount, draftEventCount int
	if err := database.SQL.QueryRow(`
SELECT item.id,item.library_import_job_id,item.library_import_item_id,
 (SELECT count(*) FROM import_jobs),
 (SELECT count(*) FROM review_events event
  WHERE event.import_item_id=item.library_import_item_id AND event.event_type='DRAFT_SAVED')
FROM pegasus_import_items item
WHERE item.import_id=? AND item.title='Discarded Fixture'
`, created.ID).Scan(
		&resumedPegasusItemID,
		&resumedImportJobID,
		&resumedReviewItemID,
		&importJobCount,
		&draftEventCount,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.Exec(`
UPDATE pegasus_imports
SET state='RUNNING',phase='VALIDATING',completed_at_ms=NULL
WHERE id=?
`, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.Exec(`
UPDATE pegasus_import_items
SET execution_state='PENDING',completed_at_ms=NULL
WHERE id=? AND execution_state='REVIEW_PENDING'
`, resumedPegasusItemID); err != nil {
		t.Fatal(err)
	}
	resumed, found, err := service.nextItem(ctx, created.ID)
	if err != nil || !found || resumed.LibraryImportJobID != resumedImportJobID ||
		resumed.LibraryImportItemID != resumedReviewItemID {
		t.Fatalf("resumed review handoff = %#v, found=%v, error=%v", resumed, found, err)
	}
	service.processItem(ctx, importWork, service.roots["games"], resumed)
	if err := service.finishImport(ctx, importWork); err != nil {
		t.Fatal(err)
	}
	var resumedState string
	var resumedImportJobCount, resumedDraftEventCount int
	if err := database.SQL.QueryRow(`
SELECT item.execution_state,
 (SELECT count(*) FROM import_jobs),
 (SELECT count(*) FROM review_events event
  WHERE event.import_item_id=item.library_import_item_id AND event.event_type='DRAFT_SAVED')
FROM pegasus_import_items item WHERE item.id=?
`, resumedPegasusItemID).Scan(&resumedState, &resumedImportJobCount, &resumedDraftEventCount); err != nil {
		t.Fatal(err)
	}
	if resumedState != "REVIEW_PENDING" || resumedImportJobCount != importJobCount ||
		resumedDraftEventCount != draftEventCount {
		t.Fatalf(
			"resumed review = state:%s imports:%d/%d draft events:%d/%d",
			resumedState,
			resumedImportJobCount,
			importJobCount,
			resumedDraftEventCount,
			draftEventCount,
		)
	}
	if err := database.SQL.QueryRow(`SELECT count(*) FROM review_draft_tags WHERE tag_id=?`, mappedTag.TagID).
		Scan(&mappedDrafts); err != nil || mappedDrafts != 2 {
		t.Fatalf("resumed tag inheritance = %d, %v", mappedDrafts, err)
	}
	if _, err := importer.Approve(ctx, reviewItemID, reviewVersion); err != nil {
		t.Fatal(err)
	}
	var discardedItemID string
	var discardedVersion int64
	if err := database.SQL.QueryRow(`
SELECT item.library_import_item_id,draft.version
FROM pegasus_import_items item
JOIN review_drafts draft ON draft.import_item_id=item.library_import_item_id
WHERE item.import_id=? AND item.execution_state='REVIEW_PENDING' AND item.title='Discarded Fixture'
`, created.ID).Scan(&discardedItemID, &discardedVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := importer.Discard(ctx, discardedItemID, discardedVersion, "not suitable"); err != nil {
		t.Fatal(err)
	}
	var gameID, title string
	var assetCount, gameMappedTags, gameExternalTags int
	if err := database.SQL.QueryRow(`SELECT game.id,metadata.title,
  (SELECT count(*) FROM game_assets asset WHERE asset.game_id=game.id AND asset.metadata_revision_id=game.current_metadata_revision_id),
  (SELECT count(*) FROM game_tags relation WHERE relation.game_id=game.id AND relation.tag_id=?),
  (SELECT count(*) FROM game_tags relation WHERE relation.game_id=game.id AND relation.tag_id=?)
FROM games game JOIN game_metadata_revisions metadata ON metadata.id=game.current_metadata_revision_id
WHERE metadata.source_kind='SERVER_PEGASUS_IMPORT'`, mappedTag.TagID, externalTag.TagID).
		Scan(&gameID, &title, &assetCount, &gameMappedTags, &gameExternalTags); err != nil {
		t.Fatal(err)
	}
	if gameID == "" || title != "Published Fixture" || assetCount != 2 || gameMappedTags != 1 || gameExternalTags != 0 {
		t.Fatalf("published game = %q/%q assets=%d tags=%d/%d", gameID, title, assetCount, gameMappedTags, gameExternalTags)
	}
	decided, err := service.Get(ctx, created.ID)
	if err != nil || decided.Counts.ReviewPending != 0 || decided.Counts.Published != 1 ||
		decided.Counts.ReviewDiscarded != 1 {
		t.Fatalf("review decisions = %#v, error=%v", decided, err)
	}
}

func TestRecoverWorkClosesExhaustedLeaseAsFailed(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := testsupport.SeedPlatformInstances(ctx, database.SQL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	const importID = "01980000-0000-7000-8000-000000000820"
	const scanJobID = "01980000-0000-7000-8000-000000000821"
	const workJobID = "01980000-0000-7000-8000-000000000822"
	const userID = "01980000-0000-7000-8000-000000000823"
	now := time.UnixMilli(1_786_000_000_000)
	if _, err := database.SQL.Exec(`INSERT INTO profiles(id,display_name,created_at_ms) VALUES('pegasus-recovery-profile','Recovery',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.Exec(`
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'pegasus-recovery-profile','pegasus-recovery','Recovery','ADMIN','ENABLED',1,1)
`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.Exec(`
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,version,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_SCAN',?,1,'{}',1,'SUCCEEDED',1,4,1,1,1,1,1)
`, scanJobID, importID, strings.Repeat("1", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.Exec(`
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,version,available_at_ms,execution_started_at_ms,execution_deadline_at_ms,leased_until_ms,heartbeat_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_IMPORT',?,1,'{}',1,'RUNNING',4,4,1,1,1,?,?,?,1,1)
`, workJobID, importID, strings.Repeat("2", 64), now.Add(time.Hour).UnixMilli(), now.Add(-time.Second).UnixMilli(), now.Add(-time.Second).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.Exec(`
INSERT INTO pegasus_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,phase,scan_job_id,import_job_id,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)
VALUES(?,'games','Games','',?,'RUNNING','COPYING_CONTENT',?,?,?,1,1,?)
`, importID, strings.Repeat("3", 64), scanJobID, workJobID, userID, now.Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	service := &Service{database: database.SQL, now: func() time.Time { return now }}
	if err := service.recoverWork(ctx); err != nil {
		t.Fatal(err)
	}
	var aggregateState, jobState, aggregateCode, jobCode string
	var completedAt int64
	if err := database.SQL.QueryRow(`SELECT import.state,job.state,import.last_error_code,job.error_code,import.completed_at_ms FROM pegasus_imports import JOIN jobs job ON job.id=import.import_job_id WHERE import.id=?`, importID).Scan(&aggregateState, &jobState, &aggregateCode, &jobCode, &completedAt); err != nil {
		t.Fatal(err)
	}
	if aggregateState != "FAILED" || jobState != "FAILED" || aggregateCode != "PEGASUS_WORKER_ATTEMPTS_EXHAUSTED" ||
		jobCode != aggregateCode ||
		completedAt != now.UnixMilli() {
		t.Fatalf(
			"recovered state = aggregate:%s job:%s codes:%s/%s completed:%d",
			aggregateState,
			jobState,
			aggregateCode,
			jobCode,
			completedAt,
		)
	}
}

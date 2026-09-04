package emulationstationimport

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
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
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

type emulationStationTestExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func mustExecEmulationStationTest(
	t *testing.T,
	execer emulationStationTestExecer,
	query string,
	arguments ...any,
) {
	t.Helper()
	_, err := execer.ExecContext(t.Context(), query, arguments...)
	testassert.False(t, err != nil, err)
}

func TestScanMapImportCreatesReviewsAndReleasesTerminalSourcePayload(t *testing.T) {
	ctx := t.Context()
	dataDirectory := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDirectory, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	testassert.False(t, testsupport.SeedPlatformInstances(ctx, database.SQL) != nil, "seed platform instances")
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	testassert.False(t, testsupport.SeedRuntimeProviders(ctx, database.SQL, dependencySet.RuntimeCatalog) != nil, "seed runtime providers")
	testassert.False(t, dependencySet.Bootstrap(ctx, database.SQL, time.Now()) != nil, "bootstrap dependencies")
	const userID = "01980000-0000-7000-8000-000000000840"
	mustExecEmulationStationTest(t, database.SQL, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('emulationstation-profile','ES Test',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'emulationstation-profile','es-test','ES Test','ADMIN','ENABLED',1,1)`, userID)
	ctx = authn.WithPrincipal(ctx, authn.Principal{UserID: userID, ProfileID: "emulationstation-profile", Role: "ADMIN"})

	sourceRoot := createEmulationStationIntegrationSource(t, dataDirectory)
	blobs, err := blobstore.Open(dataDirectory)
	testassert.False(t, err != nil, err)
	for _, name := range []string{"cover.png", "video.webm"} {
		payload, readErr := os.ReadFile(filepath.Join(sourceRoot, name))
		testassert.False(t, readErr != nil, readErr)
		metadata, putErr := blobs.Put(bytes.NewReader(payload))
		testassert.False(t, putErr != nil, putErr)
		_, recordErr := blobstore.EnsureRecord(
			ctx,
			database.SQL,
			metadata,
			"application/octet-stream",
			time.Now().UnixMilli(),
		)
		testassert.False(t, recordErr != nil, recordErr)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDirectory)
	testassert.False(t, err != nil, err)
	importer := libraryimport.New(database.SQL, time.Now).WithBlobStore(blobs)
	service := New(
		database.SQL,
		blobs,
		importer,
		credentials,
		[]config.ServerImportRoot{{ID: "games", Label: "Games", Path: sourceRoot, CanonicalPath: sourceRoot}},
		time.Now,
	)
	created, err := service.Create(ctx, CreateRequest{RootID: "games", SourceRelativePath: ""}, userID)
	testassert.False(t, err != nil, err)
	scanWork, found := service.claim(ctx)
	testassert.True(t, found, "scan work was not claimable")
	service.execute(ctx, scanWork)
	scanned, err := service.Get(ctx, created.ID)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return scanned.State != "AWAITING_MAPPING" },
		func() bool { return scanned.Counts.Games != 2 },
		func() bool { return scanned.Counts.Covers != 1 },
		func() bool { return scanned.Counts.Videos != 1 },
	), "scanned summary = %#v, error = %v", scanned, err)
	collections, err := service.Collections(ctx, created.ID, "", "", 10)
	testassert.Falsef(t, err != nil || len(collections) != 1, "collections = %#v, error = %v", collections, err)
	var targetID string
	testassert.False(t, database.SQL.QueryRowContext(ctx, `
SELECT id FROM platform_instances WHERE platform_id='nes' AND enabled=1 ORDER BY sort_order,id LIMIT 1
`).Scan(&targetID) != nil, "resolve NES platform instance")
	mapped, err := service.UpdateMappings(ctx, created.ID, scanned.Version, []Mapping{{
		CollectionID: collections[0].ID, Action: "IMPORT", PlatformInstanceID: targetID, TagIDs: []string{},
	}})
	testassert.False(t, err != nil, err)
	_, err = service.StartImport(ctx, created.ID, mapped.Version)
	testassert.False(t, err != nil, err)
	importWork, found := service.claim(ctx)
	testassert.True(t, found, "import work was not claimable")
	service.execute(ctx, importWork)
	finished, err := service.Get(ctx, created.ID)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return finished.State != "COMPLETED" },
		func() bool { return finished.Counts.ReviewPending != 2 },
		func() bool { return finished.Counts.Published != 0 },
	), "finished summary = %#v, error = %v", finished, err)

	var gameCount int
	testassert.False(t, database.SQL.QueryRowContext(ctx, `SELECT count(*) FROM games`).Scan(&gameCount) != nil, "count games")
	testassert.Falsef(t, gameCount != 0, "worker published %d games before review", gameCount)
	assertEmulationStationReviewHandoffIdempotent(ctx, t, database.SQL, importer, created.ID)
	preview, err := importer.PreviewReviewBulk(ctx, libraryimport.ReviewBulkScope{
		EmulationStationImportID: created.ID,
	})
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return preview.Counts.Matched != 2 },
		func() bool { return preview.Counts.StrictReady != 1 },
		func() bool { return preview.Counts.SourceFlagged != 1 },
	), "EmulationStation bulk preview = %#v, error = %v", preview.Counts, err)
	var publishItemID, discardItemID string
	var publishVersion, discardVersion int64
	testassert.False(t, database.SQL.QueryRowContext(ctx, `
SELECT item.library_import_item_id,draft.version
FROM emulationstation_import_items item
JOIN review_drafts draft ON draft.import_item_id=item.library_import_item_id
WHERE item.import_id=? AND item.title='Publish fixture'
`, created.ID).Scan(&publishItemID, &publishVersion) != nil, "resolve publish review")
	testassert.False(t, database.SQL.QueryRowContext(ctx, `
SELECT item.library_import_item_id,draft.version
FROM emulationstation_import_items item
JOIN review_drafts draft ON draft.import_item_id=item.library_import_item_id
WHERE item.import_id=? AND item.title='Discard fixture'
`, created.ID).Scan(&discardItemID, &discardVersion) != nil, "resolve discard review")
	_, err = importer.Approve(ctx, publishItemID, publishVersion)
	testassert.False(t, err != nil, err)
	_, err = importer.Discard(ctx, discardItemID, discardVersion, "not suitable")
	testassert.False(t, err != nil, err)

	var gameID, metadataSource, contentSource string
	testassert.False(t, database.SQL.QueryRowContext(ctx, `
SELECT game.id,game.metadata_source_kind,game.content_source_kind
FROM games game
`).Scan(&gameID, &metadataSource, &contentSource) != nil, "resolve published game")
	testassert.Falsef(t,
		metadataSource != "SERVER_EMULATIONSTATION_IMPORT" || contentSource != metadataSource,
		"published sources = %q/%q", metadataSource, contentSource,
	)
	decided, err := service.Get(ctx, created.ID)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return decided.Counts.ReviewPending != 0 },
		func() bool { return decided.Counts.Published != 1 },
		func() bool { return decided.Counts.ReviewDiscarded != 1 },
	), "decision summary = %#v, error = %v", decided, err)
	assertEmulationStationPayloadReleased(t, database.SQL, blobs, created.ID, gameID)
}

func assertEmulationStationReviewHandoffIdempotent(
	ctx context.Context,
	t *testing.T,
	database *sql.DB,
	importer *libraryimport.Service,
	importID string,
) {
	t.Helper()
	var sourceItemID, targetID, relativePath, blobID, libraryItemID string
	var sizeBytes int64
	testassert.False(t, database.QueryRowContext(ctx, `
SELECT item.id,collection.target_platform_instance_id,file.relative_path,file.blob_id,
file.size_bytes,item.library_import_item_id
FROM emulationstation_import_items item
JOIN emulationstation_import_collections collection ON collection.id=item.collection_id
JOIN emulationstation_import_item_files file ON file.item_id=item.id AND file.ordinal=0
WHERE item.import_id=?
ORDER BY item.id LIMIT 1
`, importID).Scan(
		&sourceItemID,
		&targetID,
		&relativePath,
		&blobID,
		&sizeBytes,
		&libraryItemID,
	) != nil, "read source handoff identity")
	result, err := importer.CreateServerSourceOnce(
		ctx,
		"SERVER_EMULATIONSTATION_IMPORT:"+sourceItemID,
		targetID,
		"STANDARD",
		[]libraryimport.ServerSourceFile{{
			RelativePath: relativePath,
			BlobID:       blobID,
			SizeBytes:    sizeBytes,
		}},
		[]string{},
		"01980000-0000-7000-8000-000000000840",
	)
	testassert.False(t, err != nil, err)
	found := false
	for _, item := range result.Items {
		found = found || item.ItemID == libraryItemID
	}
	testassert.True(t, found, "idempotent handoff did not return the original review item")
	var importJobs int
	testassert.False(t, database.QueryRowContext(ctx, `SELECT count(*) FROM import_jobs`).Scan(&importJobs) != nil,
		"count library imports")
	testassert.Falsef(t, importJobs != 2, "idempotent handoff created %d library imports", importJobs)
}

func createEmulationStationIntegrationSource(t *testing.T, dataDirectory string) string {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	root := filepath.Join(dataDirectory, "source")
	writeScanFile(t, root, "publish.nes", []byte("deterministic publish fixture"))
	writeScanFile(t, root, "discard.nes", []byte("deterministic discard fixture"))
	cover, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
	)
	testassert.False(t, err != nil, err)
	writeScanFile(t, root, "cover.png", cover)
	video, err := os.ReadFile(filepath.Join(
		repositoryRoot,
		"testdata/public-roms/gba-smoke/emulationstation-smoke-video.webm",
	))
	testassert.False(t, err != nil, err)
	writeScanFile(t, root, "video.webm", video)
	writeScanFile(t, root, "gamelist.xml", []byte(`<gameList>
<game><path>./publish.nes</path><name>Publish fixture</name><image>./cover.png</image><video>./video.webm</video></game>
<game><path>./discard.nes</path><name>Discard fixture</name><hidden>true</hidden></game>
</gameList>`))
	return root
}

func assertEmulationStationPayloadReleased(
	t *testing.T,
	database *sql.DB,
	blobs *blobstore.Store,
	importID, gameID string,
) {
	t.Helper()
	releases, err := payloadrelease.New(database, blobs, time.Now, 7*24*time.Hour)
	testassert.False(t, err != nil, err)
	for attempt := 0; attempt < 12; attempt++ {
		worked, runErr := releases.RunOnce(t.Context())
		testassert.False(t, runErr != nil, runErr)
		if !worked {
			break
		}
	}
	var releasedSource, releasedImports, sourceBlobRefs, gamePayloadRows int64
	testassert.False(t, database.QueryRowContext(t.Context(), `
SELECT
 (SELECT count(*) FROM emulationstation_import_items WHERE import_id=? AND payload_state='RELEASED'),
 (SELECT count(*) FROM import_items WHERE id IN (
   SELECT library_import_item_id FROM emulationstation_import_items WHERE import_id=?) AND payload_state='RELEASED'),
 (SELECT count(*) FROM emulationstation_import_item_files file
   JOIN emulationstation_import_items item ON item.id=file.item_id
   WHERE item.import_id=? AND (file.blob_id IS NOT NULL OR file.source_archive_blob_id IS NOT NULL))+
 (SELECT count(*) FROM emulationstation_import_item_assets asset
   JOIN emulationstation_import_items item ON item.id=asset.item_id
   WHERE item.import_id=? AND asset.blob_id IS NOT NULL),
 (SELECT count(*) FROM game_files file WHERE file.game_id=?)+
 (SELECT count(*) FROM game_assets WHERE game_id=?)
`, importID, importID, importID, importID, gameID, gameID).Scan(
		&releasedSource, &releasedImports, &sourceBlobRefs, &gamePayloadRows,
	) != nil, "read payload release state")
	testassert.Falsef(t, testassert.Any(
		func() bool { return releasedSource != 2 },
		func() bool { return releasedImports != 2 },
		func() bool { return sourceBlobRefs != 0 },
		func() bool { return gamePayloadRows != 3 },
	), "released payload = source:%d imports:%d refs:%d game:%d",
		releasedSource, releasedImports, sourceBlobRefs, gamePayloadRows)
}

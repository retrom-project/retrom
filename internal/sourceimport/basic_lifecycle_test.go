package sourceimport

import (
	"os"
	"path/filepath"
	"testing"

	dbapi "retrom/internal/database"
	application "retrom/internal/service/sourceimport"
)

func TestBasicUsesSourceReceiveAndOrdinaryReview(t *testing.T) {
	fixture := newSourceLifecycle(t, "BASIC")
	ctx, service := fixture.ctx, fixture.service
	mapped := mapBasicLifecycle(t, fixture)
	if _, err := service.StartImport(ctx, mapped.ID, mapped.Version); err != nil {
		t.Fatal(err)
	}
	// A file changed after discovery must use the common receive failure path.
	writeFixture(t, filepath.Join(fixture.dataDir, "source", "discard.nes"), []byte("changed after scan"))
	service.execute(ctx, mustClaimSource(t, service))
	reviewID := assertBasicReceiveResults(t, fixture, mapped.ID)
	assertNoSourceGames(ctx, t, fixture.database)
	var version int64
	mustScanSourceTest(t, dbapi.QueryRowContext(ctx, fixture.database.SQL,
		`SELECT review_version FROM import_items WHERE id=?`, reviewID), &version)
	if _, err := fixture.importer.Approve(ctx, reviewID, version); err != nil {
		t.Fatal(err)
	}
	var games, tags int
	mustScanSourceTest(t, dbapi.QueryRowContext(ctx, fixture.database.SQL,
		`SELECT (SELECT count(*) FROM games),(SELECT count(*) FROM game_tags WHERE tag_id=?)`, fixture.mappedTag.TagID),
		&games, &tags)
	if games != 1 || tags != 1 {
		t.Fatalf("ordinary approval/tag inheritance missing: games=%d tags=%d", games, tags)
	}
}

func TestBasicDirectoryDiscoveryRejectsSymlinkFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	writeFixture(t, filepath.Join(root, "local.nes"), []byte("local"))
	writeFixture(t, filepath.Join(outside, "outside.nes"), []byte("outside"))
	if err := os.Symlink(filepath.Join(outside, "outside.nes"), filepath.Join(root, "link.nes")); err != nil {
		t.Fatal(err)
	}
	source := scanSource{root: Root{path: root}, acquire: (&Service{}).acquireSourceReader}
	result, err := application.ScanOrganized(t.Context(), source, "BASIC", ".nes", 2026)
	if err != nil || len(result.Items) != 1 || result.Items[0].Files[0].Path != "local.nes" {
		t.Fatalf("basic followed a symlink: %#v %v", result, err)
	}
}

func scanBasicLifecycle(t *testing.T, fixture sourceLifecycle) Summary {
	t.Helper()
	ctx, service := fixture.ctx, fixture.service
	created, err := service.Create(ctx, CreateRequest{
		Format: "BASIC", ExtensionFilter: " .NES ; .nes ", RootID: "games",
	}, "01980000-0000-7000-8000-000000000800")
	if err != nil || created.ExtensionFilter != ".nes" {
		t.Fatalf("create basic: %#v %v", created, err)
	}
	unit := mustClaimSource(t, service)
	if unit.Format != "BASIC" || unit.ExtensionFilter != ".nes" {
		t.Fatalf("scan lost frozen filter: %#v", unit)
	}
	service.execute(ctx, unit)
	scanned, err := service.Get(ctx, created.ID)
	if err != nil || scanned.State != "AWAITING_MAPPING" || scanned.Counts.Metadata != 0 || scanned.Counts.Games != 2 {
		t.Fatalf("basic scan: %#v %v", scanned, err)
	}
	return scanned
}

func mapBasicLifecycle(t *testing.T, fixture sourceLifecycle) Summary {
	t.Helper()
	ctx, service := fixture.ctx, fixture.service
	scanned := scanBasicLifecycle(t, fixture)
	collections, err := service.Collections(ctx, scanned.ID, "", 0, "", 10)
	if err != nil || len(collections) != 1 || collections[0].MappingAction != nil {
		t.Fatalf("basic mapping must be explicit: %#v %v", collections, err)
	}
	var target string
	mustScanSourceTest(t, dbapi.QueryRowContext(ctx, fixture.database.SQL,
		`SELECT id FROM platform_instances WHERE platform_id='nes' AND enabled=1 ORDER BY created_at_ms,id LIMIT 1`), &target)
	mapped, err := service.UpdateMappings(ctx, scanned.ID, scanned.Version, []Mapping{{
		CollectionID: collections[0].ID, Action: "IMPORT", PlatformInstanceID: target, TagIDs: []string{fixture.mappedTag.TagID},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return mapped
}

func assertBasicReceiveResults(t *testing.T, fixture sourceLifecycle, importID string) string {
	t.Helper()
	ctx, service := fixture.ctx, fixture.service
	items, err := service.Items(ctx, importID, "", "", "", "", "", "", 10)
	if err != nil || len(items) != 2 {
		t.Fatalf("basic results: %#v %v", items, err)
	}
	var reviewID string
	var changed bool
	for _, item := range items {
		if item.Title == "fixture" && item.ExecutionState == "REVIEW_PENDING" && item.ReviewItemID != nil {
			reviewID = *item.ReviewItemID
		}
		changed = changed || item.Title == "discard" && item.ExecutionState == "SOURCE_CHANGED"
	}
	if reviewID == "" || !changed {
		t.Fatalf("common handoff or source-change protection missing: %#v", items)
	}
	return reviewID
}

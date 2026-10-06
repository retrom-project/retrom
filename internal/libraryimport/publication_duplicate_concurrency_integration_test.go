//go:build integration

package libraryimport

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
)

func TestConcurrentPublicationAcrossIndependentFileStoresSkipsExisting(t *testing.T) {
	t.Parallel()
	fixture, source := ownedSourceFixture(t)
	first, err := fixture.service.CreateServerSource(fixture.ctx, fixture.platform, "STANDARD", source.Files, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.CreateServerSource(fixture.ctx, fixture.platform, "STANDARD", source.Files, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	record, err := filestore.ParseRecord(source.Files[0].FileRecord)
	if err != nil {
		t.Fatal(err)
	}
	root := strings.TrimSuffix(fixture.blobs.Path(source.Files[0].FileRecord), filepath.FromSlash(record.Path))
	otherFiles, err := filestore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	other := newTestImporter(t, fixture.database, otherFiles, testImportOptions{Now: ownedSourceNow})
	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	type outcome struct {
		value Approved
		err   error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	for i, importer := range []*Service{fixture.service, other} {
		itemID := []string{first.Items[0].ItemID, second.Items[0].ItemID}[i]
		go func() { <-start; value, err := importer.Approve(ctx, itemID, 1); results <- outcome{value, err} }()
	}
	close(start)
	statuses := map[string]int{}
	gameID := ""
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		statuses[result.value.Status]++
		if gameID != "" && gameID != result.value.GameID {
			t.Fatal("concurrent duplicate created separate games")
		}
		gameID = result.value.GameID
	}
	if statuses["PUBLISHED"] != 1 || statuses["SKIPPED_EXISTING"] != 1 {
		t.Fatalf("outcomes=%v", statuses)
	}
	var games, matches int
	if err := dbapi.QueryRowContext(ctx, fixture.database, `SELECT (SELECT count(*) FROM games),
 (SELECT count(*) FROM import_item_duplicate_matches WHERE detected_stage='REVIEW')`).Scan(&games, &matches); err != nil {
		t.Fatal(err)
	}
	if games != 1 || matches != 1 {
		t.Fatalf("games=%d matches=%d", games, matches)
	}
}

func TestReviewDuplicateUpdatesSourceAndImportCountsAndReplays(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	original, err := fixture.service.CreateServerSource(fixture.ctx, fixture.platform, "STANDARD", request.Files, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	owned, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	finishOwnedReviewHandoff(t, fixture, request)
	published, err := fixture.service.Approve(fixture.ctx, original.Items[0].ItemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	item := owned.Items[0].ItemID
	for range 2 {
		skipped, err := fixture.service.Approve(fixture.ctx, item, 1)
		if err != nil || skipped.Status != "SKIPPED_EXISTING" || skipped.GameID != published.GameID {
			t.Fatalf("skip=%+v err=%v", skipped, err)
		}
	}
	var state, gameID, payload string
	var sourcePending, sourceExisting, importedExisting int
	err = dbapi.QueryRowContext(fixture.ctx, fixture.database, `SELECT source.execution_state,source.existing_game_id,
 parent.review_pending_item_count,parent.existing_item_count,item.payload_state,job.already_imported_item_count
 FROM source_import_items source JOIN source_imports parent ON parent.id=source.import_id
 JOIN import_items item ON item.id=? JOIN import_jobs job ON job.id=item.import_job_id
 WHERE source.id=?`, item, request.Intent.ItemID).Scan(&state, &gameID, &sourcePending, &sourceExisting, &payload, &importedExisting)
	if err != nil {
		t.Fatal(err)
	}
	if state != "SKIPPED_EXISTING" || gameID != published.GameID || sourcePending != 0 || sourceExisting != 1 || importedExisting != 1 || payload != "RELEASING" {
		t.Fatalf("source=%s game=%s pending=%d existing=%d/%d payload=%s", state, gameID, sourcePending, sourceExisting, importedExisting, payload)
	}
}

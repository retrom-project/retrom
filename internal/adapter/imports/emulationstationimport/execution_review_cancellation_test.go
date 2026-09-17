package emulationstationimport

import (
	"encoding/json"
	"testing"

	"retrom/internal/adapter/integration/libraryimport"
)

func TestESLiveCancellationPreservesInterruptedReservedReview(t *testing.T) {
	for _, checkpoint := range []string{"reserved", "attached", "metadata"} {
		t.Run(checkpoint, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			started, unit := startLifecycleImport(t, fixture, "", "nes")
			item, imported := reserveExecutionReview(t, fixture, unit)
			metadata := prepareCancellationCheckpoint(t, fixture, unit, item, imported, checkpoint)
			requestExecutionCancellation(t, fixture, started.ID)
			closed, err := fixture.service.closeCancelled(fixture.context, unit)
			if err != nil || !closed {
				t.Fatalf("close=%v error=%v", closed, err)
			}
			var sourceState, ordinaryState, title, sourceLibraryID string
			var games, ordinaryItems int
			err = fixture.database.QueryRowContext(fixture.context, `SELECT source.execution_state,
COALESCE(source.library_import_item_id,''),item.state,json_extract(draft.metadata_json,'$.title'),
(SELECT count(*) FROM games),(SELECT count(*) FROM import_items)
FROM emulationstation_import_items source,import_items item
JOIN review_drafts draft ON draft.import_item_id=item.id
WHERE source.id=? AND item.id=?`, item.ID, imported.Items[0].ItemID).Scan(
				&sourceState, &sourceLibraryID, &ordinaryState, &title, &games, &ordinaryItems,
			)
			if err != nil || sourceState != "REVIEW_PENDING" || sourceLibraryID != imported.Items[0].ItemID ||
				ordinaryState != "REVIEW_PENDING" || title != metadata.Title || games != 0 || ordinaryItems != 1 {
				t.Fatalf("reserved review source=%s/%s ordinary=%s title=%s games=%d items=%d error=%v",
					sourceState, sourceLibraryID, ordinaryState, title, games, ordinaryItems, err)
			}
		})
	}
}

func reserveExecutionReview(
	t *testing.T,
	fixture lifecycleFixture,
	unit work,
) (executionItem, libraryimport.ServerImportResult) {
	t.Helper()
	item, found, err := fixture.service.nextItem(fixture.context, unit)
	if err != nil || !found {
		t.Fatalf("item=%v error=%v", found, err)
	}
	if !fixture.service.copyExecutionFiles(fixture.context, unit, fixture.service.roots[unit.RootID], &item) {
		t.Fatal("copy source files")
	}
	files := make([]libraryimport.ServerSourceFile, 0, len(item.Files))
	for _, file := range item.Files {
		files = append(
			files,
			libraryimport.ServerSourceFile{RelativePath: file.Path, BlobID: file.BlobID, SizeBytes: file.Size},
		)
	}
	result, err := fixture.service.importer.CreateServerSourceOnce(
		fixture.context,

		"SERVER_EMULATIONSTATION_IMPORT:"+item.ID,
		item.TargetPlatformID,
		"STANDARD",
		files,
		item.TagIDs,
		unit.CreatedByUserID,
	)
	if err != nil || len(result.Items) != 1 || result.Items[0].State != "REVIEW_PENDING" {
		t.Fatalf("reserved=%#v error=%v", result, err)
	}
	return item, result
}

func prepareCancellationCheckpoint(
	t *testing.T, fixture lifecycleFixture, unit work, item executionItem, imported libraryimport.ServerImportResult, checkpoint string,
) libraryimport.ServerMetadata {
	t.Helper()
	if checkpoint != "reserved" {
		if err := fixture.service.attachLibraryResult(
			fixture.context,
			item.ID,
			imported.Created.ImportJobID,
			imported.Items[0],
		); err != nil {
			t.Fatal(
				err,
			)
		}
	}
	var metadata libraryimport.ServerMetadata
	if err := json.Unmarshal([]byte(item.MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if checkpoint == "metadata" {
		if _, _, err := fixture.service.importer.SeedServerReviewMetadataAtYear(
			fixture.context,
			imported.Items[0].ItemID,
			metadata,
			unit.ReleaseYearMax,
		); err != nil {
			t.Fatal(
				err,
			)
		}
	}
	return metadata
}

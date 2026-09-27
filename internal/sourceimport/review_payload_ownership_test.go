package sourceimport

import (
	"testing"

	"retrom/internal/composition/cleanupjobs"
	dbapi "retrom/internal/database"
	media "retrom/internal/persistence/mediaaccess"
	access "retrom/internal/service/mediaaccess"
)

func TestReviewOwnsMediaAfterIndependentSourceReleaseAndHandoffReplay(t *testing.T) {
	service, unit, item := handoffFixture(t)
	seedHandoffMedia(t, service)
	if err := completeHandoff(t.Context(), service, unit, item); err != nil {
		t.Fatal(err)
	}
	releases, err := cleanupjobs.New(t.Context(), service.database, nil, service.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releases.Close)
	if ran, err := releases.RunOnce(t.Context()); err != nil || !ran {
		t.Fatalf("release Source: ran=%v error=%v", ran, err)
	}
	var sourceState, itemState string
	var sourceRefs, itemRefs, count int
	err = dbapi.QueryRowContext(t.Context(), service.database, `SELECT
 source.payload_state,item.payload_state,
 (SELECT count(*) FROM source_import_item_assets WHERE item_id=source.id AND blob_id IS NOT NULL),
 (SELECT count(*) FROM import_item_assets WHERE import_item_id=item.id),
 (SELECT owner_kind='IMPORT_ITEM' AND owner_id=item.id AND retired_at_ms IS NULL FROM stored_files WHERE id='handoff-media')
 FROM source_import_items source JOIN import_items item ON item.id=source.library_import_item_id
 WHERE source.id='item'`).Scan(&sourceState, &itemState, &sourceRefs, &itemRefs, &count)
	if err != nil || sourceState != "RELEASED" || itemState != "RETAINED" || sourceRefs != 0 || itemRefs != 2 || count != 1 {
		t.Fatalf("ownership after release: %s/%s refs=%d/%d count=%d error=%v",
			sourceState, itemState, sourceRefs, itemRefs, count, err)
	}
	for _, kind := range []string{"COVER", "VIDEO"} {
		if _, err := access.New(media.New(service.database)).Review(t.Context(), "item", kind); err != nil {
			t.Fatalf("review lost %s after Source release: %v", kind, err)
		}
	}
	before := readHandoffState(t, service)
	if err := completeHandoff(t.Context(), service, unit, item); err != nil {
		t.Fatal(err)
	}
	if after := readHandoffState(t, service); after != before {
		t.Fatalf("handoff replay reacquired Source payload: before=%#v after=%#v", before, after)
	}
}

package sourceimport

import (
	"testing"

	sourceimportrepo "retrom/internal/persistence/sourceimport"
)

func TestSourceMediaReflectsMaterialLifecycle(t *testing.T) {
	t.Parallel()
	db := newSourceRetryDatabase(t)
	for _, state := range []struct{ asset, expected string }{
		{"DISCOVERED", "PENDING"},
		{"COPIED", "READY"},
		{"MISSING", "MISSING"},
		{"INVALID", "WARNING"},
		{"READ_FAILED", "WARNING"},
		{"SOURCE_CHANGED", "WARNING"},
		{"RELEASED", "RELEASED"},
	} {
		t.Run(state.asset, func(t *testing.T) {
			if _, err := db.ExecContext(t.Context(), `
INSERT INTO source_import_item_assets(item_id,kind,resolution_method,relative_path,state,payload_released_at_ms,created_at_ms,updated_at_ms)
VALUES('018fbe68-0000-7000-8000-000000000010','COVER','EXPLICIT_GAME','cover.png',?,CASE WHEN ?='RELEASED' THEN 2 ELSE NULL END,1,2)
ON CONFLICT(item_id,kind) DO UPDATE SET state=excluded.state,payload_released_at_ms=excluded.payload_released_at_ms`, state.asset, state.asset); err != nil {
				t.Fatal(err)
			}
			items, err := (&Service{database: db}).Items(t.Context(), "import", "", "", "", "", "", "", 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != 1 || items[0].Media.Cover != state.expected || items[0].Media.Video != "MISSING" {
				t.Fatalf("%s projected as %+v", state.asset, items)
			}
		})
	}
}

func TestMediaWarningsUseAssetFieldsAcrossSourcePhases(t *testing.T) {
	t.Parallel()
	db := newSourceRetryDatabase(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE source_import_items SET warnings_json=?
WHERE id='018fbe68-0000-7000-8000-000000000010'`, `[
 {"code":"SOURCE_SOURCE_CHANGED","field":"cover"},
 {"code":"SOURCE_SOURCE_CHANGED","field":"video"},
 {"code":"PEGASUS_IMAGE_INVALID","field":"cover"},
 {"code":"SOURCE_SOURCE_CHANGED","field":"file"},
 {"code":"PEGASUS_METADATA_INVALID","field":"title"}
]`); err != nil {
		t.Fatal(err)
	}
	if err := sourceimportrepo.RefreshCountsAndEvent(t.Context(), db, "work", "import",
		"018fbe68-0000-7000-8000-000000000010", "REVIEW_PENDING", 3); err != nil {
		t.Fatal(err)
	}
	service := &Service{database: db}
	summary, err := service.Get(t.Context(), "import")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Counts.MediaWarnings != 3 {
		t.Errorf("media warnings = %d, want 3", summary.Counts.MediaWarnings)
	}
	items, err := service.Items(t.Context(), "import", "", "", "SOURCE_SOURCE_CHANGED", "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Media.Cover != "WARNING" || items[0].Media.Video != "WARNING" {
		t.Fatalf("media warning filter/projection = %+v", items)
	}
}

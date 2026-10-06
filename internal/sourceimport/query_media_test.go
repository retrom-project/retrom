package sourceimport

import "testing"

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

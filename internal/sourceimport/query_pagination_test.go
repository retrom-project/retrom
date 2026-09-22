package sourceimport

import (
	"fmt"
	"reflect"
	"testing"
)

func TestItemCursorPreservesTiedTitlesAndWarningFilter(t *testing.T) {
	t.Parallel()
	db := newSourceRetryDatabase(t)
	seedQueryCollection(t, db)
	if _, err := db.ExecContext(t.Context(), `UPDATE source_import_items SET title='Same title' WHERE id='item'`); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 2; index++ {
		if _, err := db.ExecContext(t.Context(), `
INSERT INTO source_import_items(id,import_id,collection_id,metadata_relative_path,game_ordinal,source_key,title,
discovery_state,execution_state,metadata_json,source_manifest_json,source_manifest_digest,
error_code,error_details_json,retryable,completed_at_ms,version,created_at_ms,updated_at_ms,warnings_json)
SELECT ?,'import','collection','metadata.pegasus.txt',?,?,'Same title',discovery_state,execution_state,
metadata_json,source_manifest_json,source_manifest_digest,error_code,error_details_json,retryable,completed_at_ms,
version,created_at_ms,updated_at_ms,'[{"code":"PEGASUS_IMAGE_INVALID","field":"cover"}]'
FROM source_import_items WHERE id='item'`, fmt.Sprintf("item-%d", index), index, fmt.Sprintf("%064x", index)); err != nil {
			t.Fatal(err)
		}
	}
	service := &Service{database: db}
	seen := make([]string, 0, 3)
	afterID := ""
	for range 3 {
		values, err := service.Items(t.Context(), "import", "Same", "COMMIT_FAILED", "", "collection", "Same title", afterID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(values) != 1 {
			t.Fatalf("page: %#v", values)
		}
		afterID = values[0].ID
		seen = append(seen, afterID)
	}
	if !reflect.DeepEqual(seen, []string{"item", "item-1", "item-2"}) {
		t.Fatalf("cursor skipped or repeated records: %v", seen)
	}
	values, err := service.Items(t.Context(), "import", "Same", "COMMIT_FAILED", "PEGASUS_IMAGE_INVALID", "collection", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 {
		t.Fatalf("warning filter: %#v", values)
	}
	for _, value := range values {
		if value.Media.Cover != "WARNING" {
			t.Fatalf("media projection: %#v", value.Media)
		}
	}
}

package maintenance

import (
	"database/sql"
	"testing"
)

func restorePayloadFixture(t *testing.T) *sql.DB {
	t.Helper()
	db := restoreReviewFixture(t)
	_, err := db.ExecContext(t.Context(), `INSERT INTO source_import_items(id,import_id,metadata_relative_path,game_ordinal,source_key,title,discovery_state,execution_state,metadata_json,source_manifest_json,source_manifest_digest,created_at_ms,updated_at_ms)
 VALUES('failed-source','import','other',1,'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff','Unfinished','READY','PENDING','{}','{}','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,1);
 UPDATE source_imports SET game_count=2,processable_item_count=2 WHERE id='import'`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

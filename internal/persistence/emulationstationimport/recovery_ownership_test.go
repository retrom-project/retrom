package emulationstationimport

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	application "retrom/internal/service/emulationstationimport"
)

func TestRecoveryNeverDeletesOwnedScanProjection(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"file blob", "asset blob", "item progress", "tag", "mapping", "published snapshot"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			db, _ := recoveryDatabase(t, false, true)
			ownScanProjection(t, db, scenario)
			before := planRows(t, db)
			blobs := planTable(t, db, "blobs")
			if err := application.NewRecovery(NewRecovery(db), func() time.Time { return time.UnixMilli(1500) }).Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) || blobs != planTable(t, db, "blobs") {
				t.Fatal("recovery deleted owned projection")
			}
		})
	}
}

func ownScanProjection(t *testing.T, db *sql.DB, scenario string) {
	t.Helper()
	queries := map[string]string{
		"file blob":     `UPDATE emulationstation_import_item_files SET blob_id='scan-blob',state='COPIED' WHERE item_id='item'`,
		"asset blob":    `UPDATE emulationstation_import_item_assets SET blob_id='scan-blob',state='COPIED' WHERE item_id='item'`,
		"item progress": `UPDATE emulationstation_import_items SET execution_state='COPYING' WHERE id='item'`,
		"tag": `INSERT INTO tags(id,name,name_key,search_text,status,created_by_user_id,updated_by_user_id,created_at_ms,updated_at_ms) VALUES('019b0000-0000-7000-8000-000000000001','Tag','tag','tag','ACTIVE','actor','actor',1,1);
INSERT INTO emulationstation_collection_tags(collection_id,tag_id,assigned_by_user_id,created_at_ms) VALUES('collection','019b0000-0000-7000-8000-000000000001','actor',1)`,
		"mapping":            `UPDATE emulationstation_import_collections SET mapping_action='SKIP' WHERE id='collection'`,
		"published snapshot": `UPDATE emulationstation_imports SET source_snapshot_digest='` + planDigest + `',scan_completed_at_ms=1 WHERE id='import-0'`,
	}
	if scenario == "file blob" || scenario == "asset blob" {
		if _, err := db.ExecContext(t.Context(), `INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms) VALUES('scan-blob','`+planDigest+`',1,'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','aaaaaaaa','application/octet-stream',1)`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(t.Context(), queries[scenario]); err != nil {
		t.Fatal(err)
	}
}

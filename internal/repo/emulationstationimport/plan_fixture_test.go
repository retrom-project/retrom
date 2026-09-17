package emulationstationimport

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	application "retrom/internal/model/emulationstationimport"
	"retrom/internal/repo/recordstore"
)

const planDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func planDatabase(t *testing.T) (*sql.DB, application.Summary) {
	t.Helper()
	db := creationDatabase(t)
	plan := creationPlan(0)
	if err := NewCreation(db).WithCreate(t.Context(), func(writer application.CreationWriter) error { _, err := writer.Insert(t.Context(), plan); return err }); err != nil {
		t.Fatal(err)
	}
	seedPlanProjection(t, db)
	if _, err := recordstore.UpdateEmulationstationImports(t.Context(), db, recordstore.Update{
		Set: `state='AWAITING_MAPPING',phase=NULL,source_snapshot_digest=?,scan_completed_at_ms=2,
 gamelist_count=1,collection_count=1,game_count=1,processable_item_count=1,version=version+1,updated_at_ms=2`,
		Values: []any{planDigest}, Scope: recordstore.Scope{Where: `id='import-0'`},
	}); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO tags(id,name,name_key,search_text,status,created_by_user_id,updated_by_user_id,created_at_ms,updated_at_ms) VALUES('019b0000-0000-7000-8000-000000000001','Tag','tag','tag','ACTIVE','actor','actor',1,1)`,
		`INSERT INTO emulationstation_collection_tags(collection_id,tag_id,assigned_by_user_id,created_at_ms) VALUES('collection','019b0000-0000-7000-8000-000000000001','actor',2)`,
		`UPDATE jobs SET state='SUCCEEDED',finished_at_ms=2,updated_at_ms=2 WHERE id='job-0'`,
	} {
		if _, err := db.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	value, err := NewQueries(db).Get(t.Context(), plan.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	return db, value
}

func seedPlanProjection(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, query := range []string{
		`INSERT INTO content_kinds(id) VALUES('SINGLE_FILE')`,
		`INSERT INTO emulationstation_import_gamelists(import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,game_count,created_at_ms)
 VALUES('import-0','gamelist.xml',128,'` + planDigest + `','` + planDigest + `','VALID',1,1)`,
		`INSERT INTO emulationstation_import_collections(id,import_id,gamelist_relative_path,relative_directory,display_name,game_count,created_at_ms,updated_at_ms)
 VALUES('collection','import-0','gamelist.xml','','Collection',1,1,1)`,
		`INSERT INTO emulationstation_import_items(id,import_id,collection_id,gamelist_relative_path,game_ordinal,source_key,title,source_flags_json,discovery_state,execution_state,content_kind,metadata_json,source_manifest_json,source_manifest_digest,created_at_ms,updated_at_ms)
 VALUES('item','import-0','collection','gamelist.xml',1,'` + planDigest + `','Game','{"hidden":false,"adult":false,"kidGame":false}','READY','PENDING','SINGLE_FILE',
 '{"schemaVersion":1,"title":"Game","description":"","developer":"","publisher":"","genre":"","players":null,"releaseYear":null}',
 '{"schemaVersion":1,"contentKind":"SINGLE_FILE","files":[{"ordinal":0,"declaredKind":"FILE","relativePath":"game.nes","sizeBytes":16,"sourceFactsDigest":"` + planDigest + `"}]}',
 '` + planDigest + `',1,1)`,
		`INSERT INTO emulationstation_import_item_files(item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,state,created_at_ms,updated_at_ms)
 VALUES('item',0,'FILE','game.nes',16,'` + planDigest + `','DISCOVERED',1,1)`,
		`INSERT INTO emulationstation_import_item_assets(item_id,kind,resolution_method,relative_path,state,created_at_ms,updated_at_ms)
 VALUES('item','COVER','EXPLICIT_IMAGE','cover.png','MISSING',1,1)`,
	} {
		if _, err := db.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
}

func planRows(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, table := range []string{"emulationstation_imports", "emulationstation_import_gamelists", "emulationstation_import_collections", "emulationstation_collection_tags", "emulationstation_import_items", "emulationstation_import_item_files", "emulationstation_import_item_assets", "tags", "jobs", "job_input_snapshots", "job_events", "audit_events"} {
		result[table] = planTable(t, db, table)
	}
	return result
}

func planTable(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), fmt.Sprintf("SELECT * FROM %s ORDER BY rowid", table))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			t.Error(err)
		}
	}()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	records := [][]any{}
	for rows.Next() {
		values := make([]any, len(columns))
		targets := make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		if err := rows.Scan(targets...); err != nil {
			t.Fatal(err)
		}
		records = append(records, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

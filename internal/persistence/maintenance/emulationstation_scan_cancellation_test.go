package maintenance

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/persistence/dbexec"
	"retrom/internal/persistence/store"
)

func restoredScanDatabase(t *testing.T, pending bool) *sql.DB {
	t.Helper()
	owner, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "restore-scan.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	db := owner.SQL
	for _, query := range []string{
		`INSERT INTO profiles(id,display_name,created_at_ms)VALUES('profile','Admin',1)`,
		`INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)VALUES('actor','profile','admin','Admin','ADMIN','ENABLED',1,1)`,
		`INSERT INTO content_kinds(id)VALUES('SINGLE_FILE')`,
		`INSERT INTO jobs(id,scope_type,scope_id,kind,execution_no,dedupe_key,payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)VALUES('scan-job','EMULATIONSTATION_IMPORT','scan','SERVER_EMULATIONSTATION_SCAN',1,'` + strings.Repeat("a", 64) + `','{}',1,'RUNNING',1,4,1,1,1)`,
		`INSERT INTO emulationstation_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,release_year_max,state,phase,scan_job_id,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)VALUES('scan','games','Games','','` + strings.Repeat("a", 64) + `',1971,'SCANNING','PARSING_GAMELISTS','scan-job','actor',1,1,604800001)`,
	} {
		if _, err := db.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	if pending {
		seedRestoredPendingScan(t, db)
	} else {
		for _, query := range []string{
			`INSERT INTO emulationstation_import_gamelists(import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,error_code,created_at_ms)VALUES('scan','gamelist.xml',128,'` + strings.Repeat("a", 64) + `','` + strings.Repeat("a", 64) + `','INVALID','EMULATIONSTATION_XML_INVALID',1)`,
			`UPDATE emulationstation_imports SET gamelist_count=1,invalid_gamelist_count=1 WHERE id='scan'`,
		} {
			if _, err := db.ExecContext(t.Context(), query); err != nil {
				t.Fatal(err)
			}
		}
	}
	return db
}

func seedRestoredPendingScan(t *testing.T, db *sql.DB) {
	t.Helper()
	digest := strings.Repeat("a", 64)
	for _, query := range []string{
		`UPDATE jobs SET state='CANCEL_REQUESTED',cancel_requested_at_ms=2,cancel_reason='Stop' WHERE id='scan-job'`,
		`UPDATE emulationstation_imports SET state='CANCEL_REQUESTED',cancel_reason='Stop' WHERE id='scan'`,
		`INSERT INTO emulationstation_import_gamelists(import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,game_count,created_at_ms)VALUES('scan','gamelist.xml',128,'` + digest + `','` + digest + `','VALID',1,1)`,
		`INSERT INTO emulationstation_import_collections(id,import_id,gamelist_relative_path,relative_directory,display_name,game_count,created_at_ms,updated_at_ms)VALUES('collection','scan','gamelist.xml','','Collection',1,1,1)`,
		`INSERT INTO emulationstation_import_items(id,import_id,collection_id,gamelist_relative_path,game_ordinal,source_key,title,source_flags_json,discovery_state,execution_state,content_kind,metadata_json,source_manifest_json,source_manifest_digest,created_at_ms,updated_at_ms)VALUES('item','scan','collection','gamelist.xml',1,'` + digest + `','Game','{"hidden":false,"adult":false,"kidGame":false}','READY','PENDING','SINGLE_FILE','{"schemaVersion":1,"title":"Game","description":"","developer":"","publisher":"","genre":"","players":null,"releaseYear":null}','{"schemaVersion":1,"contentKind":"SINGLE_FILE","files":[]}','` + digest + `',1,1)`,
		`INSERT INTO emulationstation_import_item_files(item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,state,created_at_ms,updated_at_ms)VALUES('item',0,'FILE','game.nes',16,'` + digest + `','DISCOVERED',1,1)`,
		`INSERT INTO emulationstation_import_item_assets(item_id,kind,resolution_method,relative_path,state,created_at_ms,updated_at_ms)VALUES('item','COVER','EXPLICIT_IMAGE','cover.png','MISSING',1,1)`,
	} {
		if _, err := db.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRestoreClearsUnexecutedScanningAndPendingCancellation(t *testing.T) {
	t.Parallel()
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected diagnostics", true: "pending scan"}[pending], func(t *testing.T) {
			t.Parallel()
			db := restoredScanDatabase(t, pending)
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer dbexec.Rollback(tx)
			count, err := fenceRestoredEmulationStation(t.Context(), tx, 2000)
			if err != nil || count != 1 {
				t.Fatalf("restore count=%d cause=%v", count, err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			assertRestoredScan(t, db)
		})
	}
}

func assertRestoredScan(t *testing.T, db *sql.DB) {
	t.Helper()
	var planState, jobState, code string
	var rows, counts, year, payloads int
	err := db.QueryRowContext(t.Context(), `SELECT plan.state,job.state,plan.last_error_code,
(SELECT count(*) FROM emulationstation_import_items)+(SELECT count(*) FROM emulationstation_import_item_files)+
(SELECT count(*) FROM emulationstation_import_item_assets)+(SELECT count(*) FROM emulationstation_import_collections)+
(SELECT count(*) FROM emulationstation_import_gamelists),
plan.gamelist_count+plan.invalid_gamelist_count+plan.collection_count+plan.game_count+plan.estimated_source_bytes+
plan.processable_item_count+plan.blocked_item_count+plan.failed_item_count,
plan.release_year_max,(SELECT count(*) FROM jobs WHERE kind='PAYLOAD_RELEASE')
FROM emulationstation_imports plan JOIN jobs job ON job.id=plan.scan_job_id WHERE plan.id='scan'`).Scan(&planState, &jobState, &code, &rows, &counts, &year, &payloads)
	if err != nil {
		t.Fatal(err)
	}
	if planState != "FAILED" || jobState != "FAILED" || code != "SERVER_IMPORT_SOURCE_NOT_RESTORED" || rows != 0 || counts != 0 || year != 1971 || payloads != 0 {
		t.Fatalf("restore=%s/%s/%s rows=%d counts=%d year=%d payloads=%d", planState, jobState, code, rows, counts, year, payloads)
	}
}

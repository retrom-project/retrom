package maintenance

import (
	"database/sql"
	"testing"
	"time"

	library "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
	application "retrom/internal/service/maintenance"
)

func restoredEmulationStationReview(t *testing.T, attached, seeded bool) (*sql.DB, string) {
	t.Helper()
	db, path := restoredPegasusReview(t)
	statements := []string{
		`UPDATE pegasus_import_items SET library_import_job_id=NULL,library_import_item_id=NULL,
execution_state='PENDING' WHERE id='item'`,
		`INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,available_at_ms,worker_id,leased_until_ms,execution_started_at_ms,
execution_deadline_at_ms,created_at_ms,updated_at_ms)
SELECT 'es-work','EMULATIONSTATION_IMPORT','es-import','SERVER_EMULATIONSTATION_IMPORT',dedupe_key,
1,'{}',1,'RUNNING',1,4,1,'old-worker',100,1,1000,1,1 FROM jobs WHERE id='work'`,
		`INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
SELECT 'es-scan','EMULATIONSTATION_IMPORT','es-import','SERVER_EMULATIONSTATION_SCAN',dedupe_key,
1,'{}',1,'SUCCEEDED',1,4,1,1,1,1 FROM jobs WHERE id='scan'`,
		`INSERT INTO emulationstation_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,
release_year_max,state,scan_job_id,import_job_id,created_by_user_id,game_count,processable_item_count,
created_at_ms,updated_at_ms,expires_at_ms)
SELECT 'es-import',root_id,root_label_snapshot,source_relative_path,root_config_digest,1971,'RUNNING',
'es-scan','es-work',created_by_user_id,1,1,1,1,1000 FROM pegasus_imports WHERE id='import'`,
		`INSERT INTO emulationstation_import_gamelists(import_id,relative_path,size_bytes,content_digest,
source_facts_digest,parse_state,game_count,created_at_ms)
SELECT 'es-import','gamelist.xml',1,content_digest,source_facts_digest,'VALID',1,1
FROM pegasus_import_metadata_files WHERE import_id='import'`,
		`INSERT INTO emulationstation_import_collections(id,import_id,gamelist_relative_path,relative_directory,
display_name,game_count,created_at_ms,updated_at_ms)
VALUES('es-collection','es-import','gamelist.xml','','Games',1,1,1)`,
		`INSERT INTO emulationstation_import_items(id,import_id,collection_id,gamelist_relative_path,game_ordinal,
source_key,title,source_flags_json,discovery_state,execution_state,content_kind,metadata_json,
source_manifest_json,source_manifest_digest,created_at_ms,updated_at_ms)
SELECT 'es-item','es-import','es-collection','gamelist.xml',1,source_key,'Restored title',
'{"hidden":false,"adult":false,"kidGame":false}','READY','VALIDATING','SINGLE_FILE',
'{"schemaVersion":1,"title":"Restored title","description":"","developer":"","publisher":"",
"genre":"","players":null,"releaseYear":null}',
'{"schemaVersion":1,"contentKind":"SINGLE_FILE","files":[]}',source_manifest_digest,1,1
FROM pegasus_import_items WHERE id='item'`,
		`UPDATE import_items SET review_handoff_kind='EMULATIONSTATION' WHERE id='handoff-item'`,
		`INSERT INTO server_import_upload_owners(upload_session_id,kind,source_item_id)
VALUES('handoff-upload','EMULATIONSTATION','es-item')`,
	}
	for _, query := range statements {
		if _, err := db.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	if attached {
		if _, err := db.ExecContext(t.Context(), `UPDATE emulationstation_import_items
SET library_import_job_id='handoff-job',library_import_item_id='handoff-item' WHERE id='es-item'`); err != nil {
			t.Fatal(err)
		}
	}
	if seeded {
		seedRestoredReview(t, db)
	}
	return db, path
}

func seedRestoredReview(t *testing.T, db *sql.DB) {
	t.Helper()
	_, _, err := libraryservice.NewMetadataSeeder(library.NewMetadata(db), func() time.Time {
		return time.UnixMilli(2)
	}).Seed(t.Context(), "handoff-item", libraryservice.ServerMetadata{Title: "Restored title"}, 1971)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRetainsReservedEmulationStationReviews(t *testing.T) {
	t.Parallel()
	for _, window := range []string{"reserved", "attached", "seeded"} {
		t.Run(window, func(t *testing.T) {
			t.Parallel()
			db, path := restoredEmulationStationReview(t, window != "reserved", window == "seeded")
			err := New().WithRestore(t.Context(), path, func(records application.RestoreRecords) error {
				if err := application.CompleteRestoredReviews(t.Context(), records.Reviews(), time.UnixMilli(10)); err != nil {
					return err
				}
				_, err := records.StopExternalImports(t.Context(), 10)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			assertRestoredEmulationStationReview(t, db)
		})
	}
}

func assertRestoredEmulationStationReview(t *testing.T, db *sql.DB) {
	t.Helper()
	var state, plan, job, title, item, ordinary string
	var pending, failed, events int
	err := db.QueryRowContext(t.Context(), `SELECT source.execution_state,plan.state,job.state,
json_extract(draft.metadata_json,'$.title'),COALESCE(source.library_import_item_id,''),
COALESCE(source.library_import_job_id,''),plan.review_pending_item_count,plan.failed_item_count,
(SELECT count(*) FROM review_events WHERE import_item_id='handoff-item')
FROM emulationstation_import_items source JOIN emulationstation_imports plan ON plan.id=source.import_id
JOIN jobs job ON job.id=plan.import_job_id JOIN review_drafts draft ON draft.import_item_id='handoff-item'
WHERE source.id='es-item'`).Scan(&state, &plan, &job, &title, &item, &ordinary, &pending, &failed, &events)
	if err != nil {
		t.Fatal(err)
	}
	if state != "REVIEW_PENDING" || plan != "FAILED" || job != "FAILED" || title != "Restored title" ||
		item != "handoff-item" || ordinary != "handoff-job" || pending != 1 || failed != 0 || events != 1 {
		t.Fatalf("restore lost review: %s/%s/%s title=%q item=%s job=%s pending=%d failed=%d events=%d",
			state, plan, job, title, item, ordinary, pending, failed, events)
	}
}

package pegasusimport

import (
	"testing"

	"retrom/internal/libraryimport"
)

func handoffFixture(t *testing.T) (*Service, work, executionItem) {
	t.Helper()
	service, _ := startFixture(t)
	db := service.database
	var instance, provider, target string
	if err := db.QueryRowContext(t.Context(), `SELECT p.id,t.provider_id,t.target_id FROM platform_instances p
 JOIN runtime_target_bindings t ON t.core_id=p.default_core_id
 WHERE p.platform_id='gba' AND p.enabled=1 ORDER BY p.sort_order,p.id LIMIT 1`).Scan(&instance, &provider, &target); err != nil {
		t.Fatal(err)
	}
	mustExecPegasusTest(t.Context(), t, db, `INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
 VALUES('handoff-upload','COMPLETE','FILES',1,0,?,10000,1,1)`, fixedHandoffDigest)
	mustExecPegasusTest(t.Context(), t, db, `INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
 platform_id,default_core_id,provider_id,target_id,metadata_provider,config_snapshot_json,config_snapshot_digest,
 state,total_item_count,review_pending_item_count,created_at_ms,updated_at_ms)
 VALUES('handoff-job','handoff-upload',?,1,'gba','mgba',?,?,'NONE','{}',?,'REVIEW_PENDING',1,1,1,1)`, instance, provider, target, fixedHandoffDigest)
	mustExecPegasusTest(t.Context(), t, db, `INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms)
 VALUES('handoff-item','handoff-job',?,'REVIEW_PENDING','{}',?,'original',1,1)`, fixedHandoffDigest, fixedHandoffDigest)
	mustExecPegasusTest(t.Context(), t, db, `INSERT INTO review_drafts(id,import_item_id,target_platform_instance_id,metadata_json,created_at_ms,updated_at_ms)
 VALUES('handoff-draft','handoff-item',?,'{"title":"Original"}',1,1)`, instance)
	mustExecPegasusTest(t.Context(), t, db, `UPDATE jobs SET state='RUNNING',finished_at_ms=NULL,worker_id='pegasus-import-worker',
 leased_until_ms=100,execution_deadline_at_ms=1000 WHERE id='work';
 UPDATE pegasus_imports SET state='RUNNING',import_job_id='work',completed_at_ms=NULL;
 UPDATE pegasus_import_items SET execution_state='VALIDATING',library_import_job_id='handoff-job',
 library_import_item_id='handoff-item',metadata_json='{"Title":"Changed"}',completed_at_ms=NULL;`)
	service.importer = libraryimport.New(db, service.now)
	return service, work{JobID: "work", ImportID: "import", WorkerID: "pegasus-import-worker", ExecutionNo: 1, Attempt: 1}, executionItem{ID: "item", MetadataJSON: `{"Title":"Changed"}`}
}

const fixedHandoffDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestReviewHandoffRollsBackDraftWhenProgressEventFails(t *testing.T) {
	t.Parallel()
	service, unit, item := handoffFixture(t)
	mustExecPegasusTest(t.Context(), t, service.database, `DROP TABLE job_events`)
	service.prepareLibraryReview(t.Context(), unit, item, "handoff-job", libraryimport.ServerImportItem{ItemID: "handoff-item"})
	assertHandoffDraftUntouched(t, service)
}

func TestReviewHandoffRejectsPreviousExecution(t *testing.T) {
	t.Parallel()
	service, unit, item := handoffFixture(t)
	unit.ExecutionNo++
	service.prepareLibraryReview(t.Context(), unit, item, "handoff-job", libraryimport.ServerImportItem{ItemID: "handoff-item"})
	assertHandoffDraftUntouched(t, service)
	var state string
	if err := service.database.QueryRowContext(t.Context(), `SELECT execution_state FROM pegasus_import_items WHERE id='item'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "VALIDATING" {
		t.Fatalf("stale worker changed source: %s", state)
	}
}

func assertHandoffDraftUntouched(t *testing.T, service *Service) {
	t.Helper()
	var title, search string
	var version, events int64
	if err := service.database.QueryRowContext(t.Context(), `SELECT json_extract(d.metadata_json,'$.title'),d.version,i.search_text,
 (SELECT count(*) FROM review_events WHERE import_item_id=i.id) FROM review_drafts d JOIN import_items i ON i.id=d.import_item_id
 WHERE i.id='handoff-item'`).Scan(&title, &version, &search, &events); err != nil {
		t.Fatal(err)
	}
	if title != "Original" || version != 1 || search != "original" || events != 0 {
		t.Fatalf("failed handoff committed draft: %s version=%d search=%s events=%d", title, version, search, events)
	}
}

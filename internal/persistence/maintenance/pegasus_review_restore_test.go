package maintenance

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	application "retrom/internal/service/maintenance"
)

func restoredSourceReview(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db, path := restoredSourceScan(t, "RUNNING")
	digest := strings.Repeat("b", 64)
	var instance, provider, target string
	err := db.QueryRowContext(t.Context(), `SELECT p.id,t.provider_id,t.target_id FROM platform_instances p
 JOIN runtime_target_bindings t ON t.core_id=p.default_core_id
 WHERE p.platform_id='gba' AND p.enabled=1 ORDER BY p.sort_order,p.id LIMIT 1`).Scan(&instance, &provider, &target)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`UPDATE jobs SET state='SUCCEEDED',finished_at_ms=10 WHERE id='scan'`,
		`INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,worker_id,leased_until_ms,execution_started_at_ms,
 execution_deadline_at_ms,created_at_ms,updated_at_ms)
 VALUES('work','SOURCE_IMPORT','import','IMPORT_RECEIVE',
 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',1,'{}',1,'RUNNING',1,4,1,'old-worker',100,1,1000,1,1)`,
		`UPDATE source_imports SET state='RUNNING',import_job_id='work',game_count=1,processable_item_count=1 WHERE id='import'`,
	} {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	statements := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
 VALUES('handoff-upload','COMPLETE','FILES',1,0,?,10000,1,1)`, []any{digest}},
		{`INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
 platform_id,default_core_id,provider_id,target_id,metadata_provider,config_snapshot_json,config_snapshot_digest,
 state,total_item_count,review_pending_item_count,created_at_ms,updated_at_ms)
 VALUES('handoff-job','handoff-upload',?,1,'gba','mgba',?,?,'NONE','{}',?,'REVIEW_PENDING',1,1,1,1)`, []any{instance, provider, target, digest}},
		{`INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms)
 VALUES('handoff-item','handoff-job',?,'REVIEW_PENDING','{}',?,'original',1,1)`, []any{digest, digest}},
		{`UPDATE import_items SET target_platform_instance_id=?,metadata_json='{"title":"Original"}',review_version=1,review_created_at_ms=1,review_updated_at_ms=1 WHERE id='handoff-item'`, []any{instance}},
		{`UPDATE source_import_items SET execution_state='VALIDATING',library_import_job_id='handoff-job',
 library_import_item_id='handoff-item',metadata_json='{"Title":"Restored title"}' WHERE id='item'`, nil},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(t.Context(), statement.sql, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	return db, path
}

func TestRestoreRetainsSourceReviewCreatedBeforeSourceHandoff(t *testing.T) {
	t.Parallel()
	db, path := restoredSourceReview(t)
	err := New().WithRestore(t.Context(), path, func(records application.RestoreRecords) error {
		if err := application.CompleteRestoredReviews(t.Context(), records.Imports().Reviews, time.UnixMilli(10)); err != nil {
			return err
		}
		_, err := records.StopExternalImports(t.Context(), 10)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var sourceState, planState, jobState, title string
	var pending, failed, events int64
	err = db.QueryRowContext(t.Context(), `SELECT source.execution_state,plan.state,job.state,
 json_extract(draft.metadata_json,'$.title'),plan.review_pending_item_count,plan.failed_item_count,
 (SELECT review_version-1 FROM import_items WHERE id='handoff-item')
 FROM source_import_items source JOIN source_imports plan ON plan.id=source.import_id
 JOIN jobs job ON job.id=plan.import_job_id JOIN import_items draft ON draft.id=source.library_import_item_id
 WHERE source.id='item'`).Scan(&sourceState, &planState, &jobState, &title, &pending, &failed, &events)
	if err != nil {
		t.Fatal(err)
	}
	if sourceState != "REVIEW_PENDING" || planState != "FAILED" || jobState != "FAILED" || title != "Restored title" || pending != 1 || failed != 0 || events != 1 {
		t.Fatalf("restore lost prepared review: source=%s plan=%s job=%s title=%q pending=%d failed=%d events=%d", sourceState, planState, jobState, title, pending, failed, events)
	}
}

package sourceimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/libraryimport"
	repository "retrom/internal/persistence/sourceimport"
	libraryservice "retrom/internal/service/libraryimport"
	application "retrom/internal/service/sourceimport"
	"retrom/internal/testsupport"
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
	mustExecSourceTest(t.Context(), t, db, `INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
 VALUES('handoff-upload','COMPLETE','FILES',1,0,?,10000,1,1)`, fixedHandoffDigest)
	mustExecSourceTest(t.Context(), t, db, `INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
 platform_id,default_core_id,provider_id,target_id,metadata_provider,config_snapshot_json,config_snapshot_digest,
 state,total_item_count,review_pending_item_count,created_at_ms,updated_at_ms)
 VALUES('handoff-job','handoff-upload',?,1,'gba','mgba',?,?,'NONE','{}',?,'REVIEW_PENDING',1,1,1,1)`, instance, provider, target, fixedHandoffDigest)
	mustExecSourceTest(t.Context(), t, db, `INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms)
 VALUES('handoff-item','handoff-job',?,'REVIEW_PENDING','{}',?,'original',1,1)`, fixedHandoffDigest, fixedHandoffDigest)
	mustExecSourceTest(t.Context(), t, db, `UPDATE import_items SET target_platform_instance_id=?,metadata_json='{"title":"Original"}',review_version=1,review_created_at_ms=1,review_updated_at_ms=1 WHERE id='handoff-item'`, instance)
	mustExecSourceTest(t.Context(), t, db, `UPDATE jobs SET state='RUNNING',finished_at_ms=NULL,worker_id='source-import-worker',
 leased_until_ms=100,execution_deadline_at_ms=1000 WHERE id='work';
 UPDATE source_imports SET state='RUNNING',import_job_id='work',completed_at_ms=NULL;
 UPDATE source_import_items SET execution_state='VALIDATING',library_import_job_id='handoff-job',
 library_import_item_id='handoff-item',metadata_json='{"Title":"Changed"}',completed_at_ms=NULL;`)
	service.importer = libraryimport.New(db, service.now)
	return service, work{JobID: "work", ImportID: "import", WorkerID: "source-import-worker", ExecutionNo: 1, Attempt: 1}, executionItem{ID: "item", MetadataJSON: `{"Title":"Changed"}`}
}

const fixedHandoffDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func completeHandoff(ctx context.Context, service *Service, unit work, item executionItem) error {
	handoff := application.NewReviewHandoff(repository.NewReviewHandoff(service.database),
		libraryservice.NewMetadataSeeder(nil, service.now), service.now)
	return handoff.Complete(ctx, application.ReviewHandoffRequest{
		ItemID: item.ID, ImportID: unit.ImportID, JobID: unit.JobID, WorkerID: unit.WorkerID,
		LibraryJobID: "handoff-job", LibraryItemID: "handoff-item",
		ExecutionNo: unit.ExecutionNo, Attempt: unit.Attempt,
	})
}

func TestReviewHandoffRollsBackDraftWhenProgressEventFails(t *testing.T) {
	t.Parallel()
	service, unit, item := handoffFixture(t)
	failure := errors.New("progress event unavailable")
	var writes atomic.Int64
	service.database = testsupport.OpenSQLFaultDatabase(t, service.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "INSERT INTO job_events") {
				writes.Add(1)
				return failure
			}
			return nil
		},
	})
	if err := completeHandoff(t.Context(), service, unit, item); !errors.Is(err, failure) || writes.Load() != 1 {
		t.Fatalf("handoff did not reach failing event: writes=%d error=%v", writes.Load(), err)
	}
	assertHandoffDraftUntouched(t, service)
}

func TestReviewHandoffRejectsPreviousExecution(t *testing.T) {
	t.Parallel()
	service, unit, item := handoffFixture(t)
	unit.ExecutionNo++
	if err := completeHandoff(t.Context(), service, unit, item); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale execution handoff error=%v", err)
	}
	assertHandoffDraftUntouched(t, service)
	var state string
	if err := service.database.QueryRowContext(t.Context(), `SELECT execution_state FROM source_import_items WHERE id='item'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "VALIDATING" {
		t.Fatalf("stale worker changed source: %s", state)
	}
}

func assertHandoffDraftUntouched(t *testing.T, service *Service) {
	t.Helper()
	var title, search string
	var version int64
	if err := service.database.QueryRowContext(t.Context(), `SELECT json_extract(d.metadata_json,'$.title'),d.review_version,i.search_text FROM import_items d JOIN import_items i ON i.id=d.id
 WHERE i.id='handoff-item'`).Scan(&title, &version, &search); err != nil {
		t.Fatal(err)
	}
	if title != "Original" || version != 1 || search != "original" {
		t.Fatalf("failed handoff committed draft: %s version=%d search=%s", title, version, search)
	}
}

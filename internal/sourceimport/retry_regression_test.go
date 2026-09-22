package sourceimport

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRetryClearsOnlyResolvedFailureCounts(t *testing.T) {
	t.Parallel()
	db := newSourceRetryDatabase(t)
	service := &Service{database: db, now: func() time.Time { return time.UnixMilli(10) }}
	value, err := service.Retry(t.Context(), "import", 4, "user")
	if err != nil {
		t.Fatal(err)
	}
	if value.Counts.Failed != 0 || value.State != "QUEUED" {
		t.Fatalf("retry left stale failed count: %#v", value)
	}
}

// Sequential: restore uuid's global reader before parallel cases can run.
func TestRetryPropagatesEntropyFailureWithoutResettingItems(t *testing.T) {
	db := newSourceRetryDatabase(t)
	service := &Service{database: db, now: func() time.Time { return time.UnixMilli(10) }}
	uuid.SetRand(unavailableCreationEntropy{})
	value, err := func() (Summary, error) {
		defer uuid.SetRand(nil)
		return service.Retry(t.Context(), "import", 4, "user")
	}()
	if !errors.Is(err, errCreationEntropy) || value.ID != "" {
		t.Errorf("retry ignored entropy failure: %#v, %v", value, err)
	}
	var state string
	if err := db.QueryRowContext(t.Context(), `SELECT execution_state FROM source_import_items WHERE id='item'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "COMMIT_FAILED" {
		t.Errorf("failed retry reset item to %s", state)
	}
}

func TestCancelWaitsForClaimedJobBeforeClosingImport(t *testing.T) {
	t.Parallel()
	db := newSourceRetryDatabase(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE source_imports SET state='QUEUED',completed_at_ms=NULL,failed_item_count=0;
UPDATE jobs SET state='RUNNING',finished_at_ms=NULL,worker_id='worker',leased_until_ms=100 WHERE id='work';
UPDATE source_import_items SET execution_state='PENDING',completed_at_ms=NULL,error_code=NULL,retryable=0;`); err != nil {
		t.Fatal(err)
	}
	service := &Service{database: db, now: func() time.Time { return time.UnixMilli(10) }}
	value, pending, err := service.Cancel(t.Context(), "import", 4, "Stop", "user")
	if err != nil {
		t.Fatal(err)
	}
	if !pending || value.State != "CANCEL_REQUESTED" || value.CompletedAtMS != nil {
		t.Fatalf("claimed job closed early: %#v, pending=%v", value, pending)
	}
	var state string
	if err := db.QueryRowContext(t.Context(), `SELECT execution_state FROM source_import_items WHERE id='item'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "PENDING" {
		t.Fatalf("claimed execution item changed: %s", state)
	}
}

func TestRetryReportsActiveExecutionWithoutChangingFailedPlan(t *testing.T) {
	t.Parallel()
	db := newSourceRetryDatabase(t)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES('other-scan','SOURCE_IMPORT','other','IMPORT_SCAN','eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',1,'{}',1,'QUEUED',0,4,1,1,1);
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES('other-import','SOURCE_IMPORT','other','IMPORT_RECEIVE',
'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff',1,'{}',1,'QUEUED',0,4,1,1,1);
INSERT INTO source_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,
scan_job_id,import_job_id,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)
VALUES('other','games','Games','Other',
'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',
'QUEUED','other-scan','other-import','user',1,1,100);`); err != nil {
		t.Fatal(err)
	}
	service := &Service{database: db, now: func() time.Time { return time.UnixMilli(10) }}
	value, err := service.Retry(t.Context(), "import", 4, "user")
	if !errors.Is(err, ErrNotRetryable) || value.ID != "" {
		t.Fatalf("busy retry: %#v, %v", value, err)
	}
	var state string
	if err := db.QueryRowContext(t.Context(), `SELECT execution_state FROM source_import_items WHERE id='item'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "COMMIT_FAILED" {
		t.Fatalf("busy retry changed item: %s", state)
	}
}

// Sequential: restore uuid's global reader before parallel cases can run.
func TestCancelPropagatesEntropyFailureWithoutChangingQueuedWork(t *testing.T) {
	db := newSourceRetryDatabase(t)
	if _, err := db.ExecContext(t.Context(), `
UPDATE source_imports SET state='QUEUED',completed_at_ms=NULL,failed_item_count=0;
UPDATE jobs SET state='QUEUED',finished_at_ms=NULL WHERE id='work';
UPDATE source_import_items SET execution_state='PENDING',completed_at_ms=NULL,retryable=0;`); err != nil {
		t.Fatal(err)
	}
	service := &Service{database: db, now: func() time.Time { return time.UnixMilli(10) }}
	uuid.SetRand(unavailableCreationEntropy{})
	value, pending, err := func() (Summary, bool, error) {
		defer uuid.SetRand(nil)
		return service.Cancel(t.Context(), "import", 4, "Stop", "user")
	}()
	if !errors.Is(err, errCreationEntropy) || value.ID != "" || pending {
		t.Errorf("cancellation ignored entropy error: %#v %v pending=%v", value, err, pending)
	}
	var state string
	if err := db.QueryRowContext(t.Context(), `SELECT state FROM source_imports WHERE id='import'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "QUEUED" {
		t.Errorf("entropy failure changed queued work: %s", state)
	}
}

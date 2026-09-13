package payloadrelease

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/persistence/dbexec"
)

func TestImmediateGCManualRetryStartsANewExecutionBudget(t *testing.T) {
	t.Parallel()
	database := schedulingGame(t)
	blobs, err := blobstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := blobs.Put(bytes.NewBufferString("manual GC retry"))
	if err != nil {
		t.Fatal(err)
	}
	seedManualGC(t, database, metadata)
	now := time.UnixMilli(10)
	service, err := New(database, blobs, func() time.Time { return now }, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if err := service.ReconcileGC(t.Context()); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := database.QueryRowContext(t.Context(), `SELECT gc_job_id FROM blob_gc_candidates WHERE blob_id='manual-gc-blob'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(t.Context(), `UPDATE jobs SET state='FAILED',attempt_count=4,
 execution_started_at_ms=10,execution_deadline_at_ms=20,finished_at_ms=20,error_code='BLOB_GC_PHYSICAL_DELETE_FAILED',
 error_retryable=1 WHERE id=?`, id)
	if err != nil {
		t.Fatal(err)
	}
	now = time.UnixMilli(30)
	result, err := service.ScheduleImmediateGC(t.Context(), "manual-gc-user")
	if err != nil || result.BlobCount != 1 {
		t.Fatalf("manual retry admission: %+v/%v", result, err)
	}
	assertNewManualGCBudget(t, database, id)
	did, err := service.RunOnce(t.Context())
	if err != nil || !did {
		t.Fatalf("fresh manual GC execution could not run: %t/%v", did, err)
	}
	after := releaseJobAuthority(t, database, id)
	if after.State != "SUCCEEDED" || after.Attempt != 1 || after.Started.Int64 != 30 {
		t.Fatalf("new GC execution: %+v", after)
	}
}

func seedManualGC(t *testing.T, database *sql.DB, metadata blobstore.Metadata) {
	t.Helper()
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO profiles(id,display_name,created_at_ms)
 VALUES('manual-gc-profile','GC',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO users(id,profile_id,username,display_name,role,status,
 created_at_ms,updated_at_ms)VALUES('manual-gc-user','manual-gc-profile','gc-retry','GC','ADMIN','ENABLED',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), `INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
 VALUES('manual-gc-blob',?,?,?,?,?,'application/octet-stream',1)`, metadata.SHA256, metadata.Size, metadata.MD5,
		metadata.SHA1, metadata.CRC32); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func assertNewManualGCBudget(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	authority := releaseJobAuthority(t, database, id)
	var execution, inputExecution int64
	err := database.QueryRowContext(t.Context(), `SELECT execution_no,json_extract(payload_json,'$.inputExecutionNo')
 FROM jobs WHERE id=?`, id).Scan(&execution, &inputExecution)
	if err != nil || authority.State != "QUEUED" || authority.Attempt != 0 || authority.Started.Valid || authority.Deadline.Valid ||
		execution != 2 || inputExecution != 2 {
		t.Fatalf("manual retry retained expired budget or old receipt: %+v execution=%d input=%d error=%v",
			authority, execution, inputExecution, err)
	}
	var original, current string
	err = database.QueryRowContext(t.Context(), `SELECT old.input_json,current.input_json
 FROM job_input_snapshots old JOIN job_input_snapshots current ON current.job_id=old.job_id
 WHERE old.job_id=? AND old.execution_no=1 AND current.execution_no=2`, id).Scan(&original, &current)
	var before, after scheduleInput
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(original), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(current), &after); err != nil {
		t.Fatal(err)
	}
	if before.ExecutionID == after.ExecutionID || after.ExecutionID == "" || before.Inputs != after.Inputs {
		t.Fatalf("manual retry must renew execution identity and preserve GC input: before=%+v after=%+v", before, after)
	}
}

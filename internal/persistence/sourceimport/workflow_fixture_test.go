package sourceimport

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	application "retrom/internal/service/sourceimport"
)

func workflowDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := creationDatabase(t)
	if err := NewCreation(db).WithCreate(t.Context(), func(writer application.CreationWriter) error {
		_, err := writer.Insert(t.Context(), creationPlan(0))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,execution_started_at_ms,execution_deadline_at_ms,
leased_until_ms,heartbeat_at_ms,finished_at_ms,worker_id,error_code,error_retryable,created_at_ms,updated_at_ms)
VALUES('work','SOURCE_IMPORT','import-0','IMPORT_RECEIVE',
'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',1,'{"inputExecutionNo":1}',1,'FAILED',
4,4,3,1,1,100,60,2,2,'old-worker','READ_FAILED',1,1,2);
INSERT INTO job_input_snapshots(job_id,execution_no,input_json,input_digest,created_at_ms)
VALUES('work',1,'{"old":true}','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1);
UPDATE source_imports SET state='PARTIAL_FAILURE',phase=NULL,import_job_id='work',retryable=1,
game_count=4,failed_item_count=2,review_pending_item_count=1,completed_at_ms=2 WHERE id='import-0';
`); err != nil {
		t.Fatal(err)
	}
	for index, state := range []string{"SOURCE_CHANGED", "COMMIT_FAILED", "REVIEW_PENDING", "SKIPPED_MAPPING"} {
		retryable := 0
		if index == 0 {
			retryable = 1
		}
		if _, err := db.ExecContext(t.Context(), `
INSERT INTO source_import_items(id,import_id,metadata_relative_path,game_ordinal,source_key,title,discovery_state,
execution_state,metadata_json,source_manifest_json,source_manifest_digest,error_code,error_details_json,
retryable,completed_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'import-0','metadata.pegasus.txt',?,?,'Game','READY',?,'{"frozen":"metadata"}',
'{"frozen":"manifest"}','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
'OLD_ERROR','{"schemaVersion":1}',?,2,1,2)`, fmt.Sprintf("item-%d", index), index, fmt.Sprintf("%064x", index), state, retryable); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func prepareQueuedCancellation(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `
UPDATE source_imports SET state='QUEUED',completed_at_ms=NULL,failed_item_count=1 WHERE id='import-0';
UPDATE jobs SET state='QUEUED',finished_at_ms=NULL WHERE id='work';
UPDATE source_import_items SET execution_state='PENDING',completed_at_ms=NULL,retryable=0 WHERE id='item-0';
`); err != nil {
		t.Fatal(err)
	}
}

func workflowRows(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, table := range []string{"source_imports", "source_import_items", "jobs", "job_input_snapshots", "job_events", "audit_events"} {
		result[table] = workflowTable(t, db, table)
	}
	return result
}

func workflowTable(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT * FROM "+table+" ORDER BY 1,2")
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
	values := [][]any{}
	for rows.Next() {
		row := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range row {
			pointers[i] = &row[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		values = append(values, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

package pegasusimport

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/testassert"

	_ "modernc.org/sqlite"
)

func TestProjectRuntimeCheckReturnsActionableArcadeDependencies(t *testing.T) {
	t.Parallel()
	snapshot := `{"schemaVersion":2,"machine":"1944j","missingEntries":["1944.zip"],"mismatchedEntries":[],"dependencies":[{"kind":"PARENT","machine":"1944","requiredBy":"1944j","expectedLogicalName":"1944.zip","state":"MISSING","requiredEntries":["nffe.03"]}]}`
	result := projectRuntimeCheck(
		sql.NullString{String: "BLOCKED", Valid: true},
		sql.NullString{String: "LAUNCH_PARENT_MISSING", Valid: true},
		sql.NullString{String: "fbneo", Valid: true},
		sql.NullString{String: "FinalBurn Neo", Valid: true},
		sql.NullString{String: snapshot, Valid: true},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return result == nil }, func() bool { return result.Machine == nil }, func() bool { return *result.Machine != "1944j" }, func() bool { return len(result.MissingEntries) != 1 }, func() bool { return result.MissingEntries[0] != "1944.zip" }, func() bool { return len(result.Dependencies) != 1 }, func() bool { return result.Dependencies[0].ExpectedLogicalName != "1944.zip" }, func() bool { return len(result.Dependencies[0].RequiredEntries) != 1 }), "runtime check = %#v", result)
}

func TestRetryableCurrentFailureCanBeRecheckedWithoutRescanning(t *testing.T) {
	t.Parallel()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "retry.db"))
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { _ = database.Close() })
	if _, err := database.ExecContext(context.Background(), `
CREATE TABLE users(id TEXT PRIMARY KEY,display_name TEXT);
CREATE TABLE pegasus_imports(
 id TEXT PRIMARY KEY,root_id TEXT,root_label_snapshot TEXT,source_relative_path TEXT,state TEXT,phase TEXT,
 scan_job_id TEXT,import_job_id TEXT,metadata_count INTEGER,invalid_metadata_count INTEGER,collection_count INTEGER,
 game_count INTEGER,estimated_source_bytes INTEGER,mapped_collection_count INTEGER,skipped_collection_count INTEGER,
 processable_item_count INTEGER,blocked_item_count INTEGER,review_pending_item_count INTEGER,
 published_item_count INTEGER,review_discarded_item_count INTEGER,existing_item_count INTEGER,
 failed_item_count INTEGER,cancelled_item_count INTEGER,media_warning_count INTEGER,discovered_cover_count INTEGER,
 discovered_video_count INTEGER,mapping_version INTEGER,version INTEGER,created_by_user_id TEXT,last_error_code TEXT,
 retryable INTEGER,created_at_ms INTEGER,updated_at_ms INTEGER,expires_at_ms INTEGER,completed_at_ms INTEGER
);
CREATE TABLE pegasus_import_items(id TEXT PRIMARY KEY,import_id TEXT,execution_state TEXT,error_code TEXT,error_details_json TEXT,retryable INTEGER,completed_at_ms INTEGER,version INTEGER,updated_at_ms INTEGER);
CREATE TABLE jobs(
 id TEXT PRIMARY KEY,execution_no INTEGER,state TEXT,payload_json TEXT,attempt_count INTEGER,available_at_ms INTEGER,
 execution_started_at_ms INTEGER,execution_deadline_at_ms INTEGER,leased_until_ms INTEGER,heartbeat_at_ms INTEGER,
 finished_at_ms INTEGER,worker_id TEXT,error_code TEXT,error_retryable INTEGER,cancel_requested_at_ms INTEGER,
 cancel_reason TEXT,version INTEGER,updated_at_ms INTEGER
);
CREATE TABLE job_input_snapshots(job_id TEXT,execution_no INTEGER,input_json TEXT,input_digest TEXT,created_at_ms INTEGER);
CREATE TABLE job_events(job_id TEXT,scope_type TEXT,scope_id TEXT,event_type TEXT,data_json TEXT,created_at_ms INTEGER);
INSERT INTO users VALUES('user','Admin');
INSERT INTO pegasus_imports(
 id,root_id,root_label_snapshot,source_relative_path,state,phase,scan_job_id,import_job_id,
 metadata_count,invalid_metadata_count,collection_count,game_count,estimated_source_bytes,
 mapped_collection_count,skipped_collection_count,processable_item_count,blocked_item_count,
 review_pending_item_count,published_item_count,review_discarded_item_count,existing_item_count,
 failed_item_count,cancelled_item_count,media_warning_count,discovered_cover_count,discovered_video_count,
 mapping_version,version,created_by_user_id,last_error_code,retryable,created_at_ms,updated_at_ms,
 expires_at_ms,completed_at_ms
) VALUES(
 'import','games','Games','Roms','PARTIAL_FAILURE',NULL,'scan','work',1,0,1,1,1,1,0,1,0,
 0,0,0,0,1,0,0,0,0,1,4,'user',NULL,1,1,2,9999999999999,2
);
INSERT INTO pegasus_import_items VALUES(
 'item','import','COMMIT_FAILED','PEGASUS_LIBRARY_IMPORT_FAILED',
 '{"schemaVersion":1,"stage":"LIBRARY_IMPORT"}',1,2,1,2
);
INSERT INTO jobs VALUES('work',1,'SUCCEEDED','{}',1,1,1,1,NULL,NULL,2,NULL,NULL,NULL,NULL,NULL,1,2);
`); err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(10)
	service := &Service{database: database, now: func() time.Time { return now }, wake: make(chan struct{}, 1)}
	summary, err := service.Get(context.Background(), "import")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return !summary.Retryable }), "current summary = %#v, error=%v", summary, err)
	queued, err := service.Retry(context.Background(), "import", summary.Version, "user")
	testassert.Falsef(t, testassert.Any(func() bool { return err != nil }, func() bool { return queued.State != "QUEUED" }), "queued summary = %#v, error=%v", queued, err)
	var state string
	var code, details sql.NullString
	if err := database.QueryRowContext(context.Background(),
		`SELECT execution_state,error_code,error_details_json FROM pegasus_import_items WHERE id='item'`,
	).Scan(&state, &code, &details); err != nil || state != "PENDING" || code.Valid || details.Valid {
		t.Fatalf("retried item = state:%q code:%#v details:%#v error:%v", state, code, details, err)
	}
}

func TestProjectRuntimeCheckReturnsMissingBIOSAndDiscs(t *testing.T) {
	t.Parallel()
	snapshot := `{"schemaVersion":1,"bios":[{"logicalName":"saturn_bios.bin","requirementMode":"REQUIRED","conditionCode":null,"installationStatus":null}],"multiDisc":{"missingEntries":[{"ordinal":2,"sourceReference":"Disc 2.chd"}]}}`
	result := projectRuntimeCheck(
		sql.NullString{String: "BLOCKED", Valid: true},
		sql.NullString{String: "LAUNCH_BIOS_MISSING", Valid: true},
		sql.NullString{String: "yabause", Valid: true},
		sql.NullString{String: "Yabause", Valid: true},
		sql.NullString{String: snapshot, Valid: true},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return result == nil }, func() bool { return len(result.BIOS) != 1 }, func() bool { return result.BIOS[0].LogicalName != "saturn_bios.bin" }, func() bool { return len(result.MissingDiscs) != 1 }, func() bool { return result.MissingDiscs[0].SourceReference != "Disc 2.chd" }), "runtime check = %#v", result)
}

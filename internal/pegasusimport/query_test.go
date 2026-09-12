package pegasusimport

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/testassert"

	"retrom/internal/store"
)

func TestProjectRuntimeCheckReturnsActionableArcadeDependencies(t *testing.T) {
	t.Parallel()
	snapshot := `{"schemaVersion":1,"kind":"ARCADE","machine":"1944j","missingEntries":["1944.zip"],"mismatchedEntries":[],"dependencies":[{"kind":"PARENT","machine":"1944","requiredBy":"1944j","expectedLogicalName":"1944.zip","state":"MISSING","requiredEntries":["nffe.03"]}]}`
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
	database := newPegasusRetryDatabase(t)
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
	snapshot := `{"schemaVersion":1,"kind":"STATIC","bios":[{"logicalName":"saturn_bios.bin","requirementMode":"REQUIRED","conditionCode":null,"installationStatus":null}],"multiDisc":{"missingEntries":[{"ordinal":2,"sourceReference":"Disc 2.chd"}]}}`
	result := projectRuntimeCheck(
		sql.NullString{String: "BLOCKED", Valid: true},
		sql.NullString{String: "LAUNCH_BIOS_MISSING", Valid: true},
		sql.NullString{String: "yabause", Valid: true},
		sql.NullString{String: "Yabause", Valid: true},
		sql.NullString{String: snapshot, Valid: true},
	)
	testassert.Falsef(t, testassert.Any(func() bool { return result == nil }, func() bool { return len(result.BIOS) != 1 }, func() bool { return result.BIOS[0].LogicalName != "saturn_bios.bin" }, func() bool { return len(result.MissingDiscs) != 1 }, func() bool { return result.MissingDiscs[0].SourceReference != "Disc 2.chd" }), "runtime check = %#v", result)
}

func newPegasusRetryDatabase(t *testing.T) *sql.DB {
	t.Helper()
	owner, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "retry.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})
	db := owner.SQL
	if _, err := db.ExecContext(t.Context(), `
 INSERT INTO profiles(id,display_name,created_at_ms) VALUES('retry-profile','Admin',1);
 INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
 VALUES('user','retry-profile','retry-admin','Admin','ADMIN','ENABLED',1,1);
 INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
 attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms,finished_at_ms)
 VALUES('scan','PEGASUS_IMPORT','import','SERVER_PEGASUS_SCAN',
 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,'{}',1,'SUCCEEDED',1,4,1,1,2,2),
 ('work','PEGASUS_IMPORT','import','SERVER_PEGASUS_IMPORT',
 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',1,'{}',1,'SUCCEEDED',1,4,1,1,2,2);
INSERT INTO pegasus_imports(
 id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,phase,scan_job_id,import_job_id,
 metadata_count,invalid_metadata_count,collection_count,game_count,estimated_source_bytes,
 mapped_collection_count,skipped_collection_count,processable_item_count,blocked_item_count,
 review_pending_item_count,published_item_count,review_discarded_item_count,existing_item_count,
 failed_item_count,cancelled_item_count,media_warning_count,discovered_cover_count,discovered_video_count,
 mapping_version,version,created_by_user_id,last_error_code,retryable,created_at_ms,updated_at_ms,
 expires_at_ms,completed_at_ms
) VALUES(
 'import','games','Games','Roms','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','PARTIAL_FAILURE',NULL,'scan','work',1,0,1,1,1,1,0,1,0,
 0,0,0,0,1,0,0,0,0,1,4,'user',NULL,1,1,2,9999999999999,2
);

 INSERT INTO pegasus_import_items(id,import_id,metadata_relative_path,game_ordinal,source_key,title,
 discovery_state,execution_state,metadata_json,source_manifest_json,source_manifest_digest,
 error_code,error_details_json,retryable,completed_at_ms,version,created_at_ms,updated_at_ms)
 VALUES('item','import','metadata.pegasus.txt',0,
 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc','Retry game',
 'READY','COMMIT_FAILED','{}','{}',
 'dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
 'PEGASUS_LIBRARY_IMPORT_FAILED','{"schemaVersion":1,"stage":"LIBRARY_IMPORT"}',1,2,1,1,2);
 `); err != nil {
		t.Fatal(err)
	}
	return db
}

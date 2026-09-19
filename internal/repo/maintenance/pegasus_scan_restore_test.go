package maintenance

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	maintenancemodel "retrom/internal/model/maintenance"
	"retrom/internal/testkit/testsupport"
)

func restoredPegasusScan(t *testing.T, state string) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "retrom.db")
	db, err := testsupport.OpenDatabase(t.Context(), path, func() time.Time { return time.UnixMilli(10) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	_, err = db.SQL.ExecContext(
		t.Context(),
		`INSERT INTO profiles(id,display_name,created_at_ms) VALUES('profile','Scan',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('user','profile','scan','Scan','ADMIN','ENABLED',1,1);
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
VALUES('scan','PEGASUS_IMPORT','import','SERVER_PEGASUS_SCAN',
'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,'{}',1,'QUEUED',0,4,1,1,1);
INSERT INTO pegasus_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,
state,scan_job_id,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)
VALUES('import','games','Games','',
'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','SCANNING','scan','user',1,1,100);
INSERT INTO pegasus_import_metadata_files(import_id,relative_path,size_bytes,content_digest,source_facts_digest,parse_state,created_at_ms)
VALUES('import','metadata.pegasus.txt',1,
'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','VALID',1);
INSERT INTO pegasus_import_items(id,import_id,metadata_relative_path,game_ordinal,source_key,title,
discovery_state,execution_state,metadata_json,source_manifest_json,source_manifest_digest,created_at_ms,updated_at_ms)
VALUES('item','import','metadata.pegasus.txt',0,
'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','Scanned','READY','PENDING','{}','{}',
'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,1);`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if state != "CANCEL_REQUESTED" {
		if _, err := db.SQL.ExecContext(t.Context(), `UPDATE jobs SET state=? WHERE id='scan'`, state); err != nil {
			t.Fatal(err)
		}
	}
	if state == "CANCEL_REQUESTED" {
		if _, err := db.SQL.ExecContext(
			t.Context(),
			`UPDATE jobs SET state='CANCEL_REQUESTED',cancel_reason='stop',cancel_requested_at_ms=2 WHERE id='scan';
UPDATE pegasus_imports SET state='CANCEL_REQUESTED',cancel_reason='stop' WHERE id='import'`,
		); err != nil {
			t.Fatal(err)
		}
	}
	return db.SQL, path
}

func TestRestorePegasusScanCleanupRollsBackWithEnclosingRestore(t *testing.T) {
	t.Parallel()
	db, path := restoredPegasusScan(t, "QUEUED")
	cause := errors.New("later restore step failed")
	err := New().WithRestore(t.Context(), path, func(records maintenancemodel.RestoreRecords) error {
		if _, err := records.StopExternalImports(t.Context(), 10); err != nil {
			return err
		}
		return cause
	})
	if !errors.Is(err, cause) {
		t.Fatalf("restore failure cause: %v", err)
	}
	var state string
	var items, metadata int
	err = db.QueryRowContext(t.Context(), `SELECT state,(SELECT count(*) FROM pegasus_import_items),
(SELECT count(*) FROM pegasus_import_metadata_files) FROM pegasus_imports WHERE id='import'`).Scan(
		&state,
		&items,
		&metadata,
	)
	if err != nil || state != "SCANNING" || items != 1 || metadata != 1 {
		t.Fatalf("partial restore: %s %d %d %v", state, items, metadata, err)
	}
}

func TestRestoreClosesUnpublishedPegasusScanWithoutLeavingPartialItems(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"QUEUED", "RUNNING", "CANCEL_REQUESTED"} {
		t.Run(state, func(t *testing.T) {
			db, path := restoredPegasusScan(t, state)
			err := New().WithRestore(t.Context(), path, func(records maintenancemodel.RestoreRecords) error {
				counts, err := records.StopExternalImports(t.Context(), 10)
				if err == nil && counts.Pegasus != 1 {
					t.Fatalf("restored scan count=%d", counts.Pegasus)
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			var planState, jobState string
			var rows int
			err = db.QueryRowContext(t.Context(), `SELECT plan.state,job.state,
(SELECT count(*) FROM pegasus_import_items)+(SELECT count(*) FROM pegasus_import_metadata_files)
FROM pegasus_imports plan JOIN jobs job ON job.id=plan.scan_job_id WHERE plan.id='import'`).Scan(
				&planState,
				&jobState,
				&rows,
			)
			if err != nil || planState != "FAILED" || jobState != "FAILED" || rows != 0 {
				t.Fatalf("restored partial scan: %s %s rows=%d err=%v", planState, jobState, rows, err)
			}
		})
	}
}

package importfiles

import (
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/testsupport"

	dbapi "retrom/internal/database"
	"retrom/internal/store"
)

func TestReceiveIsImmutableAndTransactional(t *testing.T) {
	t.Parallel()
	db := receiveFixture(t)
	var err error
	for range 2 {
		if err := Receive(t.Context(), db, "upload"); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var path, blob string
	var size int64
	err = dbapi.QueryRowContext(t.Context(), db, `SELECT (SELECT count(*) FROM import_files),relative_path,file_record,size_bytes FROM import_files WHERE
id='ready'`).Scan(&count, &path, &blob, &size)
	if err != nil || count != 1 || path != "games/ready.nes" ||
		blob != testsupport.FileMetadata("rom").Record || size != 3 {
		t.Fatalf("received files: %d %s %s %d %v", count, path, blob, size, err)
	}
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(t.Context(), `UPDATE upload_files SET relative_path='changed.nes' WHERE id='ready';
 UPDATE upload_files SET state='COMPLETE',final_file_record=(SELECT final_file_record FROM upload_files
WHERE id='ready'),received_size_bytes=3 WHERE id='pending'`)
	if err != nil {
		t.Fatal(err)
	}
	if err := Receive(t.Context(), tx, "upload"); err == nil {
		t.Fatal("accepted changed import input")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := dbapi.QueryRowContext(t.Context(), db,
		"SELECT count(*) FROM import_files").Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed reception leaked files: %d %v", count, err)
	}
}

func receiveFixture(t *testing.T) dbapi.DB {
	t.Helper()
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "files.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	db := database.SQL
	_, err = db.ExecContext(t.Context(), ` INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,
created_at_ms,updated_at_ms)
 VALUES('upload','COMPLETE','DIRECTORY',2,6,
'eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee',100,1,1);
 INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,
final_file_record,state,created_at_ms,updated_at_ms)
 VALUES('ready','upload','games/ready.nes',3,3,?,'COMPLETE',1,1),('pending','upload','games/pending.nes',
3,0,NULL,'PENDING',1,1)`, testsupport.FileMetadata("rom").Record)
	if err != nil {
		t.Fatal(err)
	}

	return db
}

func TestRetiredFormatAndReviewHistoryTablesAreAbsent(t *testing.T) {
	t.Parallel()
	db := receiveFixture(t)
	var retiredTables int
	if err := dbapi.QueryRowContext(t.Context(), db, `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name IN ('review_events','pegasus_imports',
'emulationstation_imports','stored_files','file_deletions')`).Scan(&retiredTables); err != nil || retiredTables != 0 {
		t.Fatalf("retired workflows remain: %d %v", retiredTables, err)
	}
}

func TestReceiveFilePublishesOnlyTheFinalizedFile(t *testing.T) {
	t.Parallel()
	db := receiveFixture(t)
	if _, err := db.ExecContext(t.Context(), `UPDATE upload_files SET state='COMPLETE',
final_file_record=(SELECT final_file_record FROM upload_files WHERE id='ready'),received_size_bytes=3
WHERE id='pending'`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := ReceiveFile(t.Context(), db, "ready"); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var id string
	err := dbapi.QueryRowContext(t.Context(), db,
		"SELECT (SELECT count(*) FROM import_files),id FROM import_files").Scan(&count, &id)
	if err != nil || count != 1 || id != "ready" {
		t.Fatalf("unexpected received files: count=%d id=%s err=%v", count, id, err)
	}
}

package receivedfiles

import (
	"path/filepath"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/store"
)

func TestRetiredOwnerReleasesTransportWithoutDeletingProvenanceOrOtherFiles(t *testing.T) {
	database, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	seedReceivedFiles(t, database.SQL)
	transaction, err := database.SQL.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(transaction)
	if err := ReleaseRetired(t.Context(), transaction, "GAME", "first-game", 3); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(t.Context(), `DELETE FROM stored_files WHERE id='retired-owned'`); err != nil {
		t.Fatalf("passive upload history blocked domain file deletion: %v", err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	var purged, retained, consumptions int
	err = dbapi.QueryRowContext(t.Context(), database.SQL, `SELECT
 (SELECT count(*) FROM upload_files u JOIN import_files i ON i.id=u.id
  WHERE u.id='retired-owned' AND u.state='PURGED' AND u.final_blob_id IS NULL AND i.blob_id IS NULL),
 (SELECT count(*) FROM upload_files u JOIN import_files i ON i.id=u.id
  WHERE u.id<>'retired-owned' AND u.state='COMPLETE' AND u.final_blob_id=i.blob_id),
 (SELECT count(*) FROM upload_consumptions WHERE upload_file_id='retired-owned')`).Scan(&purged, &retained, &consumptions)
	if err != nil || purged != 1 || retained != 2 || consumptions != 1 {
		t.Fatalf("transport closure escaped its owner: purged=%d retained=%d history=%d error=%v", purged, retained, consumptions, err)
	}
}

func seedReceivedFiles(t *testing.T, database dbapi.DB) {
	t.Helper()
	_, err := database.ExecContext(t.Context(), `
 INSERT INTO upload_sessions(id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
 VALUES('upload','COMPLETE','FILES',3,3,lower(hex(zeroblob(32))),100,1,1);
 INSERT INTO stored_files(id,sha256,md5,sha1,crc32,size_bytes,media_type,created_at_ms,owner_kind,owner_id,retired_at_ms)
 SELECT id,lower(hex(zeroblob(32))),lower(hex(zeroblob(16))),lower(hex(zeroblob(20))),
 '00000000',1,'application/octet-stream',1,'GAME',owner,retired FROM (
 SELECT 'retired-owned' AS id,'first-game' AS owner,2 AS retired
 UNION ALL SELECT 'retained-owned','first-game',NULL
 UNION ALL SELECT 'retired-foreign','second-game',2);
 INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_blob_id,state,created_at_ms,updated_at_ms)
 SELECT id,'upload',id,1,1,id,'COMPLETE',1,1 FROM stored_files;
 INSERT INTO import_files(id,upload_session_id,relative_path,blob_id,size_bytes,created_at_ms)
 SELECT id,'upload',id,id,1,1 FROM stored_files;
 INSERT INTO upload_consumptions(id,upload_session_id,upload_file_id,consumer_type,consumer_id,created_at_ms)
 VALUES('consumption','upload','retired-owned','GAME_ASSET','asset',1)`)
	if err != nil {
		t.Fatal(err)
	}
}

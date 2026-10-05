//go:build integration

package payloadpurge

import (
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	"retrom/internal/store"

	"retrom/internal/testsupport/testpostgres"

	"github.com/google/uuid"
)

func TestUploadDirectoryWaitsForEveryFileToBeReleased(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	database, err := store.Open(ctx, testpostgres.DSN(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close upload test database", database.Close()) }()
	files, err := filestore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := files.Put(strings.NewReader("upload"))
	if err != nil {
		t.Fatal(err)
	}
	session := uuid.NewString()
	_, err = database.SQL.ExecContext(ctx, `INSERT INTO upload_sessions
 (id,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
 VALUES(?,'COMPLETE','FILES',2,12,?,10000,1,1)`, session, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		id := uuid.NewString()
		owned, err := files.CopyTo(ctx, metadata.Record, "staging/uploads/"+session, id)
		if err != nil {
			t.Fatal(err)
		}
		_, err = database.SQL.ExecContext(ctx, `INSERT INTO upload_files
  (id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_file_record,state,created_at_ms,updated_at_ms)
  VALUES(?,?,?,6,6,?,'COMPLETE',1,1)`, id, session, id, owned.Record)
		if err != nil {
			t.Fatal(err)
		}
	}
	for index := range 2 {
		tx, err := database.SQL.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		records := Records{Executor: tx}
		candidates, err := records.Candidates(ctx, session, "", 1)
		if err != nil || len(candidates) != 1 {
			t.Fatalf("candidate=%v %v", candidates, err)
		}
		if err := records.Purge(ctx, candidates[0], 2); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		var tasks int
		if err := dbapi.QueryRowContext(ctx, database.SQL, `SELECT count(*) FROM jobs WHERE kind='PATH_DELETE'`).Scan(&tasks); err != nil {
			t.Fatal(err)
		}
		if tasks != index {
			t.Fatalf("after %d files, removal tasks=%d", index+1, tasks)
		}
	}
}

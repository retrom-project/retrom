package uploads_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	uploadpersistence "retrom/internal/persistence/uploads"

	"retrom/internal/service/uploads"

	_ "modernc.org/sqlite"
)

func TestPartWriteFailureRollsBackProgress(t *testing.T) {
	service, database, session := partFixture(t, false)
	err := service.PutPart(t.Context(), session.ID, session.Files[0].ID, 0, "bytes 0-4/5", digest([]byte("bytes")), bytes.NewReader([]byte("bytes")))
	if err == nil {
		t.Fatal("session check failure was ignored")
	}
	assertNoPartProgress(t, database, session, "CREATED")
}

func TestCanceledUploadCannotAcceptNewParts(t *testing.T) {
	service, database, session := partFixture(t, true)
	if _, _, err := service.Cancel(t.Context(), session.ID, session.Version); err != nil {
		t.Fatal(err)
	}
	err := service.PutPart(t.Context(), session.ID, session.Files[0].ID, 0, "bytes 0-4/5", digest([]byte("bytes")), bytes.NewReader([]byte("bytes")))
	if !errors.Is(err, uploads.ErrInvalid) {
		t.Errorf("terminal upload accepted a part: %v", err)
	}
	assertNoPartProgress(t, database, session, "CANCELLED")
}

func TestPartOffsetsMustMatchPartNumberExactly(t *testing.T) {
	service, database, session := partFixture(t, true)
	err := service.PutPart(t.Context(), session.ID, session.Files[0].ID, 0, "bytes 1-4/5", digest([]byte("ytes")), bytes.NewReader([]byte("ytes")))
	if !errors.Is(err, uploads.ErrInvalid) {
		t.Errorf("misaligned part accepted: %v", err)
	}
	assertNoPartProgress(t, database, session, "CREATED")
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha-256=:" + base64.StdEncoding.EncodeToString(sum[:]) + ":"
}

func assertNoPartProgress(t *testing.T, database *sql.DB, session uploads.Session, state string) {
	t.Helper()
	var count, received int64
	var actual string
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM upload_parts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `SELECT received_size_bytes FROM upload_files WHERE id=?`, session.Files[0].ID).Scan(&received); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(t.Context(), `SELECT state FROM upload_sessions WHERE id=?`, session.ID).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if count != 0 || received != 0 || actual != state {
		t.Fatalf("unexpected upload progress: parts=%d bytes=%d state=%s want=%s", count, received, actual, state)
	}
}

func partFixture(t *testing.T, allowProgress bool) (*uploads.Service, *sql.DB, uploads.Session) {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	database.SetMaxOpenConns(1)
	_, err = database.ExecContext(t.Context(), `
CREATE TABLE upload_sessions (
 id TEXT PRIMARY KEY,purpose TEXT,state TEXT DEFAULT 'CREATED',source_type TEXT,total_files INTEGER,total_bytes INTEGER,
 manifest_digest TEXT,expires_at_ms INTEGER,created_at_ms INTEGER,updated_at_ms INTEGER,
 version INTEGER DEFAULT 1,finalization_no INTEGER DEFAULT 0,finalize_job_id TEXT,last_error_code TEXT,
 allow_progress INTEGER DEFAULT 0,CHECK(state!='UPLOADING' OR allow_progress=1)
);
CREATE TABLE upload_files (
 id TEXT PRIMARY KEY,upload_session_id TEXT,relative_path TEXT,declared_size_bytes INTEGER,
 received_size_bytes INTEGER DEFAULT 0,state TEXT,created_at_ms INTEGER,updated_at_ms INTEGER,
 last_error_code TEXT,final_blob_id TEXT
);
CREATE TABLE upload_parts (
 upload_file_id TEXT,part_no INTEGER,offset_bytes INTEGER,size_bytes INTEGER,sha256 TEXT,storage_key TEXT,created_at_ms INTEGER,
 PRIMARY KEY(upload_file_id,part_no)
);
CREATE TABLE upload_consumptions (upload_session_id TEXT);
`)
	if err != nil {
		t.Fatal(err)
	}
	service := uploads.New(uploadpersistence.New(database), nil, t.TempDir(), func() time.Time { return time.UnixMilli(1000) })
	session, err := service.Create(t.Context(), uploads.CreateRequest{SourceType: "FILES", Files: []uploads.FileDeclaration{
		{ClientFileID: "file", RelativePath: "fixture.bin", SizeBytes: 5},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if allowProgress {
		if _, err := database.ExecContext(t.Context(), `UPDATE upload_sessions SET allow_progress=1`); err != nil {
			t.Fatal(err)
		}
	}
	return service, database, session
}

package uploads

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	uploadsmodel "retrom/internal/model/uploads"
	"retrom/internal/repo/store"
	uploadsservice "retrom/internal/service/uploads"
)

func TestFinalizerRejectsPartWhoseStoredBytesChanged(t *testing.T) {
	root := t.TempDir()
	database, err := store.Open(t.Context(), filepath.Join(root, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	blobs, err := blobstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	service := uploadsservice.New(New(database.SQL), blobs, root, time.Now)
	upload, err := service.Create(t.Context(), uploadsmodel.CreateRequest{SourceType: "FILES", Files: []uploadsmodel.FileDeclaration{{ClientFileID: "file", RelativePath: "fixture.bin", SizeBytes: 5}}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("bytes"))
	header := "sha-256=:" + base64.StdEncoding.EncodeToString(sum[:]) + ":"
	if err := service.PutPart(t.Context(), upload.ID, upload.Files[0].ID, 0, "bytes 0-4/5", header, bytes.NewReader([]byte("bytes"))); err != nil {
		t.Fatal(err)
	}
	var key string
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT storage_key FROM upload_parts WHERE upload_file_id=?`, upload.Files[0].ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tmp", "uploads", filepath.FromSlash(key)), []byte("wrong"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(t.Context(), upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := service.Complete(t.Context(), upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	awaitFinalizeState(t, database.SQL, job, "FAILED")
	var code string
	if err := database.SQL.QueryRowContext(t.Context(), "SELECT error_code FROM jobs WHERE id=?", job).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != "UPLOAD_PART_CORRUPT" {
		t.Fatalf("corrupt part code=%s", code)
	}
	assertCorruptPartRepair(t, service, database.SQL, upload, job, header)
}

func assertCorruptPartRepair(t *testing.T, service *uploadsservice.Service, database *sql.DB, upload uploadsmodel.Session, job, header string) {
	t.Helper()
	if err := service.PutPart(t.Context(), upload.ID, upload.Files[0].ID, 0, "bytes 0-4/5", header, bytes.NewReader([]byte("bytes"))); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(t.Context(), upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	repairedJob, number, err := service.Complete(t.Context(), upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	if repairedJob == job || number != 2 {
		t.Fatal("repair reused the failed finalization")
	}
	awaitFinalizeState(t, database, repairedJob, "SUCCEEDED")
	awaitFinalizeState(t, database, job, "FAILED")
	var partCount int
	if err := database.QueryRowContext(t.Context(), "SELECT count(*) FROM upload_parts WHERE upload_file_id=?", upload.Files[0].ID).Scan(&partCount); err != nil {
		t.Fatal(err)
	}
	if partCount != 0 {
		t.Fatalf("finalized parts retained: %d", partCount)
	}
}

func awaitFinalizeState(t *testing.T, database *sql.DB, job, expected string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var state string
		if err := database.QueryRowContext(t.Context(), "SELECT state FROM jobs WHERE id=?", job).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == expected {
			return
		}
		if state == "FAILED" || state == "SUCCEEDED" || time.Now().After(deadline) {
			t.Fatalf("finalization=%s want=%s", state, expected)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

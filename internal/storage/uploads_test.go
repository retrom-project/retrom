package storage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"retrom/internal/model"

	"github.com/google/uuid"
)

func stagedUpload(t *testing.T, files []model.UploadFileInput) (Uploads, string, string) {
	t.Helper()
	uploads, err := OpenUploads(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if err = uploads.Create(id, files); err != nil {
		t.Fatal(err)
	}
	source, err := uploads.Source(id)
	if err != nil {
		t.Fatal(err)
	}
	return uploads, id, source
}

func TestUploadAppendResumesAtReceivedBytesAndRejectsStaleOffsets(t *testing.T) {
	t.Parallel()
	uploads, id, source := stagedUpload(t, []model.UploadFileInput{{Path: "game/disc.bin", SizeBytes: 10}})
	if received, err := uploads.Append(id, 0, 0, strings.NewReader("hello")); err != nil || received != 5 {
		t.Fatalf("first chunk received=%d error=%v", received, err)
	}
	// A retried or reordered chunk reports the bytes already held instead of corrupting the file.
	if received, err := uploads.Append(id, 0, 3, strings.NewReader("xx")); !errors.Is(err, model.ErrUploadOffset) ||
		received != 5 {
		t.Fatalf("stale offset received=%d error=%v", received, err)
	}
	if _, err := os.Stat(filepath.Join(source, "game/disc.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an incomplete file must not appear in the source tree: %v", err)
	}
	if _, err := uploads.Append(id, 1, 0, strings.NewReader("x")); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("undeclared index error=%v", err)
	}
}

func TestUploadPublishesCompleteFilesIntoTheSourceTree(t *testing.T) {
	t.Parallel()
	uploads, id, source := stagedUpload(t,
		[]model.UploadFileInput{{Path: "game/disc.bin", SizeBytes: 10}, {Path: "empty.txt", SizeBytes: 0}})
	if _, err := os.Stat(filepath.Join(source, "empty.txt")); err != nil {
		t.Fatalf("empty file is complete at creation: %v", err)
	}
	if _, err := uploads.Append(id, 0, 0, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if received, err := uploads.Append(id, 0, 5, strings.NewReader("world")); err != nil || received != 10 {
		t.Fatalf("last chunk received=%d error=%v", received, err)
	}
	content, err := os.ReadFile(filepath.Join(source, "game/disc.bin"))
	if err != nil || !bytes.Equal(content, []byte("helloworld")) {
		t.Fatalf("published=%q error=%v", content, err)
	}
	declared, received, err := uploads.Files(id)
	if err != nil || len(declared) != 2 || received[0] != 10 || received[1] != 0 {
		t.Fatalf("files=%+v received=%v error=%v", declared, received, err)
	}
	if _, err = uploads.Append(id, 0, 10, strings.NewReader("more")); !errors.Is(err, model.ErrUploadOffset) {
		t.Fatalf("a complete file accepts no more bytes: %v", err)
	}
}

func TestUploadAppendRejectsBytesBeyondTheDeclaredSize(t *testing.T) {
	t.Parallel()
	uploads, err := OpenUploads(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if err = uploads.Create(id, []model.UploadFileInput{{Path: "rom.nes", SizeBytes: 4}}); err != nil {
		t.Fatal(err)
	}
	if received, appendErr := uploads.Append(id, 0, 0, strings.NewReader("12345")); !errors.Is(appendErr, model.ErrInvalid) ||
		received != 0 {
		t.Fatalf("oversized chunk received=%d error=%v", received, appendErr)
	}
	if received, appendErr := uploads.Append(id, 0, 0, strings.NewReader("1234")); appendErr != nil || received != 4 {
		t.Fatalf("the discarded chunk can be resent: received=%d error=%v", received, appendErr)
	}
}

func TestUploadIDsAndRemoveOnlyTouchUploadDirectories(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	uploads, err := OpenUploads(directory)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if err = uploads.Create(id, []model.UploadFileInput{{Path: "a.nes", SizeBytes: 1}}); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(directory, "not-an-upload"), 0o700); err != nil {
		t.Fatal(err)
	}
	ids, err := uploads.IDs()
	if _, staged := ids[id]; err != nil || len(ids) != 1 || !staged {
		t.Fatalf("ids=%v error=%v", ids, err)
	}
	if err = uploads.Remove(id); err != nil {
		t.Fatal(err)
	}
	if err = uploads.Remove("../not-an-upload"); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("remove outside uploads error=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(directory, "not-an-upload")); statErr != nil {
		t.Fatal(statErr)
	}
	if free, freeErr := uploads.FreeBytes(); freeErr != nil || free <= 0 {
		t.Fatalf("free=%d error=%v", free, freeErr)
	}
}

type failingReader struct{ remaining int }

func (r *failingReader) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		return 0, errors.New("connection reset")
	}
	count := min(len(buffer), r.remaining)
	r.remaining -= count
	return count, nil
}

func TestUploadInterruptedChunkLeavesThePartAtItsOffset(t *testing.T) {
	t.Parallel()
	uploads, id, _ := stagedUpload(t, []model.UploadFileInput{{Path: "disc.bin", SizeBytes: 100}})
	if _, err := uploads.Append(id, 0, 0, strings.NewReader("0123456789")); err != nil {
		t.Fatal(err)
	}
	if received, err := uploads.Append(id, 0, 10, &failingReader{remaining: 30}); err == nil || received != 10 {
		t.Fatalf("interrupted chunk received=%d error=%v", received, err)
	}
	if _, received, err := uploads.Files(id); err != nil || received[0] != 10 {
		t.Fatalf("received=%v error=%v", received, err)
	}
}

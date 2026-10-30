package storage

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"retrom/internal/model"

	"github.com/google/uuid"
)

func originTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func TestOriginalBundleAndExclusiveOwnership(t *testing.T) {
	s := originTestStore(t)
	operation, gameID, fileID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	expected := map[string]string{"selected/raw.zip": "unmodified outer archive", "companion/patch.bin": "original companion"}
	inputs := make([]model.ContentFile, 0, len(expected))
	for name, body := range expected {
		file, err := s.Stage(t.Context(), operation, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, model.ContentFile{LogicalKey: name, StorageKey: file.Key, SHA256: file.SHA256, SizeBytes: file.Size})
	}
	staged, err := s.BundleOrigin(t.Context(), operation, inputs)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.PublishOrigin(t.Context(), gameID, fileID, "original.zip", staged)
	if err != nil {
		t.Fatal(err)
	}
	assertOriginalBundle(t, s, info, expected)
	source := model.GameFile{ID: fileID, ExtInfo: model.GameFileTabExtInfo{
		ConversionInfo: &model.GameFileConversionInfo{OriginInfo: info},
	}}
	targetID := uuid.NewString()
	copied, err := s.CopyOrigin(t.Context(), gameID, source, targetID)
	if err != nil || copied.OriginInfo.SHA256 != info.SHA256 || copied.OriginInfo.StorageKey == info.StorageKey {
		t.Fatalf("exclusive copy=%+v err=%v", copied, err)
	}
	if err = s.RemoveOrigin(gameID, fileID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.root.Stat(info.StorageKey); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old attachment retained: %v", err)
	}
	if _, err = s.root.Stat(copied.OriginInfo.StorageKey); err != nil {
		t.Fatalf("deleting old owner removed new attachment: %v", err)
	}
}

func TestOriginalPublishRejectsCancellationAndOverwrite(t *testing.T) {
	s := originTestStore(t)
	operation, gameID, fileID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	staged, err := s.Stage(t.Context(), operation, strings.NewReader("original"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = s.PublishOrigin(ctx, gameID, fileID, "game.exe", staged); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
	info, err := s.PublishOrigin(t.Context(), gameID, fileID, "game.exe", staged)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Stage(t.Context(), operation, strings.NewReader("replacement"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishOrigin(t.Context(), gameID, fileID, "game.exe", second); err == nil {
		t.Fatal("overwrote existing original")
	}
	f, err := s.Read(info.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFile(f)
	body, err := io.ReadAll(f)
	if err != nil || string(body) != "original" {
		t.Fatal("collision changed original bytes")
	}
}

type failingOriginalReader struct{ err error }

func (r failingOriginalReader) Read([]byte) (int, error) { return 0, r.err }

func TestOriginalArchiveAdmissionDoesNotWidenOrdinaryLimits(t *testing.T) {
	s := originTestStore(t)
	original := statfs
	t.Cleanup(func() { statfs = original })
	statfs = func(_ string, status *syscall.Statfs_t) error {
		status.Bsize, status.Bavail = 1, uint64(2*MaximumPackageSize)
		return nil
	}
	readErr := errors.New("source read reached")
	r := failingOriginalReader{readErr}
	if _, err := s.StageOrigin(t.Context(), uuid.NewString(), r, MaximumFileSize+1); !errors.Is(err, readErr) {
		t.Fatalf("large outer archive rejected as a logical member: %v", err)
	}
	if _, err := s.StageLimit(t.Context(), uuid.NewString(), r, MaximumFileSize+1); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("ordinary member limit widened: %v", err)
	}
	statfs = func(_ string, status *syscall.Statfs_t) error {
		status.Bsize, status.Bavail = 1, 1024*1024*1024+2
		return nil
	}
	if _, err := s.StageOrigin(t.Context(), uuid.NewString(), r, 3); !errors.Is(err, model.ErrStorageFull) {
		t.Fatalf("original copy did not reserve space before reading: %v", err)
	}
}

func assertOriginalBundle(t *testing.T, s *Store, info *model.OriginGameInfo, expected map[string]string) {
	t.Helper()
	f, err := s.Read(info.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFile(f)
	archive, err := zip.NewReader(f, info.SizeBytes)
	if err != nil || len(archive.File) != len(expected) {
		t.Fatalf("original bundle=%+v err=%v", archive, err)
	}
	for _, member := range archive.File {
		r, openErr := member.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		body, readErr := io.ReadAll(r)
		closeFile(r)
		if readErr != nil || string(body) != expected[member.Name] {
			t.Fatalf("original member %s changed: %v", member.Name, readErr)
		}
	}
}

func TestOriginalPublicationRejectsChangedStaging(t *testing.T) {
	s := originTestStore(t)
	staged, err := s.Stage(t.Context(), uuid.NewString(), strings.NewReader("original"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.root.OpenFile(staged.Key, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString("modified") // Same length: size validation alone cannot detect this.
	if err = errors.Join(writeErr, f.Close()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishOrigin(t.Context(), uuid.NewString(), uuid.NewString(), "raw.exe", staged); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("modified original published with stale SHA: %v", err)
	}
}

func TestOriginalParentsCannotAliasAnotherOwner(t *testing.T) {
	s := originTestStore(t)
	gameID, other := uuid.NewString(), uuid.NewString()
	if err := s.root.MkdirAll("games/"+gameID, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.root.MkdirAll("games/"+other+"/origins", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.root.Symlink("../"+other+"/origins", "games/"+gameID+"/origins"); err != nil {
		t.Fatal(err)
	}
	staged, err := s.Stage(t.Context(), uuid.NewString(), strings.NewReader("original"))
	if err != nil {
		t.Fatal(err)
	}
	fileID := uuid.NewString()
	if _, err = s.PublishOrigin(t.Context(), gameID, fileID, "raw.zip", staged); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("published original into another owner's tree: %v", err)
	}
	if err = s.RemoveOrigin(gameID, fileID); !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("removal followed cross-owner directory alias: %v", err)
	}
}

//go:build integration

package scans

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"retrom/internal/model"
	"retrom/internal/storage"
	"retrom/internal/testsupport"

	"github.com/google/uuid"
)

func TestConvertedUploadRetainsOnlySelectedOriginalAfterStagingDeletion(t *testing.T) {
	s, f, _, originals := krkrOriginFixture(t)
	var err error
	s.Uploads, err = storage.OpenUploads(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Redis, s.Context = &memoryTemporary{}, t.Context()
	upload := model.UploadInput{Format: "pegasus"}
	for _, name := range []string{"metadata.pegasus.txt", "data.xp3", "cover.png"} {
		upload.Files = append(upload.Files, model.UploadFileInput{Path: name, SizeBytes: int64(len(originals[name]))})
	}
	view, err := s.CreateUpload(t.Context(), f.Principal, upload)
	if err != nil {
		t.Fatal(err)
	}
	for i, file := range upload.Files {
		if _, err = s.WriteUpload(t.Context(), f.Principal, view.ID, i, 0, bytes.NewReader(originals[file.Path])); err != nil {
			t.Fatal(err)
		}
	}
	source, err := s.Uploads.Source(view.ID)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := s.Inspect(t.Context(), f.Principal, model.SourceInput{UploadID: view.ID, Format: "pegasus"})
	if err != nil || len(entries) != 1 {
		t.Fatalf("uploaded source=%+v err=%v", entries, err)
	}
	scan, err := s.Create(t.Context(), f.Principal, model.GameScanInput{
		UploadID: view.ID, Format: "pegasus", Mappings: []model.SourceMapping{{SourceKey: entries[0].Key, DirectoryID: f.DirectoryID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Wait()
	progress, err := f.Repository.Scan(t.Context(), scan.ID)
	if err != nil || progress.Status != "completed" || progress.ImportedCount != 1 {
		t.Fatalf("scan=%+v err=%v", progress, err)
	}
	if _, err = os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("upload staging survived completion: %v", err)
	}
	detail := importedOriginGame(t, f)
	assertOriginal(t, s.Storage, detail.Files[0], "data.xp3", originals["data.xp3"])
	assertConvertedXP3(t, s, detail, originals["data.xp3"])
	assertNoStaging(t, s)
}

func TestArchiveAndDirectoryOriginalsStayIndependentOfConvertedPackage(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "raw archive", true: "directory ZIP"}[directory], func(t *testing.T) {
			s, f, source, originals := krkrOriginFixture(t)
			name, body := originalCandidateSource(t, source, originals, directory)
			root, err := s.Sources.Open(source)
			if err != nil {
				t.Fatal(err)
			}
			defer closeRoot(root)
			scan := normalizationScan(t, f)
			candidate := Candidate{Input: f.Game.Input, Files: []string{name}}
			for range 2 {
				if err = s.importGame(t.Context(), root, f.Principal, ".", candidate,
					model.SourceMapping{DirectoryID: f.DirectoryID}, &scan, nil); err != nil {
					t.Fatal(err)
				}
			}
			if scan.ImportedCount != 1 || scan.SkippedCount != 1 {
				t.Fatalf("duplicate scan=%+v", scan)
			}
			detail := importedOriginGame(t, f)
			if directory {
				assertDirectoryOriginal(t, s.Storage, detail.Files[0], originals["data.xp3"])
			} else {
				assertOriginal(t, s.Storage, detail.Files[0], name, body)
			}
			assertManagedFileCount(t, s.Storage, 2)
			assertSourceUnchanged(t, source, originals)
			assertNoStaging(t, s)
		})
	}
}

func TestFailedPublicationRemovesBothPackageAndOriginal(t *testing.T) {
	s, f, source, _ := krkrOriginFixture(t)
	root, err := s.Sources.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot(root)
	scan := normalizationScan(t, f)
	err = s.importGame(t.Context(), root, f.Principal, ".", Candidate{Input: f.Game.Input, Files: []string{"data.xp3"}},
		model.SourceMapping{DirectoryID: f.DirectoryID, TagIDs: []string{uuid.NewString()}}, &scan, nil)
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("removed tag did not roll back publication: %v", err)
	}
	assertCounts(t, f.Repository, scan.ID, 0, 0)
	assertManagedFileCount(t, s.Storage, 0)
	assertNoStaging(t, s)
}

func importedOriginGame(t *testing.T, f testsupport.Fixture) model.GameDetail {
	t.Helper()
	games, err := f.Repository.Games(t.Context(), f.Principal.User.ID, "pending_review", model.Query{Limit: 10})
	if err != nil || len(games.Items) != 1 {
		t.Fatalf("games=%+v err=%v", games, err)
	}
	detail, err := f.Repository.GameDetail(t.Context(), f.Principal.User.ID, games.Items[0].ID, "pending_review")
	if err != nil || len(detail.Files) != 1 || detail.Files[0].ExtInfo.ConversionInfo == nil {
		t.Fatalf("original metadata absent: %+v err=%v", detail.Files, err)
	}
	return detail
}

func assertOriginal(t *testing.T, store *storage.Store, file model.GameFile, name string, expected []byte) {
	t.Helper()
	info := file.ExtInfo.ConversionInfo.OriginInfo
	hash := sha256.Sum256(expected)
	if info == nil || info.LogicalKey != name || info.SizeBytes != int64(len(expected)) || info.SHA256 != hex.EncodeToString(hash[:]) {
		t.Fatalf("original facts=%+v", info)
	}
	assertStoredBytes(t, store, info.StorageKey, expected)
}

func assertDirectoryOriginal(t *testing.T, store *storage.Store, file model.GameFile, expected []byte) {
	t.Helper()
	info := file.ExtInfo.ConversionInfo.OriginInfo
	f, err := store.Read(info.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	defer closeFile(f)
	archive, err := zip.NewReader(f, info.SizeBytes)
	if err != nil || len(archive.File) != 1 || archive.File[0].Name != "project/data.xp3" {
		t.Fatalf("directory original includes unrelated input: %+v err=%v", archive, err)
	}
	r, err := archive.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer closeFile(r)
	body, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(body, expected) {
		t.Fatal("original directory bytes changed")
	}
}

func assertManagedFileCount(t *testing.T, store *storage.Store, expected int) {
	t.Helper()
	directory, err := store.Absolute("games")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	err = filepath.WalkDir(directory, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() {
			count++
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) || count != expected {
		t.Fatalf("managed payloads=%d expected=%d err=%v", count, expected, err)
	}
}

func attachOriginal(t *testing.T, store *storage.Store, gameID string, file *model.GameFile) {
	t.Helper()
	operation := uuid.NewString()
	staged, err := store.Stage(t.Context(), operation, bytes.NewReader([]byte("raw pre-conversion input")))
	if err != nil {
		t.Fatal(err)
	}
	info, err := store.PublishOrigin(t.Context(), gameID, file.ID, "raw.exe", staged)
	if err != nil {
		t.Fatal(err)
	}
	file.ExtInfo.ConversionInfo = &model.GameFileConversionInfo{OriginInfo: info}
	if err = store.RemoveTemporary("temporary/" + operation); err != nil {
		t.Fatal(err)
	}
}

func originalCandidateSource(t *testing.T, source string, originals map[string][]byte, directory bool) (string, []byte) {
	t.Helper()
	name := "original.zip"
	body := originalXP3Zip(t, originals["data.xp3"])
	if directory {
		name = "project"
		if err := os.Mkdir(filepath.Join(source, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, name, "data.xp3"), originals["data.xp3"], 0o600); err != nil {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(filepath.Join(source, name), body, 0o600); err != nil {
		t.Fatal(err)
	}
	return name, body
}

func assertConvertedXP3(t *testing.T, s *Service, detail model.GameDetail, original []byte) {
	t.Helper()
	files, err := s.Repository.ContentFiles(t.Context(), detail.Files)
	if err != nil || len(files) != 1 || files[0].LogicalKey != "data.xp3" {
		t.Fatalf("normalized KRKR files=%+v err=%v", files, err)
	}
	hash := sha256.Sum256(original)
	if files[0].SHA256 == hex.EncodeToString(hash[:]) {
		t.Fatal("fixture did not exercise KRKR content rewriting")
	}
}

func TestUnmodifiedKRKRRepackDoesNotRetainOriginal(t *testing.T) {
	s, f, source, _ := krkrOriginFixture(t)
	original := originXP3(false)
	if err := os.WriteFile(filepath.Join(source, "data.xp3"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := s.Sources.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRoot(root)
	scan := normalizationScan(t, f)
	if err = s.importGame(t.Context(), root, f.Principal, ".", Candidate{Input: f.Game.Input, Files: []string{"data.xp3"}}, model.SourceMapping{DirectoryID: f.DirectoryID}, &scan, nil); err != nil {
		t.Fatal(err)
	}
	games, err := f.Repository.Games(t.Context(), f.Principal.User.ID, "pending_review", model.Query{Limit: 10})
	if err != nil || len(games.Items) != 1 {
		t.Fatalf("games=%+v err=%v", games, err)
	}
	files, err := f.Repository.GameFiles(t.Context(), games.Items[0].ID)
	if err != nil || len(files) != 1 || files[0].ExtInfo.ConversionInfo != nil {
		t.Fatalf("repack retained original: %+v err=%v", files, err)
	}
	assertManagedFileCount(t, s.Storage, 1)
	assertNoStaging(t, s)
}
